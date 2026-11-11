package ai

import (
	"bufio"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
)

// sysProxy is the proxy configured in the operating system's network settings,
// which browsers use but Go's http.ProxyFromEnvironment ignores.
type sysProxy struct {
	HTTP, HTTPS, SOCKS string // proxy URLs; empty when not set
	Bypass             []string
}

func (p sysProxy) empty() bool { return p.HTTP == "" && p.HTTPS == "" && p.SOCKS == "" }

// readSystemProxy is set per platform.
var readSystemProxy = func() (sysProxy, bool) { return sysProxy{}, false }

// envProxySet reports whether the standard proxy environment variables are set;
// they take precedence over the system settings, as with curl.
func envProxySet() bool {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

// proxyFor picks the proxy for u, or nil for a direct connection.
func (p sysProxy) proxyFor(u *url.URL) *url.URL {
	host := strings.ToLower(u.Hostname())
	if bypassed(host, p.Bypass) {
		return nil
	}
	order := []string{p.HTTP, p.HTTPS, p.SOCKS}
	if u.Scheme == "https" {
		order = []string{p.HTTPS, p.HTTP, p.SOCKS}
	}
	for _, s := range order {
		if s == "" {
			continue
		}
		if pu, err := url.Parse(s); err == nil && pu.Host != "" {
			return pu
		}
	}
	return nil
}

func bypassed(host string, rules []string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return true
	}
	for _, r := range rules {
		r = strings.ToLower(strings.TrimSpace(r))
		switch {
		case r == "":
		case r == "<local>":
			if ip == nil && !strings.Contains(host, ".") {
				return true
			}
		case strings.Contains(r, "/"):
			if _, n, err := net.ParseCIDR(r); err == nil && ip != nil && n.Contains(ip) {
				return true
			}
		case strings.ContainsAny(r, "*?"):
			if ok, _ := path.Match(r, host); ok {
				return true
			}
		default:
			r = strings.TrimPrefix(r, ".")
			if host == r || strings.HasSuffix(host, "."+r) {
				return true
			}
		}
	}
	return false
}

// proxyRoute is the proxy function for requests to the AI provider:
// the configured proxy, else the environment variables, else the system settings.
func proxyRoute(configured *url.URL) func(*http.Request) (*url.URL, error) {
	if configured != nil {
		return http.ProxyURL(configured)
	}
	if envProxySet() {
		return http.ProxyFromEnvironment
	}
	sp, ok := readSystemProxy()
	if !ok || sp.empty() {
		return http.ProxyFromEnvironment
	}
	return func(r *http.Request) (*url.URL, error) { return sp.proxyFor(r.URL), nil }
}

// parseWindowsProxy reads the ProxyServer / ProxyOverride registry values:
// "host:port" for every protocol, or "http=h:p;https=h:p;socks=h:p".
func parseWindowsProxy(server, override string) sysProxy {
	var p sysProxy
	withScheme := func(scheme, addr string) string {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return ""
		}
		if strings.Contains(addr, "://") {
			return addr
		}
		return scheme + "://" + addr
	}
	server = strings.TrimSpace(server)
	if server != "" && !strings.Contains(server, "=") {
		p.HTTP = withScheme("http", server)
		p.HTTPS = p.HTTP
	} else {
		for _, part := range strings.Split(server, ";") {
			k, v, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "http":
				p.HTTP = withScheme("http", v)
			case "https":
				p.HTTPS = withScheme("http", v)
			case "socks":
				p.SOCKS = withScheme("socks5", v)
			}
		}
	}
	for _, r := range strings.Split(override, ";") {
		if r = strings.TrimSpace(r); r != "" {
			p.Bypass = append(p.Bypass, r)
		}
	}
	return p
}

// parseScutil reads the output of macOS `scutil --proxy`.
func parseScutil(out string) sysProxy {
	kv := map[string]string{}
	var bypass []string
	inList := false
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		k, v, ok := strings.Cut(line, " : ")
		switch {
		case strings.HasPrefix(line, "ExceptionsList"):
			inList = true
		case line == "}":
			inList = false
		case inList && ok:
			bypass = append(bypass, strings.TrimSpace(v))
		case ok:
			kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	get := func(prefix, scheme string) string {
		if kv[prefix+"Enable"] != "1" || kv[prefix+"Proxy"] == "" {
			return ""
		}
		host := kv[prefix+"Proxy"]
		if port := kv[prefix+"Port"]; port != "" {
			host = net.JoinHostPort(host, port)
		}
		return scheme + "://" + host
	}
	return sysProxy{HTTP: get("HTTP", "http"), HTTPS: get("HTTPS", "http"), SOCKS: get("SOCKS", "socks5"), Bypass: bypass}
}
