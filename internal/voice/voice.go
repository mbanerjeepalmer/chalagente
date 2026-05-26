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

// StreamOptions configures a real-time transcription session.
type StreamOptions struct {
	SampleRate int    // PCM 16-bit mono sample rate, e.g. 16000.
	Language   string // optional language hint, BCP-47-ish.
}

// StreamEventKind classifies events emitted by a TranscriptionStream.
type StreamEventKind int

const (
	// StreamEventPartial is an in-progress transcript that may be revised.
	StreamEventPartial StreamEventKind = iota
	// StreamEventFinal is a committed transcript that won't be revised.
	StreamEventFinal
	// StreamEventError signals a non-fatal provider error; the stream may
	// still produce more events.
	StreamEventError
)

// StreamEvent is what a TranscriptionStream returns from Recv.
type StreamEvent struct {
	Kind StreamEventKind
	Text string
	Err  error
}

// TranscriptionStream is the duplex handle the caller drives. SendAudio
// pushes raw PCM16 mono audio (in StreamOptions.SampleRate Hz). Recv blocks
// until the next event, returning io.EOF after Commit + drain. Close tears
// the underlying transport down even if the caller hasn't drained Recv.
type TranscriptionStream interface {
	SendAudio(pcm16 []byte) error
	Recv() (StreamEvent, error)
	Commit() error
	Close() error
}

// StreamingTranscriber is an optional capability on Providers that expose
// a real-time websocket transcription endpoint. Callers should type-assert
// app.Voice to this interface and fall back to file-upload Transcribe when
// it's not implemented (e.g. MockProvider in dev with no API key).
type StreamingTranscriber interface {
	OpenStream(ctx context.Context, opts StreamOptions) (TranscriptionStream, error)
}
