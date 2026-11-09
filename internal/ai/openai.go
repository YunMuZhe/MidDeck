package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/YunMuZhe/MidDeck/internal/i18n"
)

// planOpenAI calls an OpenAI-compatible /chat/completions endpoint (OpenAI, DeepSeek,
// Qwen/DashScope, Moonshot, Ollama, vLLM, LiteLLM, ...).
func planOpenAI(ctx context.Context, hc *http.Client, s Settings, user string) (Action, error) {
	base := strings.TrimRight(s.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	tools := make([]map[string]any, 0, len(Tools))
	for _, t := range Tools {
		tools = append(tools, map[string]any{
			"type":     "function",
			"function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.schema()},
		})
	}
	body := map[string]any{
		"model": s.Model,
		"messages": []map[string]any{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": user},
		},
		"tools":       tools,
		"tool_choice": "required",
	}
	a, status, err := postChat(ctx, hc, base, s.APIKey, body)
	if err != nil && status == http.StatusBadRequest {
		// Some compatible servers don't implement tool_choice=required.
		body["tool_choice"] = "auto"
		a, _, err = postChat(ctx, hc, base, s.APIKey, body)
	}
	return a, err
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func postChat(ctx context.Context, hc *http.Client, base, key string, body map[string]any) (Action, int, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return Action{}, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Action{}, 0, fmt.Errorf("%s: %w", i18n.T("ai.unreachable"), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var cr chatResponse
	_ = json.Unmarshal(raw, &cr)
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return Action{}, resp.StatusCode, errors.New(i18n.T("ai.badKey"))
		case http.StatusNotFound:
			return Action{}, resp.StatusCode, errors.New(i18n.T("ai.notFound"))
		case http.StatusTooManyRequests:
			return Action{}, resp.StatusCode, errors.New(i18n.T("ai.rateLimited"))
		}
		msg := strings.TrimSpace(string(raw))
		if cr.Error != nil && cr.Error.Message != "" {
			msg = cr.Error.Message
		}
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		return Action{}, resp.StatusCode, fmt.Errorf("%s (HTTP %d): %s", i18n.T("ai.apiError"), resp.StatusCode, msg)
	}
	if len(cr.Choices) == 0 {
		return Action{}, resp.StatusCode, errors.New(i18n.T("ai.badOutput"))
	}
	m := cr.Choices[0].Message
	a := Action{Text: strings.TrimSpace(m.Content)}
	if len(m.ToolCalls) > 0 {
		fn := m.ToolCalls[0].Function
		a.Name = fn.Name
		if err := json.Unmarshal([]byte(fn.Arguments), &a.Args); err != nil {
			return Action{}, resp.StatusCode, fmt.Errorf("%s: %w", i18n.T("ai.badOutput"), err)
		}
	} else if a.Text != "" {
		a = Action{Name: "reply", Args: map[string]any{"message": a.Text}}
	}
	return a, resp.StatusCode, nil
}
