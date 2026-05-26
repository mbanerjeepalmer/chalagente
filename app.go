package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/mbanerjeepalmer/chalagente/internal/agent"
	"github.com/mbanerjeepalmer/chalagente/internal/clerkauth"
	"github.com/mbanerjeepalmer/chalagente/internal/maps"
	"github.com/mbanerjeepalmer/chalagente/internal/store"
	"github.com/mbanerjeepalmer/chalagente/internal/voice"
	"github.com/mbanerjeepalmer/chalagente/internal/wamanager"
)

type App struct {
	Store      *store.Store
	WAMgr      *wamanager.Manager
	Agent      agent.Engine
	Voice      voice.Provider
	Maps       maps.Client
	Translator Translator
	ClerkAuth  *clerkauth.Handlers
	BaseURL    string

	busMu  sync.Mutex
	recent map[string][]Event
	subs   map[string]map[chan Event]struct{}

	pairMu       sync.Mutex
	pairSessions map[string]*pairSession

	tryOnce  sync.Once
	tryState *tryStore
}

type Event struct {
	BusinessID string    `json:"business_id"`
	Time       time.Time `json:"time"`
	Dir        string    `json:"dir"` // "in" | "out"
	Chat       string    `json:"chat"`
	Body       string    `json:"body"`
	Kind       string    `json:"kind,omitempty"` // text|audio|image|video
}

type pairSession struct {
	mu        sync.Mutex
	code      string
	event     string // "code" | "success" | "timeout" | ""
	deviceJID string
	done      bool
	cancel    context.CancelFunc
	// codeCount counts how many "code" events whatsmeow has emitted on
	// this session — each one is a refreshed QR. After pairQRMaxAuto
	// codes the goroutine stops auto-refreshing and the UI prompts the
	// user to press "Regenerar QR" (which calls /start again, opening a
	// fresh session).
	codeCount     int
	needsManual   bool
}

// pairQRMaxAuto caps how many times the pairing QR auto-refreshes itself
// before the user has to manually request a fresh code. WhatsApp issues a
// new code roughly every 20s; three codes = ~1 minute of un-scanned QR,
// after which we assume the page is sitting idle and stop spamming codes.
const pairQRMaxAuto = 3

// userIDFrom returns the authenticated local user id injected by the
// Clerk middleware.
func (a *App) userIDFrom(r *http.Request) (string, bool) {
	return a.ClerkAuth.UserIDFrom(r.Context())
}

// signInPath returns the URL path to redirect unauthenticated users to.
func (a *App) signInPath() string { return "/sign-in" }

// authMiddleware returns the Clerk auth middleware. ClerkAuth must be set.
func (a *App) authMiddleware(next http.Handler) http.Handler {
	return a.ClerkAuth.Middleware(next)
}

func newApp() *App {
	return &App{
		recent:       make(map[string][]Event),
		subs:         make(map[string]map[chan Event]struct{}),
		pairSessions: make(map[string]*pairSession),
	}
}

func (a *App) publish(e Event) {
	a.busMu.Lock()
	a.recent[e.BusinessID] = append(a.recent[e.BusinessID], e)
	if len(a.recent[e.BusinessID]) > 100 {
		a.recent[e.BusinessID] = a.recent[e.BusinessID][len(a.recent[e.BusinessID])-100:]
	}
	subs := make([]chan Event, 0, len(a.subs[e.BusinessID]))
	for ch := range a.subs[e.BusinessID] {
		subs = append(subs, ch)
	}
	a.busMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// clearRecent drops the in-memory recent-event buffer for businessID. Used
// after the persisted chat history is deleted so the live feed in the
// dashboard reflects the same empty state.
func (a *App) clearRecent(businessID string) {
	a.busMu.Lock()
	defer a.busMu.Unlock()
	delete(a.recent, businessID)
}

func (a *App) subscribe(businessID string) (chan Event, []Event, func()) {
	ch := make(chan Event, 16)
	a.busMu.Lock()
	if a.subs[businessID] == nil {
		a.subs[businessID] = make(map[chan Event]struct{})
	}
	a.subs[businessID][ch] = struct{}{}
	snapshot := append([]Event(nil), a.recent[businessID]...)
	a.busMu.Unlock()
	return ch, snapshot, func() {
		a.busMu.Lock()
		if subs := a.subs[businessID]; subs != nil {
			delete(subs, ch)
		}
		a.busMu.Unlock()
		close(ch)
	}
}

func (a *App) getPairSession(bizID string) *pairSession {
	a.pairMu.Lock()
	defer a.pairMu.Unlock()
	return a.pairSessions[bizID]
}

func (a *App) setPairSession(bizID string, s *pairSession) {
	a.pairMu.Lock()
	defer a.pairMu.Unlock()
	if old, ok := a.pairSessions[bizID]; ok && old != nil && old.cancel != nil {
		old.cancel()
	}
	a.pairSessions[bizID] = s
}

func (a *App) clearPairSession(bizID string) {
	a.pairMu.Lock()
	defer a.pairMu.Unlock()
	if s, ok := a.pairSessions[bizID]; ok && s != nil && s.cancel != nil {
		s.cancel()
	}
	delete(a.pairSessions, bizID)
}
