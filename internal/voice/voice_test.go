package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- MockProvider --------------------------------------------------------

func TestMockTranscribeDeterministic(t *testing.T) {
	m := &MockProvider{}
	ctx := context.Background()

	audio := []byte("pretend this is opus audio")
	t1, err := m.Transcribe(ctx, audio, "audio/ogg")
	if err != nil {
		t.Fatalf("Transcribe 1: %v", err)
	}
	t2, err := m.Transcribe(ctx, audio, "audio/ogg")
	if err != nil {
		t.Fatalf("Transcribe 2: %v", err)
	}
	if t1.Text != t2.Text {
		t.Fatalf("expected deterministic text, got %q vs %q", t1.Text, t2.Text)
	}
	if !strings.HasPrefix(t1.Text, "transcripción simulada:") {
		t.Fatalf("expected mock prefix, got %q", t1.Text)
	}
	if t1.Language != "es" {
		t.Fatalf("expected default language es, got %q", t1.Language)
	}

	// Different audio yields different text.
	other, err := m.Transcribe(ctx, []byte("something else entirely"), "audio/ogg")
	if err != nil {
		t.Fatalf("Transcribe other: %v", err)
	}
	if other.Text == t1.Text {
		t.Fatalf("expected different text for different audio")
	}
}

func TestMockTranscribeForceLang(t *testing.T) {
	m := &MockProvider{ForceLang: "en"}
	got, err := m.Transcribe(context.Background(), []byte("x"), "audio/ogg")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got.Language != "en" {
		t.Fatalf("expected en, got %q", got.Language)
	}
}

func TestMockSynthesizeDeterministic(t *testing.T) {
	m := &MockProvider{}
	ctx := context.Background()

	a, err := m.Synthesize(ctx, "hola mundo", "voice-1")
	if err != nil {
		t.Fatalf("Synthesize 1: %v", err)
	}
	if len(a.Audio) != 1024 {
		t.Fatalf("expected 1024 bytes, got %d", len(a.Audio))
	}
	if a.MimeType != "audio/mpeg" {
		t.Fatalf("expected audio/mpeg, got %q", a.MimeType)
	}

	b, err := m.Synthesize(ctx, "hola mundo", "voice-1")
	if err != nil {
		t.Fatalf("Synthesize 2: %v", err)
	}
	if !bytes.Equal(a.Audio, b.Audio) {
		t.Fatalf("expected identical audio for same input")
	}

	c, err := m.Synthesize(ctx, "different text", "voice-1")
	if err != nil {
		t.Fatalf("Synthesize 3: %v", err)
	}
	if bytes.Equal(a.Audio, c.Audio) {
		t.Fatalf("expected different audio for different text")
	}

	d, err := m.Synthesize(ctx, "hola mundo", "voice-2")
	if err != nil {
		t.Fatalf("Synthesize 4: %v", err)
	}
	if bytes.Equal(a.Audio, d.Audio) {
		t.Fatalf("expected different audio for different voice")
	}
}

func TestMockFailNext(t *testing.T) {
	m := &MockProvider{FailNext: true}
	ctx := context.Background()

	if _, err := m.Synthesize(ctx, "x", "v"); err == nil {
		t.Fatalf("expected error from FailNext on first call")
	}
	if _, err := m.Synthesize(ctx, "x", "v"); err != nil {
		t.Fatalf("expected recovery on second call, got %v", err)
	}

	m.FailNext = true
	if _, err := m.Transcribe(ctx, []byte("x"), "audio/ogg"); err == nil {
		t.Fatalf("expected error from FailNext on Transcribe")
	}
	if _, err := m.Transcribe(ctx, []byte("x"), "audio/ogg"); err != nil {
		t.Fatalf("expected recovery on second Transcribe, got %v", err)
	}
}

// --- MockProvider streaming ----------------------------------------------

func TestMockOpenStreamFlow(t *testing.T) {
	m := &MockProvider{}
	s, err := m.OpenStream(context.Background(), StreamOptions{SampleRate: 16000})
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	if err := s.SendAudio([]byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}
	got, err := s.Recv()
	if err != nil {
		t.Fatalf("Recv partial: %v", err)
	}
	if got.Kind != StreamEventPartial || got.Text == "" {
		t.Fatalf("expected partial event with text, got %+v", got)
	}
	if err := s.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// Drain until the final event arrives.
	var final StreamEvent
	for {
		ev, err := s.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv drain: %v", err)
		}
		if ev.Kind == StreamEventFinal {
			final = ev
			break
		}
	}
	if final.Kind != StreamEventFinal || final.Text == "" {
		t.Fatalf("expected final event with text, got %+v", final)
	}
	_ = s.Close()
}

