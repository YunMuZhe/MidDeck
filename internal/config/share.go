package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
	Salt       []byte     `json:"salt,omitempty"`
	Nonce      []byte     `json:"nonce,omitempty"`
	Ciphertext []byte     `json:"ciphertext,omitempty"`
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
	b := Bundle{Format: "middeck-bundle", Version: 1, ExportedAt: time.Now().UTC()}
	if passphrase == "" {
		for _, in := range list {
			b.Instances = append(b.Instances, stripSecrets(in))
		}
		return json.MarshalIndent(b, "", "  ")
	}
	plain, err := json.Marshal(list)
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

// DecodeBundle parses a bundle and returns its instances (decrypting if needed).
func DecodeBundle(data []byte, passphrase string) ([]Instance, error) {
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("not a MidDeck bundle: %w", err)
	}
	if b.Format != "middeck-bundle" {
		return nil, errors.New("not a MidDeck bundle")
	}
	if !b.Encrypted {
		return b.Instances, nil
	}
	if passphrase == "" {
		return nil, errors.New("this bundle is encrypted, a passphrase is required")
	}
	gcm, err := newGCM(passphrase, b.Salt)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, b.Nonce, b.Ciphertext, []byte(b.Format))
	if err != nil {
		return nil, errors.New("wrong passphrase or corrupted bundle")
	}
	var list []Instance
	if err := json.Unmarshal(plain, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// Import adds bundle instances to the store.
func (s *Store) Import(data []byte, passphrase string, policy ConflictPolicy) (ImportResult, error) {
	var res ImportResult
	list, err := DecodeBundle(data, passphrase)
	if err != nil {
		return res, err
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
			in.Name += " (imported)"
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
