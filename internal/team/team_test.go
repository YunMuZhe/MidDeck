package team

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YunMuZhe/MidDeck/internal/config"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// clones makes a bare remote and one working copy per person.
func clones(t *testing.T, people ...string) []Repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	run(t, root, "init", "-q", "--bare", "-b", "main", remote)
	var out []Repo
	for i, p := range people {
		dir := filepath.Join(root, p)
		run(t, root, "clone", "-q", remote, dir)
		run(t, dir, "config", "user.name", p)
		run(t, dir, "config", "user.email", p+"@example.com")
		run(t, dir, "config", "commit.gpgsign", "false")
		if i == 0 {
			run(t, dir, "checkout", "-q", "-b", "main")
			run(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
			run(t, dir, "push", "-q", "-u", "origin", "main")
		}
		out = append(out, Repo{Dir: dir})
	}
	return out
}

func TestShareThroughGit(t *testing.T) {
	rs := clones(t, "alice", "bob")
	alice, bob := rs[0], rs[1]
	ctx := context.Background()

	path, err := alice.WriteCatalog(Catalog{Instances: []config.Instance{{ID: "a", Name: "Orders", Kind: config.KindKafka, Env: "prod"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := alice.Save(ctx, "share", path); err != nil {
		t.Fatal(err)
	}
	if err := bob.Pull(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := bob.Catalog()
	if err != nil || len(c.Instances) != 1 || c.Instances[0].Name != "Orders" {
		t.Fatalf("bob must see the catalog: %+v %v", c, err)
	}
}

func TestCatalog(t *testing.T) {
	r := Repo{Dir: t.TempDir()}
	in := config.Instance{ID: "a", Name: "Orders", Kind: config.KindKafka, Env: "prod"}
	in.Auth.Password = "hunter2"
	path, err := r.WriteCatalog(Catalog{Instances: []config.Instance{in}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "hunter2") {
		t.Fatal("the catalog must not contain secrets")
	}
	c, err := r.Catalog()
	if err != nil || len(c.Instances) != 1 || c.Instances[0].Name != "Orders" {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestRemoteAndRedact(t *testing.T) {
	for src, want := range map[string]bool{
		"https://gitlab.example.com/ops/middeck.git":  true,
		"git@gitlab.example.com:ops/middeck.git":      true,
		"ssh://git@gitlab.example.com:2222/ops/x.git": true,
		"file:///srv/cfg.git":                         true,
		"/srv/middeck-config":                         false,
		"./cfg":                                       false,
	} {
		if IsRemote(src) != want {
			t.Errorf("IsRemote(%q) != %v", src, want)
		}
	}
	got := Redact("fatal: could not read from https://oauth2:glpat-SECRET@gitlab.example.com/ops/x.git")
	if strings.Contains(got, "SECRET") || !strings.Contains(got, "https://***@gitlab.example.com") {
		t.Fatal(got)
	}
}
