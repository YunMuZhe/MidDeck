// Package changeset describes Kafka topics and RabbitMQ exchanges, queues and
// bindings as YAML, diffs that against a live instance (plan) and applies the
// difference (apply). The same file can be promoted from one environment to the
// next; per-environment overrides cover things like replication on a one-broker
// dev cluster.
//
// The model is additive: things the file does not mention are left alone. Removal
// is explicit (state: absent) and needs an extra opt-in to apply.
package changeset

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Spec is the YAML document.
type Spec struct {
	Version  int                    `yaml:"version"`
	Kafka    *KafkaSpec             `yaml:"kafka,omitempty"`
	RabbitMQ *RabbitSpec            `yaml:"rabbitmq,omitempty"`
	Envs     map[string]EnvOverride `yaml:"envs,omitempty"`
}

// KafkaSpec lists topics; Defaults fill in fields a topic leaves out.
type KafkaSpec struct {
	Defaults TopicSettings `yaml:"defaults,omitempty"`
	Topics   []TopicSpec   `yaml:"topics"`
}

// TopicSettings are the tunable parts of a topic.
type TopicSettings struct {
	Partitions  int32             `yaml:"partitions,omitempty"`
	Replication int16             `yaml:"replication,omitempty"`
	Configs     map[string]string `yaml:"configs,omitempty"`
}

// TopicSpec is one topic.
type TopicSpec struct {
	Name          string `yaml:"name"`
	TopicSettings `yaml:",inline"`
	State         string `yaml:"state,omitempty"` // present (default) | absent
}

// RabbitSpec lists exchanges, queues and bindings in the instance's vhost.
type RabbitSpec struct {
	Exchanges []ExchangeSpec `yaml:"exchanges,omitempty"`
	Queues    []QueueSpec    `yaml:"queues,omitempty"`
	Bindings  []BindingSpec  `yaml:"bindings,omitempty"`
}

// ExchangeSpec is one exchange. Durable defaults to true.
type ExchangeSpec struct {
	Name       string         `yaml:"name"`
	Type       string         `yaml:"type,omitempty"` // direct (default) | topic | fanout | headers | x-…
	Durable    *bool          `yaml:"durable,omitempty"`
	AutoDelete bool           `yaml:"autoDelete,omitempty"`
	Internal   bool           `yaml:"internal,omitempty"`
	Arguments  map[string]any `yaml:"arguments,omitempty"`
	State      string         `yaml:"state,omitempty"`
}

// QueueSpec is one queue. Durable defaults to true.
type QueueSpec struct {
	Name       string         `yaml:"name"`
	Type       string         `yaml:"type,omitempty"` // classic (default) | quorum | stream
	Durable    *bool          `yaml:"durable,omitempty"`
	AutoDelete bool           `yaml:"autoDelete,omitempty"`
	Arguments  map[string]any `yaml:"arguments,omitempty"`
	State      string         `yaml:"state,omitempty"`
}

// BindingSpec binds an exchange to a queue (or to another exchange).
type BindingSpec struct {
	Exchange   string         `yaml:"exchange"`
	Queue      string         `yaml:"queue,omitempty"`
	ToExchange string         `yaml:"toExchange,omitempty"`
	RoutingKey string         `yaml:"routingKey,omitempty"`
	Arguments  map[string]any `yaml:"arguments,omitempty"`
	State      string         `yaml:"state,omitempty"`
}

// EnvOverride adjusts the spec for one environment (matched against the
// instance's environment, or --env). Precedence, lowest first:
// kafka.defaults < topic < envs.<env>.kafka.defaults < envs.<env>.kafka.topics.<name>.
type EnvOverride struct {
	Kafka *struct {
		Defaults TopicSettings            `yaml:"defaults,omitempty"`
		Topics   map[string]TopicSettings `yaml:"topics,omitempty"`
	} `yaml:"kafka,omitempty"`
}

