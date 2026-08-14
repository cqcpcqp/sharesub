#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASELINE="$ROOT/scripts/code-size-baseline.txt"
MAX_NEW_FILE_LINES=700

baseline_limit() {
  local relative="$1"
  local line
  line="$(awk -F '|' -v path="$relative" '$1 == path { print $2; exit }' "$BASELINE")"
  echo "${line:-$MAX_NEW_FILE_LINES}"
}

status=0
while IFS= read -r file; do
  relative="${file#"$ROOT/"}"
  lines="$(wc -l < "$file" | tr -d ' ')"
  limit="$(baseline_limit "$relative")"
  if (( lines > limit )); then
    echo "size gate: $relative has $lines lines (limit $limit)" >&2
    status=1
  fi
done < <(
  find "$ROOT/backend" "$ROOT/frontend/src" -type f \
    \( -name '*.go' -o -name '*.ts' -o -name '*.vue' -o -name '*.css' \) \
    ! -name '*_test.go' ! -name '*.test.ts' | sort
)

while IFS='|' read -r relative _; do
  [[ -z "$relative" || "$relative" == \#* ]] && continue
  if [[ ! -f "$ROOT/$relative" ]]; then
    echo "size gate: stale baseline entry $relative" >&2
    status=1
  fi
done < "$BASELINE"

while IFS= read -r number; do
  [[ -z "$number" || "$number" == "014" ]] && continue
  names="$(find "$ROOT/backend/migrations" -maxdepth 1 -type f -name "${number}_*.sql" -exec basename {} \; | sort | tr '\n' ' ')"
  echo "migration gate: duplicate sequence $number: $names" >&2
  status=1
done < <(
  find "$ROOT/backend/migrations" -maxdepth 1 -type f -name '[0-9][0-9][0-9]_*.sql' -exec basename {} \; \
    | cut -d_ -f1 | sort | uniq -d
)

if [[ ! -f "$ROOT/CONTRIBUTING.md" || ! -f "$ROOT/docs/data-governance.md" ]]; then
  echo "documentation gate: required engineering governance documents are missing" >&2
  status=1
fi

exit "$status"
