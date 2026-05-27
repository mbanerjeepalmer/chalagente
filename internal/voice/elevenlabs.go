package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// Default model identifiers for ElevenLabs. These match the v2 spec.
const (
	defaultSTTModel = "scribe_v1"
	defaultTTSModel = "eleven_multilingual_v2"
	defaultBaseURL  = "https://api.elevenlabs.io"
)

// ElevenLabsProvider is a real-shape skeleton that targets the ElevenLabs HTTP
// API. The HTTP wiring is real — endpoint paths, headers, multipart and JSON
// shapes — but every entry point fails fast if APIKey is empty so an
// uninstantiated production provider can never fire a real call by accident.
//
// HTTPClient and BaseURL are injectable so tests can point this at an
// httptest.NewServer without touching the network.
type ElevenLabsProvider struct {
	// APIKey is the ElevenLabs API key. Required.
	APIKey string

	// STTModel overrides the speech-to-text model id. Defaults to "scribe_v1".
	STTModel string

	// TTSModel overrides the text-to-speech model id. Defaults to
	// "eleven_multilingual_v2".
	TTSModel string

	// HTTPClient is used for all outbound calls. Defaults to a 30s-timeout
	// http.Client when nil.
	HTTPClient *http.Client

	// BaseURL is the API root, e.g. "https://api.elevenlabs.io". Defaults to
	// the production endpoint.
	BaseURL string
}

func (p *ElevenLabsProvider) client() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (p *ElevenLabsProvider) baseURL() string {
	if p.BaseURL != "" {
		return strings.TrimRight(p.BaseURL, "/")
	}
	return defaultBaseURL
}

func (p *ElevenLabsProvider) sttModel() string {
	if p.STTModel != "" {
		return p.STTModel
	}
	return defaultSTTModel
}

func (p *ElevenLabsProvider) ttsModel() string {
	if p.TTSModel != "" {
		return p.TTSModel
	}
	return defaultTTSModel
}

// sttResponse mirrors the subset of the ElevenLabs STT JSON we consume.
type sttResponse struct {
	Text         string `json:"text"`
	LanguageCode string `json:"language_code"`
}

// Transcribe POSTs the audio bytes as multipart form-data to
// /v1/speech-to-text and parses the JSON response.
func (p *ElevenLabsProvider) Transcribe(ctx context.Context, audio []byte, mimeType string) (Transcript, error) {
	if p.APIKey == "" {
		return Transcript{}, ErrNoAPIKey
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	if err := mw.WriteField("model_id", p.sttModel()); err != nil {
		return Transcript{}, fmt.Errorf("voice: write model_id field: %w", err)
	}
	// language_code is intentionally omitted — ElevenLabs auto-detects when
	// the field is absent and rejects empty strings as a validation error.

	filename := "audio"
	switch {
	case strings.Contains(mimeType, "ogg"):
		filename = "audio.ogg"
	case strings.Contains(mimeType, "mpeg"), strings.Contains(mimeType, "mp3"):
		filename = "audio.mp3"
	case strings.Contains(mimeType, "wav"):
		filename = "audio.wav"
	}

	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return Transcript{}, fmt.Errorf("voice: create form file: %w", err)
	}
	if _, err := fw.Write(audio); err != nil {
		return Transcript{}, fmt.Errorf("voice: write audio: %w", err)
	}
	if err := mw.Close(); err != nil {
		return Transcript{}, fmt.Errorf("voice: close multipart: %w", err)
	}

	endpoint := p.baseURL() + "/v1/speech-to-text"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return Transcript{}, fmt.Errorf("voice: build STT request: %w", err)
	}
	req.Header.Set("xi-api-key", p.APIKey)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	resp, err := p.client().Do(req)
	if err != nil {
		return Transcript{}, fmt.Errorf("voice: STT request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Transcript{}, fmt.Errorf("voice: read STT response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Transcript{}, fmt.Errorf("voice: STT status %d: %s", resp.StatusCode, truncate(string(respBody), 256))
	}

	var parsed sttResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Transcript{}, fmt.Errorf("voice: decode STT response: %w", err)
	}
	return Transcript{
		Text:     parsed.Text,
		Language: parsed.LanguageCode,
	}, nil
}

// ttsRequest mirrors the subset of the ElevenLabs TTS JSON payload we send.
type ttsRequest struct {
	Text         string `json:"text"`
	ModelID      string `json:"model_id"`
	OutputFormat string `json:"output_format,omitempty"`
}

// Synthesize POSTs JSON to /v1/text-to-speech/{voice_id} and returns the raw
// MP3 audio bytes from the response.
func (p *ElevenLabsProvider) Synthesize(ctx context.Context, text, voiceID string) (Synthesis, error) {
	if p.APIKey == "" {
		return Synthesis{}, ErrNoAPIKey
	}
	if voiceID == "" {
		return Synthesis{}, fmt.Errorf("voice: voiceID is required")
	}

	payload := ttsRequest{
		Text:         text,
		ModelID:      p.ttsModel(),
		OutputFormat: "mp3_44100_128",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Synthesis{}, fmt.Errorf("voice: marshal TTS body: %w", err)
	}

	endpoint := p.baseURL() + path.Join("/v1/text-to-speech", url.PathEscape(voiceID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Synthesis{}, fmt.Errorf("voice: build TTS request: %w", err)
	}
	req.Header.Set("xi-api-key", p.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")

	resp, err := p.client().Do(req)
	if err != nil {
		return Synthesis{}, fmt.Errorf("voice: TTS request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Synthesis{}, fmt.Errorf("voice: read TTS response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Synthesis{}, fmt.Errorf("voice: TTS status %d: %s", resp.StatusCode, truncate(string(respBody), 256))
	}

	mt := resp.Header.Get("Content-Type")
	if mt == "" {
		mt = "audio/mpeg"
	}
	return Synthesis{
		Audio:    respBody,
		MimeType: mt,
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
