#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
run=$(mktemp -d)
db=""
cleanup() {
  if [[ -n "$db" ]]; then docker rm -fv "$db" >/dev/null; fi
  rm -rf -- "$run"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

go build -o "$run/api" ./cmd/kitchen-api
go build -o "$run/mock" ./cmd/restaurant-mock
db=$(docker run -d --rm -p 127.0.0.1::5432 -e POSTGRES_PASSWORD=e2e -e POSTGRES_DB=kitchenflow_e2e postgres:16-alpine)
for ((i=0; i<60; i++)); do
  if docker exec "$db" pg_isready -h 127.0.0.1 -U postgres -d kitchenflow_e2e >/dev/null; then break; fi
  sleep 1
done
docker exec "$db" pg_isready -h 127.0.0.1 -U postgres -d kitchenflow_e2e
address=$(docker port "$db" 5432/tcp)
DATABASE_URL="postgres://postgres:e2e@$address/kitchenflow_e2e?sslmode=disable" python3 scripts/e2e.py "$run"
