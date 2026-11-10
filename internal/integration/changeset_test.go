//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/changeset"
	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/diag"
	"github.com/YunMuZhe/MidDeck/internal/driver/kafka"
	"github.com/YunMuZhe/MidDeck/internal/driver/rabbitmq"
)

func openKafka(t *testing.T, ctx context.Context) *kafka.Session {
	t.Helper()
	addr := env(t, "MIDDECK_KAFKA")
	rep := &diag.Report{}
	s, err := kafka.Open(ctx, config.Instance{Kind: config.KindKafka, Addrs: []string{addr}}, dialer(), rep)
	if err != nil {
		checkReport(t, rep)
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestKafkaChangeset(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s := openKafka(t, ctx)
	a := fmt.Sprintf("middeck-cs-%d-a", time.Now().UnixNano())
	b := strings.TrimSuffix(a, "-a") + "-b"
	defer s.DeleteTopic(ctx, a)
	defer s.DeleteTopic(ctx, b)
	if err := s.CreateTopic(ctx, kafka.CreateTopicReq{Name: b, Partitions: 1, Replication: 1}); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ { // metadata propagates asynchronously
		if _, err := s.DescribeTopic(ctx, b); err == nil {
			break
		} else if i == 50 {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	yaml := fmt.Sprintf(`
kafka:
  defaults: {replication: 3}
  topics:
    - {name: %s, partitions: 3, configs: {retention.ms: "3600000", cleanup.policy: delete}}
    - {name: %s, partitions: 2, configs: {retention.ms: "7200000"}}
envs:
  dev:
    kafka: {defaults: {replication: 1}}
`, a, b)
	spec, err := changeset.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	p, err := changeset.PlanKafka(ctx, spec, "dev", s)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + changeset.Format(p))
	if p.Count(changeset.Create) != 1 || p.Count(changeset.Update) != 1 {
		t.Fatalf("plan: %+v", p.Changes)
	}
	res, err := changeset.Apply(ctx, p, changeset.ApplyOptions{Fingerprint: p.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Status != "applied" {
			t.Fatalf("%+v", r)
		}
	}
	p2, err := changeset.PlanKafka(ctx, spec, "dev", s)
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Changes) != 0 {
		t.Fatalf("not converged: %v\n%s", err, changeset.Format(p2))
	}
	// Without the dev override the single broker cannot meet replication 3:
	// that is a warning on existing topics, not a change.
	p3, _ := changeset.PlanKafka(ctx, spec, "prod", s)
	if len(p3.Changes) != 0 || len(p3.Warnings) != 2 {
		t.Fatalf("prod plan: %s", changeset.Format(p3))
	}
	exp, err := changeset.ExportKafka(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range exp.Kafka.Topics {
		if x.Name == a {
			found = x.Partitions == 3 && x.Configs["retention.ms"] == "3600000"
		}
	}
	if !found {
		t.Fatal("export misses the topic or its configs")
	}
}

func TestKafkaSkipReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s := openKafka(t, ctx)
	topic := fmt.Sprintf("middeck-skip-%d", time.Now().UnixNano())
	group := topic + "-g"
	if err := s.CreateTopic(ctx, kafka.CreateTopicReq{Name: topic, Partitions: 2, Replication: 1}); err != nil {
		t.Fatal(err)
	}
	defer s.DeleteTopic(ctx, topic)
	bin := string([]byte{0xff, 0x00, 0x01})
	for i := 0; i < 6; i++ {
		v := fmt.Sprintf(`{"n":%d}`, i)
		if i == 1 {
			v = bin
		}
		if _, err := s.Produce(ctx, kafka.ProduceReq{Topic: topic, Partition: int32(i % 2), Key: fmt.Sprint(i), Value: v,
			Headers: []kafka.Header{{Key: "trace", Value: fmt.Sprint(i)}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ResetOffsets(ctx, kafka.ResetReq{Group: group, Topic: topic, Mode: "earliest"}); err != nil {
		t.Fatal(err)
	}
	ranges, _, err := s.SkipRanges(ctx, kafka.SkipReq{Group: group, Topic: topic, Counts: map[int32]int64{0: 1, 1: 5}})
	if err != nil {
		t.Fatal(err)
	}
	// Partition 1 holds 3 messages, so "skip 5" is capped at the end.
	if len(ranges) != 2 || ranges[0] != (kafka.Range{Partition: 0, From: 0, To: 1}) || ranges[1] != (kafka.Range{Partition: 1, From: 0, To: 3}) {
		t.Fatalf("ranges %+v", ranges)
	}
	recs, err := s.ReadRanges(ctx, topic, ranges)
	if err != nil || len(recs) != 4 {
		t.Fatalf("read %d %v", len(recs), err)
	}
	if string(recs[1].Value) != bin || string(recs[1].Headers[0].Value) != "1" {
		t.Fatalf("binary record changed: %+v", recs[1])
	}
	if err := s.CommitTo(ctx, group, topic, ranges); err != nil {
		t.Fatal(err)
	}
	gd, _ := s.DescribeGroup(ctx, group)
	if gd.Lag != 2 {
		t.Fatalf("lag after skip %d, want 2", gd.Lag)
	}
	n, err := s.ProduceRecords(ctx, topic, recs, true, []kafka.RecordHeader{{Key: "x-middeck-replay", Value: kafka.Bytes("b1")}})
	if err != nil || n != 4 {
		t.Fatalf("replay %d %v", n, err)
	}
	msgs, err := s.Fetch(ctx, kafka.FetchReq{Topic: topic, Partition: 1, Mode: "offset", Offset: 3, Limit: 10})
	if err != nil || len(msgs) != 3 {
		t.Fatalf("replayed on partition 1: %d %v", len(msgs), err)
	}
	for _, m := range msgs {
		ok := false
		for _, h := range m.Headers {
			ok = ok || h.Key == "x-middeck-replay" && h.Value == "b1"
		}
		if !ok || m.Partition != 1 {
			t.Fatalf("replayed message %+v", m)
		}
	}
}

func TestRabbitChangeset(t *testing.T) {
	mgmt := env(t, "MIDDECK_RABBIT_MGMT")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	rep := &diag.Report{}
	s, err := rabbitmq.Open(ctx, config.Instance{Kind: config.KindRabbitMQ, MgmtURL: mgmt, Auth: config.Auth{Username: "guest", Password: "guest"}}, dialer(), rep)
	if err != nil {
		checkReport(t, rep)
		t.Fatal(err)
	}
	defer s.Close()
	n := fmt.Sprintf("middeck-cs-%d", time.Now().UnixNano())
	defer s.DeleteQueue(ctx, n+"-q")
	defer s.DeleteQueue(ctx, n+"-q2")
	defer s.DeleteExchange(ctx, n+"-x")
	yaml := fmt.Sprintf(`
rabbitmq:
  exchanges: [{name: %[1]s-x, type: topic}]
  queues:
    - {name: %[1]s-q, type: quorum, arguments: {x-delivery-limit: 5}}
    - {name: %[1]s-q2, arguments: {x-message-ttl: 60000}}
  bindings:
    - {exchange: %[1]s-x, queue: %[1]s-q, routingKey: "a.*"}
    - {exchange: %[1]s-x, queue: %[1]s-q2, routingKey: "#"}
`, n)
	spec, err := changeset.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	p, err := changeset.PlanRabbit(ctx, spec, s)
	if err != nil || len(p.Changes) != 5 {
		t.Fatalf("%v\n%s", err, changeset.Format(p))
	}
	if _, err := changeset.Apply(ctx, p, changeset.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	p2, err := changeset.PlanRabbit(ctx, spec, s)
	if err != nil || len(p2.Changes) != 0 {
		t.Fatalf("not converged: %v\n%s", err, changeset.Format(p2))
	}
	// Removing a binding.
	spec.RabbitMQ.Bindings[1].State = "absent"
	p3, _ := changeset.PlanRabbit(ctx, spec, s)
	if p3.Count(changeset.Delete) != 1 {
		t.Fatalf("%s", changeset.Format(p3))
	}
	if _, err := changeset.Apply(ctx, p3, changeset.ApplyOptions{AllowDelete: true}); err != nil {
		t.Fatal(err)
	}
	if p4, _ := changeset.PlanRabbit(ctx, spec, s); len(p4.Changes) != 0 {
		t.Fatalf("binding not deleted: %s", changeset.Format(p4))
	}
}