func TestMockOpenStreamFailNext(t *testing.T) {
	m := &MockProvider{FailNext: true}
	if _, err := m.OpenStream(context.Background(), StreamOptions{}); !errors.Is(err, ErrMockForced) {
		t.Fatalf("expected ErrMockForced, got %v", err)
	}
}

// --- ElevenLabsProvider --------------------------------------------------

func TestElevenLabsNoAPIKey(t *testing.T) {
	p := &ElevenLabsProvider{}
	ctx := context.Background()

	if _, err := p.Transcribe(ctx, []byte("x"), "audio/ogg"); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey from Transcribe, got %v", err)
	}
	if _, err := p.Synthesize(ctx, "hola", "voice-1"); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey from Synthesize, got %v", err)
	}
}

func TestElevenLabsTranscribeRequestShape(t *testing.T) {
	var (
		gotPath   string
		gotKey    string
		gotMethod string
		gotBody   []byte
		gotCT     string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("xi-api-key")
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hola mundo","language_code":"es"}`))
	}))
	t.Cleanup(srv.Close)

	p := &ElevenLabsProvider{
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}

	audio := []byte("fake-opus-bytes")
	tr, err := p.Transcribe(context.Background(), audio, "audio/ogg")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/v1/speech-to-text" {
		t.Fatalf("expected /v1/speech-to-text, got %s", gotPath)
	}
	if gotKey != "test-key" {
		t.Fatalf("expected xi-api-key header to be test-key, got %q", gotKey)
	}
	if !strings.HasPrefix(gotCT, "multipart/form-data") {
		t.Fatalf("expected multipart content-type, got %q", gotCT)
	}
	if !bytes.Contains(gotBody, audio) {
		t.Fatalf("expected audio bytes in body")
	}
	if !bytes.Contains(gotBody, []byte("scribe_v1")) {
		t.Fatalf("expected model id in body")
	}
	if tr.Text != "hola mundo" {
		t.Fatalf("expected text from response, got %q", tr.Text)
	}
	if tr.Language != "es" {
		t.Fatalf("expected lang es, got %q", tr.Language)
	}
}

func TestElevenLabsSynthesizeRequestShape(t *testing.T) {
	var (
		gotPath   string
		gotKey    string
		gotMethod string
		gotBody   []byte
		gotCT     string
	)

	canned := []byte("MP3DATA")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("xi-api-key")
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(canned)
	}))
	t.Cleanup(srv.Close)

	p := &ElevenLabsProvider{
		APIKey:     "key-xyz",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}

	syn, err := p.Synthesize(context.Background(), "hola mundo", "voice-abc")
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/v1/text-to-speech/voice-abc" {
		t.Fatalf("expected /v1/text-to-speech/voice-abc, got %s", gotPath)
	}
	if gotKey != "key-xyz" {
		t.Fatalf("expected xi-api-key=key-xyz, got %q", gotKey)
	}
	if gotCT != "application/json" {
		t.Fatalf("expected application/json content-type, got %q", gotCT)
	}

	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if payload["text"] != "hola mundo" {
		t.Fatalf("expected text in body, got %v", payload["text"])
	}
	if payload["model_id"] != "eleven_multilingual_v2" {
		t.Fatalf("expected model_id eleven_multilingual_v2, got %v", payload["model_id"])
	}

	if !bytes.Equal(syn.Audio, canned) {
		t.Fatalf("expected canned audio bytes, got %d bytes", len(syn.Audio))
	}
	if syn.MimeType != "audio/mpeg" {
		t.Fatalf("expected audio/mpeg, got %q", syn.MimeType)
	}
}

// --- CachedProvider ------------------------------------------------------

// callCountingProvider wraps MockProvider but tracks call counts for cache tests.
type callCountingProvider struct {
	inner          *MockProvider
	transcribeCals int
	synthesizeCals int
}

func (c *callCountingProvider) Transcribe(ctx context.Context, audio []byte, mt string) (Transcript, error) {
	c.transcribeCals++
	return c.inner.Transcribe(ctx, audio, mt)
}

func (c *callCountingProvider) Synthesize(ctx context.Context, text, voiceID string) (Synthesis, error) {
	c.synthesizeCals++
	return c.inner.Synthesize(ctx, text, voiceID)
}

