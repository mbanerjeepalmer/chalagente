package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mbanerjeepalmer/chalagente/internal/store"
	"go.mau.fi/whatsmeow"
	"rsc.io/qr"
)

func (a *App) ensureBusiness(ctx context.Context, userID string) (store.Business, error) {
	b, err := a.Store.GetBusinessByUserID(ctx, userID)
	if err == nil {
		return b, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Business{}, err
	}
	return a.Store.CreateBusiness(ctx, userID)
}

func (a *App) requireBusiness(w http.ResponseWriter, r *http.Request) (store.Business, bool) {
	userID, ok := a.userIDFrom(r)
	if !ok {
		http.Redirect(w, r, a.signInPath(), http.StatusSeeOther)
		return store.Business{}, false
	}
	b, err := a.ensureBusiness(r.Context(), userID)
	if err != nil {
		http.Error(w, "store error: "+err.Error(), http.StatusInternalServerError)
		return store.Business{}, false
	}
	return b, true
}

// ---- WhatsApp pairing ----
// These handlers back the inline pair UI on /admin/connection. The
// /onboarding/* wizard that wrapped them is gone.

func (a *App) handleOnboardingWhatsAppStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	if b.WADeviceJID != "" {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte("already paired"))
		return
	}

	pairCtx, cancel := context.WithCancel(context.Background())
	sess := &pairSession{cancel: cancel}
	a.setPairSession(b.ID, sess)

	qrChan, err := a.WAMgr.StartPairing(pairCtx, b.ID)
	if err != nil {
		a.clearPairSession(b.ID)
		http.Error(w, "pair start: "+err.Error(), http.StatusInternalServerError)
		return
	}

	go a.drivePairing(b.ID, qrChan)
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("pairing started"))
}

func (a *App) drivePairing(bizID string, qrChan <-chan whatsmeow.QRChannelItem) {
	for evt := range qrChan {
		sess := a.getPairSession(bizID)
		if sess == nil {
			return
		}
		sess.mu.Lock()
		sess.event = evt.Event
		if evt.Event == "code" {
			sess.codeCount++
			if sess.codeCount > pairQRMaxAuto {
				// Cap reached. Cancel the underlying pairing context so
				// whatsmeow stops issuing codes, mark the session as
				// needing manual refresh, and exit the loop.
				sess.needsManual = true
				sess.event = "needs_manual"
				if sess.cancel != nil {
					sess.cancel()
				}
				sess.mu.Unlock()
				return
			}
			sess.code = evt.Code
		}
		if evt.Event == "success" {
			if jid, ok := a.WAMgr.DeviceJID(bizID); ok {
				sess.deviceJID = jid.String()
				// Persist on the business.
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				biz, err := a.Store.GetBusiness(ctx, bizID)
				if err == nil {
					biz.WADeviceJID = jid.String()
					if err := a.Store.UpdateBusiness(ctx, biz); err != nil {
						log.Printf("pair: save jid: %v", err)
					}
				}
				cancel()
			}
			sess.done = true
		}
		sess.mu.Unlock()
		if evt.Event == "success" || evt.Event == "timeout" || evt.Event == "err-client-outdated" {
			return
		}
	}
}

func (a *App) handleOnboardingQRPNG(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	sess := a.getPairSession(b.ID)
	if sess == nil {
		http.Error(w, "no pair session", http.StatusNotFound)
		return
	}
	sess.mu.Lock()
	code := sess.code
	sess.mu.Unlock()
	if code == "" {
		http.Error(w, "no qr yet", http.StatusNotFound)
		return
	}
	c, err := qr.Encode(code, qr.M)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(c.PNG())
}

func (a *App) handleOnboardingPairStatus(w http.ResponseWriter, r *http.Request) {
	b, ok := a.requireBusiness(w, r)
	if !ok {
		return
	}
	sess := a.getPairSession(b.ID)
	w.Header().Set("Content-Type", "application/json")
	if sess == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"state": "idle"})
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"state":        sess.event,
		"has_qr":       sess.code != "",
		"device_jid":   sess.deviceJID,
		"done":         sess.done,
		"needs_manual": sess.needsManual,
		"code_count":   sess.codeCount,
	})
}

func waMeURL(jidStr string) string {
	phone := phoneFromJID(jidStr)
	if phone == "" {
		return ""
	}
	return fmt.Sprintf("https://wa.me/%s", phone)
}

// phoneFromJID extracts the bare phone number from a whatsmeow JID. JIDs look
// like "5215512345678.0:13@s.whatsapp.net" or "5215512345678@s.whatsapp.net".
func phoneFromJID(jidStr string) string {
	if jidStr == "" {
		return ""
	}
	at := strings.Index(jidStr, "@")
	if at <= 0 {
		return ""
	}
	user := jidStr[:at]
	if dot := strings.Index(user, "."); dot > 0 {
		user = user[:dot]
	}
	if colon := strings.Index(user, ":"); colon > 0 {
		user = user[:colon]
	}
	return user
}

// Wizard templates removed — the inline pair UI on /admin/connection
// supplies all the onboarding affordances now.
