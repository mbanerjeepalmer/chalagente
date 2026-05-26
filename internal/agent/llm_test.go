package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---- Bedrock ----

func TestBedrockEngineNoTokenReturnsErrNoAPIKey(t *testing.T) {
	e := &BedrockEngine{Model: "claude"}
	_, err := e.Respond(context.Background(), Request{Incoming: Message{Text: "hola"}})
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("want ErrNoAPIKey, got %v", err)
	}
}

func TestBedrockEngineHitsRightEndpointAndReturnsReply(t *testing.T) {
	var (
		gotPath  string
		gotAuth  string
		gotBody  map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"text":"hola, ¿en qué puedo ayudarte?"}]}}}`))
	}))
	defer srv.Close()

	e := &BedrockEngine{
		BaseURL: srv.URL,
		Token:   "test-token",
		Model:   "us.anthropic.claude-haiku-4-5-20251001-v1:0",
	}
	reply, err := e.Respond(context.Background(), Request{
		SystemPrompt: "Eres un asistente.",
		Incoming:     Message{Role: RoleUser, Text: "hola"},
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if reply.Text != "hola, ¿en qué puedo ayudarte?" {
		t.Fatalf("text: %q", reply.Text)
	}
	if !strings.Contains(gotPath, "/model/") || !strings.HasSuffix(gotPath, "/converse") {
		t.Fatalf("path: %q", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("auth header: %q", gotAuth)
	}
	if _, ok := gotBody["system"]; !ok {
		t.Fatalf("expected system block in body; got %v", gotBody)
	}
}

func TestBedrockEngineSendsImageContentBlock(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"text":"veo un taco"}]}}}`))
	}))
	defer srv.Close()
	e := &BedrockEngine{BaseURL: srv.URL, Token: "x", Model: "m"}
	imgBytes := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic prefix is enough — we don't decode here.
	reply, err := e.Respond(context.Background(), Request{
		Incoming: Message{
			Role: RoleUser,
			Text: "¿qué ves?",
			Attachments: []Attachment{{
				Kind:     "image",
				MimeType: "image/png",
				Bytes:    imgBytes,
			}},
		},
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if reply.Text != "veo un taco" {
		t.Fatalf("text: %q", reply.Text)
	}
	msgs, ok := gotBody["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages shape: %v", gotBody)
	}
	content, _ := msgs[0].(map[string]any)["content"].([]any)
	var sawImage, sawText bool
	for _, blk := range content {
		m, _ := blk.(map[string]any)
		if img, ok := m["image"].(map[string]any); ok {
			sawImage = true
			if img["format"] != "png" {
				t.Errorf("image format = %v want png", img["format"])
			}
			if src, _ := img["source"].(map[string]any); src["bytes"] == nil {
				t.Errorf("missing source.bytes")
			}
		}
		if _, ok := m["text"]; ok {
			sawText = true
		}
	}
	if !sawImage {
		t.Errorf("no image content block in payload: %v", content)
	}
	if !sawText {
		t.Errorf("no text content block in payload: %v", content)
	}
}

