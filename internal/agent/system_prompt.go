package agent

import (
	"strings"
)

// BuildSystemPrompt renders the per-tenant system prompt from a
// BusinessContext. It is a pure function (no I/O) so it can be unit-tested by
// asserting on substrings, and so it's cheap to call on every request — the
// caller may cache it if rendering ever shows up in a profile.
//
// The prompt is in Spanish: the v1 product targets Spanish-speaking customers.
// Empty fields are omitted entirely (no orphan "Dirección: " lines).
func BuildSystemPrompt(b BusinessContext) string {
	var sb strings.Builder

	name := strings.TrimSpace(b.Name)
	if name == "" {
		name = "este negocio"
	}

	sb.WriteString("Eres el asistente de WhatsApp de ")
	sb.WriteString(name)
	sb.WriteString(".\n\n")

	// Business basics — only non-empty fields, as a bulleted block.
	var bullets []string
	if v := strings.TrimSpace(b.Address); v != "" {
		bullets = append(bullets, "Dirección: "+v)
	}
	if v := strings.TrimSpace(b.Phone); v != "" {
		bullets = append(bullets, "Teléfono: "+v)
	}
	if v := strings.TrimSpace(b.Website); v != "" {
		bullets = append(bullets, "Sitio web: "+v)
	}
	if cats := nonEmpty(b.Categories); len(cats) > 0 {
		bullets = append(bullets, "Categorías: "+strings.Join(cats, ", "))
	}
	if v := strings.TrimSpace(b.Hours); v != "" {
		bullets = append(bullets, "Horario: "+v)
	}
	if len(bullets) > 0 {
		sb.WriteString("Datos del negocio:\n")
		for _, line := range bullets {
			sb.WriteString("- ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if v := strings.TrimSpace(b.ExtraInfo); v != "" {
		sb.WriteString("Información adicional:\n")
		sb.WriteString(v)
		sb.WriteString("\n\n")
	}

	if !b.Now.IsZero() {
		sb.WriteString("Hoy es ")
		sb.WriteString(b.Now.Format("2006-01-02"))
		sb.WriteString(".\n\n")
	}

	if len(b.Tools) > 0 {
		sb.WriteString("Herramientas disponibles:\n")
		for _, t := range b.Tools {
			sb.WriteString("- ")
			sb.WriteString(t.Key)
			if d := strings.TrimSpace(t.Description); d != "" {
				sb.WriteString(": ")
				sb.WriteString(d)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Behavior rules. Keep this block stable — tests assert on its keywords.
	sb.WriteString("Reglas de comportamiento:\n")
	sb.WriteString("- Responde de forma breve y clara, optimizado para WhatsApp (1-3 frases cuando sea posible).\n")
	sb.WriteString("- Responde siempre en el mismo idioma que use el cliente.\n")
	sb.WriteString("- No inventes datos: si no tienes la información, dilo y ofrece ponerla en contacto con una persona del equipo.\n")
	sb.WriteString("- Si el cliente pide algo fuera de tu alcance, escala a un humano del negocio.\n")
	sb.WriteString("- Sé cordial y profesional; usa el tono natural del negocio.\n")

	return sb.String()
}

func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}
