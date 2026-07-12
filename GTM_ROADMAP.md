# Positronica Labs — Roadmap Comercial 2026

> **Tesis:** Somos builders que ship en días, no meses. Nuestro bottleneck no es código — es ventas, distribución, y cobros. Este documento cierra ese gap.
>
> **Equipo:** G10 (Gio) — tech/build · Jeaneth — producto/ops
>
> **Meta Q3 2026:** $5,000 MXN MRR entre todos los productos
> **Meta Q4 2026:** $30,000 MXN MRR

---

## Portfolio de Productos

| # | Producto | Status | Tiempo a venta | MRR meta Q3 | MRR meta Q4 |
|---|----------|--------|---------------|-------------|-------------|
| 1 | **Ergane** (job search SaaS) | SaaS-ready | **1 semana** | $3,000 | $15,000 |
| 2 | **TramitAI** (gov agent) | SAT funciona | 3 semanas | $2,000 | $10,000 |
| 3 | **RentalAI** (rental meta-search) | MVP CLI | 4 semanas | $0 | $5,000 |
| 4 | **Outlay OS** (dev spend) | Scaffold | 8 semanas | $0 | $0 |

**Estrategia:** Ergane primero (más rápido a revenue), TramitAI segundo (TAM más grande), RentalAI tercero (mismo tech stack), Outlay OS pausado.

---

## 1. ERGANE — Job Search Agent SaaS

### Por qué vender primero
- **Ya funciona** en producción (systemd en Pop)
- **Ya es open-source** (github.com/G10hdz/Ergane)
- **SaaS pricing ya definido** ($9-99/mo)
- **Multi-user** (Jeaneth ya lo usa con perfil propio)
- **Costo marginal casi cero** ($0.03 por CV, Ollama para scoring)

### Producto
Un agente que busca chamba por ti: scrapea 10+ fuentes, matchea con tu CV, te avisa por Telegram, genera CVs personalizados, trackea aplicaciones.

### Pricing

| Plan | Precio/mes | Para qué | Incluye |
|------|-----------|----------|---------|
| **Free** | $0 | Probar | 1 perfil, 3 fuentes, sin CV gen, Telegram bot |
| **Pro** | $149 MXN ($9 USD) | Buscadores activos | Todo ilimitado, CV gen (10/mes), ATS scanner |
| **Plus** | $399 MXN ($24 USD) | Power users | CV gen ilimitado, todas las fuentes, empresas target |
| **Team** | $999 MXN ($59 USD) | Bootcamps, universidades | 5 usuarios, analytics, API access |

### TAM
- **México:** 58M PEA (población económicamente activa)
- **Target realista año 1:** 10,000 usuarios free → 500 pagando ($149 promedio) = **$74,500 MXN/mes**
- **LATAM año 2:** expandir a Colombia, Argentina, Chile

### Diferenciación vs competencia

| Competidor | Qué hace | Por qué pierden contra Ergane |
|------------|----------|-------------------------------|
| LinkedIn Job Alerts | Email notifications | No matchea con CV, no genera CVs, no Telegram |
| Teal/JobCopilot | ATS tracker (US) | No scrapea boards MX, no multi-idioma, caro ($29 USD) |
| Turing/Andela | Marketplace | Te dan chamba, no te ayudan a buscar |

**Moat:** 33 empresas target curadas MX + 10+ fuentes LATAM + scoring multidimensional + Telegram bot. Nadie más tiene esto para LatAm.

### GTM — Primeros 100 usuarios pagando

**Canal 1: Twitter/X + LinkedIn (gratis)**
```
Semana 1: Post de launch: "Botreé 10,000 vacantes en MX con IA, estas son las que más matchean con mi CV"
Semana 2: Hilo: "Cómo detecté 47 'junior' que pedían 5+ años de experiencia"
Semana 3: Video: demo de generar CV tailored en 20 segundos
Semana 4: Open-source push: "Ergane es MIT, self-host gratis, o $149 MXN/mes managed"
```

**Canal 2: Comunidades (gratis)**
```
- Reddit r/mexico, r/devMX, r/cscareerquestions
- Discord: Bootcamps (Henry, Make It Real, Bedu)
- Telegram: Grupos de devs mexicanos
- Hashnode/Dev.to: Post técnico "Cómo construí un scraper de 10 bolsas con scoring de IA"
```

