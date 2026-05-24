package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestTryPageIsPublic(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/demo")
	if err != nil {
		t.Fatalf("GET /try: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(strings.ToLower(string(body)), "prueba") &&
		!strings.Contains(strings.ToLower(string(body)), "demo") {
		t.Fatalf("/demo should look like a demo page; got: %s", first(string(body), 200))
	}
}

func TestTryPageSetsCookieAndLoadsDefaultBusiness(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	res, err := client.Get(srv.URL + "/demo")
	if err != nil {
		t.Fatalf("GET /try: %v", err)
	}
	res.Body.Close()

	u, _ := url.Parse(srv.URL)
	cookies := jar.Cookies(u)
	var found bool
	for _, c := range cookies {
		if c.Name == tryCookieName {
			found = true
		}
	}
	if !found {
		t.Fatal("expected " + tryCookieName + " cookie to be set")
	}

	// Default business should be loadable via /try/business GET
	res, err = client.Get(srv.URL + "/demo/business")
	if err != nil {
		t.Fatalf("GET /try/business: %v", err)
	}
	defer res.Body.Close()
	var out map[string]string
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["name"] == "" {
		t.Fatalf("expected default business name; got %q", out["name"])
	}
}

func TestTrySendTextUsesDefaultBusinessHours(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if _, err := client.Get(srv.URL + "/demo"); err != nil {
		t.Fatalf("warmup GET: %v", err)
	}

	res, err := client.PostForm(srv.URL+"/demo/send", url.Values{"text": []string{"¿A qué hora abren?"}})
	if err != nil {
		t.Fatalf("POST /try/send: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
	var out struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// MockEngine hours rule should fire and reference the default hours.
	if !strings.Contains(strings.ToLower(out.Reply), "horario") {
		t.Fatalf("expected hours-aware reply; got %q", out.Reply)
	}
}

func TestTryEditBusinessChangesAgentReply(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if _, err := client.Get(srv.URL + "/demo"); err != nil {
		t.Fatalf("warmup GET: %v", err)
	}

	// Update hours
	form := url.Values{
		"name":  []string{"Bar 24 Horas"},
		"hours": []string{"Abierto 24/7"},
	}
	if _, err := client.PostForm(srv.URL+"/demo/business", form); err != nil {
		t.Fatalf("POST /try/business: %v", err)
	}

	// Ask again — should reflect new hours
	res, err := client.PostForm(srv.URL+"/demo/send", url.Values{"text": []string{"¿A qué hora abren?"}})
	if err != nil {
		t.Fatalf("POST /try/send: %v", err)
	}
	defer res.Body.Close()
	var out struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(out.Reply, "24") {
		t.Fatalf("expected reply to use edited hours; got %q", out.Reply)
	}
}

func TestTrySendAudioReturnsTranscriptAndAudioReply(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if _, err := client.Get(srv.URL + "/demo"); err != nil {
		t.Fatalf("warmup: %v", err)
	}

	body, ctype := multipartWithAudio(t, "v.ogg", []byte("fakebytes"))
	res, err := client.Post(srv.URL+"/demo/send", ctype, body)
	if err != nil {
		t.Fatalf("POST audio: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
	var out struct {
		Reply      string `json:"reply"`
		Transcript string `json:"transcript"`
		HasAudio   bool   `json:"has_audio"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Transcript == "" {
		t.Fatal("expected transcript")
	}
	if !out.HasAudio {
		t.Fatal("expected has_audio")
	}
}

func TestTryHistoryAcrossRequests(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if _, err := client.Get(srv.URL + "/demo"); err != nil {
		t.Fatalf("warmup: %v", err)
	}

	for _, msg := range []string{"hola", "qué tal"} {
		if _, err := client.PostForm(srv.URL+"/demo/send", url.Values{"text": []string{msg}}); err != nil {
			t.Fatalf("send %s: %v", msg, err)
		}
	}
	res, err := client.Get(srv.URL + "/demo/history")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	defer res.Body.Close()
	var out struct {
		Messages []struct{ Dir, Body string } `json:"messages"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Messages) != 4 {
		t.Fatalf("messages: got %d, want 4", len(out.Messages))
	}
}

func TestTryResetClearsHistoryButKeepsCookie(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if _, err := client.Get(srv.URL + "/demo"); err != nil {
		t.Fatalf("warmup: %v", err)
	}

	if _, err := client.PostForm(srv.URL+"/demo/send", url.Values{"text": []string{"hola"}}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := client.PostForm(srv.URL+"/demo/reset", url.Values{}); err != nil {
		t.Fatalf("reset: %v", err)
	}
	res, err := client.Get(srv.URL + "/demo/history")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	defer res.Body.Close()
	var out struct {
		Messages []any `json:"messages"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	if len(out.Messages) != 0 {
		t.Fatalf("expected empty history; got %d", len(out.Messages))
	}
}

func TestLandingPointsToTry(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "/demo") {
		t.Fatalf("landing should link to /demo")
	}
}
