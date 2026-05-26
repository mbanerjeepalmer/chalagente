package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/mbanerjeepalmer/chalagente/internal/agent"

	"go.mau.fi/whatsmeow"
	"github.com/mbanerjeepalmer/chalagente/internal/clerkauth"
	"github.com/mbanerjeepalmer/chalagente/internal/maps"
	"github.com/mbanerjeepalmer/chalagente/internal/store"
	"github.com/mbanerjeepalmer/chalagente/internal/voice"
	"github.com/mbanerjeepalmer/chalagente/internal/wamanager"
)

// testClerkAuth builds a clerkauth.Handlers wired to the given store with a
// stubbed Verify hook that accepts any non-empty token and treats it as the
// Clerk subject. Combined with the test Resolver below, this lets tests
// pose as any Clerk user just by setting the session cookie to a chosen
// clerkID value.
func testClerkAuth(s *store.Store) *clerkauth.Handlers {
	h := &clerkauth.Handlers{
		Store:        &storeClerkAdapter{s: s},
		Resolver:     &testResolver{},
		CookieSecure: false,
		Verify: func(_ context.Context, token string) (*clerk.SessionClaims, error) {
			if token == "" {
				return nil, errors.New("no token")
			}
			c := &clerk.SessionClaims{}
			c.Subject = token
			return c, nil
		},
	}
	h.Init()
	return h
}

// testResolver maps clerkID → "<clerkID>@example.com" so each test subject
// produces a deterministic email.
type testResolver struct{}

func (testResolver) Email(_ context.Context, clerkID string) (string, error) {
	return clerkID + "@example.com", nil
}

// signInAs gives client a session cookie for the supplied Clerk subject and
// also pre-creates the local user, returning the local user ID. The cookie
// is set on srv.URL so the test's http.Client picks it up automatically.
func signInAs(t *testing.T, a *App, jar http.CookieJar, srv *httptest.Server, clerkID string) string {
	t.Helper()
	u, err := a.Store.EnsureUserByClerk(context.Background(), clerkID, clerkID+"@example.com")
	if err != nil {
		t.Fatalf("EnsureUserByClerk: %v", err)
	}
	srvURL, _ := url.Parse(srv.URL)
	jar.SetCookies(srvURL, []*http.Cookie{{
		Name:  clerkauth.SessionCookieName,
		Value: clerkID,
		Path:  "/",
	}})
	return u.ID
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	a := newApp()
	a.Store = s
	a.Agent = agent.NewMockEngine()
	a.Voice = &voice.MockProvider{}
	a.Maps = maps.DefaultMockClient()
	a.BaseURL = "http://127.0.0.1"
	a.WAMgr = wamanager.New(nil, nil)
	a.ClerkAuth = testClerkAuth(s)
	return a
}

func TestLandingPageHasSignInCTA(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
	if !strings.Contains(string(body), "/sign-in") {
		t.Fatalf("landing missing /sign-in CTA")
	}
}

func TestHealthCheck(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
}

