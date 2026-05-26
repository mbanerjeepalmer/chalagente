package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mbanerjeepalmer/chalagente/internal/layout"
)

func (a *App) Mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleLanding)
	mux.HandleFunc("/healthz", a.handleHealth)
	mux.HandleFunc("/privacidad", a.handlePrivacy)
	mux.HandleFunc("/terminos", a.handleTerms)

	mux.HandleFunc("GET /sign-in", a.ClerkAuth.SignInPage)
	mux.HandleFunc("GET /sign-up", a.ClerkAuth.SignUpPage)
	mux.HandleFunc("POST /logout", a.ClerkAuth.Logout)
	// Legacy /signup → /sign-up so any old links keep working.
	mux.HandleFunc("GET /signup", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/sign-up", http.StatusSeeOther)
	})

	// /go/{id} is the public customer-facing redirect — a wa.me wrapper
	// that prefills the customer's first message. Kept on the unauthed
	// mux because the whole point is anyone with the link can click it.
	mux.HandleFunc("GET /go/{id}", a.handleShareRedirect)

	mux.HandleFunc("/demo", a.handleTryPage)
	mux.HandleFunc("/demo/business", a.handleTryBusiness)
	mux.HandleFunc("/demo/send", a.handleTrySend)
	mux.HandleFunc("/demo/history", a.handleTryHistory)
	mux.HandleFunc("/demo/reset", a.handleTryReset)

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

	// Canonical admin routes — /admin/* is the source of truth.
	protected.HandleFunc("/admin", a.handleDashboard)
	protected.HandleFunc("GET /admin/conversations/{id}", a.handleDashboardConversation)
	protected.HandleFunc("/admin/agent", a.handleDashboardAgentToggle)
	protected.HandleFunc("POST /admin/trigger", a.handleDashboardTriggerToggle)
	protected.HandleFunc("/admin/business", a.handleDashboardBusiness)
	protected.HandleFunc("/admin/events", a.handleDashboardEvents)
	protected.HandleFunc("/admin/qr.png", a.handleDashboardShareQR)
	protected.HandleFunc("POST /admin/whatsapp/unpair", a.handleDashboardUnpair)

	// Legacy /app/* paths 308 → /admin/*. 308 preserves method+body so
	// any old bookmarks, form posts, or QR codes targeting the previous
	// namespace keep working without losing data.
	mux.HandleFunc("/app", redirectToAdmin)
	mux.HandleFunc("/app/", redirectToAdmin)

	mux.Handle("/onboarding", a.authMiddleware(protected))
	mux.Handle("/onboarding/", a.authMiddleware(protected))
	mux.Handle("/admin", a.authMiddleware(protected))
	mux.Handle("/admin/", a.authMiddleware(protected))

	return mux
}

func (a *App) serveHTTP(addr string) {
	if err := http.ListenAndServe(addr, a.Mux()); err != nil {
		log.Printf("http server: %v", err)
	}
}

// redirectToAdmin permanently forwards /app/<rest> to /admin/<rest>, query
// string included. Uses 308 (permanent redirect, preserves method + body)
// so POSTs to legacy /app/* keep their form data on the way through.
func redirectToAdmin(w http.ResponseWriter, r *http.Request) {
	target := "/admin" + strings.TrimPrefix(r.URL.Path, "/app")
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusPermanentRedirect)
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

func (a *App) handlePrivacy(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = legalTmpl.Execute(w, legalPage{Title: "Aviso de privacidad", Body: privacyBody})
}

func (a *App) handleTerms(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = legalTmpl.Execute(w, legalPage{Title: "Términos del servicio", Body: termsBody})
}

func writeSSE(w http.ResponseWriter, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
}

func nowMillis() int64 { return time.Now().UnixNano() }

// faviconDataURI, githubURL and sharedStyles used to live here; the
// canonical copies are now in internal/layout. These aliases keep this
// file's templates readable without a tree of layout.* fully-qualified
// references inside the inline HTML strings.
const (
	faviconDataURI = layout.FaviconDataURI
	githubURL      = layout.GithubURL
)

// sharedStyles is a local alias for layout.SharedStyles so the inline
// landing/legal templates can keep concatenating it as a Go string.
const sharedStyles = layout.SharedStyles

