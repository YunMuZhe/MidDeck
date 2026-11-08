// Package kafka implements the Kafka driver on top of franz-go.
package kafka

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/compat"
	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/diag"
	"github.com/YunMuZhe/MidDeck/internal/driver"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

// Session is an open Kafka connection.
type Session struct {
	inst   config.Instance
	opts   []kgo.Opt
	dial   driver.DialFunc
	cl     *kgo.Client
	adm    *kadm.Client
	mu     sync.Mutex
	manual *kgo.Client // producer with manual partitioning
}

// Open creates the client and runs layered diagnostics into rep.
// rawDial must not apply TLS; Open wraps it.
func Open(ctx context.Context, inst config.Instance, rawDial driver.DialFunc, rep *diag.Report) (*Session, error) {
	tlsCfg, err := driver.TLSConfig(inst.TLS)
	if err != nil {
		rep.Add(diag.Step{Name: "TLS 配置", Status: diag.Fail, Detail: err.Error()})
		return nil, err
	}
	if len(inst.Addrs) == 0 {
		err := errors.New("至少需要一个 bootstrap 地址")
		rep.Add(diag.Step{Name: "配置检查", Status: diag.Fail, Detail: err.Error()})
		return nil, err
	}

	// 1. TCP (+TLS) to every seed broker.
	reachable := 0
	for _, addr := range inst.Addrs {
		err := rep.Run("TCP 连通 "+addr, func() (string, error) {
			c, err := rawDial(ctx, "tcp", addr)
			if err != nil {
				return "", err
			}
			c.Close()
			return "", nil
		}, driver.NetHint)
		if err == nil {
			reachable++
			if tlsCfg != nil {
				_ = rep.Run("TLS 握手 "+addr, func() (string, error) {
					c, err := driver.WithTLS(rawDial, tlsCfg)(ctx, "tcp", addr)
					if err != nil {
						return "", err
					}
					c.Close()
					return "", nil
				}, driver.NetHint)
			}
		}
	}
	if reachable == 0 {
		return nil, errors.New("所有 bootstrap 地址均不可达")
	}

	dial := driver.WithTLS(rawDial, tlsCfg)
	opts := []kgo.Opt{
		kgo.SeedBrokers(inst.Addrs...),
		kgo.Dialer(dial),
		kgo.DialTimeout(10 * time.Second),
		kgo.RequestTimeoutOverhead(10 * time.Second),
		kgo.RetryTimeout(15 * time.Second),
		kgo.ClientID("middeck"),
	}
	mech, err := saslMechanism(inst.Auth)
	if err != nil {
		rep.Add(diag.Step{Name: "SASL 配置", Status: diag.Fail, Detail: err.Error()})
		return nil, err
	}
	if mech != nil {
		opts = append(opts, kgo.SASL(mech))
	}
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	s := &Session{inst: inst, opts: opts, dial: dial, cl: cl, adm: kadm.NewClient(cl)}

	// 2. Auth + metadata.
	var md kadm.Metadata
	authName := "元数据请求"
	if mech != nil {
		authName = "SASL/" + strings.ToUpper(inst.Auth.Mechanism) + " 认证 + 元数据请求"
	}
	if err := rep.Run(authName, func() (string, error) {
		c, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		var err error
		md, err = s.adm.Metadata(c)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("集群 %s，%d 个 broker，%d 个 topic", md.Cluster, len(md.Brokers), len(md.Topics)), nil
	}, driver.NetHint); err != nil {
		s.Close()
		return nil, err
	}

	// 3. Every advertised broker must be reachable, or produce/fetch will hang.
	for _, b := range md.Brokers {
		addr := net.JoinHostPort(b.Host, strconv.Itoa(int(b.Port)))
		start := time.Now()
		c, err := rawDial(ctx, "tcp", addr)
		step := diag.Step{Name: fmt.Sprintf("Broker %d 地址 %s", b.NodeID, addr), Status: diag.OK, Duration: time.Since(start).Milliseconds()}
		if mapped, ok := inst.Network.AddrMap[addr]; ok {
			step.Detail = "已映射到 " + mapped
		}
		if err != nil {
			step.Status = diag.Warn
			step.Detail = err.Error()
			step.Hint = "broker 对外通告的地址（advertised.listeners）从本机不可达。在“地址映射”中把它映射到可达地址，或配置 SSH 跳板。"
		} else {
			c.Close()
		}
		rep.Add(step)
	}

	// 4. Version.
	_ = rep.Run("服务端版本识别", func() (string, error) {
		info, err := s.Info(ctx)
		if err != nil {
			return "", err
		}
		return info.Version + " · " + info.Compat.Message, nil
	}, nil)

	// 5. Permissions.
	perm := func(name string, fn func(context.Context) error) {
		start := time.Now()
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := fn(c)
		cancel()
		st := diag.Step{Name: "权限：" + name, Status: diag.OK, Duration: time.Since(start).Milliseconds()}
		if err != nil {
			st.Status = diag.Warn
			st.Detail = err.Error()
		}
		rep.Add(st)
	}
	perm("列出 Topic", func(c context.Context) error { _, err := s.adm.ListTopics(c); return err })
	perm("列出消费组", func(c context.Context) error { _, err := s.adm.ListGroups(c); return err })
	return s, nil
}

