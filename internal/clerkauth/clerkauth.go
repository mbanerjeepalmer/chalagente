// Package clerkauth integrates Clerk-hosted authentication into the Go HTTP
// server. It mirrors the surface of internal/auth (Middleware, UserIDFrom,
// Logout) so the rest of the app can swap implementations.
//
// The middleware verifies the Clerk session JWT from the __session cookie
// (or an Authorization bearer header), maps the Clerk subject to a local
// user row, and injects that local user id into the request context.
//
// Clerk's default session token does not include email, so on the first
// request from a previously-unseen Clerk user we fetch the user record
// from Clerk's user API to source an email address.
package clerkauth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/clerk/clerk-sdk-go/v2/user"
)

// FrontendAPIFromPublishableKey decodes the Clerk publishable key
// (pk_test_..., pk_live_...) and extracts the Frontend API host. The key's
// third underscore-separated segment is base64-encoded "<host>$".
// Returns "" if the key isn't in the expected format; callers should fall
// back to an explicit CLERK_FRONTEND_API env var in that case.
func FrontendAPIFromPublishableKey(pk string) string {
	parts := strings.SplitN(pk, "_", 3)
	if len(parts) < 3 {
		return ""
	}
	// Clerk's keys pad with extra '$' chars to reach base64 alignment.
	raw, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(parts[2])
		if err != nil {
			return ""
		}
	}
	host := strings.TrimRight(string(raw), "$")
	return host
}

// SessionCookieName is the cookie Clerk sets on first-party apps. The value
// is a JWT we verify with the Frontend API JWKS.
const SessionCookieName = "__session"

// ErrNoEmail is returned by Resolver implementations when the Clerk user
// has no primary email address. We require an email to seed the local
// users table.
var ErrNoEmail = errors.New("clerkauth: clerk user has no email")

// UserStore is the storage contract the middleware needs. The concrete
// implementation lives in internal/store; we keep this interface narrow
// so this package can be unit-tested in isolation.
type UserStore interface {
	GetUserIDByClerkID(ctx context.Context, clerkID string) (string, error)
	EnsureUserByClerk(ctx context.Context, clerkID, email string) (string, error)
}

// Resolver fetches the primary email for a Clerk user id. The default
// implementation calls Clerk's user API; tests substitute a stub.
type Resolver interface {
	Email(ctx context.Context, clerkID string) (string, error)
}

// APIResolver is the default Resolver. It calls user.Get with the
// configured Clerk secret key.
type APIResolver struct{}

// Email looks up clerkID via the Clerk user API and returns the address of
// the user's primary email.
func (APIResolver) Email(ctx context.Context, clerkID string) (string, error) {
	u, err := user.Get(ctx, clerkID)
	if err != nil {
		return "", fmt.Errorf("clerkauth: fetch user %q: %w", clerkID, err)
	}
	return primaryEmail(u)
}

func primaryEmail(u *clerk.User) (string, error) {
	if u.PrimaryEmailAddressID != nil {
		for _, e := range u.EmailAddresses {
			if e.ID == *u.PrimaryEmailAddressID && e.EmailAddress != "" {
				return strings.ToLower(strings.TrimSpace(e.EmailAddress)), nil
			}
		}
	}
	for _, e := range u.EmailAddresses {
		if e.EmailAddress != "" {
			return strings.ToLower(strings.TrimSpace(e.EmailAddress)), nil
		}
	}
	return "", ErrNoEmail
}

// Handlers bundles the HTTP-facing pieces of the Clerk integration.
type Handlers struct {
	// SecretKey is the Clerk secret key (sk_...). Setting Handlers.Init runs
	// clerk.SetKey so the SDK's user API calls authenticate correctly.
	SecretKey string
	// PublishableKey (pk_...) and FrontendAPI host (e.g.
	// "happy-cat-12.clerk.accounts.dev") are needed by the rendered
	// ClerkJS sign-in/sign-up pages.
	PublishableKey string
	FrontendAPI    string
	// AfterSignInURL is where ClerkJS sends the user after a successful
	// sign-in. Defaults to "/onboarding" if empty.
	AfterSignInURL string
	// Store is the user mapping store.
	Store UserStore
	// Resolver supplies the email for a freshly-seen Clerk user.
	Resolver Resolver
	// Verify lets tests substitute the Clerk JWT verification. When nil,
	// jwt.Verify with the default JWKS fetcher is used.
	Verify func(ctx context.Context, token string) (*clerk.SessionClaims, error)
	// CookieSecure controls the Secure flag on the cleared-session cookie
	// emitted by Logout.
	CookieSecure bool

	initialised bool
}

// Init applies the secret key to the Clerk SDK. Safe to call multiple
// times. The Mux call sites invoke this implicitly.
func (h *Handlers) Init() {
	if h.initialised {
		return
	}
	if h.SecretKey != "" {
		clerk.SetKey(h.SecretKey)
	}
	if h.Resolver == nil {
		h.Resolver = APIResolver{}
	}
	if h.AfterSignInURL == "" {
		h.AfterSignInURL = "/onboarding"
	}
	if h.FrontendAPI == "" {
		h.FrontendAPI = FrontendAPIFromPublishableKey(h.PublishableKey)
	}
	if h.Verify == nil {
		h.Verify = func(ctx context.Context, token string) (*clerk.SessionClaims, error) {
			return jwt.Verify(ctx, &jwt.VerifyParams{Token: token})
		}
	}
	h.initialised = true
}

// ctxKey is the typed key for the local user id.
type ctxKey struct{}

var userIDKey = ctxKey{}

