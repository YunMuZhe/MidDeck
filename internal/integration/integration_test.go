//go:build integration

// Package integration runs the drivers against real servers.
//
//	MIDDECK_KAFKA=localhost:9092 MIDDECK_REDIS=localhost:6379 \
//	MIDDECK_RABBIT_MGMT=http://localhost:15672 MIDDECK_RABBIT_AMQP=localhost:5672 \
//	go test -tags integration ./internal/integration/ -v
//
// MIDDECK_EXPECT_<KAFKA|REDIS|RABBIT>=major.minor additionally asserts the detected
// version, which keeps the "tested" lines in internal/compat honest.
package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/compat"
	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/diag"
	"github.com/YunMuZhe/MidDeck/internal/driver"
	"github.com/YunMuZhe/MidDeck/internal/driver/kafka"
	"github.com/YunMuZhe/MidDeck/internal/driver/rabbitmq"
	"github.com/YunMuZhe/MidDeck/internal/driver/redis"
	"github.com/YunMuZhe/MidDeck/internal/tunnel"
)

func env(t *testing.T, k string) string {
	v := os.Getenv(k)
	if v == "" {
		t.Skipf("%s not set", k)
	}
	return v
}

func dialer() driver.DialFunc {
	return tunnel.New(config.Network{}, "").DialContext
}

func checkReport(t *testing.T, rep *diag.Report) {
	t.Helper()
	for _, s := range rep.Finish().Steps {
		t.Logf("[%s] %s %s %s", s.Status, s.Name, s.Detail, s.Hint)
	}
	if !rep.OK {
		t.Fatal("diagnostics failed")
	}
}

func checkVersion(t *testing.T, kind, envKey string, info driver.ServerInfo) {
	t.Helper()
	t.Logf("%s version %s (raw %v): %s", kind, info.Version, info.Extra, info.Compat.Message)
	if info.Version == "" {
		t.Fatal("version not detected")
	}
	if want := os.Getenv(envKey); want != "" {
		if info.Version != want && !strings.HasPrefix(info.Version, want+".") {
			t.Fatalf("detected %s (raw %v), want %s", info.Version, info.Extra, want)
		}
		if info.Compat.Level != compat.Tested {
			t.Fatalf("%s %s is exercised in CI but not listed as tested in compat.Matrices", kind, want)
		}
	}
}

