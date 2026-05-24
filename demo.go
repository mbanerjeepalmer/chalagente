package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mbanerjeepalmer/chalagente/internal/agent"
	"github.com/mbanerjeepalmer/chalagente/internal/store"
)

// demoHistory holds the in-memory chat history for each business's preview.
// Lives on App. Reset wipes it; not persisted, by design — the demo is a
// throwaway sandbox to show what the agent will say.
type demoHistory struct {
	mu      sync.Mutex
	byBiz   map[string][]demoMessage
}

type demoMessage struct {
	Time time.Time `json:"time"`
	Dir  string    `json:"dir"` // "in" (customer) | "out" (bot)
	Body string    `json:"body"`
	Kind string    `json:"kind"` // "text" | "audio"
}

func newDemoHistory() *demoHistory {
	return &demoHistory{byBiz: make(map[string][]demoMessage)}
}

func (d *demoHistory) append(bizID string, m demoMessage) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.byBiz[bizID] = append(d.byBiz[bizID], m)
	if len(d.byBiz[bizID]) > 50 {
		d.byBiz[bizID] = d.byBiz[bizID][len(d.byBiz[bizID])-50:]
	}
}

func (d *demoHistory) list(bizID string) []demoMessage {
	d.mu.Lock()
	defer d.mu.Unlock()
	src := d.byBiz[bizID]
	out := make([]demoMessage, len(src))
	copy(out, src)
	return out
}

func (d *demoHistory) reset(bizID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.byBiz, bizID)
}

func (a *App) demo() *demoHistory {
	a.demoOnce.Do(func() { a.demoState = newDemoHistory() })
	return a.demoState
}

// ---- Handlers ----

func (a *App) handleDemoPage(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := demoTmpl.Execute(w, struct {
		Business store.Business
	}{Business: b}); err != nil {
		log.Printf("demoTmpl: %v", err)
	}
}

func (a *App) handleDemoSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var (
		incomingText string
		transcript   string
		hadAudio     bool
	)

	ctype := r.Header.Get("Content-Type")
	if strings.HasPrefix(ctype, "multipart/form-data") {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
			return
		}
		incomingText = strings.TrimSpace(r.PostForm.Get("text"))
		if f, header, err := r.FormFile("audio"); err == nil {
			defer f.Close()
			hadAudio = true
			audioBytes, _ := io.ReadAll(f)
			mime := header.Header.Get("Content-Type")
			if mime == "" {
				mime = "audio/ogg"
			}
			tr, err := a.Voice.Transcribe(ctx, audioBytes, mime)
			if err != nil {
				http.Error(w, "transcribe: "+err.Error(), http.StatusInternalServerError)
				return
			}
			transcript = tr.Text
			if incomingText == "" {
				incomingText = transcript
			}
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		incomingText = strings.TrimSpace(r.PostForm.Get("text"))
	}

	if incomingText == "" && !hadAudio {
		http.Error(w, "empty message", http.StatusBadRequest)
		return
	}

	// Persist on the in-memory demo history.
	inKind := "text"
	if hadAudio {
		inKind = "audio"
	}
	a.demo().append(b.ID, demoMessage{
		Time: time.Now(), Dir: "in", Body: incomingText, Kind: inKind,
	})

	// Build request from current demo history.
	history := a.demo().list(b.ID)
	agentHist := demoHistoryToAgent(history)

	bc := businessContextFor(b)
	req := agent.Request{
		SystemPrompt: agent.BuildSystemPrompt(bc),
		History:      agentHist,
		Incoming: agent.Message{
			Role:      agent.RoleUser,
			Text:      incomingText,
			Timestamp: time.Now(),
		},
		Business: bc,
	}
	if hadAudio {
		req.Incoming.Attachments = []agent.Attachment{{Kind: "audio"}}
	}

	reply, err := a.Agent.Respond(ctx, req)
	if err != nil {
		http.Error(w, "agent: "+err.Error(), http.StatusInternalServerError)
		return
	}

	a.demo().append(b.ID, demoMessage{
		Time: time.Now(), Dir: "out", Body: reply.Text, Kind: "text",
	})

	out := map[string]any{
		"reply":      reply.Text,
		"transcript": transcript,
		"has_audio":  false,
	}

	// If the inbound was audio (or voice mode forces it), TTS the reply.
	if hadAudio || b.VoiceMode == "always" {
		voiceID := b.VoiceID
		if voiceID == "" {
			voiceID = "default"
		}
		syn, err := a.Voice.Synthesize(ctx, reply.Text, voiceID)
		if err == nil {
			out["has_audio"] = true
			out["audio_b64"] = base64.StdEncoding.EncodeToString(syn.Audio)
			out["audio_mime"] = syn.MimeType
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (a *App) handleDemoHistory(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"messages": a.demo().list(b.ID),
	})
}

func (a *App) handleDemoReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	a.demo().reset(b.ID)
	w.WriteHeader(http.StatusOK)
}

func demoHistoryToAgent(msgs []demoMessage) []agent.Message {
	out := make([]agent.Message, 0, len(msgs))
	// Drop the very last "in" message — it's the one currently being processed
	// and gets passed as Incoming.
	last := len(msgs) - 1
	if last >= 0 && msgs[last].Dir == "in" {
		msgs = msgs[:last]
	}
	for _, m := range msgs {
		role := agent.RoleUser
		if m.Dir == "out" {
			role = agent.RoleAssistant
		}
		out = append(out, agent.Message{
			Role:      role,
			Text:      m.Body,
			Timestamp: m.Time,
		})
	}
	return out
}

// ---- Template ----

