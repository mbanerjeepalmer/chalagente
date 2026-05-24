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

var landingTmpl = template.Must(template.New("landing").Parse(`<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Chalagente — Atención al cliente por WhatsApp con IA</title>
<meta name="description" content="Un agente de IA que responde las preguntas de tus clientes en WhatsApp, 24/7. Sin apps nuevas, sin fricción.">
<style>
 :root {
  --bg: #0b0f14;
  --bg-soft: #11161d;
  --panel: #141b24;
  --border: rgba(255,255,255,0.08);
  --text: #e7edf3;
  --muted: #8a96a6;
  --accent: #25d366;
  --accent-2: #128c7e;
  --radius: 14px;
 }
 * { box-sizing: border-box; }
 html, body { margin: 0; padding: 0; }
 body {
  font-family: -apple-system, BlinkMacSystemFont, "Inter", "Segoe UI", Roboto, sans-serif;
  background: var(--bg);
  color: var(--text);
  line-height: 1.6;
  -webkit-font-smoothing: antialiased;
 }
 a { color: inherit; text-decoration: none; }
 .container { max-width: 1120px; margin: 0 auto; padding: 0 1.5rem; }

 header.nav {
  position: sticky; top: 0; z-index: 10;
  backdrop-filter: blur(12px);
  background: rgba(11,15,20,0.7);
  border-bottom: 1px solid var(--border);
 }
 .nav-inner { display: flex; align-items: center; justify-content: space-between; padding: 1rem 0; }
 .logo { display: flex; align-items: center; gap: .6rem; font-weight: 700; letter-spacing: -0.01em; }
 .logo-mark {
  width: 28px; height: 28px; border-radius: 8px;
  background: linear-gradient(135deg, var(--accent), var(--accent-2));
  display: grid; place-items: center; color: #04130b; font-weight: 800;
 }
 .nav-links { display: flex; gap: 1.5rem; align-items: center; color: var(--muted); font-size: .92rem; }
 .nav-links a:hover { color: var(--text); }
 .btn {
  display: inline-flex; align-items: center; gap: .5rem;
  padding: .7rem 1.1rem; border-radius: 999px; font-weight: 600;
  transition: transform .12s ease, box-shadow .2s ease, background .2s ease;
  font-size: .95rem;
 }
 .btn-primary {
  background: var(--accent); color: #04130b;
  box-shadow: 0 8px 24px rgba(37,211,102,0.25);
 }
 .btn-primary:hover { transform: translateY(-1px); box-shadow: 0 10px 28px rgba(37,211,102,0.35); }
 .btn-ghost { border: 1px solid var(--border); color: var(--text); }
 .btn-ghost:hover { background: var(--bg-soft); }

 section.hero {
  position: relative; overflow: hidden;
  padding: 5.5rem 0 4rem;
 }
 .hero::before {
  content: "";
  position: absolute; inset: -20% 30% 40% -20%;
  background: radial-gradient(closest-side, rgba(37,211,102,0.22), transparent 70%);
  filter: blur(20px); z-index: 0;
 }
 .hero-grid { position: relative; z-index: 1; display: grid; grid-template-columns: 1.1fr .9fr; gap: 3rem; align-items: center; }
 .eyebrow {
  display: inline-flex; align-items: center; gap: .5rem;
  padding: .35rem .75rem; border-radius: 999px;
  background: rgba(37,211,102,0.1); color: #7be4a8;
  border: 1px solid rgba(37,211,102,0.25);
  font-size: .8rem; font-weight: 600; letter-spacing: .02em;
 }
 .eyebrow .dot { width: 6px; height: 6px; border-radius: 50%; background: var(--accent); box-shadow: 0 0 0 4px rgba(37,211,102,0.2); }
 h1.hero-title {
  font-size: clamp(2.2rem, 4.5vw, 3.4rem);
  line-height: 1.1; letter-spacing: -0.02em;
  margin: 1rem 0 1rem;
 }
 .hero-title .grad {
  background: linear-gradient(135deg, #7be4a8, var(--accent));
  -webkit-background-clip: text; background-clip: text; color: transparent;
 }
 .hero-sub { color: var(--muted); font-size: 1.1rem; max-width: 30rem; }
 .hero-cta { display: flex; gap: .75rem; margin-top: 1.75rem; flex-wrap: wrap; }
 .hero-meta { margin-top: 1.25rem; color: var(--muted); font-size: .85rem; display: flex; gap: 1.2rem; flex-wrap: wrap; }
 .hero-meta span::before { content: "✓ "; color: var(--accent); }

 /* Phone mockup */
 .phone {
  position: relative; justify-self: center;
  width: 320px; max-width: 100%;
  background: #0c1117; border: 1px solid var(--border);
  border-radius: 36px; padding: 16px;
  box-shadow: 0 30px 80px rgba(0,0,0,0.5), 0 0 0 1px rgba(255,255,255,0.03) inset;
 }
 .phone-screen {
  background: #0e1620;
  border-radius: 22px; overflow: hidden;
  height: 480px; display: flex; flex-direction: column;
 }
 .phone-header {
  display: flex; align-items: center; gap: .6rem;
  padding: .8rem 1rem; background: #122131; border-bottom: 1px solid var(--border);
 }
 .avatar {
  width: 34px; height: 34px; border-radius: 50%;
  background: linear-gradient(135deg, var(--accent), var(--accent-2));
  display: grid; place-items: center; font-weight: 700; color: #04130b; font-size: .9rem;
 }
 .phone-header .name { font-weight: 600; font-size: .9rem; }
 .phone-header .sub  { color: var(--muted); font-size: .72rem; }
 .chat { flex: 1; padding: 1rem; display: flex; flex-direction: column; gap: .5rem; overflow: hidden; background: #0e1620; }
 .bubble {
  max-width: 80%; padding: .55rem .75rem; border-radius: 14px;
  font-size: .85rem; line-height: 1.35;
  opacity: 0; transform: translateY(6px);
  animation: pop .5s ease forwards;
 }
 .bubble.in  { background: #1b2735; color: var(--text); border-bottom-left-radius: 4px; align-self: flex-start; }
 .bubble.out { background: #1f6f4a; color: #f0fff5; border-bottom-right-radius: 4px; align-self: flex-end; }
 .bubble:nth-child(1) { animation-delay: .15s; }
 .bubble:nth-child(2) { animation-delay: .9s; }
 .bubble:nth-child(3) { animation-delay: 1.6s; }
 .bubble:nth-child(4) { animation-delay: 2.5s; }
 @keyframes pop { to { opacity: 1; transform: translateY(0); } }

 /* Sections */
 section.block { padding: 4.5rem 0; border-top: 1px solid var(--border); }
 .section-head { text-align: center; max-width: 38rem; margin: 0 auto 2.5rem; }
 .section-head h2 {
  font-size: clamp(1.7rem, 3vw, 2.3rem); letter-spacing: -0.01em; margin: .5rem 0 .75rem;
 }
 .section-head p { color: var(--muted); margin: 0; }

 .features { display: grid; grid-template-columns: repeat(3, 1fr); gap: 1.25rem; }
 .feature {
  background: var(--panel); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 1.5rem;
  transition: transform .2s ease, border-color .2s ease;
 }
 .feature:hover { transform: translateY(-2px); border-color: rgba(37,211,102,0.4); }
 .feature .icon {
  width: 40px; height: 40px; border-radius: 10px;
  background: rgba(37,211,102,0.12); color: var(--accent);
  display: grid; place-items: center; margin-bottom: .9rem;
  font-size: 1.3rem;
 }
 .feature h3 { margin: 0 0 .35rem; font-size: 1.05rem; }
 .feature p  { margin: 0; color: var(--muted); font-size: .92rem; }

 .steps { display: grid; grid-template-columns: repeat(3, 1fr); gap: 1.25rem; counter-reset: step; }
 .step {
  background: linear-gradient(180deg, var(--panel), var(--bg-soft));
  border: 1px solid var(--border); border-radius: var(--radius);
  padding: 1.5rem; position: relative;
 }
 .step::before {
  counter-increment: step; content: counter(step);
  position: absolute; top: -14px; left: 1.25rem;
  width: 28px; height: 28px; border-radius: 50%;
  background: var(--accent); color: #04130b;
  display: grid; place-items: center; font-weight: 800; font-size: .85rem;
 }
 .step h3 { margin: .25rem 0 .35rem; font-size: 1.05rem; }
 .step p { margin: 0; color: var(--muted); font-size: .92rem; }

 .cta {
  background: linear-gradient(135deg, rgba(37,211,102,0.12), rgba(18,140,126,0.08));
  border: 1px solid rgba(37,211,102,0.25);
  border-radius: var(--radius);
  padding: 2.5rem; text-align: center;
 }
 .cta h2 { margin: 0 0 .5rem; font-size: 1.75rem; letter-spacing: -0.01em; }
 .cta p { color: var(--muted); margin: 0 0 1.5rem; }

 footer { padding: 2rem 0 3rem; color: var(--muted); font-size: .85rem; text-align: center; border-top: 1px solid var(--border); margin-top: 2rem; }

 @media (max-width: 820px) {
  .hero-grid { grid-template-columns: 1fr; }
  .features, .steps { grid-template-columns: 1fr; }
  .phone { width: 280px; }
  .nav-links a:not(.btn) { display: none; }
 }
</style>
</head>
<body>

<header class="nav">
 <div class="container nav-inner">
  <div class="logo">
   <div class="logo-mark">C</div>
   <span>Chalagente</span>
  </div>
  <nav class="nav-links">
   <a href="#features">Características</a>
   <a href="#how">Cómo funciona</a>
   <a href="#contact" class="btn btn-ghost">Solicitar acceso</a>
  </nav>
 </div>
</header>

<section class="hero">
 <div class="container hero-grid">
  <div>
   <span class="eyebrow"><span class="dot"></span> IA conversacional para WhatsApp</span>
   <h1 class="hero-title">Responde a tus clientes <span class="grad">al instante</span>, sin levantar el teléfono.</h1>
   <p class="hero-sub">Chalagente es un agente de IA que atiende las preguntas frecuentes de tu negocio por WhatsApp — el canal donde tus clientes ya están — las 24 horas del día.</p>
   <div class="hero-cta">
    <a href="#contact" class="btn btn-primary">Empezar ahora →</a>
    <a href="#how" class="btn btn-ghost">Ver cómo funciona</a>
   </div>
   <div class="hero-meta">
    <span>Sin apps nuevas</span>
    <span>Activación en minutos</span>
    <span>Habla como tú</span>
   </div>
  </div>

  <div class="phone" aria-hidden="true">
   <div class="phone-screen">
    <div class="phone-header">
     <div class="avatar">C</div>
     <div>
      <div class="name">Tu Negocio</div>
      <div class="sub">en línea</div>
     </div>
    </div>
    <div class="chat">
     <div class="bubble in">Hola, ¿a qué hora abren mañana?</div>
     <div class="bubble out">¡Hola! Abrimos de 9:00 a 20:00 todos los días 🙌</div>
     <div class="bubble in">¿Aceptan tarjeta?</div>
     <div class="bubble out">Sí, aceptamos todas las tarjetas y también transferencia.</div>
    </div>
   </div>
  </div>
 </div>
</section>

<section id="features" class="block">
 <div class="container">
  <div class="section-head">
   <h2>Todo lo que necesitas para automatizar la atención</h2>
   <p>Pensado para pequeños y medianos negocios que viven en WhatsApp.</p>
  </div>
  <div class="features">
   <div class="feature">
    <div class="icon">💬</div>
    <h3>WhatsApp nativo</h3>
    <p>Tus clientes escriben al mismo número de siempre. Sin descargas, sin formularios, sin fricción.</p>
   </div>
   <div class="feature">
    <div class="icon">🤖</div>
    <h3>Agente de IA</h3>
    <p>Entrenado con la información de tu negocio: horarios, precios, ubicación, productos y políticas.</p>
   </div>
   <div class="feature">
    <div class="icon">⚡</div>
    <h3>Respuestas 24/7</h3>
    <p>Atiende mientras duermes, en vacaciones o cuando estás con un cliente. Nunca pierdes una conversación.</p>
   </div>
  </div>
 </div>
</section>

<section id="how" class="block">
 <div class="container">
  <div class="section-head">
   <h2>Activo en tres pasos</h2>
   <p>Desde el primer mensaje, en menos de una tarde.</p>
  </div>
  <div class="steps">
   <div class="step">
    <h3>Conecta tu WhatsApp</h3>
    <p>Vincula tu número escaneando un QR, igual que WhatsApp Web. Tu cuenta sigue siendo tuya.</p>
   </div>
   <div class="step">
    <h3>Entrena al agente</h3>
    <p>Cuéntale al agente sobre tu negocio. Nosotros nos encargamos de que aprenda a sonar como tú.</p>
   </div>
   <div class="step">
    <h3>Empieza a responder</h3>
    <p>El agente atiende las preguntas frecuentes. Tú solo intervienes cuando hace falta.</p>
   </div>
  </div>
 </div>
</section>

<section id="contact" class="block">
 <div class="container">
  <div class="cta">
   <h2>¿Listo para no perder otro mensaje?</h2>
   <p>Estamos incorporando los primeros negocios. Escríbenos y te ayudamos a montarlo.</p>
   <a href="mailto:hola@chalagente.com" class="btn btn-primary">Solicitar acceso</a>
  </div>
 </div>
</section>

<footer>
 <div class="container">© Chalagente · Atención al cliente con IA por WhatsApp</div>
</footer>

</body>
</html>`))

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Chalagente — Admin</title>
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

