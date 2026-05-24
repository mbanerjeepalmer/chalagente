// Package auth provides Cognito OIDC authentication with local session cookies
// and route middleware.
package auth

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// CookieName is the name of the session cookie set after successful login.
const CookieName = "chala_session"

// SessionTTL is how long a session cookie stays valid.
const SessionTTL = 30 * 24 * time.Hour

// stateCookieName holds the OAuth2 CSRF state between login and callback.
const stateCookieName = "oauth_state"

// stateCookieTTL is how long the OAuth state cookie is valid.
const stateCookieTTL = 10 * time.Minute

// ErrNotFound is returned by UserStore implementations when a record doesn't
// exist or has expired.
var ErrNotFound = errors.New("auth: not found")

// UserStore is the storage contract for Cognito auth and local sessions.
type UserStore interface {
	EnsureUserFromCognito(ctx context.Context, sub, email string) (userID string, err error)
	CreateSession(ctx context.Context, userID string, ttl time.Duration) (sessionID string, expiresAt time.Time, err error)
	GetSessionUser(ctx context.Context, sessionID string) (userID string, err error)
	DeleteSession(ctx context.Context, sessionID string) error
}

type ctxKey struct{}

var userIDKey = ctxKey{}

// UserIDFrom extracts the user ID set by Middleware.
func UserIDFrom(ctx context.Context) (string, bool) {
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

func withUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

type sessionConfig struct {
	cookieSecure bool
	now          func() time.Time
}

func (c sessionConfig) nowTime() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c sessionConfig) sessionCookie(value string, expiresAt time.Time) *http.Cookie {
	maxAge := int(expiresAt.Sub(c.nowTime()).Seconds())
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
		Secure:   c.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (c sessionConfig) clearSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (c sessionConfig) stateCookie(value string) *http.Cookie {
	exp := c.nowTime().Add(stateCookieTTL)
	return &http.Cookie{
		Name:     stateCookieName,
		Value:    value,
		Path:     "/",
		Expires:  exp,
		MaxAge:   int(stateCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   c.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (c sessionConfig) clearStateCookie() *http.Cookie {
	return &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

func sessionMiddleware(store UserStore, cfg sessionConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil || c.Value == "" {
			redirectToSignup(w, r, cfg, false)
			return
		}
		userID, err := store.GetSessionUser(r.Context(), c.Value)
		if err != nil || userID == "" {
			redirectToSignup(w, r, cfg, true)
			return
		}
		next.ServeHTTP(w, r.WithContext(withUserID(r.Context(), userID)))
	})
}

func redirectToSignup(w http.ResponseWriter, r *http.Request, cfg sessionConfig, clearCookie bool) {
	if clearCookie {
		http.SetCookie(w, cfg.clearSessionCookie())
	}
	http.Redirect(w, r, "/signup", http.StatusSeeOther)
}

func startSession(ctx context.Context, store UserStore, cfg sessionConfig, w http.ResponseWriter, userID string) error {
	sessID, expiresAt, err := store.CreateSession(ctx, userID, SessionTTL)
	if err != nil {
		return err
	}
	http.SetCookie(w, cfg.sessionCookie(sessID, expiresAt))
	return nil
}