var landingTmpl = template.Must(template.New("landing").Parse(`<!doctype html>
<html lang="es-MX">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Chalagente — Un agente IA que es tu chalán</title>
<meta name="description" content="Chalagente atiende a tus clientes en su idioma, por WhatsApp. Para puestos de comida, electricistas, agencias de viaje y más.">
<link rel="icon" href="` + faviconDataURI + `">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Cormorant+Garamond:wght@500;600;700&family=Inter:wght@400;500;600&display=swap" rel="stylesheet">
<style>` + sharedStyles + `
section.hero { padding: 4.5rem 0 3.5rem; position: relative; }
.hero-grid { display: grid; grid-template-columns: 1.15fr .85fr; gap: 3rem; align-items: center; }
h1.hero-title { font-size: clamp(2.4rem, 5vw, 3.8rem); margin: 0 0 1rem; color: var(--ink); }
h1.hero-title .accent { color: var(--terracotta-deep); font-style: italic; }
.hero-sub { font-size: 1.15rem; color: var(--ink-soft); max-width: 32rem; margin: 0 0 1.75rem; }
.hero-cta { display: flex; gap: .75rem; flex-wrap: wrap; }
.wa-mock {
  background: #ece5dd;
  border: 1px solid var(--line);
  border-radius: 10px;
  overflow: hidden;
  box-shadow: 0 1px 0 rgba(0,0,0,0.06), 0 12px 32px rgba(40,30,15,0.14);
  transform: rotate(-0.6deg);
  position: relative;
}
.wa-mock::after {
  content: ""; position: absolute; inset: 0; pointer-events: none;
  background: linear-gradient(180deg, transparent 78%, rgba(241,234,217,0.95));
}
.wa-mock-head {
  display: flex; align-items: center; gap: .55rem;
  padding: .65rem .85rem;
  background: #075e54; color: white;
}
.wa-mock-avatar {
  width: 32px; height: 32px; border-radius: 50%;
  background: #25d366; color: #04130b;
  display: grid; place-items: center;
  font-family: "Cormorant Garamond", serif; font-weight: 700;
}
.wa-mock-meta { flex: 1; line-height: 1.2; }
.wa-mock-name { font-weight: 600; font-size: .95rem; }
.wa-mock-sub { font-size: .72rem; opacity: .85; }
.wa-langs { display: flex; gap: .25rem; }
.wa-lang {
  background: rgba(255,255,255,0.12);
  border: 1px solid rgba(255,255,255,0.25);
  color: white;
  padding: .2rem .35rem;
  border-radius: 4px;
  cursor: pointer;
  font-size: .95rem;
  line-height: 1;
}
.wa-lang.active { background: rgba(255,255,255,0.28); border-color: white; }
.flagstack { letter-spacing: -0.35em; padding-right: .35em; }
.wa-mock-body {
  background: #ece5dd;
  background-image:
    radial-gradient(rgba(80,60,40,0.05) 1px, transparent 1px);
  background-size: 4px 4px;
  padding: 1rem .9rem 1.6rem;
  display: flex; flex-direction: column; gap: .4rem;
  min-height: 230px;
}
.wa-bubble {
  max-width: 82%;
  padding: .5rem .7rem;
  font-size: .92rem; color: #1c1a16;
  border-radius: 10px;
  box-shadow: 0 1px 1px rgba(0,0,0,.08);
  line-height: 1.4;
}
.wa-bubble.in { background: #dcf8c6; align-self: flex-end; border-bottom-right-radius: 2px; }
.wa-bubble.out { background: white; align-self: flex-start; border-bottom-left-radius: 2px; }
.wa-mock-foot {
  padding: .45rem .9rem .65rem; font-size: .72rem; color: var(--muted);
  background: var(--bone); border-top: 1px solid var(--line); text-align: center;
}
section.block { padding: 4rem 0; border-top: 1px solid var(--line); }
.section-head { max-width: 38rem; margin: 0 auto 2.5rem; text-align: center; }
.section-head h2 { font-size: clamp(2rem, 3.5vw, 2.6rem); margin: 0 0 .5rem; }
.section-head p { color: var(--muted); margin: 0; }
.threecol { display: grid; grid-template-columns: repeat(3, 1fr); gap: 1.25rem; }
.tile {
  background: var(--bone);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  padding: 1.5rem 1.4rem;
}
.tile .swatch { width: 36px; height: 36px; border-radius: 50%; margin-bottom: .9rem; box-shadow: inset 0 -2px 0 rgba(0,0,0,0.12); }
.tile h3 { margin: 0 0 .4rem; font-size: 1.25rem; }
.tile p { margin: 0; color: var(--ink-soft); font-size: .95rem; }
.steps { counter-reset: step; display: grid; grid-template-columns: repeat(3, 1fr); gap: 1.25rem; }
.step { background: var(--plaster); border-radius: var(--radius); padding: 1.5rem; border: 1px solid var(--line); }
.step .num { font-family: "Cormorant Garamond", serif; font-size: 2.4rem; color: var(--terracotta); display: block; line-height: 1; margin-bottom: .4rem; }
.step h3 { margin: 0 0 .35rem; font-size: 1.2rem; }
.step p { margin: 0; color: var(--ink-soft); }
.warn {
  background: rgba(181,72,46,0.08);
  border-left: 4px solid var(--terracotta);
  padding: 1rem 1.2rem;
  border-radius: var(--radius);
  margin-top: 1.25rem;
  font-size: .95rem;
  color: var(--ink);
}
.cta-block { background: var(--bone); border: 1px solid var(--line); border-radius: var(--radius); padding: 2.5rem; text-align: center; }
.cta-block h2 { margin: 0 0 .5rem; font-size: 2rem; }
.cta-block p { color: var(--muted); margin: 0 0 1.5rem; }
@media (max-width: 820px) {
  .hero-grid { grid-template-columns: 1fr; }
  .threecol, .steps { grid-template-columns: 1fr; }
}
</style></head>
<body>
<header class="nav">
  <div class="container nav-inner">
    <a class="logo" href="/"><span class="logo-mark">C</span><span>Chalagente</span></a>
    <nav class="nav-links">
      <a href="#para-quien">Para quién</a>
      <a href="#como">Cómo funciona</a>
      <a href="/demo">Demo</a>
      <a href="/sign-in">Iniciar sesión</a>
      <a href="/demo" class="btn btn-primary">Probar demo</a>
    </nav>
  </div>
</header>

<section class="hero">
  <div class="container hero-grid">
    <div>
      <h1 class="hero-title">Un agente IA<br><span class="accent">que es tu chalán</span></h1>
      <p class="hero-sub">Chalagente atiende a tus clientes <strong>en su idioma</strong>, por WhatsApp. Tú haces lo tuyo; él contesta.</p>
      <div class="hero-cta">
        <a href="/demo" class="btn btn-primary">Probar demo →</a>
        <a href="/sign-in" class="btn btn-ghost">Iniciar sesión</a>
      </div>
    </div>
    <aside class="wa-mock" aria-label="Vista previa de conversación">
      <div class="wa-mock-head">
        <div class="wa-mock-avatar">B</div>
        <div class="wa-mock-meta">
          <div class="wa-mock-name">Birrias El Chalán</div>
          <div class="wa-mock-sub">en línea</div>
        </div>
        <div class="wa-langs" role="tablist" aria-label="Idioma">
          <button type="button" class="wa-lang active" data-lang="en" role="tab" aria-selected="true" title="English">
            <span class="flagstack">🇬🇧🇺🇸🇨🇦</span>
          </button>
          <button type="button" class="wa-lang" data-lang="es" role="tab" aria-selected="false" title="Español">
            <span class="flagstack">🇲🇽🇨🇴🇪🇸</span>
          </button>
        </div>
      </div>
      <div class="wa-mock-body">
        <div class="wa-bubble in" data-lang="en">What is a birria taco? What makes yours special?</div>
        <div class="wa-bubble in" data-lang="es" hidden>¿Qué es un taco de birria? ¿Qué hace especial el de ustedes?</div>
        <div class="wa-bubble out" data-lang="en">
          <strong>Slow-cooked goat stew in a tortilla.</strong><br>
          Ours is marinated 24h in a secret guajillo-ancho rub — try it as a quesabirria with consomé on the side.
        </div>
        <div class="wa-bubble out" data-lang="es" hidden>
          <strong>Estofado de chivo en tortilla.</strong><br>
          La nuestra se marina 24h con un recado secreto de guajillo y ancho — pruébala en quesabirria con consomé aparte.
        </div>
      </div>
      <div class="wa-mock-foot">simulación · responde en el idioma del cliente</div>
    </aside>
  </div>
</section>

<section id="como" class="block">
  <div class="container">
    <div class="section-head">
      <h2>Cómo funciona</h2>
      <p>Tres pasos. Sin instalar nada. Sin código.</p>
    </div>
    <div class="steps">
      <div class="step"><span class="num">1</span><h3>Cuéntale de tu negocio</h3><p>Escribe — o dicta con tu voz — quién eres, qué vendes y cómo atiendes.</p></div>
      <div class="step"><span class="num">2</span><h3>Conecta tu WhatsApp</h3><p>Escanea el código QR desde la app, como un dispositivo más.</p></div>
      <div class="step"><span class="num">3</span><h3>Chalagente responde</h3><p>Cuando un cliente menciona «Chalagente» en su mensaje, el agente contesta con tu información. Una vez mencionado en la conversación, sigue respondiendo a los siguientes mensajes.</p></div>
    </div>
  </div>
</section>

<section id="para-quien" class="block">
  <div class="container">
    <div class="section-head">
      <h2>Chalagente ayuda a tus clientes a entender tu negocio</h2>
      <p>Hecho para los oficios donde WhatsApp es la puerta de entrada.</p>
    </div>
    <div class="threecol">
      <div class="tile">
        <div class="swatch" style="background:var(--terracotta)"></div>
        <h3>Puestos de comida</h3>
        <p>Explica los tacos a los gringos, sin perder al cliente que sí va a llegar.</p>
      </div>
      <div class="tile">
        <div class="swatch" style="background:var(--ochre)"></div>
        <h3>Electricistas</h3>
        <p>Haz la consulta inicial mientras estás en otra chamba.</p>
      </div>
      <div class="tile">
        <div class="swatch" style="background:var(--indigo)"></div>
        <h3>Agencias de viaje</h3>
        <p>Explica los paquetes, las fechas y los precios sin repetirte cien veces.</p>
      </div>
    </div>
  </div>
</section>

<section class="block">
  <div class="container">
    <div class="section-head">
      <h2>WhatsApp como siempre</h2>
      <p>Funciona con texto, notas de voz, imágenes y video.</p>
    </div>
    <div class="warn">
      <strong>Atención:</strong> Chalagente ve todos los mensajes de la cuenta que conectes. Conecta solo un número dedicado a tu negocio.
    </div>
  </div>
</section>

<section class="block">
  <div class="container">
    <div class="cta-block">
      <h2>Pruébalo ahora</h2>
      <p>Chatea con un agente prellenado en cinco segundos. Sin registro.</p>
      <a href="/demo" class="btn btn-primary">Abrir demo →</a>
    </div>
  </div>
</section>

` + layout.FooterMarketingHTML + `
<script>
(function(){
  const btns = document.querySelectorAll('.wa-lang');
  if (!btns.length) return;
  btns.forEach(b => b.addEventListener('click', () => {
    const lang = b.dataset.lang;
    btns.forEach(x => { x.classList.toggle('active', x === b); x.setAttribute('aria-selected', x === b); });
    document.querySelectorAll('.wa-bubble[data-lang]').forEach(el => {
      el.hidden = el.dataset.lang !== lang;
    });
  }));
})();
</script>
</body>
</html>`))

