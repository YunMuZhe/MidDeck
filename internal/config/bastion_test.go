package config

import (
	"strings"
	"testing"
)

func TestBastionResolveAndInUse(t *testing.T) {
	s := newTestStore(t)
	b, err := s.SaveBastion(Bastion{Name: "prod-jump", SSHHop: SSHHop{Host: "j1", Port: 22, User: "ops", AuthType: "password", Secret: "pw"}})
	if err != nil {
		t.Fatal(err)
	}
	in := sample()
	in.Network.SSH = []SSHHop{{BastionID: b.ID}, {Host: "inner", User: "root", AuthType: "agent"}}
	saved, err := s.Save(in)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(saved.ID)
	if err := s.Resolve(&got); err != nil {
		t.Fatal(err)
	}
	if h := got.Network.SSH[0]; h.Host != "j1" || h.Secret != "pw" || h.BastionID != b.ID {
		t.Fatalf("resolved hop: %+v", h)
	}
	if got.Network.SSH[1].Host != "inner" {
		t.Fatal("inline hop changed")
	}

	// Editing the bastion without retyping the password keeps it, and every user sees the change.
	b.Host = "j2"
	if _, err := s.SaveBastion(b); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(saved.ID)
	_ = s.Resolve(&got)
	if got.Network.SSH[0].Host != "j2" || got.Network.SSH[0].Secret != "pw" {
		t.Fatalf("after edit: %+v", got.Network.SSH[0])
	}

	list := s.ListBastions()
	if len(list) != 1 || !list[0].HasSecret || list[0].Secret != "" || len(list[0].UsedBy) != 1 {
		t.Fatalf("list: %+v", list)
	}
	if err := s.DeleteBastion(b.ID); err == nil || !strings.Contains(err.Error(), "orders") {
		t.Fatalf("delete in-use bastion: %v", err)
	}
	if err := s.Delete(saved.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBastion(b.ID); err != nil {
		t.Fatal(err)
	}
	got = sample()
	got.Network.SSH = []SSHHop{{BastionID: b.ID}}
	if err := s.Resolve(&got); err == nil {
		t.Fatal("dangling reference resolved")
	}
}

func TestBundleCarriesBastions(t *testing.T) {
	src := newTestStore(t)
	b, _ := src.SaveBastion(Bastion{Name: "jump", SSHHop: SSHHop{Host: "j1", User: "ops", AuthType: "password", Secret: "pw"}})
	in := sample()
	in.Network.SSH = []SSHHop{{BastionID: b.ID}}
	if _, err := src.Save(in); err != nil {
		t.Fatal(err)
	}
	for _, pass := range []string{"", "correct horse"} {
		data, err := src.Export(nil, pass)
		if err != nil {
			t.Fatal(err)
		}
		if pass == "" && strings.Contains(string(data), `"pw"`) {
			t.Fatal("plain export leaked bastion secret")
		}
		dst := newTestStore(t)
		if _, err := dst.Import(data, pass, ConflictSkip); err != nil {
			t.Fatal(err)
		}
		got, err := dst.GetBastion(b.ID)
		if err != nil || got.Host != "j1" {
			t.Fatalf("bastion not imported: %+v %v", got, err)
		}
		if wantSecret := pass != ""; (got.Secret == "pw") != wantSecret {
			t.Fatalf("pass=%q secret=%q", pass, got.Secret)
		}
		inst := dst.List()[0]
		if err := dst.Resolve(&inst); err != nil {
			t.Fatal(err)
		}
	}
}
