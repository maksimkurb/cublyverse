#!/usr/bin/env bash
#
# Verifies that the generated code is up to date.
#
# The check does not depend on git: it fingerprints the generated files, runs
# the generators, and fails when the content changed. That catches both a stale
# api/openapi.yaml edit and a non-deterministic generator.

set -euo pipefail

cd "$(dirname "$0")/.."

GENERATED_DIRS=(internal/httpapi/gen internal/store/db)
GENERATED_FILES=(web/src/api/schema.d.ts)

fingerprint() {
  local paths=() dir file
  for dir in "${GENERATED_DIRS[@]}"; do
    if [ -d "$dir" ]; then
      paths+=("$dir")
    fi
  done
  for file in "${GENERATED_FILES[@]}"; do
    if [ -f "$file" ]; then
      paths+=("$file")
    fi
  done

  if [ "${#paths[@]}" -eq 0 ]; then
    echo "no generated files found"
    return
  fi

  find "${paths[@]}" -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum
}

before="$(fingerprint)"

# Regenerate. The Makefile is invoked directly so the outer make job server does
# not interfere.
MAKEFLAGS= make --no-print-directory gen >/dev/null

after="$(fingerprint)"

if [ "$before" != "$after" ]; then
  echo "error: generated code is out of date" >&2
  echo "run 'make gen' and commit the regenerated files" >&2
  diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") >&2 || true
  exit 1
fi

echo "generated code is up to date"
