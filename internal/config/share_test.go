package config

import (
	"strings"
	"testing"
)

// newTestStore returns a store that keeps secrets in its file, so tests never
// touch the developer's real OS keychain.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.fileSecrets = true
	return s
}

func sample() Instance {
	return Instance{
		Name: "orders", Kind: KindKafka, Env: "prod", Addrs: []string{"k1:9092"},
		Auth:    Auth{Mechanism: "scram-sha-512", Username: "u", Password: "s3cret"},
		Network: Network{SSH: []SSHHop{{Host: "jump", User: "ops", AuthType: "password", Secret: "hop-pass"}}},
	}
}

func TestExportWithoutPassphraseStripsSecrets(t *testing.T) {
	s := newTestStore(t)
	in, err := s.Save(sample())
	if err != nil {
		t.Fatal(err)
	}
	data, err := s.Export(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "s3cret") || strings.Contains(string(data), "hop-pass") {
		t.Fatal("plain export leaked secrets")
	}
	list, err := DecodeBundle(data, "")
	if err != nil || len(list) != 1 || list[0].ID != in.ID || list[0].Auth.Password != "" {
		t.Fatalf("decode: %+v %v", list, err)
	}
}

func TestEncryptedRoundTrip(t *testing.T) {
	src := newTestStore(t)
	if _, err := src.Save(sample()); err != nil {
		t.Fatal(err)
	}
	data, err := src.Export(nil, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "s3cret") || strings.Contains(string(data), "orders") {
		t.Fatal("encrypted export contains plaintext")
	}
	if _, err := DecodeBundle(data, "wrong"); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	if _, err := DecodeBundle(data, ""); err == nil {
		t.Fatal("missing passphrase accepted")
	}

	dst := newTestStore(t)
	res, err := dst.Import(data, "correct horse", ConflictSkip)
	if err != nil || res.Added != 1 {
		t.Fatalf("import: %+v %v", res, err)
	}
	got := dst.List()[0]
	full, err := dst.Get(got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.Auth.Password != "s3cret" || full.Network.SSH[0].Secret != "hop-pass" {
		t.Fatalf("secrets not restored: %+v", full)
	}
	if got.Auth.Password != "" {
		t.Fatal("List must not expose secrets")
	}

	// Re-import: skip, copy, overwrite.
	if res, _ := dst.Import(data, "correct horse", ConflictSkip); res.Skipped != 1 {
		t.Fatalf("skip: %+v", res)
	}
	if res, _ := dst.Import(data, "correct horse", ConflictCopy); res.Added != 1 || len(dst.List()) != 2 {
		t.Fatalf("copy: %+v", res)
	}
	if res, _ := dst.Import(data, "correct horse", ConflictOverwrite); res.Overwritten != 1 || len(dst.List()) != 2 {
		t.Fatalf("overwrite: %+v", res)
	}
}

func TestStorePersists(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	s.fileSecrets = true
	in, _ := s.Save(sample())
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.Get(in.ID)
	if err != nil || got.Name != "orders" || got.Auth.Password != "s3cret" {
		t.Fatalf("reopen: %+v %v", got, err)
	}
	if err := s2.Delete(in.ID); err != nil || len(s2.List()) != 0 {
		t.Fatal("delete failed")
	}
}
