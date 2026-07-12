# Chalagente

WhatsApp auto-reply service built on [whatsmeow](https://github.com/tulir/whatsmeow).

## Run

```bash
cp .env.example .env
export $(grep -v '^#' .env | xargs)
go mod tidy
go run .
```

## Docker

```bash
docker build -t chalagente .
docker run --rm -it -e BASIC_AUTH_USER=admin -e BASIC_AUTH_PASS=changeme \
  -v "$PWD/data:/data" -p 8080:8080 chalagente
```

## Config

| Env | Required | Default |
|-----|----------|---------|
| `BASIC_AUTH_USER` | yes | — |
| `BASIC_AUTH_PASS` | yes | — |
| `STORE_PATH` | no | `./data/store.db` |
| `HTTP_ADDR` | no | `:8080` |
