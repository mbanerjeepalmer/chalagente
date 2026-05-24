package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"
)

func (a *App) Mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleLanding)
	mux.HandleFunc("/healthz", a.handleHealth)

	mux.HandleFunc("GET /signup", a.Auth.SignupForm)
	mux.HandleFunc("POST /signup", a.Auth.SignupSubmit)
	mux.HandleFunc("/auth/verify", a.Auth.Verify)
	mux.HandleFunc("POST /logout", a.Auth.Logout)

	protected := http.NewServeMux()
	protected.HandleFunc("/onboarding", a.handleOnboarding)
	protected.HandleFunc("/onboarding/business", a.handleOnboardingBusiness)
	protected.HandleFunc("/onboarding/extra", a.handleOnboardingExtra)
	protected.HandleFunc("/onboarding/whatsapp", a.handleOnboardingWhatsApp)
	protected.HandleFunc("/onboarding/whatsapp/start", a.handleOnboardingWhatsAppStart)
	protected.HandleFunc("/onboarding/whatsapp/qr.png", a.handleOnboardingQRPNG)
	protected.HandleFunc("/onboarding/whatsapp/status", a.handleOnboardingPairStatus)
	protected.HandleFunc("/onboarding/test", a.handleOnboardingTest)
	protected.HandleFunc("/onboarding/finish", a.handleOnboardingFinish)

	protected.HandleFunc("/app", a.handleDashboard)
	protected.HandleFunc("/app/agent", a.handleDashboardAgentToggle)
	protected.HandleFunc("/app/business", a.handleDashboardBusiness)
	protected.HandleFunc("/app/events", a.handleDashboardEvents)
	protected.HandleFunc("/app/qr.png", a.handleDashboardShareQR)
	protected.HandleFunc("/app/demo", a.handleDemoPage)
	protected.HandleFunc("/app/demo/send", a.handleDemoSend)
	protected.HandleFunc("/app/demo/history", a.handleDemoHistory)
	protected.HandleFunc("/app/demo/reset", a.handleDemoReset)

	mux.Handle("/onboarding", a.Auth.Middleware(protected))
	mux.Handle("/onboarding/", a.Auth.Middleware(protected))
	mux.Handle("/app", a.Auth.Middleware(protected))
	mux.Handle("/app/", a.Auth.Middleware(protected))

	return mux
}

