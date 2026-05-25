package main

import (
	"errors"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mbanerjeepalmer/chalagente/internal/store"
	"github.com/mbanerjeepalmer/chalagente/internal/wamanager"
	"rsc.io/qr"
)

type dashData struct {
	Business             store.Business
	WAMeURL              string
	Connected            bool
	LoggedIn             bool
	Conversations        []convoRow
	Flash                string
	ClerkPublishableKey  string
	ClerkFrontendAPI     string
}

type convoRow struct {
	CustomerJID string
	UpdatedAt   time.Time
	LastBody    string
	LastDir     string
}

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	if b.Name == "" || b.WADeviceJID == "" {
		http.Redirect(w, r, "/onboarding", http.StatusSeeOther)
		return
	}

	convos, err := a.Store.ListConversations(r.Context(), b.ID, 20)
	if err != nil {
		log.Printf("dashboard convos: %v", err)
	}
	rows := make([]convoRow, 0, len(convos))
	for _, c := range convos {
		msgs, _ := a.Store.ListMessages(r.Context(), c.ID, 1)
		var lastBody, lastDir string
		if len(msgs) > 0 {
			lastBody = msgs[0].Body
			lastDir = msgs[0].Direction
		}
		rows = append(rows, convoRow{
			CustomerJID: c.CustomerJID,
			UpdatedAt:   c.UpdatedAt,
			LastBody:    lastBody,
			LastDir:     lastDir,
		})
	}

	status := waStatusFor(a, b.ID)
	data := dashData{
		Business:      b,
		WAMeURL:       waMeURL(b.WADeviceJID),
		Connected:     status.Connected,
		LoggedIn:      status.LoggedIn,
		Conversations: rows,
		Flash:         r.URL.Query().Get("flash"),
	}
	if a.ClerkAuth != nil {
		data.ClerkPublishableKey = a.ClerkAuth.PublishableKey
		data.ClerkFrontendAPI = a.ClerkAuth.FrontendAPI
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dashTmpl.Execute(w, data); err != nil {
		log.Printf("dashTmpl: %v", err)
	}
}

type waStatus struct{ Connected, LoggedIn bool }

func waStatusFor(a *App, bizID string) waStatus {
	client, ok := a.WAMgr.Client(bizID)
	if !ok {
		return waStatus{}
	}
	return waStatus{Connected: client.IsConnected(), LoggedIn: client.IsLoggedIn()}
}

func (a *App) handleDashboardAgentToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	b.AgentEnabled = r.PostForm.Get("enabled") == "1"
	if err := a.Store.UpdateBusiness(r.Context(), b); err != nil {
		http.Error(w, "save: "+err.Error(), http.StatusInternalServerError)
		return
	}
	state := "encendido"
	if !b.AgentEnabled {
		state = "apagado"
	}
	http.Redirect(w, r, "/app?flash=Agente+"+state, http.StatusSeeOther)
}

func (a *App) handleDashboardTriggerToggle(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	b.TriggerRequired = r.PostForm.Get("required") == "1"
	if err := a.Store.UpdateBusiness(r.Context(), b); err != nil {
		http.Error(w, "save: "+err.Error(), http.StatusInternalServerError)
		return
	}
	state := "obligatoria"
	if !b.TriggerRequired {
		state = "opcional"
	}
	http.Redirect(w, r, "/app?flash=Palabra+clave+"+state, http.StatusSeeOther)
}

func (a *App) handleDashboardBusiness(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		b.Name = strings.TrimSpace(r.PostForm.Get("name"))
		b.Address = strings.TrimSpace(r.PostForm.Get("address"))
		b.Phone = strings.TrimSpace(r.PostForm.Get("phone"))
		b.Website = strings.TrimSpace(r.PostForm.Get("website"))
		b.Hours = strings.TrimSpace(r.PostForm.Get("hours"))
		b.ExtraInfo = strings.TrimSpace(r.PostForm.Get("extra"))
		if err := a.Store.UpdateBusiness(r.Context(), b); err != nil {
			http.Error(w, "save: "+err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/app/business?flash=Guardado", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dashBusinessTmpl.Execute(w, struct {
		Business store.Business
		Flash    string
	}{Business: b, Flash: r.URL.Query().Get("flash")}); err != nil {
		log.Printf("dashBusinessTmpl: %v", err)
	}
}

