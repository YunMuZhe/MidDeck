// Package decode turns Kafka record bytes into readable text: Confluent Schema
// Registry framed Avro / Protobuf / JSON Schema, plain Protobuf with local .proto
// files, and a base64 fallback for anything else that is not UTF-8.
package decode

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/config"
)

// DialFunc dials through the instance's tunnel.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// Reference is a schema imported by another one.
type Reference struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Version int    `json:"version"`
}

// Schema as returned by the registry. Type is AVRO, PROTOBUF or JSON.
type Schema struct {
	ID         int         `json:"id"`
	Subject    string      `json:"subject,omitempty"`
	Version    int         `json:"version,omitempty"`
	Type       string      `json:"schemaType"`
	Schema     string      `json:"schema"`
	References []Reference `json:"references,omitempty"`
}

// Subject summarizes one registry subject.
type Subject struct {
	Name     string `json:"name"`
	Versions []int  `json:"versions"`
	Latest   int    `json:"latest"`
	Type     string `json:"type"`
	ID       int    `json:"id"`
}

// Registry is a small Confluent Schema Registry client with an ID cache.
// Schemas are immutable per ID and per subject/version, so caching is safe.
type Registry struct {
	base       string
	user, pass string
	http       *http.Client

	mu     sync.Mutex
	byID   map[int]*Schema
	byVer  map[string]*Schema
	failed map[int]error // negative cache, so a bad ID does not cost a request per record
	// After a network failure the registry is skipped for a while, so reading 500
	// records does not mean 500 timeouts.
	downUntil time.Time
	downErr   error
}

const downFor = 30 * time.Second

// NewRegistry builds a client; dial routes connections through the instance's tunnel.
func NewRegistry(c config.Registry, dial DialFunc) (*Registry, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(c.URL), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid schema registry URL %q", c.URL)
	}
	tr := &http.Transport{
		DialContext:         func(ctx context.Context, network, addr string) (net.Conn, error) { return dial(ctx, network, addr) },
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: c.InsecureSkipVerify, MinVersion: tls.VersionTLS12}, //nolint:gosec // user opt-in
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     60 * time.Second,
	}
	user, pass := c.Username, c.Password
	if u.User != nil { // https://key:secret@host
		user = u.User.Username()
		pass, _ = u.User.Password()
		u.User = nil
	}
	return &Registry{
		base: u.String(), user: user, pass: pass,
		http: &http.Client{Transport: tr, Timeout: 15 * time.Second},
		byID: map[int]*Schema{}, byVer: map[string]*Schema{}, failed: map[int]error{},
	}, nil
}

// URL is the registry base URL without credentials.
func (r *Registry) URL() string { return r.base }

// Close releases idle connections.
func (r *Registry) Close() { r.http.CloseIdleConnections() }

// HTTPError is a non-2xx registry response.
type HTTPError struct {
	Status  int
	Code    int    `json:"error_code"`
	Message string `json:"message"`
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("schema registry: %s (HTTP %d)", e.Message, e.Status)
	}
	return fmt.Sprintf("schema registry: HTTP %d", e.Status)
}

func (r *Registry) get(ctx context.Context, path string, out any) error {
	r.mu.Lock()
	if time.Now().Before(r.downUntil) {
		err := r.downErr
		r.mu.Unlock()
		return err
	}
	r.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.schemaregistry.v1+json, application/json")
	if r.user != "" || r.pass != "" {
		req.SetBasicAuth(r.user, r.pass)
	}
	resp, err := r.http.Do(req)
	if err != nil {
		if ctx.Err() == nil { // not the caller giving up
			r.mu.Lock()
			r.downUntil, r.downErr = time.Now().Add(downFor), fmt.Errorf("schema registry unreachable: %w", err)
			r.mu.Unlock()
		}
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		he := &HTTPError{Status: resp.StatusCode}
		_ = json.Unmarshal(body, he)
		return he
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("schema registry: unexpected response from %s: %w", path, err)
	}
	return nil
}

func normType(t string) string {
	if t == "" {
		return "AVRO"
	}
	return strings.ToUpper(t)
}

// ByID fetches the schema with a global ID.
func (r *Registry) ByID(ctx context.Context, id int) (*Schema, error) {
	r.mu.Lock()
	if s, ok := r.byID[id]; ok {
		r.mu.Unlock()
		return s, nil
	}
	if err, ok := r.failed[id]; ok {
		r.mu.Unlock()
		return nil, err
	}
	r.mu.Unlock()
	var s Schema
	err := r.get(ctx, fmt.Sprintf("/schemas/ids/%d", id), &s)
	if err != nil {
		var he *HTTPError
		if errors.As(err, &he) && he.Status == http.StatusNotFound {
			err = fmt.Errorf("schema id %d not found in the registry", id)
			r.mu.Lock()
			r.failed[id] = err
			r.mu.Unlock()
		}
		return nil, err
	}
	s.ID = id
	s.Type = normType(s.Type)
	// Which subject/version is this? Optional (older registries lack the endpoint).
	var sv []struct {
		Subject string `json:"subject"`
		Version int    `json:"version"`
	}
	if r.get(ctx, fmt.Sprintf("/schemas/ids/%d/versions", id), &sv) == nil && len(sv) > 0 {
		sort.Slice(sv, func(i, j int) bool { return sv[i].Subject < sv[j].Subject })
		s.Subject, s.Version = sv[0].Subject, sv[0].Version
	}
	r.mu.Lock()
	r.byID[id] = &s
	r.mu.Unlock()
	return &s, nil
}

// Version fetches subject/version; version "latest" or a number.
func (r *Registry) Version(ctx context.Context, subject, version string) (*Schema, error) {
	key := subject + "\x00" + version
	if version != "latest" {
		r.mu.Lock()
		s, ok := r.byVer[key]
		r.mu.Unlock()
		if ok {
			return s, nil
		}
	}
	var s Schema
	if err := r.get(ctx, "/subjects/"+url.PathEscape(subject)+"/versions/"+url.PathEscape(version), &s); err != nil {
		return nil, err
	}
	s.Type = normType(s.Type)
	if version != "latest" {
		r.mu.Lock()
		r.byVer[key] = &s
		r.mu.Unlock()
	}
	return &s, nil
}

// Ping checks reachability and credentials; it returns the number of subjects.
func (r *Registry) Ping(ctx context.Context) (int, error) {
	var subs []string
	err := r.get(ctx, "/subjects", &subs)
	return len(subs), err
}

// Subjects lists every subject with its versions and the latest schema type.
func (r *Registry) Subjects(ctx context.Context) ([]Subject, error) {
	var names []string
	if err := r.get(ctx, "/subjects", &names); err != nil {
		return nil, err
	}
	sort.Strings(names)
	out := make([]Subject, len(names))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, n := range names {
		out[i].Name = n
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var vs []int
			if r.get(ctx, "/subjects/"+url.PathEscape(out[i].Name)+"/versions", &vs) == nil && len(vs) > 0 {
				sort.Ints(vs)
				out[i].Versions = vs
				out[i].Latest = vs[len(vs)-1]
				if s, err := r.Version(ctx, out[i].Name, fmt.Sprint(out[i].Latest)); err == nil {
					out[i].Type, out[i].ID = s.Type, s.ID
				}
			}
		}(i)
	}
	wg.Wait()
	return out, ctx.Err()
}
