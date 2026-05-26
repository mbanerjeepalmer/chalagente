package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mbanerjeepalmer/chalagente/internal/store"
	"go.mau.fi/whatsmeow"
	"rsc.io/qr"
)

// onboardingStep determines which wizard step a given business is on.
func onboardingStep(b store.Business) string {
	if b.Name == "" {
		return "business"
	}
	if b.ExtraInfo == "" {
		// Extra info optional — but we ask for it as a separate step so users
		// see the field. Skipping is allowed by submitting empty.
	}
	if b.WADeviceJID == "" {
		return "whatsapp"
	}
	return "test"
}

func (a *App) ensureBusiness(ctx context.Context, userID string) (store.Business, error) {
	b, err := a.Store.GetBusinessByUserID(ctx, userID)
	if err == nil {
		return b, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Business{}, err
	}
	return a.Store.CreateBusiness(ctx, userID)
}

func (a *App) requireBusiness(w http.ResponseWriter, r *http.Request) (store.Business, bool) {
	userID, ok := a.userIDFrom(r)
	if !ok {
		http.Redirect(w, r, a.signInPath(), http.StatusSeeOther)
		return store.Business{}, false
	}
	b, err := a.ensureBusiness(r.Context(), userID)
	if err != nil {
		http.Error(w, "store error: "+err.Error(), http.StatusInternalServerError)
		return store.Business{}, false
	}
	return b, true
}

func (a *App) handleOnboarding(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	step := onboardingStep(b)
	switch step {
	case "business":
		http.Redirect(w, r, "/onboarding/business", http.StatusSeeOther)
	case "whatsapp":
		http.Redirect(w, r, "/onboarding/whatsapp", http.StatusSeeOther)
	case "test":
		http.Redirect(w, r, "/onboarding/test", http.StatusSeeOther)
	default:
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	}
}

// ---- Step 1: business basics ----

type onbBusinessData struct {
	Business store.Business
	Query    string
	Results  []mapsResult
	Flash    string
}

type mapsResult struct {
	PlaceID    string
	Name       string
	Address    string
	Phone      string
	Website    string
	Categories string
	Hours      string
}

