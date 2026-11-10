// Package watch runs local patrol rules while MidDeck is open: Kafka consumer lag,
// RabbitMQ dead-letter queue depth and growth, and big Redis keys. A rule fires
// when a reading crosses its threshold and recovers when it no longer does; both
// transitions raise an alert, and a rule that keeps firing reminds again after a
// while.
package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Rule types.
const (
	KafkaLag    = "kafka_lag"
	RabbitDLQ   = "rabbit_dlq"
	RedisBigKey = "redis_bigkey"
)

// Rule is one check on one instance.
type Rule struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	InstanceID string `json:"instanceId"`
	Type       string `json:"type"`
	// Target narrows what is measured: a consumer group pattern, a queue pattern,
	// or a key pattern. Globs with *; empty means all groups, the queues that look
	// like dead-letter queues, or every key.
	Target string `json:"target"`
	// Threshold fires when a reading is at or above it: lag in messages, queue
	// depth in messages, key size in bytes. 0 turns the absolute check off.
	Threshold int64 `json:"threshold"`
	// Growth fires when a reading rose by at least this much since the previous
	// check (lag and queue depth only). 0 turns it off.
	Growth      int64 `json:"growth"`
	IntervalSec int   `json:"intervalSec"`
	Enabled     bool  `json:"enabled"`
}

// Validate normalises a rule and rejects ones that could never fire.
func (r *Rule) Validate() error {
	r.Name, r.Target = strings.TrimSpace(r.Name), strings.TrimSpace(r.Target)
	switch r.Type {
	case KafkaLag, RabbitDLQ:
	case RedisBigKey:
		r.Growth = 0
	default:
		return fmt.Errorf("unknown rule type %q", r.Type)
	}
	if r.InstanceID == "" {
		return errors.New("pick a connection")
	}
	if r.Threshold < 0 || r.Growth < 0 {
		return errors.New("thresholds must not be negative")
	}
	if r.Threshold == 0 && r.Growth == 0 {
		return errors.New("set a threshold")
	}
	min := 15
	if r.Type == RedisBigKey {
		min = 60 // sampling a keyspace is not free
	}
	if r.IntervalSec < min {
		r.IntervalSec = min
	}
	return nil
}

// Item is one measured thing: a consumer group, a queue, a key.
type Item struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
	// Delta is the change since the previous check (set by the engine).
	Delta int64 `json:"delta,omitempty"`
}

// Memory is what a probe may keep between checks of a rule.
type Memory struct {
	Cursors map[string]uint64 // redis: where the next sample continues, per node
	Known   []string          // redis: big keys found earlier, re-measured every check
}

// Probe measures a rule. It returns the items worth keeping and how many things
// it looked at (0 means len(items)).
type Probe func(ctx context.Context, r Rule, m *Memory) (items []Item, checked int, err error)

// States.
const (
	Idle   = "idle"
	OK     = "ok"
	Firing = "firing"
	Failed = "error"
)

// Status is the latest result of a rule.
type Status struct {
	RuleID    string    `json:"ruleId"`
	State     string    `json:"state"`
	CheckedAt time.Time `json:"checkedAt"`
	Since     time.Time `json:"since"` // when the current state began
	// Worst holds the items over the limit (largest first, at most 10); when the
	// rule is fine, the single largest item.
	Worst   []Item `json:"worst"`
	Checked int    `json:"checked"` // how many items were looked at
	Error   string `json:"error,omitempty"`
}

// Alert is one raised or recovered event.
type Alert struct {
	Time     time.Time `json:"time"`
	RuleID   string    `json:"ruleId"`
	Rule     string    `json:"rule"`
	Type     string    `json:"type"`
	Instance string    `json:"instance"` // instance id
	// Kind: firing | reminder | recovered | error
	Kind  string `json:"kind"`
	Items []Item `json:"items,omitempty"`
	Error string `json:"error,omitempty"`
}

// Remind is how long a rule keeps firing before it alerts again.
const Remind = 30 * time.Minute

const historyMax = 300

type ruleState struct {
	status   Status
	prev     map[string]int64
	mem      Memory
	running  bool
	lastSent time.Time
	failures int
}

