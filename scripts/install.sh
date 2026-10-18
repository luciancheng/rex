#!/usr/bin/env bash
# Install REX for everyday use: a Go binary on PATH, Cursor hook.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.sh | bash
#   ./scripts/install.sh          # from a clone, installs this tree
set -euo pipefail

REPO_URL="${REX_REPO:-https://github.com/luciancheng/rex.git}"
PREFIX="${REX_HOME:-$HOME/.rex}"
BIN_DIR="${REX_BIN:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
die() { printf 'rex install: %s\n' "$*" >&2; exit 1; }

have_go() {
  command -v go >/dev/null 2>&1 || return 1
  go version >/dev/null 2>&1
}

local_src() {
  if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
    local here
    here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [[ -f "$here/../go.mod" && -z "${REX_FROM_GIT:-}" ]]; then
      cd "$here/.." && pwd
      return 0
    fi
  fi
  return 1
}

have_go || die "need Go 1.22 or newer on PATH (https://go.dev/dl/)"
mkdir -p "$PREFIX" "$BIN_DIR"

if SRC="$(local_src)"; then
  say "building REX from $SRC"
  (cd "$SRC" && go build -o "$BIN_DIR/rex" ./cmd/rex)
else
  say "installing REX from $REPO_URL"
  GOBIN="$BIN_DIR" go install "${REPO_URL#https://}/cmd/rex@latest" 2>/dev/null || {
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT
    git clone --depth 1 "$REPO_URL" "$tmp/rex"
    (cd "$tmp/rex" && go build -o "$BIN_DIR/rex" ./cmd/rex)
  }
fi

if ! "$BIN_DIR/rex" ingest --install-cursor-hook; then
  say "installed rex, but could not write ~/.cursor/hooks.json (ok if Cursor is not installed)"
fi

say
say "REX is installed. Open a new terminal, then:"
say "  rex start"
if ! command -v rex >/dev/null 2>&1 && [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
  say
  say "Add this to your shell profile so \`rex\` works from anywhere:"
  say "  export PATH=\"$BIN_DIR:\$PATH\""
fi
