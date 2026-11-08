package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/driver/kafka"
	"github.com/YunMuZhe/MidDeck/internal/driver/rabbitmq"
	"github.com/YunMuZhe/MidDeck/internal/driver/redis"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ===== Kafka =====

func (a *App) KafkaTopics(id string) ([]kafka.Topic, error) {
	s, _, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return s.ListTopics(ctx)
}

func (a *App) KafkaDescribeTopic(id, topic string) (kafka.TopicDetail, error) {
	s, _, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return kafka.TopicDetail{}, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return s.DescribeTopic(ctx, topic)
}

func (a *App) KafkaCreateTopic(id string, r kafka.CreateTopicReq) error {
	s, c, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return a.write(c, "kafka.createTopic", r, func() error { return s.CreateTopic(ctx, r) })
}

func (a *App) KafkaDeleteTopic(id, topic string) error {
	s, c, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return a.write(c, "kafka.deleteTopic", topic, func() error { return s.DeleteTopic(ctx, topic) })
}

func (a *App) KafkaSetPartitions(id, topic string, total int) error {
	s, c, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return a.write(c, "kafka.setPartitions", map[string]any{"topic": topic, "total": total},
		func() error { return s.SetPartitions(ctx, topic, total) })
}

func (a *App) KafkaAlterTopicConfigs(id, topic string, set map[string]string) error {
	s, c, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return a.write(c, "kafka.alterTopicConfigs", map[string]any{"topic": topic, "set": set},
		func() error { return s.AlterTopicConfigs(ctx, topic, set) })
}

func (a *App) KafkaFetch(id string, r kafka.FetchReq) ([]kafka.Message, error) {
	s, _, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return s.Fetch(ctx, r)
}

func (a *App) KafkaProduce(id string, r kafka.ProduceReq) (kafka.Message, error) {
	s, c, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return kafka.Message{}, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	var m kafka.Message
	err = a.write(c, "kafka.produce", map[string]any{"topic": r.Topic, "partition": r.Partition, "key": r.Key},
		func() error { var e error; m, e = s.Produce(ctx, r); return e })
	return m, err
}

func (a *App) KafkaGroups(id string) ([]kafka.GroupSummary, error) {
	s, _, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return s.ListGroups(ctx)
}

func (a *App) KafkaDescribeGroup(id, group string) (kafka.GroupDetail, error) {
	s, _, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return kafka.GroupDetail{}, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return s.DescribeGroup(ctx, group)
}

func (a *App) KafkaResetOffsets(id string, r kafka.ResetReq) ([]kafka.OffsetChange, error) {
	s, c, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	if r.DryRun {
		return s.ResetOffsets(ctx, r)
	}
	var out []kafka.OffsetChange
	err = a.write(c, "kafka.resetOffsets", r, func() error { var e error; out, e = s.ResetOffsets(ctx, r); return e })
	return out, err
}

func (a *App) KafkaDeleteGroup(id, group string) error {
	s, c, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return a.write(c, "kafka.deleteGroup", group, func() error { return s.DeleteGroup(ctx, group) })
}

// ExportMessages saves fetched messages as JSON Lines.
func (a *App) ExportMessages(name string, msgs []json.RawMessage) (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title: "导出消息", DefaultFilename: fmt.Sprintf("%s-%s.jsonl", name, time.Now().Format("20060102-150405")),
	})
	if err != nil || path == "" {
		return "", err
	}
	var b strings.Builder
	for _, m := range msgs {
		b.Write(m)
		b.WriteByte('\n')
	}
	return path, os.WriteFile(path, []byte(b.String()), 0o600)
}

// ===== Redis =====

