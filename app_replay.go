package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/driver/kafka"
	"github.com/YunMuZhe/MidDeck/internal/i18n"
	"github.com/YunMuZhe/MidDeck/internal/park"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Headers added to replayed messages so consumers (and people) can tell them apart.
const (
	HeaderReplay     = "x-middeck-replay"      // batch id
	HeaderOrigin     = "x-middeck-origin"      // topic/partition/offset it was parked from
	HeaderReplayedAt = "x-middeck-replayed-at" // RFC 3339
)

func (a *App) parked() *park.Store { return park.Open(a.dataDir) }

// SkipPreview shows what a skip would park.
type SkipPreview struct {
	Ranges   []kafka.Range   `json:"ranges"`
	Messages []kafka.Message `json:"messages"`
	Total    int             `json:"total"`
	State    string          `json:"state"`
}

const previewLimit = 200

// KafkaSkipPreview reads the messages a skip would cover, without changing anything.
func (a *App) KafkaSkipPreview(id string, r kafka.SkipReq) (SkipPreview, error) {
	s, _, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return SkipPreview{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	ranges, d, err := s.SkipRanges(ctx, r)
	if err != nil {
		return SkipPreview{}, err
	}
	recs, err := s.ReadRanges(ctx, r.Topic, ranges)
	if err != nil {
		return SkipPreview{}, err
	}
	p := SkipPreview{Ranges: ranges, Total: len(recs), State: d.State, Messages: []kafka.Message{}}
	for i, rec := range recs {
		if i == previewLimit {
			break
		}
		p.Messages = append(p.Messages, truncated(rec.Message()))
	}
	return p, nil
}

func truncated(m kafka.Message) kafka.Message {
	if len(m.Value) > 4096 {
		m.Value = m.Value[:4096] + "…"
	}
	return m
}

// KafkaSkip parks the messages a skip covers and, when move is true, commits the
// group past them. The file is written and synced before offsets move, so a
// failure at any point never loses messages.
func (a *App) KafkaSkip(id string, r kafka.SkipReq, reason string, move bool) (*park.Batch, error) {
	s, c, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return nil, err
	}
	if move && c.inst.ReadOnly {
		return nil, errors.New(i18n.T("err.readOnly", c.inst.Name, "kafka.skip"))
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
	defer cancel()
	ranges, d, err := s.SkipRanges(ctx, r)
	if err != nil {
		return nil, err
	}
	if move && d.State != "Empty" && d.State != "Dead" {
		return nil, errors.New(i18n.T("kafka.resetActive", d.State))
	}
	recs, err := s.ReadRanges(ctx, r.Topic, ranges)
	if err != nil {
		return nil, err
	}
	b := &park.Batch{
		ID: park.NewID(time.Now(), config.NewID()[:6]), InstanceID: c.inst.ID, InstanceName: c.inst.Name,
		Group: r.Group, Topic: r.Topic, Reason: strings.TrimSpace(reason), CreatedAt: time.Now(), Ranges: ranges,
	}
	st := a.parked()
	if err := st.Save(b, recs); err != nil {
		return nil, fmt.Errorf("save parked messages: %w", err)
	}
	if !move {
		return st.Get(b.ID)
	}
	err = a.write(c, "kafka.skip", map[string]any{"group": r.Group, "topic": r.Topic, "ranges": ranges, "batch": b.ID, "reason": b.Reason},
		func() error { return s.CommitTo(ctx, r.Group, r.Topic, ranges) })
	if err != nil {
		return nil, errors.New(i18n.T("kafka.skipCommitFailed", err.Error(), b.ID))
	}
	b.Skipped = true
	if err := st.Update(b); err != nil {
		return nil, err
	}
	return st.Get(b.ID)
}

// ParkedBatches lists parked batches for an instance ("" = all).
func (a *App) ParkedBatches(instanceID string) ([]park.Batch, error) {
	return a.parked().List(instanceID)
}

// ParkedMessages returns a batch's messages for display.
func (a *App) ParkedMessages(batchID string, limit int) ([]kafka.Message, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	recs, err := a.parked().Records(batchID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]kafka.Message, 0, len(recs))
	for _, r := range recs {
		out = append(out, truncated(r.Message()))
	}
	return out, nil
}

