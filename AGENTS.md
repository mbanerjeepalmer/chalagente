# Chalagente

Go service wrapping [whatsmeow](https://github.com/tulir/whatsmeow) to receive
WhatsApp messages and send a hardcoded auto-reply. Deployed to Coolify via
Docker.

## Layout
- `main.go` — entrypoint, pairing, event handler, HTTP `/healthz`.
- `Dockerfile` — multi-stage build. CGO is enabled for `mattn/go-sqlite3`.
- `/data/store.db` (in container) — whatsmeow session store. Mount a Coolify
  volume at `/data` so pairing survives redeploys.

## Pairing
First run prints a QR code to stdout. Scan it from WhatsApp → Settings →
Linked Devices. Credentials persist in the SQLite store; subsequent starts
reconnect silently.

## Local dev
```
go mod tidy
go run .
```
Or via Docker:
```
docker build -t chalagente .
docker run --rm -it -v $PWD/data:/data -p 8080:8080 chalagente
```
