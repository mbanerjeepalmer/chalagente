// Package auth provides email magic-link authentication: signup form,
// magic-link verification, session cookies, and route middleware.
//
// The package defines a small UserStore interface so it can be unit-tested
// without a real database, and so the concrete storage layer (e.g.
// internal/store) is not imported here. The integration layer is expected to
// pass a *store.Store (or any other implementation) into Handlers.Store.
package auth

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CookieName is the name of the session cookie set on successful magic-link
// verification.
const CookieName = "chala_session"

// SessionTTL is how long a session cookie stays valid.
const SessionTTL = 30 * 24 * time.Hour

// MagicLinkTTL is how long a magic-link token stays valid.
const MagicLinkTTL = 15 * time.Minute

// ErrNotFound is returned by UserStore implementations when a record (user,
// magic-link token, session) doesn't exist or has expired. It's defined here
// so this package has no dependency on internal/store.
var ErrNotFound = errors.New("auth: not found")

// UserStore is the storage contract this package needs. Implementations are
// expected to be safe for concurrent use.
type UserStore interface {
	// EnsureUser returns the user ID for email, creating the user if needed.
	EnsureUser(ctx context.Context, email string) (userID string, err error)
	// CreateMagicLink stores a single-use token for email valid for ttl.
	CreateMagicLink(ctx context.Context, email string, ttl time.Duration) (token string, err error)
	// ConsumeMagicLink atomically validates and deletes a token, returning
	// the associated email. Returns ErrNotFound if the token is unknown or
	// expired.
	ConsumeMagicLink(ctx context.Context, token string) (email string, err error)
	// CreateSession creates a session for userID with the given ttl.
	CreateSession(ctx context.Context, userID string, ttl time.Duration) (sessionID string, expiresAt time.Time, err error)
	// GetSessionUser returns the user ID for a session, or ErrNotFound if the
	// session is unknown or expired.
	GetSessionUser(ctx context.Context, sessionID string) (userID string, err error)
	// DeleteSession removes a session. Deleting a missing session is not an
	// error.
	DeleteSession(ctx context.Context, sessionID string) error
}

// Mailer sends a magic-link email containing url to the given address.
type Mailer interface {
	SendMagicLink(ctx context.Context, email, url string) error
}

// ConsoleMailer is a Mailer that just logs the URL. Intended for dev when no
// email API key is configured. If Logf is nil, output is silently dropped.
type ConsoleMailer struct {
	Logf func(format string, args ...any)
}

// SendMagicLink logs the email + URL through Logf.
func (c ConsoleMailer) SendMagicLink(ctx context.Context, email, url string) error {
	if c.Logf == nil {
		return nil
	}
	c.Logf("magic link for %s: %s", email, url)
	return nil
}

// Handlers bundles the HTTP handlers and middleware for the auth flow.
type Handlers struct {
	Store   UserStore
	Mailer  Mailer
	BaseURL string
	Now     func() time.Time

	// CookieSecure controls whether the session cookie carries the Secure
	// flag. Defaults to true (zero value of bool is false, so callers must
	// explicitly set this to false for plain-HTTP dev/tests).
	CookieSecure bool
}

// ctxKey is the typed key under which the authenticated user ID is stored in
// a request context.
type ctxKey struct{}

var userIDKey = ctxKey{}

// UserIDFrom extracts the user ID set by Middleware. Returns ok=false if no
// user is set in ctx.
func (h *Handlers) UserIDFrom(ctx context.Context) (string, bool) {
	v := ctx.Value(userIDKey)
	if v == nil {
		return "", false
	}
	id, ok := v.(string)
	if !ok || id == "" {
		return "", false
	}
	return id, true
}

func (h *Handlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// --- templates -------------------------------------------------------------

var signupFormTpl = template.Must(template.New("signup").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Sign in</title></head>
<body>
<h1>Sign in</h1>
<form method="post" action="/signup">
  <label>Email <input type="email" name="email" required autofocus></label>
  <button type="submit">Send magic link</button>
</form>
</body>
</html>`))

var checkEmailTpl = template.Must(template.New("check").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Check your email</title></head>
<body>
<h1>Check your email</h1>
<p>We sent a sign-in link to <strong>{{.Email}}</strong>. Click it to continue.</p>
</body>
</html>`))

