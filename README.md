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

## Endpoints

- `GET /` — web UI.
- `GET /qr.png` — current pairing QR as PNG, `404` once paired.
- `POST /send` — form: `to`, `text`. Redirects to `/`.
- `GET /events` — Server-Sent Events stream of message activity.
- `GET /healthz` — `200 ok` once connected and logged in, else `503`.

## Config

| Env                | Required | Default            |
| ------------------ | -------- | ------------------ |
| `BASIC_AUTH_USER`  | yes      | —                  |
| `BASIC_AUTH_PASS`  | yes      | —                  |
| `STORE_PATH`       | no       | `./data/store.db`  |
| `HTTP_ADDR`        | no       | `:8080`            |

The web UI is protected with HTTP Basic Auth. The service refuses to start if
either credential is unset. `/healthz` is intentionally left open so Coolify
health checks keep working.
