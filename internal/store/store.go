// Package store is the multi-tenant data layer for chalagente v2. It owns
// the application schema (users, sessions, magic-link tokens, businesses,
// conversations, messages, tool configs) on a single SQLite file, with the
// long-term plan of swapping the driver for Postgres without changing
// queries. Whatsmeow keeps its own tables in the same file; the two
// namespaces do not collide.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// ErrNotFound is returned when a lookup does not match any row, or when a
// row exists but has been invalidated (expired session, expired or already
// consumed magic-link token, etc.).
var ErrNotFound = errors.New("store: not found")

// Store is the application data layer. It wraps *sql.DB so callers can be
// driver-agnostic.
type Store struct {
	db *sql.DB
}

// New wraps an already-open *sql.DB. Migrations are NOT run — use Open if
// you want the convenience of a one-shot constructor.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Open opens the SQLite database at dsn (a filesystem path), runs any
// pending migrations, and returns a ready-to-use Store.
func Open(dsn string) (*Store, error) {
	// _busy_timeout helps avoid spurious SQLITE_BUSY when whatsmeow and the
	// app contend on the same file. _fk=1 enables foreign-key checks.
	db, err := sql.Open("sqlite3", dsn+"?_busy_timeout=5000&_fk=1")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite handles concurrency best with a single writer.
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB exposes the raw *sql.DB for callers that need to share the connection
// (e.g. for embedding whatsmeow's session store in the same file).
func (s *Store) DB() *sql.DB { return s.db }

// nowRFC3339 returns the current time as an RFC3339 string with nanosecond
// precision. Stored as TEXT so it round-trips identically across SQLite and
// Postgres.
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		// Fall back to plain RFC3339 in case the value was written by an
		// older path or by hand.
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

// ----- Domain types -----

// User is a signed-up account. One user owns at most one Business in v2.
type User struct {
	ID          string
	Email       string
	ClerkUserID string
	CreatedAt   time.Time
}

// Session is a cookie-backed login session.
type Session struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// Business is a tenant. Most fields come from the onboarding wizard.
type Business struct {
	ID           string
	UserID       string
	Name         string
	MapsPlaceID  string
	Address      string
	Phone        string
	Hours        string // JSON blob
	Categories   string // JSON blob
	Website      string
	ExtraInfo    string
	WADeviceJID  string
	AgentEnabled bool
	VoiceMode    string // "auto" | "always" | "never"
	VoiceID      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Conversation is a 1:1 thread between a Business and a customer JID.
type Conversation struct {
	ID           string
	BusinessID   string
	CustomerJID  string
	DetectedLang string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Message is one turn inside a Conversation.
type Message struct {
	ID             string
	ConversationID string
	Direction      string // "in" | "out"
	Kind           string // "text" | "audio" | "image" | "video"
	Body           string
	MediaRef       string
	CreatedAt      time.Time
}

// ToolConfig is per-business configuration for a single tool.
type ToolConfig struct {
	ID         string
	BusinessID string
	ToolKey    string
	Enabled    bool
	ConfigJSON string
}

// ----- Users -----

// CreateUser inserts a new user. Returns an error if the email already exists.
func (s *Store) CreateUser(ctx context.Context, email string) (User, error) {
	u := User{
		ID:        uuid.NewString(),
		Email:     email,
		CreatedAt: time.Now().UTC(),
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users(id, email, created_at) VALUES(?, ?, ?)`,
		u.ID, u.Email, u.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// GetUser fetches a user by id.
func (s *Store) GetUser(ctx context.Context, id string) (User, error) {
	return s.getUserWhere(ctx, "id = ?", id)
}

// GetUserByEmail fetches a user by email.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	return s.getUserWhere(ctx, "email = ?", email)
}

func (s *Store) getUserWhere(ctx context.Context, where string, arg string) (User, error) {
	var u User
	var createdAt string
	var clerkID sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, clerk_user_id, created_at FROM users WHERE `+where,
		arg,
	).Scan(&u.ID, &u.Email, &clerkID, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	if clerkID.Valid {
		u.ClerkUserID = clerkID.String
	}
	u.CreatedAt = parseTime(createdAt)
	return u, nil
}

// GetUserByClerkID fetches a user by their Clerk user ID.
func (s *Store) GetUserByClerkID(ctx context.Context, clerkID string) (User, error) {
	return s.getUserWhere(ctx, "clerk_user_id = ?", clerkID)
}

// LinkClerkUser sets the clerk_user_id on an existing user row. Returns
// ErrNotFound if no row was updated.
func (s *Store) LinkClerkUser(ctx context.Context, userID, clerkID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET clerk_user_id = ? WHERE id = ?`,
		clerkID, userID,
	)
	if err != nil {
		return fmt.Errorf("link clerk user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("link clerk user: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// EnsureUserByClerk returns the local user for a Clerk user, creating or
// linking as needed. clerkID is required; email is optional but used when
// creating a brand new local user (Clerk is the source of truth there).
func (s *Store) EnsureUserByClerk(ctx context.Context, clerkID, email string) (User, error) {
	if clerkID == "" {
		return User{}, fmt.Errorf("ensure user by clerk: empty clerk id")
	}
	if email == "" {
		return User{}, fmt.Errorf("ensure user by clerk: empty email")
	}
	if u, err := s.GetUserByClerkID(ctx, clerkID); err == nil {
		return u, nil
	} else if !errors.Is(err, ErrNotFound) {
		return User{}, err
	}
	// No row for this clerk_user_id yet. If we have an email, try linking an
	// existing magic-link account before creating a fresh row.
	if email != "" {
		if u, err := s.GetUserByEmail(ctx, email); err == nil {
			if err := s.LinkClerkUser(ctx, u.ID, clerkID); err != nil {
				return User{}, err
			}
			u.ClerkUserID = clerkID
			return u, nil
		} else if !errors.Is(err, ErrNotFound) {
			return User{}, err
		}
	}
	// Create a new user. Email may be empty; we'll backfill it once Clerk's
	// user API is queried.
	u := User{
		ID:          uuid.NewString(),
		Email:       email,
		ClerkUserID: clerkID,
		CreatedAt:   time.Now().UTC(),
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users(id, email, clerk_user_id, created_at) VALUES(?, ?, ?, ?)`,
		u.ID, u.Email, u.ClerkUserID, u.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return User{}, fmt.Errorf("create clerk user: %w", err)
	}
	return u, nil
}

// ----- Sessions -----

// CreateSession inserts a new session valid for ttl from now. A negative
// ttl creates an already-expired session, which is useful for tests.
func (s *Store) CreateSession(ctx context.Context, userID string, ttl time.Duration) (Session, error) {
	now := time.Now().UTC()
	sess := Session{
		ID:        uuid.NewString(),
		UserID:    userID,
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions(id, user_id, expires_at, created_at) VALUES(?, ?, ?, ?)`,
		sess.ID, sess.UserID,
		sess.ExpiresAt.Format(time.RFC3339Nano),
		sess.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return sess, nil
}

// GetSession returns the session with id, or ErrNotFound if it is missing
// or expired.
func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	var sess Session
	var expiresAt, createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, expires_at, created_at FROM sessions WHERE id = ?`,
		id,
	).Scan(&sess.ID, &sess.UserID, &expiresAt, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	sess.ExpiresAt = parseTime(expiresAt)
	sess.CreatedAt = parseTime(createdAt)
	if !sess.ExpiresAt.After(time.Now().UTC()) {
		return Session{}, ErrNotFound
	}
	return sess, nil
}

// DeleteSession removes a session. Missing rows are not an error — the
// caller's intent ("ensure this session is gone") is satisfied either way.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// ----- Magic-link tokens -----

// hashToken hashes a magic-link token for storage. We never store the
// plaintext; the hash is just SHA-256, which is plenty since tokens are
// already 256 bits of randomness and short-lived.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateMagicLink generates a fresh single-use token for email, stores its
// hash, and returns the plaintext token to be embedded in the email link.
func (s *Store) CreateMagicLink(ctx context.Context, email string, ttl time.Duration) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO magic_link_tokens(id, email, token_hash, expires_at, created_at)
		 VALUES(?, ?, ?, ?, ?)`,
		uuid.NewString(), email, hashToken(token),
		now.Add(ttl).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return "", fmt.Errorf("create magic link: %w", err)
	}
	return token, nil
}

// ConsumeMagicLink marks the token consumed (atomically) and returns the
// associated email. Returns ErrNotFound if the token is unknown, expired,
// or already consumed.
func (s *Store) ConsumeMagicLink(ctx context.Context, token string) (string, error) {
	h := hashToken(token)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		email      string
		expiresAt  string
		consumedAt sql.NullString
	)
	err = tx.QueryRowContext(ctx,
		`SELECT email, expires_at, consumed_at FROM magic_link_tokens WHERE token_hash = ?`,
		h,
	).Scan(&email, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lookup magic link: %w", err)
	}
	if consumedAt.Valid {
		return "", ErrNotFound
	}
	if !parseTime(expiresAt).After(time.Now().UTC()) {
		return "", ErrNotFound
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE magic_link_tokens SET consumed_at = ? WHERE token_hash = ? AND consumed_at IS NULL`,
		nowRFC3339(), h,
	); err != nil {
		return "", fmt.Errorf("consume magic link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit consume: %w", err)
	}
	return email, nil
}

// ----- Businesses -----

// CreateBusiness creates an empty business owned by userID, with defaults
// matching the schema (agent on, voice mode auto). Returns an error if
// the user already has a business.
func (s *Store) CreateBusiness(ctx context.Context, userID string) (Business, error) {
	now := time.Now().UTC()
	b := Business{
		ID:           uuid.NewString(),
		UserID:       userID,
		AgentEnabled: true,
		VoiceMode:    "auto",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO businesses(
			id, user_id, name, maps_place_id, address, phone, hours, categories,
			website, extra_info, wa_device_jid, agent_enabled, voice_mode, voice_id,
			created_at, updated_at
		) VALUES(?, ?, '', NULL, '', '', '', '', '', '', NULL, 1, 'auto', NULL, ?, ?)`,
		b.ID, b.UserID,
		b.CreatedAt.Format(time.RFC3339Nano),
		b.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return Business{}, fmt.Errorf("create business: %w", err)
	}
	return b, nil
}

const businessCols = `id, user_id, name, maps_place_id, address, phone, hours,
	categories, website, extra_info, wa_device_jid, agent_enabled, voice_mode,
	voice_id, created_at, updated_at`

func scanBusiness(row *sql.Row) (Business, error) {
	var b Business
	var (
		mapsPlaceID sql.NullString
		waDeviceJID sql.NullString
		voiceID     sql.NullString
		agentInt    int
		createdAt   string
		updatedAt   string
	)
	err := row.Scan(
		&b.ID, &b.UserID, &b.Name, &mapsPlaceID, &b.Address, &b.Phone,
		&b.Hours, &b.Categories, &b.Website, &b.ExtraInfo, &waDeviceJID,
		&agentInt, &b.VoiceMode, &voiceID, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Business{}, ErrNotFound
	}
	if err != nil {
		return Business{}, fmt.Errorf("scan business: %w", err)
	}
	b.MapsPlaceID = mapsPlaceID.String
	b.WADeviceJID = waDeviceJID.String
	b.VoiceID = voiceID.String
	b.AgentEnabled = agentInt != 0
	b.CreatedAt = parseTime(createdAt)
	b.UpdatedAt = parseTime(updatedAt)
	return b, nil
}

// GetBusiness fetches a business by id.
func (s *Store) GetBusiness(ctx context.Context, id string) (Business, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+businessCols+` FROM businesses WHERE id = ?`, id,
	)
	return scanBusiness(row)
}

// GetBusinessByUserID fetches the business owned by userID.
func (s *Store) GetBusinessByUserID(ctx context.Context, userID string) (Business, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+businessCols+` FROM businesses WHERE user_id = ?`, userID,
	)
	return scanBusiness(row)
}

// GetBusinessByWADeviceJID fetches the business linked to a given WhatsApp
// device JID — used to route inbound messages to the right tenant.
func (s *Store) GetBusinessByWADeviceJID(ctx context.Context, jid string) (Business, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+businessCols+` FROM businesses WHERE wa_device_jid = ?`, jid,
	)
	return scanBusiness(row)
}

// UpdateBusiness writes b back to the database, refreshing updated_at.
func (s *Store) UpdateBusiness(ctx context.Context, b Business) error {
	agentInt := 0
	if b.AgentEnabled {
		agentInt = 1
	}
	updated := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`UPDATE businesses SET
			name = ?, maps_place_id = ?, address = ?, phone = ?, hours = ?,
			categories = ?, website = ?, extra_info = ?, wa_device_jid = ?,
			agent_enabled = ?, voice_mode = ?, voice_id = ?, updated_at = ?
		WHERE id = ?`,
		b.Name,
		nullableString(b.MapsPlaceID),
		b.Address, b.Phone, b.Hours, b.Categories, b.Website, b.ExtraInfo,
		nullableString(b.WADeviceJID),
		agentInt, b.VoiceMode,
		nullableString(b.VoiceID),
		updated.Format(time.RFC3339Nano),
		b.ID,
	)
	if err != nil {
		return fmt.Errorf("update business: %w", err)
	}
	return nil
}

// nullableString returns a sql.NullString equivalent for SQLite — empty
// strings become NULL so unique indexes on optional fields behave the way
// callers expect (multiple NULL rows allowed).
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ----- Conversations + messages -----

// UpsertConversation returns the existing conversation for (businessID,
// customerJID) or creates a new one if none exists. Idempotent.
func (s *Store) UpsertConversation(ctx context.Context, businessID, customerJID string) (Conversation, error) {
	// Fast path: already exists.
	var c Conversation
	var detectedLang sql.NullString
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, business_id, customer_jid, detected_lang, created_at, updated_at
		 FROM conversations WHERE business_id = ? AND customer_jid = ?`,
		businessID, customerJID,
	).Scan(&c.ID, &c.BusinessID, &c.CustomerJID, &detectedLang, &createdAt, &updatedAt)
	if err == nil {
		c.DetectedLang = detectedLang.String
		c.CreatedAt = parseTime(createdAt)
		c.UpdatedAt = parseTime(updatedAt)
		return c, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Conversation{}, fmt.Errorf("lookup convo: %w", err)
	}

	now := time.Now().UTC()
	c = Conversation{
		ID:          uuid.NewString(),
		BusinessID:  businessID,
		CustomerJID: customerJID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO conversations(id, business_id, customer_jid, detected_lang, created_at, updated_at)
		 VALUES(?, ?, ?, NULL, ?, ?)`,
		c.ID, c.BusinessID, c.CustomerJID,
		c.CreatedAt.Format(time.RFC3339Nano),
		c.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		// Race: someone else just inserted; re-read.
		row := s.db.QueryRowContext(ctx,
			`SELECT id, business_id, customer_jid, detected_lang, created_at, updated_at
			 FROM conversations WHERE business_id = ? AND customer_jid = ?`,
			businessID, customerJID,
		)
		var existing Conversation
		var dl sql.NullString
		var ca, ua string
		if err2 := row.Scan(&existing.ID, &existing.BusinessID, &existing.CustomerJID, &dl, &ca, &ua); err2 == nil {
			existing.DetectedLang = dl.String
			existing.CreatedAt = parseTime(ca)
			existing.UpdatedAt = parseTime(ua)
			return existing, nil
		}
		return Conversation{}, fmt.Errorf("upsert convo: %w", err)
	}
	return c, nil
}

// AppendMessage stores a new message in convoID. The caller may leave
// msg.ID and msg.CreatedAt zero — they will be generated. msg is not
// mutated; the returned values are written into a fresh row.
func (s *Store) AppendMessage(ctx context.Context, convoID string, msg Message) error {
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO messages(id, conversation_id, direction, kind, body, media_ref, created_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, convoID, msg.Direction, msg.Kind, msg.Body,
		nullableString(msg.MediaRef),
		msg.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("append message: %w", err)
	}
	// Bump the conversation's updated_at so list views can sort by activity.
	if _, err := s.db.ExecContext(ctx,
		`UPDATE conversations SET updated_at = ? WHERE id = ?`,
		nowRFC3339(), convoID,
	); err != nil {
		return fmt.Errorf("touch conversation: %w", err)
	}
	return nil
}

// ListMessages returns up to limit messages in convoID, newest first.
func (s *Store) ListMessages(ctx context.Context, convoID string, limit int) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, conversation_id, direction, kind, body, media_ref, created_at
		 FROM messages WHERE conversation_id = ?
		 ORDER BY created_at DESC, id DESC
		 LIMIT ?`,
		convoID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		var m Message
		var mediaRef sql.NullString
		var createdAt string
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Direction, &m.Kind,
			&m.Body, &mediaRef, &createdAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		m.MediaRef = mediaRef.String
		m.CreatedAt = parseTime(createdAt)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iter messages: %w", err)
	}
	return out, nil
}

// ListConversations returns up to limit conversations for businessID,
// most recently active first.
func (s *Store) ListConversations(ctx context.Context, businessID string, limit int) ([]Conversation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, business_id, customer_jid, detected_lang, created_at, updated_at
		 FROM conversations WHERE business_id = ?
		 ORDER BY updated_at DESC, id DESC
		 LIMIT ?`,
		businessID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	var out []Conversation
	for rows.Next() {
		var c Conversation
		var detectedLang sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&c.ID, &c.BusinessID, &c.CustomerJID, &detectedLang,
			&createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan convo: %w", err)
		}
		c.DetectedLang = detectedLang.String
		c.CreatedAt = parseTime(createdAt)
		c.UpdatedAt = parseTime(updatedAt)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iter conversations: %w", err)
	}
	return out, nil
}

// ----- Tools -----

// SetTool upserts a tool config for (businessID, key). If a row already
// exists for that key, its enabled flag and config JSON are updated.
func (s *Store) SetTool(ctx context.Context, businessID, key string, enabled bool, configJSON string) error {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	// Try update first; if no row was affected, insert. This avoids relying
	// on SQLite-only ON CONFLICT syntax for Postgres compatibility — both
	// dialects do support `ON CONFLICT ... DO UPDATE`, but the trivial
	// update-or-insert path is fine for the volumes we expect.
	res, err := s.db.ExecContext(ctx,
		`UPDATE tool_configs SET enabled = ?, config_json = ?
		 WHERE business_id = ? AND tool_key = ?`,
		enabledInt, configJSON, businessID, key,
	)
	if err != nil {
		return fmt.Errorf("update tool: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n > 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO tool_configs(id, business_id, tool_key, enabled, config_json)
		 VALUES(?, ?, ?, ?, ?)`,
		uuid.NewString(), businessID, key, enabledInt, configJSON,
	); err != nil {
		return fmt.Errorf("insert tool: %w", err)
	}
	return nil
}

// ListTools returns all tool configs for businessID.
func (s *Store) ListTools(ctx context.Context, businessID string) ([]ToolConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, business_id, tool_key, enabled, config_json
		 FROM tool_configs WHERE business_id = ?
		 ORDER BY tool_key ASC`,
		businessID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}
	defer rows.Close()

	var out []ToolConfig
	for rows.Next() {
		var tc ToolConfig
		var enabledInt int
		if err := rows.Scan(&tc.ID, &tc.BusinessID, &tc.ToolKey, &enabledInt, &tc.ConfigJSON); err != nil {
			return nil, fmt.Errorf("scan tool: %w", err)
		}
		tc.Enabled = enabledInt != 0
		out = append(out, tc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iter tools: %w", err)
	}
	return out, nil
}
