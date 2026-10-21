#!/usr/bin/env bash
# Install REX: a Go binary on PATH, plus the Cursor hook when Cursor is here.
# Other agents are opt-in; the script prints the command for each one it finds.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/luciancheng/rex/HEAD/scripts/install.sh | bash
#   curl -fsSL …/install.sh | REX_REF=dev bash        # a tag, branch or commit
#   ./scripts/install.sh                                # from a clone: installs this tree
set -euo pipefail

MODULE="github.com/luciancheng/rex"
REPO_URL="${REX_REPO:-https://$MODULE.git}"
# A local path is taken from here, not from the temp folder git runs in.
[[ -d "$REPO_URL" ]] && REPO_URL="$(cd "$REPO_URL" && pwd)"
REF="${REX_REF:-}"
BIN_DIR="${REX_BIN:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
die() { printf 'rex install: %s\n' "$*" >&2; exit 1; }

have_go() {
  command -v go >/dev/null 2>&1 || return 1
  go version >/dev/null 2>&1
}

local_src() {
  if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" && -z "${REX_FROM_GIT:-}" && -z "$REF" ]]; then
    local here
    here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [[ -f "$here/../go.mod" ]]; then
      cd "$here/.." && pwd
      return 0
    fi
  fi
  return 1
}

# build_from_git fetches exactly REF (a tag, branch or commit) and builds
# it. A branch-only clone could not fetch a commit.
REX_TMP=""
trap '[[ -n "$REX_TMP" ]] && rm -rf "$REX_TMP"' EXIT
build_from_git() {
  # Called under ||, where set -e is off, so every step checks itself.
  REX_TMP="$(mktemp -d)" || return 1
  git -C "$REX_TMP" init -q || return 1
  git -C "$REX_TMP" remote add origin "$REPO_URL" || return 1
  git -C "$REX_TMP" fetch -q --depth 1 origin "${REF:-HEAD}" || return 1
  git -C "$REX_TMP" checkout -q FETCH_HEAD || return 1
  (cd "$REX_TMP" && go build -o "$BIN_DIR/rex" ./cmd/rex)
}

have_go || die "need Go 1.22 or newer on PATH (https://go.dev/dl/)"
mkdir -p "$BIN_DIR"
BIN_DIR="$(cd "$BIN_DIR" && pwd)" # go build runs in another folder

if SRC="$(local_src)"; then
  say "building REX from $SRC"
  (cd "$SRC" && go build -o "$BIN_DIR/rex" ./cmd/rex)
elif [[ "$REPO_URL" == "https://$MODULE.git" || "$REPO_URL" == "https://$MODULE" ]] &&
  say "installing $MODULE/cmd/rex@${REF:-latest}" &&
  GOBIN="$BIN_DIR" go install "$MODULE/cmd/rex@${REF:-latest}"; then
  :
else
  say "building REX from $REPO_URL${REF:+ at $REF}"
  build_from_git || die "could not fetch and build ${REF:-the default branch} from $REPO_URL"
fi

REX="$BIN_DIR/rex"
"$REX" --version

# Cursor is the one agent wired by default, as before. The old flag is
# used because every rex binary knows it, whatever REX_REF installed.
hints=()
if [[ -d "$HOME/.cursor" ]]; then
  "$REX" ingest --install-cursor-hook || say "rex is installed, but the Cursor hook could not be written"
else
  say "Cursor isn't here; once it is: rex ingest --install cursor"
fi
claude="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
[[ -d "$claude" ]] && hints+=("rex ingest --install claude        # Claude Code (add --project for one repo)")
[[ -d "${CODEX_HOME:-$HOME/.codex}" ]] && hints+=("rex ingest --install codex         # Codex, then trust the hooks in /hooks")
[[ -d "${XDG_CONFIG_HOME:-$HOME/.config}/opencode" ]] && hints+=("rex ingest --install opencode      # OpenCode")
# The skill lives in ~/.claude/skills: offered only where that folder is.
skill_for="Claude Code, Cursor and OpenCode"
[[ -n "${CLAUDE_CONFIG_DIR:-}" ]] && skill_for="Claude Code (it goes in \$CLAUDE_CONFIG_DIR)"
[[ -d "$claude" ]] && hints+=("rex ingest --install-skill         # teach $skill_for the plan format")
if ((${#hints[@]})); then
  say
  say "Other agents (opt-in):"
  for h in "${hints[@]}"; do say "  $h"; done
fi

say
say "REX is installed. Open a new terminal, then, in the repo you want to send off:"
say "  cd <your-repo> && rex start"
if ! command -v rex >/dev/null 2>&1 && [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
  say
  say "Add this to your shell profile so \`rex\` works from anywhere:"
  say "  export PATH=\"$BIN_DIR:\$PATH\""
fi