func (a *App) handleOnboardingBusiness(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		action := r.PostForm.Get("action")
		if action == "search" {
			q := strings.TrimSpace(r.PostForm.Get("q"))
			places, err := a.Maps.Search(r.Context(), q)
			if err != nil {
				renderOnbBusiness(w, b, q, nil, "Búsqueda falló: "+err.Error())
				return
			}
			results := make([]mapsResult, 0, len(places))
			for _, p := range places {
				results = append(results, mapsResult{
					PlaceID: p.PlaceID, Name: p.Name, Address: p.Address,
					Phone: p.Phone, Website: p.Website,
					Categories: strings.Join(p.Categories, ", "),
					Hours:      p.Hours,
				})
			}
			renderOnbBusiness(w, b, q, results, "")
			return
		}
		// Save form
		b.Name = strings.TrimSpace(r.PostForm.Get("name"))
		b.Address = strings.TrimSpace(r.PostForm.Get("address"))
		b.Phone = strings.TrimSpace(r.PostForm.Get("phone"))
		b.Website = strings.TrimSpace(r.PostForm.Get("website"))
		b.Hours = strings.TrimSpace(r.PostForm.Get("hours"))
		b.MapsPlaceID = strings.TrimSpace(r.PostForm.Get("place_id"))
		if b.Name == "" {
			renderOnbBusiness(w, b, "", nil, "El nombre es obligatorio.")
			return
		}
		if err := a.Store.UpdateBusiness(r.Context(), b); err != nil {
			http.Error(w, "save: "+err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/onboarding/extra", http.StatusSeeOther)
		return
	}
	renderOnbBusiness(w, b, "", nil, "")
}

func renderOnbBusiness(w http.ResponseWriter, b store.Business, q string, results []mapsResult, flash string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := onbBusinessTmpl.Execute(w, onbBusinessData{Business: b, Query: q, Results: results, Flash: flash}); err != nil {
		log.Printf("onbBusinessTmpl: %v", err)
	}
}

// ---- Step 2: extra info ----

func (a *App) handleOnboardingExtra(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		b.ExtraInfo = strings.TrimSpace(r.PostForm.Get("extra"))
		if err := a.Store.UpdateBusiness(r.Context(), b); err != nil {
			http.Error(w, "save: "+err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/onboarding/whatsapp", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := onbExtraTmpl.Execute(w, b); err != nil {
		log.Printf("onbExtraTmpl: %v", err)
	}
}

// ---- Step 3: WhatsApp pairing ----

func (a *App) handleOnboardingWhatsApp(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	if b.WADeviceJID != "" {
		// Already paired; skip ahead.
		http.Redirect(w, r, "/onboarding/test", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := onbWATmpl.Execute(w, b); err != nil {
		log.Printf("onbWATmpl: %v", err)
	}
}

func (a *App) handleOnboardingWhatsAppStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	if b.WADeviceJID != "" {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte("already paired"))
		return
	}

	pairCtx, cancel := context.WithCancel(context.Background())
	sess := &pairSession{cancel: cancel}
	a.setPairSession(b.ID, sess)

	qrChan, err := a.WAMgr.StartPairing(pairCtx, b.ID)
	if err != nil {
		a.clearPairSession(b.ID)
		http.Error(w, "pair start: "+err.Error(), http.StatusInternalServerError)
		return
	}

	go a.drivePairing(b.ID, qrChan)
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("pairing started"))
}

func (a *App) drivePairing(bizID string, qrChan <-chan whatsmeow.QRChannelItem) {
	for evt := range qrChan {
		sess := a.getPairSession(bizID)
		if sess == nil {
			return
		}
		sess.mu.Lock()
		sess.event = evt.Event
		if evt.Event == "code" {
			sess.code = evt.Code
		}
		if evt.Event == "success" {
			if jid, ok := a.WAMgr.DeviceJID(bizID); ok {
				sess.deviceJID = jid.String()
				// Persist on the business.
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				biz, err := a.Store.GetBusiness(ctx, bizID)
				if err == nil {
					biz.WADeviceJID = jid.String()
					if err := a.Store.UpdateBusiness(ctx, biz); err != nil {
						log.Printf("pair: save jid: %v", err)
					}
				}
				cancel()
			}
			sess.done = true
		}
		sess.mu.Unlock()
		if evt.Event == "success" || evt.Event == "timeout" || evt.Event == "err-client-outdated" {
			return
		}
	}
}

func (a *App) handleOnboardingQRPNG(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	sess := a.getPairSession(b.ID)
	if sess == nil {
		http.Error(w, "no pair session", http.StatusNotFound)
		return
	}
	sess.mu.Lock()
	code := sess.code
	sess.mu.Unlock()
	if code == "" {
		http.Error(w, "no qr yet", http.StatusNotFound)
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

func (a *App) handleOnboardingPairStatus(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	sess := a.getPairSession(b.ID)
	w.Header().Set("Content-Type", "application/json")
	if sess == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"state": "idle"})
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"state":      sess.event,
		"has_qr":     sess.code != "",
		"device_jid": sess.deviceJID,
		"done":       sess.done,
	})
}

// ---- Step 4: test run ----

