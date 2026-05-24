package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeStore is an in-memory UserStore for tests.
type fakeStore struct {
	mu sync.Mutex

	usersByEmail map[string]string // email -> userID
	emailsByUser map[string]string // userID -> email
	nextUserID   int

	links map[string]linkEntry // token -> entry

	sessions map[string]sessionEntry // sessionID -> entry

	now func() time.Time

	// hooks to inject errors
	ensureUserErr    error
	createLinkErr    error
	consumeLinkErr   error
	createSessionErr error
}

type linkEntry struct {
	email     string
	expiresAt time.Time
}

type sessionEntry struct {
	userID    string
	expiresAt time.Time
}

func newFakeStore(now func() time.Time) *fakeStore {
	if now == nil {
		now = time.Now
	}
	return &fakeStore{
		usersByEmail: map[string]string{},
		emailsByUser: map[string]string{},
		links:        map[string]linkEntry{},
		sessions:     map[string]sessionEntry{},
		now:          now,
	}
}

func (f *fakeStore) EnsureUser(ctx context.Context, email string) (string, error) {
	if f.ensureUserErr != nil {
		return "", f.ensureUserErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.usersByEmail[email]; ok {
		return id, nil
	}
	f.nextUserID++
	id := fmt.Sprintf("user-%d", f.nextUserID)
	f.usersByEmail[email] = id
	f.emailsByUser[id] = email
	return id, nil
}

func (f *fakeStore) CreateMagicLink(ctx context.Context, email string, ttl time.Duration) (string, error) {
	if f.createLinkErr != nil {
		return "", f.createLinkErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	token := fmt.Sprintf("tok-%d", len(f.links)+1)
	f.links[token] = linkEntry{email: email, expiresAt: f.now().Add(ttl)}
	return token, nil
}

func (f *fakeStore) ConsumeMagicLink(ctx context.Context, token string) (string, error) {
	if f.consumeLinkErr != nil {
		return "", f.consumeLinkErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.links[token]
	if !ok {
		return "", ErrNotFound
	}
	if f.now().After(entry.expiresAt) {
		delete(f.links, token)
		return "", ErrNotFound
	}
	delete(f.links, token)
	return entry.email, nil
}

func (f *fakeStore) CreateSession(ctx context.Context, userID string, ttl time.Duration) (string, time.Time, error) {
	if f.createSessionErr != nil {
		return "", time.Time{}, f.createSessionErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	id := fmt.Sprintf("sess-%d", len(f.sessions)+1)
	exp := f.now().Add(ttl)
	f.sessions[id] = sessionEntry{userID: userID, expiresAt: exp}
	return id, exp, nil
}

func (f *fakeStore) GetSessionUser(ctx context.Context, sessionID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.sessions[sessionID]
	if !ok {
		return "", ErrNotFound
	}
	if f.now().After(entry.expiresAt) {
		return "", ErrNotFound
	}
	return entry.userID, nil
}

func (f *fakeStore) DeleteSession(ctx context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, sessionID)
	return nil
}

// fakeMailer captures outbound messages.
type fakeMailer struct {
	mu    sync.Mutex
	sent  []sentMail
	err   error
}

type sentMail struct {
	email string
	url   string
}

func (m *fakeMailer) SendMagicLink(ctx context.Context, email, urlStr string) error {
	if m.err != nil {
		return m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentMail{email: email, url: urlStr})
	return nil
}

func (m *fakeMailer) snapshot() []sentMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]sentMail, len(m.sent))
	copy(out, m.sent)
	return out
}

func newTestHandlers() (*Handlers, *fakeStore, *fakeMailer) {
	store := newFakeStore(nil)
	mailer := &fakeMailer{}
	h := &Handlers{
		Store:        store,
		Mailer:       mailer,
		BaseURL:      "https://example.test",
		Now:          time.Now,
		CookieSecure: false, // tests use http
	}
	return h, store, mailer
}

func TestSignupForm_GET(t *testing.T) {
	h, _, _ := newTestHandlers()
	req := httptest.NewRequest(http.MethodGet, "/signup", nil)
	rr := httptest.NewRecorder()
	h.SignupForm(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "<form") {
		t.Errorf("expected an HTML form, got %q", body)
	}
	if !strings.Contains(strings.ToLower(body), "email") {
		t.Errorf("expected an email field, got %q", body)
	}
}

