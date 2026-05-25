package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mbanerjeepalmer/chalagente/internal/agent"
	"github.com/mbanerjeepalmer/chalagente/internal/auth"
	"github.com/mbanerjeepalmer/chalagente/internal/maps"
	"github.com/mbanerjeepalmer/chalagente/internal/store"
	"github.com/mbanerjeepalmer/chalagente/internal/voice"
	"github.com/mbanerjeepalmer/chalagente/internal/wamanager"

	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func main() {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf(".env: %v", err)
	}

	dbPath := getenv("DB_PATH", "./data/app.db")
	httpAddr := getenv("HTTP_ADDR", ":8080")
	baseURL := getenv("BASE_URL", "http://localhost:8080")

	if err := os.MkdirAll(dirOf(dbPath), 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	appStore, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer appStore.Close()

	waContainer, err := sqlstore.New(ctx,
		"sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on&_busy_timeout=5000", dbPath),
		waLog.Stdout("DB", "INFO", true))
	if err != nil {
		log.Fatalf("open whatsmeow store: %v", err)
	}
	defer waContainer.Close()

	wam := wamanager.New(waContainer, waLog.Stdout("WA", "INFO", true))

	app := newApp()
	app.Store = appStore
	app.WAMgr = wam
	app.Agent = buildAgent()
	app.Voice = voice.NewCachedProvider(&voice.MockProvider{}, 256)
	app.Maps = maps.DefaultMockClient()
	app.BaseURL = baseURL
	app.Auth = &auth.Handlers{
		Store:        &storeAuthAdapter{s: appStore},
		Mailer:       auth.ConsoleMailer{Logf: log.Printf},
		BaseURL:      baseURL,
		CookieSecure: getenv("COOKIE_SECURE", "false") == "true",
	}

	wam.SetEventHandler(app.handleWAEvent)

	if err := app.bootPairedTenants(ctx); err != nil {
		log.Printf("boot tenants: %v", err)
	}

	go app.serveHTTP(httpAddr)
	log.Printf("HTTP listening on %s", httpAddr)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	<-sigs
	log.Printf("shutting down...")
	wam.Shutdown()
}

func (a *App) bootPairedTenants(ctx context.Context) error {
	// Iterate every business that has a wa_device_jid and connect it.
	// We don't have a "list all businesses" helper yet; do a small inline
	// query via the store's DB until we need a richer API.
	rows, err := a.Store.DB().QueryContext(ctx,
		`SELECT id, wa_device_jid FROM businesses WHERE wa_device_jid IS NOT NULL AND wa_device_jid != ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var bizID, jidStr string
		if err := rows.Scan(&bizID, &jidStr); err != nil {
			log.Printf("boot scan: %v", err)
			continue
		}
		jid, err := types.ParseJID(jidStr)
		if err != nil {
			log.Printf("boot parse jid %q: %v", jidStr, err)
			continue
		}
		if err := a.WAMgr.StartPaired(ctx, bizID, jid); err != nil {
			log.Printf("boot start %s: %v", bizID, err)
			continue
		}
		log.Printf("boot: connected %s as %s", bizID, jid)
	}
	return rows.Err()
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// buildAgent picks the best agent.Engine for the current environment:
//   - if AWS_BEARER_TOKEN_BEDROCK is set, BedrockEngine is added to the chain;
//   - if MISTRAL_API_KEY is set, MistralEngine is added (as a fallback if
//     Bedrock is also configured, or as the only engine if not);
//   - if neither is set, MockEngine — so dev and tests still work without keys.
func buildAgent() agent.Engine {
	var chain []agent.Engine
	if tok := os.Getenv("AWS_BEARER_TOKEN_BEDROCK"); tok != "" {
		region := getenv("AWS_REGION", "us-east-1")
		model := getenv("BEDROCK_MODEL_ID", "us.anthropic.claude-haiku-4-5-20251001-v1:0")
		base := getenv("BEDROCK_ENDPOINT", fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", region))
		chain = append(chain, &agent.BedrockEngine{Token: tok, Model: model, BaseURL: base})
		log.Printf("agent: bedrock enabled (model=%s)", model)
	}
	if tok := os.Getenv("MISTRAL_API_KEY"); tok != "" {
		model := getenv("MISTRAL_MODEL_ID", "mistral-small-latest")
		base := getenv("MISTRAL_ENDPOINT", "https://api.mistral.ai")
		chain = append(chain, &agent.MistralEngine{Token: tok, Model: model, BaseURL: base})
		log.Printf("agent: mistral enabled (model=%s)", model)
	}
	switch len(chain) {
	case 0:
		log.Printf("agent: no LLM key set, using MockEngine")
		return agent.NewMockEngine()
	case 1:
		return chain[0]
	default:
		return agent.FallbackEngine{Engines: chain}
	}
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}
