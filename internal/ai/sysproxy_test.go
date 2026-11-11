package ai

import (
	"net/http"
	"net/url"
	"testing"
)

func TestParseWindowsProxy(t *testing.T) {
	p := parseWindowsProxy("127.0.0.1:7890", "localhost;127.*;10.*;192.168.*;<local>")
	if p.HTTP != "http://127.0.0.1:7890" || p.HTTPS != p.HTTP || p.SOCKS != "" {
		t.Fatalf("single: %+v", p)
	}
	for host, want := range map[string]bool{
		"llm.example.cloud": false, "10.1.2.3": true, "192.168.0.9": true, "intranet": true, "localhost": true, "127.0.0.1": true,
	} {
		if got := p.proxyFor(&url.URL{Scheme: "https", Host: host + ":443"}) == nil; got != want {
			t.Errorf("%s bypassed=%v, want %v", host, got, want)
		}
	}

	p = parseWindowsProxy("http=proxy:80;https=proxy:443;socks=127.0.0.1:1080", "")
	if p.HTTP != "http://proxy:80" || p.HTTPS != "http://proxy:443" || p.SOCKS != "socks5://127.0.0.1:1080" {
		t.Fatalf("per protocol: %+v", p)
	}
	if u := p.proxyFor(&url.URL{Scheme: "https", Host: "api.example.com"}); u.String() != "http://proxy:443" {
		t.Fatalf("https route %v", u)
	}
	if u := parseWindowsProxy("socks=127.0.0.1:1080", "").proxyFor(&url.URL{Scheme: "https", Host: "a.b"}); u.String() != "socks5://127.0.0.1:1080" {
		t.Fatalf("socks only %v", u)
	}
}

func TestParseScutil(t *testing.T) {
	out := `<dictionary> {
  ExceptionsList : <array> {
    0 : 127.0.0.1
    1 : 192.168.0.0/16
    2 : *.local
    3 : corp.example.com
  }
  HTTPEnable : 1
  HTTPPort : 7890
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 1
  HTTPSPort : 7890
  HTTPSProxy : 127.0.0.1
  SOCKSEnable : 0
}`
	p := parseScutil(out)
	if p.HTTPS != "http://127.0.0.1:7890" || p.SOCKS != "" || len(p.Bypass) != 4 {
		t.Fatalf("%+v", p)
	}
	for host, want := range map[string]bool{"api.openai.com": false, "192.168.3.4": true, "printer.local": true, "git.corp.example.com": true} {
		if got := p.proxyFor(&url.URL{Scheme: "https", Host: host}) == nil; got != want {
			t.Errorf("%s bypassed=%v, want %v", host, got, want)
		}
	}
	if !parseScutil("<dictionary> {\n  HTTPEnable : 0\n}").empty() {
		t.Fatal("disabled proxy should be empty")
	}
}

func TestProxyRoutePrecedence(t *testing.T) {
	old := readSystemProxy
	defer func() { readSystemProxy = old }()
	readSystemProxy = func() (sysProxy, bool) { return sysProxy{HTTPS: "http://sys:1"}, true }
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		t.Setenv(k, "")
	}
	req, _ := http.NewRequest("POST", "https://llm.example.cloud/v1/chat/completions", nil)

	if u, _ := proxyRoute(nil)(req); u == nil || u.Host != "sys:1" {
		t.Fatalf("system proxy not used: %v", u)
	}
	cfg, _ := url.Parse("socks5://127.0.0.1:1080")
	if u, _ := proxyRoute(cfg)(req); u.String() != cfg.String() {
		t.Fatalf("configured proxy not used: %v", u)
	}
	t.Setenv("HTTPS_PROXY", "http://env:2")
	if !envProxySet() {
		t.Fatal("env not detected")
	}
}
