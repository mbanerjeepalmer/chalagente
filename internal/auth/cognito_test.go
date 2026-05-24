package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/oauth2"
)

type fakeStore struct {
	mu sync.Mutex

	usersBySub   map[string]string
	usersByEmail map[string]string
	emailsByUser map[string]string
	nextUserID   int

	sessions map[string]sessionEntry

	now func() time.Time

	ensureUserErr    error
	createSessionErr error
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
		usersBySub:   map[string]string{},
		usersByEmail: map[string]string{},
		emailsByUser: map[string]string{},
		sessions:     map[string]sessionEntry{},
		now:          now,
	}
}

func (f *fakeStore) EnsureUserFromCognito(ctx context.Context, sub, email string) (string, error) {
	if f.ensureUserErr != nil {
		return "", f.ensureUserErr
	}
	email = strings.ToLower(strings.TrimSpace(email))
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.usersBySub[sub]; ok {
		return id, nil
	}
	if id, ok := f.usersByEmail[email]; ok {
		f.usersBySub[sub] = id
		return id, nil
	}
	f.nextUserID++
	id := fmt.Sprintf("user-%d", f.nextUserID)
	f.usersBySub[sub] = id
	f.usersByEmail[email] = id
	f.emailsByUser[id] = email
	return id, nil
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

func TestMiddleware_AllowsAuthenticated(t *testing.T) {
	store := newFakeStore(nil)
	h := NewTestCognitoHandlers(store, "http://example.test")

	userID, _ := store.EnsureUserFromCognito(context.Background(), "sub-1", "a@b.com")
	sessID, exp, _ := store.CreateSession(context.Background(), userID, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req.AddCookie(h.sessionCookie(sessID, exp))
	rr := httptest.NewRecorder()
	called := false
	h.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		id, ok := h.UserIDFrom(r.Context())
		if !ok || id != userID {
			t.Errorf("UserIDFrom = %q, ok=%v, want %q", id, ok, userID)
		}
	})).ServeHTTP(rr, req)

	if !called {
		t.Fatal("handler not called")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestMiddleware_RedirectsUnauthenticated(t *testing.T) {
	h := NewTestCognitoHandlers(newFakeStore(nil), "http://example.test")
	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	rr := httptest.NewRecorder()
	h.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	if !strings.HasSuffix(rr.Header().Get("Location"), "/signup") {
		t.Fatalf("Location = %q, want /signup redirect", rr.Header().Get("Location"))
	}
}

func TestMiddleware_ClearsInvalidSession(t *testing.T) {
	h := NewTestCognitoHandlers(newFakeStore(nil), "http://example.test")
	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "bogus"})
	rr := httptest.NewRecorder()
	h.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == CookieName && c.MaxAge != -1 {
			t.Errorf("expected session cookie cleared, got MaxAge=%d", c.MaxAge)
		}
	}
}

func TestLogout_ClearsSession(t *testing.T) {
	store := newFakeStore(nil)
	h := NewTestCognitoHandlers(store, "http://example.test")
	userID, _ := store.EnsureUserFromCognito(context.Background(), "sub-1", "a@b.com")
	sessID, exp, _ := store.CreateSession(context.Background(), userID, time.Hour)

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(h.sessionCookie(sessID, exp))
	rr := httptest.NewRecorder()
	h.Logout(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("Location"), "logout") {
		t.Fatalf("Location = %q, want cognito logout URL", rr.Header().Get("Location"))
	}
	if _, err := store.GetSessionUser(context.Background(), sessID); err == nil {
		t.Fatal("session should be deleted")
	}
}

type mockOIDCServer struct {
	Server   *httptest.Server
	Issuer   string
	Key      *rsa.PrivateKey
	KID      string
	ClientID string
}

