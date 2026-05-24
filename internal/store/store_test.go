package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	return s
}

func TestMigrationsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close 1: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("close 2: %v", err)
	}
}

func TestCreateUserAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == "" {
		t.Fatalf("expected user id, got empty")
	}
	if u.Email != "alice@example.com" {
		t.Fatalf("email mismatch: %s", u.Email)
	}
	if u.CreatedAt.IsZero() {
		t.Fatalf("expected non-zero created_at")
	}

	got, err := s.GetUserByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("expected %s, got %s", u.ID, got.ID)
	}

	gotByID, err := s.GetUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if gotByID.Email != "alice@example.com" {
		t.Fatalf("email mismatch via id: %s", gotByID.Email)
	}
}

func TestUserEmailUnique(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.CreateUser(ctx, "dup@example.com"); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	if _, err := s.CreateUser(ctx, "dup@example.com"); err == nil {
		t.Fatalf("expected uniqueness error, got nil")
	}
}

func TestGetUserNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.GetUser(ctx, "no-such-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := s.GetUserByEmail(ctx, "missing@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEnsureUserFromCognito(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.EnsureUserFromCognito(ctx, "sub-1", "new@example.com")
	if err != nil {
		t.Fatalf("EnsureUserFromCognito create: %v", err)
	}
	if u.CognitoSub != "sub-1" || u.Email != "new@example.com" {
		t.Fatalf("user = %+v", u)
	}

	u2, err := s.EnsureUserFromCognito(ctx, "sub-1", "new@example.com")
	if err != nil {
		t.Fatalf("EnsureUserFromCognito lookup: %v", err)
	}
	if u2.ID != u.ID {
		t.Fatalf("expected same user id, got %q vs %q", u2.ID, u.ID)
	}

	legacy, err := s.CreateUser(ctx, "legacy@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	linked, err := s.EnsureUserFromCognito(ctx, "sub-legacy", "legacy@example.com")
	if err != nil {
		t.Fatalf("EnsureUserFromCognito link: %v", err)
	}
	if linked.ID != legacy.ID {
		t.Fatalf("expected link to legacy user, got %q vs %q", linked.ID, legacy.ID)
	}
	got, err := s.GetUserByCognitoSub(ctx, "sub-legacy")
	if err != nil {
		t.Fatalf("GetUserByCognitoSub: %v", err)
	}
	if got.ID != legacy.ID {
		t.Fatalf("linked cognito sub not persisted")
	}
}

func TestMagicLinkRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tok, err := s.CreateMagicLink(ctx, "magic@example.com", time.Minute)
	if err != nil {
		t.Fatalf("CreateMagicLink: %v", err)
	}
	if tok == "" {
		t.Fatalf("expected token, got empty")
	}

	email, err := s.ConsumeMagicLink(ctx, tok)
	if err != nil {
		t.Fatalf("ConsumeMagicLink: %v", err)
	}
	if email != "magic@example.com" {
		t.Fatalf("expected magic@example.com, got %s", email)
	}

	// Second consumption must fail.
	if _, err := s.ConsumeMagicLink(ctx, tok); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second consume, got %v", err)
	}
}

func TestMagicLinkExpired(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tok, err := s.CreateMagicLink(ctx, "exp@example.com", -time.Minute)
	if err != nil {
		t.Fatalf("CreateMagicLink: %v", err)
	}
	if _, err := s.ConsumeMagicLink(ctx, tok); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for expired token, got %v", err)
	}
}

