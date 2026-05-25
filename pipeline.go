package main

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/mbanerjeepalmer/chalagente/internal/agent"
	"github.com/mbanerjeepalmer/chalagente/internal/store"

	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// handleWAEvent is the wamanager EventHandler. It runs in the manager's
// goroutine; keep it non-blocking for the simple cases and detach the agent
// pipeline into its own goroutine.
func (a *App) handleWAEvent(businessID string, evt any) {
	msg, ok := evt.(*events.Message)
	if !ok {
		return
	}
	if msg.Info.IsFromMe {
		return
	}
	switch msg.Info.Chat.Server {
	case types.DefaultUserServer, types.HiddenUserServer:
	default:
		return
	}
	go a.processIncoming(businessID, msg)
}

func (a *App) processIncoming(businessID string, msg *events.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body, kind := extractMessage(msg)

	biz, err := a.Store.GetBusiness(ctx, businessID)
	if err != nil {
		log.Printf("pipeline: get business %s: %v", businessID, err)
		return
	}
	convo, err := a.Store.UpsertConversation(ctx, businessID, msg.Info.Chat.String())
	if err != nil {
		log.Printf("pipeline: upsert conversation: %v", err)
		return
	}

	// Voice STT pre-step (interface layer, not the agent's problem).
	if kind == "audio" && body == "" {
		body = "[nota de voz recibida]"
		// Real audio bytes would be downloaded via client.Download here; in
		// dev / no-keys mode we just feed a placeholder. Real STT lands when
		// ElevenLabsProvider gets an API key.
	}

	inMsg := store.Message{
		Direction: "in",
		Kind:      kind,
		Body:      body,
	}
	if err := a.Store.AppendMessage(ctx, convo.ID, inMsg); err != nil {
		log.Printf("pipeline: append in: %v", err)
	}
	a.publish(Event{
		BusinessID: businessID,
		Time:       time.Now(),
		Dir:        "in",
		Chat:       msg.Info.Sender.String(),
		Body:       body,
		Kind:       kind,
	})

	if !biz.AgentEnabled {
		log.Printf("pipeline: agent disabled for %s; logged only", businessID)
		return
	}

	history, err := a.Store.ListMessages(ctx, convo.ID, 20)
	if err != nil {
		log.Printf("pipeline: list history: %v", err)
	}

	if !chatHasTrigger(history, body) {
		return
	}

	bc := businessContextFor(biz)
	req := agent.Request{
		SystemPrompt: agent.BuildSystemPrompt(bc),
		History:      historyToAgent(history),
		Incoming: agent.Message{
			Role:      agent.RoleUser,
			Text:      body,
			Timestamp: time.Now(),
		},
		Business: bc,
	}
	if kind != "text" {
		req.Incoming.Attachments = []agent.Attachment{{Kind: kind, Ref: msg.Info.ID}}
	}

	reply, err := a.Agent.Respond(ctx, req)
	if err != nil {
		log.Printf("pipeline: agent respond: %v", err)
		return
	}
	if strings.TrimSpace(reply.Text) == "" {
		return
	}

	if err := a.sendText(ctx, businessID, msg.Info.Chat, reply.Text); err != nil {
		log.Printf("pipeline: send reply: %v", err)
		return
	}
	if err := a.Store.AppendMessage(ctx, convo.ID, store.Message{
		Direction: "out", Kind: "text", Body: reply.Text,
	}); err != nil {
		log.Printf("pipeline: append out: %v", err)
	}
	a.publish(Event{
		BusinessID: businessID,
		Time:       time.Now(),
		Dir:        "out",
		Chat:       msg.Info.Chat.String(),
		Body:       reply.Text,
		Kind:       "text",
	})
}

// triggerKeyword gates the agent: it only replies when this word appears
// somewhere in the conversation (history or the incoming message).
const triggerKeyword = "chalagente"

func chatHasTrigger(history []store.Message, incoming string) bool {
	if containsTrigger(incoming) {
		return true
	}
	for _, m := range history {
		if containsTrigger(m.Body) {
			return true
		}
	}
	return false
}

func containsTrigger(s string) bool {
	return strings.Contains(strings.ToLower(s), triggerKeyword)
}

func extractMessage(msg *events.Message) (body, kind string) {
	if t := msg.Message.GetConversation(); t != "" {
		return t, "text"
	}
	if e := msg.Message.GetExtendedTextMessage(); e != nil && e.GetText() != "" {
		return e.GetText(), "text"
	}
	if msg.Message.GetAudioMessage() != nil {
		return "", "audio"
	}
	if im := msg.Message.GetImageMessage(); im != nil {
		return im.GetCaption(), "image"
	}
	if v := msg.Message.GetVideoMessage(); v != nil {
		return v.GetCaption(), "video"
	}
	return "", "text"
}

func historyToAgent(msgs []store.Message) []agent.Message {
	// store.ListMessages returns newest-first; agent wants oldest-first.
	out := make([]agent.Message, 0, len(msgs))
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		role := agent.RoleUser
		if m.Direction == "out" {
			role = agent.RoleAssistant
		}
		out = append(out, agent.Message{
			Role:      role,
			Text:      m.Body,
			Timestamp: m.CreatedAt,
		})
	}
	return out
}

func businessContextFor(b store.Business) agent.BusinessContext {
	return agent.BusinessContext{
		Name:      b.Name,
		Address:   b.Address,
		Phone:     b.Phone,
		Website:   b.Website,
		Hours:     b.Hours,
		ExtraInfo: b.ExtraInfo,
		Now:       time.Now(),
	}
}

func (a *App) sendText(ctx context.Context, businessID string, to types.JID, text string) error {
	client, ok := a.WAMgr.Client(businessID)
	if !ok {
		return errors.New("no client for business")
	}
	msg := &waProto.Message{Conversation: proto.String(text)}
	_, err := client.SendMessage(ctx, to, msg)
	return err
}
