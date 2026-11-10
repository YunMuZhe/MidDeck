package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/driver/kafka"
	"github.com/YunMuZhe/MidDeck/internal/driver/rabbitmq"
	"github.com/YunMuZhe/MidDeck/internal/driver/redis"
	"github.com/YunMuZhe/MidDeck/internal/i18n"
	"github.com/YunMuZhe/MidDeck/internal/notify"
	"github.com/YunMuZhe/MidDeck/internal/watch"
)

// Patrol alerts: rules are checked in the background while the desktop app is
// open (see internal/watch). Checks only read; they reuse the app's sessions.

func (a *App) startWatch() {
	a.watcher = watch.New(a.dataDir, a.probe, a.alert)
	a.watcher.Start(a.ctx)
}

func (a *App) needWatch() (*watch.Engine, error) {
	if a.watcher == nil {
		return nil, errors.New(i18n.T("watch.off"))
	}
	return a.watcher, nil
}

// redisSample is how many keys one big-key check may look at.
const redisSample = 5000

func (a *App) probe(ctx context.Context, r watch.Rule, m *watch.Memory) ([]watch.Item, int, error) {
	switch r.Type {
	case watch.KafkaLag:
		s, _, err := sessionAs[*kafka.Session](a, r.InstanceID)
		if err != nil {
			return nil, 0, err
		}
		groups, err := s.ListGroups(ctx)
		if err != nil {
			return nil, 0, err
		}
		items := []watch.Item{}
		for _, g := range groups {
			if watch.Match(r.Target, g.Group) {
				items = append(items, watch.Item{Name: g.Group, Value: g.Lag})
			}
		}
		return items, 0, nil
	case watch.RabbitDLQ:
		s, _, err := sessionAs[*rabbitmq.Session](a, r.InstanceID)
		if err != nil {
			return nil, 0, err
		}
		queues, err := s.ListQueues(ctx)
		if err != nil {
			return nil, 0, err
		}
		dead := deadLetterQueues(queues)
		items := []watch.Item{}
		for _, q := range queues {
			if (r.Target == "" && dead[q.Name]) || (r.Target != "" && watch.Match(r.Target, q.Name)) {
				items = append(items, watch.Item{Name: q.Name, Value: q.Messages})
			}
		}
		return items, 0, nil
	case watch.RedisBigKey:
		s, _, err := sessionAs[*redis.Session](a, r.InstanceID)
		if err != nil {
			return nil, 0, err
		}
		if m.Cursors == nil {
			m.Cursors = map[string]uint64{}
		}
		pattern := r.Target
		if pattern != "" && !strings.Contains(pattern, "*") {
			pattern = "*" + pattern + "*"
		}
		sizes, err := s.SampleMemory(ctx, pattern, m.Cursors, redisSample, m.Known)
		if err != nil {
			return nil, 0, err
		}
		// Keep the keys over the limit, and the largest one so a quiet rule still
		// shows a reading.
		sort.Slice(sizes, func(i, j int) bool { return sizes[i].Bytes > sizes[j].Bytes })
		items, known := []watch.Item{}, []string{}
		for i, k := range sizes {
			if k.Bytes >= r.Threshold && len(known) < 200 {
				known = append(known, k.Key)
			} else if i > 0 {
				break
			}
			items = append(items, watch.Item{Name: k.Key, Value: k.Bytes})
		}
		m.Known = known
		return items, len(sizes), nil
	}
	return nil, 0, fmt.Errorf("unknown rule type %q", r.Type)
}

// deadLetterQueues guesses which queues hold dead letters: ones named like it,
// and ones another queue routes its dead letters to by name.
func deadLetterQueues(queues []rabbitmq.Queue) map[string]bool {
	dead := map[string]bool{}
	for _, q := range queues {
		n := strings.ToLower(q.Name)
		for _, hint := range []string{"dlq", "dlx", "dead", "deadletter", "dead-letter", "dead_letter"} {
			if strings.Contains(n, hint) {
				dead[q.Name] = true
			}
		}
		if key, ok := q.Arguments["x-dead-letter-routing-key"].(string); ok && key != "" && key != q.Name {
			dead[key] = true
		}
	}
	// A routing key is only a queue name by convention; keep real queues.
	names := map[string]bool{}
	for _, q := range queues {
		names[q.Name] = true
	}
	for n := range dead {
		if !names[n] {
			delete(dead, n)
		}
	}
	return dead
}

