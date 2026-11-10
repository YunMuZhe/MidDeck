package main

import (
	"errors"

	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/decode"
	"github.com/YunMuZhe/MidDeck/internal/driver/kafka"
	"github.com/YunMuZhe/MidDeck/internal/i18n"
)

func (a *App) registry(id string) (*decode.Registry, error) {
	s, c, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return nil, err
	}
	if s.Decoder() == nil || s.Decoder().Registry() == nil {
		return nil, errors.New(i18n.T("schema.noRegistry", c.inst.Name))
	}
	return s.Decoder().Registry(), nil
}

// KafkaSchemaSubjects lists the Schema Registry subjects of a Kafka instance.
func (a *App) KafkaSchemaSubjects(id string) ([]decode.Subject, error) {
	r, err := a.registry(id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return r.Subjects(ctx)
}

// KafkaSchema returns one subject version ("latest" or a number).
func (a *App) KafkaSchema(id, subject, version string) (*decode.Schema, error) {
	r, err := a.registry(id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.reqCtx()
	defer cancel()
	return r.Version(ctx, subject, version)
}

// ProtoTypes is the local Protobuf catalog of an instance.
type ProtoTypes struct {
	Types    []string              `json:"types"`
	Problems []string              `json:"problems"`
	Bindings []config.ProtoBinding `json:"bindings"`
	Paths    []string              `json:"paths"`
	Registry string                `json:"registry"`
}

// KafkaProtoTypes lists message types from the instance's .proto files; reload
// recompiles them after the files changed.
func (a *App) KafkaProtoTypes(id string, reload bool) (ProtoTypes, error) {
	s, c, err := sessionAs[*kafka.Session](a, id)
	if err != nil {
		return ProtoTypes{}, err
	}
	out := ProtoTypes{Types: []string{}, Problems: []string{}, Bindings: c.inst.Proto.Bindings, Paths: c.inst.Proto.Paths}
	if out.Bindings == nil {
		out.Bindings = []config.ProtoBinding{}
	}
	if d := s.Decoder(); d != nil {
		t, p := d.ProtoTypes(reload)
		out.Types = append(out.Types, t...)
		out.Problems = append(out.Problems, p...)
		if d.Registry() != nil {
			out.Registry = d.Registry().URL()
		}
	}
	return out, nil
}

// KafkaSetProtoBinding sets (or, with empty types, removes) the Protobuf types used
// to decode a topic's keys and values. It is a local display setting: it is saved
// to the connection but changes nothing on the cluster.
func (a *App) KafkaSetProtoBinding(id string, b config.ProtoBinding) error {
	in, err := a.store.Get(id)
	if err != nil {
		return err
	}
	var out []config.ProtoBinding
	for _, x := range in.Proto.Bindings {
		if x.Topic != b.Topic {
			out = append(out, x)
		}
	}
	if b.Key != "" || b.Value != "" {
		out = append(out, b)
	}
	in.Proto.Bindings = out
	if _, err := a.store.Save(in); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if c, ok := a.conns[id]; ok {
		c.inst.Proto.Bindings = out
		if s, ok := c.session.(*kafka.Session); ok && s.Decoder() != nil {
			s.Decoder().SetBindings(out)
		}
	}
	return nil
}
