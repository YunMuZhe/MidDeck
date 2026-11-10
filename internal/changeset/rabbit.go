package changeset

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/YunMuZhe/MidDeck/internal/driver/rabbitmq"
	"github.com/YunMuZhe/MidDeck/internal/i18n"
)

// Rabbit is the part of a RabbitMQ session the changeset needs.
type Rabbit interface {
	ListExchanges(ctx context.Context) ([]rabbitmq.Exchange, error)
	ListQueues(ctx context.Context) ([]rabbitmq.Queue, error)
	ListBindings(ctx context.Context) ([]rabbitmq.Binding, error)
	CreateExchange(ctx context.Context, r rabbitmq.CreateExchangeReq) error
	DeleteExchange(ctx context.Context, name string) error
	CreateQueue(ctx context.Context, r rabbitmq.CreateQueueReq) error
	DeleteQueue(ctx context.Context, name string) error
	CreateBinding(ctx context.Context, b rabbitmq.Binding) error
	DeleteBinding(ctx context.Context, b rabbitmq.Binding) error
}

// PlanRabbit diffs exchanges, queues and bindings against the vhost.
func PlanRabbit(ctx context.Context, s *Spec, r Rabbit) (*Plan, error) {
	if s.RabbitMQ == nil {
		return nil, fmt.Errorf("the changeset has no rabbitmq section")
	}
	spec := s.RabbitMQ
	p := &Plan{Kind: "rabbitmq"}
	xs, err := r.ListExchanges(ctx)
	if err != nil {
		return nil, err
	}
	qs, err := r.ListQueues(ctx)
	if err != nil {
		return nil, err
	}
	bs, err := r.ListBindings(ctx)
	if err != nil {
		return nil, err
	}
	haveX := map[string]rabbitmq.Exchange{}
	for _, x := range xs {
		haveX[x.Name] = x
	}
	haveQ := map[string]rabbitmq.Queue{}
	for _, q := range qs {
		haveQ[q.Name] = q
	}
	var delB, delQ, delX []Change

	for _, x := range spec.Exchanges {
		x := x
		cur, ok := haveX[x.Name]
		if x.State == "absent" {
			if ok {
				delX = append(delX, Change{Resource: "exchange", Name: x.Name, Action: Delete,
					apply: func(ctx context.Context) error { return r.DeleteExchange(ctx, x.Name) }})
			} else {
				p.Unchanged++
			}
			continue
		}
		typ := x.Type
		if typ == "" {
			typ = "direct"
		}
		durable := boolOr(x.Durable, true)
		want := []Diff{
			{"type", cur.Type, typ},
			{"durable", strconv.FormatBool(cur.Durable), strconv.FormatBool(durable)},
			{"autoDelete", strconv.FormatBool(cur.AutoDelete), strconv.FormatBool(x.AutoDelete)},
			{"internal", strconv.FormatBool(cur.Internal), strconv.FormatBool(x.Internal)},
		}
		want = append(want, argDiffs(cur.Arguments, x.Arguments, nil)...)
		if !ok {
			p.Changes = append(p.Changes, Change{Resource: "exchange", Name: x.Name, Action: Create, Diffs: createDiffs(want),
				apply: func(ctx context.Context) error {
					return r.CreateExchange(ctx, rabbitmq.CreateExchangeReq{Name: x.Name, Type: typ, Durable: durable, AutoDelete: x.AutoDelete, Internal: x.Internal, Arguments: x.Arguments})
				}})
			continue
		}
		if d := changed(want); len(d) > 0 {
			p.Changes = append(p.Changes, Change{Resource: "exchange", Name: x.Name, Action: Conflict, Diffs: d,
				Notes: []string{i18n.T("cs.exchangeImmutable")}})
		} else {
			p.Unchanged++
		}
	}

	for _, q := range spec.Queues {
		q := q
		cur, ok := haveQ[q.Name]
		if q.State == "absent" {
			if ok {
				c := Change{Resource: "queue", Name: q.Name, Action: Delete,
					apply: func(ctx context.Context) error { return r.DeleteQueue(ctx, q.Name) }}
				if cur.Messages > 0 {
					c.Notes = append(c.Notes, i18n.T("cs.queueLoss", cur.Messages))
				}
				delQ = append(delQ, c)
			} else {
				p.Unchanged++
			}
			continue
		}
		typ := q.Type
		if typ == "" {
			typ = "classic"
		}
		durable := boolOr(q.Durable, true)
		curType := cur.Type
		if curType == "" {
			curType = "classic"
		}
		want := []Diff{
			{"type", curType, typ},
			{"durable", strconv.FormatBool(cur.Durable), strconv.FormatBool(durable)},
			{"autoDelete", strconv.FormatBool(cur.AutoDelete), strconv.FormatBool(q.AutoDelete)},
		}
		want = append(want, argDiffs(cur.Arguments, q.Arguments, map[string]bool{"x-queue-type": true})...)
		if !ok {
			p.Changes = append(p.Changes, Change{Resource: "queue", Name: q.Name, Action: Create, Diffs: createDiffs(want),
				apply: func(ctx context.Context) error {
					return r.CreateQueue(ctx, rabbitmq.CreateQueueReq{Name: q.Name, Type: typ, Durable: durable, AutoDelete: q.AutoDelete, Arguments: q.Arguments})
				}})
			continue
		}
		if d := changed(want); len(d) > 0 {
			p.Changes = append(p.Changes, Change{Resource: "queue", Name: q.Name, Action: Conflict, Diffs: d,
				Notes: []string{i18n.T("cs.queueImmutable")}})
		} else {
			p.Unchanged++
		}
	}

	for _, b := range spec.Bindings {
		want := rabbitmq.Binding{Source: b.Exchange, Destination: b.Queue, DestinationType: "queue", RoutingKey: b.RoutingKey, Arguments: b.Arguments}
		if b.ToExchange != "" {
			want.Destination, want.DestinationType = b.ToExchange, "exchange"
		}
		name := bindingName(want)
		var found *rabbitmq.Binding
		for i := range bs {
			if sameBinding(bs[i], want) {
				found = &bs[i]
				break
			}
		}
		switch {
		case b.State == "absent" && found != nil:
			cur := *found
			delB = append(delB, Change{Resource: "binding", Name: name, Action: Delete,
				apply: func(ctx context.Context) error { return r.DeleteBinding(ctx, cur) }})
		case b.State == "absent", found != nil:
			p.Unchanged++
		default:
			p.Changes = append(p.Changes, Change{Resource: "binding", Name: name, Action: Create,
				Diffs: argDiffs(nil, b.Arguments, nil),
				apply: func(ctx context.Context) error { return r.CreateBinding(ctx, want) }})
		}
	}
	// Deletions run last, most dependent first.
	p.Changes = append(append(append(p.Changes, delB...), delQ...), delX...)
	p.seal()
	return p, nil
}

