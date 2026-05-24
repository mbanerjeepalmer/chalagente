package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Replier interface {
	Reply(ctx context.Context, chat, text string) (string, bool, error)
}

type scriptedReplier struct {
	mu        sync.Mutex
	idx       map[string]int
	responses []string
}

func newScriptedReplier(responses []string) *scriptedReplier {
	return &scriptedReplier{idx: map[string]int{}, responses: responses}
}

func (s *scriptedReplier) Reply(_ context.Context, chat, _ string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.idx[chat]
	if i >= len(s.responses) {
		return "", false, nil
	}
	s.idx[chat] = i + 1
	return s.responses[i], true, nil
}

func (s *scriptedReplier) Snapshot() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.idx))
	for k, v := range s.idx {
		out[k] = v
	}
	return out
}

func (s *scriptedReplier) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idx = map[string]int{}
}

type bedrockMessage struct {
	Role    string                   `json:"role"`
	Content []map[string]interface{} `json:"content"`
}

type bedrockReplier struct {
	endpoint     string // e.g. https://bedrock-runtime.us-east-1.amazonaws.com
	token        string
	model        string
	systemPrompt string
	httpClient   *http.Client

	mu      sync.Mutex
	history map[string][]bedrockMessage
}

func newBedrockReplier(endpoint, token, model, systemPrompt string) *bedrockReplier {
	return &bedrockReplier{
		endpoint:     strings.TrimRight(endpoint, "/"),
		token:        token,
		model:        model,
		systemPrompt: systemPrompt,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		history:      map[string][]bedrockMessage{},
	}
}

func (b *bedrockReplier) historyFor(chat string) []bedrockMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]bedrockMessage(nil), b.history[chat]...)
}

func (b *bedrockReplier) Reply(ctx context.Context, chat, text string) (string, bool, error) {
	b.mu.Lock()
	hist := append([]bedrockMessage(nil), b.history[chat]...)
	b.mu.Unlock()

	hist = append(hist, bedrockMessage{
		Role:    "user",
		Content: []map[string]interface{}{{"text": text}},
	})

	body := map[string]interface{}{"messages": hist}
	if b.systemPrompt != "" {
		body["system"] = []map[string]interface{}{{"text": b.systemPrompt}}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", false, err
	}

	url := fmt.Sprintf("%s/model/%s/converse", b.endpoint, b.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := b.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", false, fmt.Errorf("bedrock %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Output struct {
			Message bedrockMessage `json:"message"`
		} `json:"output"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", false, fmt.Errorf("decode bedrock response: %w", err)
	}
	var out strings.Builder
	for _, c := range parsed.Output.Message.Content {
		if s, ok := c["text"].(string); ok {
			out.WriteString(s)
		}
	}
	reply := out.String()
	if reply == "" {
		return "", false, fmt.Errorf("empty bedrock reply")
	}

	b.mu.Lock()
	if b.history == nil {
		b.history = map[string][]bedrockMessage{}
	}
	b.history[chat] = append(b.history[chat], hist[len(hist)-1], parsed.Output.Message)
	b.mu.Unlock()

	return reply, true, nil
}
