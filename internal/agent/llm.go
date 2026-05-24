// Real LLM-backed Engine implementations: BedrockEngine (Anthropic Claude via
// AWS Bedrock Converse API) and MistralEngine (Mistral chat completions).
// FallbackEngine chains engines and returns the first non-error reply.
//
// None of these implementations are stateful. Conversation history comes in
// via Request.History; persistence is the caller's responsibility (typically
// internal/store). That keeps the engines composable and unit-testable.

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNoAPIKey is returned by LLM engines when the token/key is empty. Callers
// can check with errors.Is to fall back to MockEngine in keyless dev.
var ErrNoAPIKey = errors.New("agent: no API key configured")

const defaultHTTPTimeout = 30 * time.Second

// ---- Bedrock (AWS Bedrock Converse API, Claude models) ----

type BedrockEngine struct {
	Token   string       // AWS_BEARER_TOKEN_BEDROCK
	Model   string       // e.g. "us.anthropic.claude-haiku-4-5-20251001-v1:0"
	BaseURL string       // default https://bedrock-runtime.us-east-1.amazonaws.com
	Client  *http.Client // optional; defaults to a 30s client
}

type bedrockMessage struct {
	Role    string                   `json:"role"`
	Content []map[string]interface{} `json:"content"`
}

func (b *BedrockEngine) Respond(ctx context.Context, req Request) (Reply, error) {
	if b.Token == "" {
		return Reply{}, ErrNoAPIKey
	}
	base := b.BaseURL
	if base == "" {
		base = "https://bedrock-runtime.us-east-1.amazonaws.com"
	}
	base = strings.TrimRight(base, "/")

	messages := make([]bedrockMessage, 0, len(req.History)+1)
	for _, m := range req.History {
		messages = append(messages, bedrockMessage{
			Role:    roleToBedrock(m.Role),
			Content: []map[string]interface{}{{"text": m.Text}},
		})
	}
	messages = append(messages, bedrockMessage{
		Role:    "user",
		Content: []map[string]interface{}{{"text": req.Incoming.Text}},
	})

	body := map[string]interface{}{"messages": messages}
	if req.SystemPrompt != "" {
		body["system"] = []map[string]interface{}{{"text": req.SystemPrompt}}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Reply{}, err
	}

	url := fmt.Sprintf("%s/model/%s/converse", base, b.Model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return Reply{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+b.Token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := b.client().Do(httpReq)
	if err != nil {
		return Reply{}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return Reply{}, fmt.Errorf("bedrock %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var parsed struct {
		Output struct {
			Message bedrockMessage `json:"message"`
		} `json:"output"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Reply{}, fmt.Errorf("decode bedrock response: %w", err)
	}
	var out strings.Builder
	for _, c := range parsed.Output.Message.Content {
		if s, ok := c["text"].(string); ok {
			out.WriteString(s)
		}
	}
	if out.Len() == 0 {
		return Reply{}, fmt.Errorf("empty bedrock reply")
	}
	return Reply{Text: out.String()}, nil
}

func (b *BedrockEngine) client() *http.Client {
	if b.Client != nil {
		return b.Client
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

// ---- Mistral chat completions ----

type MistralEngine struct {
	Token   string
	Model   string // e.g. "mistral-small-latest"
	BaseURL string // default https://api.mistral.ai
	Client  *http.Client
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (m *MistralEngine) Respond(ctx context.Context, req Request) (Reply, error) {
	if m.Token == "" {
		return Reply{}, ErrNoAPIKey
	}
	base := m.BaseURL
	if base == "" {
		base = "https://api.mistral.ai"
	}
	base = strings.TrimRight(base, "/")

	msgs := make([]chatMessage, 0, len(req.History)+2)
	if req.SystemPrompt != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: req.SystemPrompt})
	}
	for _, h := range req.History {
		msgs = append(msgs, chatMessage{Role: roleToOpenAI(h.Role), Content: h.Text})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: req.Incoming.Text})

	raw, err := json.Marshal(map[string]interface{}{
		"model":    m.Model,
		"messages": msgs,
	})
	if err != nil {
		return Reply{}, err
	}

	url := base + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return Reply{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+m.Token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := m.client().Do(httpReq)
	if err != nil {
		return Reply{}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return Reply{}, fmt.Errorf("mistral %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}
	var parsed struct {
		Choices []struct {
			Message chatMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Reply{}, fmt.Errorf("decode mistral response: %w", err)
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return Reply{}, fmt.Errorf("empty mistral reply")
	}
	return Reply{Text: parsed.Choices[0].Message.Content}, nil
}

func (m *MistralEngine) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

// ---- Fallback ----

// FallbackEngine tries each engine in order, returning the first non-error
// reply. If every engine errors, returns the last error.
type FallbackEngine struct {
	Engines []Engine
}

func (f FallbackEngine) Respond(ctx context.Context, req Request) (Reply, error) {
	if len(f.Engines) == 0 {
		return Reply{}, fmt.Errorf("agent: empty fallback chain")
	}
	var lastErr error
	for _, e := range f.Engines {
		r, err := e.Respond(ctx, req)
		if err == nil {
			return r, nil
		}
		lastErr = err
	}
	return Reply{}, lastErr
}

// ---- helpers ----

func roleToBedrock(r Role) string {
	if r == RoleAssistant {
		return "assistant"
	}
	return "user"
}

func roleToOpenAI(r Role) string {
	if r == RoleAssistant {
		return "assistant"
	}
	return "user"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
