package changeset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Actions a change can carry.
const (
	Create   = "create"
	Update   = "update"
	Delete   = "delete"
	Conflict = "conflict" // differs in a way that cannot be changed in place; skipped
)

// Diff is one field that changes.
type Diff struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// Change is one resource to create, update or delete.
type Change struct {
	Resource string   `json:"resource"` // topic | exchange | queue | binding
	Name     string   `json:"name"`
	Action   string   `json:"action"`
	Diffs    []Diff   `json:"diffs,omitempty"`
	Notes    []string `json:"notes,omitempty"`

	apply func(ctx context.Context) error
}

// Plan is the difference between a changeset and an instance.
type Plan struct {
	Kind      string   `json:"kind"`
	Env       string   `json:"env"`
	Changes   []Change `json:"changes"`
	Unchanged int      `json:"unchanged"`
	// Warnings are differences the changeset will not touch (e.g. replication factor).
	Warnings []string `json:"warnings"`
	// Fingerprint identifies this exact plan, so apply can refuse when the
	// instance changed after the user reviewed it.
	Fingerprint string `json:"fingerprint"`
}

// Count returns how many changes have the action.
func (p *Plan) Count(action string) int {
	n := 0
	for _, c := range p.Changes {
		if c.Action == action {
			n++
		}
	}
	return n
}

func (p *Plan) seal() {
	if p.Changes == nil {
		p.Changes = []Change{}
	}
	if p.Warnings == nil {
		p.Warnings = []string{}
	}
	b, _ := json.Marshal(struct {
		K, E string
		C    []Change
	}{p.Kind, p.Env, p.Changes})
	h := sha256.Sum256(b)
	p.Fingerprint = hex.EncodeToString(h[:8])
}

// ApplyOptions control apply.
type ApplyOptions struct {
	AllowDelete bool
	// Fingerprint, when set, must match the freshly computed plan.
	Fingerprint string
	// SkipConflicts applies the rest of the plan when some changes conflict.
	SkipConflicts bool
	// Guard wraps every change (read-only check, audit log). May be nil.
	Guard func(c Change, run func() error) error
}

// Result is what happened to one change.
type Result struct {
	Change
	Status string `json:"status"` // applied | failed | skipped
	Error  string `json:"error,omitempty"`
}

// Errors returned by Apply before anything runs.
var (
	ErrStale     = errors.New("the instance changed since this plan was made; plan again and review it")
	ErrDeletes   = errors.New("the plan deletes resources; allow deletions to apply it")
	ErrConflicts = errors.New("the plan has conflicts that cannot be applied in place")
)

// Apply runs the plan's changes in order and stops at the first failure.
func Apply(ctx context.Context, p *Plan, o ApplyOptions) ([]Result, error) {
	if o.Fingerprint != "" && o.Fingerprint != p.Fingerprint {
		return nil, ErrStale
	}
	if p.Count(Delete) > 0 && !o.AllowDelete {
		return nil, ErrDeletes
	}
	if p.Count(Conflict) > 0 && !o.SkipConflicts {
		return nil, ErrConflicts
	}
	out := make([]Result, 0, len(p.Changes))
	failed := false
	for _, c := range p.Changes {
		r := Result{Change: c}
		switch {
		case c.Action == Conflict, failed:
			r.Status = "skipped"
		default:
			run := func() error { return c.apply(ctx) }
			var err error
			if o.Guard != nil {
				err = o.Guard(c, run)
			} else {
				err = run()
			}
			if err != nil {
				r.Status, r.Error, failed = "failed", err.Error(), true
			} else {
				r.Status = "applied"
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// Format renders a plan for the terminal.
func Format(p *Plan) string {
	var b strings.Builder
	sign := map[string]string{Create: "+", Update: "~", Delete: "-", Conflict: "!"}
	for _, c := range p.Changes {
		fmt.Fprintf(&b, "  %s %s %s", sign[c.Action], c.Resource, c.Name)
		if c.Action == Conflict {
			b.WriteString("  (conflict, skipped)")
		}
		b.WriteByte('\n')
		for _, d := range c.Diffs {
			switch {
			case c.Action == Create:
				fmt.Fprintf(&b, "      %s: %s\n", d.Field, d.To)
			case d.To == "":
				fmt.Fprintf(&b, "      %s: %s → (default)\n", d.Field, d.From)
			default:
				fmt.Fprintf(&b, "      %s: %s → %s\n", d.Field, orDash(d.From), d.To)
			}
		}
		for _, n := range c.Notes {
			fmt.Fprintf(&b, "      # %s\n", n)
		}
	}
	for _, w := range p.Warnings {
		fmt.Fprintf(&b, "  warning: %s\n", w)
	}
	fmt.Fprintf(&b, "\nPlan: %d to create, %d to update, %d to delete, %d conflicts, %d unchanged.\n",
		p.Count(Create), p.Count(Update), p.Count(Delete), p.Count(Conflict), p.Unchanged)
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
