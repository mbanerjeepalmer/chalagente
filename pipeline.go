package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"github.com/mbanerjeepalmer/chalagente/internal/agent"
	"github.com/mbanerjeepalmer/chalagente/internal/store"

	"go.mau.fi/whatsmeow"
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
	// We transcribe with ElevenLabs so the agent sees plain text and the
	// trigger gate / history viewer have something searchable. The
	// transcript also drives the reply language detection downstream.
	var (
		wasAudio     bool
		audioMime    string
		audioLang    string // BCP-47-ish, from STT
	)
	if kind == "audio" {
		wasAudio = true
		audioMime = "audio/ogg"
		if am := msg.Message.GetAudioMessage(); am != nil {
			if mt := am.GetMimetype(); mt != "" {
				audioMime = mt
			}
			if client, ok := a.WAMgr.Client(businessID); ok {
				data, derr := client.Download(ctx, am)
				if derr != nil {
					log.Printf("pipeline: download audio %s: %v", msg.Info.ID, derr)
				} else {
					tr, terr := a.Voice.Transcribe(ctx, data, audioMime)
					if terr != nil {
						log.Printf("pipeline: transcribe %s: %v", msg.Info.ID, terr)
					} else {
						body = tr.Text
						audioLang = tr.Language
					}
				}
			}
		}
		if body == "" {
			body = "[nota de voz recibida]"
		}
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
	if !convo.AgentEnabled {
		log.Printf("pipeline: agent disabled for conversation %s; logged only", convo.ID)
		return
	}

	history, err := a.Store.ListMessages(ctx, convo.ID, 20)
	if err != nil {
		log.Printf("pipeline: list history: %v", err)
	}

	if biz.TriggerRequired && !chatHasTrigger(history, body) {
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
		att := agent.Attachment{Kind: kind, Ref: msg.Info.ID}
		if kind == "image" {
			if im := msg.Message.GetImageMessage(); im != nil {
				if client, ok := a.WAMgr.Client(businessID); ok {
					data, derr := client.Download(ctx, im)
					if derr == nil {
						att.Bytes = data
						att.MimeType = im.GetMimetype()
					} else {
						log.Printf("pipeline: download image %s: %v", msg.Info.ID, derr)
					}
				}
			}
		}
		req.Incoming.Attachments = []agent.Attachment{att}
	}

	reply, err := a.Agent.Respond(ctx, req)
	if err != nil {
		log.Printf("pipeline: agent respond: %v", err)
		return
	}
	if strings.TrimSpace(reply.Text) == "" {
		return
	}

	outKind := "text"
	if wasAudio {
		// Spec: "always respond to voice with voice, text with text."
		// Synthesize with ElevenLabs using a per-language default voice,
		// then upload + send a WhatsApp audio message with PTT=true so it
		// renders as a real voice note in the recipient's chat.
		if err := a.sendVoiceReply(ctx, businessID, msg.Info.Chat, reply.Text, audioLang); err != nil {
			log.Printf("pipeline: send voice reply: %v; falling back to text", err)
			if err2 := a.sendText(ctx, businessID, msg.Info.Chat, reply.Text); err2 != nil {
				log.Printf("pipeline: send text fallback: %v", err2)
				return
			}
		} else {
			outKind = "audio"
		}
	} else {
		if err := a.sendText(ctx, businessID, msg.Info.Chat, reply.Text); err != nil {
			log.Printf("pipeline: send reply: %v", err)
			return
		}
	}
	if err := a.Store.AppendMessage(ctx, convo.ID, store.Message{
		Direction: "out", Kind: outKind, Body: reply.Text,
	}); err != nil {
		log.Printf("pipeline: append out: %v", err)
	}
	a.publish(Event{
		BusinessID: businessID,
		Time:       time.Now(),
		Dir:        "out",
		Chat:       msg.Info.Chat.String(),
		Body:       reply.Text,
		Kind:       outKind,
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

// sendVoiceReply synthesizes text via the configured voice.Provider, uploads
// the resulting audio bytes through whatsmeow, and dispatches a WhatsApp
// AudioMessage with PTT=true so the customer sees a voice-note bubble. Any
// failure returns an error so the caller can fall back to plain text.
func (a *App) sendVoiceReply(ctx context.Context, businessID string, to types.JID, text, lang string) error {
	client, ok := a.WAMgr.Client(businessID)
	if !ok {
		return errors.New("no client for business")
	}
	voiceID := voiceIDForLang(lang)
	syn, err := a.Voice.Synthesize(ctx, text, voiceID)
	if err != nil {
		return err
	}
	if len(syn.Audio) == 0 {
		return errors.New("empty synthesized audio")
	}
	uploaded, err := client.Upload(ctx, syn.Audio, whatsmeow.MediaAudio)
	if err != nil {
		return err
	}
	mime := syn.MimeType
	if mime == "" {
		mime = "audio/mpeg"
	}
	audio := &waProto.AudioMessage{
		URL:           proto.String(uploaded.URL),
		DirectPath:    proto.String(uploaded.DirectPath),
		Mimetype:      proto.String(mime),
		MediaKey:      uploaded.MediaKey,
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uploaded.FileLength),
		PTT:           proto.Bool(true),
	}
	if _, err := client.SendMessage(ctx, to, &waProto.Message{AudioMessage: audio}); err != nil {
		return err
	}
	return nil
}

// voiceIDForLang picks the ElevenLabs voice ID for a detected language. The
// refining-notes answer was 'just use a default voice per language', so the
// mapping is intentionally tiny — a single env-overridable default plus a
// per-language override. ElevenLabs' multilingual TTS handles the actual
// language switching at synthesis time.
func voiceIDForLang(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if len(lang) > 2 {
		lang = lang[:2] // "es-MX" → "es"
	}
	if lang != "" {
		if v := os.Getenv("ELEVENLABS_VOICE_" + strings.ToUpper(lang)); v != "" {
			return v
		}
	}
	if v := os.Getenv("ELEVENLABS_VOICE_DEFAULT"); v != "" {
		return v
	}
	// Rachel — ElevenLabs' default multilingual voice. Override per-tenant
	// via env vars; we intentionally don't make this per-business yet.
	return "21m00Tcm4TlvDq8ikWAM"
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