func TestMagicLinkUnknown(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.ConsumeMagicLink(ctx, "completely-bogus-token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSessionCreateGetExpire(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "sess@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	sess, err := s.CreateSession(ctx, u.ID, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ID == "" {
		t.Fatalf("expected session id")
	}

	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.UserID != u.ID {
		t.Fatalf("UserID mismatch: %s vs %s", got.UserID, u.ID)
	}

	if err := s.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.GetSession(ctx, sess.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	expired, err := s.CreateSession(ctx, u.ID, -time.Minute)
	if err != nil {
		t.Fatalf("CreateSession expired: %v", err)
	}
	if _, err := s.GetSession(ctx, expired.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for expired session, got %v", err)
	}
}

func TestBusinessCreateUpdateGetByJID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "biz@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	b, err := s.CreateBusiness(ctx, u.ID)
	if err != nil {
		t.Fatalf("CreateBusiness: %v", err)
	}
	if b.ID == "" {
		t.Fatalf("expected business id")
	}
	if !b.AgentEnabled {
		t.Fatalf("expected AgentEnabled true by default")
	}
	if b.VoiceMode != "auto" {
		t.Fatalf("expected VoiceMode=auto, got %q", b.VoiceMode)
	}

	// One business per user.
	if _, err := s.CreateBusiness(ctx, u.ID); err == nil {
		t.Fatalf("expected uniqueness error creating second business for same user")
	}

	gotByUser, err := s.GetBusinessByUserID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetBusinessByUserID: %v", err)
	}
	if gotByUser.ID != b.ID {
		t.Fatalf("expected %s, got %s", b.ID, gotByUser.ID)
	}

	b.Name = "Café Test"
	b.Address = "123 Main St"
	b.Phone = "+5215555555555"
	b.Hours = `{"mon":"9-5"}`
	b.Categories = `["coffee","bakery"]`
	b.Website = "https://example.com"
	b.ExtraInfo = "We do oat milk."
	b.MapsPlaceID = "ChIJxyz"
	b.WADeviceJID = "5215555555555.0:0@s.whatsapp.net"
	b.AgentEnabled = false
	b.VoiceMode = "always"
	b.VoiceID = "voice-xyz"

	if err := s.UpdateBusiness(ctx, b); err != nil {
		t.Fatalf("UpdateBusiness: %v", err)
	}

	got, err := s.GetBusiness(ctx, b.ID)
	if err != nil {
		t.Fatalf("GetBusiness: %v", err)
	}
	if got.Name != "Café Test" {
		t.Fatalf("Name mismatch: %s", got.Name)
	}
	if got.AgentEnabled {
		t.Fatalf("expected AgentEnabled=false")
	}
	if got.VoiceMode != "always" {
		t.Fatalf("VoiceMode mismatch: %s", got.VoiceMode)
	}
	if got.VoiceID != "voice-xyz" {
		t.Fatalf("VoiceID mismatch: %s", got.VoiceID)
	}
	if got.WADeviceJID != "5215555555555.0:0@s.whatsapp.net" {
		t.Fatalf("WADeviceJID mismatch: %s", got.WADeviceJID)
	}

	byJID, err := s.GetBusinessByWADeviceJID(ctx, "5215555555555.0:0@s.whatsapp.net")
	if err != nil {
		t.Fatalf("GetBusinessByWADeviceJID: %v", err)
	}
	if byJID.ID != b.ID {
		t.Fatalf("expected %s, got %s", b.ID, byJID.ID)
	}

	if _, err := s.GetBusinessByWADeviceJID(ctx, "no-such-jid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown JID, got %v", err)
	}
}

func TestConversationUpsertIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "convo@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	b, err := s.CreateBusiness(ctx, u.ID)
	if err != nil {
		t.Fatalf("CreateBusiness: %v", err)
	}

	jid := "5215551234567@s.whatsapp.net"
	c1, err := s.UpsertConversation(ctx, b.ID, jid)
	if err != nil {
		t.Fatalf("UpsertConversation 1: %v", err)
	}
	c2, err := s.UpsertConversation(ctx, b.ID, jid)
	if err != nil {
		t.Fatalf("UpsertConversation 2: %v", err)
	}
	if c1.ID != c2.ID {
		t.Fatalf("expected same convo, got %s vs %s", c1.ID, c2.ID)
	}

	// Different JID should yield a new convo.
	c3, err := s.UpsertConversation(ctx, b.ID, "5215559999999@s.whatsapp.net")
	if err != nil {
		t.Fatalf("UpsertConversation 3: %v", err)
	}
	if c3.ID == c1.ID {
		t.Fatalf("expected different convo for different JID")
	}
}

func TestAppendAndListMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "msg@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	b, err := s.CreateBusiness(ctx, u.ID)
	if err != nil {
		t.Fatalf("CreateBusiness: %v", err)
	}
	c, err := s.UpsertConversation(ctx, b.ID, "5215551112222@s.whatsapp.net")
	if err != nil {
		t.Fatalf("UpsertConversation: %v", err)
	}

	bodies := []string{"hello", "hi back", "how are you", "great thanks"}
	for i, body := range bodies {
		dir := "in"
		if i%2 == 1 {
			dir = "out"
		}
		if err := s.AppendMessage(ctx, c.ID, Message{
			Direction: dir,
			Kind:      "text",
			Body:      body,
		}); err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
		// Ensure distinct timestamps to make ordering unambiguous.
		time.Sleep(2 * time.Millisecond)
	}

	msgs, err := s.ListMessages(ctx, c.ID, 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != len(bodies) {
		t.Fatalf("expected %d messages, got %d", len(bodies), len(msgs))
	}
	// Newest first.
	if msgs[0].Body != bodies[len(bodies)-1] {
		t.Fatalf("expected newest first, got %s", msgs[0].Body)
	}
	if msgs[len(msgs)-1].Body != bodies[0] {
		t.Fatalf("expected oldest last, got %s", msgs[len(msgs)-1].Body)
	}

	// Limit honoured.
	short, err := s.ListMessages(ctx, c.ID, 2)
	if err != nil {
		t.Fatalf("ListMessages limit: %v", err)
	}
	if len(short) != 2 {
		t.Fatalf("expected 2, got %d", len(short))
	}
}

func TestListConversations(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "convos@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	b, err := s.CreateBusiness(ctx, u.ID)
	if err != nil {
		t.Fatalf("CreateBusiness: %v", err)
	}

	jids := []string{"a@s.whatsapp.net", "b@s.whatsapp.net", "c@s.whatsapp.net"}
	for _, j := range jids {
		if _, err := s.UpsertConversation(ctx, b.ID, j); err != nil {
			t.Fatalf("UpsertConversation: %v", err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	convos, err := s.ListConversations(ctx, b.ID, 10)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convos) != 3 {
		t.Fatalf("expected 3 convos, got %d", len(convos))
	}
}

func TestSetAndListTools(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "tools@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	b, err := s.CreateBusiness(ctx, u.ID)
	if err != nil {
		t.Fatalf("CreateBusiness: %v", err)
	}

	if err := s.SetTool(ctx, b.ID, "elevenlabs_tts", true, `{"voice":"abc"}`); err != nil {
		t.Fatalf("SetTool: %v", err)
	}
	if err := s.SetTool(ctx, b.ID, "elevenlabs_stt", true, `{}`); err != nil {
		t.Fatalf("SetTool stt: %v", err)
	}

	// Upsert: same key updates rather than duplicates.
	if err := s.SetTool(ctx, b.ID, "elevenlabs_tts", false, `{"voice":"xyz"}`); err != nil {
		t.Fatalf("SetTool upsert: %v", err)
	}

	tools, err := s.ListTools(ctx, b.ID)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	var ttsFound bool
	for _, tc := range tools {
		if tc.ToolKey == "elevenlabs_tts" {
			ttsFound = true
			if tc.Enabled {
				t.Fatalf("expected tts disabled after upsert")
			}
			if !strings.Contains(tc.ConfigJSON, "xyz") {
				t.Fatalf("expected updated config, got %s", tc.ConfigJSON)
			}
		}
	}
	if !ttsFound {
		t.Fatalf("expected elevenlabs_tts in list")
	}
}
