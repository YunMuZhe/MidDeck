package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/changeset"
	"github.com/YunMuZhe/MidDeck/internal/i18n"
	"github.com/zalando/go-keyring"
)

const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai" // any OpenAI-compatible chat completions endpoint

	DefaultAnthropicModel = "claude-opus-5-5"

	keyringService = "MidDeck"
	keyringKey     = "ai/apikey"
)

// Settings configure the assistant. APIKey lives in the OS keychain.
type Settings struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	Model    string `json:"model"`
	APIKey   string `json:"apiKey,omitempty"`
	// Proxy is an optional http://, https:// or socks5:// URL for reaching the provider.
	Proxy string `json:"proxy,omitempty"`
	// SendAddresses includes instance addresses in the context sent to the model.
	SendAddresses bool `json:"sendAddresses"`
	// HasKey is set when listing settings; the key itself is never sent to the UI.
	HasKey bool `json:"hasKey"`
}

type settingsFile struct {
	Settings
	// FileKey holds the key only when the keychain is unavailable.
	FileKey string `json:"fileKey,omitempty"`
}

// useKeychain is switched off in tests so they never touch the developer's keychain.
var useKeychain = true

func settingsPath(dir string) string { return filepath.Join(dir, "ai.json") }

// Load reads settings including the API key.
func Load(dir string) (Settings, error) {
	var f settingsFile
	b, err := os.ReadFile(settingsPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return Settings{Provider: ProviderAnthropic, Model: DefaultAnthropicModel}, nil
	}
	if err != nil {
		return Settings{}, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return Settings{}, err
	}
	s := f.Settings
	s.APIKey = f.FileKey
	if s.APIKey == "" && useKeychain {
		s.APIKey, _ = keyring.Get(keyringService, keyringKey)
	}
	s.HasKey = s.APIKey != ""
	return s, nil
}