**Canal 3: Partnerships (costo cero, revenue share)**
```
- Bootcamps (Bedu, Henry, TalentoDigital): include Ergane Pro para egresados
  → Tú das 3 meses free, bootcamp paga $50 MXN/alumno/mes
- Universidades (IPN, UNAM FES): servidor académico
  → Free para estudiantes, $149 si quieren CV gen
- YouTubers de carreras tech (Fazt, Midudev, etc.): código de descuento + comisión
```

**Canal 4: Product Hunt + Hacker News**
```
- Launch en Product Hunt (gratis, miles de early adopters)
- "Show HN: I built an AI agent that applies to jobs for me" (Hacker News)
- Timing: Martes o Miércoles 12:01 AM PT
```

### Infra necesaria para cobrar

**Conekta (México) — recomiendo sobre Stripe por:**
- Acepta OXXO Pay (50% de mexicanos sin tarjeta)
- SPEI transfer (transferencia bancaria instantánea)
- Tarjetas MX e internacionales
- Comprobantes fiscales automáticos (facturación)

```typescript
// Setup mínimo para Ergane
// 1. Crear cuenta en Conekta.com (gratis, cobran 2.9% + $2.5 MXN por transacción)
// 2. Crear 3 planes:
//    - ergane_pro: $149 MXN/mes
//    - ergane_plus: $399 MXN/mes
//    - ergane_team: $999 MXN/mes
// 3. Webhook → activate user profile en Ergane DB
// 4. Landing page con botón "Suscribirme" → Conekta Checkout
```

### Landing page — Copy

```
ERGANE — Tu agente de IA que busca chamba por ti

¿Cansado de scrollear OCC, LinkedIn y CompuTrabajo todos los días?

Ergane monitorea 10+ bolsas de trabajo, las compara con tu CV,
y te manda por Telegram solo las vacantes que realmente te convienen.

✓ Detecta "junior" falsos que piden 5+ años de experiencia
✓ Genera CVs personalizados para cada vacante en 20 segundos
✓ Scrapea directamente las páginas de empleos de Clip, Bitso, Konfío...
✓ Te recuerda cuáles ya aplicaste y cuáles se te están pasando

[Probar gratis] [Ver planes desde $149 MXN/mes]

Sin tarjeta de crédito. Cancela cuando quieras.
```

### Métricas a trackear

| Métrica | Meta Q3 | Meta Q4 |
|---------|---------|---------|
| Usuarios free | 500 | 3,000 |
| Conversion free→paid | 5% | 7% |
| Usuarios pagando | 25 | 210 |
| ARPU | $149 MXN | $200 MXN |
| Churn | <10% | <7% |
| MRR | $3,725 | $42,000 |

### Roadmap de producto (para vender más)

| Mes | Feature | Impacto en revenue |
|-----|---------|-------------------|
| Mes 1 | Conekta + landing + Telegram onboarding | **Empezar a cobrar** |
| Mes 2 | CV gen en español neutro + portugués | +30% conversión LATAM |
| Mes 3 | Entrevista prep (mock interview con IA) | Upsell: $99 MXN add-on |
| Mes 4 | Salary negotiation bot | Upsell: $49 MXN add-on |
| Mes 5 | API pública | Plan Team ($999 MXN) |
| Mes 6 | Mobile app (React Native) | +50% retención |

---

## 2. TRAMITAI — Agente Gubernamental Universal

### Producto
Un agente de voz/chat que hace trámites gubernamentales por ti: SAT (facturas, declaraciones), IMSS (altas/bajas), pasaportes (citas), y más.

### Pricing

| Plan | Precio/mes | Para | Incluye |
|------|-----------|------|---------|
| **Ciudadano Free** | $0 | Cualquiera | Consultas gratis, requisitos, citas |
| **Ciudadano+** | $49 MXN | Persona | Trámite guiado, alertas, historial |
| **Gestor** | $299 MXN | Contador/Gestor | 25 clientes, SAT + IMSS |
| **Gestor Pro** | $999 MXN | Despacho | Clientes ilimitados, lote, multi-provider |
| **Empresa** | $199 MXN | PYME | Self-service SAT + IMSS + INFONAVIT |

