package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/changeset"
	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/diag"
	"github.com/YunMuZhe/MidDeck/internal/driver/kafka"
	"github.com/YunMuZhe/MidDeck/internal/driver/rabbitmq"
	"github.com/YunMuZhe/MidDeck/internal/i18n"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// planFor parses the YAML and diffs it against the instance. env "" means the
// instance's own environment.
func (a *App) planFor(ctx context.Context, id, text, env string) (*changeset.Plan, *conn, error) {
	spec, err := changeset.Parse([]byte(text))
	if err != nil {
		return nil, nil, err
	}
	c, err := a.get(id)
	if err != nil {
		return nil, nil, err
	}
	if env == "" {
		env = c.inst.Env
	}
	var p *changeset.Plan
	switch s := c.session.(type) {
	case *kafka.Session:
		p, err = changeset.PlanKafka(ctx, spec, env, s)
	case *rabbitmq.Session:
		p, err = changeset.PlanRabbit(ctx, spec, s)
		if p != nil {
			p.Env = env
		}
	default:
		err = errors.New(i18n.T("changeset.unsupported", c.inst.Kind))
	}
	return p, c, err
}

// ChangesetPlan shows what applying the YAML to the instance would do.
func (a *App) ChangesetPlan(id, text, env string) (*changeset.Plan, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	p, _, err := a.planFor(ctx, id, text, env)
	return p, err
}

// ChangesetApply re-plans and applies, refusing if the plan no longer matches the
// fingerprint the user reviewed. Conflicting changes are skipped and reported.
func (a *App) ChangesetApply(id, text, env, fingerprint string, allowDelete bool) ([]changeset.Result, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
	defer cancel()
	return a.applyChangeset(ctx, id, text, env, changeset.ApplyOptions{Fingerprint: fingerprint, AllowDelete: allowDelete, SkipConflicts: true})
}

func (a *App) applyChangeset(ctx context.Context, id, text, env string, o changeset.ApplyOptions) ([]changeset.Result, error) {
	p, c, err := a.planFor(ctx, id, text, env)
	if err != nil {
		return nil, err
	}
	if c.inst.ReadOnly && len(p.Changes) > 0 {
		return nil, errors.New(i18n.T("err.readOnly", c.inst.Name, "changeset.apply"))
	}
	o.Guard = func(ch changeset.Change, run func() error) error {
		return a.write(c, "changeset."+ch.Action+"."+ch.Resource, map[string]any{"name": ch.Name, "diffs": ch.Diffs, "env": p.Env}, run)
	}
	res, err := changeset.Apply(ctx, p, o)
	switch {
	case errors.Is(err, changeset.ErrStale):
		return nil, errors.New(i18n.T("changeset.stale"))
	case errors.Is(err, changeset.ErrDeletes):
		return nil, errors.New(i18n.T("changeset.needDelete"))
	}
	return res, err
}

// ChangesetExport describes the instance's topics / exchanges, queues and bindings as YAML.
func (a *App) ChangesetExport(id string) (string, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	c, err := a.get(id)
	if err != nil {
		return "", err
	}
	return exportChangeset(ctx, c)
}

func exportChangeset(ctx context.Context, c *conn) (string, error) {
	var (
		spec *changeset.Spec
		err  error
	)
	switch s := c.session.(type) {
	case *kafka.Session:
		spec, err = changeset.ExportKafka(ctx, s)
	case *rabbitmq.Session:
		spec, err = changeset.ExportRabbit(ctx, s)
	default:
		err = errors.New(i18n.T("changeset.unsupported", c.inst.Kind))
	}
	if err != nil {
		return "", err
	}
	where := c.inst.Name
	if c.inst.Env != "" {
		where += " (" + c.inst.Env + ")"
	}
	header := fmt.Sprintf("MidDeck changeset, exported from %s on %s.\nApply elsewhere with: MidDeck changeset apply -f <file> -i <instance>",
		where, time.Now().Format("2006-01-02 15:04"))
	b, err := changeset.Encode(spec, header)
	return string(b), err
}

// TextFile is a file picked in a dialog.
type TextFile struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ChangesetOpenFile lets the user pick a YAML file.
func (a *App) ChangesetOpenFile() (TextFile, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   i18n.T("dlg.openChangeset"),
		Filters: []runtime.FileFilter{{DisplayName: "YAML", Pattern: "*.yaml;*.yml"}},
	})
	if err != nil || path == "" {
		return TextFile{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return TextFile{}, err
	}
	if len(b) > 4<<20 {
		return TextFile{}, errors.New("file is larger than 4 MiB")
	}
	return TextFile{Path: path, Name: filepath.Base(path), Content: string(b)}, nil
}

// ChangesetSaveFile writes YAML to path, or asks where when path is empty.
func (a *App) ChangesetSaveFile(path, name, content string) (string, error) {
	if path == "" {
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			name += ".yaml"
		}
		var err error
		path, err = runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
			Title: i18n.T("dlg.saveChangeset"), DefaultFilename: name,
			Filters: []runtime.FileFilter{{DisplayName: "YAML", Pattern: "*.yaml;*.yml"}},
		})
		if err != nil || path == "" {
			return "", err
		}
	}
	return path, os.WriteFile(path, []byte(content), 0o644)
}

// connectInline opens an unsaved instance (CLI flags) under a temporary id.
func (a *App) connectInline(ctx context.Context, inst config.Instance) (string, error) {
	inst.ID = "cli-" + config.NewID()
	c, rep := a.open(ctx, inst)
	if c == nil {
		var lines []string
		for _, s := range rep.Steps {
			if s.Status == diag.Fail {
				lines = append(lines, s.Name+": "+s.Detail)
			}
		}
		return "", fmt.Errorf("%s\n  %s", i18n.T("err.connectFailed"), strings.Join(lines, "\n  "))
	}
	a.mu.Lock()
	a.conns[inst.ID] = c
	a.mu.Unlock()
	return inst.ID, nil
}
