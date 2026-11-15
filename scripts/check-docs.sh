#!/usr/bin/env bash
#
# Verifies that the documentation covers the code:
#
#   * every MCP tool registered in the server appears in docs/MCP.md;
#   * every environment variable read by internal/config appears in
#     docs/DEPLOY.md and in .env.example.
#
# The tool list comes from the binary itself, not from a grep over the sources,
# so a tool that is registered but undocumented fails here.

set -euo pipefail

cd "$(dirname "$0")/.."

failed=0

fail() {
  echo "error: $*" >&2
  failed=1
}

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

if [ "$failed" -ne 0 ]; then
  exit 1
fi

echo "documentation is up to date"
