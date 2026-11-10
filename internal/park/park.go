// Package park keeps messages that were skipped past, so they can be replayed
// once the consumer is fixed. Each batch is a directory under the data dir:
//
//	parked/<id>/batch.json      metadata and replay history
//	parked/<id>/messages.jsonl  one kafka.Record per line, exactly as read
package park

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/driver/kafka"
)

// Batch describes one set of parked messages.
type Batch struct {
	ID           string        `json:"id"`
	InstanceID   string        `json:"instanceId"`
	InstanceName string        `json:"instanceName"`
	Group        string        `json:"group"`
	Topic        string        `json:"topic"`
	Reason       string        `json:"reason,omitempty"`
	CreatedAt    time.Time     `json:"createdAt"`
	Count        int           `json:"count"`
	Bytes        int64         `json:"bytes"`
	Ranges       []kafka.Range `json:"ranges"`
	// Skipped is true once the group's offsets were moved past the batch.
	Skipped bool     `json:"skipped"`
	Replays []Replay `json:"replays"`
	Dir     string   `json:"dir"`
}

// Replay is one time the batch was produced again.
type Replay struct {
	At             time.Time `json:"at"`
	InstanceID     string    `json:"instanceId"`
	InstanceName   string    `json:"instanceName"`
	Topic          string    `json:"topic"`
	KeepPartitions bool      `json:"keepPartitions"`
	Count          int       `json:"count"`
	Error          string    `json:"error,omitempty"`
}

// Store is the parked/ directory.
type Store struct{ dir string }

func Open(dataDir string) *Store { return &Store{dir: filepath.Join(dataDir, "parked")} }

var idRe = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[a-z0-9]+$`)

func (s *Store) path(id string, name string) (string, error) {
	if !idRe.MatchString(id) {
		return "", fmt.Errorf("invalid batch id %q", id)
	}
	return filepath.Join(s.dir, id, name), nil
}

// NewID returns a sortable batch id.
func NewID(now time.Time, suffix string) string {
	return now.Format("20060102-150405") + "-" + suffix
}

// Save writes the messages, then the metadata, and syncs both to disk before
// returning: callers move offsets only after Save succeeds.
func (s *Store) Save(b *Batch, recs []kafka.Record) error {
	dir := filepath.Join(s.dir, b.ID)
	if _, err := s.path(b.ID, ""); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "messages.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	b.Count, b.Bytes = len(recs), 0
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			f.Close()
			return err
		}
		b.Bytes += int64(len(r.Key) + len(r.Value))
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return s.Update(b)
}

// Update rewrites a batch's metadata atomically.
func (s *Store) Update(b *Batch) error {
	p, err := s.path(b.ID, "batch.json")
	if err != nil {
		return err
	}
	if b.Replays == nil {
		b.Replays = []Replay{}
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, p)
}

// Get loads one batch's metadata.
func (s *Store) Get(id string) (*Batch, error) {
	p, err := s.path(id, "batch.json")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var b Batch
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	b.Dir = filepath.Dir(p)
	return &b, nil
}

// List returns batches, newest first; instanceID "" lists all.
func (s *Store) List(instanceID string) ([]Batch, error) {
	ents, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Batch{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Batch{}
	for _, e := range ents {
		if !e.IsDir() || !idRe.MatchString(e.Name()) {
			continue
		}
		b, err := s.Get(e.Name())
		if err != nil || instanceID != "" && b.InstanceID != instanceID {
			continue
		}
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// Records reads up to limit records (limit <= 0: all).
func (s *Store) Records(id string, limit int) ([]kafka.Record, error) {
	p, err := s.path(id, "messages.jsonl")
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 256<<20)
	out := []kafka.Record{}
	for sc.Scan() && (limit <= 0 || len(out) < limit) {
		var r kafka.Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("line %d: %w", len(out)+1, err)
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

// Delete removes a batch.
func (s *Store) Delete(id string) error {
	p, err := s.path(id, "")
	if err != nil {
		return err
	}
	return os.RemoveAll(p)
}
