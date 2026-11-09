package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var wc = Context{Instances: []InstanceDigest{{ID: "abc123", Name: "Orders Kafka", Kind: "kafka", Env: "prod"}}}

func TestPlanAnthropic(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || (r.Header.Get("x-api-key") != "k" && r.Header.Get("Authorization") != "Bearer k") {
			t.Errorf("unexpected request %s headers=%v", r.URL.Path, r.Header)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5",
			"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1},
			"content":[{"type":"tool_use","id":"tu_1","name":"kafka_create_topic",
			"input":{"instance_id":"abc123","name":"order-refund","partitions":12}}]}`)
	}))
	defer srv.Close()

	a, err := Plan(context.Background(), Settings{Provider: ProviderAnthropic, BaseURL: srv.URL, Model: "claude-opus-5-5", APIKey: "k"}, wc, "建个 topic")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "kafka_create_topic" || a.Args["name"] != "order-refund" || a.Args["partitions"].(float64) != 12 {
		t.Fatalf("action: %+v", a)
	}
	if tc, _ := got["tool_choice"].(map[string]any); tc["type"] != "auto" {
		t.Fatalf("tool_choice must be auto for current models: %v", got["tool_choice"])
	}
	if oc, _ := got["output_config"].(map[string]any); oc["effort"] != "low" {
		t.Fatalf("effort not set: %v", got["output_config"])
	}
	if _, ok := got["fallbacks"]; ok {
		t.Fatal("fallbacks must only be sent to the first-party API")
	}
}

func TestPlanOpenAIFallsBackToAuto(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["tool_choice"] == "required" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"tool_choice required unsupported"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"","tool_calls":[{"type":"function",
			"function":{"name":"open_instance","arguments":"{\"instance_id\":\"abc123\",\"tab\":\"groups\"}"}}]}}]}`)
	}))
	defer srv.Close()

	a, err := Plan(context.Background(), Settings{Provider: ProviderOpenAI, BaseURL: srv.URL, Model: "qwen-plus", APIKey: "k"}, wc, "open orders")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || a.Name != "open_instance" || a.Args["tab"] != "groups" {
		t.Fatalf("calls=%d action=%+v", calls, a)
	}
}

func TestPlanRejectsUnknownInstance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"tool_calls":[{"function":{"name":"open_instance","arguments":"{\"instance_id\":\"nope\"}"}}]}}]}`)
	}))
	defer srv.Close()
	_, err := Plan(context.Background(), Settings{Provider: ProviderOpenAI, BaseURL: srv.URL, Model: "m"}, wc, "x")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("want unknown instance error, got %v", err)
	}
}

func TestSettingsKeepKey(t *testing.T) {
	useKeychain = false
	defer func() { useKeychain = true }()
	dir := t.TempDir()
	if err := Save(dir, Settings{Provider: ProviderOpenAI, Model: "m", APIKey: "middeck-test-key"}); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, Settings{Provider: ProviderOpenAI, Model: "m2"}); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil || s.Model != "m2" || s.APIKey != "middeck-test-key" || !s.HasKey {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestSupportsEffort(t *testing.T) {
	for m, want := range map[string]bool{"claude-opus-5-5": true, "claude-sonnet-4-6": true, "claude-haiku-4-5": false, "deepseek-chat": false} {
		if supportsEffort(m) != want {
			t.Errorf("%s", m)
		}
	}
}
