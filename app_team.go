package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/i18n"
	"github.com/YunMuZhe/MidDeck/internal/team"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// --- team repository ------------------------------------------------------------

type teamConfig struct {
	Repo string `json:"repo"`
}

func (a *App) teamConfigPath() string { return filepath.Join(a.dataDir, "team.json") }

// team returns the configured team repository, if any.
func (a *App) team() (team.Repo, bool) {
	var c teamConfig
	b, err := os.ReadFile(a.teamConfigPath())
	if err != nil || json.Unmarshal(b, &c) != nil || c.Repo == "" {
		return team.Repo{}, false
	}
	return team.Repo{Dir: c.Repo}, true
}

func (a *App) needTeam() (team.Repo, error) {
	r, ok := a.team()
	if !ok {
		return r, errors.New(i18n.T("team.notConfigured"))
	}
	return r, nil
}

func (a *App) gitCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.ctx, 60*time.Second)
}

// TeamStatus is what the Team panel shows.
type TeamStatus struct {
	Configured bool   `json:"configured"`
	Repo       string `json:"repo"`
	team.Status
	User  team.Person `json:"user"`
	Error string      `json:"error,omitempty"`
}

func (a *App) TeamStatus() TeamStatus {
	r, ok := a.team()
	if !ok {
		return TeamStatus{}
	}
	ctx, cancel := a.gitCtx()
	defer cancel()
	st := TeamStatus{Configured: true, Repo: r.Dir}
	if err := r.Check(ctx); err != nil {
		st.Error = err.Error()
		return st
	}
	var err error
	if st.Status, err = r.Status(ctx); err != nil {
		st.Error = err.Error()
	}
	st.User = r.Identity(ctx)
	return st
}

// TeamPickRepo asks for the local clone of the team repository.
func (a *App) TeamPickRepo() (TeamStatus, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: i18n.T("dlg.teamRepo")})
	if err != nil || dir == "" {
		return a.TeamStatus(), err
	}
	return a.TeamSetRepo(dir)
}

// TeamSetRepo connects a local clone; an empty path disconnects.
func (a *App) TeamSetRepo(dir string) (TeamStatus, error) {
	dir = strings.TrimSpace(dir)
	if dir != "" {
		ctx, cancel := a.gitCtx()
		defer cancel()
		if err := (team.Repo{Dir: dir}).Check(ctx); err != nil {
			return a.TeamStatus(), err
		}
	}
	b, _ := json.MarshalIndent(teamConfig{Repo: dir}, "", "  ")
	if err := os.WriteFile(a.teamConfigPath(), b, 0o600); err != nil {
		return a.TeamStatus(), err
	}
	return a.TeamStatus(), nil
}

// TeamSync pulls, then pushes local commits.
func (a *App) TeamSync() (TeamStatus, error) {
	r, err := a.needTeam()
	if err != nil {
		return TeamStatus{}, err
	}
	ctx, cancel := a.gitCtx()
	defer cancel()
	if err := r.Pull(ctx); err != nil {
		return a.TeamStatus(), err
	}
	err = r.Push(ctx)
	return a.TeamStatus(), err
}

// --- shared connection catalog ----------------------------------------------------

