package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/coder/websocket"

	"github.com/mbanerjeepalmer/chalagente/internal/voice"
)

// transcribeStreamSampleRate is the PCM sample rate the browser is asked
// to deliver. Matches ElevenLabs' realtime expectation (16kHz mono PCM16).
const transcribeStreamSampleRate = 16000

// handleTryTranscribeWS bridges a browser WebSocket to the configured
// voice.StreamingTranscriber. The browser sends:
//   - binary frames containing raw 16kHz PCM16 mono audio chunks
//   - a text frame `{"type":"commit"}` when recording stops
//
// The server pipes audio into the provider stream and pushes transcript
// events back as JSON text frames: `{"kind":"partial"|"final"|"error","text":"…"}`.
// When the provider doesn't implement StreamingTranscriber, we close the
// WebSocket with a useful status code so the JS client can fall back to
// the file-upload path. Missing ELEVENLABS_API_KEY surfaces later as an
// error event from the stream itself, not here.
func (a *App) handleTryTranscribeWS(w http.ResponseWriter, r *http.Request) {
	streamer, ok := a.Voice.(voice.StreamingTranscriber)
	if !ok {
		http.Error(w, "streaming transcription not configured", http.StatusServiceUnavailable)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Same-origin only — the demo always loads /demo/transcribe/ws
		// from the same host. Don't expose this to third-party JS.
		InsecureSkipVerify: false,
	})
	if err != nil {
		log.Printf("transcribe ws accept: %v", err)
		return
	}
	defer conn.Close(websocket.StatusInternalError, "server closing")
	// Allow up to ~1MiB per frame — the browser shouldn't send more than
	// 32 KiB at a time but we leave headroom for noisy mics.
	conn.SetReadLimit(1 << 20)

	ctx := r.Context()
	stream, err := streamer.OpenStream(ctx, voice.StreamOptions{
		SampleRate: transcribeStreamSampleRate,
	})
	if err != nil {
		_ = conn.Write(ctx, websocket.MessageText, mustJSON(map[string]any{
			"kind": "error", "text": err.Error(),
		}))
		conn.Close(websocket.StatusInternalError, "open stream failed")
		return
	}
	defer stream.Close()

	// Pump provider events → browser in a separate goroutine so the
	// read-from-browser loop can drive Commit / Close cleanly.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			ev, err := stream.Recv()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					_ = conn.Write(ctx, websocket.MessageText, mustJSON(map[string]any{
						"kind": "error", "text": err.Error(),
					}))
				}
				return
			}
			kind := "partial"
			text := ev.Text
			switch ev.Kind {
			case voice.StreamEventFinal:
				kind = "final"
			case voice.StreamEventError:
				kind = "error"
				if ev.Err != nil {
					text = ev.Err.Error()
				}
			}
			if err := conn.Write(ctx, websocket.MessageText, mustJSON(map[string]any{
				"kind": kind, "text": text,
			})); err != nil {
				return
			}
		}
	}()

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		switch typ {
		case websocket.MessageBinary:
			if err := stream.SendAudio(data); err != nil {
				_ = conn.Write(ctx, websocket.MessageText, mustJSON(map[string]any{
					"kind": "error", "text": err.Error(),
				}))
				break
			}
		case websocket.MessageText:
			// Currently the only text control message we accept is
			// {"type":"commit"} — anything else is ignored.
			var msg struct{ Type string }
			if err := json.Unmarshal(data, &msg); err == nil && msg.Type == "commit" {
				if err := stream.Commit(); err != nil {
					log.Printf("transcribe ws commit: %v", err)
				}
			}
		}
	}
	stream.Close()
	<-done
	conn.Close(websocket.StatusNormalClosure, "done")
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
