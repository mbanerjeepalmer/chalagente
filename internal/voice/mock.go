package voice

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
)

// MockProvider is a deterministic, network-free Provider for tests and local
// development. The same inputs always yield the same outputs, so tests can
// assert on byte-equality across runs.
type MockProvider struct {
	// ForceLang overrides the detected language for Transcribe. Empty defaults
	// to "es" to match the v1 hardcoded-Spanish bias.
	ForceLang string

	// FailNext, when true, causes the next call (Transcribe or Synthesize) to
	// return an error and resets itself to false. Lets tests exercise error
	// paths without a separate failing-impl type.
	FailNext bool

	mu sync.Mutex
}

// ErrMockForced is returned by MockProvider when FailNext is set.
var ErrMockForced = errors.New("voice: mock forced failure")

func (m *MockProvider) consumeFailNext() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailNext {
		m.FailNext = false
		return true
	}
	return false
}

// Transcribe returns a deterministic stub transcript derived from the audio
// bytes. The result is stable across calls for the same input.
func (m *MockProvider) Transcribe(_ context.Context, audio []byte, _ string) (Transcript, error) {
	if m.consumeFailNext() {
		return Transcript{}, ErrMockForced
	}
	sum := sha256.Sum256(audio)
	tag := base64.StdEncoding.EncodeToString(sum[:])
	if len(tag) > 32 {
		tag = tag[:32]
	}
	lang := m.ForceLang
	if lang == "" {
		lang = "es"
	}
	return Transcript{
		Text:     "transcripción simulada: " + tag,
		Language: lang,
	}, nil
}

// Synthesize returns 1024 deterministic bytes derived from (voiceID, text). The
// bytes are not real MP3 data — tests treat them as opaque payload — but they
// are stable per input so cache tests can assert byte equality.
func (m *MockProvider) Synthesize(_ context.Context, text, voiceID string) (Synthesis, error) {
	if m.consumeFailNext() {
		return Synthesis{}, ErrMockForced
	}
	const size = 1024
	out := make([]byte, size)
	// Stretch the seed deterministically by hashing (seed || counter).
	seed := []byte(voiceID + "\x00" + text)
	written := 0
	var counter byte
	for written < size {
		h := sha256.New()
		h.Write(seed)
		h.Write([]byte{counter})
		block := h.Sum(nil)
		n := copy(out[written:], block)
		written += n
		counter++
	}
	return Synthesis{
		Audio:    out,
		MimeType: "audio/mpeg",
	}, nil
}
