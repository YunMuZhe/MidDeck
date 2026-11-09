package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/i18n"

	"golang.org/x/crypto/scrypt"
)

// Bundle is the export/import file format used to share instances between colleagues.
//
// Without a passphrase, secrets are stripped and Instances is plain JSON.
// With a passphrase, the full instance list (secrets included) is encrypted with
// AES-256-GCM using a scrypt-derived key and stored in Ciphertext.
type Bundle struct {
	Format     string     `json:"format"` // "middeck-bundle"
	Version    int        `json:"version"`
	ExportedAt time.Time  `json:"exportedAt"`
	Encrypted  bool       `json:"encrypted"`
	Instances  []Instance `json:"instances,omitempty"`
	// Bastions referenced by the exported instances (version 2+).
	Bastions   []Bastion `json:"bastions,omitempty"`
	Salt       []byte    `json:"salt,omitempty"`
	Nonce      []byte    `json:"nonce,omitempty"`
	Ciphertext []byte    `json:"ciphertext,omitempty"`
}

// payload is the encrypted part of a version 2 bundle. Version 1 encrypted a bare []Instance.
type payload struct {
	Instances []Instance `json:"instances"`
	Bastions  []Bastion  `json:"bastions,omitempty"`
}

// ConflictPolicy decides what happens when an imported instance's ID already exists.
type ConflictPolicy string

const (
	ConflictSkip      ConflictPolicy = "skip"
	ConflictOverwrite ConflictPolicy = "overwrite"
	ConflictCopy      ConflictPolicy = "copy" // import as a new instance
)

// ImportResult summarises an import.
type ImportResult struct {
	Added       int `json:"added"`
	Overwritten int `json:"overwritten"`
	Skipped     int `json:"skipped"`
}

// Export serialises the given instances (all when ids is empty).
func (s *Store) Export(ids []string, passphrase string) ([]byte, error) {
	var list []Instance
	if len(ids) == 0 {
		for _, in := range s.List() {
			ids = append(ids, in.ID)
		}
	}
	for _, id := range ids {
		in, err := s.Get(id)
		if err != nil {
			return nil, err
		}
		list = append(list, in)
	}
	var bastions []Bastion
	seen := map[string]bool{}
	for _, in := range list {
		for _, h := range in.Network.SSH {
			if h.BastionID == "" || seen[h.BastionID] {
				continue
			}
			seen[h.BastionID] = true
			bs, err := s.GetBastion(h.BastionID)
			if err != nil {
				return nil, err
			}
			bastions = append(bastions, bs)
		}
	}
	b := Bundle{Format: "middeck-bundle", Version: 2, ExportedAt: time.Now().UTC()}
	if passphrase == "" {
		for _, in := range list {
			b.Instances = append(b.Instances, stripSecrets(in))
		}
		for _, bs := range bastions {
			bs.Secret = ""
			b.Bastions = append(b.Bastions, bs)
		}
		return json.MarshalIndent(b, "", "  ")
	}
	plain, err := json.Marshal(payload{Instances: list, Bastions: bastions})
	if err != nil {
		return nil, err
	}
	b.Encrypted = true
	b.Salt = make([]byte, 16)
	if _, err := rand.Read(b.Salt); err != nil {
		return nil, err
	}
	gcm, err := newGCM(passphrase, b.Salt)
	if err != nil {
		return nil, err
	}
	b.Nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(b.Nonce); err != nil {
		return nil, err
	}
	b.Ciphertext = gcm.Seal(nil, b.Nonce, plain, []byte(b.Format))
	return json.MarshalIndent(b, "", "  ")
}

// DecodeBundle parses a bundle and returns its instances and bastions (decrypting if needed).
func DecodeBundle(data []byte, passphrase string) ([]Instance, []Bastion, error) {
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", i18n.T("share.notBundle"), err)
	}
	if b.Format != "middeck-bundle" {
		return nil, nil, errors.New(i18n.T("share.notBundle"))
	}
	if !b.Encrypted {
		return b.Instances, b.Bastions, nil
	}
	if passphrase == "" {
		return nil, nil, errors.New(i18n.T("share.needPass"))
	}
	gcm, err := newGCM(passphrase, b.Salt)
	if err != nil {
		return nil, nil, err
	}
	plain, err := gcm.Open(nil, b.Nonce, b.Ciphertext, []byte(b.Format))
	if err != nil {
		return nil, nil, errors.New(i18n.T("share.wrongPass"))
	}
	if b.Version < 2 {
		var list []Instance
		err := json.Unmarshal(plain, &list)
		return list, nil, err
	}
	var p payload
	if err := json.Unmarshal(plain, &p); err != nil {
		return nil, nil, err
	}
	return p.Instances, p.Bastions, nil
}

// Import adds bundle instances to the store.
func (s *Store) Import(data []byte, passphrase string, policy ConflictPolicy) (ImportResult, error) {
	var res ImportResult
	list, bastions, err := DecodeBundle(data, passphrase)
	if err != nil {
		return res, err
	}
	// Shared jump hosts first so instances can reference them. Existing ones are kept
	// unless the user chose to overwrite.
	for _, bs := range bastions {
		if _, err := s.GetBastion(bs.ID); err == nil && policy != ConflictOverwrite {
			continue
		}
		s.mu.Lock()
		delete(s.bastions, bs.ID) // so an empty secret isn't merged with a stale one
		s.mu.Unlock()
		if _, err := s.SaveBastion(bs); err != nil {
			return res, err
		}
	}
	for _, in := range list {
		_, err := s.Get(in.ID)
		exists := err == nil
		switch {
		case !exists:
			res.Added++
		case policy == ConflictOverwrite:
			res.Overwritten++
		case policy == ConflictCopy:
			in.ID = ""
			in.Name += i18n.T("share.imported")
			res.Added++
		default:
			res.Skipped++
			continue
		}
		if _, err := s.Save(in); err != nil {
			return res, err
		}
	}
	return res, nil
}

func newGCM(passphrase string, salt []byte) (cipher.AEAD, error) {
	key, err := scrypt.Key([]byte(passphrase), salt, 1<<15, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