### TAM
- **B2B:** 500K contadores + 200K gestores + 6M PYMEs
- **B2C:** 60M contribuyentes
- **TAM realista año 1:** 100 gestores pagando $299 = **$29,900 MXN/mes**

### Diferenciación

| Competidor | Qué hace | Por qué ganamos |
|------------|----------|-----------------|
| Contpaqui/Aspel | Software contable desktop | No hace trámites, solo contabilidad |
| SAT Móvil | App del gobierno | Horrible UX, no voz, no IA |
| Gestores humanos | Cobran $200-500/cliente/mes | Caros, lentos, error-prone |
| **TramitAI** | **Voz + IA + scraping directo** | **$299/mes ilimitado, 24/7, sin errores** |

**Moat:** Scraper funcional de SAT con e.firma + captcha solving con Claude Vision. Nadie más tiene esto. IMSS usa la misma e.firma → expansión natural.

### GTM — Primeros 50 gestores

**Canal 1: Cold outreach a contadores (WhatsApp/LinkedIn)**
```
Target: Contadores que ya usan herramientas digitales (los que tienen Contpaqui)
Mensaje: "Hago que tus clientes nunca más te pregunten '¿ya salieron mis facturas?'. 
Tu bot de Telegram les consulta facturas del SAT automáticamente. $299/mes. 
¿Te muestro en 10 minutos?"

Volumen: 20 mensajes/día → 5 respuestas → 1 demo → 0.3 conversión
Meta: 30 gestores en 3 meses
```

**Canal 2: Grupos de WhatsApp de contadores**
```
Hay cientos de grupos de WhatsApp de contadores en MX.
Estrategia: Jeaneth se meta a 20 grupos, contestar dudas gratis 1 semana,
mencionar TramitAI orgánicamente cuando alguien pregunte sobre SAT.
No spam — helpful primero, producto segundo.
```

**Canal 3: Demo en vivo a despachos**
```
Ofrecer demo gratuita a despachos contables medianos (5-20 contadores).
"En 5 minutos consulto las facturas de 10 de sus clientes simultáneamente"
Si les gusta → 1 semana free trial → $999 MXN/mes (plan Pro)
```

### Roadmap de producto

| Mes | Feature | Por qué vende |
|-----|---------|---------------|
| Mes 1 | Multi-cliente + Conekta + landing | **Empezar a cobrar** |
| Mes 2 | IMSS IDSE (altas, bajas, SIPARE) | Doble valor, justifica $299 |
| Mes 3 | Tax calendar + alertas WhatsApp | Retención, reduce churn |
| Mes 4 | Declaración anual asistida | Estacional: febrero-marzo = peak |
| Mes 5 | App ciudadano (B2C) | Expansión a consumidor |
| Mes 6 | Pasaporte SRE + citas INE | Nuevas vertical |

---

## 3. RENTALAI — Agente de Renta con IA

### Producto
Agente que busca departamentos para ti en TODOS los portales + FB Marketplace + sitios individuales (con Firecrawl), analiza fotos con IA, los scorea contra tus criterios, y te avisa en tiempo real.

### NO es Trivago. ES Ergane para rentas.

### Pricing

| Plan | Precio/mes | Para | Incluye |
|------|-----------|------|---------|
| **Free** | $0 | Probar | 3 búsquedas, 1 zona, sin alertas tiempo real |
| **Hunter** | $99 MXN | Inquilino activo | Búsquedas ilimitadas, alertas FB Marketplace, scoring IA |
| **Pro** | $199 MXN | Inquilino exigente | Fotos analizadas con IA, red flags, comparación de precios, geocoding |
| **Broker** | $499 MXN | Inmobiliaria | Lead gen calificado, multi-listing, API |

### TAM
- **CDMX:** 6M hogares, ~20% rentan = 1.2M hogares
- **México:** 35M hogares, ~25% rentan = 8.75M
- **Target realista año 1:** 1,000 usuarios Hunter ($99) = **$99,000 MXN/mes**

### Diferenciación

| Competidor | Gap que dejan | Cómo ganamos |
|------------|---------------|--------------|
| Inmueble24/Vivanuncios | Solo su inventario. No FB Marketplace | Scrapeamos TODO con Firecrawl |
| FB Marketplace search | Búsqueda manual, sin scoring | Alertas proactivas + scoring IA |
| Properati/Lamudi | Solo portales formales | WhatsApp groups + FB + cualquier sitio |
| Radar Inmobiliario | Datos de precios, no búsqueda | Búsqueda activa + match personal |

