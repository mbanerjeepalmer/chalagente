package main

import (
	"context"
	"sync"
	"time"

	"github.com/mbanerjeepalmer/chalagente/internal/agent"
	"github.com/mbanerjeepalmer/chalagente/internal/auth"
	"github.com/mbanerjeepalmer/chalagente/internal/maps"
	"github.com/mbanerjeepalmer/chalagente/internal/store"
	"github.com/mbanerjeepalmer/chalagente/internal/voice"
	"github.com/mbanerjeepalmer/chalagente/internal/wamanager"
)

type App struct {
	Store    *store.Store
	WAMgr    *wamanager.Manager
	Agent    agent.Engine
	Voice    voice.Provider
	Maps     maps.Client
	Auth     *auth.Handlers
	BaseURL  string

	busMu  sync.Mutex
	recent map[string][]Event
	subs   map[string]map[chan Event]struct{}

	pairMu       sync.Mutex
	pairSessions map[string]*pairSession

	demoOnce  sync.Once
	demoState *demoHistory

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
