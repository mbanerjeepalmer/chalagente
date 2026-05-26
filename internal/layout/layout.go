// Package layout owns the visual chrome shared across every Chalagente
// page — palette tokens, typography, favicon, footer. Templates concat
// these strings into their HTML so we get a single source of truth without
// switching to file-based templates or html/template includes.
//
// The package is deliberately tiny and dependency-free so any other
// internal package (e.g. clerkauth) can import it without dragging in the
// main binary's transitive imports.
package layout

// FaviconDataURI is a tiny inline SVG of the Chalagente "C" mark in
// terracotta on bone — embedded so we don't need to ship a separate asset.
const FaviconDataURI = `data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 64 64'><rect width='64' height='64' rx='14' fill='%23b5482e'/><text x='50%25' y='54%25' text-anchor='middle' dominant-baseline='middle' font-family='Georgia,serif' font-size='42' font-weight='700' fill='%23faf6ea'>C</text></svg>`

// FaviconLink is the full <link rel="icon"> tag pointing at FaviconDataURI.
const FaviconLink = `<link rel="icon" href="` + FaviconDataURI + `">`

// FontsLink loads Cormorant Garamond (serif headings) + Inter (sans body)
// from Google Fonts. Use display=swap so initial paint isn't blocked.
const FontsLink = `<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Cormorant+Garamond:wght@500;600;700&family=Inter:wght@400;500;600&display=swap" rel="stylesheet">`

// GithubURL is the public source repo, linked from footers.
const GithubURL = "https://github.com/mbanerjeepalmer/chalagente"

// GithubIconSVG is the octocat glyph used next to the GitHub footer link.
const GithubIconSVG = `<svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" style="vertical-align:-3px"><path fill="currentColor" d="M12 .5C5.73.5.75 5.48.75 11.75c0 4.95 3.21 9.14 7.66 10.62.56.1.77-.24.77-.54v-1.9c-3.11.68-3.77-1.5-3.77-1.5-.51-1.3-1.25-1.64-1.25-1.64-1.02-.7.08-.68.08-.68 1.13.08 1.72 1.16 1.72 1.16 1 1.72 2.63 1.22 3.27.94.1-.73.39-1.22.7-1.5-2.48-.28-5.09-1.24-5.09-5.52 0-1.22.44-2.22 1.15-3-.12-.28-.5-1.42.11-2.96 0 0 .94-.3 3.08 1.15a10.7 10.7 0 0 1 5.6 0c2.14-1.45 3.08-1.15 3.08-1.15.62 1.54.23 2.68.11 2.96.72.78 1.15 1.78 1.15 3 0 4.29-2.61 5.23-5.1 5.51.4.34.76 1.02.76 2.06v3.05c0 .3.2.65.78.54 4.45-1.48 7.66-5.67 7.66-10.62C23.25 5.48 18.27.5 12 .5z"/></svg>`

