# KB Audio Scripts — Viajes México Tuyo
> Cada sección = 1 audio en ElevenLabs. Todos bajo 15 segundos.
> Trigger = intención detectada por el agente.

---

## 1. Precio
**Trigger:** ¿cuánto cuesta? / precio / costo / pagos

> "El paquete completo es de 8,500 pesos por persona. Aparta tu lugar hoy con solo 2,000 pesos y paga el resto en meses sin intereses."

---

## 2. Qué incluye
**Trigger:** ¿qué incluye? / qué viene / qué cubre

> "El precio incluye transporte ejecutivo, hotel, todas tus comidas, tours y actividades. No pagas nada extra al llegar."

---

## 3. Actividades
**Trigger:** ¿qué vamos a hacer? / actividades / plan / itinerario

> "Te llevamos a un paseo en catamarán con barra libre, música de banda en la playa, visita a las tierras del tequila y el Grito Mexicano en vivo."

---

## 4. Fechas y destino
**Trigger:** ¿cuándo? / fechas / a dónde / destino

> "Salimos del 12 al 17 de septiembre a Mazatlán y Tequila, Jalisco. Son cinco días completos para disfrutar las Fiestas Patrias."

---

## 5. Oferta de grupo
**Trigger:** ¿hay descuento? / somos grupo / descuento / promoción

> "Si viajan en grupo de 4, el cuarto lugar es completamente gratis. Es la mejor forma de celebrar en grande con tus amigos o familia."

---

## 6. Disponibilidad / urgencia
**Trigger:** ¿quedan lugares? / disponibilidad / cupos

> "Solo hay 40 lugares disponibles y se están llenando rápido. Si quieres asegurar el tuyo, aparta hoy mismo."

---

## 7. CTA / cierre
**Trigger:** ¿cómo reservo? / quiero apartar / reservar / información

> "Para reservar escríbenos por WhatsApp al 55-1234-5678 o búscanos como Viajes México Tuyo en Facebook. Te confirmamos en minutos."

---

## Notas de implementación

- Usar el mismo `voice_id` en todos los audios para consistencia de marca
- Pre-generar y guardar los `.mp3` en storage (no generar en tiempo real)
- El agente mapea intención → ID de audio → manda el archivo por WhatsApp
- Fallback: si la intención no matchea ningún trigger, responde en texto con el CTA de reserva