type legalPage struct {
	Title string
	Body  template.HTML
}

var privacyBody = template.HTML(`
<p>Chalagente conecta con tu cuenta de WhatsApp para leer los mensajes entrantes y responder en tu nombre. Guardamos:</p>
<ul>
  <li>Los datos de tu negocio que tú nos das.</li>
  <li>Los mensajes que pasan por la cuenta de WhatsApp que conectes, para responder y para que tú los puedas ver.</li>
  <li>Tu correo, para iniciar sesión.</li>
</ul>
<p>No vendemos tus datos. Conecta solo un número dedicado a tu negocio.</p>
<p>Para borrar tu cuenta y tus datos, escríbenos.</p>
`)

var termsBody = template.HTML(`
<p>Chalagente es un servicio en pruebas. Lo usas bajo tu propio riesgo.</p>
<p>No te garantizamos disponibilidad permanente ni que las respuestas del agente sean siempre correctas. Revisa los mensajes importantes.</p>
<p>No uses Chalagente para spam, fraude ni actividades ilegales. WhatsApp puede desconectar tu número si lo haces.</p>
`)

var legalTmpl = template.Must(template.New("legal").Parse(`<!doctype html>
<html lang="es-MX"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{ .Title }} — Chalagente</title>
<link rel="icon" href="` + faviconDataURI + `">
<link href="https://fonts.googleapis.com/css2?family=Cormorant+Garamond:wght@500;600;700&family=Inter:wght@400;500;600&display=swap" rel="stylesheet">
<style>` + sharedStyles + `
.legal { max-width: 680px; margin: 3rem auto; padding: 0 1.5rem; }
.legal h1 { font-size: clamp(2rem, 4vw, 2.8rem); margin-bottom: 1rem; }
.legal p, .legal li { color: var(--ink-soft); }
.legal ul { padding-left: 1.2rem; }
</style></head><body>
<header class="nav"><div class="container nav-inner">
  <a class="logo" href="/"><span class="logo-mark">C</span><span>Chalagente</span></a>
  <nav class="nav-links"><a href="/demo">Demo</a><a href="/sign-in">Iniciar sesión</a></nav>
</div></header>
<main class="legal">
  <h1>{{ .Title }}</h1>
  {{ .Body }}
</main>
` + layout.FooterLegalHTML + `
</body></html>`))
