package voice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// CachedProvider wraps a Provider and memoises Synthesize results by a SHA-256
// of (voiceID + "\x00" + text). Transcribe is passed through unchanged —
// inbound audio bytes are large and effectively unique, so caching them is
// wasted work.
//
// Eviction is simple FIFO by insertion order. When the cache hits its size
// cap, the oldest inserted entry is dropped. Hits do NOT refresh insertion
// order (this is FIFO, not LRU) — the v2 spec is explicit that we want the
// dumbest thing that works.
type CachedProvider struct {
	Inner Provider

	mu      sync.Mutex
	maxSize int
	entries map[string]Synthesis
	order   []string // insertion order; index 0 is oldest
}

// NewCachedProvider wraps inner with a FIFO cache that holds at most maxSize
// synthesis results. A maxSize <= 0 disables caching entirely.
func NewCachedProvider(inner Provider, maxSize int) *CachedProvider {
	return &CachedProvider{
		Inner:   inner,
		maxSize: maxSize,
		entries: make(map[string]Synthesis),
	}
}

// cacheKey hashes (voiceID, text) so we never hold the raw text in the map
// key (cheaper to compare, no memory blow-up for long replies).
func cacheKey(voiceID, text string) string {
	h := sha256.New()
	h.Write([]byte(voiceID))
	h.Write([]byte{0})
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}

// Transcribe delegates straight to the inner provider — never cached.
func (c *CachedProvider) Transcribe(ctx context.Context, audio []byte, mimeType string) (Transcript, error) {
	return c.Inner.Transcribe(ctx, audio, mimeType)
}

// OpenStream forwards to the inner provider when it implements
// StreamingTranscriber. There's nothing to cache here — every session is
// unique audio — so this is a thin pass-through that lets callers keep
// holding a CachedProvider as their app.Voice without losing streaming.
func (c *CachedProvider) OpenStream(ctx context.Context, opts StreamOptions) (TranscriptionStream, error) {
	s, ok := c.Inner.(StreamingTranscriber)
	if !ok {
		return nil, ErrNoAPIKey
	}
	return s.OpenStream(ctx, opts)
}

// Synthesize returns the cached result if (voiceID, text) is known, otherwise
// calls the inner provider and stores the result.
func (c *CachedProvider) Synthesize(ctx context.Context, text, voiceID string) (Synthesis, error) {
	key := cacheKey(voiceID, text)

	c.mu.Lock()
	if hit, ok := c.entries[key]; ok {
		c.mu.Unlock()
		return hit, nil
	}
	c.mu.Unlock()

	// Call the inner provider OUTSIDE the lock — TTS calls can take seconds
	// and we don't want to block unrelated cache reads.
	syn, err := c.Inner.Synthesize(ctx, text, voiceID)
	if err != nil {
		return Synthesis{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check: another goroutine may have populated this key while we
	// were calling Inner. Prefer the existing entry to keep FIFO order
	// deterministic.
	if hit, ok := c.entries[key]; ok {
		return hit, nil
	}
	if c.maxSize > 0 {
		for len(c.order) >= c.maxSize {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.entries, oldest)
		}
		c.entries[key] = syn
		c.order = append(c.order, key)
	}
	return syn, nil
}
