package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/compat"
	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/diag"
	"github.com/YunMuZhe/MidDeck/internal/driver"
	"github.com/YunMuZhe/MidDeck/internal/driver/kafka"
	"github.com/YunMuZhe/MidDeck/internal/driver/rabbitmq"
	"github.com/YunMuZhe/MidDeck/internal/driver/redis"
	"github.com/YunMuZhe/MidDeck/internal/i18n"
	"github.com/YunMuZhe/MidDeck/internal/tunnel"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// version is set at build time: -ldflags "-X main.version=v0.1.0"
var version = "dev"

// App is bound to the frontend; every exported method becomes a JS function.
type App struct {
	ctx     context.Context
	dataDir string
	store   *config.Store

	mu    sync.Mutex
	conns map[string]*conn
}

type conn struct {
	inst    config.Instance
	dialer  *tunnel.Dialer
	session driver.Session
	info    driver.ServerInfo
}

func NewApp() *App { return &App{conns: map[string]*conn{}} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	dir, err := config.DefaultDir()
	if err == nil {
		a.dataDir = dir
		a.store, err = config.Open(dir)
	}
	if err != nil {
		runtime.LogErrorf(ctx, "open config store: %v", err)
	}
}

func (a *App) shutdown(context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, c := range a.conns {
		c.session.Close()
		c.dialer.Close()
		delete(a.conns, id)
	}
}

// Version returns the app version.
func (a *App) Version() string { return version }

// --- instances ----------------------------------------------------------------

func (a *App) ListInstances() ([]config.Instance, error) {
	if a.store == nil {
		return nil, errors.New(i18n.T("err.storeUnavailable"))
	}
	return a.store.List(), nil
}

func (a *App) GetInstance(id string) (config.Instance, error) { return a.store.Get(id) }

func (a *App) SaveInstance(in config.Instance) (config.Instance, error) {
	saved, err := a.store.Save(in)
	if err == nil {
		a.Disconnect(saved.ID) // settings changed: reconnect lazily
	}
	return saved, err
}

func (a *App) DeleteInstance(id string) error {
	a.Disconnect(id)
	return a.store.Delete(id)
}