func newMockOIDCServer(t *testing.T, clientID string) *mockOIDCServer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	kid := "test-kid"
	m := &mockOIDCServer{Key: key, KID: kid, ClientID: clientID}

	mux := http.NewServeMux()
	m.Server = httptest.NewServer(mux)
	m.Issuer = m.Server.URL

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                m.Issuer,
			"authorization_endpoint":                m.Issuer + "/authorize",
			"token_endpoint":                        m.Issuer + "/token",
			"jwks_uri":                              m.Issuer + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA",
				"kid": kid,
				"alg": "RS256",
				"use": "sig",
				"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}},
		})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		redirectURI := q.Get("redirect_uri")
		state := q.Get("state")
		u, _ := url.Parse(redirectURI)
		q2 := u.Query()
		q2.Set("code", "test-auth-code")
		q2.Set("state", state)
		u.RawQuery = q2.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		idToken := m.signIDToken(t, "user-sub-1", "alice@example.com")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token":  "access-token",
			"token_type":    "Bearer",
			"expires_in":    "3600",
			"id_token":      idToken,
			"refresh_token": "refresh-token",
		})
	})
	return m
}

func (m *mockOIDCServer) signIDToken(t *testing.T, sub, email string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":            sub,
		"email":          email,
		"email_verified": true,
		"iss":            m.Issuer,
		"aud":            m.ClientID,
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = m.KID
	signed, err := token.SignedString(m.Key)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	return signed
}

func (m *mockOIDCServer) Close() { m.Server.Close() }

func newMockCognitoHandlers(t *testing.T, store UserStore, mock *mockOIDCServer, baseURL string) *CognitoHandlers {
	t.Helper()
	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, mock.Issuer)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}
	oauth2Cfg := oauth2.Config{
		ClientID:     mock.ClientID,
		ClientSecret: "test-secret",
		RedirectURL:  strings.TrimRight(baseURL, "/") + "/auth/cognito/callback",
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email"},
	}
	return &CognitoHandlers{
		Store:         store,
		BaseURL:       strings.TrimRight(baseURL, "/"),
		CognitoDomain: "test.auth.example.com",
		OAuth2:        oauth2Cfg,
		Verifier:      provider.Verifier(&oidc.Config{ClientID: mock.ClientID}),
		sessionConfig: sessionConfig{cookieSecure: false},
	}
}

func TestLogin_SetsStateAndRedirects(t *testing.T) {
	mock := newMockOIDCServer(t, "test-client")
	defer mock.Close()
	h := newMockCognitoHandlers(t, newFakeStore(nil), mock, "http://example.test")

	req := httptest.NewRequest(http.MethodGet, "/signup", nil)
	rr := httptest.NewRecorder()
	h.Login(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "/authorize") {
		t.Fatalf("Location = %q, want authorize endpoint", loc)
	}
	var stateCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == stateCookieName {
			stateCookie = c
		}
	}
	if stateCookie == nil || stateCookie.Value == "" {
		t.Fatal("missing oauth state cookie")
	}
}

func TestCallback_ValidCodeCreatesSession(t *testing.T) {
	mock := newMockOIDCServer(t, "test-client")
	defer mock.Close()
	store := newFakeStore(nil)
	h := newMockCognitoHandlers(t, store, mock, "http://example.test")

	state := "fixed-state-for-test"
	req := httptest.NewRequest(http.MethodGet, "/auth/cognito/callback?code=test-auth-code&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state, Path: "/"})
	rr := httptest.NewRecorder()
	h.Callback(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d body=%q, want 303", rr.Code, rr.Body.String())
	}
	if !strings.HasSuffix(rr.Header().Get("Location"), "/onboarding") {
		t.Fatalf("Location = %q, want /onboarding", rr.Header().Get("Location"))
	}
	var sessCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == CookieName {
			sessCookie = c
		}
	}
	if sessCookie == nil || sessCookie.Value == "" {
		t.Fatal("missing session cookie")
	}
	userID, err := store.GetSessionUser(context.Background(), sessCookie.Value)
	if err != nil {
		t.Fatalf("GetSessionUser: %v", err)
	}
	if userID != "user-1" {
		t.Fatalf("userID = %q, want user-1", userID)
	}
}

func TestCallback_InvalidState(t *testing.T) {
	mock := newMockOIDCServer(t, "test-client")
	defer mock.Close()
	h := newMockCognitoHandlers(t, newFakeStore(nil), mock, "http://example.test")

	req := httptest.NewRequest(http.MethodGet, "/auth/cognito/callback?code=x&state=wrong", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "expected", Path: "/"})
	rr := httptest.NewRecorder()
	h.Callback(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}
