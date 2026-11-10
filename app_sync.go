package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/team"
)

// Repo sync keeps this machine's connections in step with middeck/catalog.json in
// a Git repository (e.g. on a self-hosted GitLab), the same file the desktop app's
// Team panel publishes. It is meant for servers running "MidDeck mcp --repo …":
// connections come from the repository, passwords are set locally.
//
// Connections imported from the repository are remembered; when one disappears
// from the catalog it is removed here too. Connections created locally are never
// touched, and a repository connection edited locally is overwritten by the next
// sync (passwords excepted).

type repoSync struct {
	mu       sync.Mutex
	src      string // URL or local folder
	branch   string
	interval time.Duration
	last     SyncReport
}

// SyncReport is the outcome of one sync.
type SyncReport struct {
	Source    string    `json:"source"`
	Commit    string    `json:"commit,omitempty"`
	Time      time.Time `json:"time"`
	Added     int       `json:"added"`
	Updated   int       `json:"updated"`
	Unchanged int       `json:"unchanged"`
	Removed   int       `json:"removed"`
	Error     string    `json:"error,omitempty"`
}

type managedFile struct {
	Source string   `json:"source"`
	IDs    []string `json:"ids"`
}

func (a *App) managedPath() string { return filepath.Join(a.dataDir, "repo-managed.json") }

func (a *App) managedIDs() map[string]bool {
	var m managedFile
	b, err := os.ReadFile(a.managedPath())
	out := map[string]bool{}
	if err == nil && json.Unmarshal(b, &m) == nil {
		for _, id := range m.IDs {
			out[id] = true
		}
	}
	return out
}

// fromRepo reports whether a connection is maintained by the synced repository.
func (a *App) fromRepo(id string) bool { return a.sync != nil && a.managedIDs()[id] }

func (a *App) cloneDir(src string) string {
	sum := sha256.Sum256([]byte(team.Redact(src)))
	return filepath.Join(a.dataDir, "repo", hex.EncodeToString(sum[:6]))
}

// syncRepo pulls the repository and applies its catalog.
func (a *App) syncRepo(ctx context.Context) (SyncReport, error) {
	s := a.sync
	s.mu.Lock()
	defer s.mu.Unlock()
	rep := SyncReport{Source: team.Redact(s.src), Time: time.Now()}
	err := a.syncOnce(ctx, s, &rep)
	if err != nil {
		rep.Error = err.Error()
	}
	s.last = rep
	return rep, err
}

func (a *App) syncOnce(ctx context.Context, s *repoSync, rep *SyncReport) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var r team.Repo
	var err error
	if team.IsRemote(s.src) {
		r, err = team.Clone(ctx, s.src, s.branch, a.cloneDir(s.src))
	} else {
		r = team.Repo{Dir: s.src}
		if err = r.Check(ctx); err == nil {
			err = r.Pull(ctx)
		}
	}
	if err != nil {
		return err
	}
	rep.Commit = r.Head(ctx)
	cat, err := r.Catalog()
	if err != nil {
		return err
	}
	res, err := a.importCatalog(cat, nil)
	rep.Added, rep.Updated, rep.Unchanged = res.Added, res.Overwritten, res.Skipped
	if err != nil {
		return err
	}
	now := map[string]bool{}
	ids := []string{}
	for _, in := range cat.Instances {
		now[in.ID] = true
		ids = append(ids, in.ID)
	}
	for id := range a.managedIDs() {
		if now[id] {
			continue
		}
		if _, err := a.store.Get(id); err == nil {
			if err := a.DeleteInstance(id); err != nil {
				return err
			}
			rep.Removed++
		}
	}
	sort.Strings(ids)
	b, _ := json.MarshalIndent(managedFile{Source: team.Redact(s.src), IDs: ids}, "", "  ")
	return os.WriteFile(a.managedPath(), b, 0o600)
}

// startRepoSync syncs once (so the first tool call sees the connections) and then
// every interval until ctx ends. Problems go to stderr; stdout carries MCP.
func (a *App) startRepoSync(ctx context.Context, src, branch string, interval time.Duration) {
	a.sync = &repoSync{src: src, branch: branch, interval: interval}
	logf := func(rep SyncReport, err error) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "middeck: sync %s: %v\n", rep.Source, err)
			return
		}
		fmt.Fprintf(os.Stderr, "middeck: synced %s@%s: %d added, %d updated, %d removed\n", rep.Source, rep.Commit, rep.Added, rep.Updated, rep.Removed)
	}
	logf(a.syncRepo(ctx))
	if interval <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				logf(a.syncRepo(ctx))
			}
		}
	}()
}