// SharedStyles is the Diego Rivera "Fraternidad" palette + typography
// shared across landing, legal, demo, dashboard and the Clerk auth pages.
// Includes container/nav/footer rules; page-specific CSS is appended by
// each template.
const SharedStyles = `
:root {
  --wall: #f1ead9;
  --wall-shade: #e6dec7;
  --plaster: #ece2cb;
  --ink: #1c1a16;
  --ink-soft: #3a352c;
  --muted: #6b6354;
  --line: rgba(28,26,22,0.14);
  --terracotta: #b5482e;
  --terracotta-deep: #8a3320;
  --ochre: #c8932b;
  --indigo: #25406e;
  --leaf: #4f6a3a;
  --bone: #faf6ea;
  --radius: 6px;
}
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; }
body {
  font-family: "Inter", "Helvetica Neue", Helvetica, Arial, sans-serif;
  background: var(--wall);
  color: var(--ink-soft);
  line-height: 1.6;
  -webkit-font-smoothing: antialiased;
  background-image:
    radial-gradient(rgba(110,90,60,0.05) 1px, transparent 1px),
    radial-gradient(rgba(80,60,40,0.04) 1px, transparent 1px),
    linear-gradient(180deg, var(--wall), var(--wall-shade));
  background-size: 3px 3px, 7px 7px, 100% 100%;
  background-position: 0 0, 1px 2px, 0 0;
}
h1, h2, h3, h4 {
  font-family: "Cormorant Garamond", "Playfair Display", Georgia, "Times New Roman", serif;
  color: var(--ink);
  font-weight: 600;
  letter-spacing: -0.005em;
  line-height: 1.15;
}
a { color: var(--terracotta-deep); }
.container { max-width: 1080px; margin: 0 auto; padding: 0 1.5rem; }
header.nav {
  position: sticky; top: 0; z-index: 10;
  background: rgba(241,234,217,0.92);
  backdrop-filter: blur(6px);
  border-bottom: 1px solid var(--line);
}
.nav-inner { display: flex; align-items: center; justify-content: space-between; padding: 1rem 0; }
.logo { display: flex; align-items: center; gap: .6rem; font-family: "Cormorant Garamond", serif; font-weight: 700; font-size: 1.35rem; color: var(--ink); text-decoration: none; letter-spacing: .01em; }
.logo-mark {
  width: 30px; height: 30px; border-radius: 50%;
  background: var(--terracotta);
  display: grid; place-items: center; color: var(--bone);
  font-family: "Cormorant Garamond", serif; font-weight: 700; font-size: 1rem;
  box-shadow: inset 0 -2px 0 rgba(0,0,0,0.15);
}
.nav-links { display: flex; gap: 1.4rem; align-items: center; font-size: .92rem; color: var(--ink-soft); }
.nav-links a { color: var(--ink-soft); text-decoration: none; }
.nav-links a:hover { color: var(--terracotta-deep); }
.btn {
  display: inline-flex; align-items: center; gap: .5rem;
  padding: .7rem 1.2rem; border-radius: var(--radius);
  font-weight: 600; font-size: .95rem; text-decoration: none;
  transition: transform .12s ease, box-shadow .15s ease;
  border: 1px solid transparent;
}
.btn-primary { background: var(--terracotta); color: var(--bone); box-shadow: 0 2px 0 var(--terracotta-deep); }
.btn-primary:hover { transform: translateY(-1px); }
.btn-ghost { background: transparent; color: var(--ink); border-color: var(--ink); }
.btn-ghost:hover { background: rgba(28,26,22,0.05); }
footer { padding: 2rem 0 2.5rem; color: var(--muted); font-size: .85rem; border-top: 1px solid var(--line); margin-top: 3rem; }
footer .container { display: flex; justify-content: space-between; flex-wrap: wrap; gap: 1rem; }
footer a { color: var(--muted); text-decoration: none; margin-right: 1rem; }
footer a:hover { color: var(--ink); }
footer .foot-links { display: inline-flex; align-items: center; flex-wrap: wrap; gap: .25rem 1rem; }
footer .foot-links a { margin-right: 0; }
@media (max-width: 720px) {
  .nav-links a:not(.btn) { display: none; }
}
`

// LogoLink is the standard top-left logo (clickable circle + wordmark).
// Used by the marketing/admin nav and by the auth shell. Marked up so the
// .logo / .logo-mark styles from SharedStyles pick it up.
const LogoLink = `<a class="logo" href="/"><span class="logo-mark">C</span><span>Chalagente</span></a>`

// FooterMarketingHTML is the public-facing footer shared by landing and
// legal pages: privacy / terms / demo / sign-in plus GitHub. Renders
// inside a normal <footer> tag and assumes SharedStyles is loaded.
const FooterMarketingHTML = `<footer>
  <div class="container">
    <span>© Chalagente · Hecho en México</span>
    <span class="foot-links">
      <a href="/privacidad">Aviso de privacidad</a>
      <a href="/terminos">Términos</a>
      <a href="/demo">Demo</a>
      <a href="/sign-in">Iniciar sesión</a>
      <a href="` + GithubURL + `" rel="noopener" aria-label="GitHub" title="GitHub">` + GithubIconSVG + `</a>
    </span>
  </div>
</footer>`

