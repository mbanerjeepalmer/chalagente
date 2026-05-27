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
	if err := tryTmpl.Execute(w, tryView{Business: biz}); err != nil {
		log.Printf("tryTmpl: %v", err)
	}
}

// tryView is the data shape rendered into tryTmpl. IsAdmin swaps the
// marketing chrome for the admin nav and adds the customer/business
// flip toggle — see /admin/conversations/demo.
type tryView struct {
	Business tryBusiness
	IsAdmin  bool
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
		audioLang    string
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
			audioLang = tr.Language
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
		syn, err := a.Voice.Synthesize(ctx, reply.Text, voiceIDForLang(audioLang))
		if err != nil {
			out["audio_error"] = err.Error()
		} else {
			out["has_audio"] = true
			out["audio_b64"] = base64.StdEncoding.EncodeToString(syn.Audio)
			out["audio_mime"] = syn.MimeType
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// presetVoicePrompt is the demo's signature first-message phrase — the
// French question a visitor "asks" by hitting the prefilled voice-note
// button. Stays in French so the agent's reply uses the multilingual
// behaviour the demo is supposed to show off.
const presetVoicePrompt = "Bonjour, qu'est-ce que la birria ?"

// presetVoiceLang controls which ElevenLabs voice handles the synth — the
// pipeline's voiceIDForLang helper maps this back to ELEVENLABS_VOICE_FR
// in prod, the multilingual default otherwise.
const presetVoiceLang = "fr"

// handleTryPresetAudio returns the audio bytes for the prefilled voice
// note. The voice provider's cache (NewCachedProvider wraps the real
// ElevenLabs client in main.go) means subsequent requests hit memory, not
// the network. When no API key is configured the MockProvider returns an
// empty audio buffer and we serve a 404 so the JS falls back to the text
// flow gracefully.
func (a *App) handleTryPresetAudio(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	syn, err := a.Voice.Synthesize(ctx, presetVoicePrompt, voiceIDForLang(presetVoiceLang))
	if err != nil {
		http.Error(w, "synth: "+err.Error(), http.StatusBadGateway)
		return
	}
	if len(syn.Audio) == 0 {
		http.Error(w, "no audio", http.StatusNotFound)
		return
	}
	mime := syn.MimeType
	if mime == "" {
		mime = "audio/ogg"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=600")
	_, _ = w.Write(syn.Audio)
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
<style>` + layout.SharedStyles + layout.ChatPaneStyles + `
/* Demo-specific overrides — tighter line-height than the marketing
 * pages and a single-radial dotted background (the landing layers two).
 * Kept inline so the demo's local feel is obviously the local layer. */
body{line-height:1.55}
body{background-size: 3px 3px, 100% 100%}
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
/* chatpane, phead, chat, bubble and audio rules come from
 * layout.ChatPaneStyles at the top of this style block. */
.chatpane{min-height:560px}
.composer{display:flex;gap:.5rem;padding:.6rem .8rem;background:#f0f0f0;border-top:1px solid #ccc}
.preset-row{padding:.4rem .8rem .55rem;background:#f0f0f0;border-top:1px solid #e0e0e0;text-align:center}
.preset-btn{background:transparent;border:1px solid var(--terracotta);color:var(--terracotta-deep);border-radius:18px;padding:.35rem .9rem;font-size:.82rem;font-family:inherit;cursor:pointer}
.preset-btn:hover{background:rgba(181,72,46,0.08)}
.preset-btn:disabled{opacity:.6;cursor:wait}
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
 {{ if .IsAdmin }}
 <nav style="display:flex;gap:1.1rem;align-items:center;font-size:.92rem">
  <a href="/admin">Conversaciones</a>
  <a href="/admin/connection">Conexión</a>
  <a href="/admin/business">Información</a>
 </nav>
 {{ else }}
 <div style="display:flex;gap:.6rem;align-items:center">
  <a href="/sign-in" style="font-size:.92rem">Iniciar sesión</a>
  <a class="btn" href="/sign-up">Crear cuenta →</a>
 </div>
 {{ end }}
</div>
<div class="banner">
 {{ if .IsAdmin }}
  <strong>Demo</strong> · Esta es la conversación de ejemplo. Cambia entre vista cliente y negocio con el botón de arriba; nada se guarda en tu base de datos.
 {{ else }}
  <strong>Modo demo</strong> · Edita los datos del negocio a la izquierda y mira cómo el agente los usa. Nada se guarda al cerrar la pestaña.
 {{ end }}
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
 <div class="chatpane" id="chatpane">
  <div class="phead">
   <div class="avatar" id="avatar">B</div>
   <div style="flex:1"><div class="name" id="bizName">{{ .Business.Name }}</div><div class="sub">simulador WhatsApp — sin número real</div></div>
   {{ if .IsAdmin }}
   <button type="button" id="flipBtn" data-mode="customer" title="Cambiar perspectiva" style="margin-left:auto;background:transparent;border:1px solid rgba(255,255,255,0.35);color:#fff;border-radius:14px;padding:.25rem .7rem;font-size:.78rem;cursor:pointer">Ver como negocio</button>
   {{ end }}
  </div>
  <div class="chat" id="chat"></div>
  <form class="composer" onsubmit="return sendText(event)">
   <input type="file" id="audio" accept="audio/*" onchange="sendAudio()">
   <button type="button" class="audiobtn" onclick="document.getElementById('audio').click()" title="Subir nota de voz">🎙</button>
   <input type="text" id="text" value="Bonjour, qu'est-ce que la birria ?" placeholder="Escribe como si fueras un cliente" autocomplete="off">
   <button type="submit" title="Enviar">➤</button>
  </form>
  <div class="preset-row" id="presetRow">
   <button type="button" class="preset-btn" id="presetBtn" onclick="sendPresetVoice()" title="Mandar como nota de voz">▶ Enviar como nota de voz</button>
   <button type="button" class="preset-btn" id="micBtn" onclick="toggleLiveMic()" title="Hablar y ver la transcripción en vivo">🎤 Dictar</button>
  </div>
  <div class="cta-footer" id="ctaFoot">
   {{ if .IsAdmin }}
   <span>Demo · no afecta tus chats reales</span>
   <a href="/admin">← Volver a conversaciones</a>
   {{ else }}
   <span>¿Te gusta cómo responde?</span>
   <a href="/sign-up">Conecta tu WhatsApp →</a>
   {{ end }}
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
 const msgs = d.messages || [];
 for (const m of msgs) bubble(m.dir, m.body, null, null, m.kind);
 sentCount = msgs.filter(m=>m.dir==='in').length;
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

// sendPresetVoice fetches the server-synthesized French preset audio and
// posts it through the existing voice-note path, so the visitor sees
// (and hears) the same flow a real customer would. In dev without an
// ElevenLabs key the preset endpoint 404s and we fall back to sending
// the same text via sendText so the demo still works.
async function sendPresetVoice(){
 const btn = document.getElementById('presetBtn');
 const row = document.getElementById('presetRow');
 btn.disabled = true;
 try {
  const audioRes = await fetch('/demo/preset.ogg');
  if (!audioRes.ok) {
   textInput.value = 'Bonjour, qu\'est-ce que la birria ?';
   document.querySelector('.composer button[type=submit]').click();
   return;
  }
  const blob = await audioRes.blob();
  bubble('in', '🎙 [nota de voz]', null, null, 'audio');
  const fd = new FormData();
  fd.append('audio', blob, 'preset.ogg');
  const r = await fetch('/demo/send', {method:'POST', body: fd});
  if (!r.ok) { bubble('out', '[error '+r.status+']'); return; }
  const d = await r.json();
  if (d.transcript) bubble('out', '(transcripción: ' + d.transcript + ')');
  bubble('out', d.reply, d.audio_b64, d.audio_mime);
  onAfterSend();
 } finally {
  row.style.display = 'none';
 }
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

// Live-mic dictation. Streams 16kHz PCM16 mono to /demo/transcribe/ws and
// pipes partial transcripts into the composer's text field. When the user
// hits Dictar again we commit, wait for the final transcript, then stop.
// In dev without a streaming provider configured the WS rejects with 503
// and we surface a one-line note instead of breaking the rest of the demo.
let micState = null;
async function toggleLiveMic(){
 const btn = document.getElementById('micBtn');
 if (micState) { await stopLiveMic(); return; }
 btn.disabled = true;
 btn.textContent = '… conectando';
 try {
  const ws = new WebSocket(location.origin.replace(/^http/, 'ws') + '/demo/transcribe/ws');
  ws.binaryType = 'arraybuffer';
  await new Promise((res, rej) => {
   ws.onopen = res;
   ws.onerror = () => rej(new Error('no live transcription'));
  });
  const stream = await navigator.mediaDevices.getUserMedia({audio: true});
  const ctx = new (window.AudioContext || window.webkitAudioContext)({sampleRate: 16000});
  const src = ctx.createMediaStreamSource(stream);
  // Worklet code is a string we inline as a Blob URL so the demo stays
  // single-file. Captures float32 frames, downscales to int16 PCM and
  // posts the resulting buffer back to the main thread.
  const workletCode = "class P extends AudioWorkletProcessor { process(inputs){ const ch = inputs[0][0]; if(!ch) return true; const out = new Int16Array(ch.length); for (let i=0;i<ch.length;i++){ const s = Math.max(-1, Math.min(1, ch[i])); out[i] = s < 0 ? s * 0x8000 : s * 0x7fff; } this.port.postMessage(out.buffer, [out.buffer]); return true; } } registerProcessor('chala-pcm', P);";
  const blob = new Blob([workletCode], {type: 'application/javascript'});
  await ctx.audioWorklet.addModule(URL.createObjectURL(blob));
  const node = new AudioWorkletNode(ctx, 'chala-pcm');
  node.port.onmessage = (ev) => { if (ws.readyState === 1) ws.send(ev.data); };
  src.connect(node);
  node.connect(ctx.destination); // satisfies graph; gain 0 in destination won't actually play mic back

  ws.onmessage = (m) => {
   try {
    const d = JSON.parse(m.data);
    if (d.kind === 'partial' || d.kind === 'final') {
     textInput.value = d.text;
    }
    if (d.kind === 'final') { stopLiveMic(); }
    if (d.kind === 'error') { console.warn('transcribe:', d.text); }
   } catch(e){}
  };
  micState = {ws, ctx, stream, node, src};
  btn.disabled = false;
  btn.textContent = '⏹ Detener';
 } catch (err) {
  console.warn('mic:', err);
  btn.disabled = false;
  btn.textContent = '🎤 Dictar';
  alert('Dictado en vivo no disponible. Usa el botón 🎙 para subir un archivo.');
 }
}
async function stopLiveMic(){
 if (!micState) return;
 const s = micState;
 micState = null;
 try { s.node.disconnect(); s.src.disconnect(); } catch(e){}
 try { s.stream.getTracks().forEach(t => t.stop()); } catch(e){}
 try { if (s.ws.readyState === 1) s.ws.send(JSON.stringify({type:'commit'})); } catch(e){}
 try { await s.ctx.close(); } catch(e){}
 setTimeout(() => { try { s.ws.close(); } catch(e){} }, 500);
 const btn = document.getElementById('micBtn');
 btn.textContent = '🎤 Dictar';
}

// Any keystroke in the form counts as editing the business
document.getElementById('bizform').addEventListener('input', () => { bizEdited = true; clearHighlight(); });
sidebar.addEventListener('click', () => { if (sidebar.classList.contains('ringed')) clearHighlight(); });

loadHistory();

// Admin-only: flip toggle on the chat header. Toggles .from-business
// on the chatpane; layout.ChatPaneStyles already styles both sides.
(function(){
 const btn = document.getElementById('flipBtn');
 const pane = document.getElementById('chatpane');
 if (!btn || !pane) return;
 btn.addEventListener('click', () => {
  const asBiz = btn.dataset.mode === 'customer';
  pane.classList.toggle('from-business', asBiz);
  btn.dataset.mode = asBiz ? 'business' : 'customer';
  btn.textContent = asBiz ? 'Ver como cliente' : 'Ver como negocio';
 });
})();
</script>
</body></html>`))