func TestSignupSubmit_ValidEmail(t *testing.T) {
	h, store, mailer := newTestHandlers()
	form := url.Values{}
	form.Set("email", "  Foo@Example.COM  ")
	req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.SignupSubmit(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	body := strings.ToLower(rr.Body.String())
	if !strings.Contains(body, "check your email") {
		t.Errorf("expected 'check your email' in body, got %q", body)
	}

	// user should have been ensured with normalized email
	store.mu.Lock()
	if _, ok := store.usersByEmail["foo@example.com"]; !ok {
		store.mu.Unlock()
		t.Errorf("expected user to be created with normalized email; usersByEmail=%v", store.usersByEmail)
	} else {
		store.mu.Unlock()
	}

	// mailer should have received exactly one mail
	sent := mailer.snapshot()
	if len(sent) != 1 {
		t.Fatalf("mailer got %d mails, want 1", len(sent))
	}
	if sent[0].email != "foo@example.com" {
		t.Errorf("mailer email = %q, want foo@example.com", sent[0].email)
	}
	if !strings.HasPrefix(sent[0].url, "https://example.test/auth/verify?token=") {
		t.Errorf("mailer url = %q, expected /auth/verify?token= prefix", sent[0].url)
	}

	// link should exist in the store under the normalized email
	store.mu.Lock()
	if len(store.links) != 1 {
		store.mu.Unlock()
		t.Fatalf("store should have 1 link, got %d", len(store.links))
	}
	for _, entry := range store.links {
		if entry.email != "foo@example.com" {
			t.Errorf("link email = %q, want foo@example.com", entry.email)
		}
	}
	store.mu.Unlock()
}

func TestSignupSubmit_MissingEmail(t *testing.T) {
	h, _, mailer := newTestHandlers()
	form := url.Values{}
	form.Set("email", "")
	req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.SignupSubmit(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if len(mailer.snapshot()) != 0 {
		t.Errorf("mailer should not have been called")
	}
}

func TestSignupSubmit_InvalidEmail(t *testing.T) {
	h, _, mailer := newTestHandlers()
	cases := []string{"foo", "foo@", "@bar.com", "not-an-email", "foo bar@baz.com"}
	for _, em := range cases {
		t.Run(em, func(t *testing.T) {
			form := url.Values{}
			form.Set("email", em)
			req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()
			h.SignupSubmit(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("status for %q = %d, want 400", em, rr.Code)
			}
		})
	}
	if len(mailer.snapshot()) != 0 {
		t.Errorf("mailer should not have been called for invalid emails")
	}
}

func TestVerify_ValidToken(t *testing.T) {
	h, store, mailer := newTestHandlers()

	// Submit signup to seed a token.
	form := url.Values{}
	form.Set("email", "alice@example.com")
	req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.SignupSubmit(httptest.NewRecorder(), req)

	sent := mailer.snapshot()
	if len(sent) != 1 {
		t.Fatalf("expected 1 mail, got %d", len(sent))
	}
	verifyURL := sent[0].url

	req2 := httptest.NewRequest(http.MethodGet, verifyURL, nil)
	rr := httptest.NewRecorder()
	h.Verify(rr, req2)

	if rr.Code != http.StatusSeeOther && rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 303 or 302", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if loc != "/onboarding" {
		t.Errorf("Location = %q, want /onboarding", loc)
	}

	// cookie set
	resp := rr.Result()
	defer resp.Body.Close()
	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == CookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected cookie %q to be set", CookieName)
	}
	if !sessionCookie.HttpOnly {
		t.Errorf("cookie should be HttpOnly")
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie SameSite = %v, want Lax", sessionCookie.SameSite)
	}
	if sessionCookie.Path != "/" {
		t.Errorf("cookie Path = %q, want /", sessionCookie.Path)
	}
	if sessionCookie.MaxAge <= 0 {
		t.Errorf("cookie MaxAge should be > 0, got %d", sessionCookie.MaxAge)
	}
	if sessionCookie.Value == "" {
		t.Errorf("cookie Value should not be empty")
	}

	// session should exist in store
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.sessions[sessionCookie.Value]; !ok {
		t.Errorf("expected session %q to exist in store", sessionCookie.Value)
	}
}

func TestVerify_BadToken(t *testing.T) {
	h, _, _ := newTestHandlers()
	req := httptest.NewRequest(http.MethodGet, "/auth/verify?token=nope", nil)
	rr := httptest.NewRecorder()
	h.Verify(rr, req)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d, want 4xx", rr.Code)
	}
}

func TestVerify_MissingToken(t *testing.T) {
	h, _, _ := newTestHandlers()
	req := httptest.NewRequest(http.MethodGet, "/auth/verify", nil)
	rr := httptest.NewRecorder()
	h.Verify(rr, req)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d, want 4xx", rr.Code)
	}
}

func TestVerify_ExpiredToken(t *testing.T) {
	now := time.Now()
	store := newFakeStore(func() time.Time { return now })
	mailer := &fakeMailer{}
	h := &Handlers{
		Store:   store,
		Mailer:  mailer,
		BaseURL: "https://example.test",
		Now:     func() time.Time { return now },
	}

	form := url.Values{}
	form.Set("email", "alice@example.com")
	req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.SignupSubmit(httptest.NewRecorder(), req)

	// advance time past expiry
	now = now.Add(MagicLinkTTL + time.Second)

	verifyURL := mailer.snapshot()[0].url
	req2 := httptest.NewRequest(http.MethodGet, verifyURL, nil)
	rr := httptest.NewRecorder()
	h.Verify(rr, req2)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d, want 4xx for expired token", rr.Code)
	}
}