func bindingName(b rabbitmq.Binding) string {
	arrow := " → "
	if b.DestinationType == "exchange" {
		arrow = " → exchange "
	}
	n := b.Source + arrow + b.Destination
	if b.RoutingKey != "" {
		n += " (" + b.RoutingKey + ")"
	}
	return n
}

func sameBinding(a, b rabbitmq.Binding) bool {
	return a.Source == b.Source && a.Destination == b.Destination && a.DestinationType == b.DestinationType &&
		a.RoutingKey == b.RoutingKey && reflect.DeepEqual(norm(nonNil(a.Arguments)), norm(nonNil(b.Arguments)))
}

// ExportRabbit describes the vhost's exchanges, queues and bindings as a changeset.
// Built-in exchanges, exclusive queues and the default exchange's implicit bindings are skipped.
func ExportRabbit(ctx context.Context, r Rabbit) (*Spec, error) {
	xs, err := r.ListExchanges(ctx)
	if err != nil {
		return nil, err
	}
	qs, err := r.ListQueues(ctx)
	if err != nil {
		return nil, err
	}
	bs, err := r.ListBindings(ctx)
	if err != nil {
		return nil, err
	}
	rs := &RabbitSpec{}
	for _, x := range xs {
		if x.Name == "" || strings.HasPrefix(x.Name, "amq.") {
			continue
		}
		e := ExchangeSpec{Name: x.Name, Type: x.Type, AutoDelete: x.AutoDelete, Internal: x.Internal, Arguments: emptyNil(x.Arguments)}
		if x.Type == "direct" {
			e.Type = ""
		}
		if !x.Durable {
			e.Durable = new(bool)
		}
		rs.Exchanges = append(rs.Exchanges, e)
	}
	skipQ := map[string]bool{}
	for _, q := range qs {
		if q.Exclusive {
			skipQ[q.Name] = true
			continue
		}
		args := map[string]any{}
		for k, v := range q.Arguments {
			if k != "x-queue-type" {
				args[k] = v
			}
		}
		e := QueueSpec{Name: q.Name, Type: q.Type, AutoDelete: q.AutoDelete, Arguments: emptyNil(args)}
		if q.Type == "classic" {
			e.Type = ""
		}
		if !q.Durable {
			e.Durable = new(bool)
		}
		rs.Queues = append(rs.Queues, e)
	}
	for _, b := range bs {
		if b.Source == "" || skipQ[b.Destination] && b.DestinationType == "queue" {
			continue
		}
		e := BindingSpec{Exchange: b.Source, RoutingKey: b.RoutingKey, Arguments: emptyNil(b.Arguments)}
		if b.DestinationType == "exchange" {
			e.ToExchange = b.Destination
		} else {
			e.Queue = b.Destination
		}
		rs.Bindings = append(rs.Bindings, e)
	}
	sort.SliceStable(rs.Bindings, func(i, j int) bool { return rs.Bindings[i].Exchange < rs.Bindings[j].Exchange })
	return &Spec{Version: 1, RabbitMQ: rs}, nil
}

func createDiffs(ds []Diff) []Diff {
	out := make([]Diff, 0, len(ds))
	for _, d := range ds {
		out = append(out, Diff{Field: d.Field, To: d.To})
	}
	return out
}

func changed(ds []Diff) []Diff {
	var out []Diff
	for _, d := range ds {
		if d.From != d.To {
			out = append(out, d)
		}
	}
	return out
}

// argDiffs compares argument maps as JSON so 60000 (YAML int) equals 60000 (JSON number).
func argDiffs(have, want map[string]any, ignore map[string]bool) []Diff {
	h, w := norm(have), norm(want)
	keys := map[string]bool{}
	for k := range h {
		keys[k] = true
	}
	for k := range w {
		keys[k] = true
	}
	var out []Diff
	for _, k := range sortedKeys(keys) {
		if ignore[k] {
			continue
		}
		out = append(out, Diff{"arguments." + k, jsonStr(h[k]), jsonStr(w[k])})
	}
	return out
}

func norm(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return m
	}
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func jsonStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func nonNil(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func emptyNil(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	return m
}