// ReplayReq produces a parked batch again.
type ReplayReq struct {
	BatchID        string `json:"batchId"`
	InstanceID     string `json:"instanceId"` // target; "" = where it was parked from
	Topic          string `json:"topic"`      // "" = original topic
	KeepPartitions bool   `json:"keepPartitions"`
	// Force replays a batch that was already replayed.
	Force bool `json:"force"`
}

// KafkaReplay writes a parked batch back, tagging every message with replay headers.
func (a *App) KafkaReplay(r ReplayReq) (park.Replay, error) {
	st := a.parked()
	b, err := st.Get(r.BatchID)
	if err != nil {
		return park.Replay{}, err
	}
	if len(b.Replays) > 0 && !r.Force {
		last := b.Replays[len(b.Replays)-1]
		if last.Error == "" {
			return park.Replay{}, errors.New(i18n.T("kafka.replayAgain", last.At.Local().Format("2006-01-02 15:04"), last.Topic))
		}
	}
	if r.InstanceID == "" {
		r.InstanceID = b.InstanceID
	}
	if r.Topic == "" {
		r.Topic = b.Topic
	}
	s, c, err := sessionAs[*kafka.Session](a, r.InstanceID)
	if err != nil {
		return park.Replay{}, err
	}
	recs, err := st.Records(b.ID, 0)
	if err != nil {
		return park.Replay{}, err
	}
	now := time.Now()
	for i := range recs {
		hs := recs[i].Headers[:0:0]
		for _, h := range recs[i].Headers {
			if !strings.HasPrefix(h.Key, "x-middeck-") {
				hs = append(hs, h)
			}
		}
		recs[i].Headers = append(hs,
			kafka.RecordHeader{Key: HeaderReplay, Value: kafka.Bytes(b.ID)},
			kafka.RecordHeader{Key: HeaderOrigin, Value: kafka.Bytes(b.Topic + "/" + strconv.Itoa(int(recs[i].Partition)) + "/" + strconv.FormatInt(recs[i].Offset, 10))},
			kafka.RecordHeader{Key: HeaderReplayedAt, Value: kafka.Bytes(now.UTC().Format(time.RFC3339))},
		)
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
	defer cancel()
	rep := park.Replay{At: now, InstanceID: c.inst.ID, InstanceName: c.inst.Name, Topic: r.Topic, KeepPartitions: r.KeepPartitions}
	err = a.write(c, "kafka.replay", map[string]any{"batch": b.ID, "topic": r.Topic, "count": len(recs), "keepPartitions": r.KeepPartitions},
		func() error {
			var e error
			rep.Count, e = s.ProduceRecords(ctx, r.Topic, recs, r.KeepPartitions, nil)
			return e
		})
	if err != nil {
		rep.Error = err.Error()
	}
	if rep.Count > 0 || err != nil {
		b.Replays = append(b.Replays, rep)
		if uerr := st.Update(b); uerr != nil && err == nil {
			err = uerr
		}
	}
	return rep, err
}

// DeleteParked removes a batch from disk.
func (a *App) DeleteParked(batchID string) error { return a.parked().Delete(batchID) }

// ExportParked copies a batch's messages (JSON Lines) to a file the user picks.
func (a *App) ExportParked(batchID string) (string, error) {
	b, err := a.parked().Get(batchID)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(b.Dir + "/messages.jsonl")
	if err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title: i18n.T("dlg.exportMessages"), DefaultFilename: fmt.Sprintf("%s-%s.jsonl", b.Topic, b.ID),
	})
	if err != nil || path == "" {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o600)
}