func TestMiddleware_NoCookie(t *testing.T) {
	h, _, _ := newTestHandlers()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("next should not be called when no cookie")
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rr := httptest.NewRecorder()
	h.Middleware(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther && rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want redirect", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/signup" {
		t.Errorf("Location = %q, want /signup", loc)
	}
}

func TestMiddleware_ValidCookie(t *testing.T) {
	h, store, _ := newTestHandlers()

	// Seed a user + session.
	userID, err := store.EnsureUser(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	sessID, _, err := store.CreateSession(context.Background(), userID, SessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	var seenUser string
	var seenOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUser, seenOK = h.UserIDFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sessID})
	rr := httptest.NewRecorder()
	h.Middleware(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !seenOK {
		t.Errorf("UserIDFrom returned ok=false")
	}
	if seenUser != userID {
		t.Errorf("user id in context = %q, want %q", seenUser, userID)
	}
}

func TestMiddleware_ExpiredSession(t *testing.T) {
	now := time.Now()
	store := newFakeStore(func() time.Time { return now })
	h := &Handlers{
		Store:   store,
		Mailer:  &fakeMailer{},
		BaseURL: "https://example.test",
		Now:     func() time.Time { return now },
	}
	userID, _ := store.EnsureUser(context.Background(), "alice@example.com")
	sessID, _, _ := store.CreateSession(context.Background(), userID, time.Minute)

	// advance past expiry
	now = now.Add(2 * time.Minute)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("next should not be called with expired session")
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sessID})
	rr := httptest.NewRecorder()
	h.Middleware(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther && rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want redirect", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/signup" {
		t.Errorf("Location = %q, want /signup", loc)
	}
	// cookie should be cleared
	cleared := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == CookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Errorf("expected cleared cookie (MaxAge<0) in response")
	}
}

func TestMiddleware_UnknownSession(t *testing.T) {
	h, _, _ := newTestHandlers()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("next should not be called with unknown session")
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "nonexistent"})
	rr := httptest.NewRecorder()
	h.Middleware(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther && rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want redirect", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/signup" {
		t.Errorf("Location = %q, want /signup", loc)
	}
}

func TestLogout(t *testing.T) {
	h, store, _ := newTestHandlers()
	userID, _ := store.EnsureUser(context.Background(), "alice@example.com")
	sessID, _, _ := store.CreateSession(context.Background(), userID, SessionTTL)

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sessID})
	rr := httptest.NewRecorder()
	h.Logout(rr, req)

	if rr.Code != http.StatusSeeOther && rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want redirect", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want /", loc)
	}

	// cookie cleared
	cleared := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == CookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Errorf("expected cleared cookie (MaxAge<0) in response")
	}

	// session removed
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.sessions[sessID]; ok {
		t.Errorf("session should have been deleted")
	}
}

func TestUserIDFrom_Empty(t *testing.T) {
	h, _, _ := newTestHandlers()
	_, ok := h.UserIDFrom(context.Background())
	if ok {
		t.Errorf("UserIDFrom on empty context should return ok=false")
	}
}

func TestConsoleMailer_LogsURL(t *testing.T) {
	var captured string
	cm := ConsoleMailer{Logf: func(format string, args ...any) {
		captured = fmt.Sprintf(format, args...)
	}}
	if err := cm.SendMagicLink(context.Background(), "x@y.com", "https://z/abc"); err != nil {
		t.Fatalf("SendMagicLink: %v", err)
	}
	if !strings.Contains(captured, "https://z/abc") {
		t.Errorf("expected URL in log, got %q", captured)
	}
	if !strings.Contains(captured, "x@y.com") {
		t.Errorf("expected email in log, got %q", captured)
	}
}

func TestConsoleMailer_NilLogf(t *testing.T) {
	cm := ConsoleMailer{}
	// shouldn't panic
	if err := cm.SendMagicLink(context.Background(), "x@y.com", "https://z/abc"); err != nil {
		t.Errorf("SendMagicLink with nil Logf returned error: %v", err)
	}
}

func TestErrNotFoundIsNotNil(t *testing.T) {
	if ErrNotFound == nil {
		t.Fatalf("ErrNotFound must be non-nil")
	}
	if !errors.Is(ErrNotFound, ErrNotFound) {
		t.Errorf("errors.Is(ErrNotFound, ErrNotFound) must be true")
	}
}

// Sanity check: drain the body of an httptest response so we don't leak.
func drain(t *testing.T, r io.Reader) {
	t.Helper()
	if _, err := io.Copy(io.Discard, r); err != nil {
		t.Errorf("drain: %v", err)
	}
}

// silence unused warnings if/when refactoring tests.
var _ = drain
