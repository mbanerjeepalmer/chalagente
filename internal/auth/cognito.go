package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// CognitoConfig holds AWS Cognito OIDC settings.
type CognitoConfig struct {
	Region       string
	UserPoolID   string
	ClientID     string
	ClientSecret string
	Domain       string // Hosted UI domain, without https://
	BaseURL      string
	CookieSecure bool
}

// CognitoHandlers implements login via Cognito Hosted UI with local sessions.
type CognitoHandlers struct {
	Store        UserStore
	BaseURL      string
	CognitoDomain string
	OAuth2       oauth2.Config
	Verifier     *oidc.IDTokenVerifier
	sessionConfig
}

// NewCognitoHandlers builds handlers wired to a Cognito User Pool.
func NewCognitoHandlers(ctx context.Context, store UserStore, cfg CognitoConfig) (*CognitoHandlers, error) {
	if store == nil {
		return nil, fmt.Errorf("auth: nil store")
	}
	if cfg.Region == "" || cfg.UserPoolID == "" || cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.Domain == "" {
		return nil, fmt.Errorf("auth: incomplete Cognito config")
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("auth: empty BaseURL")
	}

	issuerURL := fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", cfg.Region, cfg.UserPoolID)
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("auth: oidc provider: %w", err)
	}

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  baseURL + "/auth/cognito/callback",
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email"},
	}

	return &CognitoHandlers{
		Store:         store,
		BaseURL:       baseURL,
		CognitoDomain: strings.TrimPrefix(strings.TrimPrefix(cfg.Domain, "https://"), "http://"),
		OAuth2:        oauth2Cfg,
		Verifier:      provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		sessionConfig: sessionConfig{cookieSecure: cfg.CookieSecure},
	}, nil
}

// Login redirects the browser to Cognito Hosted UI.
func (h *CognitoHandlers) Login(w http.ResponseWriter, r *http.Request) {
	state, err := randomState()
	if err != nil {
		http.Error(w, "could not start login", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, h.stateCookie(state))
	url := h.OAuth2.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusFound)
}

// Callback exchanges the authorization code, verifies the ID token, and starts a session.
func (h *CognitoHandlers) Callback(w http.ResponseWriter, r *http.Request) {
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		http.Error(w, "login failed: "+errMsg, http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	state := r.URL.Query().Get("state")
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != state {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, h.clearStateCookie())

	ctx := r.Context()
	rawToken, err := h.OAuth2.Exchange(ctx, code)
	if err != nil {
		http.Error(w, "failed to exchange token", http.StatusInternalServerError)
		return
	}
	rawIDToken, ok := rawToken.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		http.Error(w, "missing id_token", http.StatusInternalServerError)
		return
	}
	idToken, err := h.Verifier.Verify(ctx, rawIDToken)
	if err != nil {
		http.Error(w, "invalid id token", http.StatusBadRequest)
		return
	}

	var claims struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "invalid claims", http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if claims.Sub == "" || email == "" {
		http.Error(w, "missing sub or email", http.StatusBadRequest)
		return
	}

	userID, err := h.Store.EnsureUserFromCognito(ctx, claims.Sub, email)
	if err != nil {
		http.Error(w, "could not look up account", http.StatusInternalServerError)
		return
	}
	if err := startSession(ctx, h.Store, h.sessionConfig, w, userID); err != nil {
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/onboarding", http.StatusSeeOther)
}

// Logout clears the local session and redirects to Cognito logout.
func (h *CognitoHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		_ = h.Store.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, h.clearSessionCookie())

	logoutURI := url.QueryEscape(h.BaseURL + "/")
	logoutURL := fmt.Sprintf("https://%s/logout?client_id=%s&logout_uri=%s",
		h.CognitoDomain, url.QueryEscape(h.OAuth2.ClientID), logoutURI)
	http.Redirect(w, r, logoutURL, http.StatusFound)
}

// Middleware enforces an authenticated session on protected routes.
func (h *CognitoHandlers) Middleware(next http.Handler) http.Handler {
	return sessionMiddleware(h.Store, h.sessionConfig, next)
}

// UserIDFrom extracts the authenticated user ID from the request context.
func (h *CognitoHandlers) UserIDFrom(ctx context.Context) (string, bool) {
	return UserIDFrom(ctx)
}

// NewTestCognitoHandlers builds handlers with stub OAuth settings for tests
// that exercise session middleware without a live Cognito endpoint.
func NewTestCognitoHandlers(store UserStore, baseURL string) *CognitoHandlers {
	return &CognitoHandlers{
		Store:         store,
		BaseURL:       strings.TrimRight(baseURL, "/"),
		CognitoDomain: "test.auth.example.com",
		OAuth2:        oauth2.Config{ClientID: "test-client-id"},
		sessionConfig: sessionConfig{cookieSecure: false},
	}
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
