package voice

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
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

// OpenStream returns a fake streaming transcription session. It accumulates
// audio bytes, hashes them, and emits a single committed transcript when
// the caller Commits. Lets the WS bridge handler be exercised end-to-end
// without a real ElevenLabs key.
func (m *MockProvider) OpenStream(ctx context.Context, opts StreamOptions) (TranscriptionStream, error) {
	if m.consumeFailNext() {
		return nil, ErrMockForced
	}
	s := &mockStream{
		events: make(chan StreamEvent, 4),
		closed: make(chan struct{}),
		lang:   opts.Language,
	}
	if s.lang == "" {
		s.lang = "es"
	}
	return s, nil
}

type mockStream struct {
	mu     sync.Mutex
	hash   []byte // running sha256 of all audio bytes received
	events chan StreamEvent
	closed chan struct{}
	lang   string
}

func (s *mockStream) SendAudio(pcm16 []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.closed:
		return io.ErrClosedPipe
	default:
	}
	// Cheap rolling hash: feed prior hash + new bytes back through sha256.
	h := sha256.New()
	h.Write(s.hash)
	h.Write(pcm16)
	s.hash = h.Sum(nil)
	// Emit a partial roughly every chunk to simulate streaming behaviour.
	tag := base64.StdEncoding.EncodeToString(s.hash)[:8]
	select {
	case s.events <- StreamEvent{Kind: StreamEventPartial, Text: "… " + tag}:
	default:
	}
	return nil
}

func (s *mockStream) Recv() (StreamEvent, error) {
	select {
	case e, ok := <-s.events:
		if !ok {
			return StreamEvent{}, io.EOF
		}
		return e, nil
	case <-s.closed:
		return StreamEvent{}, io.EOF
	}
}

func (s *mockStream) Commit() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tag := "(silencio)"
	if len(s.hash) > 0 {
		tag = "transcripción simulada: " + base64.StdEncoding.EncodeToString(s.hash)[:16]
	}
	select {
	case s.events <- StreamEvent{Kind: StreamEventFinal, Text: tag}:
	case <-s.closed:
		return io.ErrClosedPipe
	}
	close(s.events)
	return nil
}

func (s *mockStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
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
