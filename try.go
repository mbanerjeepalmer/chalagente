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
	"github.com/mbanerjeepalmer/chalagente/internal/layout"
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
		Name:    "Birrias El Chalán",
		Hours:   "Mar-Dom 8:00-15:00 · Lunes cerrado",
		Address: "Esquina de Calle Donato Guerra y 5 de Mayo, Guadalajara, Jalisco",
		ExtraInfo: "Puesto callejero de birria de chivo, estilo tapatío. La birria se marina 24h con chiles guajillo, ancho, ajo, comino y nuestro recado secreto. " +
			"Servimos en tacos dorados, quesabirria, consomé y por kilo para llevar. Salsa de molcajete picosa. " +
			"Acompañamos con tortillas hechas a mano, cebollita, cilantro y limón. " +
			"Precio: taco $25, quesabirria $40, consomé $50, kilo $380. Solo efectivo. " +
			"Hablamos español; con turistas podemos contestar en inglés o francés.",
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

var tryTmpl = template.Must(template.New("try").Parse(`<!doctype html><html lang="es-MX"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Demo · Chalagente — chatea con un agente sin registrarte</title>
` + layout.FaviconLink + `
` + layout.FontsLink + `
<style>
:root {
  --wall:#f1ead9; --wall-shade:#e6dec7; --plaster:#ece2cb; --bone:#faf6ea;
  --ink:#1c1a16; --ink-soft:#3a352c; --muted:#6b6354; --line:rgba(28,26,22,0.14);
  --terracotta:#b5482e; --terracotta-deep:#8a3320; --ochre:#c8932b; --indigo:#25406e; --leaf:#4f6a3a;
}
*{box-sizing:border-box}
html,body{margin:0;padding:0;background:var(--wall);color:var(--ink-soft);font-family:"Inter","Helvetica Neue",sans-serif;line-height:1.55;
  background-image: radial-gradient(rgba(110,90,60,0.05) 1px, transparent 1px), linear-gradient(180deg,var(--wall),var(--wall-shade));
  background-size: 3px 3px, 100% 100%;}
h1,h2,h3{font-family:"Cormorant Garamond",Georgia,serif;color:var(--ink);font-weight:600;letter-spacing:-0.005em}
a{color:var(--terracotta-deep)}
.topbar{display:flex;justify-content:space-between;align-items:center;padding:.9rem 1.6rem;border-bottom:1px solid var(--line);background:rgba(241,234,217,0.95);backdrop-filter:blur(6px);position:sticky;top:0;z-index:50}
.topbar .logo{display:flex;align-items:center;gap:.55rem;font-family:"Cormorant Garamond",serif;font-weight:700;font-size:1.3rem;color:var(--ink);text-decoration:none}
.topbar .logo-mark{width:28px;height:28px;border-radius:50%;background:var(--terracotta);display:grid;place-items:center;color:var(--bone);font-family:"Cormorant Garamond",serif;font-weight:700;font-size:.95rem;box-shadow:inset 0 -2px 0 rgba(0,0,0,0.15)}
.topbar a{text-decoration:none;color:var(--ink-soft)}
.btn{display:inline-flex;align-items:center;gap:.4rem;padding:.55rem 1rem;border-radius:6px;font-weight:600;font-size:.92rem;background:var(--terracotta);color:var(--bone);text-decoration:none;border:none;cursor:pointer;box-shadow:0 2px 0 var(--terracotta-deep)}
.btn-ghost{background:transparent;color:var(--ink);border:1px solid var(--ink);box-shadow:none}
.banner{background:rgba(200,147,43,0.12);border-bottom:1px solid var(--line);padding:.7rem 1.6rem;font-size:.9rem;color:var(--ink-soft)}
.banner strong{color:var(--terracotta-deep);font-weight:600}
.layout{display:grid;grid-template-columns:380px 1fr;gap:1.5rem;padding:1.5rem;max-width:1280px;margin:0 auto;align-items:start}
@media(max-width:880px){.layout{grid-template-columns:1fr;padding:1rem}}
.sidebar{padding:1.5rem;background:var(--bone);border:1px solid var(--line);border-radius:6px;position:relative;transition:box-shadow .25s ease, transform .25s ease}
.sidebar.ringed{box-shadow:0 0 0 4px rgba(181,72,46,0.18), 0 0 0 6px rgba(181,72,46,0.35); animation:pulse 2.4s ease-in-out infinite}
@keyframes pulse{0%,100%{box-shadow:0 0 0 4px rgba(181,72,46,0.18), 0 0 0 6px rgba(181,72,46,0.35)} 50%{box-shadow:0 0 0 8px rgba(181,72,46,0.10), 0 0 0 10px rgba(181,72,46,0.20)}}
.sidebar h2{margin:0 0 .25rem;font-size:1.5rem}
.sidebar p.muted{color:var(--muted);font-size:.88em;margin:0 0 1rem}
.sidebar label{display:block;font-size:.74em;color:var(--muted);text-transform:uppercase;letter-spacing:.06em;margin:.9rem 0 .3rem;font-weight:600}
.sidebar input,.sidebar textarea{width:100%;padding:.55rem .7rem;background:var(--wall);color:var(--ink);border:1px solid var(--line);border-radius:4px;font-size:.93rem;font-family:inherit}
.sidebar input:focus,.sidebar textarea:focus{outline:none;border-color:var(--terracotta);background:var(--bone)}
.sidebar textarea{resize:vertical;min-height:8rem}
.sidebar .hint{color:var(--muted);font-size:.78em;margin:.25rem 0 0;font-style:italic}
.sidebar .savebtn{margin-top:1rem;padding:.6rem 1rem;background:var(--terracotta);color:var(--bone);border:none;border-radius:4px;font-weight:600;font-size:.92rem;cursor:pointer;width:100%;box-shadow:0 2px 0 var(--terracotta-deep)}
.sidebar .savebtn.saved{background:var(--leaf);box-shadow:0 2px 0 #2f4422}
.actions{display:flex;gap:.5rem;margin-top:.8rem}
.actions button{flex:1;padding:.45rem;background:transparent;border:1px solid var(--line);color:var(--muted);border-radius:4px;font-size:.8em;cursor:pointer;font-family:inherit}
.actions button:hover{color:var(--ink);border-color:var(--ink)}
.tooltip{position:absolute;top:-14px;right:-14px;background:var(--ink);color:var(--bone);padding:.55rem .85rem;border-radius:6px;font-size:.82rem;font-weight:500;box-shadow:0 6px 16px rgba(0,0,0,0.2);opacity:0;transform:translateY(-6px) scale(.95);transition:opacity .35s ease, transform .35s ease;pointer-events:none;max-width:260px;line-height:1.35;z-index:5}
.tooltip.visible{opacity:1;transform:translateY(0) scale(1)}
.tooltip::after{content:"";position:absolute;bottom:-6px;right:36px;width:12px;height:12px;background:var(--ink);transform:rotate(45deg)}

/* The WhatsApp chat panel: keep clone styling. */
.chatpane{display:flex;flex-direction:column;background:#0e1620;border-radius:6px;overflow:hidden;border:1px solid var(--line);box-shadow:0 8px 24px rgba(40,30,15,0.10);min-height:560px}
.phead{display:flex;align-items:center;gap:.6rem;padding:.8rem 1.2rem;background:#075e54;color:white;border-bottom:1px solid #04443c}
.phead .avatar{width:36px;height:36px;border-radius:50%;background:#25d366;display:grid;place-items:center;font-weight:700;color:#04130b}
.phead .name{font-weight:600}.phead .sub{font-size:.75em;opacity:.85}
.chat{flex:1;overflow-y:auto;padding:1rem 1.2rem;background:#ece5dd;display:flex;flex-direction:column;gap:.4rem;min-height:380px}
.bubble{max-width:75%;padding:.55rem .75rem;border-radius:12px;font-size:.95rem;color:#222;box-shadow:0 1px 1px rgba(0,0,0,.08);word-wrap:break-word;font-family:"Inter",sans-serif}
.bubble.in{background:#dcf8c6;align-self:flex-end;border-bottom-right-radius:2px}
.bubble.out{background:white;align-self:flex-start;border-bottom-left-radius:2px}
.bubble small{display:block;color:#888;font-size:.7em;margin-top:.2rem}
audio{width:100%;margin-top:.25rem;height:32px}
.composer{display:flex;gap:.5rem;padding:.6rem .8rem;background:#f0f0f0;border-top:1px solid #ccc}
.composer input[type=text]{flex:1;padding:.55rem .9rem;border:none;border-radius:18px;font-size:.95rem;font-family:"Inter",sans-serif}
.composer button{border:none;border-radius:50%;width:42px;height:42px;background:#25d366;color:white;font-size:1.2rem;cursor:pointer}
.composer .audiobtn{background:#075e54}
.composer input[type=file]{display:none}
.cta-footer{padding:.7rem 1.2rem;background:#0e1620;border-top:1px solid var(--line);display:flex;justify-content:space-between;align-items:center;font-size:.85em;color:#8a96a6}
.cta-footer.highlight{background:linear-gradient(90deg,rgba(181,72,46,.18),rgba(200,147,43,.08));color:var(--bone)}
.cta-footer a{color:#dcf8c6;font-weight:600;text-decoration:none}
.cta-footer.highlight a{color:var(--bone);background:var(--terracotta);padding:.35rem .8rem;border-radius:4px;box-shadow:0 2px 0 var(--terracotta-deep)}
</style>
</head><body>
<div class="topbar">
 <a class="logo" href="/"><span class="logo-mark">C</span><span>Chalagente</span></a>
 <div style="display:flex;gap:.6rem;align-items:center">
  <a href="/sign-in" style="font-size:.92rem">Iniciar sesión</a>
  <a class="btn" href="/sign-up">Crear cuenta →</a>
 </div>
</div>
<div class="banner">
 <strong>Modo demo</strong> · Edita los datos del negocio a la izquierda y mira cómo el agente los usa. Nada se guarda al cerrar la pestaña.
</div>
<div class="layout">
 <div class="sidebar" id="sidebar">
  <div class="tooltip" id="bizTooltip">¡Bien! Ahora cuéntale de <em>tu</em> negocio aquí — luego mándale más mensajes.</div>
  <h2>Mi negocio</h2>
  <p class="muted">Estos son los datos que el agente usa para responder. Cámbialos y prueba otra vez.</p>
  <form id="bizform" onsubmit="return saveBiz(event)">
   <label>Nombre</label><input name="name" value="{{ .Business.Name }}">
   <label>Horario</label><input name="hours" value="{{ .Business.Hours }}">
   <label>Dirección</label><textarea name="address" rows="2">{{ .Business.Address }}</textarea>
   <label>Información extra (menú, precios, formas de pago, políticas, idiomas...)</label>
   <textarea name="extra_info" rows="9">{{ .Business.ExtraInfo }}</textarea>
   <p class="hint">Cuéntale lo que un cliente nuevo querría saber: qué vendes, cuánto cuesta, cómo se paga, en qué idiomas hablas.</p>
   <button class="savebtn" id="savebtn">Guardar datos</button>
  </form>
  <div class="actions">
   <button type="button" onclick="resetChat()">Borrar chat</button>
   <button type="button" onclick="resetBiz()">Restaurar ejemplo</button>
  </div>
 </div>
 <div class="chatpane">
  <div class="phead">
   <div class="avatar" id="avatar">B</div>
   <div><div class="name" id="bizName">{{ .Business.Name }}</div><div class="sub">simulador WhatsApp — sin número real</div></div>
  </div>
  <div class="chat" id="chat"></div>
  <form class="composer" onsubmit="return sendText(event)">
   <input type="file" id="audio" accept="audio/*" onchange="sendAudio()">
   <button type="button" class="audiobtn" onclick="document.getElementById('audio').click()" title="Subir nota de voz">🎙</button>
   <input type="text" id="text" value="Bonjour, qu'est-ce que la birria ?" placeholder="Escribe como si fueras un cliente" autocomplete="off">
   <button type="submit" title="Enviar">➤</button>
  </form>
  <div class="cta-footer" id="ctaFoot">
   <span>¿Te gusta cómo responde?</span>
   <a href="/sign-up">Conecta tu WhatsApp →</a>
  </div>
 </div>
</div>
<script>
const chat = document.getElementById('chat');
const textInput = document.getElementById('text');
const bizName = document.getElementById('bizName');
const avatar = document.getElementById('avatar');
const sidebar = document.getElementById('sidebar');
const tooltip = document.getElementById('bizTooltip');
const ctaFoot = document.getElementById('ctaFoot');

let sentCount = 0;
let bizEdited = false;

function avatarLetter(name){return (name||'B').trim().slice(0,1).toUpperCase()}

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

function highlightBusiness() {
 if (bizEdited) return;
 sidebar.classList.add('ringed');
 setTimeout(()=>tooltip.classList.add('visible'), 250);
}
function clearHighlight(){
 sidebar.classList.remove('ringed');
 tooltip.classList.remove('visible');
}
function highlightSignup(){ ctaFoot.classList.add('highlight'); }

async function loadHistory() {
 const r = await fetch('/demo/history');
 const d = await r.json();
 chat.innerHTML = '';
 for (const m of d.messages) bubble(m.dir, m.body, null, null, m.kind);
 sentCount = (d.messages||[]).filter(m=>m.dir==='in').length;
 if (sentCount === 0) {
  bubble('out', '¡Hola! Soy el agente de ' + bizName.textContent + '. Escríbeme como si fueras un cliente. Mándale al botón ➤ para probar la pregunta prellenada.');
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
 onAfterSend();
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
 onAfterSend();
}

function onAfterSend(){
 sentCount++;
 if (sentCount === 1 && !bizEdited) highlightBusiness();
 if (sentCount >= 2 && bizEdited) highlightSignup();
}

async function saveBiz(ev) {
 ev.preventDefault();
 bizEdited = true;
 clearHighlight();
 const fd = new FormData(document.getElementById('bizform'));
 const r = await fetch('/demo/business', {method:'POST', body: fd});
 if (r.ok) {
  const d = await r.json();
  bizName.textContent = d.name;
  avatar.textContent = avatarLetter(d.name);
  const btn = document.getElementById('savebtn');
  btn.textContent = '✓ Guardado · ahora prueba otro mensaje';
  btn.classList.add('saved');
  setTimeout(() => { btn.textContent = 'Guardar datos'; btn.classList.remove('saved'); }, 2500);
 }
 return false;
}

async function resetChat() {
 await fetch('/demo/reset', {method:'POST'});
 chat.innerHTML = '';
 sentCount = 0;
 loadHistory();
}

async function resetBiz() {
 // Re-fetches server defaults by clearing and reloading via the cookie session would be easier;
 // simplest path: reload the page so we get the prefilled values from the server again.
 location.reload();
}

// Any keystroke in the form counts as editing the business
document.getElementById('bizform').addEventListener('input', () => { bizEdited = true; clearHighlight(); });
sidebar.addEventListener('click', () => { if (sidebar.classList.contains('ringed')) clearHighlight(); });

loadHistory();
</script>
</body></html>`))
