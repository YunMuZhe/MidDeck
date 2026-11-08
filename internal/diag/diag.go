// Package diag holds the layered connection-diagnostics report types.
package diag

import "time"

type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
	Skip Status = "skip"
)

// Step is one line in a connection test report, e.g. "SSH hop 1", "TLS handshake".
type Step struct {
	Name     string `json:"name"`
	Status   Status `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Hint     string `json:"hint,omitempty"`
	Duration int64  `json:"durationMs"`
}

// Report collects steps.
type Report struct {
	Steps []Step `json:"steps"`
	// OK is false if any step failed.
	OK bool `json:"ok"`
}

func (r *Report) Add(s Step) {
	r.Steps = append(r.Steps, s)
}

// Run times fn and records it as a step. It returns fn's error.
func (r *Report) Run(name string, fn func() (detail string, err error), hint func(error) string) error {
	start := time.Now()
	detail, err := fn()
	s := Step{Name: name, Status: OK, Detail: detail, Duration: time.Since(start).Milliseconds()}
	if err != nil {
		s.Status = Fail
		s.Detail = err.Error()
		if hint != nil {
			s.Hint = hint(err)
		}
	}
	r.Add(s)
	return err
}

// Finish computes OK.
func (r *Report) Finish() *Report {
	r.OK = true
	for _, s := range r.Steps {
		if s.Status == Fail {
			r.OK = false
		}
	}
	return r
}
