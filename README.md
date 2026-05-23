# Chalagente

WhatsApp auto-reply POC built on [whatsmeow](https://github.com/tulir/whatsmeow).

Receives messages on a paired WhatsApp account and replies with a hardcoded
string. Designed to deploy to Coolify as a Docker container.

## Run locally

Requires Go ≥ 1.23 (or just Docker).

```bash
go mod tidy
go run .
```

On first start, scan the QR code printed in the terminal with WhatsApp →
Settings → Linked Devices. The session is saved to `./data/store.db`.

## Docker

```bash
docker build -t chalagente .
docker run --rm -it -v "$PWD/data:/data" -p 8080:8080 chalagente
```

## Coolify deploy

- New resource → Dockerfile-based application, pointing at this repo.
- Mount a persistent volume at `/data`.
- Expose port `8080`.
- Open the container logs on first deploy and scan the QR code.

## Web UI

Open `http://localhost:8080/` to:
- Scan the QR code in your browser (no terminal needed).
- See connection status and the linked JID.
- Send a message to any number.
- Watch a live feed of incoming and outgoing messages.

## Demo mode

Demo mode works without WhatsApp pairing — useful for development and demos.

- `GET /demo` — public WhatsApp-style chat UI (customer view). No auth required.
- `GET /demo/events` — public SSE stream of demo messages.
- `GET /demo/media/{id}` — public media files uploaded in demo mode.
- `GET /demo/bot` — control panel to send messages as the customer (Basic Auth).
- `POST /demo/bot/send` — multipart form: `type` (`text`|`image`|`audio`|`video`), `body` (text or caption), `file` (required for media).

Open `/demo` in one tab and `/demo/bot` in another. Messages sent from the bot panel appear in the chat and trigger the same hardcoded auto-reply as real WhatsApp.

## Endpoints

- `GET /` — web UI (Basic Auth).
- `GET /qr.png` — current pairing QR as PNG, `404` once paired.
- `POST /send` — form: `to`, `text`. Redirects to `/`.
- `GET /events` — Server-Sent Events stream of message activity.
- `GET /healthz` — `200 ok` once connected and logged in, else `503`.
- `GET /demo` — demo chat UI (public).
- `GET /demo/bot` — demo control panel (Basic Auth).

## Config

| Env                | Required | Default            |
| ------------------ | -------- | ------------------ |
| `BASIC_AUTH_USER`  | yes      | —                  |
| `BASIC_AUTH_PASS`  | yes      | —                  |
| `STORE_PATH`       | no       | `./data/store.db`  |
| `HTTP_ADDR`        | no       | `:8080`            |

The admin web UI (`/`, `/send`, `/events`, `/demo/bot`) is protected with HTTP
Basic Auth. The service refuses to start if either credential is unset.
`/healthz` and `/demo` (including `/demo/events` and `/demo/media/*`) are
intentionally left open.
