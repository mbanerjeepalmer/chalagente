package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mbanerjeepalmer/chalagente/internal/store"
	"github.com/mbanerjeepalmer/chalagente/internal/wamanager"
	"rsc.io/qr"
)

// defaultPrefillTemplate is the seed copy a business gets if they haven't
// customised their wa.me prefill yet. Refining-notes answer was
// 'Chalagente, help me at {business name}'. The placeholder is substituted
// in resolvePrefill.
const defaultPrefillTemplate = "Chalagente, help me at {business}"

// resolvePrefill expands the per-business prefill template against the
// business name. If the tenant left the template blank we fall back to
// defaultPrefillTemplate. We also re-insert the trigger keyword silently
// when the business has gating on but their custom template forgot it,
// so a customer scanning the QR always trips the agent.
func resolvePrefill(b store.Business) string {
	tpl := strings.TrimSpace(b.WAPrefillTemplate)
	if tpl == "" {
		tpl = defaultPrefillTemplate
	}
	return finalisePrefill(b, tpl)
}

// resolvePrefillForLang picks the translation that best matches lang from
// the business's stored translations map, then runs it through the same
// {business} substitution + keyword-injection as resolvePrefill. Returns
// the source-language prefill if no usable translation is found.
func resolvePrefillForLang(b store.Business, lang string) string {
	if t := pickTranslation(b.WAPrefillTranslations, lang); t != "" {
		return finalisePrefill(b, t)
	}
	return resolvePrefill(b)
}

// finalisePrefill substitutes {business} and prepends the trigger keyword
// when gating is on. Shared between the source-language and translated
// branches so the keyword rule applies uniformly.
func finalisePrefill(b store.Business, tpl string) string {
	out := strings.ReplaceAll(tpl, "{business}", b.Name)
	if b.TriggerRequired && !strings.Contains(strings.ToLower(out), triggerKeyword) {
		out = "Chalagente, " + out
	}
	return out
}

// pickTranslation walks the Accept-Language header (or a bare language tag)
// in priority order and returns the first stored translation whose primary
// subtag matches. Returns empty string when no entry matches; the caller is
// expected to fall back to the source template.
func pickTranslation(translations map[string]string, acceptLang string) string {
	if len(translations) == 0 || acceptLang == "" {
		return ""
	}
	for _, tag := range parseAcceptLanguage(acceptLang) {
		if v, ok := translations[tag]; ok && v != "" {
			return v
		}
	}
	return ""
}

// parseAcceptLanguage extracts language primary-subtags from a header value
// in q-weighted order. "es-MX,es;q=0.9,en;q=0.8" → ["es", "en"].
// Bare language codes (e.g. "fr") work too.
func parseAcceptLanguage(header string) []string {
	type weighted struct {
		tag string
		q   float64
	}
	var out []weighted
	seen := map[string]bool{}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag := part
		q := 1.0
		if i := strings.Index(part, ";"); i >= 0 {
			tag = strings.TrimSpace(part[:i])
			rest := part[i+1:]
			if eq := strings.Index(rest, "q="); eq >= 0 {
				if f, err := strconv.ParseFloat(strings.TrimSpace(rest[eq+2:]), 64); err == nil {
					q = f
				}
			}
		}
		if dash := strings.Index(tag, "-"); dash > 0 {
			tag = tag[:dash]
		}
		tag = strings.ToLower(tag)
		if tag == "" || tag == "*" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, weighted{tag: tag, q: q})
	}
	// Stable sort by q desc — keeps original order for ties.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].q < out[j].q; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	tags := make([]string, len(out))
	for i, w := range out {
		tags[i] = w.tag
	}
	return tags
}

// businessShareURL is the canonical link a business hands out — a
// chalagente.com /go/<id> redirect that lands the customer in WhatsApp with
// the prefill text already typed. The pretty URL is the part we encode into
// the QR; the redirect handler unfolds it into the real wa.me link.
func (a *App) businessShareURL(b store.Business) string {
	base := strings.TrimRight(a.BaseURL, "/")
	if base == "" {
		base = "https://chalagente.com"
	}
	return fmt.Sprintf("%s/go/%s", base, b.ID)
}