var demoTmpl = template.Must(template.New("demo").Parse(`<!doctype html><html lang="es"><head><meta charset="utf-8"><title>Probar el agente — Chalagente</title>
<style>
body{font-family:system-ui,sans-serif;max-width:520px;margin:1rem auto;padding:0 1rem;color:#222;background:#e6ddd4}
h1{margin:.25rem 0;font-size:1.2rem}
.muted{color:#666;font-size:.85em}
.phone{background:#0e1620;border-radius:18px;padding:0;overflow:hidden;box-shadow:0 4px 24px rgba(0,0,0,0.2)}
.phead{display:flex;align-items:center;gap:.6rem;padding:.7rem 1rem;background:#075e54;color:white}
.avatar{width:34px;height:34px;border-radius:50%;background:#25d366;display:grid;place-items:center;font-weight:700;color:#04130b}
.chat{background:url('data:image/svg+xml;utf8,<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><rect width="40" height="40" fill="%23ece5dd"/></svg>') #ece5dd;height:50vh;overflow-y:auto;padding:.75rem;display:flex;flex-direction:column;gap:.4rem}
.bubble{max-width:80%;padding:.5rem .7rem;border-radius:12px;font-size:.92rem;line-height:1.3;word-wrap:break-word;box-shadow:0 1px 1px rgba(0,0,0,0.08)}
.bubble.in{background:#dcf8c6;align-self:flex-end;border-bottom-right-radius:2px}
.bubble.out{background:white;align-self:flex-start;border-bottom-left-radius:2px}
.bubble small{color:#888;font-size:.7rem;display:block;margin-top:.15rem}
.composer{display:flex;gap:.4rem;padding:.5rem;background:#f0f0f0;border-top:1px solid #ccc}
.composer input{flex:1;padding:.5rem .75rem;border:none;border-radius:18px;font-size:.95rem}
.composer button{padding:.5rem 1rem;border:none;border-radius:50%;width:44px;height:44px;background:#25d366;color:white;font-size:1.3rem;cursor:pointer}
.composer button.audio{background:#075e54}
.toolbar{display:flex;justify-content:space-between;align-items:center;padding:.5rem 0}
.toolbar a,.toolbar button{font-size:.85em;color:#555;background:none;border:1px solid #999;border-radius:4px;padding:.25rem .5rem;cursor:pointer;text-decoration:none}
audio{width:100%;margin-top:.25rem}
input[type=file]{display:none}
</style></head><body>
<div class="toolbar">
 <a href="/app">← Volver</a>
 <button onclick="resetChat()">Reiniciar chat</button>
</div>
<h1>Prueba el agente de {{ .Business.Name }}</h1>
<p class="muted">Esto es como te verán tus clientes en WhatsApp. Los mensajes se quedan en este simulador — no salen a nadie.</p>
<div class="phone">
 <div class="phead">
  <div class="avatar">{{ slice .Business.Name 0 1 }}</div>
  <div><div style="font-weight:600">{{ .Business.Name }}</div><div style="font-size:.75em;opacity:.8">en línea (simulador)</div></div>
 </div>
 <div class="chat" id="chat"></div>
 <form class="composer" id="composer" onsubmit="return sendText(event)">
  <label class="composer" style="margin:0;padding:0;background:none;border:none">
   <input type="file" id="audio" accept="audio/*" onchange="sendAudio()">
   <button type="button" class="audio" onclick="document.getElementById('audio').click()" title="Enviar audio">🎙</button>
  </label>
  <input id="text" placeholder="Escribe un mensaje" autocomplete="off">
  <button type="submit" title="Enviar">➤</button>
 </form>
</div>
<script>
const chat = document.getElementById('chat');
const textInput = document.getElementById('text');

function bubble(dir, body, audioB64, audioMime, kind) {
 const div = document.createElement('div');
 div.className = 'bubble ' + dir;
 div.textContent = body || (kind === 'audio' ? '[audio]' : '');
 if (audioB64) {
  const audio = document.createElement('audio');
  audio.controls = true;
  audio.src = 'data:' + (audioMime || 'audio/mpeg') + ';base64,' + audioB64;
  div.appendChild(audio);
 }
 chat.appendChild(div);
 chat.scrollTop = chat.scrollHeight;
}

async function loadHistory() {
 chat.innerHTML = '';
 const r = await fetch('/app/demo/history');
 const d = await r.json();
 for (const m of d.messages) bubble(m.dir, m.body, null, null, m.kind);
}

async function sendText(ev) {
 ev.preventDefault();
 const text = textInput.value.trim();
 if (!text) return false;
 textInput.value = '';
 bubble('in', text);
 const fd = new FormData();
 fd.append('text', text);
 const r = await fetch('/app/demo/send', {method:'POST', body: fd});
 if (!r.ok) { bubble('out', '[error '+r.status+']'); return false; }
 const d = await r.json();
 bubble('out', d.reply, d.audio_b64, d.audio_mime);
 return false;
}

async function sendAudio() {
 const file = document.getElementById('audio').files[0];
 if (!file) return;
 bubble('in', '🎙 [nota de voz]', null, null, 'audio');
 const fd = new FormData();
 fd.append('audio', file);
 const r = await fetch('/app/demo/send', {method:'POST', body: fd});
 if (!r.ok) { bubble('out', '[error '+r.status+']'); return; }
 const d = await r.json();
 if (d.transcript) bubble('out', '(transcripción: ' + d.transcript + ')');
 bubble('out', d.reply, d.audio_b64, d.audio_mime);
 document.getElementById('audio').value = '';
}

async function resetChat() {
 await fetch('/app/demo/reset', {method:'POST'});
 chat.innerHTML = '';
}

loadHistory();
</script>
</body></html>`))
