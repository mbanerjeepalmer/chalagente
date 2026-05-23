package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const demoMaxUpload = 10 << 20 // 10 MB

type DemoMessage struct {
	ID       string    `json:"id"`
	Time     time.Time `json:"time"`
	Dir      string    `json:"dir"` // "in" | "out"
	Type     string    `json:"type"`
	Body     string    `json:"body,omitempty"`
	MediaURL string    `json:"mediaUrl,omitempty"`
}

type mediaMeta struct {
	path string
	mime string
}

type DemoStore struct {
	mediaDir string

	mediaMu sync.RWMutex
	media   map[string]mediaMeta

	busMu  sync.Mutex
	recent []DemoMessage
	subs   map[chan DemoMessage]struct{}
}

func newDemoStore(mediaDir string) (*DemoStore, error) {
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return nil, err
	}
	return &DemoStore{
		mediaDir: mediaDir,
		media:    make(map[string]mediaMeta),
		subs:     make(map[chan DemoMessage]struct{}),
	}, nil
}

func replyToInbound(inbound DemoMessage) DemoMessage {
	_ = inbound
	return DemoMessage{
		ID:   uuid.NewString(),
		Time: time.Now(),
		Dir:  "out",
		Type: "text",
		Body: autoReply,
	}
}

func (d *DemoStore) publish(m DemoMessage) {
	d.busMu.Lock()
	d.recent = append(d.recent, m)
	if len(d.recent) > 100 {
		d.recent = d.recent[len(d.recent)-100:]
	}
	subs := make([]chan DemoMessage, 0, len(d.subs))
	for ch := range d.subs {
		subs = append(subs, ch)
	}
	d.busMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- m:
		default:
		}
	}
}

func (d *DemoStore) subscribe() (chan DemoMessage, []DemoMessage, func()) {
	ch := make(chan DemoMessage, 16)
	d.busMu.Lock()
	d.subs[ch] = struct{}{}
	snapshot := append([]DemoMessage(nil), d.recent...)
	d.busMu.Unlock()
	return ch, snapshot, func() {
		d.busMu.Lock()
		delete(d.subs, ch)
		d.busMu.Unlock()
		close(ch)
	}
}

func (a *App) handleDemoInbound(msg DemoMessage) {
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}
	if msg.Time.IsZero() {
		msg.Time = time.Now()
	}
	msg.Dir = "in"
	a.demo.publish(msg)
	reply := replyToInbound(msg)
	a.demo.publish(reply)
}

func (d *DemoStore) saveMedia(r io.Reader, mime, filename string) (id string, err error) {
	if err := validateDemoMIME(mime); err != nil {
		return "", err
	}
	id = uuid.NewString()
	ext := extFromMIME(mime)
	if ext == "" {
		ext = extFromFilename(filename)
	}
	if ext == "" {
		ext = ".bin"
	}
	path := filepath.Join(d.mediaDir, id+ext)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	d.mediaMu.Lock()
	d.media[id] = mediaMeta{path: path, mime: mime}
	d.mediaMu.Unlock()
	return id, nil
}

func (d *DemoStore) openMedia(id string) (path, mime string, ok bool) {
	d.mediaMu.RLock()
	meta, ok := d.media[id]
	d.mediaMu.RUnlock()
	if ok {
		return meta.path, meta.mime, true
	}
	matches, _ := filepath.Glob(filepath.Join(d.mediaDir, id+".*"))
	if len(matches) == 0 {
		return "", "", false
	}
	path = matches[0]
	mime = mimeFromExt(filepath.Ext(path))
	d.mediaMu.Lock()
	d.media[id] = mediaMeta{path: path, mime: mime}
	d.mediaMu.Unlock()
	return path, mime, true
}

func validateDemoMIME(mime string) error {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return nil
	case strings.HasPrefix(mime, "audio/"):
		return nil
	case strings.HasPrefix(mime, "video/"):
		return nil
	default:
		return fmt.Errorf("unsupported media type %q", mime)
	}
}

func validateDemoType(t string) error {
	switch t {
	case "text", "image", "audio", "video":
		return nil
	default:
		return fmt.Errorf("unsupported message type %q", t)
	}
}

func extFromMIME(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/jpeg"):
		return ".jpg"
	case strings.HasPrefix(mime, "image/png"):
		return ".png"
	case strings.HasPrefix(mime, "image/gif"):
		return ".gif"
	case strings.HasPrefix(mime, "image/webp"):
		return ".webp"
	case strings.HasPrefix(mime, "audio/mpeg"):
		return ".mp3"
	case strings.HasPrefix(mime, "audio/ogg"):
		return ".ogg"
	case strings.HasPrefix(mime, "audio/wav"), strings.HasPrefix(mime, "audio/x-wav"):
		return ".wav"
	case strings.HasPrefix(mime, "audio/mp4"):
		return ".m4a"
	case strings.HasPrefix(mime, "video/mp4"):
		return ".mp4"
	case strings.HasPrefix(mime, "video/webm"):
		return ".webm"
	default:
		return ""
	}
}

func extFromFilename(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".mp3", ".ogg", ".wav", ".m4a", ".mp4", ".webm":
		return ext
	default:
		return ""
	}
}

func mimeFromExt(ext string) string {
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	case ".m4a":
		return "audio/mp4"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	default:
		return "application/octet-stream"
	}
}
