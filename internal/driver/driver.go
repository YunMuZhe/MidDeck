// Package driver contains shared helpers for middleware drivers.
package driver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/YunMuZhe/MidDeck/internal/compat"
	"github.com/YunMuZhe/MidDeck/internal/config"
)

// DialFunc dials an address (usually through a tunnel.Dialer).
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// ServerInfo is what a driver reports after connecting.
type ServerInfo struct {
	Kind    string            `json:"kind"`
	Version string            `json:"version"`
	Mode    string            `json:"mode"` // e.g. kraft/zookeeper, cluster/standalone
	Extra   map[string]string `json:"extra,omitempty"`
	Compat  compat.Result     `json:"compat"`
}

// Session is an open connection to one instance.
type Session interface {
	Info(ctx context.Context) (ServerInfo, error)
	Close()
}

// TLSConfig builds a *tls.Config from instance settings, or nil when TLS is off.
func TLSConfig(t config.TLS) (*tls.Config, error) {
	if !t.Enabled {
		return nil, nil
	}
	cfg := &tls.Config{InsecureSkipVerify: t.InsecureSkipVerify, MinVersion: tls.VersionTLS12} //nolint:gosec // user opt-in
	if t.CAFile != "" {
		pem, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no certificates found in %s", t.CAFile)
		}
		cfg.RootCAs = pool
	}
	if t.CertFile != "" && t.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

// WithTLS wraps dial so that connections are TLS-wrapped when cfg is non-nil.
func WithTLS(dial DialFunc, cfg *tls.Config) DialFunc {
	if cfg == nil {
		return dial
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		raw, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		c := cfg.Clone()
		if c.ServerName == "" {
			if host, _, err := net.SplitHostPort(addr); err == nil {
				c.ServerName = host
			}
		}
		tc := tls.Client(raw, c)
		if err := tc.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, err
		}
		return tc, nil
	}
}

// NetHint turns common network errors into a human hint.
func NetHint(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	switch {
	case contains(s, "no such host"):
		return "DNS 解析失败：该域名可能只在内网可解析，可配置 SSH 跳板，或改用 IP。"
	case contains(s, "refused"):
		return "端口拒绝连接：确认服务端口和监听地址。"
	case contains(s, "timeout"), contains(s, "deadline"):
		return "连接超时：检查网络、VPN、安全组，或是否需要通过堡垒机访问。"
	case contains(s, "certificate"), contains(s, "x509"), contains(s, "tls"):
		return "TLS 校验失败：配置 CA 证书，或在测试环境勾选“跳过证书校验”。"
	case contains(s, "SASL"), contains(s, "auth"), contains(s, "NOAUTH"), contains(s, "WRONGPASS"), contains(s, "401"):
		return "认证失败：检查用户名、密码和认证机制。"
	}
	return ""
}

func contains(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
