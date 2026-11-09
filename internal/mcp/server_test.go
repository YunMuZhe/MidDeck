package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func run(t *testing.T, s *Server, lines ...string) []map[string]any {
	t.Helper()
	pr, pw := io.Pipe()
	go func() {
		_ = s.Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), pw)
		pw.Close()
	}()
	var out []map[string]any
	sc := bufio.NewScanner(pr)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad line %q", sc.Text())
		}
		out = append(out, m)
	}
	return out
}

func TestProtocol(t *testing.T) {
	s := &Server{Name: "middeck", Version: "test", Tools: []Tool{
		{Name: "echo", Properties: map[string]any{"x": map[string]any{"type": "string"}}, Handler: func(_ context.Context, a map[string]any) (any, error) {
			return map[string]string{"x": Str(a, "x")}, nil
		}},
		{Name: "boom", Handler: func(context.Context, map[string]any) (any, error) { return nil, errors.New("nope") }},
	}}
	out := run(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"x":"hi"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"boom"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"nope"}`,
	)
	byID := map[float64]map[string]any{}
	for _, m := range out {
		byID[m["id"].(float64)] = m
	}
	if len(byID) != 5 {
		t.Fatalf("want 5 responses (notification gets none), got %d: %v", len(byID), out)
	}
	if v := byID[1]["result"].(map[string]any)["protocolVersion"]; v != "2025-03-26" {
		t.Fatalf("protocol version not echoed: %v", v)
	}
	if n := len(byID[2]["result"].(map[string]any)["tools"].([]any)); n != 2 {
		t.Fatalf("tools/list returned %d tools", n)
	}
	r3 := byID[3]["result"].(map[string]any)
	if r3["isError"] != false || !strings.Contains(r3["content"].([]any)[0].(map[string]any)["text"].(string), `"hi"`) {
		t.Fatalf("echo: %v", r3)
	}
	if r4 := byID[4]["result"].(map[string]any); r4["isError"] != true {
		t.Fatalf("tool errors must be isError results: %v", r4)
	}
	if byID[5]["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Fatalf("unknown method: %v", byID[5])
	}
}
