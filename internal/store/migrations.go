package store

import (
	"context"
	"database/sql"
	"fmt"
)

// migration is a single named SQL migration applied to the database.
// Migrations are applied in order; once applied, an entry is recorded in
// the schema_migrations table so the same migration is not re-applied.
//
// SQL is written to be portable between SQLite and Postgres:
//   - TEXT for ids (UUID strings) and timestamps (RFC3339 strings)
//   - INTEGER 0/1 for booleans
//   - explicit ids, no autoincrement quirks
//
// TODO: switch the `?` placeholders in store.go to `$1, $2, …` style and
// add a placeholder helper when we add the Postgres driver. For now the
// queries use `?`, which go-sqlite3 expects.
type migration struct {
	name string
	sql  string
}

var migrations = []migration{
	{
		name: "0001_init",
		sql: `
CREATE TABLE users (
    id          TEXT PRIMARY KEY,
    email       TEXT NOT NULL UNIQUE,
    created_at  TEXT NOT NULL
);

CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id),
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);

CREATE TABLE magic_link_tokens (
    id          TEXT PRIMARY KEY,
    email       TEXT NOT NULL,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TEXT NOT NULL,
    consumed_at TEXT,
    created_at  TEXT NOT NULL
);
CREATE INDEX idx_magic_link_tokens_email ON magic_link_tokens(email);

CREATE TABLE businesses (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL UNIQUE REFERENCES users(id),
    name            TEXT NOT NULL DEFAULT '',
    maps_place_id   TEXT,
    address         TEXT NOT NULL DEFAULT '',
    phone           TEXT NOT NULL DEFAULT '',
    hours           TEXT NOT NULL DEFAULT '',
    categories      TEXT NOT NULL DEFAULT '',
    website         TEXT NOT NULL DEFAULT '',
    extra_info      TEXT NOT NULL DEFAULT '',
    wa_device_jid   TEXT,
    agent_enabled   INTEGER NOT NULL DEFAULT 1,
    voice_mode      TEXT NOT NULL DEFAULT 'auto',
    voice_id        TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
CREATE INDEX idx_businesses_user_id ON businesses(user_id);
CREATE INDEX idx_businesses_wa_device_jid ON businesses(wa_device_jid);

CREATE TABLE conversations (
    id              TEXT PRIMARY KEY,
    business_id     TEXT NOT NULL REFERENCES businesses(id),
    customer_jid    TEXT NOT NULL,
    detected_lang   TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE(business_id, customer_jid)
);
CREATE INDEX idx_conversations_business_id ON conversations(business_id);
CREATE INDEX idx_conversations_business_customer ON conversations(business_id, customer_jid);

CREATE TABLE messages (
    id              TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id),
    direction       TEXT NOT NULL,
    kind            TEXT NOT NULL,
    body            TEXT NOT NULL DEFAULT '',
    media_ref       TEXT,
    created_at      TEXT NOT NULL
);
CREATE INDEX idx_messages_conversation_created ON messages(conversation_id, created_at);

CREATE TABLE tool_configs (
    id              TEXT PRIMARY KEY,
    business_id     TEXT NOT NULL REFERENCES businesses(id),
    tool_key        TEXT NOT NULL,
    enabled         INTEGER NOT NULL DEFAULT 1,
    config_json     TEXT NOT NULL DEFAULT '',
    UNIQUE(business_id, tool_key)
);
CREATE INDEX idx_tool_configs_business_id ON tool_configs(business_id);
`,
	},
	{
		name: "0002_clerk_user_id",
		sql: `
ALTER TABLE users ADD COLUMN clerk_user_id TEXT;
CREATE UNIQUE INDEX idx_users_clerk_user_id ON users(clerk_user_id) WHERE clerk_user_id IS NOT NULL;
`,
	},
	{
		name: "0003_trigger_required",
		sql: `
ALTER TABLE businesses ADD COLUMN trigger_required INTEGER NOT NULL DEFAULT 1;
`,
	},
	{
		name: "0004_wa_prefill",
		sql: `
ALTER TABLE businesses ADD COLUMN wa_prefill_template TEXT NOT NULL DEFAULT '';
`,
	},
}

// runMigrations applies any pending migrations, in order. It creates the
// schema_migrations bookkeeping table on first run.
func runMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    name        TEXT PRIMARY KEY,
    applied_at  TEXT NOT NULL
);`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, m := range migrations {
		var existing string
		err := db.QueryRowContext(ctx,
			`SELECT name FROM schema_migrations WHERE name = ?`, m.name,
		).Scan(&existing)
		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("check migration %s: %w", m.name, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(name, applied_at) VALUES(?, ?)`,
			m.name, nowRFC3339(),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.name, err)
		}
	}
	return nil
}
