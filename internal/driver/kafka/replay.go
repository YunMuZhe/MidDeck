package kafka

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/YunMuZhe/MidDeck/internal/i18n"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Bytes is a record key or value. It is written to JSON as a plain string when it
// is valid UTF-8 (so parked files stay readable) and as {"base64": "…"} otherwise,
// so binary payloads survive a round trip unchanged.
type Bytes []byte

func (b Bytes) MarshalJSON() ([]byte, error) {
	if b == nil {
		return []byte("null"), nil
	}
	if utf8.Valid(b) {
		return json.Marshal(string(b))
	}
	return json.Marshal(map[string]string{"base64": base64.StdEncoding.EncodeToString(b)})
}

func (b *Bytes) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*b = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*b = Bytes(s)
		return nil
	}
	var o struct {
		Base64 string `json:"base64"`
	}
	if err := json.Unmarshal(data, &o); err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(o.Base64)
	*b = raw
	return err
}

// RecordHeader is a header with raw bytes.
type RecordHeader struct {
	Key   string `json:"key"`
	Value Bytes  `json:"value"`
}

// Record is a message exactly as stored, for parking and replay.
type Record struct {
	Partition int32          `json:"partition"`
	Offset    int64          `json:"offset"`
	Timestamp int64          `json:"timestamp"`
	Key       Bytes          `json:"key"`
	Value     Bytes          `json:"value"`
	Headers   []RecordHeader `json:"headers,omitempty"`
}

// Range is a half-open offset range [From, To) in one partition.
type Range struct {
	Partition int32 `json:"partition"`
	From      int64 `json:"from"`
	To        int64 `json:"to"`
}

// ReadRanges reads every record in the ranges. It is strict: it either returns
// all of them or an error, because callers skip past what it returns.
// Transaction markers are consumed but not returned.
func (s *Session) ReadRanges(ctx context.Context, topic string, ranges []Range) ([]Record, error) {
	parts := map[int32]kgo.Offset{}
	stop := map[int32]int64{}
	for _, r := range ranges {
		if r.To > r.From {
			parts[r.Partition] = kgo.NewOffset().At(r.From)
			stop[r.Partition] = r.To
		}
	}
	if len(parts) == 0 {
		return []Record{}, nil
	}
	opts := append([]kgo.Opt{}, s.opts...)
	opts = append(opts,
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{topic: parts}),
		kgo.FetchMaxWait(500*time.Millisecond),
		kgo.KeepControlRecords(),
	)
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var out []Record
	for len(stop) > 0 {
		fs := cl.PollFetches(ctx)
		if ctx.Err() != nil {
			var left []string
			for p, to := range stop {
				left = append(left, fmt.Sprintf("%d (up to %d)", p, to))
			}
			sort.Strings(left)
			return nil, fmt.Errorf("timed out reading partition(s) %v; nothing was changed", left)
		}
		var ferr error
		fs.EachError(func(_ string, _ int32, err error) { ferr = err })
		if ferr != nil {
			return nil, ferr
		}
		fs.EachRecord(func(rec *kgo.Record) {
			to, ok := stop[rec.Partition]
			if !ok || rec.Offset >= to {
				delete(stop, rec.Partition)
				return
			}
			if rec.Offset+1 >= to {
				delete(stop, rec.Partition)
			}
			if rec.Attrs.IsControl() {
				return
			}
			r := Record{Partition: rec.Partition, Offset: rec.Offset, Timestamp: rec.Timestamp.UnixMilli(), Key: rec.Key, Value: rec.Value}
			for _, h := range rec.Headers {
				r.Headers = append(r.Headers, RecordHeader{Key: h.Key, Value: h.Value})
			}
			out = append(out, r)
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Partition != out[j].Partition {
			return out[i].Partition < out[j].Partition
		}
		return out[i].Offset < out[j].Offset
	})
	return out, nil
}

// SkipReq asks to move a group past the next Count messages of each partition.
type SkipReq struct {
	Group  string          `json:"group"`
	Topic  string          `json:"topic"`
	Counts map[int32]int64 `json:"counts"` // partition -> messages to skip
}

// MaxSkip bounds one skip so a typo cannot park a whole topic.
const MaxSkip = 10000

