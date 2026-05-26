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

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/mbanerjeepalmer/chalagente/internal/agent"
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

	res, err := client.PostForm(srv.URL+"/app/whatsapp/unpair", url.Values{})
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
	res, err := client.Get(srv.URL + "/app/conversations/" + convo.ID)
	if err != nil {
		t.Fatalf("GET history: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	for _, want := range []string{"¿abren hoy?", "hasta las 22h.", "audio", "solo lectura"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("history missing %q in body: %s", want, first(string(body), 600))
		}
	}

	// Sanity: another user's convo should 404 even if they guess the id.
	jar2, _ := cookiejar.New(nil)
	signInAs(t, a, jar2, srv, "clerk-other")
	other := &http.Client{Jar: jar2}
	res2, err := other.Get(srv.URL + "/app/conversations/" + convo.ID)
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

	for _, path := range []string{"/app", "/onboarding/test"} {
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