<section>
 <h2>Session</h2>
 <p>Hardcoded responses cycle through {{ .ResponseCount }} messages per chat. Reset to start over.</p>
 <form method="POST" action="/admin/reset-session">
  <button>Reset session</button>
 </form>
</section>

{{ if not .LoggedIn }}
<section>
 <h2>Pair</h2>
 <p>Open WhatsApp → Settings → Linked Devices → Link a Device, and scan:</p>
 {{ if .HasQR }}<img class="qr" src="/admin/qr.png?ts={{ .Now }}" alt="QR">{{ else }}<p><em>Waiting for QR code…</em></p>{{ end }}
 <p><small>QR rotates every ~20 seconds. This page auto-refreshes.</small></p>
 <script>setTimeout(() => location.reload(), 5000);</script>
</section>
{{ else }}
<section>
 <h2>Send a message</h2>
 <form method="POST" action="/admin/send">
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
  const es = new EventSource('/admin/events');
  es.onmessage = (e) => { try { fmt(JSON.parse(e.data)); } catch {} };
 </script>
</section>
</body>
</html>`))

type pageData struct {
	LoggedIn      bool
	Connected     bool
	JID           string
	HasQR         bool
	Now           int64
	Flash         string
	FlashErr      bool
	ResponseCount int
}

func (a *App) serveHTTP(addr string) {
	user := os.Getenv("BASIC_AUTH_USER")
	pass := os.Getenv("BASIC_AUTH_PASS")
	if user == "" || pass == "" {
		log.Fatal("BASIC_AUTH_USER and BASIC_AUTH_PASS must be set")
	}

	admin := http.NewServeMux()
	admin.HandleFunc("/admin", a.handleAdmin)
	admin.HandleFunc("/admin/", a.handleAdmin)
	admin.HandleFunc("/admin/qr.png", a.handleQR)
	admin.HandleFunc("/admin/send", a.handleSend)
	admin.HandleFunc("/admin/events", a.handleEvents)
	admin.HandleFunc("/admin/reset-session", a.handleResetSession)

	protected := basicAuth(admin, user, pass)

	root := http.NewServeMux()
	root.HandleFunc("/healthz", a.handleHealth)
	root.HandleFunc("/", a.handleLanding)
	root.Handle("/admin", protected)
	root.Handle("/admin/", protected)

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

func (a *App) handleLanding(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := landingTmpl.Execute(w, nil); err != nil {
		log.Printf("landing template: %v", err)
	}
}

func (a *App) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin" && r.URL.Path != "/admin/" {
		http.NotFound(w, r)
		return
	}
	qrCode, _ := a.qr()
	data := pageData{
		LoggedIn:      a.client.IsLoggedIn(),
		Connected:     a.client.IsConnected(),
		HasQR:         qrCode != "",
		Now:           time.Now().UnixNano(),
		Flash:         r.URL.Query().Get("flash"),
		FlashErr:      r.URL.Query().Get("err") == "1",
		ResponseCount: len(hardcodedResponses),
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
	http.Redirect(w, r, "/admin?"+q.Encode(), http.StatusSeeOther)
}

func (a *App) handleResetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.resetSessions()
	redirectFlash(w, r, "session reset", false)
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
