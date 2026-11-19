#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
docker compose up -d postgres redis
for attempt in {1..30}; do
  if docker compose exec -T postgres pg_isready -U tyrelink -d tyrelink >/dev/null 2>&1; then break; fi
  sleep 1
done
for migration in migrations/*.sql; do
  docker compose exec -T postgres psql -U tyrelink -d tyrelink -v ON_ERROR_STOP=1 < "$migration"
done
docker compose up -d --build api
