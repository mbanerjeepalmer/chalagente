package main

import (
	"strings"
)

type AgentResponse struct {
	Intent    string
	Text      string
	AudioPath string
}

type intentRoute struct {
	intent    string
	keywords  []string
	text      string
	audioPath string
}

var intentRoutes = []intentRoute{
	{
		intent:    "precio",
		keywords:  []string{"cuanto cuesta", "precio", "costo", "costos", "pago", "pagos"},
		text:      "El paquete completo es de 8,500 pesos por persona. Aparta tu lugar hoy con solo 2,000 pesos y paga el resto en meses sin intereses.",
		audioPath: "assets/audio/01-precio.mp3",
	},
	{
		intent:    "que_incluye",
		keywords:  []string{"que incluye", "incluye", "que viene", "viene incluido", "que cubre", "cubre"},
		text:      "El precio incluye transporte ejecutivo, hotel, todas tus comidas, tours y actividades. No pagas nada extra al llegar.",
		audioPath: "assets/audio/02-que-incluye.mp3",
	},
	{
		intent:    "actividades",
		keywords:  []string{"que vamos a hacer", "actividades", "actividad", "plan", "itinerario", "catamaran", "playa", "tequila"},
		text:      "Te llevamos a un paseo en catamaran con barra libre, musica de banda en la playa, visita a las tierras del tequila y el Grito Mexicano en vivo.",
		audioPath: "assets/audio/03-actividades.mp3",
	},
	{
		intent:    "fechas_destino",
		keywords:  []string{"cuando", "fecha", "fechas", "a donde", "destino", "mazatlan", "jalisco", "septiembre"},
		text:      "Salimos del 12 al 17 de septiembre a Mazatlan y Tequila, Jalisco. Son cinco dias completos para disfrutar las Fiestas Patrias.",
		audioPath: "assets/audio/04-fechas-destino.mp3",
	},
	{
		intent:    "oferta_grupo",
		keywords:  []string{"descuento", "grupo", "somos grupo", "promocion", "oferta", "gratis", "cuarto lugar"},
		text:      "Si viajan en grupo de 4, el cuarto lugar es completamente gratis. Es la mejor forma de celebrar en grande con tus amigos o familia.",
		audioPath: "assets/audio/05-oferta-grupo.mp3",
	},
	{
		intent:    "disponibilidad",
		keywords:  []string{"quedan lugares", "lugares", "disponibilidad", "disponible", "cupos", "cupo", "hay lugar"},
		text:      "Solo hay 40 lugares disponibles y se estan llenando rapido. Si quieres asegurar el tuyo, aparta hoy mismo.",
		audioPath: "assets/audio/06-disponibilidad.mp3",
	},
	{
		intent:    "cta_cierre",
		keywords:  []string{"como reservo", "reservar", "reservo", "apartar", "quiero apartar", "informacion", "info", "me interesa"},
		text:      "Para reservar escribenos por WhatsApp al 55-1234-5678 o buscanos como Viajes Mexico Tuyo en Facebook. Te confirmamos en minutos.",
		audioPath: "assets/audio/07-cta-cierre.mp3",
	},
}

var fallbackResponse = AgentResponse{
	Intent:    "fallback",
	Text:      "Gracias por escribir a Viajes Mexico Tuyo. Para reservar escribenos por WhatsApp al 55-1234-5678 o buscanos como Viajes Mexico Tuyo en Facebook. Te confirmamos en minutos.",
	AudioPath: "assets/audio/07-cta-cierre.mp3",
}

func selectAgentResponse(message string) AgentResponse {
	normalized := normalizeText(message)
	bestIndex := -1
	bestScore := 0

	for i, route := range intentRoutes {
		score := 0
		for _, keyword := range route.keywords {
			if strings.Contains(normalized, normalizeText(keyword)) {
				score++
			}
		}
		if score > bestScore {
			bestIndex = i
			bestScore = score
		}
	}

	if bestIndex == -1 {
		return fallbackResponse
	}

	route := intentRoutes[bestIndex]
	return AgentResponse{
		Intent:    route.intent,
		Text:      route.text,
		AudioPath: route.audioPath,
	}
}

func normalizeText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	replacer := strings.NewReplacer(
		"á", "a",
		"é", "e",
		"í", "i",
		"ó", "o",
		"ú", "u",
		"ü", "u",
		"ñ", "n",
		"¿", "",
		"?", "",
		"¡", "",
		"!", "",
		",", " ",
		".", " ",
		":", " ",
		";", " ",
	)
	return strings.Join(strings.Fields(replacer.Replace(s)), " ")
}
