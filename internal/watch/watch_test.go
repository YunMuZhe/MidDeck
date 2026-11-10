package watch

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		p, n string
		want bool
	}{
		{"", "anything", true}, {"order", "order-svc", true}, {"order", "pay", false},
		{"order-*", "order-svc", true}, {"order-*", "x-order-svc", false},
		{"*.dlq", "orders.dlq", true}, {"*.dlq", "orders.dlq.old", false},
		{"a*b*c", "a1b2c", true}, {"a*b*c", "a1c", false}, {"*", "", true},
	} {
		if got := Match(c.p, c.n); got != c.want {
			t.Errorf("Match(%q,%q)=%v", c.p, c.n, got)
		}
	}
}

func TestValidate(t *testing.T) {
	r := Rule{Type: KafkaLag, InstanceID: "i"}
	if r.Validate() == nil {
		t.Error("a rule without thresholds must be rejected")
	}
	r = Rule{Type: RedisBigKey, InstanceID: "i", Threshold: 1, Growth: 5, IntervalSec: 1}
	if err := r.Validate(); err != nil || r.Growth != 0 || r.IntervalSec != 60 {
		t.Errorf("got %+v, %v", r, err)
	}
}

func TestEngineTransitions(t *testing.T) {
	var (
		lag    int64
		fail   error
		alerts []Alert
		now    = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	)
	e := New(t.TempDir(), func(context.Context, Rule, *Memory) ([]Item, int, error) {
		if fail != nil {
			return nil, 0, fail
		}
		return []Item{{Name: "g1", Value: lag}, {Name: "g2", Value: 1}}, 0, nil
	}, func(a Alert, _ Rule) { alerts = append(alerts, a) })
	e.now = func() time.Time { return now }
	n := 0
	r, err := e.Save(Rule{Name: "lag", Type: KafkaLag, InstanceID: "i", Threshold: 1000, Growth: 300, Enabled: true}, func() string { n++; return "r1" })
	if err != nil {
		t.Fatal(err)
	}
	check := func(wantState string, wantAlerts int, kind string) {
		t.Helper()
		st, err := e.CheckNow(context.Background(), r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if st.State != wantState || len(alerts) != wantAlerts {
			t.Fatalf("state %s alerts %d, want %s %d", st.State, len(alerts), wantState, wantAlerts)
		}
		if kind != "" && alerts[len(alerts)-1].Kind != kind {
			t.Fatalf("last alert %s, want %s", alerts[len(alerts)-1].Kind, kind)
		}
		now = now.Add(time.Minute)
	}
	lag = 10
	check(OK, 0, "")
	lag = 400 // +390 since the last check: growth
	check(Firing, 1, "firing")
	if alerts[0].Items[0].Name != "g1" || alerts[0].Items[0].Delta != 390 {
		t.Fatalf("%+v", alerts[0].Items)
	}
	lag = 450 // +50: quiet again
	check(OK, 2, "recovered")
	lag = 5000 // absolute threshold
	check(Firing, 3, "firing")
	check(Firing, 3, "") // still firing: no repeat yet
	now = now.Add(Remind)
	check(Firing, 4, "reminder")
	fail = errors.New("down")
	check(Failed, 4, "")
	check(Failed, 4, "")
	check(Failed, 5, "error") // third failure in a row
	fail, lag = nil, 1
	check(OK, 5, "") // error -> ok is not a recovery from firing

	// rules and history survive a restart
	e2 := New(e.dir, nil, nil)
	if len(e2.Rules()) != 1 || len(e2.History(10)) != 5 || e2.History(1)[0].Kind != "error" {
		t.Fatalf("reload: %d rules, %d alerts", len(e2.Rules()), len(e2.History(10)))
	}
}
