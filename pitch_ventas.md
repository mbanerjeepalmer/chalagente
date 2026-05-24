# Chalagente — Pitch de Ventas en Español

---

## ¿Qué es Chalagente?

**Chalagente** es un asistente virtual para WhatsApp que atiende automáticamente
a tus clientes las 24 horas, los 7 días de la semana. Cuando alguien escribe a
tu número de negocio, Chalagente detecta qué quiere saber y responde al
instante — con **audio de voz natural, en español mexicano y con la misma voz
de tu marca**.

No es un chatbot genérico. Responde con mensajes de audio pregrabados por una
voz profesional (vía ElevenLabs) que suenan completamente humanos, sobre los
temas que tus clientes más preguntan.

---

## ¿Por qué es diferente a un chatbot normal?

> La gente no lee textos largos en WhatsApp. Pero todos escuchan un audio.

**Chalagente responde con mensajes de VOZ**, no con textos. Imagina que un
cliente potencial te escribe un domingo a las 11 PM preguntando "¿cuánto
cuesta el viaje?". En menos de un segundo, Chalagente entiende la pregunta
y le manda un audio de 12 segundos diciendo el precio exacto, con la misma
voz cálida que usaría un asesor real.

Eso convierte más que cualquier texto automático.

---

## El caso: Viajes México Tuyo 🇲🇽

Viajes México Tuyo vende un paquete turístico espectacular:

- 🏖️ **Mazatlán y Tequila, Jalisco** — Fiestas Patrias, 5 días
- 🛥️ Paseo en catamarán con barra libre
- 🎺 Música de banda en vivo en la playa
- 🥃 Visita a las tierras del tequila con cantaritos
- 🏨 Hospedaje 3 noches, todo incluido
- 🚌 Transporte ejecutivo redondo
- 💰 $8,500 MXN por persona — paga 3, el 4° GRATIS
- 🎟️ Solo 40 lugares disponibles

Chalagente responde automáticamente estas 7 preguntas clave **con audios de
voz pregrabados**, uno para cada intención:

| ¿Qué pregunta el cliente? | ¿Qué responde Chalagente? |
|---------------------------|---------------------------|
| 💰 Precio / costo / pagos | Audio "precio": $8,500 por persona, aparta con $2,000 |
| 🎒 ¿Qué incluye? | Audio "incluye": transportación, hotel, comidas, tours — todo cubierto |
| 🎉 ¿Qué vamos a hacer? | Audio "actividades": catamarán, banda, tequila, Grito Mexicano |
| 📅 ¿Cuándo y a dónde? | Audio "fechas": 12–17 septiembre, Mazatlán y Tequila |
| 👥 ¿Descuento por grupo? | Audio "promoción": viajan 4, el cuarto gratis |
| ⚠️ ¿Quedan lugares? | Audio "disponibilidad": solo 40 lugares, se están llenando |
| 📲 ¿Cómo reservo? | Audio "cierre": escríbenos o búscanos en Facebook |

Si el cliente pregunta algo fuera de estos temas, Chalagente responde con un
mensaje de texto amable redirigiéndolo a contactar por WhatsApp o redes
sociales.

---

## ¿Cómo funciona técnicamente?

Chalagente corre como un servicio de software ligero — pesa menos de 50 MB
y se ejecuta en un contenedor Docker. Está desplegado en la nube (Coolify),
conectado permanentemente a WhatsApp como un "dispositivo vinculado", igual
que WhatsApp Web pero sin necesitar navegador.

```
Cliente escribe por WhatsApp
       │
       ▼
Chalagente recibe el mensaje (vía whatsmeow)
       │
       ▼
Detecta la intención del mensaje (próximamente: IA)
       │
       ▼
Envía el audio MP3 correspondiente (o texto de fallback)
       │
       ▼
El cliente escucha la respuesta — natural, inmediata, 24/7
```

No hay delays, no hay "un asesor te atenderá pronto", no hay mensajes
genéricos que ignoran lo que preguntaste. Respuesta **instantánea y relevante**.

---

## Beneficios para tu agencia de viajes

### 1. Nunca más pierdes un cliente por no contestar a tiempo
El 78% de los clientes compra con el primero que responde. Si te escriben a las
2 AM del sábado y contestas el lunes, ya perdiste la venta. Chalagente responde
en el mismo segundo.

### 2. Respuestas consistentes y profesionales
Cada audio está grabado por una sola voz profesional, revisado y aprobado. No
hay asesores que digan precios equivocados, se equivoquen de fechas u olviden
mencionar la promoción del cuarto gratis.

### 3. Escalas sin contratar más gente
Si recibes 5 mensajes al día o 500, Chalagente responde igual. Puedes hacer
campañas, poner anuncios, viralizarte — y nunca colapsa. Un solo servidor
maneja cientos de conversaciones simultáneas.

### 4. Te enfocas en cerrar, no en contestar lo mismo 100 veces
¿Cuántas horas invierten tus asesores repitiendo "el precio es $8,500, incluye
transporte, hotel..."? Chalagente maneja las preguntas repetitivas. Tus
asesores solo intervienen cuando el cliente ya está listo para reservar.

### 5. Opera 24/7, sin descanso, sin vacaciones, sin rotación
No se enferma, no pide aumento, no se va a la competencia.

---

## Lo que incluye

| Componente | Detalle |
|------------|---------|
| Servicio Chalagente | Bot WhatsApp con detección de intención y respuestas de voz |
| 7 audios profesionales | Generados con ElevenLabs, mismo voice_id, ≤15 segundos cada uno |
| Panel web privado | Vista de estado, QR de vinculación, envío manual de mensajes, feed en vivo |
| Feed en tiempo real | Ves cada mensaje que entra y cada respuesta que sale, en el momento |
| Despliegue en la nube | Dockerizado en Coolify, disponible 24/7 desde cualquier lugar |
| Sesión persistente | Si el servidor se reinicia, la conexión a WhatsApp se recupera sola |
| Protegido con contraseña | Solo tú y tu equipo acceden al panel y a las funciones de envío |

---

## Próximos pasos (fase 2: IA)

La versión actual de Chalagente usa detección de intención por palabras clave
(si el mensaje contiene "precio", "costo" o "cuánto" → manda audio de precio).

La **fase 2** integra un modelo de lenguaje (LLM) que entiende lenguaje natural
completo, incluso preguntas mezcladas como *"hola, me interesa el viaje pero
voy con mi familia de 5, ¿tienen descuento y cuánto sería en total?"* — y
responde con precisión.

También se puede integrar con un sistema de reservas real para confirmar cupos
en tiempo real y procesar pagos directamente desde WhatsApp.

---

## ¿Qué necesitas para empezar?

1. **Un número de WhatsApp** para tu negocio (puede ser el mismo que ya usas)
2. **Escanear un código QR** una sola vez (como vincular WhatsApp Web)
3. **Listo.** Chalagente empieza a responder automáticamente.

No necesitas instalar nada en tu computadora. Todo corre en la nube. Tú y tu
equipo acceden al panel de control desde cualquier navegador, con usuario y
contraseña.

---

## Resumen

Chalagente no reemplaza a tu equipo de ventas — **los potencia**. Maneja lo
repetitivo para que ellos se concentren en lo que importa: cerrar ventas y dar
experiencias memorables.

> *"Si pueden soñarlo, podemos viajarlo."* — Viajes México Tuyo 🇲🇽
