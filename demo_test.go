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

// Helper: sign up, verify, save business info — leaves the client logged in
// with a configured business. Returns the http client (with session cookie)
// and the server URL.
func loggedInWithBusiness(t *testing.T) (*httptest.Server, *http.Client, *App) {
	t.Helper()
	a, mail := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	t.Cleanup(srv.Close)
	a.Auth.BaseURL = srv.URL

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	if _, err := client.PostForm(srv.URL+"/signup", url.Values{"email": []string{"demo@example.com"}}); err != nil {
		t.Fatalf("signup: %v", err)
	}
	res, err := client.Get(mail.lastURL())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	res.Body.Close()

	form := url.Values{
		"action":  []string{"save"},
		"name":    []string{"Café Demo"},
		"address": []string{"Av. Reforma 1"},
		"hours":   []string{"Lun-Vie 9-18"},
	}
	if _, err := client.PostForm(srv.URL+"/onboarding/business", form); err != nil {
		t.Fatalf("save business: %v", err)
	}
	return srv, client, a
}

func TestDemoPageRequiresAuth(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a.Mux())
	defer srv.Close()

	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := c.Get(srv.URL + "/app/demo")
	if err != nil {
		t.Fatalf("GET demo: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther && res.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect, got %d", res.StatusCode)
	}
	if !strings.Contains(res.Header.Get("Location"), "/signup") {
		t.Fatalf("expected redirect to /signup, got %s", res.Header.Get("Location"))
	}
}

func TestDemoPageRendersForLoggedInUser(t *testing.T) {
	srv, client, _ := loggedInWithBusiness(t)
	res, err := client.Get(srv.URL + "/app/demo")
	if err != nil {
		t.Fatalf("GET /app/demo: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "demo") && !strings.Contains(string(body), "Demo") {
		t.Fatalf("demo page missing demo content; body: %s", first(string(body), 200))
	}
}

func TestDemoSendTextGetsAgentReply(t *testing.T) {
	srv, client, _ := loggedInWithBusiness(t)

	form := url.Values{"text": []string{"¿A qué hora abren?"}}
	res, err := client.PostForm(srv.URL+"/app/demo/send", form)
	if err != nil {
		t.Fatalf("POST demo/send: %v", err)
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
	if out.Reply == "" {
		t.Fatal("empty reply")
	}
	// MockEngine's hours rule fires on "hora" + "?" — should mention "Lun-Vie 9-18"
	if !strings.Contains(strings.ToLower(out.Reply), "lun") && !strings.Contains(strings.ToLower(out.Reply), "9-18") {
		t.Fatalf("expected hours-aware reply; got %q", out.Reply)
	}
}

func TestDemoSendAudioGetsTranscriptAndReply(t *testing.T) {
	srv, client, _ := loggedInWithBusiness(t)

	// Multipart with an "audio" file. The mock voice provider returns a
	// deterministic transcript; the agent then produces its mock reply.
	body, ctype := multipartWithAudio(t, "audio.ogg", []byte("fakeaudiobytes"))

	res, err := client.Post(srv.URL+"/app/demo/send", ctype, body)
	if err != nil {
		t.Fatalf("POST demo/send audio: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status: %d body: %s", res.StatusCode, readAll(res.Body))
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
		t.Fatal("expected a transcript")
	}
	if out.Reply == "" {
		t.Fatal("expected a reply")
	}
	if !out.HasAudio {
		t.Fatal("expected has_audio=true (TTS attempted)")
	}
}

func TestDemoHistoryPersistsAcrossRequests(t *testing.T) {
	srv, client, _ := loggedInWithBusiness(t)

	// First message
	if _, err := client.PostForm(srv.URL+"/app/demo/send", url.Values{"text": []string{"hola"}}); err != nil {
		t.Fatalf("send 1: %v", err)
	}
	// Second message
	if _, err := client.PostForm(srv.URL+"/app/demo/send", url.Values{"text": []string{"hola otra vez"}}); err != nil {
		t.Fatalf("send 2: %v", err)
	}

	// Fetch history
	res, err := client.Get(srv.URL + "/app/demo/history")
	if err != nil {
		t.Fatalf("GET history: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
	var out struct {
		Messages []struct {
			Dir  string `json:"dir"`
			Body string `json:"body"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 2 user messages + 2 bot replies = 4
	if len(out.Messages) != 4 {
		t.Fatalf("history size: got %d, want 4 (%+v)", len(out.Messages), out.Messages)
	}
}

func TestDemoResetClearsHistory(t *testing.T) {
	srv, client, _ := loggedInWithBusiness(t)

	if _, err := client.PostForm(srv.URL+"/app/demo/send", url.Values{"text": []string{"hola"}}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := client.PostForm(srv.URL+"/app/demo/reset", url.Values{}); err != nil {
		t.Fatalf("reset: %v", err)
	}
	res, err := client.Get(srv.URL + "/app/demo/history")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	defer res.Body.Close()
	var out struct {
		Messages []any `json:"messages"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	if len(out.Messages) != 0 {
		t.Fatalf("expected empty history after reset, got %d", len(out.Messages))
	}
}

func TestDemoAvailableBeforeWhatsAppPairing(t *testing.T) {
	srv, client, a := loggedInWithBusiness(t)

	// Sanity: business has no WADeviceJID.
	u, _ := a.Store.GetUserByEmail(reqCtx(), "demo@example.com")
	b, _ := a.Store.GetBusinessByUserID(reqCtx(), u.ID)
	if b.WADeviceJID != "" {
		t.Fatalf("expected unpaired business, got jid=%s", b.WADeviceJID)
	}

	// Demo still works.
	res, err := client.PostForm(srv.URL+"/app/demo/send", url.Values{"text": []string{"hola"}})
	if err != nil {
		t.Fatalf("demo send: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("demo send status: %d", res.StatusCode)
	}
}
