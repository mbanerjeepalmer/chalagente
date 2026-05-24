package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

func fullContext() BusinessContext {
	return BusinessContext{
		Name:       "Café Luna",
		Address:    "Calle Falsa 123, Madrid",
		Phone:      "+34 600 123 456",
		Website:    "https://cafeluna.example",
		Categories: []string{"Cafetería", "Desayunos"},
		Hours:      "Lun-Vie 8:00-20:00, Sáb-Dom 9:00-14:00",
		ExtraInfo:  "Tenemos opciones veganas y wifi gratis.",
		Now:        time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC),
		Tools: []ToolSpec{
			{Key: "reservations", Description: "Crear una reserva en el sistema."},
		},
	}
}

func TestBuildSystemPromptIncludesAllFieldsWhenPresent(t *testing.T) {
	prompt := BuildSystemPrompt(fullContext())

	wants := []string{
		"Café Luna",
		"Calle Falsa 123, Madrid",
		"+34 600 123 456",
		"https://cafeluna.example",
		"Cafetería",
		"Desayunos",
		"Lun-Vie 8:00-20:00, Sáb-Dom 9:00-14:00",
		"Tenemos opciones veganas y wifi gratis.",
		"Información adicional:",
		"Hoy es 2026-05-24",
		"reservations",
		"Crear una reserva en el sistema.",
	}
	for _, w := range wants {
		if !strings.Contains(prompt, w) {
			t.Errorf("expected prompt to contain %q\n--- prompt ---\n%s", w, prompt)
		}
	}
}

func TestBuildSystemPromptOmitsMissingFieldsGracefully(t *testing.T) {
	bc := BusinessContext{
		Name: "Tienda X",
		Now:  time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	prompt := BuildSystemPrompt(bc)

	// Must include
	if !strings.Contains(prompt, "Tienda X") {
		t.Errorf("expected prompt to contain business name")
	}
	if !strings.Contains(prompt, "Hoy es 2026-01-02") {
		t.Errorf("expected prompt to contain formatted date, got:\n%s", prompt)
	}

	// Must NOT include empty bullets / labels for absent fields
	bad := []string{
		"Dirección:",
		"Dirección: ",
		"Teléfono:",
		"Sitio web:",
		"Categorías:",
		"Horario:",
		"Información adicional:",
		"Herramientas disponibles:",
	}
	for _, b := range bad {
		if strings.Contains(prompt, b) {
			t.Errorf("expected prompt to NOT contain %q (no value provided)\n--- prompt ---\n%s", b, prompt)
		}
	}
}

func TestBuildSystemPromptIncludesBehaviorRules(t *testing.T) {
	prompt := BuildSystemPrompt(fullContext())
	lower := strings.ToLower(prompt)

	// Behavior expectations: brevity for WhatsApp, language matching, no invention, escalation.
	checks := []string{"whatsapp", "idioma", "invent", "human"}
	for _, c := range checks {
		if !strings.Contains(lower, c) {
			t.Errorf("expected behavior rules to mention %q\n--- prompt ---\n%s", c, prompt)
		}
	}
}

func TestMockEngineHoursQuestion(t *testing.T) {
	bc := fullContext()
	eng := NewMockEngineWithContext(bc)
	prompt := BuildSystemPrompt(bc)

	req := Request{
		SystemPrompt: prompt,
		Incoming: Message{
			Role:      RoleUser,
			Text:      "Hola, ¿a qué hora abren mañana?",
			Timestamp: time.Now(),
		},
	}
	reply, err := eng.Respond(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(reply.Text, bc.Hours) {
		t.Errorf("expected reply to include hours %q, got: %s", bc.Hours, reply.Text)
	}
}

func TestMockEngineHoursQuestionNoHoursConfigured(t *testing.T) {
	eng := NewMockEngine()
	req := Request{
		Incoming: Message{
			Role: RoleUser,
			Text: "¿Cuál es el horario?",
		},
	}
	reply, err := eng.Respond(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(reply.Text), "horario") {
		t.Errorf("expected fallback to mention horario, got: %s", reply.Text)
	}
}

func TestMockEngineLocationQuestion(t *testing.T) {
	eng := NewMockEngine()
	bc := fullContext()
	req := Request{
		SystemPrompt: BuildSystemPrompt(bc),
		Incoming: Message{
			Role: RoleUser,
			Text: "¿Dónde están ubicados?",
		},
	}
	// MockEngine needs the business context to know the address; pass it via the request.
	// The engine inspects the BusinessContext through a setter or constructor.
	eng = NewMockEngineWithContext(bc)
	reply, err := eng.Respond(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(reply.Text, bc.Address) {
		t.Errorf("expected reply to include address %q, got: %s", bc.Address, reply.Text)
	}
}

func TestMockEnginePriceQuestion(t *testing.T) {
	eng := NewMockEngine()
	req := Request{
		Incoming: Message{
			Role: RoleUser,
			Text: "¿Cuánto cuesta el café?",
		},
	}
	reply, err := eng.Respond(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(reply.Text), "precio") {
		t.Errorf("expected price-related reply, got: %s", reply.Text)
	}
}

func TestMockEngineAudioAttachmentNoText(t *testing.T) {
	eng := NewMockEngine()
	req := Request{
		Incoming: Message{
			Role: RoleUser,
			Text: "",
			Attachments: []Attachment{
				{Kind: "audio", MimeType: "audio/ogg", Ref: "blob://abc"},
			},
		},
	}
	reply, err := eng.Respond(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(reply.Text), "nota de voz") {
		t.Errorf("expected voice-note acknowledgement, got: %s", reply.Text)
	}
}

func TestMockEngineImageAttachment(t *testing.T) {
	eng := NewMockEngine()
	req := Request{
		Incoming: Message{
			Role: RoleUser,
			Text: "",
			Attachments: []Attachment{
				{Kind: "image", MimeType: "image/jpeg", Ref: "blob://img1"},
			},
		},
	}
	reply, err := eng.Respond(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(reply.Text), "imagen") {
		t.Errorf("expected image acknowledgement, got: %s", reply.Text)
	}
}

func TestMockEngineDefaultQuotesUserText(t *testing.T) {
	eng := NewMockEngine()
	userText := "Quería preguntar si tienen mesa para cuatro personas el viernes por la noche."
	req := Request{
		Incoming: Message{
			Role: RoleUser,
			Text: userText,
		},
	}
	reply, err := eng.Respond(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default greeting present
	if !strings.Contains(strings.ToLower(reply.Text), "hola") {
		t.Errorf("expected default greeting, got: %s", reply.Text)
	}
	// First 60 chars of user text should be quoted somewhere in the reply.
	snippet := userText
	if len(snippet) > 60 {
		snippet = snippet[:60]
	}
	if !strings.Contains(reply.Text, snippet) {
		t.Errorf("expected reply to quote user text snippet %q, got: %s", snippet, reply.Text)
	}
}

func TestMockEngineDefaultNoText(t *testing.T) {
	eng := NewMockEngine()
	req := Request{
		Incoming: Message{
			Role: RoleUser,
			Text: "",
		},
	}
	reply, err := eng.Respond(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply.Text == "" {
		t.Errorf("expected a non-empty default reply for empty input")
	}
}

// Ensure MockEngine satisfies Engine interface.
var _ Engine = (*MockEngine)(nil)
