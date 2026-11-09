package config

import (
	"errors"
	"sort"
	"strings"

	"github.com/YunMuZhe/MidDeck/internal/i18n"
	"github.com/zalando/go-keyring"
)

func bastionKey(id string) string { return "bastion/" + id }

// ListBastions returns all shared jump hosts (without secrets) and the instances using each.
func (s *Store) ListBastions() []BastionView {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]BastionView, 0, len(s.bastions))
	for _, b := range s.bastions {
		v := BastionView{Bastion: *b, UsedBy: s.usersLocked(b.ID)}
		if s.fileSecrets {
			v.HasSecret = b.Secret != ""
		} else {
			sec, _ := keyring.Get(keyringService, bastionKey(b.ID))
			v.HasSecret = sec != ""
		}
		v.Secret = ""
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func (s *Store) usersLocked(id string) []string {
	users := []string{}
	for _, in := range s.instances {
		for _, h := range in.Network.SSH {
			if h.BastionID == id {
				users = append(users, in.Name)
				break
			}
		}
	}
	sort.Strings(users)
	return users
}

// GetBastion returns a bastion including its secret.
func (s *Store) GetBastion(id string) (Bastion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getBastionLocked(id)
}

func (s *Store) getBastionLocked(id string) (Bastion, error) {
	b, ok := s.bastions[id]
	if !ok {
		return Bastion{}, errors.New(i18n.T("bastion.notFound", id))
	}
	cp := *b
	if !s.fileSecrets {
		cp.Secret, _ = keyring.Get(keyringService, bastionKey(id))
	}
	return cp, nil
}

// SaveBastion creates or updates a bastion. An empty secret keeps the stored one
// unless the auth type no longer needs a secret.
func (s *Store) SaveBastion(b Bastion) (Bastion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b.Name = strings.TrimSpace(b.Name)
	if b.Name == "" || b.Host == "" || b.User == "" {
		return Bastion{}, errors.New(i18n.T("bastion.required"))
	}
	b.BastionID = ""
	if b.ID == "" {
		b.ID = NewID()
	} else if b.Secret == "" && b.AuthType != "agent" {
		if old, err := s.getBastionLocked(b.ID); err == nil {
			b.Secret = old.Secret
		}
	}
	if b.AuthType == "agent" {
		b.Secret = ""
	}
	stored := b
	if !s.fileSecrets {
		var err error
		if b.Secret == "" {
			_ = keyring.Delete(keyringService, bastionKey(b.ID))
		} else {
			err = keyring.Set(keyringService, bastionKey(b.ID), b.Secret)
		}
		if err != nil {
			s.fileSecrets = true
		} else {
			stored.Secret = ""
		}
	}
	s.bastions[b.ID] = &stored
	b.Secret = ""
	return b, s.flush()
}

// DeleteBastion removes a bastion that no instance uses any more.
func (s *Store) DeleteBastion(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if users := s.usersLocked(id); len(users) > 0 {
		return errors.New(i18n.T("bastion.inUse", strings.Join(users, ", ")))
	}
	if !s.fileSecrets {
		_ = keyring.Delete(keyringService, bastionKey(id))
	}
	delete(s.bastions, id)
	return s.flush()
}

// Resolve fills hops that reference a shared bastion with its current settings and secret.
func (s *Store) Resolve(in *Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	hops := make([]SSHHop, len(in.Network.SSH))
	for i, h := range in.Network.SSH {
		if h.BastionID != "" {
			b, err := s.getBastionLocked(h.BastionID)
			if err != nil {
				return err
			}
			h = b.SSHHop
			h.BastionID = b.ID
		}
		hops[i] = h
	}
	in.Network.SSH = hops
	return nil
}
