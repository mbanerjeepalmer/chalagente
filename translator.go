package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mbanerjeepalmer/chalagente/internal/agent"
)

// supportedPrefillLangs is the curated set of BCP-47 language codes we ask
// the LLM to translate prefill copy into. Anthropic's Claude models have
// strong coverage for these; the set is intentionally small so the LLM
// stays in a single short call. The source template itself is stored on
// the business — typically English — and used as the fallback when no
// matching translation is available.
var supportedPrefillLangs = []string{
	"es", // Spanish
	"pt", // Portuguese
	"fr", // French
	"de", // German
	"it", // Italian
	"zh", // Chinese (Simplified)
	"ja", // Japanese
	"ko", // Korean
	"hi", // Hindi
	"ar", // Arabic
	"ru", // Russian
	"nl", // Dutch
}

// Translator turns a single short source string into translations for the
// requested language codes. Returning ("", nil) for a language is fine —
// callers fall back to the source.
type Translator func(ctx context.Context, source string, langs []string) (map[string]string, error)

// agentTranslator returns a Translator that drives an agent.Engine. The
// engine value carries the LLM fallback chain (Bedrock → Mistral → Mock)
// from buildAgent, so this stays a thin adapter — no second hierarchy.
// When engine is nil the returned function is a no-op.
func agentTranslator(engine agent.Engine) Translator {
	if engine == nil {
		return func(context.Context, string, []string) (map[string]string, error) {
			return map[string]string{}, nil
		}
	}
	return func(ctx context.Context, source string, langs []string) (map[string]string, error) {
		source = strings.TrimSpace(source)
		if source == "" || len(langs) == 0 {
			return map[string]string{}, nil
		}
		prompt := fmt.Sprintf(
			"Source (English): %q\nTarget languages: %s\nReturn JSON like {\"es\":\"...\",\"pt\":\"...\"}.",
			source, strings.Join(langs, ", "),
		)
		reply, err := engine.Respond(ctx, agent.Request{
			SystemPrompt: translatorSystemPrompt,
			Incoming:     agent.Message{Role: agent.RoleUser, Text: prompt},
		})
		if err != nil {
			return nil, fmt.Errorf("translate: %w", err)
		}
		out, err := parseTranslationJSON(reply.Text)
		if err != nil {
			return nil, fmt.Errorf("translate parse: %w", err)
		}
		keep := make(map[string]struct{}, len(langs))
		for _, l := range langs {
			keep[l] = struct{}{}
		}
		for k := range out {
			if _, ok := keep[k]; !ok {
				delete(out, k)
			}
		}
		return out, nil
	}
}

const translatorSystemPrompt = `You are a translator for short WhatsApp greeting copy.
Output ONLY a JSON object mapping each requested BCP-47 language code to the
translated string. Preserve any placeholders like {business} verbatim.
Preserve any literal proper nouns or brand names. Keep the tone short, casual,
suitable for a customer sending a WhatsApp message to a small business.
Do not add commentary, code fences, or extra keys.`

// parseTranslationJSON pulls the first balanced JSON object out of raw and
// decodes it. Models sometimes wrap the JSON in prose or markdown fences
// despite instructions; this tolerates that.
func parseTranslationJSON(raw string) (map[string]string, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("no JSON object in response: %q", truncateForLog(raw, 200))
	}
	body := raw[start : end+1]
	out := map[string]string{}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, fmt.Errorf("unmarshal %q: %w", truncateForLog(body, 200), err)
	}
	return out, nil
}

func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