// SkipRanges works out which offsets a skip would cover, from the group's committed offsets.
func (s *Session) SkipRanges(ctx context.Context, r SkipReq) ([]Range, GroupDetail, error) {
	d, err := s.DescribeGroup(ctx, r.Group)
	if err != nil {
		return nil, d, err
	}
	committed := map[int32]PartitionLag{}
	for _, p := range d.Partitions {
		if p.Topic == r.Topic {
			committed[p.Partition] = p
		}
	}
	var out []Range
	total := int64(0)
	for _, p := range sortedParts(r.Counts) {
		n := r.Counts[p]
		if n <= 0 {
			continue
		}
		c, ok := committed[p]
		if !ok || c.Committed < 0 {
			return nil, d, errors.New(i18n.T("kafka.skipNoCommit", r.Group, p))
		}
		to := c.Committed + n
		if to > c.End {
			to = c.End
		}
		if to > c.Committed {
			out = append(out, Range{Partition: p, From: c.Committed, To: to})
			total += to - c.Committed
		}
	}
	if total > MaxSkip {
		return nil, d, errors.New(i18n.T("kafka.skipTooMany", MaxSkip))
	}
	if len(out) == 0 {
		return nil, d, errors.New(i18n.T("kafka.skipNothing"))
	}
	return out, d, nil
}

func sortedParts(m map[int32]int64) []int32 {
	ps := make([]int32, 0, len(m))
	for p := range m {
		ps = append(ps, p)
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i] < ps[j] })
	return ps
}

// CommitTo sets the group's committed offsets for the ranges to their ends.
// Kafka only accepts this while the group has no active members.
func (s *Session) CommitTo(ctx context.Context, group, topic string, ranges []Range) error {
	d, err := s.DescribeGroup(ctx, group)
	if err != nil {
		return err
	}
	if d.State != "Empty" && d.State != "Dead" {
		return errors.New(i18n.T("kafka.resetActive", d.State))
	}
	offsets := kadm.Offsets{}
	for _, r := range ranges {
		offsets.Add(kadm.Offset{Topic: topic, Partition: r.Partition, At: r.To, LeaderEpoch: -1})
	}
	resp, err := s.adm.CommitOffsets(ctx, group, offsets)
	if err != nil {
		return err
	}
	return resp.Error()
}

// PartitionCount returns how many partitions a topic has.
func (s *Session) PartitionCount(ctx context.Context, topic string) (int, error) {
	td, err := s.adm.ListTopics(ctx, topic)
	if err != nil {
		return 0, err
	}
	t, ok := td[topic]
	if !ok || t.Err != nil {
		return 0, fmt.Errorf("topic %s not found", topic)
	}
	return len(t.Partitions), nil
}

// ProduceRecords writes records to topic in order. With keepPartition each goes
// to its original partition; otherwise the key decides. extra headers replace
// headers of the same name. It returns how many were written before any error.
func (s *Session) ProduceRecords(ctx context.Context, topic string, recs []Record, keepPartition bool, extra []RecordHeader) (int, error) {
	cl := s.cl
	if keepPartition {
		n, err := s.PartitionCount(ctx, topic)
		if err != nil {
			return 0, err
		}
		for _, r := range recs {
			if int(r.Partition) >= n {
				return 0, errors.New(i18n.T("kafka.replayPartition", topic, n, r.Partition))
			}
		}
		if cl, err = s.manualClient(); err != nil {
			return 0, err
		}
	}
	drop := map[string]bool{}
	for _, h := range extra {
		drop[h.Key] = true
	}
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		ok    int
		first error
	)
	for _, r := range recs {
		rec := &kgo.Record{Topic: topic, Key: r.Key, Value: r.Value}
		if keepPartition {
			rec.Partition = r.Partition
		}
		for _, h := range r.Headers {
			if !drop[h.Key] {
				rec.Headers = append(rec.Headers, kgo.RecordHeader{Key: h.Key, Value: h.Value})
			}
		}
		for _, h := range extra {
			rec.Headers = append(rec.Headers, kgo.RecordHeader{Key: h.Key, Value: h.Value})
		}
		wg.Add(1)
		cl.Produce(ctx, rec, func(_ *kgo.Record, err error) {
			defer wg.Done()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				return
			}
			ok++
		})
	}
	wg.Wait()
	return ok, first
}

func (s *Session) manualClient() (*kgo.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.manual == nil {
		opts := append(append([]kgo.Opt{}, s.opts...), kgo.RecordPartitioner(kgo.ManualPartitioner()))
		m, err := kgo.NewClient(opts...)
		if err != nil {
			return nil, err
		}
		s.manual = m
	}
	return s.manual, nil
}