// businessShareTarget builds the wa.me URL we'll 302 the customer to. When
// acceptLang is set we serve the matching translation from
// b.WAPrefillTranslations; otherwise we fall back to the source-language
// template.
func businessShareTarget(b store.Business, acceptLang string) string {
	phone := phoneFromJID(b.WADeviceJID)
	if phone == "" {
		return ""
	}
	text := resolvePrefillForLang(b, acceptLang)
	if text == "" {
		return fmt.Sprintf("https://wa.me/%s", phone)
	}
	return fmt.Sprintf("https://wa.me/%s?text=%s", phone, url.QueryEscape(text))
}

type dashData struct {
	Business            store.Business
	WAMeURL             string // wa.me link with the prefilled text
	ShareURL            string // /go/<id> redirect URL — what we put on the QR
	PrefillResolved     string // the actual prefilled message text, post-template
	Connected           bool
	LoggedIn            bool
	Conversations       []convoRow
	Flash               string
	ClerkPublishableKey string
	ClerkFrontendAPI    string
}

type convoRow struct {
	ID          string
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
			ID:          c.ID,
			CustomerJID: c.CustomerJID,
			UpdatedAt:   c.UpdatedAt,
			LastBody:    lastBody,
			LastDir:     lastDir,
		})
	}

	status := waStatusFor(a, b.ID)
	data := dashData{
		Business:        b,
		WAMeURL:         businessShareTarget(b, ""),
		ShareURL:        a.businessShareURL(b),
		PrefillResolved: resolvePrefill(b),
		Connected:       status.Connected,
		LoggedIn:        status.LoggedIn,
		Conversations:   rows,
		Flash:           r.URL.Query().Get("flash"),
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

// prefillInputChanged returns true when the inputs that affect the cached
// translation set changed — the source-language template, the business
// name (which is substituted into {business} in the LLM prompt) or the
// trigger-required flag (which decides whether to ask the model to fold
// the keyword into the source string).
func prefillInputChanged(prev, next store.Business) bool {
	return prev.WAPrefillTemplate != next.WAPrefillTemplate ||
		prev.Name != next.Name ||
		prev.TriggerRequired != next.TriggerRequired
}

// refreshPrefillTranslations asks the configured Translator for fresh
// translations of the current source-language prefill (after {business}
// substitution and any keyword injection). Returns nil + nil when no
// translator is configured — the caller keeps the existing translations.
func (a *App) refreshPrefillTranslations(ctx context.Context, b store.Business) (map[string]string, error) {
	if a.Translator == nil {
		return nil, nil
	}
	source := resolvePrefill(b)
	if strings.TrimSpace(source) == "" {
		return map[string]string{}, nil
	}
	return a.Translator(ctx, source, supportedPrefillLangs)
}

// handleShareRedirect is the public /go/{id} endpoint. It looks up the
// business, resolves their current prefill text against the latest business
// name + keyword setting, and 302s the visitor at the matching wa.me link.
// 404s are intentional for unknown or unconnected businesses so the URL
// doesn't leak existence of unpaired tenants.
func (a *App) handleShareRedirect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	b, err := a.Store.GetBusiness(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	target := businessShareTarget(b, r.Header.Get("Accept-Language"))
	if target == "" {
		http.NotFound(w, r)
		return
	}
	// Vary on Accept-Language so any cache (CDN, browser) treats different
	// browser locales as separate responses.
	w.Header().Set("Vary", "Accept-Language")
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusFound)
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

// historyMaxMessages caps the read-only conversation viewer. Real chats run
// to thousands of messages over time; this keeps the page light and the SSR
// quick. Future pagination would lift this number — for now it's a single
// big window so the viewer matches the "Full history" expectation in the
// refining notes.
const historyMaxMessages = 2000

type historyMessage struct {
	Direction string
	Kind      string
	Body      string
	Time      time.Time
}

type historyView struct {
	Business    store.Business
	CustomerJID string
	Messages    []historyMessage
	Total       int
	Truncated   bool
}

// handleDashboardConversation renders the full read-only message history for
// one conversation. The conversation id comes from the URL path; we verify
// it belongs to the calling user's business before showing anything.
func (a *App) handleDashboardConversation(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	convoID := r.PathValue("id")
	if convoID == "" {
		http.Error(w, "missing conversation id", http.StatusBadRequest)
		return
	}
	convo, err := a.Store.GetConversation(r.Context(), convoID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "get conversation: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if convo.BusinessID != b.ID {
		http.NotFound(w, r)
		return
	}

	raw, err := a.Store.ListMessages(r.Context(), convo.ID, historyMaxMessages)
	if err != nil {
		http.Error(w, "list messages: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// ListMessages is newest-first; reverse so the viewer reads top-down
	// like a regular WhatsApp chat.
	msgs := make([]historyMessage, 0, len(raw))
	for i := len(raw) - 1; i >= 0; i-- {
		m := raw[i]
		msgs = append(msgs, historyMessage{
			Direction: m.Direction,
			Kind:      m.Kind,
			Body:      m.Body,
			Time:      m.CreatedAt,
		})
	}

	view := historyView{
		Business:    b,
		CustomerJID: convo.CustomerJID,
		Messages:    msgs,
		Total:       len(msgs),
		Truncated:   len(raw) == historyMaxMessages,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dashHistoryTmpl.Execute(w, view); err != nil {
		log.Printf("dashHistoryTmpl: %v", err)
	}
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
	prev := b
	b.TriggerRequired = r.PostForm.Get("required") == "1"
	if prefillInputChanged(prev, b) {
		if newT, err := a.refreshPrefillTranslations(r.Context(), b); err != nil {
			log.Printf("prefill: refresh translations on keyword toggle: %v", err)
		} else if newT != nil {
			b.WAPrefillTranslations = newT
		}
	}
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
		prev := b
		b.Name = strings.TrimSpace(r.PostForm.Get("name"))
		b.Address = strings.TrimSpace(r.PostForm.Get("address"))
		b.Phone = strings.TrimSpace(r.PostForm.Get("phone"))
		b.Website = strings.TrimSpace(r.PostForm.Get("website"))
		b.Hours = strings.TrimSpace(r.PostForm.Get("hours"))
		b.ExtraInfo = strings.TrimSpace(r.PostForm.Get("extra"))
		b.WAPrefillTemplate = strings.TrimSpace(r.PostForm.Get("wa_prefill_template"))
		// Regenerate cached translations when the source-language template,
		// the business name (used in the {business} placeholder) or the
		// keyword setting changes. Failures are non-fatal — we keep the
		// previous translations so a transient LLM hiccup doesn't wipe
		// good data.
		if prefillInputChanged(prev, b) {
			if newT, err := a.refreshPrefillTranslations(r.Context(), b); err != nil {
				log.Printf("prefill: refresh translations: %v", err)
			} else if newT != nil {
				b.WAPrefillTranslations = newT
			}
		}
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

// handleDashboardUnpair drops the WhatsApp link: it tells WhatsApp servers
// to remove the linked device (best-effort — a missing live client is fine),
// deletes the tenant's Chalagente chat history (the spec says the disconnect
// button "deletes your chat history from chalagente.com but won't disturb
// your WhatsApp"), then clears the persisted device JID so the tenant won't
// be auto-reconnected on next boot. The user will need to scan a fresh QR
// to pair again.
func (a *App) handleDashboardUnpair(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	if err := a.WAMgr.Logout(r.Context(), b.ID); err != nil && !errors.Is(err, wamanager.ErrNotRegistered) {
		log.Printf("unpair: logout %s: %v", b.ID, err)
	}
	if err := a.Store.DeleteChatHistory(r.Context(), b.ID); err != nil {
		http.Error(w, "delete history: "+err.Error(), http.StatusInternalServerError)
		return
	}
	a.clearRecent(b.ID)
	b.WADeviceJID = ""
	if err := a.Store.UpdateBusiness(r.Context(), b); err != nil {
		http.Error(w, "save: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/app?flash=WhatsApp+desvinculado+y+historial+borrado", http.StatusSeeOther)
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
	// Encode the /go/<id> redirect, not the bare wa.me link. That way the
	// QR keeps working even if the tenant changes their prefill copy —
	// the redirect handler reads the current business state at click
	// time. It also gives us a single place to plug in per-language
	// matching later.
	link := a.businessShareURL(b)
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
         onsubmit="return confirm('¿Desvincular WhatsApp?\n\nEsto BORRARÁ todo tu historial de chats de chalagente.com. Tu WhatsApp no se toca — los mensajes siguen en tu teléfono.\n\nTendrás que escanear el QR otra vez para volver a conectar.');">
    <button>Desvincular WhatsApp y borrar historial</button>
   </form>
   {{ end }}
  </div>
  <div class="card">
   <h2 style="margin-top:0">Conversaciones recientes</h2>
   {{ if .Conversations }}
    <ul class="convos">
    {{ range .Conversations }}
     <li>
      <a href="/app/conversations/{{ .ID }}" style="display:block;color:inherit;text-decoration:none">
       <span class="when">{{ .UpdatedAt.Format "15:04" }}</span>
       <div class="who">{{ .CustomerJID }}</div>
       <div class="body">{{ if eq .LastDir "out" }}→{{ else }}←{{ end }} {{ .LastBody }}</div>
      </a>
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
   <p style="font-size:.8em;color:#666;margin:.4rem 0 .2rem">Cuando escanean tu QR, el cliente entra a WhatsApp con este mensaje listo para enviar:</p>
   <p style="font-size:.85em;font-style:italic;background:#f5f5f5;border:1px solid #ddd;border-radius:4px;padding:.4rem .6rem;margin:.2rem 0">{{ .PrefillResolved }}</p>
   <p style="font-size:.78em;color:#888;margin:.4rem 0 0">QR apunta a <a href="{{ .ShareURL }}">{{ .ShareURL }}</a></p>
   <p style="font-size:.78em;color:#888;margin:.2rem 0 0">→ <a href="{{ .WAMeURL }}">{{ .WAMeURL }}</a></p>
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

var dashHistoryTmpl = template.Must(template.New("dashHistory").Parse(`<!doctype html><html lang="es"><head><meta charset="utf-8"><title>{{ .CustomerJID }} — Chalagente</title>
<style>
body{font-family:system-ui,sans-serif;max-width:760px;margin:0 auto;padding:1rem;color:#222;line-height:1.5;background:#ece5dd}
.crumbs{font-size:.85em;margin-bottom:.5rem}
.crumbs a{color:#075e54;text-decoration:none}
.crumbs a:hover{text-decoration:underline}
h1{margin:.2rem 0 .8rem;font-size:1.4rem;color:#075e54}
.meta{font-size:.85em;color:#555;margin-bottom:.5rem}
.chat{display:flex;flex-direction:column;gap:.35rem;padding:1rem;background:#ece5dd;border-radius:6px}
.bubble{max-width:80%;padding:.5rem .7rem;border-radius:10px;font-size:.95rem;color:#222;box-shadow:0 1px 1px rgba(0,0,0,.08);word-wrap:break-word}
.bubble.in{background:white;align-self:flex-start;border-bottom-left-radius:2px}
.bubble.out{background:#dcf8c6;align-self:flex-end;border-bottom-right-radius:2px}
.bubble .when{display:block;color:#888;font-size:.7em;margin-top:.2rem}
.bubble .kindbadge{display:inline-block;font-size:.7em;color:#555;background:rgba(0,0,0,.05);border-radius:3px;padding:1px 5px;margin-right:.3rem;text-transform:uppercase;letter-spacing:.05em}
.empty{padding:2rem;text-align:center;color:#666}
.note{color:#555;font-size:.85em;margin-top:.8rem;text-align:center}
</style></head><body>
<div class="crumbs"><a href="/app">← Conversaciones</a></div>
<h1>{{ .CustomerJID }}</h1>
<p class="meta">{{ .Total }} mensaje{{ if ne .Total 1 }}s{{ end }} · solo lectura</p>
<div class="chat">
{{ range .Messages }}
 <div class="bubble {{ .Direction }}">
  {{ if ne .Kind "text" }}<span class="kindbadge">{{ .Kind }}</span>{{ end }}{{ .Body }}
  <span class="when">{{ .Time.Format "2006-01-02 15:04" }}</span>
 </div>
{{ else }}
 <div class="empty">Aún no hay mensajes en este chat.</div>
{{ end }}
</div>
{{ if .Truncated }}<p class="note">Mostrando los últimos {{ .Total }} mensajes — el historial más antiguo está guardado pero no se muestra aquí.</p>{{ end }}
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
 <label>Mensaje prellenado del QR
  <input name="wa_prefill_template" value="{{ .Business.WAPrefillTemplate }}" placeholder="Chalagente, help me at {business}">
 </label>
 <p style="font-size:.82em;color:#666;margin:.2rem 0 .8rem">
  Texto que el cliente ve precargado en WhatsApp al escanear tu QR. Usa <code>{business}</code> para insertar el nombre del negocio. Si la palabra clave «Chalagente» es obligatoria y tu mensaje no la incluye, se añade automáticamente.
 </p>
 <button>Guardar</button>
</form>
</body></html>`))