func saslMechanism(a config.Auth) (sasl.Mechanism, error) {
	switch strings.ToLower(a.Mechanism) {
	case "", "none":
		return nil, nil
	case "plain":
		return plain.Auth{User: a.Username, Pass: a.Password}.AsMechanism(), nil
	case "scram-sha-256":
		return scram.Auth{User: a.Username, Pass: a.Password}.AsSha256Mechanism(), nil
	case "scram-sha-512":
		return scram.Auth{User: a.Username, Pass: a.Password}.AsSha512Mechanism(), nil
	}
	return nil, fmt.Errorf("unsupported SASL mechanism %q", a.Mechanism)
}

// Close releases the clients.
func (s *Session) Close() {
	s.cl.Close()
	s.mu.Lock()
	if s.manual != nil {
		s.manual.Close()
	}
	s.mu.Unlock()
}

var verRe = regexp.MustCompile(`v?(\d+\.\d+(\.\d+)?)`)

// Info guesses the broker version from ApiVersions.
func (s *Session) Info(ctx context.Context) (driver.ServerInfo, error) {
	info := driver.ServerInfo{Kind: "kafka", Extra: map[string]string{}}
	vs, err := s.adm.ApiVersions(ctx)
	if err != nil {
		return info, err
	}
	lowest := ""
	vs.Each(func(v kadm.BrokerApiVersions) {
		if v.Err != nil {
			return
		}
		g := v.VersionGuess()
		info.Extra[fmt.Sprintf("broker-%d", v.NodeID)] = g
		if m := verRe.FindStringSubmatch(g); m != nil {
			if lowest == "" || compat.Compare(m[1], lowest) < 0 {
				lowest = m[1]
			}
		}
	})
	info.Version = lowest
	info.Compat = compat.Check("kafka", lowest)
	if md, err := s.adm.Metadata(ctx); err == nil {
		info.Extra["cluster"] = md.Cluster
		info.Extra["controller"] = strconv.Itoa(int(md.Controller))
	}
	return info, nil
}

// --- topics -----------------------------------------------------------------

type Topic struct {
	Name        string `json:"name"`
	Partitions  int    `json:"partitions"`
	Replication int    `json:"replication"`
	Internal    bool   `json:"internal"`
	Error       string `json:"error,omitempty"`
}

type Partition struct {
	ID          int32   `json:"id"`
	Leader      int32   `json:"leader"`
	Replicas    []int32 `json:"replicas"`
	ISR         []int32 `json:"isr"`
	StartOffset int64   `json:"startOffset"`
	EndOffset   int64   `json:"endOffset"`
}

type ConfigEntry struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Source    string `json:"source"`
	Sensitive bool   `json:"sensitive"`
}

type TopicDetail struct {
	Topic
	PartitionList []Partition     `json:"partitionList"`
	Configs       []ConfigEntry   `json:"configs"`
	Messages      int64           `json:"messages"`
}

