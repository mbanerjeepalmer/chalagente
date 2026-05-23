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
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

const autoReply = "¡Hola! Gracias por escribir. Esta es una respuesta automática del POC de Chalagente."

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
}

func newApp(client *whatsmeow.Client) *App {
	return &App{
		client: client,
		subs:   make(map[chan Event]struct{}),
	}
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
	if msg.Info.Chat.Server != "s.whatsapp.net" {
		log.Printf("skipping non-1:1 chat server=%s", msg.Info.Chat.Server)
		return
	}

	body := msg.Message.GetConversation()
	if body == "" && msg.Message.GetExtendedTextMessage() != nil {
		body = msg.Message.GetExtendedTextMessage().GetText()
	}
	a.publish(Event{Time: time.Now(), Dir: "in", Chat: msg.Info.Sender.String(), Body: body})

	reply := &waProto.Message{Conversation: proto.String(autoReply)}
	if _, err := a.client.SendMessage(context.Background(), msg.Info.Chat, reply); err != nil {
		log.Printf("send reply: %v", err)
		return
	}
	a.publish(Event{Time: time.Now(), Dir: "out", Chat: msg.Info.Chat.String(), Body: autoReply})
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