**Moat:** Firecrawl = cualquier sitio scrapeable. Multimodal AI = fotos analizadas. Proactivo = te avisa antes que otros vean el listing.

### GTM — Primeros 500 usuarios

**Canal 1: Grupos de Facebook de rentas CDMX**
```
"Busco depa en CDMX", "Renta CDMX", "Departamentos México"
~50 grupos grandes con 10K-100K miembros cada uno
Estrategia: Bot que responde "¿Qué zona y presupuesto?" → 
muestra 3 listings matcheados → "Para más resultados, prueba RentalAI"
```

**Canal 2: TikTok/Reels**
```
"Este bot encontró un depa de $8,000 en la Roma 3 minutos después de publicarse"
"3 red flags en fotos de depas que detectó IA que NO viste"
"Comparé el mismo depa en 5 portales y encontré uno $2,000 más barato"
Formato: screen recording de Telegram bot → listing → scoring
```

**Canal 3: Inmobiliarias (B2B)**
```
Broker plan: $499 MXN/mes
"Calificamos leads automáticamente. Solo te llegan inquilinos serios 
que ya matchearon con tus listings."
Vender a 20 inmobiliarias = $9,980 MXN/mes
```

### Roadmap de producto

| Mes | Feature | Por qué vende |
|-----|---------|---------------|
| Mes 1 | Firecrawl integration + web UI + scoring | **MVP vendible** |
| Mes 2 | FB Marketplace scraper + alertas Telegram | Diferenciador principal |
| Mes 3 | Análisis de fotos con IA (multimodal) | Upsell a Pro ($199) |
| Mes 4 | Price comparison (mismo depa, múltiples portales) | Diferenciador vs portales |
| Mes 5 | Broker dashboard (B2B) | Plan $499 |
| Mes 6 | Expansión GDL + Monterrey | 3x TAM |

---

## 4. INFRAESTRUCTURA COMPARTIDA (hacer una vez, usar en todos)

### Conekta Integration Module

```typescript
// packages/payments/src/conekta.ts
// Shared payment module — úsalo en Ergane, TramitAI, RentalAI

interface ProductConfig {
  productId: 'ergane' | 'tramitai' | 'rentalai';
  plans: Plan[];
  webhookUrl: string;
}

// Flujo:
// 1. Landing page → botón "Suscribirme"
// 2. Conekta Checkout (modal, no redirect)
// 3. Pago procesado → webhook a tu API
// 4. Activar plan en DB del producto correspondiente
// 5. Si falla pago → suspender + email + WhatsApp
```

**Setup una vez:**
1. Crear cuenta Conekta (gratis, 2.9% + $2.5 por transacción)
2. Crear planes para cada producto
3. Webhook handler compartido
4. Dashboard de ingresos unificado

### Landing Pages (una por producto)

Cada landing page es una sola página con:
```
- Hero: qué hace + CTA (15 palabras)
- Problema: el dolor en 3 bullets
- Solución: demo en GIF/video (10 seg)
- Pricing: tabla simple
- FAQ: 5 preguntas
- CTA final: botón Conekta
```

**Stack:** Next.js o Vite + Tailwind. Una página por producto. Deploy a Vercel (gratis).

### Funnel de WhatsApp (reutilizas wa-bots)

```
Usuario aterriza en landing → escanea QR de WhatsApp
  → Bot: "¿Qué necesitas? (1) Buscar chamba (2) Trámite gobierno (3) Buscar depa"
  → Redirige al bot del producto correspondiente
  → Onboarding guiado dentro de WhatsApp
  → Upsell a plan pago dentro del chat
```

### Analytics compartido

```
Supabase dashboard con:
  - Usuarios por producto
  - Conversión free → paid
  - MRR por producto
  - Churn por producto
  - CAC (costo de adquisición) por canal
```

---

## 5. CRONOGRAMA DE EJECUCIÓN

### Semana 1-2: Ergane venda
```
[Tech]  Conekta account + webhook + plan activation
[Tech]  Landing page Ergane (1 página, Tailwind)
[Tech]  Onboarding flow: landing → pago → Telegram setup
[Gio]   Twitter thread de launch + Product Hunt prep
[Jean]  Grupos de WhatsApp/devs — research + primer post
[Jean]  Contactar 5 bootcamps (Bedu, Henry, etc.)
```