// ExportInstances writes a bundle file chosen by the user. Returns the path ("" if cancelled).
func (a *App) ExportInstances(ids []string, passphrase string) (string, error) {
	data, err := a.store.Export(ids, passphrase)
	if err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           i18n.T("dlg.exportTitle"),
		DefaultFilename: fmt.Sprintf("middeck-%s.json", time.Now().Format("20060102")),
		Filters:         []runtime.FileFilter{{DisplayName: i18n.T("dlg.bundle"), Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o600)
}

// ImportInstances reads a bundle chosen by the user.
func (a *App) ImportInstances(passphrase string, policy string) (config.ImportResult, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   i18n.T("dlg.importTitle"),
		Filters: []runtime.FileFilter{{DisplayName: i18n.T("dlg.bundle"), Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return config.ImportResult{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return config.ImportResult{}, err
	}
	return a.store.Import(data, passphrase, config.ConflictPolicy(policy))
}

// --- connections --------------------------------------------------------------

func (a *App) open(ctx context.Context, inst config.Instance) (*conn, *diag.Report) {
	rep := &diag.Report{}
	d := tunnel.New(inst.Network, filepath.Join(a.dataDir, "known_hosts"))
	if err := d.Connect(ctx, rep); err != nil {
		return nil, rep.Finish()
	}
	if d.Direct() {
		rep.Add(diag.Step{Name: i18n.T("step.network"), Status: diag.OK, Detail: i18n.T("step.direct")})
	}
	var (
		s   driver.Session
		err error
	)
	switch inst.Kind {
	case config.KindKafka:
		s, err = openAs(kafka.Open(ctx, inst, d.DialContext, rep))
	case config.KindRedis:
		s, err = openAs(redis.Open(ctx, inst, d.DialContext, rep))
	case config.KindRabbitMQ:
		s, err = openAs(rabbitmq.Open(ctx, inst, d.DialContext, rep))
	default:
		err = errors.New(i18n.T("err.unknownKind", inst.Kind))
		rep.Add(diag.Step{Name: i18n.T("step.config"), Status: diag.Fail, Detail: err.Error()})
	}
	if err != nil {
		d.Close()
		return nil, rep.Finish()
	}
	c := &conn{inst: inst, dialer: d, session: s}
	if info, err := s.Info(ctx); err == nil {
		c.info = info
		st := diag.Step{Name: i18n.T("step.compat"), Status: diag.OK, Detail: info.Compat.Message}
		switch info.Compat.Level {
		case compat.Partial, compat.Unknown:
			st.Status = diag.Warn
		case compat.Unsupported:
			st.Status = diag.Warn
			st.Hint = i18n.T("compat.tooOldHint")
		}
		if len(info.Compat.Unavailable) > 0 {
			st.Detail += i18n.T("compat.unavailable", strings.Join(info.Compat.Unavailable, ", "))
		}
		rep.Add(st)
	}
	return c, rep.Finish()
}

// openAs adapts a concrete (*T, error) into (driver.Session, error) without a typed-nil interface.
func openAs[T driver.Session](s T, err error) (driver.Session, error) {
	if err != nil {
		return nil, err
	}
	return s, nil
}

// TestConnection runs layered diagnostics for an (unsaved) instance form.
// Empty secrets fall back to the stored ones so editing doesn't require retyping.
func (a *App) TestConnection(inst config.Instance) *diag.Report {
	if inst.ID != "" {
		if stored, err := a.store.Get(inst.ID); err == nil {
			fillSecrets(&inst, stored)
		}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
	defer cancel()
	c, rep := a.open(ctx, inst)
	if c != nil {
		c.session.Close()
		c.dialer.Close()
	}
	return rep
}

func fillSecrets(dst *config.Instance, src config.Instance) {
	if dst.Auth.Password == "" {
		dst.Auth.Password = src.Auth.Password
	}
	if dst.Network.Proxy.Secret == "" {
		dst.Network.Proxy.Secret = src.Network.Proxy.Secret
	}
	for i := range dst.Network.SSH {
		if dst.Network.SSH[i].Secret == "" && i < len(src.Network.SSH) {
			dst.Network.SSH[i].Secret = src.Network.SSH[i].Secret
		}
	}
}

// ConnectResult is returned by Connect.
type ConnectResult struct {
	Info   driver.ServerInfo `json:"info"`
	Report *diag.Report      `json:"report"`
}

// Connect opens (or reuses) a session for a saved instance.
func (a *App) Connect(id string) (ConnectResult, error) {
	a.mu.Lock()
	if c, ok := a.conns[id]; ok {
		a.mu.Unlock()
		return ConnectResult{Info: c.info}, nil
	}
	a.mu.Unlock()
	inst, err := a.store.Get(id)
	if err != nil {
		return ConnectResult{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
	defer cancel()
	c, rep := a.open(ctx, inst)
	if c == nil {
		return ConnectResult{Report: rep}, errors.New(i18n.T("err.connectFailed"))
	}
	a.mu.Lock()
	a.conns[id] = c
	a.mu.Unlock()
	return ConnectResult{Info: c.info, Report: rep}, nil
}

func (a *App) Disconnect(id string) {
	a.mu.Lock()
	c, ok := a.conns[id]
	delete(a.conns, id)
	a.mu.Unlock()
	if ok {
		c.session.Close()
		c.dialer.Close()
	}
}

func (a *App) get(id string) (*conn, error) {
	a.mu.Lock()
	c, ok := a.conns[id]
	a.mu.Unlock()
	if ok {
		return c, nil
	}
	if _, err := a.Connect(id); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.conns[id], nil
}

func sessionAs[T any](a *App, id string) (T, *conn, error) {
	var zero T
	c, err := a.get(id)
	if err != nil {
		return zero, nil, err
	}
	s, ok := c.session.(T)
	if !ok {
		return zero, nil, errors.New(i18n.T("err.wrongKind", c.inst.Name))
	}
	return s, c, nil
}

func (a *App) reqCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.ctx, 60*time.Second)
}

// write guards mutating operations: read-only instances are refused, and every
// attempt is written to the audit log.
func (a *App) write(c *conn, action string, detail any, fn func() error) error {
	if c.inst.ReadOnly {
		return errors.New(i18n.T("err.readOnly", c.inst.Name, action))
	}
	err := fn()
	a.audit(c.inst, action, detail, err)
	return err
}

// --- audit log ----------------------------------------------------------------

type AuditEntry struct {
	Time     time.Time `json:"time"`
	Instance string    `json:"instance"`
	Name     string    `json:"name"`
	Env      string    `json:"env"`
	Action   string    `json:"action"`
	Detail   any       `json:"detail"`
	Error    string    `json:"error,omitempty"`
}

func (a *App) audit(inst config.Instance, action string, detail any, err error) {
	e := AuditEntry{Time: time.Now(), Instance: inst.ID, Name: inst.Name, Env: inst.Env, Action: action, Detail: detail}
	if err != nil {
		e.Error = err.Error()
	}
	b, _ := json.Marshal(e)
	f, ferr := os.OpenFile(filepath.Join(a.dataDir, "audit.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if ferr != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

// AuditLog returns the most recent n entries, newest first.
func (a *App) AuditLog(n int) ([]AuditEntry, error) {
	data, err := os.ReadFile(filepath.Join(a.dataDir, "audit.log"))
	if errors.Is(err, os.ErrNotExist) {
		return []AuditEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []AuditEntry
	lines := splitLines(data)
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		var e AuditEntry
		if json.Unmarshal(lines[i], &e) == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

// CompatMatrix exposes the support matrix (with labels in the current language).
func (a *App) CompatMatrix() map[string]compat.Matrix { return compat.Localized() }

// SetLanguage switches backend messages between "en" and "zh".
func (a *App) SetLanguage(lang string) { i18n.SetLang(lang) }

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				out = append(out, b[start:i])
			}
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}
