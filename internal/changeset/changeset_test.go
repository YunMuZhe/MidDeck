package changeset

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/YunMuZhe/MidDeck/internal/driver/kafka"
	"github.com/YunMuZhe/MidDeck/internal/driver/rabbitmq"
)

type fakeKafka struct {
	topics map[string]*kafka.TopicDetail
	calls  []string
}

func (f *fakeKafka) ListTopics(context.Context) ([]kafka.Topic, error) {
	var out []kafka.Topic
	for _, t := range f.topics {
		out = append(out, t.Topic)
	}
	return out, nil
}

func (f *fakeKafka) DescribeTopic(_ context.Context, n string) (kafka.TopicDetail, error) {
	if t, ok := f.topics[n]; ok {
		return *t, nil
	}
	return kafka.TopicDetail{}, errors.New("not found")
}

func (f *fakeKafka) CreateTopic(_ context.Context, r kafka.CreateTopicReq) error {
	f.calls = append(f.calls, "create "+r.Name)
	f.topics[r.Name] = &kafka.TopicDetail{Topic: kafka.Topic{Name: r.Name, Partitions: int(r.Partitions), Replication: int(r.Replication)}}
	for k, v := range r.Configs {
		f.topics[r.Name].Configs = append(f.topics[r.Name].Configs, kafka.ConfigEntry{Key: k, Value: v, Source: "DYNAMIC_TOPIC_CONFIG"})
	}
	return nil
}

func (f *fakeKafka) SetPartitions(_ context.Context, n string, total int) error {
	f.calls = append(f.calls, "partitions "+n)
	f.topics[n].Partitions = total
	return nil
}

func (f *fakeKafka) AlterTopicConfigs(_ context.Context, n string, set map[string]string) error {
	f.calls = append(f.calls, "configs "+n)
	t := f.topics[n]
	for k, v := range set {
		found := false
		for i := range t.Configs {
			if t.Configs[i].Key == k {
				t.Configs[i].Value, found = v, true
			}
		}
		if !found {
			t.Configs = append(t.Configs, kafka.ConfigEntry{Key: k, Value: v, Source: "DYNAMIC_TOPIC_CONFIG"})
		}
	}
	return nil
}

func (f *fakeKafka) DeleteTopic(_ context.Context, n string) error {
	f.calls = append(f.calls, "delete "+n)
	delete(f.topics, n)
	return nil
}

const kafkaYAML = `
version: 1
kafka:
  defaults:
    replication: 3
    configs:
      min.insync.replicas: 2
  topics:
    - name: orders
      partitions: 12
      configs:
        retention.ms: 604800000
    - name: payments
      partitions: 6
    - name: legacy
      state: absent
envs:
  dev:
    kafka:
      defaults:
        replication: 1
        configs: {min.insync.replicas: "1"}
      topics:
        orders: {partitions: 3}
`

