package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
	"rsc.io/qr"
)

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Chalagente</title>
<style>
 body { font-family: system-ui, sans-serif; max-width: 720px; margin: 2rem auto; padding: 0 1rem; color: #222; }
 h1 { margin-top: 0; }
 section { border: 1px solid #ddd; border-radius: 8px; padding: 1rem; margin-bottom: 1rem; }
 .status { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 0.85em; }
 .ok  { background: #d1fadf; color: #054f31; }
 .bad { background: #fde2e1; color: #6e1d1a; }
 form { display: flex; gap: .5rem; flex-wrap: wrap; }
 input[type=text] { flex: 1; padding: .4rem; min-width: 12rem; }
 button { padding: .4rem .8rem; }
 #feed { list-style: none; padding: 0; max-height: 16rem; overflow: auto; font-family: ui-monospace, monospace; font-size: 0.85em; }
 #feed li { padding: .25rem 0; border-bottom: 1px solid #eee; }
 .in  { color: #054f31; }
 .out { color: #1a4f7a; }
 .flash { background: #eef; border: 1px solid #99c; padding: .5rem; border-radius: 4px; margin-bottom: 1rem; }
 .err   { background: #fee; border-color: #c99; }
 img.qr { image-rendering: pixelated; width: 256px; height: 256px; }
</style>
</head>
<body>
<h1>Chalagente</h1>

{{ if .Flash }}<div class="flash {{ if .FlashErr }}err{{ end }}">{{ .Flash }}</div>{{ end }}

<section>
 <strong>Status:</strong>
 {{ if .LoggedIn }}<span class="status ok">paired · {{ .JID }}</span>
 {{ else }}<span class="status bad">not paired</span>{{ end }}
 · {{ if .Connected }}<span class="status ok">connected</span>{{ else }}<span class="status bad">disconnected</span>{{ end }}
</section>

{{ if not .LoggedIn }}
<section>
 <h2>Pair</h2>
 <p>Open WhatsApp → Settings → Linked Devices → Link a Device, and scan:</p>
 {{ if .HasQR }}<img class="qr" src="/qr.png?ts={{ .Now }}" alt="QR">{{ else }}<p><em>Waiting for QR code…</em></p>{{ end }}
 <p><small>QR rotates every ~20 seconds. This page auto-refreshes.</small></p>
 <script>setTimeout(() => location.reload(), 5000);</script>
</section>
{{ else }}
<section>
 <h2>Send a message</h2>
 <form method="POST" action="/send">
  <input type="text" name="to"   placeholder="447700900123 or jid@s.whatsapp.net" required>
  <input type="text" name="text" placeholder="message" required>
  <button>Send</button>
 </form>
</section>
{{ end }}

<section>
 <h2>Live feed</h2>
 <ul id="feed"></ul>
 <script>
  const feed = document.getElementById('feed');
  const fmt = (d) => {
    const t = new Date(d.time).toLocaleTimeString();
    const arrow = d.dir === 'in' ? '←' : '→';
    const li = document.createElement('li');
    li.className = d.dir;
    li.textContent = t + ' ' + arrow + ' ' + d.chat + ': ' + d.body;
    feed.prepend(li);
  };
  const es = new EventSource('/events');
  es.onmessage = (e) => { try { fmt(JSON.parse(e.data)); } catch {} };
 </script>
</section>
</body>
</html>`))

type pageData struct {
	LoggedIn  bool
	Connected bool
	JID       string
	HasQR     bool
	Now       int64
	Flash     string
	FlashErr  bool
}

func (a *App) serveHTTP(addr string) {
	user := os.Getenv("BASIC_AUTH_USER")
	pass := os.Getenv("BASIC_AUTH_PASS")
	if user == "" || pass == "" {
		log.Fatal("BASIC_AUTH_USER and BASIC_AUTH_PASS must be set")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/qr.png", a.handleQR)
	mux.HandleFunc("/send", a.handleSend)
	mux.HandleFunc("/events", a.handleEvents)

	protected := basicAuth(mux, user, pass)

	demoPublic := http.NewServeMux()
	demoPublic.HandleFunc("/demo", a.handleDemoChat)
	demoPublic.HandleFunc("/demo/events", a.handleDemoEvents)
	demoPublic.HandleFunc("/demo/media/", a.handleDemoMedia)

	demoBot := http.NewServeMux()
	demoBot.HandleFunc("/demo/bot", a.handleDemoBot)
	demoBot.HandleFunc("/demo/bot/send", a.handleDemoBotSend)

	root := http.NewServeMux()
	root.HandleFunc("/healthz", a.handleHealth)
	root.Handle("/demo/bot/", basicAuth(demoBot, user, pass))
	root.Handle("/demo/bot", basicAuth(demoBot, user, pass))
	root.Handle("/demo/", demoPublic)
	root.Handle("/demo", demoPublic)
	root.Handle("/", protected)

	log.Printf("HTTP listening on %s (auth enabled, user=%s)", addr, user)
	if err := http.ListenAndServe(addr, root); err != nil {
		log.Printf("http server: %v", err)
	}
}

func basicAuth(next http.Handler, user, pass string) http.Handler {
	expUser := []byte(user)
	expPass := []byte(pass)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(u), expUser) == 1
		passOK := subtle.ConstantTimeCompare([]byte(p), expPass) == 1
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="chalagente"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	qrCode, _ := a.qr()
	data := pageData{
		LoggedIn:  a.client.IsLoggedIn(),
		Connected: a.client.IsConnected(),
		HasQR:     qrCode != "",
		Now:       time.Now().UnixNano(),
		Flash:     r.URL.Query().Get("flash"),
		FlashErr:  r.URL.Query().Get("err") == "1",
	}
	if id := a.client.Store.ID; id != nil {
		data.JID = id.String()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := pageTmpl.Execute(w, data); err != nil {
		log.Printf("template: %v", err)
	}
}

func (a *App) handleQR(w http.ResponseWriter, _ *http.Request) {
	code, _ := a.qr()
	if code == "" {
		http.Error(w, "no QR available", http.StatusNotFound)
		return
	}
	c, err := qr.Encode(code, qr.M)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(c.PNG())
}

func (a *App) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.client.IsLoggedIn() {
		redirectFlash(w, r, "not paired yet", true)
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectFlash(w, r, "bad form: "+err.Error(), true)
		return
	}
	to := strings.TrimSpace(r.PostForm.Get("to"))
	text := r.PostForm.Get("text")
	if to == "" || text == "" {
		redirectFlash(w, r, "to and text are required", true)
		return
	}
	jid, err := parseRecipient(to)
	if err != nil {
		redirectFlash(w, r, "bad recipient: "+err.Error(), true)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	msg := &waProto.Message{Conversation: proto.String(text)}
	if _, err := a.client.SendMessage(ctx, jid, msg); err != nil {
		redirectFlash(w, r, "send failed: "+err.Error(), true)
		return
	}
	a.publish(Event{Time: time.Now(), Dir: "out", Chat: jid.String(), Body: text})
	redirectFlash(w, r, "sent to "+jid.String(), false)
}

func parseRecipient(s string) (types.JID, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "+")
	if strings.Contains(s, "@") {
		return types.ParseJID(s)
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
	if digits == "" {
		return types.JID{}, fmt.Errorf("no digits in %q", s)
	}
	return types.NewJID(digits, types.DefaultUserServer), nil
}

func redirectFlash(w http.ResponseWriter, r *http.Request, msg string, isErr bool) {
	q := url.Values{}
	q.Set("flash", msg)
	if isErr {
		q.Set("err", "1")
	}
	http.Redirect(w, r, "/?"+q.Encode(), http.StatusSeeOther)
}

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")

	ch, snapshot, unsub := a.subscribe()
	defer unsub()

	for _, e := range snapshot {
		writeSSE(w, e)
	}
	flusher.Flush()

	ka := time.NewTicker(20 * time.Second)
	defer ka.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case e, open := <-ch:
			if !open {
				return
			}
			writeSSE(w, e)
			flusher.Flush()
		case <-ka.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, e Event) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
}

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	if a.client.IsConnected() && a.client.IsLoggedIn() {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("not ready"))
}