// ChatPaneStyles is the WhatsApp-clone visual treatment shared by /demo
// and /admin/conversations/{id}. The classes (chatpane, phead, chat,
// bubble.in / bubble.out) are kept identical so the two screens render
// the same conversation visually — the demo just adds a composer below
// and the admin viewer doesn't.
const ChatPaneStyles = `
.chatpane{display:flex;flex-direction:column;background:#0e1620;border-radius:6px;overflow:hidden;border:1px solid var(--line);box-shadow:0 8px 24px rgba(40,30,15,0.10);min-height:520px}
.chatpane .phead{display:flex;align-items:center;gap:.6rem;padding:.8rem 1.2rem;background:#075e54;color:white;border-bottom:1px solid #04443c}
.chatpane .phead .avatar{width:36px;height:36px;border-radius:50%;background:#25d366;display:grid;place-items:center;font-weight:700;color:#04130b}
.chatpane .phead .name{font-weight:600}
.chatpane .phead .sub{font-size:.75em;opacity:.85}
.chatpane .chat{flex:1;overflow-y:auto;padding:1rem 1.2rem;background:#ece5dd;display:flex;flex-direction:column;gap:.4rem;min-height:380px}
.chatpane .bubble{max-width:75%;padding:.55rem .75rem;border-radius:12px;font-size:.95rem;color:#222;box-shadow:0 1px 1px rgba(0,0,0,.08);word-wrap:break-word;font-family:"Inter","Helvetica Neue",sans-serif}
/*
 * Bubble alignment defaults to the customer's perspective: the customer's
 * own message (Direction="in" — *inbound* from the agent's point of view)
 * sits on the right in green, and the agent's reply (Direction="out")
 * sits on the left in white. This matches WhatsApp's "you typed on the
 * right" mental model when the customer is the one looking at the chat.
 *
 * .chatpane.from-business swaps the alignment for the dashboard's
 * history viewer, where the operator is the business owner: the
 * business's own outgoing messages sit on the right, the customer's
 * inbound messages on the left.
 */
.chatpane .bubble.in{background:#dcf8c6;align-self:flex-end;border-bottom-right-radius:2px}
.chatpane .bubble.out{background:white;align-self:flex-start;border-bottom-left-radius:2px}
.chatpane.from-business .bubble.in{background:white;align-self:flex-start;border-bottom-right-radius:12px;border-bottom-left-radius:2px}
.chatpane.from-business .bubble.out{background:#dcf8c6;align-self:flex-end;border-bottom-left-radius:12px;border-bottom-right-radius:2px}
.chatpane .bubble small,.chatpane .bubble .when{display:block;color:#888;font-size:.7em;margin-top:.2rem}
.chatpane .bubble .kindbadge{display:inline-block;font-size:.7em;color:#555;background:rgba(0,0,0,.05);border-radius:3px;padding:1px 5px;margin-right:.3rem;text-transform:uppercase;letter-spacing:.05em}
.chatpane audio{width:100%;margin-top:.25rem;height:32px}
`

// FooterLegalHTML is the slimmer footer used on legal pages — no demo /
// sign-in links because those pages already feel like end-of-the-funnel.
const FooterLegalHTML = `<footer><div class="container">
  <span>© Chalagente</span>
  <span class="foot-links">
    <a href="/privacidad">Aviso de privacidad</a>
    <a href="/terminos">Términos</a>
    <a href="` + GithubURL + `" rel="noopener" aria-label="GitHub" title="GitHub">` + GithubIconSVG + `</a>
  </span>
</div></footer>`