// UserIDFrom returns the local user id injected by Middleware.
func (h *Handlers) UserIDFrom(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(userIDKey).(string)
	return v, ok && v != ""
}

// Middleware enforces a valid Clerk session. On success it maps the Clerk
// subject to a local user id and injects it into the request context. On
// failure it redirects to the sign-in page.
func (h *Handlers) Middleware(next http.Handler) http.Handler {
	h.Init()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			h.redirectToSignIn(w, r)
			return
		}
		claims, err := h.Verify(r.Context(), token)
		if err != nil || claims == nil || claims.Subject == "" {
			h.redirectToSignIn(w, r)
			return
		}
		localID, err := h.resolveLocalUser(r.Context(), claims.Subject)
		if err != nil {
			http.Error(w, "could not link clerk user", http.StatusInternalServerError)
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, localID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handlers) resolveLocalUser(ctx context.Context, clerkID string) (string, error) {
	if id, err := h.Store.GetUserIDByClerkID(ctx, clerkID); err == nil {
		return id, nil
	}
	email, err := h.Resolver.Email(ctx, clerkID)
	if err != nil {
		return "", err
	}
	return h.Store.EnsureUserByClerk(ctx, clerkID, email)
}

func extractToken(r *http.Request) string {
	if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	if a := strings.TrimSpace(r.Header.Get("Authorization")); a != "" {
		return strings.TrimPrefix(a, "Bearer ")
	}
	return ""
}

// SignInPage renders a self-contained ClerkJS page that mounts the
// hosted-style <SignIn /> component.
func (h *Handlers) SignInPage(w http.ResponseWriter, r *http.Request) {
	h.Init()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = clerkPageTmpl.Execute(w, clerkPageData{
		Mode:           "sign-in",
		PublishableKey: h.PublishableKey,
		FrontendAPI:    h.FrontendAPI,
		AfterSignInURL: h.AfterSignInURL,
		Title:          "Inicia sesión",
		Heading:        "Inicia sesión en Chalagente",
	})
}

// SignUpPage renders the sign-up variant.
func (h *Handlers) SignUpPage(w http.ResponseWriter, r *http.Request) {
	h.Init()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = clerkPageTmpl.Execute(w, clerkPageData{
		Mode:           "sign-up",
		PublishableKey: h.PublishableKey,
		FrontendAPI:    h.FrontendAPI,
		AfterSignInURL: h.AfterSignInURL,
		Title:          "Crea tu cuenta",
		Heading:        "Crea tu cuenta de Chalagente",
	})
}

// Logout signs the user out of Clerk on the client and clears the
// __session cookie just in case.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	h.Init()
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = clerkLogoutTmpl.Execute(w, clerkPageData{
		PublishableKey: h.PublishableKey,
		FrontendAPI:    h.FrontendAPI,
	})
}

func (h *Handlers) redirectToSignIn(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/sign-in", http.StatusSeeOther)
}

type clerkPageData struct {
	Mode           string
	Title          string
	Heading        string
	PublishableKey string
	FrontendAPI    string
	AfterSignInURL string
}

// clerkPageTmpl renders sign-in or sign-up by loading ClerkJS from the
// instance's Frontend API and mounting the appropriate component into
// a div. We render minimal chrome here; landing-page styling can come
// later if we want a fully branded sign-in.
var clerkPageTmpl = template.Must(template.New("clerk").Parse(`<!doctype html>
<html lang="es-MX">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}} · Chalagente</title>
  <style>
    body { font-family: system-ui, -apple-system, sans-serif; margin: 0;
           min-height: 100vh; display: flex; flex-direction: column;
           align-items: center; justify-content: center; background: #faf6ef; }
    h1 { font-family: 'Cormorant Garamond', Georgia, serif; font-weight: 500;
         color: #1a1a1a; margin-bottom: 1rem; }
    #clerk-mount { min-height: 480px; min-width: 320px; }
  </style>
</head>
<body>
  <h1>{{.Heading}}</h1>
  <div id="clerk-mount"></div>
  <script
    async
    crossorigin="anonymous"
    data-clerk-publishable-key="{{.PublishableKey}}"
    src="https://{{.FrontendAPI}}/npm/@clerk/clerk-js@5/dist/clerk.browser.js"
    type="text/javascript"
    onload="bootClerk()"
  ></script>
  <script>
    async function bootClerk() {
      await window.Clerk.load();
      if (window.Clerk.user) {
        window.location.href = {{.AfterSignInURL}};
        return;
      }
      const mount = document.getElementById('clerk-mount');
      const opts = {
        afterSignInUrl: {{.AfterSignInURL}},
        afterSignUpUrl: {{.AfterSignInURL}},
      };
      {{if eq .Mode "sign-up"}}
      window.Clerk.mountSignUp(mount, opts);
      {{else}}
      window.Clerk.mountSignIn(mount, opts);
      {{end}}
    }
  </script>
</body>
</html>`))

var clerkLogoutTmpl = template.Must(template.New("clerk-logout").Parse(`<!doctype html>
<html lang="es-MX">
<head>
  <meta charset="utf-8">
  <title>Cerrando sesión · Chalagente</title>
</head>
<body>
  <p>Cerrando sesión…</p>
  <script
    async
    crossorigin="anonymous"
    data-clerk-publishable-key="{{.PublishableKey}}"
    src="https://{{.FrontendAPI}}/npm/@clerk/clerk-js@5/dist/clerk.browser.js"
    type="text/javascript"
    onload="bootClerk()"
  ></script>
  <script>
    async function bootClerk() {
      await window.Clerk.load();
      await window.Clerk.signOut();
      window.location.href = '/';
    }
  </script>
</body>
</html>`))
