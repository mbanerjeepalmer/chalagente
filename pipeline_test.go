package main

import (
	"testing"

	"github.com/mbanerjeepalmer/chalagente/internal/store"
)

func TestVoiceIDForLang(t *testing.T) {
	const builtinDefault = "21m00Tcm4TlvDq8ikWAM"
	t.Setenv("ELEVENLABS_VOICE_DEFAULT", "")
	t.Setenv("ELEVENLABS_VOICE_ES", "")
	t.Setenv("ELEVENLABS_VOICE_EN", "")
	if got := voiceIDForLang(""); got != builtinDefault {
		t.Errorf("blank lang: got %q want builtin %q", got, builtinDefault)
	}
	if got := voiceIDForLang("es-MX"); got != builtinDefault {
		t.Errorf("es-MX with no override: got %q", got)
	}
	t.Setenv("ELEVENLABS_VOICE_DEFAULT", "fallback")
	t.Setenv("ELEVENLABS_VOICE_ES", "spanish")
	if got := voiceIDForLang("es"); got != "spanish" {
		t.Errorf("es with override: got %q want spanish", got)
	}
	if got := voiceIDForLang("en"); got != "fallback" {
		t.Errorf("en without lang override falls to DEFAULT: got %q want fallback", got)
	}
}

func TestChatHasTrigger(t *testing.T) {
	tests := []struct {
		name     string
		history  []store.Message
		incoming string
		want     bool
	}{
		{
			name:     "incoming contains keyword",
			incoming: "Hola Chalagente, ¿qué tal?",
			want:     true,
		},
		{
			name:     "incoming contains keyword lowercase",
			incoming: "oye chalagente ayúdame",
			want:     true,
		},
		{
			name:     "incoming contains keyword uppercase with punctuation",
			incoming: "¡CHALAGENTE! responde",
			want:     true,
		},
		{
			name:     "no keyword anywhere",
			incoming: "Hola, ¿cuánto cuesta la birria?",
			history: []store.Message{
				{Direction: "in", Body: "Buen día"},
				{Direction: "out", Body: "¡Hola!"},
			},
			want: false,
		},
		{
			name:     "keyword appeared earlier in history (incoming)",
			incoming: "¿Y los horarios?",
			history: []store.Message{
				{Direction: "in", Body: "Hola Chalagente"},
				{Direction: "out", Body: "¡Hola! ¿en qué te ayudo?"},
			},
			want: true,
		},
		{
			name:     "keyword only in assistant reply counts (already engaged)",
			incoming: "gracias",
			history: []store.Message{
				{Direction: "in", Body: "hola"},
				{Direction: "out", Body: "Soy Chalagente, tu asistente."},
			},
			want: true,
		},
		{
			name:     "empty history and empty incoming",
			incoming: "",
			want:     false,
		},
		{
			name:     "substring inside another word still matches",
			incoming: "prechalagentepost",
			want:     true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := chatHasTrigger(tc.history, tc.incoming)
			if got != tc.want {
				t.Fatalf("chatHasTrigger(...) = %v, want %v", got, tc.want)
			}
		})
	}
}
