package main

import (
	_ "embed"
	"html/template"
	"log"
	"net/http"
)

//go:embed docs/v1.4.1_landing.md
var pitchMD string

var pitchTmpl = template.Must(template.New("pitch").Parse(`<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Pitch — Chalagente para La Terraza Reforma</title>
<meta name="description" content="Historia de venta: Juan, La Terraza Reforma y Chalagente. ~3 minutos.">
<style>
 :root {
  --bg: #0b0f14; --panel: #141b24; --border: rgba(255,255,255,0.08);
  --text: #e7edf3; --muted: #8a96a6; --accent: #25d366;
 }
 * { box-sizing: border-box; }
 html, body { margin: 0; padding: 0; }
 body {
  font-family: ui-serif, Georgia, "Iowan Old Style", Cambria, serif;
  background: var(--bg); color: var(--text);
  line-height: 1.7; -webkit-font-smoothing: antialiased;
 }
 .container { max-width: 720px; margin: 0 auto; padding: 3rem 1.5rem 5rem; }
 .topbar { font-family: -apple-system, BlinkMacSystemFont, "Inter", sans-serif; font-size: .9rem; color: var(--muted); margin-bottom: 2.5rem; display: flex; justify-content: space-between; align-items: center; }
 .topbar a { color: var(--accent); text-decoration: none; }
 .topbar a:hover { text-decoration: underline; }
 article { font-size: 1.08rem; }
 article h1 {
  font-family: -apple-system, BlinkMacSystemFont, "Inter", sans-serif;
  font-size: clamp(1.9rem, 3.5vw, 2.6rem);
  letter-spacing: -0.02em; line-height: 1.15; margin: 0 0 1rem;
 }
 article h2 {
  font-family: -apple-system, BlinkMacSystemFont, "Inter", sans-serif;
  font-size: 1.4rem; margin: 2.5rem 0 1rem; letter-spacing: -0.01em;
 }
 article p { margin: 0 0 1.25rem; }
 article strong { color: #fff; font-weight: 700; }
 article em { color: #c8d2de; }
 article hr {
  border: 0; height: 1px; background: var(--border); margin: 2.5rem 0;
 }
 article blockquote {
  margin: 2rem 0; padding: .5rem 1.25rem;
  border-left: 3px solid var(--accent);
  color: var(--text); font-style: italic; font-size: 1.15rem;
 }
 article blockquote p { margin: 0; }
 article ul { padding-left: 1.4rem; margin: 0 0 1.25rem; }
 article li { margin-bottom: .4rem; color: #c8d2de; }
 article table {
  width: 100%; border-collapse: collapse; margin: 1.5rem 0;
  background: var(--panel); border: 1px solid var(--border); border-radius: 10px;
  overflow: hidden;
  font-family: -apple-system, BlinkMacSystemFont, "Inter", sans-serif;
  font-size: .95rem;
 }
 article th, article td {
  padding: .7rem 1rem; text-align: left; border-bottom: 1px solid var(--border);
  vertical-align: top;
 }
 article th { background: rgba(37,211,102,0.08); color: #c8d2de; font-weight: 600; font-size: .8em; text-transform: uppercase; letter-spacing: .04em; }
 article tr:last-child td { border-bottom: 0; }
 article td:first-child { color: var(--text); font-weight: 600; }
 .meta {
  font-family: -apple-system, BlinkMacSystemFont, "Inter", sans-serif;
  color: var(--muted); font-size: .9rem; font-style: italic;
  margin-bottom: 2rem;
 }
 .notes {
  font-family: -apple-system, BlinkMacSystemFont, "Inter", sans-serif;
  font-size: .9rem; color: var(--muted);
  background: var(--panel); border: 1px solid var(--border); border-radius: 10px;
  padding: 1.25rem 1.5rem; margin-top: 2.5rem;
 }
 .notes h2 { font-size: .8rem; text-transform: uppercase; letter-spacing: .06em; color: var(--text); margin: 0 0 .75rem; }
 .notes ul { padding-left: 1.2rem; margin: 0; }
 .notes li { margin-bottom: .3rem; }
</style>
</head>
<body>
<div class="container">

<div class="topbar">
 <a href="/">← Chalagente</a>
 <span>Pitch · ~3 minutos</span>
</div>

<article>

<h1>Historia de venta — Juan &amp; Chalagente</h1>
<div class="meta">Duración: ~3 minutos · Formato: pitch oral / video / demo<br>Ubicación: CDMX, colonia Juárez — a dos cuadras del Ángel de la Independencia</div>

<hr>

<h2>Guión</h2>

<p>Este es <strong>Juan</strong>.</p>

<p>Juan tiene La Terraza Reforma, un restaurante en la colonia Juárez, a dos cuadras del <strong>Ángel de la Independencia</strong>. Terraza con vista a Reforma, cervezas artesanales, pantallas para los partidos. Lleva ocho años ahí. Oficinistas de las torres cercanas, grupos de Roma y Zona Rosa — todos le escriben por <strong>WhatsApp</strong> para reservar, preguntar precios o armar paquetes para ver el fútbol.</p>

<p>Juan conoce a sus clientes. Les contesta con cariño. Así ha crecido.</p>

<p>Pero se acerca la <strong>Copa del Mundo 2026</strong>. Y Juan sabe lo que viene.</p>

<p>En 2022, el sábado de México vs Argentina, recibió <strong>147 mensajes en tres horas</strong>. Tenía la terraza llena y fila en la puerta. Solo alcanzó a contestar <strong>38</strong>. El lunes, tres clientes le dijeron lo mismo: <em>“Te escribí el sábado pero nadie me contestó. Fuimos al de Polanco.”</em></p>

<p>Juan hizo las cuentas. Cada mesa de seis personas son unos <strong>$1,800 pesos</strong>. Perdió al menos doce mesas ese fin de semana. Más de <strong>$21,000 pesos</strong> — por no responder a tiempo.</p>

<p>Y esto no fue un caso aislado. El <strong>78% de los clientes compra con quien responde primero</strong>. En la CDMX, donde hay un restaurante en cada esquina entre El Ángel y Chapultepec, quien no contesta pierde la mesa al instante.</p>

<p>La Copa no es un fin de semana. Son <strong>cinco semanas</strong> de partidos, noches seguidas, grupos de quince, mensajes a las dos de la mañana:</p>

<ul>
 <li>“¿Tienen mesa para ocho el sábado?”</li>
 <li>“¿Cuánto cuesta el paquete de cervezas?”</li>
 <li>“¿Hay estacionamiento cerca del Ángel?”</li>
</ul>

<p>Juan no puede contratar tres personas solo para junio y julio. Tampoco quiere que alguien le diga mal el precio o se le olvide mencionar la promo.</p>

<p><strong>El flujo de ventas está a punto de dispararse. Juan no está listo.</strong></p>

<hr>

<p>Entonces le presentan <strong>Chalagente</strong>.</p>

<p>No es un chatbot que manda textos que nadie lee. Cuando un cliente pregunta “¿cuánto cuesta reservar mesa para el partido?”, en menos de un segundo recibe un <strong>audio de voz</strong> — cálido, claro, en español mexicano:</p>

<blockquote><p>“El paquete partido incluye mesa reservada, cuatro cervezas y botana por persona desde $349. Aparta con $100 por WhatsApp y te confirmamos en minutos.”</p></blockquote>

<p>La gente <strong>escucha</strong> el audio. No lo ignora.</p>

<p>Juan escanea un QR — como vincular WhatsApp Web — y en <strong>cinco minutos</strong> Chalagente ya está conectado a su número de negocio.</p>

<p>Chalagente responde las preguntas que más dinero le cuestan:</p>

<table>
 <thead><tr><th>Pregunta del cliente</th><th>Respuesta automática</th></tr></thead>
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

<p>Si preguntan algo fuera de lo habitual, Chalagente los redirige a Juan — <strong>solo cuando ya vale la pena que intervenga un humano</strong>.</p>

<hr>

<p><strong>Antes:</strong> 147 mensajes, 38 respondidos, $21,000 perdidos, clientes yéndose a Polanco.</p>

<p><strong>Después:</strong> 147 mensajes, <strong>147 respondidos al instante</strong>. Juan sirve mesas en la terraza. Chalagente cierra ventas por WhatsApp. Funciona a las dos de la mañana, sin enfermarse, sin pedir aumento.</p>

<blockquote><p>“No reemplaza a mi equipo. Les quita lo repetitivo para que se concentren en llenar mesas.”</p></blockquote>

<hr>

<p>La Copa arranca en <strong>menos de tres semanas</strong>. Cada día que Juan contesta tarde, otra mesa se va al restaurante de enfrente.</p>

<p><strong>Un paso:</strong> escanea el QR. Chalagente empieza a responder. Tú te enfocas en servir.</p>

<blockquote><p><strong>“La Copa del Mundo llena tu restaurante en Reforma. Chalagente llena tu WhatsApp.”</strong></p></blockquote>

</article>

<div class="notes">
 <h2>Notas de producción</h2>
 <ul>
  <li><strong>Ritmo:</strong> ~140 palabras/min → ~420 palabras totales</li>
  <li><strong>Tono:</strong> cercano, narrativo, sin tecnicismos</li>
  <li><strong>CTA:</strong> demo en /demo o escaneo de QR en /admin</li>
  <li><strong>Diferenciador clave:</strong> respuestas en <strong>audio de voz</strong>, no solo texto</li>
 </ul>
</div>

</div>
</body>
</html>`))

// Keep the markdown embedded so future renderers (or readers viewing source)
// can use the source-of-truth document without it drifting from the rendered
// page.
var _ = pitchMD

func (a *App) handlePitch(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/pitch" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pitchTmpl.Execute(w, nil); err != nil {
		log.Printf("pitch template: %v", err)
	}
}