func (a *App) handleOnboardingTest(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	if b.WADeviceJID == "" {
		http.Redirect(w, r, "/onboarding/whatsapp", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := onbTestTmpl.Execute(w, struct {
		Business store.Business
		WAMeURL  string
	}{
		Business: b,
		WAMeURL:  waMeURL(b.WADeviceJID),
	}); err != nil {
		log.Printf("onbTestTmpl: %v", err)
	}
}

func (a *App) handleOnboardingFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.clearPairSession("") // no-op safety
	// Land on the connection screen so the user lands somewhere actionable —
	// they've just paired, and the tooltip there nudges them into filling
	// out business info next.
	http.Redirect(w, r, "/admin/connection?hint=biz", http.StatusSeeOther)
}

func waMeURL(jidStr string) string {
	phone := phoneFromJID(jidStr)
	if phone == "" {
		return ""
	}
	return fmt.Sprintf("https://wa.me/%s", phone)
}

// phoneFromJID extracts the bare phone number from a whatsmeow JID. JIDs look
// like "5215512345678.0:13@s.whatsapp.net" or "5215512345678@s.whatsapp.net".
func phoneFromJID(jidStr string) string {
	if jidStr == "" {
		return ""
	}
	at := strings.Index(jidStr, "@")
	if at <= 0 {
		return ""
	}
	user := jidStr[:at]
	if dot := strings.Index(user, "."); dot > 0 {
		user = user[:dot]
	}
	if colon := strings.Index(user, ":"); colon > 0 {
		user = user[:colon]
	}
	return user
}

// ----- templates -----

var onbBusinessTmpl = template.Must(template.New("onbBusiness").Parse(onbCommonCSS + `
<h1>Paso 1 de 3 — Tu negocio</h1>
{{ if .Flash }}<div class="flash">{{ .Flash }}</div>{{ end }}
<form method="POST" action="/onboarding/business">
 <input type="hidden" name="action" value="search">
 <label>Busca tu negocio en Google Maps:</label>
 <div class="row"><input name="q" value="{{ .Query }}" placeholder="Nombre del negocio + ciudad"><button>Buscar</button></div>
</form>
{{ if .Results }}
<h2>Resultados</h2>
<ul class="results">
{{ range .Results }}
 <li>
  <strong>{{ .Name }}</strong><br>
  <span class="muted">{{ .Address }}</span><br>
  <form method="POST" action="/onboarding/business" class="inline">
   <input type="hidden" name="action" value="save">
   <input type="hidden" name="place_id" value="{{ .PlaceID }}">
   <input type="hidden" name="name" value="{{ .Name }}">
   <input type="hidden" name="address" value="{{ .Address }}">
   <input type="hidden" name="phone" value="{{ .Phone }}">
   <input type="hidden" name="website" value="{{ .Website }}">
   <input type="hidden" name="hours" value="{{ .Hours }}">
   <button>Usar este</button>
  </form>
 </li>
{{ end }}
</ul>
{{ end }}
<h2>O escribe los datos a mano</h2>
<form method="POST" action="/onboarding/business">
 <input type="hidden" name="action" value="save">
 <label>Nombre*<input name="name" value="{{ .Business.Name }}" required></label>
 <label>Dirección<input name="address" value="{{ .Business.Address }}"></label>
 <label>Teléfono<input name="phone" value="{{ .Business.Phone }}"></label>
 <label>Sitio web<input name="website" value="{{ .Business.Website }}"></label>
 <label>Horarios<textarea name="hours" rows="3">{{ .Business.Hours }}</textarea></label>
 <button class="primary">Guardar y continuar</button>
</form>
`))

var onbExtraTmpl = template.Must(template.New("onbExtra").Parse(onbCommonCSS + `
<h1>Paso 2 de 3 — Información extra</h1>
<p>Lo que no aparece en Maps: precios, FAQ, formas de pago, políticas. Esto lo lee el agente cada vez que responde.</p>
<form method="POST" action="/onboarding/extra">
 <textarea name="extra" rows="12" placeholder="Ej: Aceptamos efectivo y tarjeta. Las reservas se hacen por adelantado. Tenemos opciones vegetarianas...">{{ .ExtraInfo }}</textarea>
 <button class="primary">Guardar y continuar</button>
</form>
<div style="margin:1.5rem 0;padding:1rem;background:#eef7ee;border:1px solid #cde2cd;border-radius:6px">
 <strong>¿Quieres probar el agente ahora mismo?</strong>
 <p>Antes de conectar WhatsApp, puedes mandarle mensajes desde aquí y ver cómo responde con los datos que ya escribiste.</p>
 <a href="/demo" class="primary" style="display:inline-block;padding:.5rem 1rem;background:#25d366;color:white;border-radius:4px;text-decoration:none;font-weight:600">Abrir simulador →</a>
</div>
<p><a href="/onboarding/whatsapp">Saltar este paso</a></p>
`))

var onbWATmpl = template.Must(template.New("onbWA").Parse(onbCommonCSS + `
<h1>Paso 3 de 3 — Conecta WhatsApp</h1>
<p>Vincula tu número escaneando un QR desde WhatsApp → Ajustes → Dispositivos vinculados → Vincular un dispositivo.</p>
<button onclick="startPairing()" id="startBtn">Generar QR</button>
<div id="qrBox" style="display:none">
 <img id="qrImg" src="" alt="QR" style="width:256px;height:256px;image-rendering:pixelated;border:1px solid #ccc">
 <p id="status" class="muted">Esperando QR…</p>
</div>
<script>
async function startPairing() {
 document.getElementById('startBtn').disabled = true;
 const r = await fetch('/onboarding/whatsapp/start', {method:'POST'});
 if (!r.ok) { document.getElementById('status').textContent = 'Error: ' + r.status; return; }
 document.getElementById('qrBox').style.display = 'block';
 poll();
}
async function poll() {
 try {
  const r = await fetch('/onboarding/whatsapp/status');
  const s = await r.json();
  document.getElementById('status').textContent = 'Estado: ' + (s.state || 'pendiente');
  if (s.has_qr) document.getElementById('qrImg').src = '/onboarding/whatsapp/qr.png?ts=' + Date.now();
  if (s.done) { window.location = '/onboarding/test'; return; }
 } catch (e) { console.error(e); }
 setTimeout(poll, 1500);
}
</script>
`))

var onbTestTmpl = template.Must(template.New("onbTest").Parse(onbCommonCSS + `
<h1>¡Casi listo! — Prueba el agente</h1>
<p>Tu número está conectado. Mándale un mensaje desde otro teléfono y mira al agente responder en vivo.</p>
<p><strong>Importante:</strong> el agente solo responde cuando tu mensaje incluye la palabra «Chalagente». Una vez mencionado, sigue respondiendo a los siguientes mensajes de esa conversación.</p>
<p><strong>Tu link wa.me:</strong> <a href="{{ .WAMeURL }}">{{ .WAMeURL }}</a></p>
<h2>Transcripción en vivo</h2>
<ul id="feed" style="font-family:monospace;font-size:.85em;max-height:18rem;overflow:auto;border:1px solid #ddd;padding:.5rem"></ul>
<form method="POST" action="/onboarding/finish">
 <button class="primary">Listo, esto funciona →</button>
</form>
<script>
const feed = document.getElementById('feed');
const es = new EventSource('/admin/events');
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
`))

const onbCommonCSS = `<!doctype html><html lang="es"><head><meta charset="utf-8"><title>Configuración — Chalagente</title>
<style>
body{font-family:system-ui,sans-serif;max-width:680px;margin:2rem auto;padding:0 1rem;color:#222;line-height:1.5}
h1{margin-top:0}h2{margin-top:2rem;font-size:1.1rem}
label{display:block;margin:.75rem 0 .25rem;font-weight:500}
input,textarea{width:100%;padding:.5rem;font-size:1rem;border:1px solid #bbb;border-radius:4px;font-family:inherit;box-sizing:border-box}
button{padding:.6rem 1rem;font-size:1rem;border-radius:4px;border:1px solid #bbb;background:#f5f5f5;cursor:pointer}
button.primary{background:#25d366;color:white;border-color:#1f9c4d;font-weight:600}
button:disabled{opacity:.5}
.row{display:flex;gap:.5rem}.row input{flex:1}
form.inline{display:inline;margin:0}
.results{list-style:none;padding:0}.results li{border:1px solid #ddd;padding:.75rem;margin:.5rem 0;border-radius:4px}
.muted{color:#666;font-size:.9em}
.flash{background:#fee;border:1px solid #c66;padding:.5rem;border-radius:4px;margin:1rem 0}
</style></head><body>`
