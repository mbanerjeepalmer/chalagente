package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

type Event struct {
	Time time.Time `json:"time"`
	Dir  string    `json:"dir"` // "in" | "out"
	Chat string    `json:"chat"`
	Body string    `json:"body"`
}

type App struct {
	client *whatsmeow.Client

	mu          sync.RWMutex
	currentQR   string
	lastQREvent string

	busMu  sync.Mutex
	recent []Event
	subs   map[chan Event]struct{}

	scripted *scriptedReplier
	agent    Replier

	modeMu      sync.RWMutex
	currentMode string
}

func newApp(client *whatsmeow.Client) *App {
	return &App{
		client:      client,
		subs:        make(map[chan Event]struct{}),
		scripted:    newScriptedReplier(hardcodedResponses),
		currentMode: "scripted",
	}
}

func (a *App) mode() string {
	a.modeMu.RLock()
	defer a.modeMu.RUnlock()
	return a.currentMode
}

func (a *App) setMode(m string) error {
	if m != "scripted" && m != "agent" {
		return fmt.Errorf("invalid mode %q", m)
	}
	a.modeMu.Lock()
	a.currentMode = m
	a.modeMu.Unlock()
	return nil
}

func (a *App) reply(ctx context.Context, chat, text string) (string, bool, error) {
	if a.mode() == "agent" && a.agent != nil {
		return a.agent.Reply(ctx, chat, text)
	}
	return a.scripted.Reply(ctx, chat, text)
}

func (a *App) isLoggedIn() bool  { return a.client != nil && a.client.IsLoggedIn() }
func (a *App) isConnected() bool { return a.client != nil && a.client.IsConnected() }

func (a *App) sessionSnapshot() map[string]int {
	return a.scripted.Snapshot()
}

func (a *App) resetSessions() {
	a.scripted.Reset()
}

func (a *App) setQR(code, ev string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.currentQR = code
	a.lastQREvent = ev
}

func (a *App) qr() (string, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.currentQR, a.lastQREvent
}

