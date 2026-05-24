package main

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mbanerjeepalmer/chalagente/internal/agent"
	"github.com/mbanerjeepalmer/chalagente/internal/auth"
	"github.com/mbanerjeepalmer/chalagente/internal/maps"
	"github.com/mbanerjeepalmer/chalagente/internal/store"
	"github.com/mbanerjeepalmer/chalagente/internal/voice"
	"github.com/mbanerjeepalmer/chalagente/internal/wamanager"
)

type captureMailer struct {
	mu    sync.Mutex
	email string
	url   string
}

func (m *captureMailer) SendMagicLink(_ context.Context, email, link string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.email = email
	m.url = link
	return nil
}

func (m *captureMailer) lastURL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.url
}

func newTestApp(t *testing.T) (*App, *captureMailer) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	mail := &captureMailer{}
	a := newApp()
	a.Store = s
	a.Agent = agent.NewMockEngine()
	a.Voice = &voice.MockProvider{}
	a.Maps = maps.DefaultMockClient()
	a.BaseURL = "http://127.0.0.1"
	a.WAMgr = wamanager.New(nil, nil)
	a.Auth = &auth.Handlers{
		Store:        &storeAuthAdapter{s: s},
		Mailer:       mail,
		BaseURL:      a.BaseURL,
		CookieSecure: false,
	}
	return a, mail
}

func TestLandingPageHasSignupCTA(t *testing.T) {
	a, _ := newTestApp(t)
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
	if !strings.Contains(string(body), "/signup") {
		t.Fatalf("landing missing /signup CTA")
	}
}

func TestHealthCheck(t *testing.T) {
	a, _ := newTestApp(t)
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

func TestUnauthenticatedRedirectsToSignup(t *testing.T) {
	a, _ := newTestApp(t)
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
	if !strings.Contains(loc, "/signup") {
		t.Fatalf("redirect to %q, want /signup", loc)
	}
}

func TestSignupFlowEndToEnd(t *testing.T) {
	a, mail := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()
	a.Auth.BaseURL = srv.URL

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	res, err := client.PostForm(srv.URL+"/signup", url.Values{"email": []string{"hola@example.com"}})
	if err != nil {
		t.Fatalf("POST /signup: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("signup status: %d", res.StatusCode)
	}

	link := mail.lastURL()
	if link == "" {
		t.Fatal("mailer did not receive a link")
	}

	res, err = client.Get(link)
	if err != nil {
		t.Fatalf("verify GET: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("verify final status: %d (last URL %s)", res.StatusCode, res.Request.URL)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "Paso 1") {
		t.Fatalf("expected onboarding step 1; final URL %s; body starts: %s",
			res.Request.URL, first(string(body), 200))
	}

	u, _ := url.Parse(srv.URL)
	cookies := jar.Cookies(u)
	var hasSess bool
	for _, c := range cookies {
		if c.Name == auth.CookieName {
			hasSess = true
		}
	}
	if !hasSess {
		t.Fatal("missing session cookie after verify")
	}

	// Sanity: the user actually got created in the store.
	if _, err := a.Store.GetUserByEmail(context.Background(), "hola@example.com"); err != nil {
		t.Fatalf("user not created: %v", err)
	}
}

func TestOnboardingBusinessSavesBusiness(t *testing.T) {
	a, mail := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()
	a.Auth.BaseURL = srv.URL

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	if _, err := client.PostForm(srv.URL+"/signup", url.Values{"email": []string{"a@b.com"}}); err != nil {
		t.Fatalf("signup: %v", err)
	}
	res, err := client.Get(mail.lastURL())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	res.Body.Close()

	form := url.Values{
		"action":  []string{"save"},
		"name":    []string{"Café Pruebas"},
		"address": []string{"Calle Falsa 123"},
		"phone":   []string{"+52 55 1234 5678"},
		"hours":   []string{"Lun-Vie 9-18"},
	}
	res, err = client.PostForm(srv.URL+"/onboarding/business", form)
	if err != nil {
		t.Fatalf("POST business: %v", err)
	}
	res.Body.Close()

	u, err := a.Store.GetUserByEmail(context.Background(), "a@b.com")
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
	a, mail := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()
	a.Auth.BaseURL = srv.URL

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	if _, err := client.PostForm(srv.URL+"/signup", url.Values{"email": []string{"x@y.com"}}); err != nil {
		t.Fatalf("signup: %v", err)
	}
	res, err := client.Get(mail.lastURL())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	res.Body.Close()

	form := url.Values{"action": []string{"search"}, "q": []string{"taqueria"}}
	res, err = client.PostForm(srv.URL+"/onboarding/business", form)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(strings.ToLower(string(body)), "taqu") {
		t.Fatalf("expected taqueria results; body: %s", first(string(body), 400))
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
