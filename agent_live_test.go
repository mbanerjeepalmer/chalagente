//go:build live

package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveBedrock(t *testing.T) {
	tok := os.Getenv("AWS_BEARER_TOKEN_BEDROCK")
	if tok == "" {
		t.Skip("AWS_BEARER_TOKEN_BEDROCK not set")
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}
	model := os.Getenv("BEDROCK_MODEL_ID")
	if model == "" {
		model = "us.anthropic.claude-haiku-4-5-20251001-v1:0"
	}
	endpoint := "https://bedrock-runtime." + region + ".amazonaws.com"

	br := newBedrockReplier(endpoint, tok, model, "Reply in one short sentence in Spanish.")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	reply, ok, err := br.Reply(ctx, "live-chat", "Hola, ¿a qué hora abren mañana?")
	if err != nil {
		t.Fatalf("bedrock error: %v", err)
	}
	if !ok {
		t.Fatal("no reply")
	}
	t.Logf("turn 1 reply: %q", reply)

	reply2, ok, err := br.Reply(ctx, "live-chat", "¿Y los domingos?")
	if err != nil {
		t.Fatalf("bedrock turn 2 error: %v", err)
	}
	if !ok {
		t.Fatal("no reply on turn 2")
	}
	t.Logf("turn 2 reply: %q", reply2)
}

func TestLiveMistral(t *testing.T) {
	tok := os.Getenv("MISTRAL_API_KEY")
	if tok == "" {
		t.Skip("MISTRAL_API_KEY not set")
	}
	model := os.Getenv("MISTRAL_MODEL_ID")
	if model == "" {
		model = "mistral-small-latest"
	}

	mr := newMistralReplier("https://api.mistral.ai", tok, model, "Reply in one short sentence in Spanish.")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	reply, ok, err := mr.Reply(ctx, "live-chat", "Hola, ¿a qué hora abren mañana?")
	if err != nil {
		t.Fatalf("mistral error: %v", err)
	}
	if !ok {
		t.Fatal("no reply")
	}
	t.Logf("turn 1 reply: %q", reply)

	reply2, ok, err := mr.Reply(ctx, "live-chat", "¿Y los domingos?")
	if err != nil {
		t.Fatalf("mistral turn 2 error: %v", err)
	}
	t.Logf("turn 2 reply: %q", reply2)
}

func TestLiveFallback_PrimaryDeadFallsToMistral(t *testing.T) {
	mTok := os.Getenv("MISTRAL_API_KEY")
	if mTok == "" {
		t.Skip("MISTRAL_API_KEY not set")
	}
	// Force Bedrock to fail with a bogus token; Mistral should pick up.
	bad := newBedrockReplier("https://bedrock-runtime.us-east-1.amazonaws.com", "expired-junk", "us.anthropic.claude-haiku-4-5-20251001-v1:0", "be brief")
	mr := newMistralReplier("https://api.mistral.ai", mTok, "mistral-small-latest", "Reply in one short sentence in Spanish.")
	f := &fallbackReplier{replier: []Replier{bad, mr}}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	reply, ok, err := f.Reply(ctx, "live-chat-fallback", "Hola")
	if err != nil || !ok {
		t.Fatalf("fallback err=%v ok=%v", err, ok)
	}
	t.Logf("fallback reply: %q", reply)
}