// CatalogEntry compares one connection between this machine and the team catalog.
type CatalogEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Project string `json:"project"`
	Env     string `json:"env"`
	// Status: same | changed | teamOnly | localOnly
	Status string `json:"status"`
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func (a *App) TeamCatalog() ([]CatalogEntry, error) {
	r, err := a.needTeam()
	if err != nil {
		return nil, err
	}
	cat, err := r.Catalog()
	if err != nil {
		return nil, err
	}
	shared := map[string]config.Instance{}
	for _, in := range cat.Instances {
		shared[in.ID] = in
	}
	out := []CatalogEntry{}
	for _, in := range a.store.List() {
		e := CatalogEntry{ID: in.ID, Name: in.Name, Kind: string(in.Kind), Project: in.Project, Env: in.Env, Status: "localOnly"}
		if s, ok := shared[in.ID]; ok {
			e.Status = "changed"
			if sameJSON(in, s) {
				e.Status = "same"
			}
			delete(shared, in.ID)
		}
		out = append(out, e)
	}
	for _, in := range shared {
		out = append(out, CatalogEntry{ID: in.ID, Name: in.Name, Kind: string(in.Kind), Project: in.Project, Env: in.Env, Status: "teamOnly"})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Project != out[j].Project {
			return out[i].Project < out[j].Project
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// TeamPublishCatalog makes the catalog hold exactly the given local connections,
// plus whatever teammates published that this machine does not have. Secrets
// never leave the keychain.
func (a *App) TeamPublishCatalog(ids []string) error {
	r, err := a.needTeam()
	if err != nil {
		return err
	}
	ctx, cancel := a.gitCtx()
	defer cancel()
	if err := r.Pull(ctx); err != nil {
		return err
	}
	old, err := r.Catalog()
	if err != nil {
		return err
	}
	local := map[string]config.Instance{}
	for _, in := range a.store.List() { // List strips secrets
		local[in.ID] = in
	}
	next := team.Catalog{}
	bastions := map[string]config.Bastion{}
	for _, b := range old.Bastions {
		bastions[b.ID] = b
	}
	for _, in := range old.Instances {
		if _, mine := local[in.ID]; !mine {
			next.Instances = append(next.Instances, in)
		}
	}
	for _, id := range ids {
		in, ok := local[id]
		if !ok {
			return fmt.Errorf("instance %q not found", id)
		}
		next.Instances = append(next.Instances, in)
		for _, h := range in.Network.SSH {
			if h.BastionID == "" {
				continue
			}
			b, err := a.store.GetBastion(h.BastionID)
			if err != nil {
				return err
			}
			b.Secret = ""
			bastions[b.ID] = b
		}
	}
	// Keep only jump hosts something in the catalog still uses.
	used := map[string]bool{}
	for _, in := range next.Instances {
		for _, h := range in.Network.SSH {
			used[h.BastionID] = true
		}
	}
	for id, b := range bastions {
		if used[id] {
			next.Bastions = append(next.Bastions, b)
		}
	}
	path, err := r.WriteCatalog(next)
	if err != nil {
		return err
	}
	return r.Save(ctx, fmt.Sprintf("catalog: %d connections", len(next.Instances)), path)
}

// TeamImportCatalog copies catalog connections to this machine (all when ids is
// empty). Existing ones are updated; passwords already saved here are kept.
func (a *App) TeamImportCatalog(ids []string) (config.ImportResult, error) {
	r, err := a.needTeam()
	if err != nil {
		return config.ImportResult{}, err
	}
	cat, err := r.Catalog()
	if err != nil {
		return config.ImportResult{}, err
	}
	return a.importCatalog(cat, ids)
}

// importCatalog adds or updates catalog connections (all when ids is empty) and
// the jump hosts they use. Passwords saved here are kept.
func (a *App) importCatalog(cat team.Catalog, ids []string) (config.ImportResult, error) {
	var res config.ImportResult
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	bastions := map[string]config.Bastion{}
	for _, b := range cat.Bastions {
		bastions[b.ID] = b
	}
	for _, in := range cat.Instances {
		if len(want) > 0 && !want[in.ID] {
			continue
		}
		for _, h := range in.Network.SSH {
			if b, ok := bastions[h.BastionID]; ok {
				if _, err := a.store.SaveBastion(b); err != nil { // an empty secret keeps the saved one
					return res, err
				}
				delete(bastions, h.BastionID)
			}
		}
		if old, err := a.store.Get(in.ID); err == nil {
			if sameJSON(a.strip(old), in) {
				res.Skipped++
				continue
			}
			fillSecrets(&in, old)
			res.Overwritten++
		} else {
			res.Added++
		}
		if _, err := a.SaveInstance(in); err != nil {
			return res, err
		}
	}
	return res, nil
}

// strip returns the instance as List would: without secrets.
func (a *App) strip(in config.Instance) config.Instance {
	in.Auth.Password, in.Network.Proxy.Secret, in.Registry.Password = "", "", ""
	ssh := make([]config.SSHHop, len(in.Network.SSH))
	copy(ssh, in.Network.SSH)
	for i := range ssh {
		ssh[i].Secret = ""
	}
	if in.Network.SSH != nil {
		in.Network.SSH = ssh
	}
	return in
}