// --- handlers --------------------------------------------------------------

// SignupForm renders the email-entry form.
func (h *Handlers) SignupForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = signupFormTpl.Execute(w, nil)
}

// SignupSubmit handles POST /signup: validates the email, ensures the user
// exists, creates a magic-link token, and emails the verification URL.
func (h *Handlers) SignupSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	email := normalizeEmail(r.FormValue("email"))
	if !validEmail(email) {
		http.Error(w, "invalid email address", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if _, err := h.Store.EnsureUser(ctx, email); err != nil {
		http.Error(w, "could not create account", http.StatusInternalServerError)
		return
	}
	token, err := h.Store.CreateMagicLink(ctx, email, MagicLinkTTL)
	if err != nil {
		http.Error(w, "could not create magic link", http.StatusInternalServerError)
		return
	}
	verifyURL := h.buildVerifyURL(token)
	if err := h.Mailer.SendMagicLink(ctx, email, verifyURL); err != nil {
		http.Error(w, "could not send email", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = checkEmailTpl.Execute(w, struct{ Email string }{Email: email})
}

// Verify consumes a magic-link token and starts a session.
func (h *Handlers) Verify(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	email, err := h.Store.ConsumeMagicLink(ctx, token)
	if err != nil {
		http.Error(w, "invalid or expired link", http.StatusBadRequest)
		return
	}
	userID, err := h.Store.EnsureUser(ctx, email)
	if err != nil {
		http.Error(w, "could not look up account", http.StatusInternalServerError)
		return
	}
	sessID, expiresAt, err := h.Store.CreateSession(ctx, userID, SessionTTL)
	if err != nil {
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, h.sessionCookie(sessID, expiresAt))
	http.Redirect(w, r, "/onboarding", http.StatusSeeOther)
}

// Logout clears the session cookie and deletes the underlying session.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		_ = h.Store.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, h.clearCookie())
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Middleware enforces an authenticated session. On success it injects the
// user ID into the request context. On failure it clears the cookie (if any)
// and redirects to /signup.
func (h *Handlers) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil || c.Value == "" {
			h.redirectToSignup(w, r, false)
			return
		}
		userID, err := h.Store.GetSessionUser(r.Context(), c.Value)
		if err != nil || userID == "" {
			h.redirectToSignup(w, r, true)
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handlers) redirectToSignup(w http.ResponseWriter, r *http.Request, clearCookie bool) {
	if clearCookie {
		http.SetCookie(w, h.clearCookie())
	}
	http.Redirect(w, r, "/signup", http.StatusSeeOther)
}

// --- helpers ---------------------------------------------------------------

func (h *Handlers) buildVerifyURL(token string) string {
	base := strings.TrimRight(h.BaseURL, "/")
	return fmt.Sprintf("%s/auth/verify?token=%s", base, url.QueryEscape(token))
}

func (h *Handlers) sessionCookie(value string, expiresAt time.Time) *http.Cookie {
	maxAge := int(time.Until(expiresAt).Seconds())
	if h.Now != nil {
		maxAge = int(expiresAt.Sub(h.now()).Seconds())
	}
	if maxAge <= 0 {
		maxAge = int(SessionTTL.Seconds())
	}
	return &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (h *Handlers) clearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

// normalizeEmail trims surrounding whitespace and lowercases the address.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// validEmail performs a minimal syntactic check: exactly one '@', no spaces,
// non-empty local part and host with at least one '.' in the host.
//
// This is intentionally cheap — final validation happens when the mailer
// tries to deliver the message.
func validEmail(email string) bool {
	if email == "" {
		return false
	}
	if strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	at := strings.IndexByte(email, '@')
	if at <= 0 || at != strings.LastIndexByte(email, '@') {
		return false
	}
	local, host := email[:at], email[at+1:]
	if local == "" || host == "" {
		return false
	}
	if !strings.Contains(host, ".") {
		return false
	}
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	return true
}
