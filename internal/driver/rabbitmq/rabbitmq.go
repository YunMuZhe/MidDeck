// Package rabbitmq implements the RabbitMQ driver via the Management HTTP API.
package rabbitmq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/compat"
	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/diag"
	"github.com/YunMuZhe/MidDeck/internal/driver"
	"github.com/YunMuZhe/MidDeck/internal/i18n"
)

type Session struct {
	inst config.Instance
	base string
	http *http.Client
}

func Open(ctx context.Context, inst config.Instance, rawDial driver.DialFunc, rep *diag.Report) (*Session, error) {
	tlsCfg, err := driver.TLSConfig(inst.TLS)
	if err != nil {
		rep.Add(diag.Step{Name: i18n.T("step.tlsConfig"), Status: diag.Fail, Detail: err.Error()})
		return nil, err
	}
	// AMQP port check (optional: only if configured).
	for _, addr := range inst.Addrs {
		_ = rep.Run(i18n.T("rabbit.amqp", addr), func() (string, error) {
			c, err := rawDial(ctx, "tcp", addr)
			if err != nil {
				return "", err
			}
			c.Close()
			return "", nil
		}, driver.NetHint)
	}
	if inst.MgmtURL == "" {
		err := errors.New(i18n.T("rabbit.needMgmt"))
		rep.Add(diag.Step{Name: i18n.T("step.config"), Status: diag.Fail, Detail: err.Error(), Hint: i18n.T("rabbit.needMgmtHint")})
		return nil, err
	}
	u, err := url.Parse(strings.TrimRight(inst.MgmtURL, "/"))
	if err != nil || u.Host == "" {
		err = errors.New(i18n.T("rabbit.badURL", inst.MgmtURL))
		rep.Add(diag.Step{Name: i18n.T("step.config"), Status: diag.Fail, Detail: err.Error()})
		return nil, err
	}
	tr := &http.Transport{
		DialContext:     func(ctx context.Context, network, addr string) (net.Conn, error) { return rawDial(ctx, network, addr) },
		TLSClientConfig: tlsCfg,
		Proxy:           nil,
	}
	s := &Session{inst: inst, base: u.String(), http: &http.Client{Transport: tr, Timeout: 20 * time.Second}}
	host := u.Host
	if u.Port() == "" {
		if u.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	if err := rep.Run(i18n.T("rabbit.mgmt", host), func() (string, error) {
		c, err := rawDial(ctx, "tcp", host)
		if err != nil {
			return "", err
		}
		c.Close()
		return "", nil
	}, driver.NetHint); err != nil {
		return nil, err
	}
	if err := rep.Run(i18n.T("rabbit.auth"), func() (string, error) {
		var ov map[string]any
		if err := s.get(ctx, "/api/overview", &ov); err != nil {
			return "", err
		}
		return i18n.T("rabbit.cluster", ov["cluster_name"]), nil
	}, func(err error) string {
		if strings.Contains(err.Error(), "401") {
			return i18n.T("rabbit.401")
		}
		return driver.NetHint(err)
	}); err != nil {
		return nil, err
	}
	_ = rep.Run(i18n.T("step.version"), func() (string, error) {
		info, err := s.Info(ctx)
		if err != nil {
			return "", err
		}
		return info.Version + " · " + info.Compat.Message, nil
	}, nil)
	return s, nil
}

func (s *Session) Close() { s.http.CloseIdleConnections() }

func (s *Session) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, rd)
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.inst.Auth.Username, s.inst.Auth.Password)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 300 {
		var e struct{ Error, Reason string }
		_ = json.Unmarshal(data, &e)
		msg := e.Reason
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (s *Session) get(ctx context.Context, path string, out any) error {
	return s.do(ctx, http.MethodGet, path, nil, out)
}

func (s *Session) vhost() string {
	if s.inst.VHost == "" {
		return "/"
	}
	return s.inst.VHost
}

func esc(s string) string { return url.PathEscape(s) }

func (s *Session) Info(ctx context.Context) (driver.ServerInfo, error) {
	info := driver.ServerInfo{Kind: "rabbitmq", Extra: map[string]string{}}
	var ov struct {
		RabbitMQVersion   string         `json:"rabbitmq_version"`
		ErlangVersion     string         `json:"erlang_version"`
		ManagementVersion string         `json:"management_version"`
		ClusterName       string         `json:"cluster_name"`
		ObjectTotals      map[string]int `json:"object_totals"`
	}
	if err := s.get(ctx, "/api/overview", &ov); err != nil {
		return info, err
	}
	info.Version = ov.RabbitMQVersion
	info.Extra["erlang"] = ov.ErlangVersion
	info.Extra["cluster"] = ov.ClusterName
	for k, v := range ov.ObjectTotals {
		info.Extra[k] = fmt.Sprint(v)
	}
	info.Compat = compat.Check("rabbitmq", info.Version)
	return info, nil
}

type Queue struct {
	Name       string         `json:"name"`
	VHost      string         `json:"vhost"`
	Type       string         `json:"type"`
	Durable    bool           `json:"durable"`
	AutoDelete bool           `json:"auto_delete"`
	State      string         `json:"state"`
	Messages   int64          `json:"messages"`
	Ready      int64          `json:"messages_ready"`
	Unacked    int64          `json:"messages_unacknowledged"`
	Consumers  int64          `json:"consumers"`
	Arguments  map[string]any `json:"arguments"`
	Node       string         `json:"node"`
}

func (s *Session) ListQueues(ctx context.Context) ([]Queue, error) {
	var qs []Queue
	if err := s.get(ctx, "/api/queues/"+esc(s.vhost()), &qs); err != nil {
		return nil, err
	}
	sort.Slice(qs, func(i, j int) bool { return qs[i].Name < qs[j].Name })
	return qs, nil
}

type Exchange struct {
	Name       string         `json:"name"`
	VHost      string         `json:"vhost"`
	Type       string         `json:"type"`
	Durable    bool           `json:"durable"`
	AutoDelete bool           `json:"auto_delete"`
	Internal   bool           `json:"internal"`
	Arguments  map[string]any `json:"arguments"`
}

func (s *Session) ListExchanges(ctx context.Context) ([]Exchange, error) {
	var xs []Exchange
	if err := s.get(ctx, "/api/exchanges/"+esc(s.vhost()), &xs); err != nil {
		return nil, err
	}
	sort.Slice(xs, func(i, j int) bool { return xs[i].Name < xs[j].Name })
	return xs, nil
}

type Binding struct {
	Source          string         `json:"source"`
	Destination     string         `json:"destination"`
	DestinationType string         `json:"destination_type"`
	RoutingKey      string         `json:"routing_key"`
	Arguments       map[string]any `json:"arguments"`
}

func (s *Session) QueueBindings(ctx context.Context, queue string) ([]Binding, error) {
	var bs []Binding
	err := s.get(ctx, "/api/queues/"+esc(s.vhost())+"/"+esc(queue)+"/bindings", &bs)
	return bs, err
}

type CreateQueueReq struct {
	Name      string         `json:"name"`
	Type      string         `json:"type"` // classic | quorum | stream
	Durable   bool           `json:"durable"`
	Arguments map[string]any `json:"arguments"`
	// Optional binding created together with the queue.
	BindExchange string `json:"bindExchange"`
	RoutingKey   string `json:"routingKey"`
}

func (s *Session) CreateQueue(ctx context.Context, r CreateQueueReq) error {
	args := map[string]any{}
	for k, v := range r.Arguments {
		args[k] = v
	}
	if r.Type != "" && r.Type != "classic" {
		args["x-queue-type"] = r.Type
		r.Durable = true
	}
	body := map[string]any{"durable": r.Durable, "auto_delete": false, "arguments": args}
	if err := s.do(ctx, http.MethodPut, "/api/queues/"+esc(s.vhost())+"/"+esc(r.Name), body, nil); err != nil {
		return err
	}
	if r.BindExchange != "" {
		return s.do(ctx, http.MethodPost, "/api/bindings/"+esc(s.vhost())+"/e/"+esc(r.BindExchange)+"/q/"+esc(r.Name),
			map[string]any{"routing_key": r.RoutingKey}, nil)
	}
	return nil
}

func (s *Session) DeleteQueue(ctx context.Context, name string) error {
	return s.do(ctx, http.MethodDelete, "/api/queues/"+esc(s.vhost())+"/"+esc(name), nil, nil)
}

func (s *Session) PurgeQueue(ctx context.Context, name string) error {
	return s.do(ctx, http.MethodDelete, "/api/queues/"+esc(s.vhost())+"/"+esc(name)+"/contents", nil, nil)
}

type CreateExchangeReq struct {
	Name    string `json:"name"`
	Type    string `json:"type"` // direct | topic | fanout | headers
	Durable bool   `json:"durable"`
}

func (s *Session) CreateExchange(ctx context.Context, r CreateExchangeReq) error {
	return s.do(ctx, http.MethodPut, "/api/exchanges/"+esc(s.vhost())+"/"+esc(r.Name),
		map[string]any{"type": r.Type, "durable": r.Durable, "auto_delete": false, "internal": false, "arguments": map[string]any{}}, nil)
}

type PeekedMessage struct {
	Payload         string         `json:"payload"`
	PayloadEncoding string         `json:"payload_encoding"`
	PayloadBytes    int            `json:"payload_bytes"`
	Redelivered     bool           `json:"redelivered"`
	Exchange        string         `json:"exchange"`
	RoutingKey      string         `json:"routing_key"`
	MessageCount    int            `json:"message_count"`
	Properties      map[string]any `json:"properties"`
}

// Peek fetches up to n messages and requeues them. Note: requeued messages are
// marked redelivered and, for classic queues, may change position.
func (s *Session) Peek(ctx context.Context, queue string, n int) ([]PeekedMessage, error) {
	if n <= 0 || n > 500 {
		n = 20
	}
	var out []PeekedMessage
	err := s.do(ctx, http.MethodPost, "/api/queues/"+esc(s.vhost())+"/"+esc(queue)+"/get",
		map[string]any{"count": n, "ackmode": "ack_requeue_true", "encoding": "auto", "truncate": 50000}, &out)
	return out, err
}

type PublishReq struct {
	Exchange   string            `json:"exchange"`
	RoutingKey string            `json:"routingKey"`
	Payload    string            `json:"payload"`
	Headers    map[string]string `json:"headers"`
	Persistent bool              `json:"persistent"`
}

func (s *Session) Publish(ctx context.Context, r PublishReq) (bool, error) {
	props := map[string]any{}
	if len(r.Headers) > 0 {
		props["headers"] = r.Headers
	}
	if r.Persistent {
		props["delivery_mode"] = 2
	}
	ex := r.Exchange
	if ex == "" {
		ex = "amq.default"
	}
	var out struct {
		Routed bool `json:"routed"`
	}
	err := s.do(ctx, http.MethodPost, "/api/exchanges/"+esc(s.vhost())+"/"+esc(ex)+"/publish",
		map[string]any{"properties": props, "routing_key": r.RoutingKey, "payload": r.Payload, "payload_encoding": "string"}, &out)
	return out.Routed, err
}

// Definitions exports the vhost definitions (queues, exchanges, bindings, policies).
func (s *Session) Definitions(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.get(ctx, "/api/definitions/"+esc(s.vhost()), &raw)
	return raw, err
}
