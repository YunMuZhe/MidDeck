package changeset

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/driver/kafka"
	"github.com/YunMuZhe/MidDeck/internal/i18n"
)

// Kafka is the part of a Kafka session the changeset needs.
type Kafka interface {
	ListTopics(ctx context.Context) ([]kafka.Topic, error)
	DescribeTopic(ctx context.Context, name string) (kafka.TopicDetail, error)
	CreateTopic(ctx context.Context, r kafka.CreateTopicReq) error
	SetPartitions(ctx context.Context, name string, total int) error
	AlterTopicConfigs(ctx context.Context, name string, set map[string]string) error
	DeleteTopic(ctx context.Context, name string) error
}

// PlanKafka diffs the spec's topics against the cluster.
func PlanKafka(ctx context.Context, s *Spec, env string, k Kafka) (*Plan, error) {
	if s.Kafka == nil || len(s.Kafka.Topics) == 0 {
		return nil, fmt.Errorf("the changeset has no kafka topics")
	}
	p := &Plan{Kind: "kafka", Env: env}
	list, err := k.ListTopics(ctx)
	if err != nil {
		return nil, err
	}
	listed := map[string]bool{}
	for _, t := range list {
		listed[t.Name] = true
	}
	// The topic list can lag a moment behind a create or partition change, while a
	// targeted describe is current; confirm before planning a create or a no-op delete.
	exists := func(name string) bool {
		if listed[name] {
			return true
		}
		_, err := k.DescribeTopic(ctx, name)
		return err == nil
	}
	var deletes []Change
	for _, t := range s.effectiveTopics(env) {
		t := t
		if t.State == "absent" {
			if exists(t.Name) {
				deletes = append(deletes, Change{Resource: "topic", Name: t.Name, Action: Delete,
					apply: func(ctx context.Context) error { return k.DeleteTopic(ctx, t.Name) }})
			} else {
				p.Unchanged++
			}
			continue
		}
		if !exists(t.Name) {
			p.Changes = append(p.Changes, createTopic(k, t))
			continue
		}
		d, err := describe(ctx, k, t.Name)
		if err != nil {
			return nil, fmt.Errorf("describe %s: %w", t.Name, err)
		}
		c := Change{Resource: "topic", Name: t.Name, Action: Update}
		grow := 0
		if t.Partitions > 0 {
			switch have := int32(d.Partitions); {
			case t.Partitions > have:
				grow = int(t.Partitions)
				c.Diffs = append(c.Diffs, Diff{"partitions", itoa(have), itoa(t.Partitions)})
			case t.Partitions < have:
				p.Warnings = append(p.Warnings, i18n.T("cs.fewerPartitions", t.Name, have, t.Partitions))
			}
		}
		if t.Replication > 0 && int(t.Replication) != d.Replication {
			p.Warnings = append(p.Warnings, i18n.T("cs.replication", t.Name, d.Replication, t.Replication))
		}
		current := map[string]string{}
		for _, e := range d.Configs {
			current[e.Key] = e.Value
		}
		set := map[string]string{}
		for _, key := range sortedKeys(t.Configs) {
			if want := t.Configs[key]; current[key] != want {
				set[key] = want
				c.Diffs = append(c.Diffs, Diff{key, current[key], want})
			}
		}
		if len(c.Diffs) == 0 {
			p.Unchanged++
			continue
		}
		c.apply = func(ctx context.Context) error {
			if grow > 0 {
				if err := k.SetPartitions(ctx, t.Name, grow); err != nil {
					return err
				}
			}
			if len(set) > 0 {
				return k.AlterTopicConfigs(ctx, t.Name, set)
			}
			return nil
		}
		p.Changes = append(p.Changes, c)
	}
	p.Changes = append(p.Changes, deletes...)
	p.seal()
	return p, nil
}

// describe retries briefly while a just-created or just-grown topic settles.
func describe(ctx context.Context, k Kafka, name string) (kafka.TopicDetail, error) {
	for i := 0; ; i++ {
		d, err := k.DescribeTopic(ctx, name)
		if err == nil || i == 20 || !transient(err) {
			return d, err
		}
		select {
		case <-ctx.Done():
			return d, err
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func transient(err error) bool {
	s := err.Error()
	return strings.Contains(s, "UNKNOWN_TOPIC_OR_PARTITION") || strings.Contains(s, "LEADER_NOT_AVAILABLE") || strings.Contains(s, "NOT_LEADER")
}

func createTopic(k Kafka, t TopicSpec) Change {
	parts, repl := t.Partitions, t.Replication
	if parts == 0 {
		parts = -1 // broker default
	}
	if repl == 0 {
		repl = -1
	}
	c := Change{Resource: "topic", Name: t.Name, Action: Create}
	c.Diffs = append(c.Diffs, Diff{"partitions", "", orDefault(t.Partitions)}, Diff{"replication", "", orDefault(int32(t.Replication))})
	for _, key := range sortedKeys(t.Configs) {
		c.Diffs = append(c.Diffs, Diff{key, "", t.Configs[key]})
	}
	c.apply = func(ctx context.Context) error {
		return k.CreateTopic(ctx, kafka.CreateTopicReq{Name: t.Name, Partitions: parts, Replication: repl, Configs: t.Configs})
	}
	return c
}

// ExportKafka describes the cluster's user topics as a changeset. Only configs
// set on the topic itself are included; broker defaults are left out.
func ExportKafka(ctx context.Context, k Kafka) (*Spec, error) {
	list, err := k.ListTopics(ctx)
	if err != nil {
		return nil, err
	}
	ks := &KafkaSpec{Topics: []TopicSpec{}}
	for _, t := range list {
		if t.Internal || t.Name == "__consumer_offsets" || len(t.Name) > 0 && t.Name[0] == '_' {
			continue
		}
		d, err := describe(ctx, k, t.Name)
		if err != nil {
			return nil, fmt.Errorf("describe %s: %w", t.Name, err)
		}
		ts := TopicSpec{Name: t.Name, TopicSettings: TopicSettings{Partitions: int32(d.Partitions), Replication: int16(d.Replication)}}
		for _, e := range d.Configs {
			if e.Source == "DYNAMIC_TOPIC_CONFIG" && !e.Sensitive {
				if ts.Configs == nil {
					ts.Configs = map[string]string{}
				}
				ts.Configs[e.Key] = e.Value
			}
		}
		ks.Topics = append(ks.Topics, ts)
	}
	return &Spec{Version: 1, Kafka: ks}, nil
}

func itoa[T ~int32 | ~int16 | ~int](n T) string { return strconv.Itoa(int(n)) }

func orDefault(n int32) string {
	if n <= 0 {
		return "(broker default)"
	}
	return itoa(n)
}
