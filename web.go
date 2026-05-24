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
	"sort"
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
<title>Chalagente — Llena tu WhatsApp durante la Copa del Mundo 2026</title>
<meta name="description" content="Chalagente responde por WhatsApp con audios de voz en español mexicano, al instante. Pensado para restaurantes que no quieren perder mesas durante la Copa del Mundo 2026.">
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

 /* Story / narrative blocks */
 .lede {
  font-size: clamp(1.05rem, 1.4vw, 1.2rem);
  color: #c8d2de; max-width: 38rem; margin: 0 auto 2rem; text-align: center;
 }
 .lede strong { color: var(--text); }
 .story p { max-width: 38rem; margin: 0 auto 1.1rem; color: #c8d2de; font-size: 1.05rem; }
 .story p.center { text-align: center; }
 .story .pullquote {
  max-width: 36rem; margin: 2rem auto;
  border-left: 3px solid var(--accent);
  padding: .6rem 1.2rem; color: var(--text); font-style: italic; font-size: 1.1rem;
 }

 .stats { display: grid; grid-template-columns: repeat(3, 1fr); gap: 1rem; margin: 2.5rem 0 1rem; }
 .stat {
  background: var(--panel); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 1.5rem; text-align: center;
 }
 .stat .num {
  font-size: clamp(1.8rem, 3vw, 2.4rem); font-weight: 800; letter-spacing: -0.02em;
  background: linear-gradient(135deg, #ff8a8a, #ffb27a);
  -webkit-background-clip: text; background-clip: text; color: transparent;
 }
 .stat.good .num { background: linear-gradient(135deg, #7be4a8, var(--accent)); -webkit-background-clip: text; background-clip: text; }
 .stat .lbl { color: var(--muted); font-size: .88rem; margin-top: .35rem; }

 .qa-table {
  width: 100%; border-collapse: collapse;
  background: var(--panel); border: 1px solid var(--border); border-radius: var(--radius);
  overflow: hidden;
 }
 .qa-table th, .qa-table td {
  padding: .85rem 1rem; text-align: left; font-size: .95rem;
  border-bottom: 1px solid var(--border); vertical-align: top;
 }
 .qa-table th { background: rgba(37,211,102,0.08); color: #c8d2de; font-weight: 600; font-size: .85em; letter-spacing: .03em; text-transform: uppercase; }
 .qa-table tr:last-child td { border-bottom: 0; }
 .qa-table td:first-child { color: var(--text); font-weight: 600; width: 38%; }
 .qa-table td:last-child { color: var(--muted); }

 .compare { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; margin: 1.5rem 0; }
 .compare .card {
  background: var(--panel); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 1.5rem;
 }
 .compare .card.before { border-top: 3px solid #c75757; }
 .compare .card.after  { border-top: 3px solid var(--accent); }
 .compare h3 { margin: 0 0 .75rem; font-size: 1rem; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); }
 .compare ul { margin: 0; padding-left: 1.1rem; color: #c8d2de; }
 .compare li { margin-bottom: .4rem; }

 /* Audio bubble */
 .bubble.audio { display: flex; align-items: center; gap: .55rem; min-width: 60%; }
 .bubble.audio .play {
  width: 28px; height: 28px; border-radius: 50%;
  background: rgba(255,255,255,0.2); display: grid; place-items: center; font-size: .8rem;
  flex-shrink: 0;
 }
 .bubble.audio .wave {
  flex: 1; display: flex; align-items: center; gap: 2px; height: 18px;
 }
 .bubble.audio .wave span {
  display: block; width: 2px; background: rgba(255,255,255,0.65); border-radius: 1px;
 }
 .bubble.audio .dur { font-size: .7rem; opacity: .8; }

 .urgency {
  background: rgba(255,138,138,0.08); border: 1px solid rgba(255,138,138,0.25);
  border-radius: var(--radius); padding: 1.2rem 1.5rem; margin: 1.5rem auto; max-width: 38rem;
  color: #ffd0d0; text-align: center; font-size: 1rem;
 }
 .urgency strong { color: #ffb27a; }

 @media (max-width: 820px) {
  .hero-grid { grid-template-columns: 1fr; }
  .features, .steps, .stats, .compare { grid-template-columns: 1fr; }
  .phone { width: 280px; }
  .nav-links a:not(.btn) { display: none; }
  .qa-table th, .qa-table td { font-size: .88rem; padding: .65rem .75rem; }
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
   <a href="#historia">Historia</a>
   <a href="#como">Cómo funciona</a>
   <a href="/pitch">Pitch completo</a>
   <a href="#contacto" class="btn btn-ghost">Empezar</a>
  </nav>
 </div>
</header>

<section class="hero">
 <div class="container hero-grid">
  <div>
   <span class="eyebrow"><span class="dot"></span> Copa del Mundo 2026 · CDMX</span>
   <h1 class="hero-title">La Copa llena tu restaurante. <span class="grad">Chalagente llena tu WhatsApp.</span></h1>
   <p class="hero-sub">Responde cada mensaje al instante — con un <strong>audio de voz</strong> en español mexicano. Sin contratar a nadie nuevo, sin perder mesas con quien responde primero.</p>
   <div class="hero-cta">
    <a href="#contacto" class="btn btn-primary">Quiero conectar mi WhatsApp →</a>
    <a href="/pitch" class="btn btn-ghost">Ver pitch completo</a>
   </div>
   <div class="hero-meta">
    <span>Respuesta en menos de 1 segundo</span>
    <span>Audio de voz, no solo texto</span>
    <span>Activación en 5 minutos</span>
   </div>
  </div>

  <div class="phone" aria-hidden="true">
   <div class="phone-screen">
    <div class="phone-header">
     <div class="avatar">T</div>
     <div>
      <div class="name">La Terraza Reforma</div>
      <div class="sub">en línea</div>
     </div>
    </div>
    <div class="chat">
     <div class="bubble in">¿Cuánto cuesta reservar mesa para el partido del sábado?</div>
     <div class="bubble out audio">
      <div class="play">▶</div>
      <div class="wave">
       <span style="height:6px"></span><span style="height:12px"></span><span style="height:8px"></span>
       <span style="height:16px"></span><span style="height:10px"></span><span style="height:14px"></span>
       <span style="height:7px"></span><span style="height:13px"></span><span style="height:9px"></span>
       <span style="height:15px"></span><span style="height:6px"></span><span style="height:11px"></span>
       <span style="height:8px"></span><span style="height:14px"></span><span style="height:10px"></span>
      </div>
      <span class="dur">0:08</span>
     </div>
     <div class="bubble in">¿Y hay estacionamiento cerca del Ángel?</div>
     <div class="bubble out audio">
      <div class="play">▶</div>
      <div class="wave">
       <span style="height:7px"></span><span style="height:14px"></span><span style="height:9px"></span>
       <span style="height:12px"></span><span style="height:16px"></span><span style="height:8px"></span>
       <span style="height:13px"></span><span style="height:10px"></span><span style="height:15px"></span>
       <span style="height:6px"></span><span style="height:11px"></span><span style="height:14px"></span>
      </div>
      <span class="dur">0:06</span>
     </div>
    </div>
   </div>
  </div>
 </div>
</section>

<section id="historia" class="block">
 <div class="container">
  <div class="section-head">
   <h2>Este es Juan.</h2>
   <p>Juan tiene <strong>La Terraza Reforma</strong>, un restaurante en la colonia Juárez, a dos cuadras del Ángel de la Independencia.</p>
  </div>
  <div class="story">
   <p>Terraza con vista a Reforma, cervezas artesanales, pantallas para los partidos. Ocho años ahí. Sus clientes — oficinistas de las torres cercanas, grupos de Roma y Zona Rosa — le escriben por <strong>WhatsApp</strong> para reservar, preguntar precios o armar paquetes para ver el fútbol.</p>
   <p>Juan conoce a sus clientes. Les contesta con cariño. Así ha crecido.</p>
   <p class="center"><strong>Pero se acerca la Copa del Mundo 2026.</strong></p>
  </div>

  <div class="stats">
   <div class="stat">
    <div class="num">147</div>
    <div class="lbl">mensajes en 3 horas<br>(México vs Argentina, 2022)</div>
   </div>
   <div class="stat">
    <div class="num">38</div>
    <div class="lbl">respondidos</div>
   </div>
   <div class="stat">
    <div class="num">−$21,000</div>
    <div class="lbl">en mesas perdidas<br>un solo fin de semana</div>
   </div>
  </div>

  <div class="story">
   <p class="center">El lunes, tres clientes le dijeron lo mismo: <em>“Te escribí el sábado pero nadie me contestó. Fuimos al de Polanco.”</em></p>
   <div class="pullquote">El 78% de los clientes compra con quien responde primero. En CDMX, entre el Ángel y Chapultepec hay un restaurante en cada esquina — quien no contesta, pierde la mesa al instante.</div>
   <p class="center">Y la Copa no es un fin de semana. Son <strong>cinco semanas</strong> de partidos, noches seguidas, mensajes a las dos de la mañana.</p>
  </div>
 </div>
</section>

<section id="como" class="block">
 <div class="container">
  <div class="section-head">
   <h2>Entonces le presentan Chalagente.</h2>
   <p>No es un chatbot que manda textos que nadie lee. Cuando un cliente pregunta algo, recibe en <strong>menos de un segundo</strong> un audio de voz — cálido, claro, en español mexicano.</p>
  </div>

  <div class="pullquote" style="text-align:center">“El paquete partido incluye mesa reservada, cuatro cervezas y botana por persona desde $349. Aparta con $100 por WhatsApp y te confirmamos en minutos.”</div>

  <p class="lede">La gente <strong>escucha</strong> el audio. No lo ignora.</p>

  <table class="qa-table" aria-label="Lo que Chalagente responde">
   <thead><tr><th>Pregunta del cliente</th><th>Lo que responde Chalagente</th></tr></thead>
   <tbody>
    <tr><td>Precio y paquetes</td><td>Tarifas, anticipo, promos</td></tr>
    <tr><td>Horarios y partidos</td><td>Apertura especial, qué transmiten</td></tr>
    <tr><td>Qué incluye</td><td>Cervezas, botana, mesa reservada</td></tr>
    <tr><td>Grupos grandes</td><td>Mesas de 6+, descuentos</td></tr>
    <tr><td>Ubicación</td><td>A dos cuadras del Ángel, estacionamiento en Juárez</td></tr>
    <tr><td>Disponibilidad</td><td>Cupos limitados, urgencia real</td></tr>
    <tr><td>Reservas</td><td>CTA directo: escribe “RESERVA”</td></tr>
   </tbody>
  </table>

  <p class="lede" style="margin-top:2rem">Si preguntan algo fuera de lo habitual, Chalagente redirige a Juan — <strong>solo cuando ya vale la pena que intervenga un humano</strong>.</p>
 </div>
</section>

<section class="block">
 <div class="container">
  <div class="section-head">
   <h2>Antes y después</h2>
  </div>
  <div class="compare">
   <div class="card before">
    <h3>Antes</h3>
    <ul>
     <li>147 mensajes recibidos</li>
     <li>38 respondidos</li>
     <li>$21,000 perdidos en un fin de semana</li>
     <li>Clientes yéndose a Polanco</li>
    </ul>
   </div>
   <div class="card after">
    <h3>Después</h3>
    <ul>
     <li>147 mensajes recibidos</li>
     <li><strong>147 respondidos al instante</strong></li>
     <li>Juan sirve mesas en la terraza</li>
     <li>Chalagente cierra ventas a las 2 AM, sin enfermarse, sin pedir aumento</li>
    </ul>
   </div>
  </div>
  <div class="pullquote" style="margin-top:2rem">“No reemplaza a mi equipo. Les quita lo repetitivo para que se concentren en llenar mesas.”</div>
 </div>
</section>

<section id="contacto" class="block">
 <div class="container">
  <div class="urgency">La Copa arranca en <strong>menos de tres semanas</strong>. Cada día que contestas tarde, otra mesa se va al restaurante de enfrente.</div>
  <div class="cta">
   <h2>Un paso: escanea el QR.</h2>
   <p>Chalagente empieza a responder. Tú te enfocas en servir.</p>
   <a href="mailto:hola@chalagente.com" class="btn btn-primary">Quiero conectar mi WhatsApp →</a>
   <p style="margin-top:1rem; font-size:.85rem"><a href="/pitch" style="color:var(--accent)">Leer el pitch completo →</a></p>
  </div>
 </div>
</section>

<footer>
 <div class="container">© Chalagente · La Copa del Mundo llena tu restaurante en Reforma. Chalagente llena tu WhatsApp.</div>
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
 .script { list-style: none; padding: 0; margin: 0; }
 .script li { padding: .6rem .75rem; border: 1px solid #e3e3e3; border-radius: 6px; margin-bottom: .5rem; background: #fafafa; }
 .script li.done { opacity: .55; background: #f0f4ef; }
 .script li.next { border-color: #1a7f37; background: #e6f4ea; }
 .script .topic { font-weight: 600; font-size: .85em; color: #1a4f7a; }
 .script .trig { font-size: .8em; color: #555; font-style: italic; margin: .15rem 0; }
 .script .reply { font-size: .9em; }
 .script .badge { float: right; font-size: .7em; padding: 1px 6px; border-radius: 10px; background: #ddd; color: #333; }
 .script li.done .badge { background: #cfe3cf; color: #054f31; }
 .script li.next .badge { background: #1a7f37; color: white; }
 .sessions { font-family: ui-monospace, monospace; font-size: .8em; color: #555; }
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
 <h2>Scripted responses</h2>
 <p>Each incoming message advances the chat by one step. After step {{ .ResponseCount }}, the bot stops replying until reset.</p>
 <form method="POST" action="/admin/reset-session">
  <button>Reset session</button>
 </form>
 {{ if .Sessions }}
 <p class="sessions">Active chats:
  {{ range .Sessions }}<br>· {{ .Chat }} — step {{ .Idx }}/{{ $.ResponseCount }}{{ end }}
 </p>
 {{ end }}
 <ol class="script">
  {{ range .Script }}
  <li>
   <span class="badge">#{{ .Num }}</span>
   <div class="topic">{{ .Topic }}</div>
   <div class="trig">Expected message: {{ .Triggers }}</div>
   <div class="reply">{{ .Reply }}</div>
  </li>
  {{ end }}
 </ol>
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
	Script        []scriptRow
	Sessions      []sessionRow
}

type scriptRow struct {
	Num      int
	Topic    string
	Triggers string
	Reply    string
}

type sessionRow struct {
	Chat string
	Idx  int
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
	root.HandleFunc("/pitch", a.handlePitch)
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
	script := make([]scriptRow, len(hardcodedScript))
	for i, s := range hardcodedScript {
		script[i] = scriptRow{Num: i + 1, Topic: s.Topic, Triggers: s.Triggers, Reply: s.Reply}
	}
	snap := a.sessionSnapshot()
	sessions := make([]sessionRow, 0, len(snap))
	for chat, idx := range snap {
		sessions = append(sessions, sessionRow{Chat: chat, Idx: idx})
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Chat < sessions[j].Chat })
	data := pageData{
		LoggedIn:      a.client.IsLoggedIn(),
		Connected:     a.client.IsConnected(),
		HasQR:         qrCode != "",
		Now:           time.Now().UnixNano(),
		Flash:         r.URL.Query().Get("flash"),
		FlashErr:      r.URL.Query().Get("err") == "1",
		ResponseCount: len(hardcodedResponses),
		Script:        script,
		Sessions:      sessions,
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
