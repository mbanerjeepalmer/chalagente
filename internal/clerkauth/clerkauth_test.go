package clerkauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
)

// fakeStore is a tiny in-memory UserStore for middleware tests.
type fakeStore struct {
	mu       sync.Mutex
	byClerk  map[string]string // clerkID -> localID
	emails   map[string]string // localID -> email
	nextID   int
	ensureFn func(ctx context.Context, clerkID, email string) (string, error)
}

func newFakeStore() *fakeStore {
	return &fakeStore{byClerk: map[string]string{}, emails: map[string]string{}}
}

func (s *fakeStore) GetUserIDByClerkID(_ context.Context, clerkID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.byClerk[clerkID]; ok {
		return id, nil
	}
	return "", errors.New("not found")
}

func (s *fakeStore) EnsureUserByClerk(ctx context.Context, clerkID, email string) (string, error) {
	if s.ensureFn != nil {
		return s.ensureFn(ctx, clerkID, email)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.byClerk[clerkID]; ok {
		return id, nil
	}
	s.nextID++
	id := "local-" + itoa(s.nextID)
	s.byClerk[clerkID] = id
	s.emails[id] = email
	return id, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

type stubResolver struct {
	email string
	err   error
	calls int
}

func (s *stubResolver) Email(context.Context, string) (string, error) {
	s.calls++
	if s.err != nil {
		return "", s.err
	}
	return s.email, nil
}

func okNext(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func TestMiddleware_NoCookieRedirectsToSignIn(t *testing.T) {
	h := &Handlers{Store: newFakeStore(), Verify: failVerify}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	h.Middleware(http.HandlerFunc(okNext)).ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d want %d", rr.Code, http.StatusSeeOther)
	}
	if loc := rr.Header().Get("Location"); loc != "/sign-in" {
		t.Errorf("redirect: got %q want /sign-in", loc)
	}
}

func TestMiddleware_InvalidJWTRedirects(t *testing.T) {
	h := &Handlers{Store: newFakeStore(), Verify: failVerify}
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "bogus.jwt.value"})
	rr := httptest.NewRecorder()
	h.Middleware(http.HandlerFunc(okNext)).ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestMiddleware_KnownClerkUserSkipsResolver(t *testing.T) {
	store := newFakeStore()
	store.byClerk["user_abc"] = "local-7"
	resolver := &stubResolver{email: "should-not-be-called@example.com"}
	h := &Handlers{
		Store:    store,
		Resolver: resolver,
		Verify:   verifyAs("user_abc"),
	}

	var seenUser string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := h.UserIDFrom(r.Context())
		seenUser = id
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "token"})
	rr := httptest.NewRecorder()
	h.Middleware(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if seenUser != "local-7" {
		t.Errorf("user: got %q want local-7", seenUser)
	}
	if resolver.calls != 0 {
		t.Errorf("resolver called %d times; expected 0 for known clerk user", resolver.calls)
	}
}

func TestMiddleware_UnknownClerkUserResolvesEmailAndEnsures(t *testing.T) {
	store := newFakeStore()
	resolver := &stubResolver{email: "new@example.com"}
	h := &Handlers{
		Store:    store,
		Resolver: resolver,
		Verify:   verifyAs("user_new"),
	}

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "token"})
	rr := httptest.NewRecorder()
	h.Middleware(http.HandlerFunc(okNext)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d, body=%s", rr.Code, rr.Body.String())
	}
	if resolver.calls != 1 {
		t.Errorf("resolver calls: got %d want 1", resolver.calls)
	}
	if got := store.byClerk["user_new"]; got == "" {
		t.Errorf("user_new not stored in fake store")
	}
}

func TestMiddleware_AuthorizationHeaderFallback(t *testing.T) {
	store := newFakeStore()
	store.byClerk["user_hdr"] = "local-h"
	h := &Handlers{Store: store, Verify: verifyAs("user_hdr")}

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer abc.def.ghi")
	rr := httptest.NewRecorder()
	h.Middleware(http.HandlerFunc(okNext)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestSignInPageRendersClerkScript(t *testing.T) {
	h := &Handlers{
		PublishableKey: "pk_test_demo",
		FrontendAPI:    "frontend.example.dev",
		Store:          newFakeStore(),
		Verify:         failVerify,
	}
	rr := httptest.NewRecorder()
	h.SignInPage(rr, httptest.NewRequest(http.MethodGet, "/sign-in", nil))
	body := rr.Body.String()
	if !strings.Contains(body, "pk_test_demo") {
		t.Errorf("missing publishable key in body")
	}
	if !strings.Contains(body, "frontend.example.dev/npm/@clerk/clerk-js") {
		t.Errorf("missing clerk-js script URL with frontend api")
	}
	if !strings.Contains(body, "mountSignIn") {
		t.Errorf("expected mountSignIn call in body")
	}
}

func TestPrimaryEmail(t *testing.T) {
	id := "email_1"
	u := &clerk.User{
		PrimaryEmailAddressID: &id,
		EmailAddresses: []*clerk.EmailAddress{
			{ID: "email_0", EmailAddress: "secondary@example.com"},
			{ID: "email_1", EmailAddress: "Primary@Example.com"},
		},
	}
	got, err := primaryEmail(u)
	if err != nil {
		t.Fatalf("primaryEmail: %v", err)
	}
	if got != "primary@example.com" {
		t.Errorf("got %q want lowercase primary", got)
	}
}

func TestPrimaryEmail_NoEmail(t *testing.T) {
	u := &clerk.User{}
	_, err := primaryEmail(u)
	if !errors.Is(err, ErrNoEmail) {
		t.Fatalf("err: got %v want ErrNoEmail", err)
	}
}

// --- helpers ---------------------------------------------------------------

func failVerify(context.Context, string) (*clerk.SessionClaims, error) {
	return nil, errors.New("verify forced fail")
}

func verifyAs(subject string) func(context.Context, string) (*clerk.SessionClaims, error) {
	return func(context.Context, string) (*clerk.SessionClaims, error) {
		c := &clerk.SessionClaims{}
		c.Subject = subject
		return c, nil
	}
}

func TestFrontendAPIFromPublishableKey(t *testing.T) {
	// "right-donkey-25.clerk.accounts.dev$" base64-encoded
	pk := "pk_test_cmlnaHQtZG9ua2V5LTI1LmNsZXJrLmFjY291bnRzLmRldiQ"
	if got := FrontendAPIFromPublishableKey(pk); got != "right-donkey-25.clerk.accounts.dev" {
		t.Errorf("got %q", got)
	}
	if got := FrontendAPIFromPublishableKey("garbage"); got != "" {
		t.Errorf("garbage key returned %q, want empty", got)
	}
}