func TestKafkaPlanApply(t *testing.T) {
	s, err := Parse([]byte(kafkaYAML))
	if err != nil {
		t.Fatal(err)
	}
	k := &fakeKafka{topics: map[string]*kafka.TopicDetail{
		"payments": {Topic: kafka.Topic{Name: "payments", Partitions: 3, Replication: 3},
			Configs: []kafka.ConfigEntry{{Key: "min.insync.replicas", Value: "2"}}},
		"legacy": {Topic: kafka.Topic{Name: "legacy", Partitions: 1, Replication: 3}},
	}}
	ctx := context.Background()
	p, err := PlanKafka(ctx, s, "prod", k)
	if err != nil {
		t.Fatal(err)
	}
	if p.Count(Create) != 1 || p.Count(Update) != 1 || p.Count(Delete) != 1 {
		t.Fatalf("unexpected plan:\n%s", Format(p))
	}
	if c := p.Changes[0]; c.Name != "orders" || !hasDiff(c, "retention.ms", "604800000") || !hasDiff(c, "replication", "3") {
		t.Fatalf("create orders: %+v", c)
	}
	if c := p.Changes[1]; c.Name != "payments" || len(c.Diffs) != 1 || c.Diffs[0] != (Diff{"partitions", "3", "6"}) {
		t.Fatalf("update payments: %+v", c)
	}
	if p.Changes[2].Name != "legacy" {
		t.Fatal("deletes go last")
	}
	if _, err := Apply(ctx, p, ApplyOptions{}); !errors.Is(err, ErrDeletes) {
		t.Fatalf("deletes need opt-in, got %v", err)
	}
	if _, err := Apply(ctx, p, ApplyOptions{AllowDelete: true, Fingerprint: "nope"}); !errors.Is(err, ErrStale) {
		t.Fatalf("stale fingerprint, got %v", err)
	}
	var guarded []string
	res, err := Apply(ctx, p, ApplyOptions{AllowDelete: true, Fingerprint: p.Fingerprint, Guard: func(c Change, run func() error) error {
		guarded = append(guarded, c.Name)
		return run()
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Status != "applied" {
			t.Fatalf("result %+v", r)
		}
	}
	if strings.Join(k.calls, ",") != "create orders,partitions payments,delete legacy" || len(guarded) != 3 {
		t.Fatalf("calls %v guarded %v", k.calls, guarded)
	}
	// Second plan is empty.
	p2, _ := PlanKafka(ctx, s, "prod", k)
	if len(p2.Changes) != 0 || p2.Unchanged != 3 {
		t.Fatalf("not converged:\n%s", Format(p2))
	}
}

func TestKafkaEnvOverride(t *testing.T) {
	s, _ := Parse([]byte(kafkaYAML))
	k := &fakeKafka{topics: map[string]*kafka.TopicDetail{
		"payments": {Topic: kafka.Topic{Name: "payments", Partitions: 6, Replication: 3}},
	}}
	p, err := PlanKafka(context.Background(), s, "dev", k)
	if err != nil {
		t.Fatal(err)
	}
	o := p.Changes[0]
	if o.Name != "orders" || !hasDiff(o, "partitions", "3") || !hasDiff(o, "replication", "1") || !hasDiff(o, "min.insync.replicas", "1") {
		t.Fatalf("dev overrides not applied: %+v", o)
	}
	// payments: replication 3 on the cluster vs 1 for dev → warning, not a change.
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "replication") {
		t.Fatalf("warnings %v", p.Warnings)
	}
	if p.Changes[1].Name != "payments" || !hasDiff(p.Changes[1], "min.insync.replicas", "1") {
		t.Fatalf("payments: %+v", p.Changes[1])
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{
		"",
		"version: 2\nkafka: {topics: [{name: a}]}",
		"kafka: {topics: [{name: a}, {name: a}]}",
		"kafka: {topics: [{name: a, state: gone}]}",
		"kafka: {topics: [{name: a, partitons: 3}]}", // typo
		"rabbitmq: {bindings: [{exchange: x}]}",
		"rabbitmq: {exchanges: [{name: amq.topic}]}",
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

type fakeRabbit struct {
	xs    []rabbitmq.Exchange
	qs    []rabbitmq.Queue
	bs    []rabbitmq.Binding
	calls []string
}

func (f *fakeRabbit) ListExchanges(context.Context) ([]rabbitmq.Exchange, error) { return f.xs, nil }
func (f *fakeRabbit) ListQueues(context.Context) ([]rabbitmq.Queue, error)       { return f.qs, nil }
func (f *fakeRabbit) ListBindings(context.Context) ([]rabbitmq.Binding, error)   { return f.bs, nil }
func (f *fakeRabbit) CreateExchange(_ context.Context, r rabbitmq.CreateExchangeReq) error {
	f.calls = append(f.calls, "x+ "+r.Name+" "+r.Type)
	return nil
}
func (f *fakeRabbit) DeleteExchange(_ context.Context, n string) error {
	f.calls = append(f.calls, "x- "+n)
	return nil
}
func (f *fakeRabbit) CreateQueue(_ context.Context, r rabbitmq.CreateQueueReq) error {
	f.calls = append(f.calls, "q+ "+r.Name+" "+r.Type)
	return nil
}
func (f *fakeRabbit) DeleteQueue(_ context.Context, n string) error {
	f.calls = append(f.calls, "q- "+n)
	return nil
}
func (f *fakeRabbit) CreateBinding(_ context.Context, b rabbitmq.Binding) error {
	f.calls = append(f.calls, "b+ "+b.Source+">"+b.Destination)
	return nil
}
func (f *fakeRabbit) DeleteBinding(_ context.Context, b rabbitmq.Binding) error {
	f.calls = append(f.calls, "b- "+b.PropertiesKey)
	return nil
}

func TestRabbitPlan(t *testing.T) {
	s, err := Parse([]byte(`
rabbitmq:
  exchanges:
    - {name: orders, type: topic}
    - {name: audit, type: fanout}
  queues:
    - {name: order-refund, type: quorum, arguments: {x-delivery-limit: 5}}
    - {name: order-mail, arguments: {x-message-ttl: 60000}}
    - {name: tmp, state: absent}
  bindings:
    - {exchange: orders, queue: order-refund, routingKey: order.refund.*}
    - {exchange: orders, queue: order-mail, routingKey: order.created}
    - {exchange: orders, queue: tmp, state: absent}
`))
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRabbit{
		xs: []rabbitmq.Exchange{{Name: "orders", Type: "topic", Durable: true}, {Name: "audit", Type: "direct", Durable: true}},
		qs: []rabbitmq.Queue{
			{Name: "order-mail", Type: "classic", Durable: true, Arguments: map[string]any{"x-message-ttl": float64(60000)}},
			{Name: "tmp", Type: "classic", Messages: 4},
		},
		bs: []rabbitmq.Binding{
			{Source: "orders", Destination: "order-mail", DestinationType: "queue", RoutingKey: "order.created", Arguments: map[string]any{}},
			{Source: "orders", Destination: "tmp", DestinationType: "queue", PropertiesKey: "~"},
		},
	}
	p, err := PlanRabbit(context.Background(), s, r)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, c := range p.Changes {
		got = append(got, c.Action+" "+c.Resource+" "+c.Name)
	}
	want := []string{
		"conflict exchange audit",
		"create queue order-refund",
		"create binding orders → order-refund (order.refund.*)",
		"delete binding orders → tmp",
		"delete queue tmp",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("plan:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if p.Unchanged != 3 { // orders exchange, order-mail queue (60000 == 60000), its binding
		t.Fatalf("unchanged %d", p.Unchanged)
	}
	if _, err := Apply(context.Background(), p, ApplyOptions{AllowDelete: true}); !errors.Is(err, ErrConflicts) {
		t.Fatalf("conflicts must block by default: %v", err)
	}
	res, err := Apply(context.Background(), p, ApplyOptions{AllowDelete: true, SkipConflicts: true})
	if err != nil || res[0].Status != "skipped" {
		t.Fatalf("%v %+v", err, res)
	}
	if strings.Join(r.calls, ",") != "q+ order-refund quorum,b+ orders>order-refund,b- ~,q- tmp" {
		t.Fatalf("calls %v", r.calls)
	}
}

func TestExportRoundTrip(t *testing.T) {
	r := &fakeRabbit{
		xs: []rabbitmq.Exchange{{Name: ""}, {Name: "amq.topic"}, {Name: "orders", Type: "topic", Durable: true}},
		qs: []rabbitmq.Queue{{Name: "q1", Type: "quorum", Durable: true, Arguments: map[string]any{"x-queue-type": "quorum"}}, {Name: "amq.gen-x", Exclusive: true}},
		bs: []rabbitmq.Binding{{Source: "", Destination: "q1", DestinationType: "queue"}, {Source: "orders", Destination: "q1", DestinationType: "queue", RoutingKey: "#"}},
	}
	s, err := ExportRabbit(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	y, err := Encode(s, "exported")
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(y)
	if err != nil {
		t.Fatalf("%v\n%s", err, y)
	}
	p, _ := PlanRabbit(context.Background(), back, r)
	if len(p.Changes) != 0 {
		t.Fatalf("export should plan clean against its source:\n%s\n%s", y, Format(p))
	}
}

func hasDiff(c Change, field, to string) bool {
	for _, d := range c.Diffs {
		if d.Field == field && d.To == to {
			return true
		}
	}
	return false
}
