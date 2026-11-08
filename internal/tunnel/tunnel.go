// Package tunnel builds network dialers that reach middleware through
// SOCKS5 proxies and multi-hop SSH jump hosts.
//
// Every middleware connection is dialed through Dialer.DialContext. Because the
// final hop dials the target from the jump host itself, Kafka brokers that
// advertise internal addresses keep working without per-broker port forwards;
// Network.AddrMap can additionally rewrite unreachable advertised addresses.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/diag"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/net/proxy"
)

// Dialer dials through the configured proxy / SSH chain.
type Dialer struct {
	cfg            config.Network
	knownHostsFile string

	mu      sync.Mutex
	clients []*ssh.Client
}

// New returns a dialer. knownHostsFile is MidDeck's own known_hosts (trust on first use).
func New(cfg config.Network, knownHostsFile string) *Dialer {
	return &Dialer{cfg: cfg, knownHostsFile: knownHostsFile}
}

// Direct reports whether no proxy and no SSH hops are configured.
func (d *Dialer) Direct() bool {
	return d.cfg.Proxy.Type == "" && len(d.cfg.SSH) == 0
}

// Connect establishes the SSH chain, recording one diagnostic step per hop.
func (d *Dialer) Connect(ctx context.Context, rep *diag.Report) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.clients) == len(d.cfg.SSH) {
		return nil
	}
	if d.cfg.Proxy.Type != "" && rep != nil {
		rep.Add(diag.Step{Name: "Proxy " + d.cfg.Proxy.Addr, Status: diag.OK, Detail: d.cfg.Proxy.Type})
	}
	for i, hop := range d.cfg.SSH {
		addr := net.JoinHostPort(hop.Host, strconv.Itoa(defaultPort(hop.Port, 22)))
		name := fmt.Sprintf("SSH hop %d (%s@%s)", i+1, hop.User, addr)
		connect := func() (string, error) {
			c, err := d.dialHop(ctx, hop, addr)
			if err != nil {
				return "", err
			}
			d.clients = append(d.clients, c)
			return string(c.ServerVersion()), nil
		}
		var err error
		if rep != nil {
			err = rep.Run(name, connect, sshHint)
		} else {
			_, err = connect()
		}
		if err != nil {
			d.closeLocked()
			return err
		}
	}
	return nil
}

func (d *Dialer) dialHop(ctx context.Context, hop config.SSHHop, addr string) (*ssh.Client, error) {
	auth, err := authMethods(hop)
	if err != nil {
		return nil, err
	}
	cc := &ssh.ClientConfig{
		User:            hop.User,
		Auth:            auth,
		HostKeyCallback: d.hostKeyCallback(),
		Timeout:         10 * time.Second,
	}
	conn, err := d.dialVia(d.clients, ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cc)
	if err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

// dialVia dials via the last of the given SSH clients, else the proxy, else directly.
func (d *Dialer) dialVia(clients []*ssh.Client, ctx context.Context, network, addr string) (net.Conn, error) {
	if n := len(clients); n > 0 {
		return clients[n-1].DialContext(ctx, network, addr)
	}
	if d.cfg.Proxy.Type == "socks5" {
		var auth *proxy.Auth
		if d.cfg.Proxy.User != "" {
			auth = &proxy.Auth{User: d.cfg.Proxy.User, Password: d.cfg.Proxy.Secret}
		}
		p, err := proxy.SOCKS5("tcp", d.cfg.Proxy.Addr, auth, &net.Dialer{Timeout: 10 * time.Second})
		if err != nil {
			return nil, err
		}
		if cd, ok := p.(proxy.ContextDialer); ok {
			return cd.DialContext(ctx, network, addr)
		}
		return p.Dial(network, addr)
	}
	var nd net.Dialer
	nd.Timeout = 10 * time.Second
	return nd.DialContext(ctx, network, addr)
}

// DialContext dials addr through the chain, applying AddrMap first.
func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if err := d.Connect(ctx, nil); err != nil {
		return nil, err
	}
	if m, ok := d.cfg.AddrMap[addr]; ok && m != "" {
		addr = m
	}
	d.mu.Lock()
	clients := d.clients
	d.mu.Unlock()
	return d.dialVia(clients, ctx, network, addr)
}

