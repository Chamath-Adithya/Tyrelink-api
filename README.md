# Tyrelink API

Go API for catalogue reads, WhatsApp order leads, warranty/order lookup and
protected administration. PostgreSQL is the source of truth for mutable
business data; the frontend is a separate deployable.

Run locally: `cp .env.example .env`, start PostgreSQL with the local compose
file, apply migrations, then `go run ./cmd/api`.

Production must use the infra repository's private-network deployment, HTTPS
reverse proxy, external secrets, backups and a shared/edge rate limiter before
multiple API instances are added. Never use the development database password
or expose PostgreSQL publicly.

Go HTTP API and PostgreSQL migrations for product catalogue, WhatsApp order leads, warranty records, orders and protected admin actions.

Run locally: `go test ./...`, then set `DATABASE_URL`, `TYRELINK_ADMIN_PASSWORD`, `TYRELINK_SESSION_SECRET` and run `go run ./cmd/api`.
