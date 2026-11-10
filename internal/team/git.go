// Package team keeps the shared part of a MidDeck setup in a Git repository the
// whole team clones: the connection catalog, i.e. connections and jump hosts
// without secrets, in middeck/catalog.json.
//
// MidDeck shells out to the user's git, so authentication (SSH agent, credential
// helper) and identity (user.name / user.email) are whatever git already uses.
package team

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/YunMuZhe/MidDeck/internal/execx"
)

// Repo is a local clone.
type Repo struct{ Dir string }

// Person is a git identity.
type Person struct {
	Name  string `yaml:"name" json:"name"`
	Email string `yaml:"email" json:"email"`
}

func (p Person) String() string {
	if p.Name == "" {
		return p.Email
	}
	if p.Email == "" {
		return p.Name
	}
	return p.Name + " <" + p.Email + ">"
}

func (r Repo) git(ctx context.Context, args ...string) (string, error) {
	cmd := execx.Command(ctx, "git", append([]string{"-C", r.Dir}, args...)...)
	// Never wait for a password on a terminal nobody can see.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("git %s: %s", args[0], msg)
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// Check verifies Dir is the root of (or inside) a git work tree.
func (r Repo) Check(ctx context.Context) error {
	if st, err := os.Stat(r.Dir); err != nil || !st.IsDir() {
		return fmt.Errorf("%s is not a directory", r.Dir)
	}
	if _, err := r.git(ctx, "rev-parse", "--is-inside-work-tree"); err != nil {
		return fmt.Errorf("%s is not a Git repository (clone the team repository, or run git init there): %w", r.Dir, err)
	}
	return nil
}

// Identity is git's user.name / user.email for this repository.
func (r Repo) Identity(ctx context.Context) Person {
	name, _ := r.git(ctx, "config", "user.name")
	email, _ := r.git(ctx, "config", "user.email")
	return Person{Name: strings.TrimSpace(name), Email: strings.TrimSpace(email)}
}

// Status of the work tree relative to its upstream.
type Status struct {
	Branch   string `json:"branch"`
	Upstream string `json:"upstream"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	// Dirty counts changed or untracked files.
	Dirty int `json:"dirty"`
}

func (r Repo) Status(ctx context.Context) (Status, error) {
	var st Status
	out, err := r.git(ctx, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return st, err
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			st.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.upstream "):
			st.Upstream = strings.TrimPrefix(line, "# branch.upstream ")
		case strings.HasPrefix(line, "# branch.ab "):
			for _, f := range strings.Fields(strings.TrimPrefix(line, "# branch.ab ")) {
				n, _ := strconv.Atoi(f[1:])
				if f[0] == '+' {
					st.Ahead = n
				} else {
					st.Behind = n
				}
			}
		case line != "" && !strings.HasPrefix(line, "#"):
			st.Dirty++
		}
	}
	return st, nil
}

func (r Repo) upstream(ctx context.Context) bool {
	_, err := r.git(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	return err == nil
}

// Pull rebases local commits onto the upstream. Without an upstream it does nothing.
func (r Repo) Pull(ctx context.Context) error {
	if !r.upstream(ctx) {
		return nil
	}
	if _, err := r.git(ctx, "pull", "--rebase", "--autostash"); err != nil {
		_, _ = r.git(context.WithoutCancel(ctx), "rebase", "--abort")
		return err
	}
	return nil
}

// Commit records the given paths (relative to the repository). It reports false
// when they have no changes.
func (r Repo) Commit(ctx context.Context, msg string, paths ...string) (bool, error) {
	if len(paths) == 0 {
		return false, nil
	}
	if _, err := r.git(ctx, append([]string{"add", "-A", "--"}, paths...)...); err != nil {
		return false, err
	}
	if _, err := r.git(ctx, append([]string{"diff", "--cached", "--quiet", "--"}, paths...)...); err == nil {
		return false, nil
	}
	if id := r.Identity(ctx); id.Email == "" {
		return false, errors.New("git user.email is not set for this repository; run: git config user.email you@example.com")
	}
	_, err := r.git(ctx, append([]string{"commit", "-q", "-m", msg, "--"}, paths...)...)
	return err == nil, err
}

// Push publishes local commits; when the remote moved it pulls and tries once more.
func (r Repo) Push(ctx context.Context) error {
	if !r.upstream(ctx) {
		return nil
	}
	if _, err := r.git(ctx, "push", "-q"); err != nil {
		if perr := r.Pull(ctx); perr != nil {
			return perr
		}
		_, err = r.git(ctx, "push", "-q")
		return err
	}
	return nil
}

// Save commits the paths and pushes. A push failure keeps the commit.
func (r Repo) Save(ctx context.Context, msg string, paths ...string) error {
	if _, err := r.Commit(ctx, msg, paths...); err != nil {
		return err
	}
	return r.Push(ctx)
}

func (r Repo) path(rel ...string) string { return filepath.Join(append([]string{r.Dir}, rel...)...) }

// IsRemote reports whether src is a Git URL rather than a local folder.
func IsRemote(src string) bool {
	return strings.Contains(src, "://") || (strings.Contains(src, "@") && strings.Contains(src, ":") && !strings.HasPrefix(src, "/"))
}

// Redact hides credentials in a URL such as https://oauth2:TOKEN@gitlab.example.com/…
func Redact(s string) string { return userinfo.ReplaceAllString(s, "://***@") }

var userinfo = regexp.MustCompile(`://[^/@\s]+@`)

// Clone clones url into dir (branch may be empty for the default branch), or
// pulls when dir already holds a clone.
func Clone(ctx context.Context, url, branch, dir string) (Repo, error) {
	r := Repo{Dir: dir}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		if _, err := r.git(ctx, "remote", "set-url", "origin", url); err != nil {
			return r, errors.New(Redact(err.Error()))
		}
		if err := r.Pull(ctx); err != nil {
			return r, errors.New(Redact(err.Error()))
		}
		return r, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return r, err
	}
	args := []string{"clone", "--quiet"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	cmd := execx.Command(ctx, "git", append(args, url, dir)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(dir)
		return r, fmt.Errorf("git clone %s: %s", Redact(url), Redact(strings.TrimSpace(string(out))))
	}
	return r, nil
}

// Head is the short commit id of HEAD.
func (r Repo) Head(ctx context.Context) string {
	out, _ := r.git(ctx, "rev-parse", "--short", "HEAD")
	return strings.TrimSpace(out)
}
