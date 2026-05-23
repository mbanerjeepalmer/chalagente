package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

var demoChatTmpl = template.Must(template.New("demoChat").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Chalagente · Demo</title>
<style>
 * { box-sizing: border-box; }
 body { margin: 0; min-height: 100vh; background: #0b141a; font-family: system-ui, -apple-system, sans-serif; display: flex; justify-content: center; align-items: center; padding: 1rem; }
 .phone { width: 100%; max-width: 390px; height: min(720px, 90vh); background: #efeae2; border-radius: 12px; overflow: hidden; display: flex; flex-direction: column; box-shadow: 0 8px 32px rgba(0,0,0,.4); }
 .header { background: #075e54; color: #fff; padding: .75rem 1rem; display: flex; align-items: center; gap: .5rem; }
 .header h1 { margin: 0; font-size: 1rem; font-weight: 600; flex: 1; }
 .badge { font-size: .65rem; background: rgba(255,255,255,.2); padding: 2px 6px; border-radius: 4px; text-transform: uppercase; letter-spacing: .04em; }
 .chat { flex: 1; overflow-y: auto; padding: .75rem; display: flex; flex-direction: column; gap: .4rem; background: url("data:image/svg+xml,%3Csvg width='60' height='60' xmlns='http://www.w3.org/2000/svg'%3E%3Cg fill='%23d9d0c6' fill-opacity='.25'%3E%3Cpath d='M0 0h30v30H0zm30 30h30v30H30z'/%3E%3C/g%3E%3C/svg%3E"); }
 .msg { max-width: 80%; padding: .45rem .6rem; border-radius: 8px; font-size: .9rem; line-height: 1.35; word-wrap: break-word; }
 .msg.in  { align-self: flex-end; background: #d9fdd3; border-top-right-radius: 0; }
 .msg.out { align-self: flex-start; background: #fff; border-top-left-radius: 0; }
 .msg .time { display: block; font-size: .65rem; color: #667; text-align: right; margin-top: .2rem; }
 .msg img, .msg video { max-width: 100%; border-radius: 4px; display: block; }
 .msg audio { width: 100%; min-width: 200px; }
 .caption { margin-top: .35rem; }
</style>
</head>
<body>
<div class="phone">
 <div class="header">
  <h1>Chalagente</h1>
  <span class="badge">Demo</span>
 </div>
 <div class="chat" id="chat"></div>
</div>
<script>
 const chat = document.getElementById('chat');

 function fmtTime(iso) {
  return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
 }

 function renderMsg(d) {
  const wrap = document.createElement('div');
  wrap.className = 'msg ' + d.dir;
  let inner = '';
  if (d.type === 'image' && d.mediaUrl) {
   inner += '<img src="' + d.mediaUrl + '" alt="image">';
  } else if (d.type === 'video' && d.mediaUrl) {
   inner += '<video controls src="' + d.mediaUrl + '"></video>';
  } else if (d.type === 'audio' && d.mediaUrl) {
   inner += '<audio controls src="' + d.mediaUrl + '"></audio>';
  }
  if (d.body) {
   inner += '<div class="caption">' + escapeHtml(d.body) + '</div>';
  } else if (d.type === 'text') {
   inner += escapeHtml(d.body || '');
  }
  inner += '<span class="time">' + fmtTime(d.time) + '</span>';
  wrap.innerHTML = inner;
  chat.appendChild(wrap);
  chat.scrollTop = chat.scrollHeight;
 }

 function escapeHtml(s) {
  const el = document.createElement('span');
  el.textContent = s;
  return el.innerHTML;
 }

 const es = new EventSource('/demo/events');
 es.onmessage = (e) => { try { renderMsg(JSON.parse(e.data)); } catch {} };
</script>
</body>
</html>`))

var demoBotTmpl = template.Must(template.New("demoBot").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Chalagente · Demo Bot</title>
<style>
 body { font-family: system-ui, sans-serif; max-width: 640px; margin: 2rem auto; padding: 0 1rem; color: #222; }
 h1 { margin-top: 0; }
 section { border: 1px solid #ddd; border-radius: 8px; padding: 1rem; margin-bottom: 1rem; }
 label { display: block; margin: .5rem 0 .25rem; font-weight: 500; }
 select, input[type=text], input[type=file] { width: 100%; padding: .4rem; margin-bottom: .5rem; }
 button { padding: .5rem 1rem; cursor: pointer; }
 .flash { background: #eef; border: 1px solid #99c; padding: .5rem; border-radius: 4px; margin-bottom: 1rem; }
 .err { background: #fee; border-color: #c99; }
 .hint { font-size: .85em; color: #666; }
 a { color: #075e54; }
</style>
</head>
<body>
<h1>Demo Bot</h1>
<p class="hint">Send messages as the customer. View the chat at <a href="/demo" target="_blank">/demo</a>.</p>

{{ if .Flash }}<div class="flash {{ if .FlashErr }}err{{ end }}">{{ .Flash }}</div>{{ end }}

<section>
 <form method="POST" action="/demo/bot/send" enctype="multipart/form-data">
  <label for="type">Message type</label>
  <select name="type" id="type" required>
   <option value="text">Text</option>
   <option value="image">Image</option>
   <option value="audio">Audio</option>
   <option value="video">Video</option>
  </select>

  <label for="body">Text / caption</label>
  <input type="text" name="body" id="body" placeholder="Message text or media caption">

  <label for="file">Media file</label>
  <input type="file" name="file" id="file" accept="image/*,audio/*,video/*">

  <p class="hint">Text is required for text messages. File is required for media types.</p>
  <button type="submit">Send as customer</button>
 </form>
</section>
</body>
</html>`))

type demoBotData struct {
	Flash    string
	FlashErr bool
}

func (a *App) handleDemoChat(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/demo" || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := demoChatTmpl.Execute(w, nil); err != nil {
		log.Printf("demo chat template: %v", err)
	}
}

func (a *App) handleDemoEvents(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/demo/events" || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")

	ch, snapshot, unsub := a.demo.subscribe()
	defer unsub()

	for _, m := range snapshot {
		writeDemoSSE(w, m)
	}
	flusher.Flush()

	ka := time.NewTicker(20 * time.Second)
	defer ka.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case m, open := <-ch:
			if !open {
				return
			}
			writeDemoSSE(w, m)
			flusher.Flush()
		case <-ka.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func writeDemoSSE(w http.ResponseWriter, m DemoMessage) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
}

func (a *App) handleDemoMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/demo/media/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	path, mimeType, ok := a.demo.openMedia(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mimeType)
	http.ServeFile(w, r, path)
}

func (a *App) handleDemoBot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/demo/bot" || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	data := demoBotData{
		Flash:    r.URL.Query().Get("flash"),
		FlashErr: r.URL.Query().Get("err") == "1",
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := demoBotTmpl.Execute(w, data); err != nil {
		log.Printf("demo bot template: %v", err)
	}
}

func (a *App) handleDemoBotSend(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/demo/bot/send" || r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, demoMaxUpload)
	if err := r.ParseMultipartForm(demoMaxUpload); err != nil {
		redirectDemoFlash(w, r, "bad form: "+err.Error(), true)
		return
	}

	msgType := strings.TrimSpace(r.FormValue("type"))
	body := r.FormValue("body")
	if err := validateDemoType(msgType); err != nil {
		redirectDemoFlash(w, r, err.Error(), true)
		return
	}

	msg := DemoMessage{Type: msgType, Body: body}

	if msgType == "text" {
		if strings.TrimSpace(body) == "" {
			redirectDemoFlash(w, r, "text is required for text messages", true)
			return
		}
	} else {
		file, header, err := r.FormFile("file")
		if err != nil {
			redirectDemoFlash(w, r, "file is required for media messages", true)
			return
		}
		defer file.Close()

		mimeType := header.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = mime.TypeByExtension(filepath.Ext(header.Filename))
		}
		if mimeType == "" || mimeType == "application/octet-stream" {
			mimeType = mimeFromExt(filepath.Ext(header.Filename))
		}
		id, err := a.demo.saveMedia(file, mimeType, header.Filename)
		if err != nil {
			redirectDemoFlash(w, r, "save media: "+err.Error(), true)
			return
		}
		msg.MediaURL = "/demo/media/" + id
	}

	a.handleDemoInbound(msg)
	redirectDemoFlash(w, r, "message sent", false)
}

func redirectDemoFlash(w http.ResponseWriter, r *http.Request, msg string, isErr bool) {
	q := url.Values{}
	q.Set("flash", msg)
	if isErr {
		q.Set("err", "1")
	}
	http.Redirect(w, r, "/demo/bot?"+q.Encode(), http.StatusSeeOther)
}
