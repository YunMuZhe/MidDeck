// Package mcp is a minimal Model Context Protocol server over stdio
// (newline-delimited JSON-RPC 2.0), enough for tools: initialize, tools/list,
// tools/call and ping. It has no dependencies so it ships inside the MidDeck binary.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// LatestProtocol is answered when the client asks for a version we don't know.
const LatestProtocol = "2025-06-18"

var knownProtocols = map[string]bool{"2024-11-05": true, "2025-03-26": true, "2025-06-18": true, "2025-11-25": true}

// Tool is one callable tool.
type Tool struct {
	Name        string
	Title       string
	Description string
	Properties  map[string]any
	// Writes marks tools that change MidDeck's own configuration; Destructive ones delete.
	Writes, Destructive bool
	Required            []string
	// Handler returns any JSON-serialisable value; it is sent back as text.
	Handler func(ctx context.Context, args map[string]any) (any, error)
}

// Server serves tools to one client.
type Server struct {
	Name, Version, Instructions string
	Tools                       []Tool

	mu  sync.Mutex
	out io.Writer
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve reads requests from in until EOF. Tool calls run concurrently.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	s.out = out
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	var wg sync.WaitGroup
	defer wg.Wait()
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			s.send(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			continue
		}
		if len(req.ID) == 0 { // notification
			continue
		}
		if req.Method == "tools/call" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.reply(req, s.call(ctx, req.Params))
			}()
			continue
		}
		s.reply(req, s.handle(req))
	}
	return sc.Err()
}

func (s *Server) reply(req request, result any) {
	resp := response{JSONRPC: "2.0", ID: req.ID}
	if e, ok := result.(*rpcError); ok {
		resp.Error = e
	} else {
		resp.Result = result
	}
	s.send(resp)
}

func (s *Server) send(r response) {
	b, _ := json.Marshal(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.out.Write(append(b, '\n'))
}

func (s *Server) handle(req request) any {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		v := p.ProtocolVersion
		if !knownProtocols[v] {
			v = LatestProtocol
		}
		res := map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}
		if s.Instructions != "" {
			res["instructions"] = s.Instructions
		}
		return res
	case "ping":
		return map[string]any{}
	case "tools/list":
		list := make([]map[string]any, 0, len(s.Tools))
		for _, t := range s.Tools {
			props := t.Properties
			if props == nil {
				props = map[string]any{}
			}
			schema := map[string]any{"type": "object", "properties": props}
			if len(t.Required) > 0 {
				schema["required"] = t.Required
			}
			list = append(list, map[string]any{
				"name": t.Name, "title": t.Title, "description": t.Description, "inputSchema": schema,
				"annotations": map[string]any{"readOnlyHint": !t.Writes, "destructiveHint": t.Destructive, "openWorldHint": false},
			})
		}
		return map[string]any{"tools": list}
	case "resources/list":
		return map[string]any{"resources": []any{}}
	case "prompts/list":
		return map[string]any{"prompts": []any{}}
	}
	return &rpcError{-32601, "method not found: " + req.Method}
}

func (s *Server) call(ctx context.Context, params json.RawMessage) any {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return &rpcError{-32602, "invalid params"}
	}
	for _, t := range s.Tools {
		if t.Name != p.Name {
			continue
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		out, err := safeCall(ctx, t, p.Arguments)
		if err != nil {
			return textResult(err.Error(), true)
		}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return textResult(err.Error(), true)
		}
		return textResult(string(b), false)
	}
	return &rpcError{-32602, "unknown tool: " + p.Name}
}

func safeCall(ctx context.Context, t Tool, args map[string]any) (out any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("internal error: %v", r)
		}
	}()
	return t.Handler(ctx, args)
}

func textResult(text string, isError bool) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isError}
}

// --- argument helpers ------------------------------------------------------------

// Str returns a string argument or "".
func Str(args map[string]any, k string) string {
	if v, ok := args[k].(string); ok {
		return v
	}
	return ""
}

// Int returns an integer argument or def.
func Int(args map[string]any, k string, def int64) int64 {
	switch v := args[k].(type) {
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return def
}
