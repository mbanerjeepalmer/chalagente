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
	mux.HandleFunc("/privacidad", a.handlePrivacy)
	mux.HandleFunc("/terminos", a.handleTerms)

	if a.ClerkAuth != nil {
		mux.HandleFunc("GET /sign-in", a.ClerkAuth.SignInPage)
		mux.HandleFunc("GET /sign-up", a.ClerkAuth.SignUpPage)
		mux.HandleFunc("POST /logout", a.ClerkAuth.Logout)
		mux.HandleFunc("GET /signup", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/sign-up", http.StatusSeeOther)
		})
	} else {
		mux.HandleFunc("GET /signup", a.Auth.SignupForm)
		mux.HandleFunc("POST /signup", a.Auth.SignupSubmit)
		mux.HandleFunc("/auth/verify", a.Auth.Verify)
		mux.HandleFunc("POST /logout", a.Auth.Logout)
	}

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

	protected.HandleFunc("/app", a.handleDashboard)
	protected.HandleFunc("/admin", a.handleDashboard)
	protected.HandleFunc("/app/agent", a.handleDashboardAgentToggle)
	protected.HandleFunc("POST /app/trigger", a.handleDashboardTriggerToggle)
	protected.HandleFunc("/app/business", a.handleDashboardBusiness)
	protected.HandleFunc("/app/events", a.handleDashboardEvents)
	protected.HandleFunc("/app/qr.png", a.handleDashboardShareQR)
	protected.HandleFunc("POST /app/whatsapp/unpair", a.handleDashboardUnpair)

	mux.Handle("/onboarding", a.authMiddleware(protected))
	mux.Handle("/onboarding/", a.authMiddleware(protected))
	mux.Handle("/app", a.authMiddleware(protected))
	mux.Handle("/app/", a.authMiddleware(protected))
	mux.Handle("/admin", a.authMiddleware(protected))
	mux.Handle("/admin/", a.authMiddleware(protected))

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

// faviconDataURI is a tiny inline SVG of the Chalagente "C" mark in
// terracotta on bone — embedded so we don't need to ship a separate asset.
const faviconDataURI = `data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 64 64'><rect width='64' height='64' rx='14' fill='%23b5482e'/><text x='50%25' y='54%25' text-anchor='middle' dominant-baseline='middle' font-family='Georgia,serif' font-size='42' font-weight='700' fill='%23faf6ea'>C</text></svg>`

// githubURL is the public source repo, linked in the footer.
const githubURL = "https://github.com/mbanerjeepalmer/chalagente"

// Diego Rivera "Fraternidad" palette: terracotta, ochre, indigo, deep green,
// warm bone-white wall. Serif headings (painted-on-wall feel), gallery sans body.
const sharedStyles = `
:root {
  --wall: #f1ead9;
  --wall-shade: #e6dec7;
  --plaster: #ece2cb;
  --ink: #1c1a16;
  --ink-soft: #3a352c;
  --muted: #6b6354;
  --line: rgba(28,26,22,0.14);
  --terracotta: #b5482e;
  --terracotta-deep: #8a3320;
  --ochre: #c8932b;
  --indigo: #25406e;
  --leaf: #4f6a3a;
  --bone: #faf6ea;
  --radius: 6px;
}
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; }
body {
  font-family: "Inter", "Helvetica Neue", Helvetica, Arial, sans-serif;
  background: var(--wall);
  color: var(--ink-soft);
  line-height: 1.6;
  -webkit-font-smoothing: antialiased;
  background-image:
    radial-gradient(rgba(110,90,60,0.05) 1px, transparent 1px),
    radial-gradient(rgba(80,60,40,0.04) 1px, transparent 1px),
    linear-gradient(180deg, var(--wall), var(--wall-shade));
  background-size: 3px 3px, 7px 7px, 100% 100%;
  background-position: 0 0, 1px 2px, 0 0;
}
h1, h2, h3, h4 {
  font-family: "Cormorant Garamond", "Playfair Display", Georgia, "Times New Roman", serif;
  color: var(--ink);
  font-weight: 600;
  letter-spacing: -0.005em;
  line-height: 1.15;
}
a { color: var(--terracotta-deep); }
.container { max-width: 1080px; margin: 0 auto; padding: 0 1.5rem; }
header.nav {
  position: sticky; top: 0; z-index: 10;
  background: rgba(241,234,217,0.92);
  backdrop-filter: blur(6px);
  border-bottom: 1px solid var(--line);
}
.nav-inner { display: flex; align-items: center; justify-content: space-between; padding: 1rem 0; }
.logo { display: flex; align-items: center; gap: .6rem; font-family: "Cormorant Garamond", serif; font-weight: 700; font-size: 1.35rem; color: var(--ink); text-decoration: none; letter-spacing: .01em; }
.logo-mark {
  width: 30px; height: 30px; border-radius: 50%;
  background: var(--terracotta);
  display: grid; place-items: center; color: var(--bone);
  font-family: "Cormorant Garamond", serif; font-weight: 700; font-size: 1rem;
  box-shadow: inset 0 -2px 0 rgba(0,0,0,0.15);
}
.nav-links { display: flex; gap: 1.4rem; align-items: center; font-size: .92rem; color: var(--ink-soft); }
.nav-links a { color: var(--ink-soft); text-decoration: none; }
.nav-links a:hover { color: var(--terracotta-deep); }
.btn {
  display: inline-flex; align-items: center; gap: .5rem;
  padding: .7rem 1.2rem; border-radius: var(--radius);
  font-weight: 600; font-size: .95rem; text-decoration: none;
  transition: transform .12s ease, box-shadow .15s ease;
  border: 1px solid transparent;
}
.btn-primary { background: var(--terracotta); color: var(--bone); box-shadow: 0 2px 0 var(--terracotta-deep); }
.btn-primary:hover { transform: translateY(-1px); }
.btn-ghost { background: transparent; color: var(--ink); border-color: var(--ink); }
.btn-ghost:hover { background: rgba(28,26,22,0.05); }
footer { padding: 2rem 0 2.5rem; color: var(--muted); font-size: .85rem; border-top: 1px solid var(--line); margin-top: 3rem; }
footer .container { display: flex; justify-content: space-between; flex-wrap: wrap; gap: 1rem; }
footer a { color: var(--muted); text-decoration: none; margin-right: 1rem; }
footer a:hover { color: var(--ink); }
footer .foot-links { display: inline-flex; align-items: center; flex-wrap: wrap; gap: .25rem 1rem; }
footer .foot-links a { margin-right: 0; }
@media (max-width: 720px) {
  .nav-links a:not(.btn) { display: none; }
}
`

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
      <a href="/signup">Iniciar sesión</a>
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
        <a href="/signup" class="btn btn-ghost">Iniciar sesión</a>
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

