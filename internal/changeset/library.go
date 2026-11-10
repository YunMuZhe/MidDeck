package changeset

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Library is a folder of changeset files on this machine: drafts the user saved
// and proposals AI tools wrote. Nothing in it is applied until a person plans
// and applies it.
type Library struct{ Dir string }

// File is one changeset in the library.
type File struct {
	Name     string    `json:"name"`
	Modified time.Time `json:"modified"`
	Size     int64     `json:"size"`
}

// ErrExists is returned by Write when the file exists and overwrite is false.
var ErrExists = errors.New("changeset already exists")

var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// CleanName validates a file name and adds .yaml when missing. Names are plain
// file names: no directories, so nothing can escape the folder.
func CleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
		name += ".yaml"
	}
	if !safeName.MatchString(name) {
		return "", fmt.Errorf("invalid changeset name %q: use letters, digits, dot, dash and underscore", name)
	}
	return name, nil
}

// Path is the absolute path of name, after CleanName.
func (l Library) Path(name string) (string, error) {
	name, err := CleanName(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(l.Dir, name), nil
}

// List returns the YAML files, newest first.
func (l Library) List() ([]File, error) {
	es, err := os.ReadDir(l.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []File{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []File{}
	for _, e := range es {
		if e.IsDir() || !(strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml")) {
			continue
		}
		if info, err := e.Info(); err == nil {
			out = append(out, File{Name: e.Name(), Modified: info.ModTime(), Size: info.Size()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}

func (l Library) Read(name string) (string, error) {
	p, err := l.Path(name)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	return string(b), err
}

// Write validates content as a changeset and stores it; it returns the path.
func (l Library) Write(name, content string, overwrite bool) (string, error) {
	if _, err := Parse([]byte(content)); err != nil {
		return "", err
	}
	p, err := l.Path(name)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(p); err == nil && !overwrite {
		return p, ErrExists
	}
	if err := os.MkdirAll(l.Dir, 0o755); err != nil {
		return "", err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return "", err
	}
	return p, os.Rename(tmp, p)
}

func (l Library) Delete(name string) error {
	p, err := l.Path(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}
