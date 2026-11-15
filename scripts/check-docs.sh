#!/usr/bin/env bash
#
# Verifies that the documentation covers the code and stays self-consistent:
#
#   * every operation and schema in api/openapi.yaml has a summary/description
#     and every operation has a unique id and a declared tag;
#   * every MCP tool registered in the server appears in docs/MCP.md;
#   * every environment variable read by internal/config appears in
#     docs/DEPLOY.md and in .env.example;
#   * every relative Markdown link between the documents resolves;
#   * every docs/*.md file is listed in the docs index (docs/README.md);
#   * every migration file is listed in docs/DATABASE.md;
#   * every Makefile target is described in docs/DEVELOPMENT.md.
#
# The tool list comes from the binary itself, not from a grep over the sources,
# so a tool that is registered but undocumented fails here. The checks are
# dependency-free: bash, grep, find and the Go toolchain the project already
# requires. No network access and no database are needed.

set -euo pipefail

cd "$(dirname "$0")/.."

failed=0

fail() {
  echo "error: $*" >&2
  failed=1
}

# --- OpenAPI completeness ---------------------------------------------------

if ! go run ./cmd/cublyverse docs api --check; then
  fail "api/openapi.yaml has an undocumented operation or schema"
fi

# --- MCP tools -------------------------------------------------------------

tools="$(go run ./cmd/cublyverse docs mcp --names)"
if [ -z "$tools" ]; then
  fail "the MCP server registered no tools"
fi

for tool in $tools; do
  if ! grep -qF "\`$tool\`" docs/MCP.md; then
    fail "MCP tool \"$tool\" is not documented in docs/MCP.md"
  fi
done

# --- configuration variables ----------------------------------------------

vars="$(
  {
    grep -rhoE 'getenv\("[A-Z0-9_]+"\)' internal/config/*.go
    grep -rhoE 'durationFromEnv\(getenv, "[A-Z0-9_]+"' internal/config/*.go
  } | grep -oE '"[A-Z0-9_]+"' | tr -d '"' | LC_ALL=C sort -u
)"

if [ -z "$vars" ]; then
  fail "no environment variables found in internal/config"
fi

for variable in $vars; do
  if ! grep -qF "\`$variable\`" docs/DEPLOY.md; then
    fail "environment variable $variable is not documented in docs/DEPLOY.md"
  fi
  if ! grep -qE "^${variable}=" .env.example; then
    fail "environment variable $variable is missing from .env.example"
  fi
done

# --- relative Markdown links ----------------------------------------------

# Only the repository documents are checked: node_modules and build output are
# not part of the documentation. External URLs and in-page anchors are skipped.
while IFS= read -r file; do
  dir="$(dirname "$file")"
  while IFS= read -r target; do
    [ -n "$target" ] || continue
    case "$target" in
      http://* | https://* | mailto:* | "#"*)
        continue
        ;;
    esac
    path="${target%%#*}"
    path="${path%%\?*}"
    [ -n "$path" ] || continue
    if [ ! -e "$dir/$path" ]; then
      fail "broken relative link in $file: $target"
    fi
  done < <(grep -oE '\]\([^)[:space:]]+' "$file" | sed 's/^](//')
done < <(find . -name '*.md' -not -path './.git/*' -not -path './web/node_modules/*' -not -path './bin/*' | LC_ALL=C sort)

# --- documentation index ---------------------------------------------------

if [ ! -f docs/README.md ]; then
  fail "docs/README.md (the documentation index) is missing"
else
  for file in docs/*.md; do
    name="$(basename "$file")"
    if [ "$name" = "README.md" ]; then
      continue
    fi
    if ! grep -qF "\`$name\`" docs/README.md; then
      fail "docs/$name is not listed in docs/README.md"
    fi
  done
fi

# --- migrations ------------------------------------------------------------

migrations="$(find internal/store/migrations -maxdepth 1 -name '*.sql' -print | LC_ALL=C sort)"
if [ -z "$migrations" ]; then
  fail "no migration files found in internal/store/migrations"
fi
for file in $migrations; do
  name="$(basename "$file")"
  if ! grep -qF "$name" docs/DATABASE.md; then
    fail "migration $name is not listed in docs/DATABASE.md"
  fi
done

# --- Makefile targets ------------------------------------------------------

targets="$(awk -F: '/^[a-z][a-z0-9_-]*:/ { print $1 }' Makefile | LC_ALL=C sort -u)"
if [ -z "$targets" ]; then
  fail "no Makefile targets found"
fi
for target in $targets; do
  if ! grep -qF "\`$target\`" docs/DEVELOPMENT.md; then
    fail "make target \"$target\" is not documented in docs/DEVELOPMENT.md"
  fi
done

if [ "$failed" -ne 0 ]; then
  exit 1
fi

echo "documentation is up to date"
