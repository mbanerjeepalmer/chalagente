package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScriptedReplier_AdvancesPerChat(t *testing.T) {
	s := newScriptedReplier([]string{"one", "two", "three"})
	ctx := context.Background()

	got, ok, err := s.Reply(ctx, "alice", "hi")
	if err != nil || !ok || got != "one" {
		t.Fatalf("alice #1: got %q ok=%v err=%v", got, ok, err)
	}
	got, ok, _ = s.Reply(ctx, "alice", "again")
	if !ok || got != "two" {
		t.Fatalf("alice #2: got %q ok=%v", got, ok)
	}
	got, ok, _ = s.Reply(ctx, "bob", "hello")
	if !ok || got != "one" {
		t.Fatalf("bob #1: got %q ok=%v", got, ok)
	}
}

func TestScriptedReplier_Exhausted(t *testing.T) {
	s := newScriptedReplier([]string{"only"})
	ctx := context.Background()
	_, _, _ = s.Reply(ctx, "x", "")
	_, ok, err := s.Reply(ctx, "x", "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Fatalf("expected exhausted, got ok=true")
	}
}

func TestScriptedReplier_Reset(t *testing.T) {
	s := newScriptedReplier([]string{"a", "b"})
	ctx := context.Background()
	_, _, _ = s.Reply(ctx, "c", "")
	s.Reset()
	if len(s.Snapshot()) != 0 {
		t.Fatalf("expected empty snapshot after reset")
	}
	got, ok, _ := s.Reply(ctx, "c", "")
	if !ok || got != "a" {
		t.Fatalf("expected first response after reset, got %q ok=%v", got, ok)
	}
}

func TestApp_DefaultModeScripted(t *testing.T) {
	a := newApp(nil)
	if m := a.mode(); m != "scripted" {
		t.Fatalf("default mode = %q, want scripted", m)
	}
}

func TestApp_SetMode(t *testing.T) {
	a := newApp(nil)
	if err := a.setMode("agent"); err != nil {
		t.Fatalf("setMode agent: %v", err)
	}
	if m := a.mode(); m != "agent" {
		t.Fatalf("mode = %q, want agent", m)
	}
	if err := a.setMode("nope"); err == nil {
		t.Fatalf("expected error for bad mode")
	}
}

type fakeReplier struct {
	calls []struct{ chat, text string }
	reply string
}

func (f *fakeReplier) Reply(_ context.Context, chat, text string) (string, bool, error) {
	f.calls = append(f.calls, struct{ chat, text string }{chat, text})
	return f.reply, true, nil
}

func TestApp_RespondScriptedMode(t *testing.T) {
	a := newApp(nil)
	a.scripted = newScriptedReplier([]string{"scripted-1"})
	a.agent = &fakeReplier{reply: "agent-1"}
	got, ok, err := a.reply(context.Background(), "chat1", "hi")
	if err != nil || !ok || got != "scripted-1" {
		t.Fatalf("scripted dispatch: got %q ok=%v err=%v", got, ok, err)
	}
}

func TestApp_RespondAgentMode(t *testing.T) {
	a := newApp(nil)
	a.scripted = newScriptedReplier([]string{"scripted-1"})
	fake := &fakeReplier{reply: "agent-1"}
	a.agent = fake
	_ = a.setMode("agent")
	got, ok, err := a.reply(context.Background(), "chat1", "hello agent")
	if err != nil || !ok || got != "agent-1" {
		t.Fatalf("agent dispatch: got %q ok=%v err=%v", got, ok, err)
	}
	if len(fake.calls) != 1 || fake.calls[0].text != "hello agent" {
		t.Fatalf("agent not called with text, calls=%+v", fake.calls)
	}
}

func TestBedrockReplier_RequestAndResponse(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"text":"hola amigo"}]}}}`))
	}))
	defer srv.Close()

	br := &bedrockReplier{
		endpoint:     srv.URL,
		token:        "tok-123",
		model:        "anthropic.claude-haiku-4-5-20251001-v1:0",
		systemPrompt: "be brief",
		httpClient:   srv.Client(),
	}
	got, ok, err := br.Reply(context.Background(), "chatX", "hello")
	if err != nil || !ok || got != "hola amigo" {
		t.Fatalf("got %q ok=%v err=%v", got, ok, err)
	}
	if !strings.Contains(gotPath, "/model/anthropic.claude-haiku-4-5-20251001-v1:0/converse") {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok-123" {
		t.Fatalf("auth = %q", gotAuth)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatalf("body not json: %v\n%s", err, gotBody)
	}
	msgs, _ := payload["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d: %v", len(msgs), payload)
	}
	if _, hasSys := payload["system"]; !hasSys {
		t.Fatalf("expected system block in body: %v", payload)
	}
}

func TestBedrockReplier_MultiTurnHistory(t *testing.T) {
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		lastBody = string(b)
		_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"text":"ack"}]}}}`))
	}))
	defer srv.Close()

	br := &bedrockReplier{endpoint: srv.URL, token: "t", model: "m", httpClient: srv.Client()}
	ctx := context.Background()
	_, _, _ = br.Reply(ctx, "c", "msg1")
	_, _, _ = br.Reply(ctx, "c", "msg2")

	var payload map[string]any
	_ = json.Unmarshal([]byte(lastBody), &payload)
	msgs, _ := payload["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages after 2 turns, got %d: %s", len(msgs), lastBody)
	}
}

func TestBedrockReplier_PerChatHistoryIsolated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"text":"x"}]}}}`))
	}))
	defer srv.Close()
	br := &bedrockReplier{endpoint: srv.URL, token: "t", model: "m", httpClient: srv.Client()}
	ctx := context.Background()
	_, _, _ = br.Reply(ctx, "alice", "hi")
	_, _, _ = br.Reply(ctx, "bob", "hi")
	if got := len(br.historyFor("alice")); got != 2 {
		t.Fatalf("alice history = %d, want 2", got)
	}
	if got := len(br.historyFor("bob")); got != 2 {
		t.Fatalf("bob history = %d, want 2", got)
	}
}