// Parse reads and validates a YAML changeset.
func Parse(data []byte) (*Spec, error) {
	var s Spec
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the changeset is empty")
		}
		return nil, fmt.Errorf("invalid changeset: %w", err)
	}
	if s.Version == 0 {
		s.Version = 1
	}
	if s.Version != 1 {
		return nil, fmt.Errorf("unsupported changeset version %d", s.Version)
	}
	return &s, s.validate()
}

func (s *Spec) validate() error {
	var errs []string
	bad := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	state := func(where, st string) {
		if st != "" && st != "present" && st != "absent" {
			bad("%s: state must be present or absent, got %q", where, st)
		}
	}
	if s.Kafka != nil {
		seen := map[string]bool{}
		for i, t := range s.Kafka.Topics {
			where := fmt.Sprintf("kafka.topics[%d]", i)
			if t.Name == "" {
				bad("%s: name is required", where)
				continue
			}
			where = "kafka topic " + t.Name
			if seen[t.Name] {
				bad("%s: listed twice", where)
			}
			seen[t.Name] = true
			if t.Partitions < 0 || t.Replication < 0 {
				bad("%s: partitions and replication must be positive", where)
			}
			state(where, t.State)
		}
	}
	if s.RabbitMQ != nil {
		r := s.RabbitMQ
		seen := map[string]bool{}
		for i, x := range r.Exchanges {
			if x.Name == "" {
				bad("rabbitmq.exchanges[%d]: name is required", i)
				continue
			}
			if strings.HasPrefix(x.Name, "amq.") {
				bad("exchange %s: amq.* exchanges are built in", x.Name)
			}
			if seen["e/"+x.Name] {
				bad("exchange %s: listed twice", x.Name)
			}
			seen["e/"+x.Name] = true
			state("exchange "+x.Name, x.State)
		}
		for i, q := range r.Queues {
			if q.Name == "" {
				bad("rabbitmq.queues[%d]: name is required", i)
				continue
			}
			if seen["q/"+q.Name] {
				bad("queue %s: listed twice", q.Name)
			}
			seen["q/"+q.Name] = true
			switch q.Type {
			case "", "classic", "quorum", "stream":
			default:
				bad("queue %s: type must be classic, quorum or stream", q.Name)
			}
			state("queue "+q.Name, q.State)
		}
		for i, b := range r.Bindings {
			where := fmt.Sprintf("rabbitmq.bindings[%d]", i)
			if b.Exchange == "" {
				bad("%s: exchange is required", where)
			}
			if (b.Queue == "") == (b.ToExchange == "") {
				bad("%s: set exactly one of queue or toExchange", where)
			}
			state(where, b.State)
		}
	}
	if s.Kafka == nil && s.RabbitMQ == nil {
		bad("nothing to do: add a kafka or rabbitmq section")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}

// effectiveTopics applies defaults and the environment's overrides.
func (s *Spec) effectiveTopics(env string) []TopicSpec {
	if s.Kafka == nil {
		return nil
	}
	ov := s.Envs[env].Kafka
	out := make([]TopicSpec, 0, len(s.Kafka.Topics))
	for _, t := range s.Kafka.Topics {
		e := TopicSpec{Name: t.Name, State: t.State}
		layers := []TopicSettings{s.Kafka.Defaults, t.TopicSettings}
		if ov != nil {
			layers = append(layers, ov.Defaults, ov.Topics[t.Name])
		}
		for _, l := range layers {
			if l.Partitions > 0 {
				e.Partitions = l.Partitions
			}
			if l.Replication > 0 {
				e.Replication = l.Replication
			}
			for k, v := range l.Configs {
				if e.Configs == nil {
					e.Configs = map[string]string{}
				}
				e.Configs[k] = v
			}
		}
		out = append(out, e)
	}
	return out
}

// Encode renders a spec as YAML with a short header.
func Encode(s *Spec, header string) ([]byte, error) {
	var b bytes.Buffer
	for _, l := range strings.Split(strings.TrimSpace(header), "\n") {
		if l != "" {
			b.WriteString("# " + l + "\n")
		}
	}
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return b.Bytes(), enc.Close()
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}
