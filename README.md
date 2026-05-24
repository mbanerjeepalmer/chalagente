# Chalagente

Multi-tenant WhatsApp agent built on [whatsmeow](https://github.com/tulir/whatsmeow).
Production: [chalagente.com](https://chalagente.com)

Businesses sign up via Cognito, link WhatsApp, and an AI agent replies to customers.
Deployed to Coolify as a Docker container.

## Coolify deploy

- Dockerfile-based application, repo root.
- Persistent volume at `/data`.
- Expose port `8080`.
- Set env vars in Coolify (see local `docs/coolify-env.md` — gitignored, contains secrets).

Required: `BASE_URL`, `COGNITO_REGION`, `COGNITO_USER_POOL_ID`, `COGNITO_CLIENT_ID`, `COGNITO_CLIENT_SECRET`, `COGNITO_DOMAIN`, `COOKIE_SECURE=true`.

## Endpoints

| Route | Auth | Description |
|-------|------|-------------|
| `GET /` | — | Landing page |
| `GET /signup` | — | Redirect to Cognito login |
| `GET /auth/cognito/callback` | — | OAuth callback |
| `POST /logout` | session | Logout |
| `GET /healthz` | — | Health check |
| `/onboarding/*` | session | Onboarding wizard |
| `/app/*` | session | Dashboard |