func TestKafka(t *testing.T) {
	addr := env(t, "MIDDECK_KAFKA")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rep := &diag.Report{}
	s, err := kafka.Open(ctx, config.Instance{Kind: config.KindKafka, Addrs: []string{addr}}, dialer(), rep)
	checkReport(t, rep)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	info, err := s.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checkVersion(t, "kafka", "MIDDECK_EXPECT_KAFKA", info)

	topic := fmt.Sprintf("middeck-it-%d", time.Now().UnixNano())
	if err := s.CreateTopic(ctx, kafka.CreateTopicReq{Name: topic, Partitions: 2, Replication: 1, Configs: map[string]string{"retention.ms": "3600000"}}); err != nil {
		t.Fatal(err)
	}
	defer s.DeleteTopic(ctx, topic)

	for i := 0; i < 5; i++ {
		if _, err := s.Produce(ctx, kafka.ProduceReq{Topic: topic, Partition: -1, Key: fmt.Sprint(i), Value: fmt.Sprintf(`{"n":%d}`, i),
			Headers: []kafka.Header{{Key: "h", Value: "v"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Produce(ctx, kafka.ProduceReq{Topic: topic, Partition: 1, Value: "manual"}); err != nil {
		t.Fatal(err)
	}
	msgs, err := s.Fetch(ctx, kafka.FetchReq{Topic: topic, Partition: -1, Mode: "oldest", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 6 {
		t.Fatalf("fetched %d messages, want 6", len(msgs))
	}
	filtered, err := s.Fetch(ctx, kafka.FetchReq{Topic: topic, Partition: -1, Mode: "newest", Limit: 100, Contains: `"n":3`})
	if err != nil || len(filtered) != 1 {
		t.Fatalf("filter: %v %d", err, len(filtered))
	}

	d, err := s.DescribeTopic(ctx, topic)
	if err != nil || d.Partitions != 2 || d.Messages != 6 {
		t.Fatalf("describe: %+v %v", d, err)
	}
	if err := s.SetPartitions(ctx, topic, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.AlterTopicConfigs(ctx, topic, map[string]string{"retention.ms": "7200000"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListGroups(ctx); err != nil {
		t.Fatal(err)
	}

	// Offset reset on an inactive group: preview, then commit, then verify.
	group := topic + "-g"
	preview, err := s.ResetOffsets(ctx, kafka.ResetReq{Group: group, Topic: topic, Mode: "latest", DryRun: true})
	if err != nil || len(preview) != 3 {
		t.Fatalf("dry run: %+v %v", preview, err)
	}
	if _, err := s.ResetOffsets(ctx, kafka.ResetReq{Group: group, Topic: topic, Mode: "latest"}); err != nil {
		t.Fatal(err)
	}
	gd, err := s.DescribeGroup(ctx, group)
	if err != nil || gd.Lag != 0 {
		t.Fatalf("after reset to latest: %+v %v", gd, err)
	}
	if _, err := s.ResetOffsets(ctx, kafka.ResetReq{Group: group, Topic: topic, Mode: "earliest"}); err != nil {
		t.Fatal(err)
	}
	if gd, err = s.DescribeGroup(ctx, group); err != nil || gd.Lag != 6 {
		t.Fatalf("after reset to earliest: lag=%d %v", gd.Lag, err)
	}
	if err := s.DeleteGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
}

func TestRedis(t *testing.T) {
	addr := env(t, "MIDDECK_REDIS")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	rep := &diag.Report{}
	s, err := redis.Open(ctx, config.Instance{Kind: config.KindRedis, Addrs: []string{addr}}, dialer(), rep)
	checkReport(t, rep)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	info, err := s.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checkVersion(t, "redis", "MIDDECK_EXPECT_REDIS", info)

	prefix := fmt.Sprintf("middeck:it:%d:", time.Now().UnixNano())
	if err := s.SetString(ctx, prefix+"s", `{"a":1}`, 60); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{
		"HSET " + prefix + "h f1 v1 f2 v2",
		"RPUSH " + prefix + "l a b c",
		"ZADD " + prefix + "z 1 a 2 b",
		`SET "` + prefix + `quoted key" "hello world"`,
	} {
		if _, err := s.Exec(ctx, cmd); err != nil {
			t.Fatalf("%s: %v", cmd, err)
		}
	}
	res, err := s.Scan(ctx, prefix+"*", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	keys := res.Keys
	for !res.Done {
		if res, err = s.Scan(ctx, prefix+"*", res.Cursor, 100); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, res.Keys...)
	}
	if len(keys) != 5 {
		t.Fatalf("scan found %d keys, want 5: %+v", len(keys), keys)
	}
	v, err := s.GetKey(ctx, prefix+"h")
	if err != nil || v.Type != "hash" || len(v.Items) != 2 {
		t.Fatalf("hash: %+v %v", v, err)
	}
	v, err = s.GetKey(ctx, prefix+"quoted key")
	if err != nil || v.String != "hello world" {
		t.Fatalf("quoted: %+v %v", v, err)
	}
	var all []string
	for _, k := range keys {
		all = append(all, k.Key)
	}
	if n, err := s.DeleteKeys(ctx, all); err != nil || n != 5 {
		t.Fatalf("delete: %d %v", n, err)
	}
	if _, err := s.SlowLog(ctx, 10); err != nil {
		t.Fatal(err)
	}
}

func TestRabbitMQ(t *testing.T) {
	mgmt := env(t, "MIDDECK_RABBIT_MGMT")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	inst := config.Instance{
		Kind: config.KindRabbitMQ, MgmtURL: mgmt, VHost: "/",
		Auth: config.Auth{Username: "guest", Password: "guest"},
	}
	if a := os.Getenv("MIDDECK_RABBIT_AMQP"); a != "" {
		inst.Addrs = []string{a}
	}
	rep := &diag.Report{}
	s, err := rabbitmq.Open(ctx, inst, dialer(), rep)
	checkReport(t, rep)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	info, err := s.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checkVersion(t, "rabbitmq", "MIDDECK_EXPECT_RABBIT", info)

	q := fmt.Sprintf("middeck-it-%d", time.Now().UnixNano())
	if err := s.CreateExchange(ctx, rabbitmq.CreateExchangeReq{Name: q + "-x", Type: "topic", Durable: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateQueue(ctx, rabbitmq.CreateQueueReq{Name: q, Type: "quorum", BindExchange: q + "-x", RoutingKey: "order.#"}); err != nil {
		t.Fatal(err)
	}
	defer s.DeleteQueue(ctx, q)
	defer s.DeleteExchange(ctx, q+"-x")
	routed, err := s.Publish(ctx, rabbitmq.PublishReq{Exchange: q + "-x", RoutingKey: "order.created", Payload: `{"id":1}`, Persistent: true})
	if err != nil || !routed {
		t.Fatalf("publish routed=%v err=%v", routed, err)
	}
	var msgs []rabbitmq.PeekedMessage
	for i := 0; i < 20 && len(msgs) == 0; i++ {
		if msgs, err = s.Peek(ctx, q, 10); err != nil {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if len(msgs) != 1 || msgs[0].Payload != `{"id":1}` {
		t.Fatalf("peek: %+v", msgs)
	}
	if err := s.PurgeQueue(ctx, q); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Definitions(ctx); err != nil {
		t.Fatal(err)
	}
}
