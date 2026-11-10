package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/YunMuZhe/MidDeck/internal/config"
)

const (
	catalogPath   = "middeck/catalog.json"
	catalogFormat = "middeck-catalog"
)

// --- catalog ------------------------------------------------------------------

// Catalog is the shared connection list. It never contains secrets.
type Catalog struct {
	Format    string            `json:"format"`
	Version   int               `json:"version"`
	Instances []config.Instance `json:"instances"`
	Bastions  []config.Bastion  `json:"bastions,omitempty"`
}

// Catalog reads middeck/catalog.json; a missing file is an empty catalog.
func (r Repo) Catalog() (Catalog, error) {
	c := Catalog{Format: catalogFormat, Version: 1}
	b, err := os.ReadFile(r.path(catalogPath))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s: %w", catalogPath, err)
	}
	if c.Format != catalogFormat {
		return c, fmt.Errorf("%s is not a MidDeck catalog", catalogPath)
	}
	return c, nil
}

// WriteCatalog stores the catalog in a stable order (so diffs stay small) and
// returns the path to commit. Secrets are dropped again here as a last line of defence.
func (r Repo) WriteCatalog(c Catalog) (string, error) {
	c.Format, c.Version = catalogFormat, 1
	for i := range c.Instances {
		in := &c.Instances[i]
		in.Auth.Password, in.Network.Proxy.Secret, in.Registry.Password = "", "", ""
		for j := range in.Network.SSH {
			in.Network.SSH[j].Secret = ""
		}
	}
	for i := range c.Bastions {
		c.Bastions[i].Secret = ""
	}
	sort.Slice(c.Instances, func(i, j int) bool {
		a, b := c.Instances[i], c.Instances[j]
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	sort.Slice(c.Bastions, func(i, j int) bool { return c.Bastions[i].ID < c.Bastions[j].ID })
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	return catalogPath, writeFile(r.path(catalogPath), append(b, '\n'))
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