// Engine schedules the rules.
type Engine struct {
	dir    string
	probe  Probe
	notify func(Alert, Rule)
	now    func() time.Time

	mu      sync.Mutex
	rules   []Rule
	state   map[string]*ruleState
	history []Alert
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// New loads rules and alert history from dir.
func New(dir string, probe Probe, notify func(Alert, Rule)) *Engine {
	e := &Engine{dir: dir, probe: probe, notify: notify, now: time.Now, state: map[string]*ruleState{}}
	if b, err := os.ReadFile(e.rulesPath()); err == nil {
		_ = json.Unmarshal(b, &e.rules)
	}
	if b, err := os.ReadFile(e.historyPath()); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			var a Alert
			if line != "" && json.Unmarshal([]byte(line), &a) == nil {
				e.history = append(e.history, a)
			}
		}
		if len(e.history) > historyMax {
			e.history = e.history[len(e.history)-historyMax:]
		}
	}
	return e
}

func (e *Engine) rulesPath() string   { return filepath.Join(e.dir, "watch.json") }
func (e *Engine) historyPath() string { return filepath.Join(e.dir, "alerts.log") }

// Start begins checking due rules until Stop.
func (e *Engine) Start(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			e.tick(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// Stop cancels running checks and waits for them.
func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
		e.wg.Wait()
	}
}

func (e *Engine) tick(ctx context.Context) {
	e.mu.Lock()
	now := e.now()
	var due []Rule
	for _, r := range e.rules {
		st := e.stateOf(r.ID)
		if !r.Enabled || st.running {
			continue
		}
		if st.status.CheckedAt.IsZero() || now.Sub(st.status.CheckedAt) >= time.Duration(r.IntervalSec)*time.Second {
			st.running = true
			due = append(due, r)
		}
	}
	e.mu.Unlock()
	for _, r := range due {
		e.wg.Add(1)
		go func(r Rule) {
			defer e.wg.Done()
			e.check(ctx, r)
		}(r)
	}
}

func (e *Engine) stateOf(id string) *ruleState {
	st, ok := e.state[id]
	if !ok {
		st = &ruleState{status: Status{RuleID: id, State: Idle}}
		e.state[id] = st
	}
	return st
}

// CheckNow runs one rule immediately and returns its status.
func (e *Engine) CheckNow(ctx context.Context, id string) (Status, error) {
	e.mu.Lock()
	var rule *Rule
	for i := range e.rules {
		if e.rules[i].ID == id {
			r := e.rules[i]
			rule = &r
		}
	}
	if rule == nil {
		e.mu.Unlock()
		return Status{}, errors.New("rule not found")
	}
	st := e.stateOf(id)
	if st.running {
		e.mu.Unlock()
		return st.status, nil
	}
	st.running = true
	e.mu.Unlock()
	e.check(ctx, *rule)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stateOf(id).status, nil
}

func (e *Engine) check(ctx context.Context, r Rule) {
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	e.mu.Lock()
	st := e.stateOf(r.ID)
	mem := st.mem
	e.mu.Unlock()

	items, checked, err := e.probe(cctx, r, &mem)
	if checked == 0 {
		checked = len(items)
	}

	e.mu.Lock()
	st = e.stateOf(r.ID)
	st.running = false
	if ctx.Err() != nil { // shutting down: not a real failure
		e.mu.Unlock()
		return
	}
	now := e.now()
	prevState := st.status.State
	next := Status{RuleID: r.ID, CheckedAt: now, Since: st.status.Since, Checked: checked}
	var alert *Alert
	if err != nil {
		st.failures++
		next.State, next.Error, next.Worst = Failed, err.Error(), st.status.Worst
		// One hiccup is not worth a notification; a rule that stays blind is.
		if st.failures == 3 {
			alert = &Alert{Kind: "error", Error: err.Error()}
		}
	} else {
		st.failures = 0
		st.mem = mem
		over, top := evaluate(r, items, st.prev)
		st.prev = map[string]int64{}
		for _, it := range items {
			st.prev[it.Name] = it.Value
		}
		if len(over) > 0 {
			next.State, next.Worst = Firing, over
			switch {
			case prevState != Firing:
				alert = &Alert{Kind: "firing", Items: over}
			case now.Sub(st.lastSent) >= Remind:
				alert = &Alert{Kind: "reminder", Items: over}
			}
		} else {
			next.State, next.Worst = OK, top
			if prevState == Firing {
				alert = &Alert{Kind: "recovered", Items: top}
			}
		}
	}
	if next.State != prevState || next.Since.IsZero() {
		next.Since = now
	}
	st.status = next
	if alert != nil {
		alert.Time, alert.RuleID, alert.Rule, alert.Type, alert.Instance = now, r.ID, r.Name, r.Type, r.InstanceID
		if alert.Kind != "error" {
			st.lastSent = now
		}
		e.record(*alert)
	}
	e.mu.Unlock()
	if alert != nil && e.notify != nil {
		e.notify(*alert, r)
	}
}