func TestBedrockEngineEmptyCaptionAddsFallbackText(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"text":"ok"}]}}}`))
	}))
	defer srv.Close()
	e := &BedrockEngine{BaseURL: srv.URL, Token: "x", Model: "m"}
	_, err := e.Respond(context.Background(), Request{
		Incoming: Message{
			Role: RoleUser,
			Text: "",
			Attachments: []Attachment{{Kind: "image", MimeType: "image/jpeg", Bytes: []byte{0xff, 0xd8}}},
		},
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	msgs, _ := gotBody["messages"].([]any)
	content, _ := msgs[0].(map[string]any)["content"].([]any)
	var textBlock string
	for _, blk := range content {
		m, _ := blk.(map[string]any)
		if t, ok := m["text"].(string); ok {
			textBlock = t
		}
	}
	if textBlock == "" {
		t.Fatalf("expected fallback text block when caption empty")
	}
}

func TestBedrockEngineSurfacesHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
	}))
	defer srv.Close()
	e := &BedrockEngine{BaseURL: srv.URL, Token: "x", Model: "m"}
	_, err := e.Respond(context.Background(), Request{Incoming: Message{Text: "hola"}})
	if err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestBedrockEnginePassesHistory(t *testing.T) {
	var gotMsgs []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		gotMsgs, _ = body["messages"].([]any)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"message":{"content":[{"text":"ok"}]}}}`))
	}))
	defer srv.Close()
	e := &BedrockEngine{BaseURL: srv.URL, Token: "x", Model: "m"}
	_, err := e.Respond(context.Background(), Request{
		History: []Message{
			{Role: RoleUser, Text: "hola"},
			{Role: RoleAssistant, Text: "hola, ¿en qué te ayudo?"},
		},
		Incoming: Message{Role: RoleUser, Text: "¿a qué hora abren?"},
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	// 2 history + 1 incoming = 3 messages
	if len(gotMsgs) != 3 {
		t.Fatalf("messages count: got %d, want 3", len(gotMsgs))
	}
}

// ---- Mistral ----

func TestMistralEngineNoTokenReturnsErrNoAPIKey(t *testing.T) {
	e := &MistralEngine{Model: "mistral-small-latest"}
	_, err := e.Respond(context.Background(), Request{Incoming: Message{Text: "hola"}})
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("want ErrNoAPIKey, got %v", err)
	}
}

func TestMistralEngineHitsRightEndpointAndReturnsReply(t *testing.T) {
	var (
		gotPath string
		gotAuth string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hola"}}]}`))
	}))
	defer srv.Close()

	e := &MistralEngine{BaseURL: srv.URL, Token: "t", Model: "mistral-small-latest"}
	reply, err := e.Respond(context.Background(), Request{
		SystemPrompt: "sé útil",
		Incoming:     Message{Role: RoleUser, Text: "hola"},
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if reply.Text != "hola" {
		t.Fatalf("text: %q", reply.Text)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path: %q", gotPath)
	}
	if gotAuth != "Bearer t" {
		t.Fatalf("auth: %q", gotAuth)
	}
}

func TestMistralEngineSurfacesHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`rate limited`))
	}))
	defer srv.Close()
	e := &MistralEngine{BaseURL: srv.URL, Token: "x", Model: "m"}
	_, err := e.Respond(context.Background(), Request{Incoming: Message{Text: "hola"}})
	if err == nil {
		t.Fatal("expected error on 429")
	}
}

// ---- Fallback ----

func TestFallbackEngineUsesFirstSuccess(t *testing.T) {
	first := fakeEngine{err: errors.New("upstream down")}
	second := fakeEngine{reply: "fallback win"}
	chain := FallbackEngine{Engines: []Engine{first, second}}
	r, err := chain.Respond(context.Background(), Request{Incoming: Message{Text: "x"}})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if r.Text != "fallback win" {
		t.Fatalf("text: %q", r.Text)
	}
}

func TestFallbackEngineEmptyReturnsError(t *testing.T) {
	chain := FallbackEngine{}
	_, err := chain.Respond(context.Background(), Request{Incoming: Message{Text: "x"}})
	if err == nil {
		t.Fatal("want error from empty chain")
	}
}

func TestFallbackEngineAllErrorsReturnsLast(t *testing.T) {
	chain := FallbackEngine{Engines: []Engine{
		fakeEngine{err: errors.New("a")},
		fakeEngine{err: errors.New("b")},
	}}
	_, err := chain.Respond(context.Background(), Request{Incoming: Message{Text: "x"}})
	if err == nil {
		t.Fatal("want error")
	}
}

type fakeEngine struct {
	reply string
	err   error
}

func (f fakeEngine) Respond(_ context.Context, _ Request) (Reply, error) {
	if f.err != nil {
		return Reply{}, f.err
	}
	return Reply{Text: f.reply}, nil
}

// Ensure tests don't hang on a runaway server.
func init() {
	http.DefaultClient.Timeout = 5 * time.Second
}