func TestCachedSynthesizeHit(t *testing.T) {
	inner := &callCountingProvider{inner: &MockProvider{}}
	cp := NewCachedProvider(inner, 16)

	ctx := context.Background()
	a, err := cp.Synthesize(ctx, "hola", "v1")
	if err != nil {
		t.Fatalf("Synthesize 1: %v", err)
	}
	b, err := cp.Synthesize(ctx, "hola", "v1")
	if err != nil {
		t.Fatalf("Synthesize 2: %v", err)
	}
	if !bytes.Equal(a.Audio, b.Audio) {
		t.Fatalf("expected cached audio to match")
	}
	if inner.synthesizeCals != 1 {
		t.Fatalf("expected 1 inner call after cache hit, got %d", inner.synthesizeCals)
	}

	// Different text → cache miss.
	if _, err := cp.Synthesize(ctx, "adios", "v1"); err != nil {
		t.Fatalf("Synthesize 3: %v", err)
	}
	if inner.synthesizeCals != 2 {
		t.Fatalf("expected 2 inner calls after distinct text, got %d", inner.synthesizeCals)
	}

	// Different voice, same text → cache miss.
	if _, err := cp.Synthesize(ctx, "hola", "v2"); err != nil {
		t.Fatalf("Synthesize 4: %v", err)
	}
	if inner.synthesizeCals != 3 {
		t.Fatalf("expected 3 inner calls after distinct voice, got %d", inner.synthesizeCals)
	}
}

func TestCachedTranscribePassthrough(t *testing.T) {
	inner := &callCountingProvider{inner: &MockProvider{}}
	cp := NewCachedProvider(inner, 16)

	ctx := context.Background()
	if _, err := cp.Transcribe(ctx, []byte("a"), "audio/ogg"); err != nil {
		t.Fatalf("Transcribe 1: %v", err)
	}
	if _, err := cp.Transcribe(ctx, []byte("a"), "audio/ogg"); err != nil {
		t.Fatalf("Transcribe 2: %v", err)
	}
	if inner.transcribeCals != 2 {
		t.Fatalf("expected transcribe NOT cached, want 2 calls, got %d", inner.transcribeCals)
	}
}

func TestCachedEvictionFIFO(t *testing.T) {
	inner := &callCountingProvider{inner: &MockProvider{}}
	cp := NewCachedProvider(inner, 3)
	ctx := context.Background()

	// Fill cache with 3 entries.
	for _, txt := range []string{"a", "b", "c"} {
		if _, err := cp.Synthesize(ctx, txt, "v1"); err != nil {
			t.Fatalf("Synthesize %s: %v", txt, err)
		}
	}
	if inner.synthesizeCals != 3 {
		t.Fatalf("expected 3 inner calls, got %d", inner.synthesizeCals)
	}

	// Confirm "a" is cached.
	if _, err := cp.Synthesize(ctx, "a", "v1"); err != nil {
		t.Fatalf("Synthesize a: %v", err)
	}
	if inner.synthesizeCals != 3 {
		t.Fatalf("expected cache hit for a, got %d inner calls", inner.synthesizeCals)
	}

	// Add a 4th entry → triggers eviction of oldest by insertion order ("a").
	if _, err := cp.Synthesize(ctx, "d", "v1"); err != nil {
		t.Fatalf("Synthesize d: %v", err)
	}
	if inner.synthesizeCals != 4 {
		t.Fatalf("expected 4 inner calls after add d, got %d", inner.synthesizeCals)
	}

	// "a" should now be evicted → next call hits inner again.
	if _, err := cp.Synthesize(ctx, "a", "v1"); err != nil {
		t.Fatalf("Synthesize a again: %v", err)
	}
	if inner.synthesizeCals != 5 {
		t.Fatalf("expected a to be evicted, want 5 inner calls, got %d", inner.synthesizeCals)
	}

	// "b" should still be present (insertion order: b, c, d after eviction of a; then a re-added evicts b).
	// After last step, cache contains c, d, a. So "b" must also be a miss.
	if _, err := cp.Synthesize(ctx, "b", "v1"); err != nil {
		t.Fatalf("Synthesize b: %v", err)
	}
	if inner.synthesizeCals != 6 {
		t.Fatalf("expected b to also be evicted, want 6 inner calls, got %d", inner.synthesizeCals)
	}
}
