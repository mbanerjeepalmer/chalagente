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
	"testing"
	"time"

	"github.com/mbanerjeepalmer/chalagente/internal/agent"
	"github.com/mbanerjeepalmer/chalagente/internal/auth"
	"github.com/mbanerjeepalmer/chalagente/internal/maps"
	"github.com/mbanerjeepalmer/chalagente/internal/store"
	"github.com/mbanerjeepalmer/chalagente/internal/voice"
	"github.com/mbanerjeepalmer/chalagente/internal/wamanager"
)

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
	a.Auth = auth.NewTestCognitoHandlers(&storeAuthAdapter{s: s}, a.BaseURL)
	return a
}

func loginTestUser(t *testing.T, a *App, jar *cookiejar.Jar, srvURL, email, cognitoSub string) {
	t.Helper()
	ctx := context.Background()
	u, err := a.Store.EnsureUserFromCognito(ctx, cognitoSub, email)
	if err != nil {
		t.Fatalf("EnsureUserFromCognito: %v", err)
	}
	sess, err := a.Store.CreateSession(ctx, u.ID, 24*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	uParsed, err := url.Parse(srvURL)
	if err != nil {
		t.Fatalf("parse srv URL: %v", err)
	}
	jar.SetCookies(uParsed, []*http.Cookie{{
		Name:     auth.CookieName,
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
	}})
}

func TestLandingPageHasSignupCTA(t *testing.T) {
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
	if !strings.Contains(string(body), "/signup") {
		t.Fatalf("landing missing /signup CTA")
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

func TestUnauthenticatedRedirectsToSignup(t *testing.T) {
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
	if !strings.Contains(loc, "/signup") {
		t.Fatalf("redirect to %q, want /signup", loc)
	}
}

func TestOnboardingReachableAfterLogin(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	loginTestUser(t, a, jar, srv.URL, "hola@example.com", "sub-hola")

	res, err := client.Get(srv.URL + "/onboarding")
	if err != nil {
		t.Fatalf("GET /onboarding: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("onboarding status: %d", res.StatusCode)
	}
	if !strings.Contains(string(body), "Paso 1") {
		t.Fatalf("expected onboarding step 1; body starts: %s", first(string(body), 200))
	}
	if _, err := a.Store.GetUserByEmail(context.Background(), "hola@example.com"); err != nil {
		t.Fatalf("user not created: %v", err)
	}
}

func TestOnboardingBusinessSavesBusiness(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	loginTestUser(t, a, jar, srv.URL, "a@b.com", "sub-ab")

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
	a := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	loginTestUser(t, a, jar, srv.URL, "x@y.com", "sub-xy")

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