func (a *App) publish(e Event) {
	a.busMu.Lock()
	a.recent = append(a.recent, e)
	if len(a.recent) > 100 {
		a.recent = a.recent[len(a.recent)-100:]
	}
	subs := make([]chan Event, 0, len(a.subs))
	for ch := range a.subs {
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

func (a *App) subscribe() (chan Event, []Event, func()) {
	ch := make(chan Event, 16)
	a.busMu.Lock()
	a.subs[ch] = struct{}{}
	snapshot := append([]Event(nil), a.recent...)
	a.busMu.Unlock()
	return ch, snapshot, func() {
		a.busMu.Lock()
		delete(a.subs, ch)
		a.busMu.Unlock()
		close(ch)
	}
}

func main() {
	storePath := getenv("STORE_PATH", "./data/store.db")
	httpAddr := getenv("HTTP_ADDR", ":8080")

	if os.Getenv("HTTP_ONLY") == "1" {
		app := newApp(nil)
		configureAgent(app)
		log.Printf("HTTP_ONLY=1: skipping WhatsApp client; serving admin UI only")
		app.serveHTTP(httpAddr)
		return
	}

	if err := os.MkdirAll(dirOf(storePath), 0o755); err != nil {
		log.Fatalf("create store dir: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dsn := fmt.Sprintf("file:%s?_foreign_keys=on", storePath)
	container, err := sqlstore.New(ctx, "sqlite3", dsn, waLog.Stdout("DB", "INFO", true))
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer container.Close()

	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		log.Fatalf("get device: %v", err)
	}

	client := whatsmeow.NewClient(device, waLog.Stdout("Client", "INFO", true))
	app := newApp(client)
	configureAgent(app)
	client.AddEventHandler(app.handleEvent)

	if client.Store.ID == nil {
		qrChan, err := client.GetQRChannel(ctx)
		if err != nil {
			log.Fatalf("get qr channel: %v", err)
		}
		if err := client.Connect(); err != nil {
			log.Fatalf("connect: %v", err)
		}
		go app.consumeQR(qrChan)
	} else {
		if err := client.Connect(); err != nil {
			log.Fatalf("reconnect: %v", err)
		}
		fmt.Println("Reconnected as", client.Store.ID.String())
	}

	go app.serveHTTP(httpAddr)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	<-sigs
	fmt.Println("Shutting down...")
	client.Disconnect()
}

func (a *App) consumeQR(qrChan <-chan whatsmeow.QRChannelItem) {
	for evt := range qrChan {
		switch evt.Event {
		case "code":
			a.setQR(evt.Code, "code")
			fmt.Println("New QR code available at /")
		case "success":
			a.setQR("", "success")
			fmt.Println("Paired successfully.")
		default:
			a.setQR("", evt.Event)
			fmt.Println("QR event:", evt.Event)
		}
	}
}

func (a *App) handleEvent(evt interface{}) {
	msg, ok := evt.(*events.Message)
	if !ok {
		log.Printf("event: %T", evt)
		return
	}
	log.Printf("message: chat=%s sender=%s fromMe=%v type=%s", msg.Info.Chat, msg.Info.Sender, msg.Info.IsFromMe, msg.Info.Type)
	if msg.Info.IsFromMe {
		return
	}
	switch msg.Info.Chat.Server {
	case types.DefaultUserServer, types.HiddenUserServer:
	default:
		log.Printf("skipping non-1:1 chat server=%s", msg.Info.Chat.Server)
		return
	}

	body := msg.Message.GetConversation()
	if body == "" && msg.Message.GetExtendedTextMessage() != nil {
		body = msg.Message.GetExtendedTextMessage().GetText()
	}
	a.publish(Event{Time: time.Now(), Dir: "in", Chat: msg.Info.Sender.String(), Body: body})

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	replyText, ok, err := a.reply(ctx, msg.Info.Chat.String(), body)
	if err != nil {
		log.Printf("reply (%s mode): %v", a.mode(), err)
		return
	}
	if !ok {
		log.Printf("no reply for %s in %s mode", msg.Info.Chat, a.mode())
		return
	}
	reply := &waProto.Message{Conversation: proto.String(replyText)}
	if _, err := a.client.SendMessage(ctx, msg.Info.Chat, reply); err != nil {
		log.Printf("send reply: %v", err)
		return
	}
	a.publish(Event{Time: time.Now(), Dir: "out", Chat: msg.Info.Chat.String(), Body: replyText})
}

const defaultSystemPrompt = `You are Chalagente, a friendly WhatsApp customer-service assistant for a small business. Reply concisely (1-3 sentences), in the same language the customer uses (default Spanish). Be warm, professional, and direct.`

func configureAgent(app *App) {
	systemPrompt := getenv("AGENT_SYSTEM_PROMPT", defaultSystemPrompt)
	var chain []Replier

	if tok := os.Getenv("AWS_BEARER_TOKEN_BEDROCK"); tok != "" {
		region := getenv("AWS_REGION", "us-east-1")
		model := getenv("BEDROCK_MODEL_ID", "us.anthropic.claude-haiku-4-5-20251001-v1:0")
		endpoint := getenv("BEDROCK_ENDPOINT", fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", region))
		chain = append(chain, newBedrockReplier(endpoint, tok, model, systemPrompt))
		log.Printf("bedrock agent enabled (model=%s)", model)
	}
	if tok := os.Getenv("MISTRAL_API_KEY"); tok != "" {
		model := getenv("MISTRAL_MODEL_ID", "mistral-small-latest")
		endpoint := getenv("MISTRAL_ENDPOINT", "https://api.mistral.ai")
		chain = append(chain, newMistralReplier(endpoint, tok, model, systemPrompt))
		log.Printf("mistral fallback enabled (model=%s)", model)
	}

	switch len(chain) {
	case 0:
		log.Printf("no agent provider configured (set AWS_BEARER_TOKEN_BEDROCK or MISTRAL_API_KEY); agent mode unavailable")
		return
	case 1:
		app.agent = chain[0]
	default:
		app.agent = &fallbackReplier{replier: chain}
	}
	if os.Getenv("DEFAULT_MODE") == "agent" {
		_ = app.setMode("agent")
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}