<footer>
  <div class="container">
    <span>© Chalagente · Hecho en México</span>
    <span class="foot-links">
      <a href="/privacidad">Aviso de privacidad</a>
      <a href="/terminos">Términos</a>
      <a href="/demo">Demo</a>
      <a href="/signup">Iniciar sesión</a>
      <a href="` + githubURL + `" rel="noopener" aria-label="GitHub" title="GitHub">
        <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" style="vertical-align:-3px"><path fill="currentColor" d="M12 .5C5.73.5.75 5.48.75 11.75c0 4.95 3.21 9.14 7.66 10.62.56.1.77-.24.77-.54v-1.9c-3.11.68-3.77-1.5-3.77-1.5-.51-1.3-1.25-1.64-1.25-1.64-1.02-.7.08-.68.08-.68 1.13.08 1.72 1.16 1.72 1.16 1 1.72 2.63 1.22 3.27.94.1-.73.39-1.22.7-1.5-2.48-.28-5.09-1.24-5.09-5.52 0-1.22.44-2.22 1.15-3-.12-.28-.5-1.42.11-2.96 0 0 .94-.3 3.08 1.15a10.7 10.7 0 0 1 5.6 0c2.14-1.45 3.08-1.15 3.08-1.15.62 1.54.23 2.68.11 2.96.72.78 1.15 1.78 1.15 3 0 4.29-2.61 5.23-5.1 5.51.4.34.76 1.02.76 2.06v3.05c0 .3.2.65.78.54 4.45-1.48 7.66-5.67 7.66-10.62C23.25 5.48 18.27.5 12 .5z"/></svg>
      </a>
    </span>
  </div>
</footer>
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
  <nav class="nav-links"><a href="/demo">Demo</a><a href="/signup">Iniciar sesión</a></nav>
</div></header>
<main class="legal">
  <h1>{{ .Title }}</h1>
  {{ .Body }}
</main>
<footer><div class="container">
  <span>© Chalagente</span>
  <span class="foot-links">
    <a href="/privacidad">Aviso de privacidad</a>
    <a href="/terminos">Términos</a>
    <a href="` + githubURL + `" rel="noopener" aria-label="GitHub" title="GitHub">
      <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" style="vertical-align:-3px"><path fill="currentColor" d="M12 .5C5.73.5.75 5.48.75 11.75c0 4.95 3.21 9.14 7.66 10.62.56.1.77-.24.77-.54v-1.9c-3.11.68-3.77-1.5-3.77-1.5-.51-1.3-1.25-1.64-1.25-1.64-1.02-.7.08-.68.08-.68 1.13.08 1.72 1.16 1.72 1.16 1 1.72 2.63 1.22 3.27.94.1-.73.39-1.22.7-1.5-2.48-.28-5.09-1.24-5.09-5.52 0-1.22.44-2.22 1.15-3-.12-.28-.5-1.42.11-2.96 0 0 .94-.3 3.08 1.15a10.7 10.7 0 0 1 5.6 0c2.14-1.45 3.08-1.15 3.08-1.15.62 1.54.23 2.68.11 2.96.72.78 1.15 1.78 1.15 3 0 4.29-2.61 5.23-5.1 5.51.4.34.76 1.02.76 2.06v3.05c0 .3.2.65.78.54 4.45-1.48 7.66-5.67 7.66-10.62C23.25 5.48 18.27.5 12 .5z"/></svg>
    </a>
  </span>
</div></footer>
</body></html>`))
