package voice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/coder/websocket"
)

// elevenLabsRealtimeURL is the documented endpoint for the Scribe v2
// real-time transcription websocket.
const elevenLabsRealtimeURL = "wss://api.elevenlabs.io/v1/speech-to-text/realtime?model_id=scribe_v2_realtime"

// OpenStream opens a duplex websocket against ElevenLabs' realtime STT
// endpoint. The returned TranscriptionStream accepts 16kHz PCM16 mono
// chunks via SendAudio and surfaces partial / committed transcripts via
// Recv. Authentication uses the xi-api-key header per the docs.
func (p *ElevenLabsProvider) OpenStream(ctx context.Context, opts StreamOptions) (TranscriptionStream, error) {
	if p.APIKey == "" {
		return nil, ErrNoAPIKey
	}
	sr := opts.SampleRate
	if sr == 0 {
		sr = 16000
	}
	headers := http.Header{}
	headers.Set("xi-api-key", p.APIKey)
	conn, _, err := websocket.Dial(ctx, elevenLabsRealtimeURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		return nil, fmt.Errorf("elevenlabs ws dial: %w", err)
	}
	// Disable the read limit so a long final transcript doesn't drop the
	// connection. coder/websocket defaults to 32KiB which is plenty for
	// normal transcripts but we bump it to be safe for long sessions.
	conn.SetReadLimit(1 << 20)
	es := &elevenStream{
		conn:       conn,
		sampleRate: sr,
		events:     make(chan StreamEvent, 8),
		closed:     make(chan struct{}),
	}
	es.readerCtx, es.readerCancel = context.WithCancel(context.Background())
	go es.readLoop()
	return es, nil
}

// elevenStream wraps the open websocket and a goroutine that demuxes
// inbound JSON messages onto an events channel for Recv.
type elevenStream struct {
	conn       *websocket.Conn
	sampleRate int

	writeMu sync.Mutex

	events       chan StreamEvent
	closed       chan struct{}
	closeOnce    sync.Once
	readerCtx    context.Context
	readerCancel context.CancelFunc
}

// inputAudioChunk is the JSON shape we send for each audio frame, per
// ElevenLabs' docs: {"message_type":"input_audio_chunk","audio_base_64":"…",
// "commit":false,"sample_rate":16000}.
type inputAudioChunk struct {
	MessageType string `json:"message_type"`
	AudioBase64 string `json:"audio_base_64"`
	Commit      bool   `json:"commit"`
	SampleRate  int    `json:"sample_rate"`
}

func (s *elevenStream) SendAudio(pcm16 []byte) error {
	if len(pcm16) == 0 {
		return nil
	}
	select {
	case <-s.closed:
		return io.ErrClosedPipe
	default:
	}
	msg := inputAudioChunk{
		MessageType: "input_audio_chunk",
		AudioBase64: base64.StdEncoding.EncodeToString(pcm16),
		Commit:      false,
		SampleRate:  s.sampleRate,
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.Write(s.readerCtx, websocket.MessageText, raw)
}

func (s *elevenStream) Commit() error {
	select {
	case <-s.closed:
		return io.ErrClosedPipe
	default:
	}
	msg := inputAudioChunk{
		MessageType: "input_audio_chunk",
		AudioBase64: "",
		Commit:      true,
		SampleRate:  s.sampleRate,
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.Write(s.readerCtx, websocket.MessageText, raw)
}

func (s *elevenStream) Recv() (StreamEvent, error) {
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

func (s *elevenStream) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
		s.readerCancel()
		_ = s.conn.Close(websocket.StatusNormalClosure, "client done")
	})
	return nil
}

// elevenMessage is the union of message_types we care about coming back
// from the realtime endpoint. The full schema includes timestamped
// transcripts and error variants we route into StreamEventError.
type elevenMessage struct {
	MessageType string `json:"message_type"`
	Text        string `json:"text"`
	Error       string `json:"error"`
}

func (s *elevenStream) readLoop() {
	defer close(s.events)
	for {
		_, raw, err := s.conn.Read(s.readerCtx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				select {
				case s.events <- StreamEvent{Kind: StreamEventError, Err: err}:
				case <-s.closed:
				}
			}
			return
		}
		var m elevenMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		var ev StreamEvent
		switch m.MessageType {
		case "partial_transcript":
			ev = StreamEvent{Kind: StreamEventPartial, Text: m.Text}
		case "committed_transcript", "committed_transcript_with_timestamps":
			ev = StreamEvent{Kind: StreamEventFinal, Text: m.Text}
		case "input_error":
			ev = StreamEvent{Kind: StreamEventError, Err: errors.New(m.Error)}
		default:
			continue
		}
		select {
		case s.events <- ev:
		case <-s.closed:
			return
		}
	}
}
