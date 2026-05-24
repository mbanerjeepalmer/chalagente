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
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func main() {
	_ = godotenv.Load()

	dbPath := getenv("DB_PATH", "./data/app.db")
	httpAddr := getenv("HTTP_ADDR", ":8080")
	baseURL := getenv("BASE_URL", "https://chalagente.com")

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
	app.Agent = agent.NewMockEngine()
	app.Voice = voice.NewCachedProvider(&voice.MockProvider{}, 256)
	app.Maps = maps.DefaultMockClient()
	app.BaseURL = baseURL

	cognitoAuth, err := auth.NewCognitoHandlers(ctx, &storeAuthAdapter{s: appStore}, auth.CognitoConfig{
		Region:       requireEnv("COGNITO_REGION"),
		UserPoolID:   requireEnv("COGNITO_USER_POOL_ID"),
		ClientID:     requireEnv("COGNITO_CLIENT_ID"),
		ClientSecret: requireEnv("COGNITO_CLIENT_SECRET"),
		Domain:       requireEnv("COGNITO_DOMAIN"),
		BaseURL:      baseURL,
		CookieSecure: getenv("COOKIE_SECURE", "false") == "true",
	})
	if err != nil {
		log.Fatalf("cognito auth: %v", err)
	}
	app.Auth = cognitoAuth

	wam.SetEventHandler(app.handleWAEvent)

	if err := app.bootPairedTenants(ctx); err != nil {
		log.Printf("boot tenants: %v", err)
	}

	go func() {
		if err := app.serveHTTP(httpAddr); err != nil {
			log.Fatalf("http server: %v", err)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	<-sigs
	log.Printf("shutting down...")
	wam.Shutdown()
}

func (a *App) bootPairedTenants(ctx context.Context) error {
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

func requireEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("missing required env var %s", k)
	}
	return v
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}
