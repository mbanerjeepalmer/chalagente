package main

import (
	"context"
	"crypto/rand"
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
)

const tryCookieName = "chala_try"

type demoMessage struct {
	Time time.Time `json:"time"`
	Dir  string    `json:"dir"` // "in" (customer) | "out" (bot)
	Body string    `json:"body"`
	Kind string    `json:"kind"` // "text" | "audio"
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

// tryBusiness mirrors the fields the agent actually reads off of a tenant.
// Kept here (not in internal/store) because demo sessions are in-memory only.
type tryBusiness struct {
	Name      string `json:"name"`
	Hours     string `json:"hours"`
	Address   string `json:"address"`
	ExtraInfo string `json:"extra_info"`
}

type trySession struct {
	mu       sync.Mutex
	Business tryBusiness
	History  []demoMessage
	Touched  time.Time
}

type tryStore struct {
	mu sync.Mutex
	m  map[string]*trySession
}

func newTryStore() *tryStore { return &tryStore{m: make(map[string]*trySession)} }

func (s *tryStore) getOrCreate(id string) *trySession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ses, ok := s.m[id]; ok {
		ses.Touched = time.Now()
		return ses
	}
	ses := &trySession{
		Business: defaultTryBusiness(),
		Touched:  time.Now(),
	}
	s.m[id] = ses
	return ses
}

func defaultTryBusiness() tryBusiness {
	return tryBusiness{
		Name:    "Café Demo",
		Hours:   "Lun-Vie 9:00-20:00 · Sáb-Dom 10:00-22:00",
		Address: "Av. Reforma 123, Ciudad de México",
		ExtraInfo: "Aceptamos efectivo, tarjeta y transferencia. Tenemos opciones vegetarianas y sin gluten. " +
			"Hacemos pedidos para llevar. Para reservas de más de 6 personas escríbenos con un día de anticipación.",
	}
}

func (a *App) try() *tryStore {
	a.tryOnce.Do(func() { a.tryState = newTryStore() })
	return a.tryState
}

// trySessionFor reads (or sets) the demo cookie and returns the corresponding
// session. Sets the cookie if missing.
func (a *App) trySessionFor(w http.ResponseWriter, r *http.Request) *trySession {
	var id string
	if c, err := r.Cookie(tryCookieName); err == nil && c.Value != "" {
		id = c.Value
	}
	if id == "" {
		id = randomToken()
		http.SetCookie(w, &http.Cookie{
			Name:     tryCookieName,
			Value:    id,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   60 * 60 * 24, // 24h
		})
	}
	return a.try().getOrCreate(id)
}

func randomToken() string {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// ---- Handlers ----

func (a *App) handleTryPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/demo" {
		http.NotFound(w, r)
		return
	}
	ses := a.trySessionFor(w, r)
	ses.mu.Lock()
	biz := ses.Business
	ses.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tryTmpl.Execute(w, struct{ Business tryBusiness }{biz}); err != nil {
		log.Printf("tryTmpl: %v", err)
	}
}

func (a *App) handleTryBusiness(w http.ResponseWriter, r *http.Request) {
	ses := a.trySessionFor(w, r)
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		ses.mu.Lock()
		ses.Business.Name = strings.TrimSpace(r.PostForm.Get("name"))
		ses.Business.Hours = strings.TrimSpace(r.PostForm.Get("hours"))
		ses.Business.Address = strings.TrimSpace(r.PostForm.Get("address"))
		ses.Business.ExtraInfo = strings.TrimSpace(r.PostForm.Get("extra_info"))
		biz := ses.Business
		ses.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(biz)
		return
	}
	ses.mu.Lock()
	biz := ses.Business
	ses.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(biz)
}

