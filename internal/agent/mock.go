package agent

import (
	"context"
	"strings"
)

// MockEngine is a deterministic Engine implementation used by tests and by
// local development when no real LLM provider is configured. It pattern-matches
// the incoming message against a small set of keyword rules so that callers
// can assert on exact outputs.
//
// MockEngine is intentionally simple — it isn't trying to be a good assistant,
// just a predictable one.
type MockEngine struct {
	bc BusinessContext
}

// NewMockEngine returns a MockEngine with no business context. Replies that
// would normally reference business info (hours, address) fall back to a
// generic acknowledgement.
func NewMockEngine() *MockEngine {
	return &MockEngine{}
}

// NewMockEngineWithContext returns a MockEngine that knows the business
// details. Use this in tests that need to assert the mock surfaces, e.g.,
// the configured address or hours.
func NewMockEngineWithContext(bc BusinessContext) *MockEngine {
	return &MockEngine{bc: bc}
}

// Respond implements Engine.
func (m *MockEngine) Respond(ctx context.Context, req Request) (Reply, error) {
	in := req.Incoming
	text := strings.ToLower(strings.TrimSpace(in.Text))
	hasQuestion := strings.Contains(in.Text, "?") || strings.Contains(in.Text, "¿")

	// Rule 1: hours.
	if hasQuestion && containsAny(text, "hora", "horario", "abren", "cierran") {
		if h := strings.TrimSpace(m.bc.Hours); h != "" {
			return Reply{Text: "Nuestro horario es: " + h}, nil
		}
		return Reply{Text: "Te confirmo el horario en un momento."}, nil
	}

	// Rule 2: location.
	if containsAny(text, "dirección", "direccion", "donde", "dónde", "ubicación", "ubicacion") {
		if a := strings.TrimSpace(m.bc.Address); a != "" {
			return Reply{Text: "Estamos en " + a + "."}, nil
		}
		return Reply{Text: "Te confirmo la dirección en un momento."}, nil
	}

	// Rule 3: price.
	if containsAny(text, "precio", "cuánto", "cuanto", "cuesta", "costo") {
		return Reply{Text: "Déjame revisar el precio y te confirmo."}, nil
	}

	// Rule 4: audio attachment with empty text.
	if in.Text == "" && hasAttachmentOfKind(in.Attachments, "audio") {
		return Reply{Text: "Recibí tu nota de voz, te respondo en un momento."}, nil
	}

	// Rule 5: image attachment.
	if hasAttachmentOfKind(in.Attachments, "image") {
		return Reply{Text: "Recibí tu imagen. ¿Me cuentas qué necesitas?"}, nil
	}

	// Rule 6: default greeting, optionally quoting the user's text.
	greeting := "Hola, gracias por tu mensaje. ¿Cómo te puedo ayudar?"
	if in.Text != "" {
		snippet := in.Text
		if len(snippet) > 60 {
			snippet = snippet[:60]
		}
		greeting = greeting + " (Recibí: \"" + snippet + "\")"
	}
	return Reply{Text: greeting}, nil
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func hasAttachmentOfKind(atts []Attachment, kind string) bool {
	for _, a := range atts {
		if a.Kind == kind {
			return true
		}
	}
	return false
}
