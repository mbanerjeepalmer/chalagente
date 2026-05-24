// Package agent defines the LLM-facing abstraction for chalagente: an Engine
// that produces a reply from a system prompt + chat history + a new incoming
// message, plus helpers for building per-tenant system prompts.
//
// This package intentionally has no dependency on the WhatsApp transport, the
// voice (STT/TTS) layer, or the storage layer — those are wired in by the
// caller (main.go and friends). Engines may be backed by a real LLM provider
// or by the deterministic MockEngine used for tests and local dev.
package agent

import (
	"context"
	"time"
)

// Role identifies the speaker of a Message in a conversation.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Attachment is a non-text payload attached to a Message. Ref is an opaque
// identifier — a blob URL, a storage key, or whatever the engine implementation
// knows how to resolve.
type Attachment struct {
	Kind     string // "image" | "audio" | "video"
	MimeType string
	Ref      string
}

// Message is a single turn in a conversation.
type Message struct {
	Role        Role
	Text        string
	Attachments []Attachment
	Timestamp   time.Time
}

// BusinessContext is the per-tenant info used to build the system prompt.
// Only non-empty fields end up in the rendered prompt.
type BusinessContext struct {
	Name       string
	Address    string
	Phone      string
	Website    string
	Categories []string
	Hours      string // pre-formatted, human-readable
	ExtraInfo  string
	Now        time.Time
	Tools      []ToolSpec
}

// ToolSpec is a lightweight description of a tool the agent may invoke.
// The actual call mechanics live elsewhere — this is just what the prompt
// needs to know about.
type ToolSpec struct {
	Key         string
	Description string
}

// Request is what an Engine receives to produce a Reply.
type Request struct {
	SystemPrompt string
	History      []Message
	Incoming     Message
	// Business is optional. Real LLM engines should use SystemPrompt as their
	// authoritative knowledge source; MockEngine uses these fields directly
	// to feel context-aware without parsing prose.
	Business BusinessContext
}

// Reply is what an Engine returns. Future versions may include tool calls.
type Reply struct {
	Text string
}

// Engine is the agent abstraction. Implementations: MockEngine (deterministic,
// for tests + dev) and — eventually — a real LLM-backed engine.
type Engine interface {
	Respond(ctx context.Context, req Request) (Reply, error)
}