// Close tears down the SSH chain.
func (d *Dialer) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closeLocked()
}

func (d *Dialer) closeLocked() {
	for i := len(d.clients) - 1; i >= 0; i-- {
		d.clients[i].Close()
	}
	d.clients = nil
}

func authMethods(h config.SSHHop) ([]ssh.AuthMethod, error) {
	switch h.AuthType {
	case "", "password":
		return []ssh.AuthMethod{
			ssh.Password(h.Secret),
			ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				ans := make([]string, len(qs))
				for i := range ans {
					ans[i] = h.Secret
				}
				return ans, nil
			}),
		}, nil
	case "key":
		path := expandHome(h.KeyPath)
		if path == "" {
			path = expandHome("~/.ssh/id_rsa")
		}
		pem, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read private key: %w", err)
		}
		var signer ssh.Signer
		if h.Secret != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(h.Secret))
		} else {
			signer, err = ssh.ParsePrivateKey(pem)
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	case "agent":
		sock := os.Getenv("SSH_AUTH_SOCK")
		if sock == "" {
			return nil, errors.New("SSH_AUTH_SOCK is not set, ssh-agent unavailable")
		}
		c, err := net.Dial("unix", sock)
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeysCallback(agent.NewClient(c).Signers)}, nil
	}
	return nil, fmt.Errorf("unknown ssh auth type %q", h.AuthType)
}

// hostKeyCallback verifies against ~/.ssh/known_hosts and MidDeck's own file,
// trusting and recording unknown hosts on first use. A changed key is rejected.
func (d *Dialer) hostKeyCallback() ssh.HostKeyCallback {
	files := []string{}
	for _, f := range []string{expandHome("~/.ssh/known_hosts"), d.knownHostsFile} {
		if _, err := os.Stat(f); err == nil {
			files = append(files, f)
		}
	}
	var check ssh.HostKeyCallback
	if len(files) > 0 {
		check, _ = knownhosts.New(files...)
	}
	return func(host string, remote net.Addr, key ssh.PublicKey) error {
		if check != nil {
			err := check(host, remote, key)
			var ke *knownhosts.KeyError
			if err == nil {
				return nil
			}
			if !errors.As(err, &ke) || len(ke.Want) > 0 {
				return fmt.Errorf("host key mismatch for %s: %w", host, err)
			}
		}
		if d.knownHostsFile == "" {
			return nil
		}
		_ = os.MkdirAll(filepath.Dir(d.knownHostsFile), 0o700)
		f, err := os.OpenFile(d.knownHostsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil
		}
		defer f.Close()
		_, _ = f.WriteString(knownhosts.Line([]string{knownhosts.Normalize(host)}, key) + "\n")
		return nil
	}
}

func sshHint(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "unable to authenticate"):
		return "SSH 认证失败：检查用户名、密码或私钥。堡垒机若要求二次验证（OTP），请先使用私钥认证。"
	case strings.Contains(s, "host key mismatch"):
		return "主机指纹与记录不一致，可能是机器重装或存在中间人，确认后从 known_hosts 中删除旧记录。"
	case strings.Contains(s, "administratively prohibited"), strings.Contains(s, "open failed"):
		return "跳板机禁止了 TCP 转发（AllowTcpForwarding no）。可改用 Agent 模式或联系运维开通。"
	case strings.Contains(s, "timeout"), strings.Contains(s, "i/o timeout"):
		return "连接超时：检查地址、端口、VPN 和防火墙。"
	case strings.Contains(s, "refused"):
		return "端口拒绝连接：确认 SSH 服务端口是否正确。"
	}
	return ""
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

func defaultPort(p, def int) int {
	if p == 0 {
		return def
	}
	return p
}
