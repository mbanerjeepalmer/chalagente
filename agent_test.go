package main

import "testing"

func TestSelectAgentResponse(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		intent string
		audio  string
	}{
		{
			name:   "price with accents",
			input:  "Hola, cuanto cuesta y como son los pagos?",
			intent: "precio",
			audio:  "assets/audio/01-precio.mp3",
		},
		{
			name:   "included package",
			input:  "Que incluye el paquete?",
			intent: "que_incluye",
			audio:  "assets/audio/02-que-incluye.mp3",
		},
		{
			name:   "availability",
			input:  "Todavia quedan lugares disponibles?",
			intent: "disponibilidad",
			audio:  "assets/audio/06-disponibilidad.mp3",
		},
		{
			name:   "fallback",
			input:  "Hola",
			intent: "fallback",
			audio:  "assets/audio/07-cta-cierre.mp3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectAgentResponse(tt.input)
			if got.Intent != tt.intent {
				t.Fatalf("intent = %q, want %q", got.Intent, tt.intent)
			}
			if got.AudioPath != tt.audio {
				t.Fatalf("audio = %q, want %q", got.AudioPath, tt.audio)
			}
			if got.Text == "" {
				t.Fatal("expected response text")
			}
		})
	}
}
