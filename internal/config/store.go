package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/zalando/go-keyring"
)

const keyringService = "MidDeck"

// Store persists instances to a JSON file in the user config dir.
// Secrets (passwords, passphrases) go to the OS keychain when available.
type Store struct {
	mu        sync.Mutex
	path      string
	instances map[string]*Instance
	bastions  map[string]*Bastion
	// fileSecrets is true when the keychain is unavailable and secrets fall back to the file.
	fileSecrets bool
}

type fileFormat struct {
	Version     int         `json:"version"`
	FileSecrets bool        `json:"fileSecrets,omitempty"`
	Instances   []*Instance `json:"instances"`
	Bastions    []*Bastion  `json:"bastions,omitempty"`
}

// FileSecrets makes new stores keep secrets in instances.json (mode 0600) instead
// of the OS keychain. The headless build sets it: servers rarely run a Secret
// Service. MIDDECK_SECRETS=file does the same for any build.
var FileSecrets = false

// DefaultDir returns the per-user data directory for MidDeck, or MIDDECK_DATA_DIR.
func DefaultDir() (string, error) {
	if d := os.Getenv("MIDDECK_DATA_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "MidDeck"), nil
}

// Open loads (or creates) the store in dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "instances.json"), instances: map[string]*Instance{}, bastions: map[string]*Bastion{}}
	s.fileSecrets = FileSecrets || os.Getenv("MIDDECK_SECRETS") == "file"
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f fileFormat
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path, err)
	}
	s.fileSecrets = s.fileSecrets || f.FileSecrets
	for _, in := range f.Instances {
		s.instances[in.ID] = in
	}
	for _, b := range f.Bastions {
		s.bastions[b.ID] = b
	}
	return s, nil
}

// List returns all instances without secrets, sorted by project/env/name.
func (s *Store) List() []Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Instance, 0, len(s.instances))
	for _, in := range s.instances {
		out = append(out, stripSecrets(*in))
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		if a.Env != b.Env {
			return a.Env < b.Env
		}
		return a.Name < b.Name
	})
	return out
}

// Get returns the instance including secrets loaded from the keychain.
func (s *Store) Get(id string) (Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in, ok := s.instances[id]
	if !ok {
		return Instance{}, fmt.Errorf("instance %q not found", id)
	}
	cp := clone(*in)
	if !s.fileSecrets {
		s.loadSecrets(&cp)
	}
	return cp, nil
}

// Save creates or updates an instance. An empty ID creates a new one.
func (s *Store) Save(in Instance) (Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.Name == "" {
		return Instance{}, errors.New("name is required")
	}
	if in.ID == "" {
		in.ID = NewID()
	}
	in = clone(in)
	if !s.fileSecrets {
		if err := s.storeSecrets(&in); err != nil {
			// Keychain unavailable (e.g. headless Linux): fall back to file storage.
			s.fileSecrets = true
		}
	}
	stored := in
	if !s.fileSecrets {
		stored = stripSecrets(in)
	}
	s.instances[in.ID] = &stored
	return stripSecrets(in), s.flush()
}

// Delete removes an instance and its secrets.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in, ok := s.instances[id]; ok && !s.fileSecrets {
		for _, k := range secretKeys(in) {
			_ = keyring.Delete(keyringService, k)
		}
	}
	delete(s.instances, id)
	return s.flush()
}

func (s *Store) flush() error {
	f := fileFormat{Version: 1, FileSecrets: s.fileSecrets}
	for _, in := range s.instances {
		f.Instances = append(f.Instances, in)
	}
	sort.Slice(f.Instances, func(i, j int) bool { return f.Instances[i].ID < f.Instances[j].ID })
	for _, b := range s.bastions {
		f.Bastions = append(f.Bastions, b)
	}
	sort.Slice(f.Bastions, func(i, j int) bool { return f.Bastions[i].ID < f.Bastions[j].ID })
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// --- secrets -------------------------------------------------------------

func secretKeys(in *Instance) []string {
	keys := []string{in.ID + "/auth", in.ID + "/proxy", in.ID + "/registry"}
	for i := range in.Network.SSH {
		keys = append(keys, fmt.Sprintf("%s/ssh/%d", in.ID, i))
	}
	return keys
}

func (s *Store) storeSecrets(in *Instance) error {
	set := func(key, val string) error {
		if val == "" {
			_ = keyring.Delete(keyringService, key)
			return nil
		}
		return keyring.Set(keyringService, key, val)
	}
	if err := set(in.ID+"/auth", in.Auth.Password); err != nil {
		return err
	}
	if err := set(in.ID+"/proxy", in.Network.Proxy.Secret); err != nil {
		return err
	}
	if err := set(in.ID+"/registry", in.Registry.Password); err != nil {
		return err
	}
	for i, h := range in.Network.SSH {
		if h.BastionID != "" {
			h.Secret = ""
		}
		if err := set(fmt.Sprintf("%s/ssh/%d", in.ID, i), h.Secret); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadSecrets(in *Instance) {
	get := func(key string) string {
		v, _ := keyring.Get(keyringService, key)
		return v
	}
	in.Auth.Password = get(in.ID + "/auth")
	in.Network.Proxy.Secret = get(in.ID + "/proxy")
	in.Registry.Password = get(in.ID + "/registry")
	for i := range in.Network.SSH {
		in.Network.SSH[i].Secret = get(fmt.Sprintf("%s/ssh/%d", in.ID, i))
	}
}

func stripSecrets(in Instance) Instance {
	in = clone(in)
	in.Auth.Password = ""
	in.Network.Proxy.Secret = ""
	in.Registry.Password = ""
	for i := range in.Network.SSH {
		in.Network.SSH[i].Secret = ""
	}
	return in
}

func clone(in Instance) Instance {
	b, _ := json.Marshal(in)
	var out Instance
	_ = json.Unmarshal(b, &out)
	return out
}

// NewID returns a random 12-hex-char identifier.
func NewID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