// evaluate returns the items over the rule's limits (largest first, at most 10)
// and, for a quiet rule, the single largest item.
func evaluate(r Rule, items []Item, prev map[string]int64) (over, top []Item) {
	for i := range items {
		it := &items[i]
		if p, ok := prev[it.Name]; ok {
			it.Delta = it.Value - p
		}
		if (r.Threshold > 0 && it.Value >= r.Threshold) || (r.Growth > 0 && it.Delta >= r.Growth) {
			over = append(over, *it)
		}
	}
	bySize := func(l []Item) {
		sort.SliceStable(l, func(i, j int) bool { return l[i].Value > l[j].Value })
	}
	bySize(over)
	if len(over) > 10 {
		over = over[:10]
	}
	if len(over) == 0 && len(items) > 0 {
		all := append([]Item(nil), items...)
		bySize(all)
		top = all[:1]
	}
	return over, top
}

func (e *Engine) record(a Alert) {
	e.history = append(e.history, a)
	if len(e.history) > historyMax {
		e.history = e.history[len(e.history)-historyMax:]
	}
	var b []byte
	for _, h := range e.history {
		line, _ := json.Marshal(h)
		b = append(append(b, line...), '\n')
	}
	_ = os.WriteFile(e.historyPath(), b, 0o600)
}

// Rules returns the rules in the order they were added.
func (e *Engine) Rules() []Rule {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Rule{}, e.rules...)
}

// Statuses returns the latest status of every rule.
func (e *Engine) Statuses() []Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Status, 0, len(e.rules))
	for _, r := range e.rules {
		out = append(out, e.stateOf(r.ID).status)
	}
	return out
}

// History returns alerts, newest first.
func (e *Engine) History(n int) []Alert {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []Alert{}
	for i := len(e.history) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, e.history[i])
	}
	return out
}

// ClearHistory forgets past alerts.
func (e *Engine) ClearHistory() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.history = nil
	_ = os.Remove(e.historyPath())
}

// Save adds or replaces a rule. Changing a rule resets its state, so it is
// checked right away. newID makes an id for a new rule.
func (e *Engine) Save(r Rule, newID func() string) (Rule, error) {
	if err := r.Validate(); err != nil {
		return r, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if r.ID == "" {
		r.ID = newID()
		e.rules = append(e.rules, r)
	} else {
		found := false
		for i := range e.rules {
			if e.rules[i].ID == r.ID {
				e.rules[i], found = r, true
			}
		}
		if !found {
			e.rules = append(e.rules, r)
		}
		if st, ok := e.state[r.ID]; ok && !st.running {
			delete(e.state, r.ID)
		}
	}
	return r, e.flush()
}

// Delete removes a rule.
func (e *Engine) Delete(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	keep := e.rules[:0]
	for _, r := range e.rules {
		if r.ID != id {
			keep = append(keep, r)
		}
	}
	e.rules = keep
	delete(e.state, id)
	return e.flush()
}

func (e *Engine) flush() error {
	b, err := json.MarshalIndent(e.rules, "", "  ")
	if err != nil {
		return err
	}
	tmp := e.rulesPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, e.rulesPath())
}

// Match reports whether name matches a pattern with * wildcards. An empty
// pattern matches everything; a pattern without * must be contained in name.
func Match(pattern, name string) bool {
	if pattern == "" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return strings.Contains(name, pattern)
	}
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(name, parts[0]) {
		return false
	}
	name = name[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(name, p)
		if i < 0 {
			return false
		}
		name = name[i+len(p):]
	}
	return strings.HasSuffix(name, last)
}
