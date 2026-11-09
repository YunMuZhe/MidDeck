package main

import (
	"context"
	"errors"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/ai"
	"github.com/YunMuZhe/MidDeck/internal/i18n"
)

// GetAISettings returns the assistant settings without the API key.
func (a *App) GetAISettings() (ai.Settings, error) {
	s, err := ai.Load(a.dataDir)
	s.APIKey = ""
	return s, err
}

// SaveAISettings stores the settings; an empty key keeps the saved one.
func (a *App) SaveAISettings(s ai.Settings) error { return ai.Save(a.dataDir, s) }

// TestAI sends a tiny request with the given (unsaved) settings.
func (a *App) TestAI(s ai.Settings) (string, error) {
	if s.APIKey == "" {
		if old, err := ai.Load(a.dataDir); err == nil {
			s.APIKey = old.APIKey
		}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
	defer cancel()
	start := time.Now()
	act, err := ai.Plan(ctx, s, ai.Context{Instances: []ai.InstanceDigest{}}, "Connectivity check: call the reply tool with the message OK.")
	if err != nil {
		return "", err
	}
	return i18n.T("ai.testOK", act.Name, time.Since(start).Milliseconds()), nil
}

// AskAI turns a request into one proposed UI action. current is the open instance id.
func (a *App) AskAI(prompt, current string) (ai.Action, error) {
	s, err := ai.Load(a.dataDir)
	if err != nil {
		return ai.Action{}, err
	}
	if !s.Enabled {
		return ai.Action{}, errors.New(i18n.T("ai.disabled"))
	}
	wc := ai.Context{Current: current}
	for _, in := range a.store.List() {
		d := ai.InstanceDigest{
			ID: in.ID, Name: in.Name, Kind: string(in.Kind), Project: in.Project, Env: in.Env,
			Tags: in.Tags, Aliases: in.Aliases, Description: in.Description, ReadOnly: in.ReadOnly,
		}
		if s.SendAddresses {
			d.Addrs = in.Addrs
			if in.MgmtURL != "" {
				d.Addrs = append(d.Addrs, in.MgmtURL)
			}
		}
		wc.Instances = append(wc.Instances, d)
	}
	for _, b := range a.store.ListBastions() {
		d := ai.BastionDigest{ID: b.ID, Name: b.Name, Description: b.Description}
		if s.SendAddresses {
			d.Host = b.Host
		}
		wc.Bastions = append(wc.Bastions, d)
	}
	ctx, cancel := context.WithTimeout(a.ctx, 120*time.Second)
	defer cancel()
	return ai.Plan(ctx, s, wc, prompt)
}