func (a *App) serveHTTP(addr string) {
	if err := http.ListenAndServe(addr, a.Mux()); err != nil {
		log.Printf("http server: %v", err)
	}
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

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	if err := a.Store.DB().Ping(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("db not ready"))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func writeSSE(w http.ResponseWriter, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
}

func nowMillis() int64 { return time.Now().UnixNano() }

var landingTmpl = template.Must(template.New("landing").Parse(`<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Chalagente — Atención al cliente por WhatsApp con IA</title>
<meta name="description" content="Un agente de IA que responde las preguntas de tus clientes en WhatsApp, 24/7.">
<style>
 :root { --bg:#0b0f14; --bg-soft:#11161d; --panel:#141b24; --border:rgba(255,255,255,0.08); --text:#e7edf3; --muted:#8a96a6; --accent:#25d366; --accent-2:#128c7e; --radius:14px; }
 * { box-sizing: border-box; }
 html, body { margin: 0; padding: 0; }
 body { font-family: -apple-system, BlinkMacSystemFont, "Inter", "Segoe UI", Roboto, sans-serif; background: var(--bg); color: var(--text); line-height: 1.6; -webkit-font-smoothing: antialiased; }
 a { color: inherit; text-decoration: none; }
 .container { max-width: 1120px; margin: 0 auto; padding: 0 1.5rem; }
 header.nav { position: sticky; top: 0; z-index: 10; backdrop-filter: blur(12px); background: rgba(11,15,20,0.7); border-bottom: 1px solid var(--border); }
 .nav-inner { display: flex; align-items: center; justify-content: space-between; padding: 1rem 0; }
 .logo { display: flex; align-items: center; gap: .6rem; font-weight: 700; letter-spacing: -0.01em; }
 .logo-mark { width: 28px; height: 28px; border-radius: 8px; background: linear-gradient(135deg, var(--accent), var(--accent-2)); display: grid; place-items: center; color: #04130b; font-weight: 800; }
 .nav-links { display: flex; gap: 1.5rem; align-items: center; color: var(--muted); font-size: .92rem; }
 .nav-links a:hover { color: var(--text); }
 .btn { display: inline-flex; align-items: center; gap: .5rem; padding: .7rem 1.1rem; border-radius: 999px; font-weight: 600; transition: transform .12s ease, box-shadow .2s ease; font-size: .95rem; }
 .btn-primary { background: var(--accent); color: #04130b; box-shadow: 0 8px 24px rgba(37,211,102,0.25); }
 .btn-primary:hover { transform: translateY(-1px); box-shadow: 0 10px 28px rgba(37,211,102,0.35); }
 .btn-ghost { border: 1px solid var(--border); color: var(--text); }
 .btn-ghost:hover { background: var(--bg-soft); }
 section.hero { position: relative; overflow: hidden; padding: 5.5rem 0 4rem; }
 .hero::before { content: ""; position: absolute; inset: -20% 30% 40% -20%; background: radial-gradient(closest-side, rgba(37,211,102,0.22), transparent 70%); filter: blur(20px); z-index: 0; }
 .hero-grid { position: relative; z-index: 1; display: grid; grid-template-columns: 1.1fr .9fr; gap: 3rem; align-items: center; }
 .eyebrow { display: inline-flex; align-items: center; gap: .5rem; padding: .35rem .75rem; border-radius: 999px; background: rgba(37,211,102,0.1); color: #7be4a8; border: 1px solid rgba(37,211,102,0.25); font-size: .8rem; font-weight: 600; }
 .eyebrow .dot { width: 6px; height: 6px; border-radius: 50%; background: var(--accent); box-shadow: 0 0 0 4px rgba(37,211,102,0.2); }
 h1.hero-title { font-size: clamp(2.2rem, 4.5vw, 3.4rem); line-height: 1.1; letter-spacing: -0.02em; margin: 1rem 0 1rem; }
 .hero-title .grad { background: linear-gradient(135deg, #7be4a8, var(--accent)); -webkit-background-clip: text; background-clip: text; color: transparent; }
 .hero-sub { color: var(--muted); font-size: 1.1rem; max-width: 30rem; }
 .hero-cta { display: flex; gap: .75rem; margin-top: 1.75rem; flex-wrap: wrap; }
 .phone { position: relative; justify-self: center; width: 320px; max-width: 100%; background: #0c1117; border: 1px solid var(--border); border-radius: 36px; padding: 16px; box-shadow: 0 30px 80px rgba(0,0,0,0.5); }
 .phone-screen { background: #0e1620; border-radius: 22px; overflow: hidden; height: 480px; display: flex; flex-direction: column; }
 .phone-header { display: flex; align-items: center; gap: .6rem; padding: .8rem 1rem; background: #122131; border-bottom: 1px solid var(--border); }
 .avatar { width: 34px; height: 34px; border-radius: 50%; background: linear-gradient(135deg, var(--accent), var(--accent-2)); display: grid; place-items: center; font-weight: 700; color: #04130b; font-size: .9rem; }
 .phone-header .name { font-weight: 600; font-size: .9rem; }
 .phone-header .sub  { color: var(--muted); font-size: .72rem; }
 .chat { flex: 1; padding: 1rem; display: flex; flex-direction: column; gap: .5rem; overflow: hidden; background: #0e1620; }
 .bubble { max-width: 80%; padding: .55rem .75rem; border-radius: 14px; font-size: .85rem; line-height: 1.35; }
 .bubble.in { background: #1b2735; color: var(--text); border-bottom-left-radius: 4px; align-self: flex-start; }
 .bubble.out { background: #1f6f4a; color: #f0fff5; border-bottom-right-radius: 4px; align-self: flex-end; }
 section.block { padding: 4.5rem 0; border-top: 1px solid var(--border); }
 .section-head { text-align: center; max-width: 38rem; margin: 0 auto 2.5rem; }
 .section-head h2 { font-size: clamp(1.7rem, 3vw, 2.3rem); letter-spacing: -0.01em; margin: .5rem 0 .75rem; }
 .section-head p { color: var(--muted); margin: 0; }
 .features { display: grid; grid-template-columns: repeat(3, 1fr); gap: 1.25rem; }
 .feature { background: var(--panel); border: 1px solid var(--border); border-radius: var(--radius); padding: 1.5rem; }
 .feature .icon { width: 40px; height: 40px; border-radius: 10px; background: rgba(37,211,102,0.12); color: var(--accent); display: grid; place-items: center; margin-bottom: .9rem; font-size: 1.3rem; }
 .feature h3 { margin: 0 0 .35rem; font-size: 1.05rem; }
 .feature p  { margin: 0; color: var(--muted); font-size: .92rem; }
 .cta { background: linear-gradient(135deg, rgba(37,211,102,0.12), rgba(18,140,126,0.08)); border: 1px solid rgba(37,211,102,0.25); border-radius: var(--radius); padding: 2.5rem; text-align: center; }
 .cta h2 { margin: 0 0 .5rem; font-size: 1.75rem; letter-spacing: -0.01em; }
 .cta p { color: var(--muted); margin: 0 0 1.5rem; }
 footer { padding: 2rem 0 3rem; color: var(--muted); font-size: .85rem; text-align: center; border-top: 1px solid var(--border); margin-top: 2rem; }
 @media (max-width: 820px) { .hero-grid { grid-template-columns: 1fr; } .features { grid-template-columns: 1fr; } .phone { width: 280px; } .nav-links a:not(.btn) { display: none; } }
</style>
</head>
<body>
<header class="nav">
 <div class="container nav-inner">
  <div class="logo"><div class="logo-mark">C</div><span>Chalagente</span></div>
  <nav class="nav-links">
   <a href="#features">Características</a>
   <a href="#how">Cómo funciona</a>
   <a href="/signup" class="btn btn-primary">Empezar</a>
  </nav>
 </div>
</header>
<section class="hero">
 <div class="container hero-grid">
  <div>
   <span class="eyebrow"><span class="dot"></span> IA conversacional para WhatsApp</span>
   <h1 class="hero-title">Responde a tus clientes <span class="grad">al instante</span>, sin levantar el teléfono.</h1>
   <p class="hero-sub">Chalagente es un agente de IA que atiende las preguntas de tu negocio por WhatsApp — el canal donde tus clientes ya están — las 24 horas del día.</p>
   <div class="hero-cta">
    <a href="/signup" class="btn btn-primary">Crear cuenta →</a>
    <a href="#how" class="btn btn-ghost">Ver cómo funciona</a>
   </div>
  </div>
  <div class="phone" aria-hidden="true">
   <div class="phone-screen">
    <div class="phone-header">
     <div class="avatar">C</div>
     <div><div class="name">Tu Negocio</div><div class="sub">en línea</div></div>
    </div>
    <div class="chat">
     <div class="bubble in">Hola, ¿a qué hora abren mañana?</div>
     <div class="bubble out">¡Hola! Abrimos de 9:00 a 20:00 todos los días.</div>
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
   <div class="feature"><div class="icon">💬</div><h3>WhatsApp nativo</h3><p>Tus clientes escriben al mismo número de siempre. Sin descargas, sin formularios.</p></div>
   <div class="feature"><div class="icon">🤖</div><h3>Agente de IA</h3><p>Entrenado con la información de tu negocio: horarios, precios, ubicación, productos.</p></div>
   <div class="feature"><div class="icon">⚡</div><h3>Respuestas 24/7</h3><p>Atiende mientras duermes, en vacaciones o cuando estás con un cliente.</p></div>
  </div>
 </div>
</section>
<section id="how" class="block">
 <div class="container">
  <div class="cta">
   <h2>¿Listo para no perder otro mensaje?</h2>
   <p>Crea tu cuenta y activa tu agente en minutos.</p>
   <a href="/signup" class="btn btn-primary">Empezar gratis</a>
  </div>
 </div>
</section>
<footer><div class="container">© Chalagente · Atención al cliente con IA por WhatsApp</div></footer>
</body>
</html>`))
