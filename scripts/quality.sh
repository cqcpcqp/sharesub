#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_DIR="$ROOT/.run"

mkdir -p "$RUN_DIR/go-cache"

unformatted="$(cd "$ROOT/backend" && gofmt -l ./cmd ./internal ./migrations)"
if [[ -n "$unformatted" ]]; then
  echo "gofmt gate: files need formatting:" >&2
  echo "$unformatted" >&2
  exit 1
fi

git -C "$ROOT" diff --check

(
  cd "$ROOT/backend"
  GOCACHE="$RUN_DIR/go-cache" go vet ./...
  GOCACHE="$RUN_DIR/go-cache" go test ./...
)

(
  cd "$ROOT/frontend"
  pnpm lint
  pnpm test:run
  pnpm typecheck
  pnpm build
  pnpm bundle:check
)