func TestUnauthenticatedRedirectsToSignIn(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	res, err := client.Get(srv.URL + "/onboarding")
	if err != nil {
		t.Fatalf("GET /onboarding: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther && res.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect, got %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.Contains(loc, "/sign-in") {
		t.Fatalf("redirect to %q, want /sign-in", loc)
	}
}

func TestSignedInUserCanReachOnboarding(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	signInAs(t, a, jar, srv, "clerk-onboarding")

	client := &http.Client{Jar: jar}
	res, err := client.Get(srv.URL + "/onboarding")
	if err != nil {
		t.Fatalf("GET /onboarding: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("onboarding status: %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "Paso 1") {
		t.Fatalf("expected onboarding step 1; body starts: %s", first(string(body), 200))
	}
	if _, err := a.Store.GetUserByEmail(context.Background(), "clerk-onboarding@example.com"); err != nil {
		t.Fatalf("user not created: %v", err)
	}
}

func TestOnboardingBusinessSavesBusiness(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	signInAs(t, a, jar, srv, "clerk-biz")

	client := &http.Client{Jar: jar}
	form := url.Values{
		"action":  []string{"save"},
		"name":    []string{"Café Pruebas"},
		"address": []string{"Calle Falsa 123"},
		"phone":   []string{"+52 55 1234 5678"},
		"hours":   []string{"Lun-Vie 9-18"},
	}
	res, err := client.PostForm(srv.URL+"/onboarding/business", form)
	if err != nil {
		t.Fatalf("POST business: %v", err)
	}
	res.Body.Close()

	u, err := a.Store.GetUserByEmail(context.Background(), "clerk-biz@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	biz, err := a.Store.GetBusinessByUserID(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("GetBusinessByUserID: %v", err)
	}
	if biz.Name != "Café Pruebas" {
		t.Fatalf("name: %q", biz.Name)
	}
	if biz.Address != "Calle Falsa 123" {
		t.Fatalf("address: %q", biz.Address)
	}
}

func TestMapsSearchInOnboarding(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	signInAs(t, a, jar, srv, "clerk-maps")

	client := &http.Client{Jar: jar}
	form := url.Values{"action": []string{"search"}, "q": []string{"taqueria"}}
	res, err := client.PostForm(srv.URL+"/onboarding/business", form)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(strings.ToLower(string(body)), "taqu") {
		t.Fatalf("expected taqueria results; body: %s", first(string(body), 400))
	}
}

func TestUnpairWhatsAppClearsDeviceJID(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	signInAs(t, a, jar, srv, "clerk-unpair")

	client := &http.Client{Jar: jar}

	u, err := a.Store.GetUserByEmail(context.Background(), "clerk-unpair@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	biz, err := a.Store.CreateBusiness(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("CreateBusiness: %v", err)
	}
	biz.Name = "Café X"
	biz.WADeviceJID = "447700900123:1@s.whatsapp.net"
	if err := a.Store.UpdateBusiness(context.Background(), biz); err != nil {
		t.Fatalf("UpdateBusiness: %v", err)
	}

	res, err := client.PostForm(srv.URL+"/admin/whatsapp/unpair", url.Values{})
	if err != nil {
		t.Fatalf("POST unpair: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != http.StatusSeeOther && res.StatusCode != http.StatusFound {
		t.Fatalf("unpair status: %d", res.StatusCode)
	}

	after, err := a.Store.GetBusinessByUserID(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("GetBusinessByUserID after: %v", err)
	}
	if after.WADeviceJID != "" {
		t.Fatalf("WADeviceJID not cleared: %q", after.WADeviceJID)
	}
}

func TestShareRedirectAndPrefill(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()
	a.BaseURL = srv.URL

	jar, _ := cookiejar.New(nil)
	signInAs(t, a, jar, srv, "clerk-share")

	u, err := a.Store.GetUserByEmail(context.Background(), "clerk-share@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	biz, err := a.Store.CreateBusiness(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("CreateBusiness: %v", err)
	}
	biz.Name = "Birrias El Chalán"
	biz.WADeviceJID = "5215512345678:1@s.whatsapp.net"
	if err := a.Store.UpdateBusiness(context.Background(), biz); err != nil {
		t.Fatalf("UpdateBusiness: %v", err)
	}

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	res, err := client.Get(srv.URL + "/go/" + biz.ID)
	if err != nil {
		t.Fatalf("GET /go/{id}: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("redirect status: got %d want 302", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.HasPrefix(loc, "https://wa.me/5215512345678?text=") {
		t.Fatalf("redirect target: %q", loc)
	}
	// Default prefill should include the business name AND the trigger keyword
	// (TriggerRequired defaults to true).
	decoded, _ := url.QueryUnescape(loc[strings.Index(loc, "?text=")+len("?text="):])
	if !strings.Contains(strings.ToLower(decoded), "chalagente") {
		t.Errorf("default prefill missing keyword: %q", decoded)
	}
	if !strings.Contains(decoded, "Birrias El Chalán") {
		t.Errorf("default prefill missing business name: %q", decoded)
	}

	// Custom prefill with placeholder.
	biz.WAPrefillTemplate = "Hola {business}, ¿me ayudas con un pedido?"
	if err := a.Store.UpdateBusiness(context.Background(), biz); err != nil {
		t.Fatalf("UpdateBusiness 2: %v", err)
	}
	res2, err := client.Get(srv.URL + "/go/" + biz.ID)
	if err != nil {
		t.Fatalf("GET /go/{id} 2: %v", err)
	}
	res2.Body.Close()
	loc2 := res2.Header.Get("Location")
	decoded2, _ := url.QueryUnescape(loc2[strings.Index(loc2, "?text=")+len("?text="):])
	if !strings.Contains(decoded2, "Hola Birrias El Chalán") {
		t.Errorf("custom prefill not expanded: %q", decoded2)
	}
	// Keyword should still be injected because TriggerRequired is still true
	// and the custom copy lacks 'Chalagente'.
	if !strings.Contains(strings.ToLower(decoded2), "chalagente") {
		t.Errorf("keyword injection missing for gated tenant: %q", decoded2)
	}

	// Turn off gating; keyword injection should stop.
	biz.TriggerRequired = false
	if err := a.Store.UpdateBusiness(context.Background(), biz); err != nil {
		t.Fatalf("UpdateBusiness 3: %v", err)
	}
	res3, err := client.Get(srv.URL + "/go/" + biz.ID)
	if err != nil {
		t.Fatalf("GET /go/{id} 3: %v", err)
	}
	res3.Body.Close()
	loc3 := res3.Header.Get("Location")
	decoded3, _ := url.QueryUnescape(loc3[strings.Index(loc3, "?text=")+len("?text="):])
	if strings.Contains(strings.ToLower(decoded3), "chalagente") {
		t.Errorf("keyword injected when gating off: %q", decoded3)
	}

	// Unknown id should 404.
	resNF, err := client.Get(srv.URL + "/go/no-such-business")
	if err != nil {
		t.Fatalf("GET unknown: %v", err)
	}
	resNF.Body.Close()
	if resNF.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown id: got %d want 404", resNF.StatusCode)
	}
}

func TestShareRedirectPicksTranslationFromAcceptLanguage(t *testing.T) {
	a := newTestApp(t)
	// Inject a deterministic translator so we don't hit the LLM.
	a.Translator = func(_ context.Context, source string, langs []string) (map[string]string, error) {
		out := map[string]string{}
		for _, l := range langs {
			out[l] = "[" + l + "] " + source
		}
		return out, nil
	}
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()
	a.BaseURL = srv.URL

	jar, _ := cookiejar.New(nil)
	signInAs(t, a, jar, srv, "clerk-i18n")
	u, _ := a.Store.GetUserByEmail(context.Background(), "clerk-i18n@example.com")
	biz, _ := a.Store.CreateBusiness(context.Background(), u.ID)
	biz.Name = "Café del Sol"
	biz.WADeviceJID = "5215555555555:1@s.whatsapp.net"
	biz.TriggerRequired = false
	if err := a.Store.UpdateBusiness(context.Background(), biz); err != nil {
		t.Fatalf("UpdateBusiness: %v", err)
	}

	// Drive translations via the normal save path: POST /app/business with a
	// changed wa_prefill_template triggers refreshPrefillTranslations.
	client := &http.Client{Jar: jar}
	form := url.Values{
		"name":                []string{"Café del Sol"},
		"address":             []string{""},
		"phone":               []string{""},
		"website":             []string{""},
		"hours":               []string{""},
		"extra":               []string{""},
		"wa_prefill_template": []string{"Hi, help me at {business}"},
	}
	res, err := client.PostForm(srv.URL+"/admin/business", form)
	if err != nil {
		t.Fatalf("POST business: %v", err)
	}
	res.Body.Close()

	stored, err := a.Store.GetBusinessByUserID(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("GetBusinessByUserID: %v", err)
	}
	if got := stored.WAPrefillTranslations["es"]; got == "" {
		t.Fatalf("expected es translation stored; got %v", stored.WAPrefillTranslations)
	}

	noRedirect := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	hit := func(acceptLang string) string {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/go/"+biz.ID, nil)
		if acceptLang != "" {
			req.Header.Set("Accept-Language", acceptLang)
		}
		res, err := noRedirect.Do(req)
		if err != nil {
			t.Fatalf("GET /go: %v", err)
		}
		res.Body.Close()
		loc := res.Header.Get("Location")
		text := loc[strings.Index(loc, "?text=")+len("?text="):]
		decoded, _ := url.QueryUnescape(text)
		return decoded
	}

	// pt-BR maps to pt by primary subtag.
	if got := hit("pt-BR,en;q=0.8"); !strings.HasPrefix(got, "[pt] Hi, help me at") {
		t.Errorf("pt-BR should serve pt translation, got: %q", got)
	}
	// es is in the stored set.
	if got := hit("es-MX,es;q=0.9"); !strings.HasPrefix(got, "[es] Hi, help me at") {
		t.Errorf("es-MX should serve es translation, got: %q", got)
	}
	// Unknown language → fall back to source template.
	if got := hit("xx-YY"); !strings.Contains(got, "Hi, help me at Café del Sol") || strings.HasPrefix(got, "[") {
		t.Errorf("unknown lang should fall back to source, got: %q", got)
	}
	// Higher-q wins.
	if got := hit("en;q=0.1,fr;q=0.9"); !strings.HasPrefix(got, "[fr]") {
		t.Errorf("higher-q fr should win over en, got: %q", got)
	}
}

func TestPerConversationAgentToggle(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	signInAs(t, a, jar, srv, "clerk-pcat")
	u, _ := a.Store.GetUserByEmail(context.Background(), "clerk-pcat@example.com")
	biz, err := a.Store.CreateBusiness(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("CreateBusiness: %v", err)
	}
	convo, err := a.Store.UpsertConversation(context.Background(), biz.ID, "5215512345678@s.whatsapp.net")
	if err != nil {
		t.Fatalf("UpsertConversation: %v", err)
	}
	if !convo.AgentEnabled {
		t.Fatalf("expected AgentEnabled=true on fresh conversation, got false")
	}

	client := &http.Client{Jar: jar}

	// Disable.
	res, err := client.PostForm(srv.URL+"/admin/conversations/"+convo.ID+"/agent",
		url.Values{"enabled": []string{"0"}})
	if err != nil {
		t.Fatalf("POST disable: %v", err)
	}
	res.Body.Close()
	after, err := a.Store.GetConversation(context.Background(), convo.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if after.AgentEnabled {
		t.Fatalf("expected AgentEnabled=false after disable")
	}

	// Viewer should report 'en pausa' state.
	resView, err := client.Get(srv.URL + "/admin/conversations/" + convo.ID)
	if err != nil {
		t.Fatalf("GET viewer: %v", err)
	}
	body, _ := io.ReadAll(resView.Body)
	resView.Body.Close()
	if !strings.Contains(string(body), "en pausa") {
		t.Errorf("viewer missing en-pausa state: %s", first(string(body), 600))
	}
	if !strings.Contains(string(body), "Reanudar agente") {
		t.Errorf("viewer missing reanudar button: %s", first(string(body), 600))
	}

	// Cross-tenant write should 404.
	jar2, _ := cookiejar.New(nil)
	signInAs(t, a, jar2, srv, "clerk-pcat-other")
	other := &http.Client{Jar: jar2}
	resX, err := other.PostForm(srv.URL+"/admin/conversations/"+convo.ID+"/agent",
		url.Values{"enabled": []string{"1"}})
	if err != nil {
		t.Fatalf("POST cross-tenant: %v", err)
	}
	resX.Body.Close()
	if resX.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-tenant status: got %d want 404", resX.StatusCode)
	}
	stillOff, err := a.Store.GetConversation(context.Background(), convo.ID)
	if err != nil {
		t.Fatalf("GetConversation post-cross: %v", err)
	}
	if stillOff.AgentEnabled {
		t.Fatal("cross-tenant write should not have flipped the gate")
	}
}

func TestAdminConnectionScreen(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	signInAs(t, a, jar, srv, "clerk-conn")
	u, _ := a.Store.GetUserByEmail(context.Background(), "clerk-conn@example.com")
	biz, err := a.Store.CreateBusiness(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("CreateBusiness: %v", err)
	}
	biz.Name = "Café Conexión"
	biz.WADeviceJID = "5215512345678:1@s.whatsapp.net"
	if err := a.Store.UpdateBusiness(context.Background(), biz); err != nil {
		t.Fatalf("UpdateBusiness: %v", err)
	}

	client := &http.Client{Jar: jar}
	res, err := client.Get(srv.URL + "/admin/connection")
	if err != nil {
		t.Fatalf("GET connection: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	for _, want := range []string{
		"Conexión de WhatsApp",
		"Descargar PNG",
		"window.print()",
		"Ayuda",
		"Help",
		"帮助",
		"सहायता",
		"/admin/qr.png",
		"/admin/whatsapp/unpair",
		"/admin/trigger",
		"Café Conexión",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("connection screen missing %q in body: %s", want, first(string(body), 600))
		}
	}
}

func TestConnectionScreenHintAfterOnboarding(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	signInAs(t, a, jar, srv, "clerk-hint")
	u, _ := a.Store.GetUserByEmail(context.Background(), "clerk-hint@example.com")
	biz, err := a.Store.CreateBusiness(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("CreateBusiness: %v", err)
	}
	// Just paired, but business name still empty — the "Cuéntale de tu
	// negocio" tooltip should auto-fire without an explicit ?hint param.
	biz.WADeviceJID = "5215512345678:1@s.whatsapp.net"
	if err := a.Store.UpdateBusiness(context.Background(), biz); err != nil {
		t.Fatalf("UpdateBusiness: %v", err)
	}

	client := &http.Client{Jar: jar}
	res, err := client.Get(srv.URL + "/admin/connection")
	if err != nil {
		t.Fatalf("GET connection: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	for _, want := range []string{`id="hint-biz"`, "cuéntale", `id="biz-tab"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("hint markup missing %q: %s", want, first(string(body), 600))
		}
	}

	// Once the business name is set the auto-hint should stop firing.
	biz.Name = "Café Listo"
	if err := a.Store.UpdateBusiness(context.Background(), biz); err != nil {
		t.Fatalf("UpdateBusiness 2: %v", err)
	}
	res2, err := client.Get(srv.URL + "/admin/connection")
	if err != nil {
		t.Fatalf("GET connection 2: %v", err)
	}
	defer res2.Body.Close()
	body2, _ := io.ReadAll(res2.Body)
	if strings.Contains(string(body2), `id="hint-biz"`) {
		t.Errorf("hint shouldn't fire once name is set; body: %s", first(string(body2), 600))
	}
}

func TestOnboardingFinishLandsOnAdminConnection(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	signInAs(t, a, jar, srv, "clerk-fin")

	client := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	res, err := client.PostForm(srv.URL+"/onboarding/finish", url.Values{})
	if err != nil {
		t.Fatalf("POST finish: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther && res.StatusCode != http.StatusFound {
		t.Fatalf("status: %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.HasPrefix(loc, "/admin/connection") {
		t.Errorf("expected /admin/connection redirect, got %q", loc)
	}
	if !strings.Contains(loc, "hint=biz") {
		t.Errorf("expected hint=biz in redirect target, got %q", loc)
	}
}

func TestDemoPresetVoiceNote(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/demo/preset.ogg")
	if err != nil {
		t.Fatalf("GET preset: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "audio/") {
		t.Errorf("content-type %q, want audio/*", ct)
	}
	body, _ := io.ReadAll(res.Body)
	if len(body) == 0 {
		t.Fatal("preset returned no bytes")
	}

	// The demo's HTML should expose the preset button so the visitor can fire it.
	pageRes, err := http.Get(srv.URL + "/demo")
	if err != nil {
		t.Fatalf("GET demo: %v", err)
	}
	pageBody, _ := io.ReadAll(pageRes.Body)
	pageRes.Body.Close()
	for _, want := range []string{"presetBtn", "sendPresetVoice", "/demo/preset.ogg"} {
		if !strings.Contains(string(pageBody), want) {
			t.Errorf("demo page missing %q: %s", want, first(string(pageBody), 600))
		}
	}
}

func TestPairingQRRefreshCap(t *testing.T) {
	a := newTestApp(t)
	_, cancel := context.WithCancel(context.Background())
	a.setPairSession("biz-cap", &pairSession{cancel: cancel})

	ch := make(chan whatsmeow.QRChannelItem, 8)
	// Send pairQRMaxAuto+1 codes — the cap should kick in on the 4th.
	for i := 0; i < pairQRMaxAuto+1; i++ {
		ch <- whatsmeow.QRChannelItem{Event: "code", Code: "qr-" + first(string(rune('a'+i)), 1)}
	}
	close(ch)

	done := make(chan struct{})
	go func() { a.drivePairing("biz-cap", ch); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("drivePairing didn't exit after cap")
	}

	sess := a.getPairSession("biz-cap")
	if sess == nil {
		t.Fatal("pair session went missing")
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if !sess.needsManual {
		t.Errorf("expected needsManual=true after %d codes, got false (codeCount=%d)", pairQRMaxAuto+1, sess.codeCount)
	}
	if sess.event != "needs_manual" {
		t.Errorf("event = %q, want needs_manual", sess.event)
	}
}

func TestLegacyAppPathsRedirectToAdmin(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	cases := []struct {
		from, to string
	}{
		{"/app", "/admin"},
		{"/app/business", "/admin/business"},
		{"/app/conversations/abc", "/admin/conversations/abc"},
		{"/app/qr.png?foo=1", "/admin/qr.png?foo=1"},
	}
	for _, tc := range cases {
		res, err := client.Get(srv.URL + tc.from)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.from, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusPermanentRedirect {
			t.Errorf("%s: status got %d want 308", tc.from, res.StatusCode)
		}
		if got := res.Header.Get("Location"); got != tc.to {
			t.Errorf("%s: Location got %q want %q", tc.from, got, tc.to)
		}
	}
}

func TestConversationHistoryViewer(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	signInAs(t, a, jar, srv, "clerk-hist")

	u, err := a.Store.GetUserByEmail(context.Background(), "clerk-hist@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	biz, err := a.Store.CreateBusiness(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("CreateBusiness: %v", err)
	}
	convo, err := a.Store.UpsertConversation(context.Background(), biz.ID, "5215512345678@s.whatsapp.net")
	if err != nil {
		t.Fatalf("UpsertConversation: %v", err)
	}
	for _, m := range []store.Message{
		{Direction: "in", Kind: "text", Body: "Hola, ¿abren hoy?"},
		{Direction: "out", Kind: "text", Body: "Sí, hasta las 22h."},
		{Direction: "in", Kind: "audio", Body: ""},
	} {
		if err := a.Store.AppendMessage(context.Background(), convo.ID, m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	client := &http.Client{Jar: jar}
	res, err := client.Get(srv.URL + "/admin/conversations/" + convo.ID)
	if err != nil {
		t.Fatalf("GET history: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	for _, want := range []string{"¿abren hoy?", "hasta las 22h.", "audio", "solo lectura", `id="flipBtn"`, "Ver como cliente", "from-business"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("history missing %q in body: %s", want, first(string(body), 600))
		}
	}

	// Sanity: another user's convo should 404 even if they guess the id.
	jar2, _ := cookiejar.New(nil)
	signInAs(t, a, jar2, srv, "clerk-other")
	other := &http.Client{Jar: jar2}
	res2, err := other.Get(srv.URL + "/admin/conversations/" + convo.ID)
	if err != nil {
		t.Fatalf("GET as other: %v", err)
	}
	res2.Body.Close()
	if res2.StatusCode != http.StatusNotFound {
		t.Fatalf("other-user access status: got %d, want 404", res2.StatusCode)
	}
}

func TestTriggerKeywordExplained(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	// Landing page — public, no auth.
	{
		res, err := http.Get(srv.URL + "/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if !strings.Contains(string(body), "«Chalagente»") {
			t.Fatalf("landing missing trigger explanation; body: %s", first(string(body), 400))
		}
	}

	jar, _ := cookiejar.New(nil)
	signInAs(t, a, jar, srv, "clerk-trig")
	client := &http.Client{Jar: jar}

	u, err := a.Store.GetUserByEmail(context.Background(), "clerk-trig@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	biz, err := a.Store.CreateBusiness(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("CreateBusiness: %v", err)
	}
	biz.Name = "Café Trigger"
	biz.WADeviceJID = "5215512345678:1@s.whatsapp.net"
	if err := a.Store.UpdateBusiness(context.Background(), biz); err != nil {
		t.Fatalf("UpdateBusiness: %v", err)
	}

	for _, path := range []string{"/admin", "/onboarding/test"} {
		res, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if !strings.Contains(string(body), "«Chalagente»") {
			t.Fatalf("%s missing trigger explanation; body: %s", path, first(string(body), 400))
		}
	}
}

func first(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func reqCtx() context.Context { return context.Background() }

func readAll(r io.Reader) string {
	b, _ := io.ReadAll(r)
	return string(b)
}

func multipartWithAudio(t *testing.T, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("audio", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	mw.Close()
	return body, mw.FormDataContentType()
}