func (a *App) RedisOverview(id string) (map[string]string, error) {
	s, _, err := sessionAs[*redis.Session](a, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return s.Overview(ctx)
}

func (a *App) RedisScan(id, pattern string, cursor uint64, count int64) (redis.ScanResult, error) {
	s, _, err := sessionAs[*redis.Session](a, id)
	if err != nil {
		return redis.ScanResult{}, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return s.Scan(ctx, pattern, cursor, count)
}

func (a *App) RedisGet(id, key string) (redis.KeyValue, error) {
	s, _, err := sessionAs[*redis.Session](a, id)
	if err != nil {
		return redis.KeyValue{}, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return s.GetKey(ctx, key)
}

func (a *App) RedisSetString(id, key, value string, ttl int64) error {
	s, c, err := sessionAs[*redis.Session](a, id)
	if err != nil {
		return err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return a.write(c, "redis.set", map[string]any{"key": key, "ttl": ttl}, func() error { return s.SetString(ctx, key, value, ttl) })
}

func (a *App) RedisSetTTL(id, key string, ttl int64) error {
	s, c, err := sessionAs[*redis.Session](a, id)
	if err != nil {
		return err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return a.write(c, "redis.ttl", map[string]any{"key": key, "ttl": ttl}, func() error { return s.SetTTL(ctx, key, ttl) })
}

func (a *App) RedisDelete(id string, keys []string) (int64, error) {
	s, c, err := sessionAs[*redis.Session](a, id)
	if err != nil {
		return 0, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	var n int64
	err = a.write(c, "redis.delete", keys, func() error { var e error; n, e = s.DeleteKeys(ctx, keys); return e })
	return n, err
}

// RedisExec runs a console command. Dangerous commands require confirmed=true;
// non-read commands are refused on read-only instances and written to the audit log.
func (a *App) RedisExec(id, line string, confirmed bool) (string, error) {
	s, c, err := sessionAs[*redis.Session](a, id)
	if err != nil {
		return "", err
	}
	if redis.IsDangerous(line) && !confirmed {
		return "", fmt.Errorf("NEED_CONFIRM: 该命令可能修改数据或阻塞服务，请确认后执行")
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	if redis.IsReadCmd(line) {
		return s.Exec(ctx, line)
	}
	var out string
	err = a.write(c, "redis.exec", line, func() error { var e error; out, e = s.Exec(ctx, line); return e })
	return out, err
}

func (a *App) RedisSlowLog(id string, n int64) ([]redis.SlowLog, error) {
	s, _, err := sessionAs[*redis.Session](a, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return s.SlowLog(ctx, n)
}

// ===== RabbitMQ =====

func (a *App) RabbitQueues(id string) ([]rabbitmq.Queue, error) {
	s, _, err := sessionAs[*rabbitmq.Session](a, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return s.ListQueues(ctx)
}

func (a *App) RabbitExchanges(id string) ([]rabbitmq.Exchange, error) {
	s, _, err := sessionAs[*rabbitmq.Session](a, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return s.ListExchanges(ctx)
}

func (a *App) RabbitQueueBindings(id, queue string) ([]rabbitmq.Binding, error) {
	s, _, err := sessionAs[*rabbitmq.Session](a, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return s.QueueBindings(ctx, queue)
}

func (a *App) RabbitCreateQueue(id string, r rabbitmq.CreateQueueReq) error {
	s, c, err := sessionAs[*rabbitmq.Session](a, id)
	if err != nil {
		return err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return a.write(c, "rabbitmq.createQueue", r, func() error { return s.CreateQueue(ctx, r) })
}

func (a *App) RabbitCreateExchange(id string, r rabbitmq.CreateExchangeReq) error {
	s, c, err := sessionAs[*rabbitmq.Session](a, id)
	if err != nil {
		return err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return a.write(c, "rabbitmq.createExchange", r, func() error { return s.CreateExchange(ctx, r) })
}

func (a *App) RabbitDeleteQueue(id, queue string) error {
	s, c, err := sessionAs[*rabbitmq.Session](a, id)
	if err != nil {
		return err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return a.write(c, "rabbitmq.deleteQueue", queue, func() error { return s.DeleteQueue(ctx, queue) })
}

func (a *App) RabbitPurgeQueue(id, queue string) error {
	s, c, err := sessionAs[*rabbitmq.Session](a, id)
	if err != nil {
		return err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return a.write(c, "rabbitmq.purgeQueue", queue, func() error { return s.PurgeQueue(ctx, queue) })
}

func (a *App) RabbitPeek(id, queue string, n int) ([]rabbitmq.PeekedMessage, error) {
	s, _, err := sessionAs[*rabbitmq.Session](a, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return s.Peek(ctx, queue, n)
}

func (a *App) RabbitPublish(id string, r rabbitmq.PublishReq) (bool, error) {
	s, c, err := sessionAs[*rabbitmq.Session](a, id)
	if err != nil {
		return false, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	var routed bool
	err = a.write(c, "rabbitmq.publish", map[string]any{"exchange": r.Exchange, "routingKey": r.RoutingKey},
		func() error { var e error; routed, e = s.Publish(ctx, r); return e })
	return routed, err
}

// RabbitExportDefinitions saves the vhost definitions to a file.
func (a *App) RabbitExportDefinitions(id string) (string, error) {
	s, c, err := sessionAs[*rabbitmq.Session](a, id)
	if err != nil {
		return "", err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	raw, err := s.Definitions(ctx)
	if err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title: "导出 definitions", DefaultFilename: fmt.Sprintf("%s-definitions.json", c.inst.Name),
	})
	if err != nil || path == "" {
		return "", err
	}
	return path, os.WriteFile(path, raw, 0o600)
}
