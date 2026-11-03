#!/usr/bin/env bash
set -euo pipefail
: "${KITCHENFLOW_TEST_DATABASE_URL:?Set KITCHENFLOW_TEST_DATABASE_URL to a disposable PostgreSQL database}"
cd "$(dirname "$0")/.."
exec go test -race -count=1 -coverpkg=./internal/... ./... "$@"