// handleDashboardUnpair drops the WhatsApp link: it tells WhatsApp servers to
// remove the linked device (best-effort — a missing live client is fine), then
// clears the persisted device JID so the tenant won't be auto-reconnected on
// next boot. The user will need to scan a fresh QR to pair again.
func (a *App) handleDashboardUnpair(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	if err := a.WAMgr.Logout(r.Context(), b.ID); err != nil && !errors.Is(err, wamanager.ErrNotRegistered) {
		log.Printf("unpair: logout %s: %v", b.ID, err)
	}
	b.WADeviceJID = ""
	if err := a.Store.UpdateBusiness(r.Context(), b); err != nil {
		http.Error(w, "save: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/app?flash=WhatsApp+desvinculado", http.StatusSeeOther)
}

func (a *App) handleDashboardEvents(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	flusher, ok2 := w.(http.Flusher)
	if !ok2 {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")

	ch, snapshot, unsub := a.subscribe(b.ID)
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
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}

func (a *App) handleDashboardShareQR(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	if b.WADeviceJID == "" {
		http.Error(w, "no wa device", http.StatusNotFound)
		return
	}
	link := waMeURL(b.WADeviceJID)
	if link == "" {
		http.Error(w, "no wa.me link", http.StatusNotFound)
		return
	}
	c, err := qr.Encode(link, qr.M)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(c.PNG())
}

// ----- templates -----

var dashTmpl = template.Must(template.New("dash").Parse(`<!doctype html><html lang="es"><head><meta charset="utf-8"><title>{{ .Business.Name }} — Chalagente</title>
<style>
body{font-family:system-ui,sans-serif;max-width:960px;margin:2rem auto;padding:0 1rem;color:#222;line-height:1.5}
h1{margin-top:0}
.grid{display:grid;grid-template-columns:2fr 1fr;gap:1.5rem}
@media(max-width:720px){.grid{grid-template-columns:1fr}}
.card{border:1px solid #ddd;border-radius:8px;padding:1rem;margin-bottom:1rem}
.status{display:inline-block;padding:2px 8px;border-radius:4px;font-size:.85em}
.ok{background:#d1fadf;color:#054f31}.bad{background:#fde2e1;color:#6e1d1a}
.flash{background:#eef;border:1px solid #99c;padding:.5rem;border-radius:4px;margin-bottom:1rem}
nav.tabs{display:flex;gap:.5rem;margin-bottom:1.5rem;border-bottom:1px solid #ddd}
nav.tabs a{padding:.5rem 1rem;text-decoration:none;color:#555;border-bottom:2px solid transparent}
nav.tabs a.active{color:#25d366;border-bottom-color:#25d366;font-weight:600}
img.qr{width:200px;height:200px;image-rendering:pixelated;border:1px solid #ddd}
ul.convos{list-style:none;padding:0;max-height:24rem;overflow:auto}
ul.convos li{padding:.6rem;border-bottom:1px solid #eee}
ul.convos .who{font-weight:600;font-size:.9em}
ul.convos .body{color:#555;font-size:.85em}
ul.convos .when{color:#999;font-size:.75em;float:right}
button{padding:.5rem .9rem;border-radius:4px;border:1px solid #bbb;background:#f5f5f5;cursor:pointer;font-size:.9em}
button.primary{background:#25d366;color:white;border-color:#1f9c4d;font-weight:600}
form{display:inline}
</style></head><body>
<h1>{{ .Business.Name }}</h1>
<nav class="tabs">
 <a href="/app" class="active">Conversaciones</a>
 <a href="/app/business">Información</a>
</nav>
{{ if .Flash }}<div class="flash">{{ .Flash }}</div>{{ end }}
<div class="grid">
 <div>
  <div class="card">
   <strong>Agente:</strong>
   {{ if .Business.AgentEnabled }}<span class="status ok">encendido</span>{{ else }}<span class="status bad">apagado</span>{{ end }}
   <form method="POST" action="/app/agent" style="float:right">
    {{ if .Business.AgentEnabled }}
     <input type="hidden" name="enabled" value="0"><button>Apagar</button>
    {{ else }}
     <input type="hidden" name="enabled" value="1"><button class="primary">Encender</button>
    {{ end }}
   </form>
   <p style="font-size:.85em;color:#555;margin:.4rem 0 0;clear:both">
    {{ if .Business.TriggerRequired }}
     Responde cuando alguien menciona «Chalagente» en el chat. Una vez mencionado, sigue respondiendo en esa conversación.
    {{ else }}
     Responde a <strong>todos</strong> los mensajes entrantes — no se necesita palabra clave.
    {{ end }}
   </p>
   <form method="POST" action="/app/trigger" style="margin-top:.4rem">
    {{ if .Business.TriggerRequired }}
     <input type="hidden" name="required" value="0"><button type="submit">Quitar palabra clave</button>
    {{ else }}
     <input type="hidden" name="required" value="1"><button type="submit">Exigir «Chalagente»</button>
    {{ end }}
   </form>
   <br>
   <strong>WhatsApp:</strong>
   {{ if .LoggedIn }}<span class="status ok">vinculado</span>{{ else }}<span class="status bad">desvinculado</span>{{ end }}
   {{ if .Connected }}<span class="status ok">conectado</span>{{ else }}<span class="status bad">desconectado</span>{{ end }}
   <div style="font-family:monospace;font-size:.85em;color:#555">{{ .Business.WADeviceJID }}</div>
   {{ if .Business.WADeviceJID }}
   <form method="POST" action="/app/whatsapp/unpair" style="margin-top:.5rem"
         onsubmit="return confirm('¿Desvincular WhatsApp? Tendrás que escanear el QR otra vez.');">
    <button>Desvincular WhatsApp</button>
   </form>
   {{ end }}
  </div>
  <div class="card">
   <h2 style="margin-top:0">Conversaciones recientes</h2>
   {{ if .Conversations }}
    <ul class="convos">
    {{ range .Conversations }}
     <li>
      <span class="when">{{ .UpdatedAt.Format "15:04" }}</span>
      <div class="who">{{ .CustomerJID }}</div>
      <div class="body">{{ if eq .LastDir "out" }}→{{ else }}←{{ end }} {{ .LastBody }}</div>
     </li>
    {{ end }}
    </ul>
   {{ else }}
    <p style="color:#888">Aún no hay conversaciones.</p>
   {{ end }}
  </div>
  <div class="card">
   <h2 style="margin-top:0">En vivo</h2>
   <ul id="feed" style="font-family:monospace;font-size:.85em;max-height:18rem;overflow:auto;list-style:none;padding:0"></ul>
  </div>
 </div>
 <div>
  <div class="card" style="text-align:center">
   <h2 style="margin-top:0">Comparte tu número</h2>
   <img class="qr" src="/app/qr.png">
   <p style="font-size:.85em"><a href="{{ .WAMeURL }}">{{ .WAMeURL }}</a></p>
  </div>
  <div class="card">
   {{ if .ClerkPublishableKey }}
    <div id="clerk-user-button" style="display:flex;align-items:center;gap:.5rem"></div>
   {{ else }}
    <form method="POST" action="/logout"><button>Cerrar sesión</button></form>
   {{ end }}
  </div>
 </div>
</div>
<script>
const feed = document.getElementById('feed');
const es = new EventSource('/app/events');
es.onmessage = (e) => {
 try {
  const d = JSON.parse(e.data);
  const t = new Date(d.time).toLocaleTimeString();
  const arrow = d.dir === 'in' ? '←' : '→';
  const li = document.createElement('li');
  li.textContent = t + ' ' + arrow + ' ' + d.chat + ': ' + d.body;
  feed.prepend(li);
 } catch {}
};
</script>
{{ if .ClerkPublishableKey }}
<script
  async
  crossorigin="anonymous"
  data-clerk-publishable-key="{{ .ClerkPublishableKey }}"
  src="https://{{ .ClerkFrontendAPI }}/npm/@clerk/clerk-js@5/dist/clerk.browser.js"
  type="text/javascript"
  onload="bootClerkButton()"
></script>
<script>
async function bootClerkButton() {
  await window.Clerk.load();
  const mount = document.getElementById('clerk-user-button');
  if (!mount) return;
  if (!window.Clerk.user) { window.location.href = '/sign-in'; return; }
  window.Clerk.mountUserButton(mount, { afterSignOutUrl: '/' });
}
</script>
{{ end }}
</body></html>`))

var dashBusinessTmpl = template.Must(template.New("dashBusiness").Parse(`<!doctype html><html lang="es"><head><meta charset="utf-8"><title>Información — Chalagente</title>
<style>
body{font-family:system-ui,sans-serif;max-width:680px;margin:2rem auto;padding:0 1rem;color:#222;line-height:1.5}
nav.tabs{display:flex;gap:.5rem;margin-bottom:1.5rem;border-bottom:1px solid #ddd}
nav.tabs a{padding:.5rem 1rem;text-decoration:none;color:#555;border-bottom:2px solid transparent}
nav.tabs a.active{color:#25d366;border-bottom-color:#25d366;font-weight:600}
label{display:block;margin:.75rem 0 .25rem;font-weight:500}
input,textarea{width:100%;padding:.5rem;font-size:1rem;border:1px solid #bbb;border-radius:4px;font-family:inherit;box-sizing:border-box}
button{padding:.6rem 1rem;font-size:1rem;border-radius:4px;border:1px solid #bbb;background:#25d366;color:white;border-color:#1f9c4d;font-weight:600;cursor:pointer}
.flash{background:#dfe;border:1px solid #9c9;padding:.5rem;border-radius:4px;margin:1rem 0}
</style></head><body>
<nav class="tabs">
 <a href="/app">Conversaciones</a>
 <a href="/app/business" class="active">Información</a>
</nav>
<h1>Información del negocio</h1>
{{ if .Flash }}<div class="flash">{{ .Flash }}</div>{{ end }}
<form method="POST" action="/app/business">
 <label>Nombre<input name="name" value="{{ .Business.Name }}"></label>
 <label>Dirección<input name="address" value="{{ .Business.Address }}"></label>
 <label>Teléfono<input name="phone" value="{{ .Business.Phone }}"></label>
 <label>Sitio web<input name="website" value="{{ .Business.Website }}"></label>
 <label>Horarios<textarea name="hours" rows="3">{{ .Business.Hours }}</textarea></label>
 <label>Información extra (FAQ, precios, políticas)<textarea name="extra" rows="10">{{ .Business.ExtraInfo }}</textarea></label>
 <button>Guardar</button>
</form>
</body></html>`))