func (a *App) handleTrySend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ses := a.trySessionFor(w, r)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var (
		incomingText string
		transcript   string
		hadAudio     bool
	)

	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
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

	ses.mu.Lock()
	biz := ses.Business
	inKind := "text"
	if hadAudio {
		inKind = "audio"
	}
	ses.History = append(ses.History, demoMessage{
		Time: time.Now(), Dir: "in", Body: incomingText, Kind: inKind,
	})
	histCopy := append([]demoMessage(nil), ses.History...)
	ses.mu.Unlock()

	bc := agent.BusinessContext{
		Name:      biz.Name,
		Hours:     biz.Hours,
		Address:   biz.Address,
		ExtraInfo: biz.ExtraInfo,
		Now:       time.Now(),
	}
	req := agent.Request{
		SystemPrompt: agent.BuildSystemPrompt(bc),
		History:      demoHistoryToAgent(histCopy),
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

	ses.mu.Lock()
	ses.History = append(ses.History, demoMessage{
		Time: time.Now(), Dir: "out", Body: reply.Text, Kind: "text",
	})
	ses.mu.Unlock()

	out := map[string]any{
		"reply":      reply.Text,
		"transcript": transcript,
		"has_audio":  false,
	}
	if hadAudio {
		syn, err := a.Voice.Synthesize(ctx, reply.Text, "default")
		if err == nil {
			out["has_audio"] = true
			out["audio_b64"] = base64.StdEncoding.EncodeToString(syn.Audio)
			out["audio_mime"] = syn.MimeType
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (a *App) handleTryHistory(w http.ResponseWriter, r *http.Request) {
	ses := a.trySessionFor(w, r)
	ses.mu.Lock()
	hist := append([]demoMessage(nil), ses.History...)
	ses.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"messages": hist})
}

func (a *App) handleTryReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ses := a.trySessionFor(w, r)
	ses.mu.Lock()
	ses.History = nil
	ses.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// ---- Template ----

var tryTmpl = template.Must(template.New("try").Parse(`<!doctype html><html lang="es"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Probar Chalagente — chatea con un agente sin registrarte</title>
<style>
:root { --bg:#0b0f14; --panel:#141b24; --border:rgba(255,255,255,0.08); --text:#e7edf3; --muted:#8a96a6; --accent:#25d366; }
*{box-sizing:border-box}
html,body{margin:0;padding:0;background:var(--bg);color:var(--text);font-family:-apple-system,BlinkMacSystemFont,"Inter","Segoe UI",sans-serif;line-height:1.5}
.topbar{display:flex;justify-content:space-between;align-items:center;padding:.8rem 1.5rem;border-bottom:1px solid var(--border);background:rgba(11,15,20,.85);backdrop-filter:blur(8px);position:sticky;top:0;z-index:5}
.topbar .logo{display:flex;align-items:center;gap:.5rem;font-weight:700}
.topbar .logo-mark{width:24px;height:24px;border-radius:6px;background:linear-gradient(135deg,#25d366,#128c7e);display:grid;place-items:center;color:#04130b;font-weight:800;font-size:.8rem}
.topbar a{color:inherit;text-decoration:none}
.btn{display:inline-flex;align-items:center;gap:.4rem;padding:.5rem .9rem;border-radius:999px;font-weight:600;font-size:.9rem;background:var(--accent);color:#04130b;text-decoration:none}
.banner{background:linear-gradient(90deg,rgba(37,211,102,.15),rgba(18,140,126,.05));padding:.8rem 1.5rem;border-bottom:1px solid var(--border);font-size:.9rem;color:#cde}
.banner strong{color:#7be4a8}
.layout{display:grid;grid-template-columns:340px 1fr;gap:0;height:calc(100vh - 98px);min-height:600px}
@media(max-width:820px){.layout{grid-template-columns:1fr;height:auto}.sidebar{border-right:0;border-bottom:1px solid var(--border)}}
.sidebar{padding:1.5rem;background:var(--panel);border-right:1px solid var(--border);overflow-y:auto}
.sidebar h2{margin:0 0 .25rem;font-size:1rem}
.sidebar p.muted{color:var(--muted);font-size:.85em;margin:0 0 1rem}
.sidebar label{display:block;font-size:.78em;color:var(--muted);text-transform:uppercase;letter-spacing:.04em;margin:.9rem 0 .25rem}
.sidebar input,.sidebar textarea{width:100%;padding:.55rem .65rem;background:#0e1620;color:var(--text);border:1px solid var(--border);border-radius:6px;font-size:.92rem;font-family:inherit}
.sidebar textarea{resize:vertical}
.sidebar .savebtn{margin-top:1rem;padding:.55rem 1rem;background:var(--accent);color:#04130b;border:none;border-radius:6px;font-weight:600;font-size:.9rem;cursor:pointer;width:100%}
.sidebar .savebtn.saved{background:#1f6f4a;color:#e8ffea}
.chatpane{display:flex;flex-direction:column;background:#0e1620;min-height:0}
.phead{display:flex;align-items:center;gap:.6rem;padding:.8rem 1.2rem;background:#075e54;color:white;border-bottom:1px solid #04443c}
.phead .avatar{width:36px;height:36px;border-radius:50%;background:#25d366;display:grid;place-items:center;font-weight:700;color:#04130b}
.phead .name{font-weight:600}.phead .sub{font-size:.75em;opacity:.85}
.chat{flex:1;overflow-y:auto;padding:1rem 1.2rem;background:#ece5dd;display:flex;flex-direction:column;gap:.4rem}
.bubble{max-width:75%;padding:.55rem .75rem;border-radius:12px;font-size:.95rem;color:#222;box-shadow:0 1px 1px rgba(0,0,0,.08);word-wrap:break-word}
.bubble.in{background:#dcf8c6;align-self:flex-end;border-bottom-right-radius:2px}
.bubble.out{background:white;align-self:flex-start;border-bottom-left-radius:2px}
.bubble small{display:block;color:#888;font-size:.7em;margin-top:.2rem}
audio{width:100%;margin-top:.25rem;height:32px}
.composer{display:flex;gap:.5rem;padding:.6rem .8rem;background:#f0f0f0;border-top:1px solid #ccc}
.composer input[type=text]{flex:1;padding:.55rem .9rem;border:none;border-radius:18px;font-size:.95rem}
.composer button{border:none;border-radius:50%;width:42px;height:42px;background:#25d366;color:white;font-size:1.2rem;cursor:pointer}
.composer .audiobtn{background:#075e54}
.composer input[type=file]{display:none}
.cta-footer{padding:.7rem 1.2rem;background:#0e1620;border-top:1px solid var(--border);display:flex;justify-content:space-between;align-items:center;font-size:.85em;color:var(--muted)}
.cta-footer a{color:#7be4a8;font-weight:600;text-decoration:none}
.actions{display:flex;gap:.5rem;margin-top:.8rem}
.actions button{flex:1;padding:.4rem;background:transparent;border:1px solid var(--border);color:var(--muted);border-radius:6px;font-size:.8em;cursor:pointer}
</style>
</head><body>
<div class="topbar">
 <a class="logo" href="/"><div class="logo-mark">C</div><span>Chalagente</span></a>
 <a class="btn" href="/signup">Crear cuenta →</a>
</div>
<div class="banner">
 <strong>Modo demo</strong> · Edita los datos del negocio a la izquierda y mira cómo el agente los usa. Nada se guarda al cerrar la pestaña.
</div>
<div class="layout">
 <div class="sidebar">
  <h2>Mi negocio</h2>
  <p class="muted">Estos son los datos que el agente usa para responder. Cámbialos y prueba otra vez.</p>
  <form id="bizform" onsubmit="return saveBiz(event)">
   <label>Nombre</label><input name="name" value="{{ .Business.Name }}">
   <label>Horarios</label><input name="hours" value="{{ .Business.Hours }}">
   <label>Dirección</label><input name="address" value="{{ .Business.Address }}">
   <label>Información extra (FAQ, precios, políticas)</label>
   <textarea name="extra_info" rows="8">{{ .Business.ExtraInfo }}</textarea>
   <button class="savebtn" id="savebtn">Guardar datos</button>
  </form>
  <div class="actions">
   <button onclick="resetChat()">Borrar chat</button>
   <button onclick="resetBiz()">Restaurar defaults</button>
  </div>
 </div>
 <div class="chatpane">
  <div class="phead">
   <div class="avatar" id="avatar">C</div>
   <div><div class="name" id="bizName">{{ .Business.Name }}</div><div class="sub">simulador — sin WhatsApp real</div></div>
  </div>
  <div class="chat" id="chat"></div>
  <form class="composer" onsubmit="return sendText(event)">
   <input type="file" id="audio" accept="audio/*" onchange="sendAudio()">
   <button type="button" class="audiobtn" onclick="document.getElementById('audio').click()" title="Enviar nota de voz">🎙</button>
   <input type="text" id="text" placeholder="Escribe como si fueras un cliente: «¿a qué hora abren?»" autocomplete="off">
   <button type="submit" title="Enviar">➤</button>
  </form>
  <div class="cta-footer">
   <span>¿Te gusta cómo responde?</span>
   <a href="/signup">Conecta tu WhatsApp →</a>
  </div>
 </div>
</div>
<script>
const chat = document.getElementById('chat');
const textInput = document.getElementById('text');
const bizName = document.getElementById('bizName');
const avatar = document.getElementById('avatar');

function bubble(dir, body, audioB64, audioMime, kind) {
 const div = document.createElement('div');
 div.className = 'bubble ' + dir;
 div.textContent = body || (kind === 'audio' ? '🎙 [nota de voz]' : '');
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
 const r = await fetch('/demo/history');
 const d = await r.json();
 chat.innerHTML = '';
 for (const m of d.messages) bubble(m.dir, m.body, null, null, m.kind);
 if (!d.messages || d.messages.length === 0) {
  bubble('out', '¡Hola! Soy el agente de ' + bizName.textContent + '. ¿En qué te puedo ayudar?');
 }
}

async function sendText(ev) {
 ev.preventDefault();
 const text = textInput.value.trim();
 if (!text) return false;
 textInput.value = '';
 bubble('in', text);
 const fd = new FormData(); fd.append('text', text);
 const r = await fetch('/demo/send', {method:'POST', body: fd});
 if (!r.ok) { bubble('out', '[error '+r.status+']'); return false; }
 const d = await r.json();
 bubble('out', d.reply, d.audio_b64, d.audio_mime);
 return false;
}

async function sendAudio() {
 const file = document.getElementById('audio').files[0];
 if (!file) return;
 bubble('in', '🎙 [nota de voz]', null, null, 'audio');
 const fd = new FormData(); fd.append('audio', file);
 const r = await fetch('/demo/send', {method:'POST', body: fd});
 if (!r.ok) { bubble('out', '[error '+r.status+']'); return; }
 const d = await r.json();
 if (d.transcript) bubble('out', '(transcripción: ' + d.transcript + ')');
 bubble('out', d.reply, d.audio_b64, d.audio_mime);
 document.getElementById('audio').value = '';
}

async function saveBiz(ev) {
 ev.preventDefault();
 const fd = new FormData(document.getElementById('bizform'));
 const r = await fetch('/demo/business', {method:'POST', body: fd});
 if (r.ok) {
  const d = await r.json();
  bizName.textContent = d.name;
  avatar.textContent = (d.name || 'C').slice(0,1).toUpperCase();
  const btn = document.getElementById('savebtn');
  btn.textContent = '✓ Guardado';
  btn.classList.add('saved');
  setTimeout(() => { btn.textContent = 'Guardar datos'; btn.classList.remove('saved'); }, 1500);
 }
 return false;
}

async function resetChat() {
 await fetch('/demo/reset', {method:'POST'});
 chat.innerHTML = '';
 loadHistory();
}

async function resetBiz() {
 const defaults = {name:'Café Demo', hours:'Lun-Vie 9:00-20:00 · Sáb-Dom 10:00-22:00', address:'Av. Reforma 123, Ciudad de México', extra_info:'Aceptamos efectivo, tarjeta y transferencia. Tenemos opciones vegetarianas y sin gluten.'};
 for (const k in defaults) document.querySelector('[name='+k+']').value = defaults[k];
 document.getElementById('bizform').dispatchEvent(new Event('submit', {cancelable:true}));
}

loadHistory();
</script>
</body></html>`))
