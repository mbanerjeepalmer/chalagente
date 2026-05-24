// Package voice provides speech-to-text and text-to-speech abstractions for
// the WhatsApp agent. The interface is intentionally narrow: callers hand in
// audio bytes and receive a transcript, or hand in text and receive audio
// bytes. Downloading WhatsApp voice notes and sending PTT messages live in
// the WhatsApp layer, not here.
package voice

import (
	"context"
	"errors"
)

// ErrNoAPIKey is returned by network-backed providers when configured without
// credentials. Used as a guard so an accidentally-uninstantiated production
// provider can't make real API calls.
var ErrNoAPIKey = errors.New("voice: no API key configured")

// Transcript is the result of a speech-to-text call.
type Transcript struct {
	Text     string
	Language string // BCP-47-ish, e.g. "es", "en"
}

// Synthesis is the result of a text-to-speech call.
type Synthesis struct {
	Audio    []byte
	MimeType string // "audio/mpeg" for MP3
}

// Provider is the abstraction used by the agent for STT and TTS. Implementations
// must be safe for concurrent use by multiple goroutines.
type Provider interface {
	Transcribe(ctx context.Context, audio []byte, mimeType string) (Transcript, error)
	Synthesize(ctx context.Context, text, voiceID string) (Synthesis, error)
}