### Semana 3-4: Ergane distribución
```
[Gio]   Product Hunt launch (martes/miércoles)
[Gio]   Hacker News "Show HN"
[Jean]  20 grupos de WhatsApp/devs
[Jean]  5 YouTubers tech MX — email con código afiliado
[Tech]  Fix bugs de primeros usuarios
[Tech]  TramitAI: multi-cliente + Conekta
```

### Semana 5-6: TramitAI venda
```
[Tech]  TramitAI landing page
[Tech]  Gestor panel multi-cliente
[Gio]   Cold outreach: 20 contadores/día en LinkedIn
[Jean]  Grupos de WhatsApp de contadores (20 grupos)
[Jean]  Demo a 3 despachos contables
```

### Semana 7-8: RentalAI MVP
```
[Tech]  Firecrawl integration + web UI + scoring
[Tech]  FB Marketplace scraper + alertas
[Gio]   TikTok: 3 videos de demo
[Jean]  10 grupos de FB de rentas CDMX
```

### Semana 9-12: Escalar
```
[Tech]  RentalAI: análisis de fotos con IA
[Tech]  TramitAI: IMSS IDSE
[Gio]   Hablar con inmobiliarias (RentalAI broker plan)
[Jean]  Hablar con incubadoras/accelerators
[Ambos] Evaluar: ¿qué producto tiene más tracción? → duplicar esfuerzo ahí
```

---

## 6. MÉTRICAS SEMANALES (revisar cada lunes)

| Métrica | Ergane | TramitAI | RentalAI |
|---------|--------|----------|----------|
| Usuarios free nuevos | ? | ? | ? |
| Conversiones a paid | ? | ? | ? |
| MRR | ? | ? | ? |
| Churn | ? | ? | ? |
| CAC | ? | ? | ? |
| LTV | ? | ? | ? |

**Regla:** Si un producto no tiene 10 usuarios pagando después de 8 semanas de lanzamiento, se pausa. No se mata — se pausa. Volvemos a él cuando tengamos revenue de los otros.

---

## 7. RIESGOS Y MITIGACIONES

| Riesgo | Probabilidad | Impacto | Mitigación |
|--------|-------------|---------|------------|
| SAT bloquea el scraper | Media | Alto | Rotar IPs, human-like delays, fallback a API oficial si existe |
| Conekta rechaza cuenta (compliance) | Baja | Alto | Términos claros, no guardar credenciales, actuar como tool no como servicio financiero |
| Competencia copia Ergane | Alta | Medio | Velocidad de iteración + datos de scoring (ventaja de datos) |
| Ningún producto pega | Media | Alto | 8 semanas máximo por producto, luego pivotar. Costo de intentar: ~$0 (ya construido) |
| Burnout del equipo (2 personas) | Alta | Alto | Priorizar 1 producto a la vez para ventas. Tech puede paralelizar. |

---

## 8. LO QUE NECESITAS DE MÍ (Fable/opencode)

| Tarea | Producto | Estimación |
|-------|----------|-----------|
| Conekta integration module | Compartido | 1 día |
| Landing page Ergane | Ergane | 1 día |
| Landing page TramitAI | TramitAI | 1 día |
| Landing page RentalAI | RentalAI | 1 día |
| Onboarding flow Ergane | Ergane | 2 días |
| Gestor multi-cliente | TramitAI | 3 días |
| Firecrawl rental scraper | RentalAI | 2 días |
| **Total** | | **11 días** |

A 1 producto/día de velocidad, tienes todo montado en 2 semanas. Después es pura distribución y ventas.

---

## 9. TL;DR — Qué hacer LUNES

1. **Crear cuenta en Conekta** → conekta.com (20 minutos)
2. **Landing page Ergane** → yo la escribo, Gio la deploya en Vercel
3. **Twitter thread**: "Construí un bot que aplica a chambas por mí. Estos son los resultados"
4. **Product Hunt**: schedule launch para martes/miércoles
5. **WhatsApp**: Jeaneth entra a 5 grupos de devs MX

**Meta viernes:** Ergane tiene landing + botón de pago + primer tweet viral.