// alert shows a desktop notification and tells the window.
func (a *App) alert(al watch.Alert, r watch.Rule) {
	title, body := a.alertText(al, r)
	if err := notify.Send(title, body); err != nil {
		runtime.LogWarningf(a.ctx, "notify: %v", err)
	}
	runtime.EventsEmit(a.ctx, "watch:alert", map[string]any{"alert": al, "title": title, "body": body})
}

func (a *App) alertText(al watch.Alert, r watch.Rule) (title, body string) {
	where := r.InstanceID
	if inst, err := a.store.Get(r.InstanceID); err == nil {
		where = inst.Name
		if inst.Env != "" {
			where = "[" + inst.Env + "] " + inst.Name
		}
	}
	what := i18n.T("watch." + r.Type)
	switch al.Kind {
	case "recovered":
		what = i18n.T("watch.recovered") + " · " + what
	case "reminder":
		what = i18n.T("watch.reminder") + " · " + what
	case "error":
		what = i18n.T("watch.failed") + " · " + what
	}
	title = what + " · " + where
	switch al.Kind {
	case "error":
		body = al.Error
	case "recovered":
		body = i18n.T("watch.backToNormal")
	default:
		var parts []string
		for i, it := range al.Items {
			if i == 3 {
				parts = append(parts, i18n.T("watch.more", len(al.Items)-3))
				break
			}
			v := groupDigits(it.Value)
			if r.Type == watch.RedisBigKey {
				v = humanBytes(it.Value)
			}
			if it.Delta > 0 {
				v += " (+" + groupDigits(it.Delta) + ")"
			}
			parts = append(parts, it.Name+" "+v)
		}
		body = strings.Join(parts, "; ")
	}
	if r.Name != "" {
		body = r.Name + ": " + body
	}
	return title, body
}

func groupDigits(n int64) string {
	s := fmt.Sprint(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

func humanBytes(n int64) string {
	f, units := float64(n), []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f, i = f/1024, i+1
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

// --- bindings -----------------------------------------------------------------

// WatchState is everything the Alerts panel shows.
type WatchState struct {
	Rules    []watch.Rule   `json:"rules"`
	Statuses []watch.Status `json:"statuses"`
	Alerts   []watch.Alert  `json:"alerts"`
}

func (a *App) WatchState() (WatchState, error) {
	w, err := a.needWatch()
	if err != nil {
		return WatchState{}, err
	}
	return WatchState{Rules: w.Rules(), Statuses: w.Statuses(), Alerts: w.History(100)}, nil
}

func (a *App) WatchSaveRule(r watch.Rule) (watch.Rule, error) {
	w, err := a.needWatch()
	if err != nil {
		return r, err
	}
	inst, err := a.store.Get(r.InstanceID)
	if err != nil {
		return r, err
	}
	want := map[string]config.Kind{watch.KafkaLag: config.KindKafka, watch.RabbitDLQ: config.KindRabbitMQ, watch.RedisBigKey: config.KindRedis}[r.Type]
	if want != "" && inst.Kind != want {
		return r, errors.New(i18n.T("err.wrongKind", inst.Name))
	}
	return w.Save(r, config.NewID)
}

func (a *App) WatchDeleteRule(id string) error {
	w, err := a.needWatch()
	if err != nil {
		return err
	}
	return w.Delete(id)
}

// WatchCheck runs one rule now.
func (a *App) WatchCheck(id string) (watch.Status, error) {
	w, err := a.needWatch()
	if err != nil {
		return watch.Status{}, err
	}
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	return w.CheckNow(ctx, id)
}

func (a *App) WatchClearAlerts() error {
	w, err := a.needWatch()
	if err != nil {
		return err
	}
	w.ClearHistory()
	return nil
}

// WatchTestNotify shows a sample notification so the user can see (and allow) it.
func (a *App) WatchTestNotify() error {
	return notify.Send(i18n.T("watch.test"), i18n.T("watch.testBody"))
}