func (s *Session) ListTopics(ctx context.Context) ([]Topic, error) {
	td, err := s.adm.ListTopicsWithInternal(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Topic, 0, len(td))
	for _, t := range td {
		x := Topic{Name: t.Topic, Partitions: len(t.Partitions), Internal: t.IsInternal}
		if t.Err != nil {
			x.Error = t.Err.Error()
		}
		for _, p := range t.Partitions {
			x.Replication = len(p.Replicas)
			break
		}
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Session) DescribeTopic(ctx context.Context, name string) (TopicDetail, error) {
	var d TopicDetail
	td, err := s.adm.ListTopics(ctx, name)
	if err != nil {
		return d, err
	}
	t, ok := td[name]
	if !ok {
		return d, fmt.Errorf("topic %s not found", name)
	}
	if t.Err != nil {
		return d, t.Err
	}
	d.Topic = Topic{Name: name, Partitions: len(t.Partitions), Internal: t.IsInternal}
	starts, _ := s.adm.ListStartOffsets(ctx, name)
	ends, _ := s.adm.ListEndOffsets(ctx, name)
	for _, p := range t.Partitions {
		x := Partition{ID: p.Partition, Leader: p.Leader, Replicas: p.Replicas, ISR: p.ISR}
		d.Replication = len(p.Replicas)
		if o, ok := starts.Lookup(name, p.Partition); ok {
			x.StartOffset = o.Offset
		}
		if o, ok := ends.Lookup(name, p.Partition); ok {
			x.EndOffset = o.Offset
		}
		d.Messages += x.EndOffset - x.StartOffset
		d.PartitionList = append(d.PartitionList, x)
	}
	sort.Slice(d.PartitionList, func(i, j int) bool { return d.PartitionList[i].ID < d.PartitionList[j].ID })
	rcs, err := s.adm.DescribeTopicConfigs(ctx, name)
	if err == nil {
		for _, rc := range rcs {
			for _, c := range rc.Configs {
				v := ""
				if c.Value != nil {
					v = *c.Value
				}
				d.Configs = append(d.Configs, ConfigEntry{Key: c.Key, Value: v, Source: c.Source.String(), Sensitive: c.Sensitive})
			}
		}
		sort.Slice(d.Configs, func(i, j int) bool { return d.Configs[i].Key < d.Configs[j].Key })
	}
	return d, nil
}

type CreateTopicReq struct {
	Name        string            `json:"name"`
	Partitions  int32             `json:"partitions"`
	Replication int16             `json:"replication"`
	Configs     map[string]string `json:"configs"`
}

func (s *Session) CreateTopic(ctx context.Context, r CreateTopicReq) error {
	cfg := map[string]*string{}
	for k, v := range r.Configs {
		v := v
		cfg[k] = &v
	}
	resp, err := s.adm.CreateTopic(ctx, r.Partitions, r.Replication, cfg, r.Name)
	if err != nil {
		return err
	}
	return resp.Err
}

func (s *Session) DeleteTopic(ctx context.Context, name string) error {
	resp, err := s.adm.DeleteTopics(ctx, name)
	if err != nil {
		return err
	}
	return resp.Error()
}

// SetPartitions grows the topic to total partitions.
func (s *Session) SetPartitions(ctx context.Context, name string, total int) error {
	resp, err := s.adm.UpdatePartitions(ctx, total, name)
	if err != nil {
		return err
	}
	return resp.Error()
}

// AlterTopicConfigs sets (value != "") or resets (value == "") topic configs incrementally.
func (s *Session) AlterTopicConfigs(ctx context.Context, name string, set map[string]string) error {
	var alters []kadm.AlterConfig
	for k, v := range set {
		v := v
		if v == "" {
			alters = append(alters, kadm.AlterConfig{Op: kadm.DeleteConfig, Name: k})
		} else {
			alters = append(alters, kadm.AlterConfig{Op: kadm.SetConfig, Name: k, Value: &v})
		}
	}
	resp, err := s.adm.AlterTopicConfigs(ctx, alters, name)
	if err != nil {
		return err
	}
	for _, r := range resp {
		if r.Err != nil {
			return r.Err
		}
	}
	return nil
}

// --- messages ---------------------------------------------------------------

type Header struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Message struct {
	Partition int32    `json:"partition"`
	Offset    int64    `json:"offset"`
	Timestamp int64    `json:"timestamp"`
	Key       string   `json:"key"`
	Value     string   `json:"value"`
	Headers   []Header `json:"headers"`
	Size      int      `json:"size"`
}

// FetchReq selects messages. Mode: newest | oldest | offset | timestamp.
type FetchReq struct {
	Topic     string `json:"topic"`
	Partition int32  `json:"partition"` // -1 = all partitions
	Mode      string `json:"mode"`
	Offset    int64  `json:"offset"`
	Timestamp int64  `json:"timestamp"` // unix millis
	Limit     int    `json:"limit"`     // total across partitions
	KeyFilter string `json:"keyFilter"`
	Contains  string `json:"contains"` // substring match on value
}

func (s *Session) Fetch(ctx context.Context, r FetchReq) ([]Message, error) {
	if r.Limit <= 0 || r.Limit > 5000 {
		r.Limit = 100
	}
	starts, err := s.adm.ListStartOffsets(ctx, r.Topic)
	if err != nil {
		return nil, err
	}
	ends, err := s.adm.ListEndOffsets(ctx, r.Topic)
	if err != nil {
		return nil, err
	}
	var byTime kadm.ListedOffsets
	if r.Mode == "timestamp" {
		if byTime, err = s.adm.ListOffsetsAfterMilli(ctx, r.Timestamp, r.Topic); err != nil {
			return nil, err
		}
	}
	parts := map[int32]kgo.Offset{}
	remaining := map[int32]int64{} // stop offset per partition (exclusive)
	var nParts int
	ends.Each(func(o kadm.ListedOffset) {
		if r.Partition >= 0 && o.Partition != r.Partition {
			return
		}
		nParts++
	})
	if nParts == 0 {
		return nil, fmt.Errorf("partition %d not found", r.Partition)
	}
	per := int64(r.Limit/nParts + 1)
	ends.Each(func(o kadm.ListedOffset) {
		if r.Partition >= 0 && o.Partition != r.Partition {
			return
		}
		lo := int64(0)
		if so, ok := starts.Lookup(r.Topic, o.Partition); ok {
			lo = so.Offset
		}
		hi := o.Offset
		var from int64
		switch r.Mode {
		case "oldest":
			from = lo
		case "offset":
			from = r.Offset
		case "timestamp":
			from = hi
			if t, ok := byTime.Lookup(r.Topic, o.Partition); ok && t.Offset >= 0 {
				from = t.Offset
			}
		default: // newest
			from = hi - per
		}
		if from < lo {
			from = lo
		}
		if from >= hi {
			return
		}
		parts[o.Partition] = kgo.NewOffset().At(from)
		remaining[o.Partition] = hi
	})
	if len(parts) == 0 {
		return []Message{}, nil
	}

	opts := append([]kgo.Opt{}, s.opts...)
	opts = append(opts, kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{r.Topic: parts}), kgo.FetchMaxWait(500*time.Millisecond))
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	defer cl.Close()

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var out []Message
	scanned := 0
	for len(remaining) > 0 && len(out) < r.Limit && scanned < r.Limit*200 {
		fs := cl.PollFetches(ctx)
		if ctx.Err() != nil {
			break
		}
		var ferr error
		fs.EachError(func(_ string, _ int32, err error) { ferr = err })
		if ferr != nil && len(out) == 0 {
			return nil, ferr
		}
		fs.EachRecord(func(rec *kgo.Record) {
			stop, ok := remaining[rec.Partition]
			if !ok {
				return
			}
			if rec.Offset+1 >= stop {
				delete(remaining, rec.Partition)
			}
			scanned++
			if len(out) >= r.Limit {
				return
			}
			if r.KeyFilter != "" && string(rec.Key) != r.KeyFilter {
				return
			}
			if r.Contains != "" && !strings.Contains(string(rec.Value), r.Contains) {
				return
			}
			out = append(out, toMessage(rec))
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp != out[j].Timestamp {
			return out[i].Timestamp > out[j].Timestamp
		}
		return out[i].Offset > out[j].Offset
	})
	return out, nil
}

func toMessage(rec *kgo.Record) Message {
	m := Message{
		Partition: rec.Partition, Offset: rec.Offset, Timestamp: rec.Timestamp.UnixMilli(),
		Key: string(rec.Key), Value: string(rec.Value), Size: len(rec.Key) + len(rec.Value),
	}
	for _, h := range rec.Headers {
		m.Headers = append(m.Headers, Header{Key: h.Key, Value: string(h.Value)})
	}
	return m
}

type ProduceReq struct {
	Topic     string   `json:"topic"`
	Partition int32    `json:"partition"` // -1 = by key
	Key       string   `json:"key"`
	Value     string   `json:"value"`
	Headers   []Header `json:"headers"`
}

func (s *Session) Produce(ctx context.Context, r ProduceReq) (Message, error) {
	rec := &kgo.Record{Topic: r.Topic, Value: []byte(r.Value)}
	if r.Key != "" {
		rec.Key = []byte(r.Key)
	}
	for _, h := range r.Headers {
		rec.Headers = append(rec.Headers, kgo.RecordHeader{Key: h.Key, Value: []byte(h.Value)})
	}
	cl := s.cl
	if r.Partition >= 0 {
		rec.Partition = r.Partition
		s.mu.Lock()
		if s.manual == nil {
			opts := append(append([]kgo.Opt{}, s.opts...), kgo.RecordPartitioner(kgo.ManualPartitioner()))
			m, err := kgo.NewClient(opts...)
			if err != nil {
				s.mu.Unlock()
				return Message{}, err
			}
			s.manual = m
		}
		cl = s.manual
		s.mu.Unlock()
	}
	res := cl.ProduceSync(ctx, rec)
	if err := res.FirstErr(); err != nil {
		return Message{}, err
	}
	return toMessage(rec), nil
}

// --- consumer groups --------------------------------------------------------

type GroupSummary struct {
	Group   string `json:"group"`
	State   string `json:"state"`
	Members int    `json:"members"`
	Lag     int64  `json:"lag"`
	Topics  []string `json:"topics"`
	Error   string `json:"error,omitempty"`
}

type PartitionLag struct {
	Topic     string `json:"topic"`
	Partition int32  `json:"partition"`
	Committed int64  `json:"committed"`
	End       int64  `json:"end"`
	Lag       int64  `json:"lag"`
	Member    string `json:"member"`
	Host      string `json:"host"`
}

type GroupDetail struct {
	GroupSummary
	Partitions []PartitionLag `json:"partitions"`
}

func (s *Session) ListGroups(ctx context.Context) ([]GroupSummary, error) {
	lg, err := s.adm.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	lags, err := s.lag(ctx, lg.Groups()...)
	if err != nil {
		return nil, err
	}
	var out []GroupSummary
	for _, l := range lags {
		g := GroupSummary{Group: l.Group, State: l.State, Members: len(l.Members), Lag: l.Lag.Total()}
		for t := range l.Lag {
			g.Topics = append(g.Topics, t)
		}
		sort.Strings(g.Topics)
		if err := l.Error(); err != nil {
			g.Error = err.Error()
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Group < out[j].Group })
	return out, nil
}

// lag fetches group lag, retrying briefly while the group coordinator is unavailable
// (e.g. __consumer_offsets is still being created on a fresh cluster).
func (s *Session) lag(ctx context.Context, groups ...string) (kadm.DescribedGroupLags, error) {
	var (
		lags kadm.DescribedGroupLags
		err  error
	)
	for i := 0; i < 10; i++ {
		lags, err = s.adm.Lag(ctx, groups...)
		if err == nil || !strings.Contains(err.Error(), "COORDINATOR") {
			return lags, err
		}
		select {
		case <-ctx.Done():
			return lags, err
		case <-time.After(time.Second):
		}
	}
	return lags, err
}

func (s *Session) DescribeGroup(ctx context.Context, group string) (GroupDetail, error) {
	var d GroupDetail
	lags, err := s.lag(ctx, group)
	if err != nil {
		return d, err
	}
	l, ok := lags[group]
	if !ok {
		return d, fmt.Errorf("group %s not found", group)
	}
	if err := l.Error(); err != nil {
		return d, err
	}
	d.GroupSummary = GroupSummary{Group: group, State: l.State, Members: len(l.Members), Lag: l.Lag.Total()}
	for t, ps := range l.Lag {
		d.Topics = append(d.Topics, t)
		for _, p := range ps {
			pl := PartitionLag{Topic: t, Partition: p.Partition, Committed: p.Commit.At, End: p.End.Offset, Lag: p.Lag}
			if p.Member != nil {
				pl.Member = p.Member.ClientID
				pl.Host = p.Member.ClientHost
			}
			d.Partitions = append(d.Partitions, pl)
		}
	}
	sort.Strings(d.Topics)
	sort.Slice(d.Partitions, func(i, j int) bool {
		a, b := d.Partitions[i], d.Partitions[j]
		if a.Topic != b.Topic {
			return a.Topic < b.Topic
		}
		return a.Partition < b.Partition
	})
	return d, nil
}

// ResetReq moves a group's committed offsets. Mode: earliest | latest | timestamp | offset | shift.
type ResetReq struct {
	Group     string  `json:"group"`
	Topic     string  `json:"topic"`
	Partitions []int32 `json:"partitions"` // empty = all
	Mode      string  `json:"mode"`
	Value     int64   `json:"value"` // offset, unix millis, or shift delta
	DryRun    bool    `json:"dryRun"`
}

type OffsetChange struct {
	Partition int32 `json:"partition"`
	From      int64 `json:"from"`
	To        int64 `json:"to"`
}

// ResetOffsets computes (and unless DryRun, commits) new offsets.
// Kafka only accepts this when the group has no active members.
func (s *Session) ResetOffsets(ctx context.Context, r ResetReq) ([]OffsetChange, error) {
	d, err := s.DescribeGroup(ctx, r.Group)
	if err != nil {
		return nil, err
	}
	if !r.DryRun && d.State != "Empty" && d.State != "Dead" {
		return nil, fmt.Errorf("消费组当前状态为 %s，Kafka 只允许在没有活跃消费者（Empty）时重置 offset，请先停止消费者", d.State)
	}
	starts, err := s.adm.ListStartOffsets(ctx, r.Topic)
	if err != nil {
		return nil, err
	}
	ends, err := s.adm.ListEndOffsets(ctx, r.Topic)
	if err != nil {
		return nil, err
	}
	var byTime kadm.ListedOffsets
	if r.Mode == "timestamp" {
		if byTime, err = s.adm.ListOffsetsAfterMilli(ctx, r.Value, r.Topic); err != nil {
			return nil, err
		}
	}
	committed := map[int32]int64{}
	for _, p := range d.Partitions {
		if p.Topic == r.Topic {
			committed[p.Partition] = p.Committed
		}
	}
	want := map[int32]bool{}
	for _, p := range r.Partitions {
		want[p] = true
	}
	var changes []OffsetChange
	offsets := kadm.Offsets{}
	ends.Each(func(o kadm.ListedOffset) {
		if len(want) > 0 && !want[o.Partition] {
			return
		}
		lo := int64(0)
		if so, ok := starts.Lookup(r.Topic, o.Partition); ok {
			lo = so.Offset
		}
		hi := o.Offset
		from, ok := committed[o.Partition]
		if !ok {
			from = -1
		}
		var to int64
		switch r.Mode {
		case "earliest":
			to = lo
		case "latest":
			to = hi
		case "offset":
			to = r.Value
		case "timestamp":
			to = hi
			if t, ok := byTime.Lookup(r.Topic, o.Partition); ok && t.Offset >= 0 {
				to = t.Offset
			}
		case "shift":
			base := from
			if base < 0 {
				base = lo
			}
			to = base + r.Value
		}
		if to < lo {
			to = lo
		}
		if to > hi {
			to = hi
		}
		changes = append(changes, OffsetChange{Partition: o.Partition, From: from, To: to})
		offsets.Add(kadm.Offset{Topic: r.Topic, Partition: o.Partition, At: to, LeaderEpoch: -1})
	})
	sort.Slice(changes, func(i, j int) bool { return changes[i].Partition < changes[j].Partition })
	if r.DryRun {
		return changes, nil
	}
	resp, err := s.adm.CommitOffsets(ctx, r.Group, offsets)
	if err != nil {
		return nil, err
	}
	return changes, resp.Error()
}

func (s *Session) DeleteGroup(ctx context.Context, group string) error {
	resp, err := s.adm.DeleteGroups(ctx, group)
	if err != nil {
		return err
	}
	return resp.Error()
}
