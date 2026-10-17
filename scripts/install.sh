#!/usr/bin/env bash
# Install REX for everyday use: a private venv, `rex` on your PATH, Cursor hook.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.sh | bash
#   ./scripts/install.sh          # from a clone, installs this tree
set -euo pipefail

REPO_URL="${REX_REPO:-https://github.com/luciancheng/rex.git}"
PREFIX="${REX_HOME:-$HOME/.rex}"
BIN_DIR="${REX_BIN:-$HOME/.local/bin}"
VENV="$PREFIX/venv"

say() { printf '%s\n' "$*"; }
die() { printf 'rex install: %s\n' "$*" >&2; exit 1; }

find_python() {
  local candidate
  for candidate in python3 python; do
    if command -v "$candidate" >/dev/null 2>&1; then
      if "$candidate" -c 'import sys; raise SystemExit(0 if sys.version_info >= (3, 9) else 1)'; then
        printf '%s\n' "$candidate"
        return 0
      fi
    fi
  done
  return 1
}

install_src() {
  local here
  if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
    here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [[ -f "$here/../pyproject.toml" && -z "${REX_FROM_GIT:-}" ]]; then
      cd "$here/.." && pwd
      return 0
    fi
  fi
  printf 'git+%s\n' "$REPO_URL"
}

PYTHON="$(find_python)" || die "need Python 3.9 or newer on PATH"
SRC="$(install_src)"

say "installing REX into $VENV"
mkdir -p "$PREFIX" "$BIN_DIR"
"$PYTHON" -m venv "$VENV"
"$VENV/bin/python" -m pip install --upgrade pip >/dev/null
"$VENV/bin/python" -m pip install "$SRC"
ln -sf "$VENV/bin/rex" "$BIN_DIR/rex"

if ! "$VENV/bin/rex" ingest --install-cursor-hook; then
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
