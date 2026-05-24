FROM golang:1.25-alpine AS build
RUN apk add --no-cache build-base
WORKDIR /src
COPY go.mod ./
COPY go.sum* ./
RUN go mod download || true
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /out/server .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/server /server
ENV DB_PATH=/data/app.db
ENV HTTP_ADDR=:8080
ENV COOKIE_SECURE=true
# BASE_URL must be set at runtime to your public origin, e.g.
# https://chalagente.example.com — magic-link URLs are built from it.
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/server"]