// Save writes settings. An empty APIKey keeps the stored one.
func Save(dir string, s Settings) error {
	if s.APIKey == "" {
		if old, err := Load(dir); err == nil {
			s.APIKey = old.APIKey
		}
	}
	f := settingsFile{Settings: s}
	f.APIKey, f.HasKey = "", false
	if s.APIKey != "" {
		if !useKeychain || keyring.Set(keyringService, keyringKey, s.APIKey) != nil {
			f.FileKey = s.APIKey
		}
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(settingsPath(dir), b, 0o600)
}

// InstanceDigest is what the model learns about one instance. No secrets.
type InstanceDigest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Project     string   `json:"project,omitempty"`
	Env         string   `json:"env,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
	Description string   `json:"description,omitempty"`
	ReadOnly    bool     `json:"readOnly,omitempty"`
	Addrs       []string `json:"addrs,omitempty"`
}

// BastionDigest is what the model learns about a shared jump host.
type BastionDigest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Host        string `json:"host,omitempty"`
}

// Context is the workspace snapshot the model chooses from.
type Context struct {
	Instances []InstanceDigest `json:"instances"`
	Bastions  []BastionDigest  `json:"jumpHosts"`
	// Current is the id of the instance open in the UI, if any.
	Current string `json:"currentInstance,omitempty"`
}

// Action is the model's proposal.
type Action struct {
	Name string         `json:"action"`
	Args map[string]any `json:"args"`
	// Text is any prose the model wrote alongside (or instead of) the tool call.
	Text string `json:"text,omitempty"`
}

const systemPrompt = `You are the assistant inside MidDeck, a desktop client for Kafka, Redis and RabbitMQ.
The user describes what they want in one sentence. Pick the single action that best serves it by calling exactly one tool.

How MidDeck uses your answer: the action only opens a form or view with your values filled in. The user reviews it and confirms there, so prefer opening the right form over asking questions. Use the reply tool only when the target instance is genuinely ambiguous or the request is impossible.

Rules:
- Choose instances only from the provided list and copy their id exactly. Match on name, aliases, project, environment, tags and description. "prod", "production", "生产", "线上" mean env=prod; "测试" means test; "预发" means staging.
- If the user does not name an instance and one is open (currentInstance), use it when its kind fits.
- Only fill values the user stated or that follow directly from them (e.g. "7 days" retention = 604800000 ms). Leave everything else out.
- Never include passwords or secrets.
- Write any message in the language the user wrote in.

Changeset format, for the changeset_draft tool:
` + "\n" + changeset.Reference

// Plan asks the configured model for one action.
func Plan(ctx context.Context, s Settings, wc Context, prompt string) (Action, error) {
	if s.APIKey == "" && s.Provider != ProviderOpenAI {
		return Action{}, errors.New(i18n.T("ai.noKey"))
	}
	if strings.TrimSpace(s.Model) == "" {
		return Action{}, errors.New(i18n.T("ai.noModel"))
	}
	hc, err := httpClient(s.Proxy)
	if err != nil {
		return Action{}, err
	}
	ctxJSON, _ := json.Marshal(wc)
	user := "Workspace:\n" + string(ctxJSON) + "\n\nNow: " + time.Now().Format(time.RFC3339) + "\n\nRequest: " + prompt
	var a Action
	switch s.Provider {
	case ProviderOpenAI:
		a, err = planOpenAI(ctx, hc, s, user)
	default:
		a, err = planAnthropic(ctx, hc, s, user)
	}
	if err != nil {
		return a, err
	}
	return a, validate(a, wc)
}

// validate rejects actions that point at instances or jump hosts that don't exist.
func validate(a Action, wc Context) error {
	known := func(id string) bool {
		for _, in := range wc.Instances {
			if in.ID == id {
				return true
			}
		}
		return false
	}
	if a.Name == "" {
		return errors.New(i18n.T("ai.noAction"))
	}
	if id, ok := a.Args["instance_id"].(string); ok && !known(id) {
		return errors.New(i18n.T("ai.unknownInstance", id))
	}
	if a.Name == "changeset_draft" {
		// A draft that does not parse would only fail later in the dialog.
		text, _ := a.Args["yaml"].(string)
		if _, err := changeset.Parse([]byte(text)); err != nil {
			return fmt.Errorf("%s: %w", i18n.T("ai.badChangeset"), err)
		}
		for _, in := range wc.Instances {
			if in.ID == a.Args["instance_id"] && in.Kind == "redis" {
				return errors.New(i18n.T("changeset.unsupported", in.Kind))
			}
		}
	}
	if id, ok := a.Args["bastion_id"].(string); ok && id != "" {
		found := false
		for _, b := range wc.Bastions {
			found = found || b.ID == id
		}
		if !found {
			delete(a.Args, "bastion_id")
		}
	}
	return nil
}

func httpClient(proxy string) (*http.Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	var configured *url.URL
	if proxy = strings.TrimSpace(proxy); proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("%s: %q", i18n.T("ai.badProxy"), proxy)
		}
		configured = u
	}
	tr.Proxy = proxyRoute(configured)
	return &http.Client{Transport: routeErrors{tr}, Timeout: 120 * time.Second}, nil
}

// routeErrors says in a failed request's error whether it went through a proxy,
// so "the browser can reach it but MidDeck cannot" is easy to diagnose.
type routeErrors struct{ tr *http.Transport }

func (r routeErrors) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := r.tr.RoundTrip(req)
	if err != nil {
		if p, _ := r.tr.Proxy(req); p != nil {
			err = fmt.Errorf("%w (%s)", err, i18n.T("ai.viaProxy", p.Redacted()))
		} else {
			err = fmt.Errorf("%w (%s)", err, i18n.T("ai.direct"))
		}
	}
	return res, err
}

var effortModels = regexp.MustCompile(`^claude-(opus|sonnet|haiku|fable|mythos)-(5|4-[6-9])`)

// supportsEffort reports whether output_config.effort is accepted by the model.
func supportsEffort(model string) bool { return effortModels.MatchString(model) }

func isFirstParty(baseURL string) bool {
	return baseURL == "" || strings.Contains(baseURL, "api.anthropic.com")
}
