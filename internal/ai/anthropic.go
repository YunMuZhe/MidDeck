package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/YunMuZhe/MidDeck/internal/i18n"
	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// Models that accept the server-side refusal fallback ("fallbacks": "default").
var fallbackModels = map[string]bool{
	"claude-fable-5-1": true, "claude-opus-5-5": true, "claude-opus-5": true, "claude-sonnet-5-5": true,
}

func planAnthropic(ctx context.Context, hc *http.Client, s Settings, user string) (Action, error) {
	opts := []option.RequestOption{option.WithAPIKey(s.APIKey), option.WithHTTPClient(hc), option.WithMaxRetries(2)}
	// Always explicit, so an ANTHROPIC_BASE_URL in the user's shell doesn't silently redirect requests.
	base := s.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	opts = append(opts, option.WithBaseURL(base))
	if !isFirstParty(s.BaseURL) {
		// Anthropic-compatible gateways commonly read the key from x-api-key.
		opts = append(opts, option.WithHeader("x-api-key", s.APIKey))
	}
	if isFirstParty(s.BaseURL) && fallbackModels[s.Model] {
		// On a policy refusal the API re-runs the request on a fallback model it picks.
		opts = append(opts,
			option.WithHeader("anthropic-beta", "server-side-fallback-2026-07-01"),
			option.WithJSONSet("fallbacks", "default"))
	}
	client := anthropic.NewClient(opts...)

	tools := make([]anthropic.ToolUnionParam, 0, len(Tools))
	for _, t := range Tools {
		tp := anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties:  t.Properties,
				Required:    t.Required,
				ExtraFields: map[string]any{"additionalProperties": false},
			},
		}
		if isFirstParty(s.BaseURL) {
			tp.Strict = anthropic.Bool(true)
		}
		tools = append(tools, anthropic.ToolUnionParam{OfTool: &tp})
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(s.Model),
		MaxTokens: 8192,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(user))},
		Tools:     tools,
		// Forced tool choice is rejected by current models; auto + the prompt steers it.
		ToolChoice: anthropic.ToolChoiceUnionParam{OfAuto: &anthropic.ToolChoiceAutoParam{DisableParallelToolUse: anthropic.Bool(true)}},
	}
	if supportsEffort(s.Model) {
		params.OutputConfig = anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffortLow}
	}

	resp, err := client.Messages.New(ctx, params)
	if err != nil {
		return Action{}, friendly(err)
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return Action{}, errors.New(i18n.T("ai.refused"))
	}
	var a Action
	var text []string
	for _, block := range resp.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			text = append(text, b.Text)
		case anthropic.ToolUseBlock:
			if a.Name != "" {
				continue
			}
			a.Name = b.Name
			if err := json.Unmarshal([]byte(b.JSON.Input.Raw()), &a.Args); err != nil {
				return Action{}, fmt.Errorf("%s: %w", i18n.T("ai.badOutput"), err)
			}
		}
	}
	a.Text = strings.TrimSpace(strings.Join(text, "\n"))
	if a.Name == "" && a.Text != "" {
		a = Action{Name: "reply", Args: map[string]any{"message": a.Text}}
	}
	return a, nil
}

// friendly maps API errors to short, actionable messages.
func friendly(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return errors.New(i18n.T("ai.badKey"))
		case http.StatusNotFound:
			return errors.New(i18n.T("ai.notFound"))
		case http.StatusTooManyRequests:
			return errors.New(i18n.T("ai.rateLimited"))
		}
		return fmt.Errorf("%s (HTTP %d): %w", i18n.T("ai.apiError"), apiErr.StatusCode, err)
	}
	return fmt.Errorf("%s: %w", i18n.T("ai.unreachable"), err)
}
