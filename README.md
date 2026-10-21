<p align="center">
  <img src="assets/rex.png" width="200" alt="REX — a pixel-art dinosaur mascot" />
</p>

<h1 align="center">REX</h1>

<p align="center">
  <strong>Small dino. Big opinions.</strong>
</p>

<p align="center">
  Write a plan, send the work off to your coding agent,<br />
  and watch the tape from a terminal that stays out of the way.
</p>

<p align="center">
  <code>rex start</code>
</p>

<p align="center">
  macOS · Linux · Windows 10+ &nbsp;·&nbsp; Go 1.22+ &nbsp;·&nbsp; MIT
</p>

REX is a watch-only sidecar. It does not run tools, approve them, or steer the agent. You talk to Cursor or Claude Code as usual. Rex holds the plan you are sending off, and the pane next to the dino shows thoughts, edits, shell, questions, and your prompts as they happen.

Plans live in the repo at `rex/plans/` as JSON named `<name>.rex.json`. Commit that folder; teammates open the same plan from git. A plan is a goal, hard scope edges, and a flat list of PR-sized tasks, with `parent` for outline and `depends_on` for order ([format v1](docs/plan-format.md)). New plans start as an empty v1 stub for your agent to fill.

The install script wires Cursor. Claude Code is one opt-in command away (`rex ingest --install claude`, or add `--project` to wire just this repo). Codex too: `rex ingest --install codex`, then trust the hooks once in Codex's `/hooks`. OpenCode: `rex ingest --install opencode`. Works in Terminal.app, iTerm, Windows Terminal, and VS Code. Install a `rex` binary once — after that it works from any folder.

## Send off a project

1. **Pick or create a plan** in `rex/plans/`. Folders are for grouping (`rex/plans/onboarding/auth.rex.json`). Duplicate names are refused.
2. **`rex start`** from the repo. Create a plan, open one, or jump to the latest. Tab is the live tape. The header stays on the current plan path.
3. **Leave him running** and prompt your agent. Rex watches. Idle when nothing is in flight; typing while the agent is working. Ctrl+C twice to quit.

Opening a plan shows its outline: every task in file order, which one is pinned, what is done, and what is waiting on what. ⏎ on a task shows its detail. Rex re-reads the file as your agent edits it. It does not inject the JSON into the agent — you still send the work off in chat. `s⏎` builds a send-off from the plan file (goal, scope, tasks, the next task, what was done and, for a plan that is a stack of PRs, the task's branch and the branch to cut it from and open its PR against) for you to paste into a fresh chat or hand to a teammate; Rex never sends it or runs git. `b⏎` gives you a planning brief that asks your agent to fill the plan. When your agent edits files outside the pinned task's folders, or you ask for something its `scope.out` rules out, Rex shows drift (soft or hard) on the status line and the tape; it never corrects the agent. `p⏎` pins a task; `d⏎` marks the pin done and writes what was actually done, which files changed and how it was checked into the plan, prefilled from what your agent said and what Rex saw.

## Install

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.sh | bash
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.ps1 | iex
```

That builds a binary into `~/.local/bin` (PowerShell adds it to your user PATH; the shell script prints the `export PATH=…` line when it isn't there yet), and wires Cursor (if it's installed) so this machine's agent chat is watched. Claude Code, Codex and OpenCode are opt-in: the script prints the one command for each one it finds. You need [Go 1.22+](https://go.dev/dl/). Open a new terminal, then `cd` into the repo you want to send off and run `rex start`.

To install a specific tag, branch or commit, set `REX_REF` (for example `curl -fsSL …/install.sh | REX_REF=dev bash`, or a commit SHA).

Already have Go?

```bash
go install github.com/luciancheng/rex/cmd/rex@latest
rex ingest --install cursor
rex start
```

## Usage

```bash
rex start                         # plans, then the live tape
rex start --static                # print one frame and exit
rex ingest --install cursor       # (re)wire Cursor; --uninstall cursor takes it out
rex ingest --install claude       # wire Claude Code (~/.claude/settings.json, or $CLAUDE_CONFIG_DIR)
rex ingest --install claude --project   # this repo only, git-excluded
rex ingest --install codex        # wire Codex ($CODEX_HOME/hooks.json), then /hooks → trust
rex ingest --install opencode     # an OpenCode plugin (~/.config/opencode/plugins/rex.js, or $XDG_CONFIG_HOME)
rex ingest --install-skill        # teach Claude Code, Cursor and OpenCode the plan format (~/.claude/skills/rex-plan); --project for this repo
rex --help
```

Tab switches the plan ↔ the tape. In the navigator: create, open, open latest, recents; browse folders and `.rex.json` files. Nothing happens until you press Enter: type `?` for keys and verbs, `p` on the outline to pin a task, `d` to mark it done, or `breakdance` on the tape (`/breakdance` in the navigator, where typing finds or names plans) to play the dance sheet once. Anything else you type on the tape is a local note; it is never sent to your agent. `NO_COLOR=1` disables ANSI color. Pipes and CI print a static frame instead of opening the live view.

## Hack on it

```bash
git clone https://github.com/luciancheng/rex.git
cd rex
go test ./...
go run ./cmd/rex start
```

Do not `go build -o rex` at the repo root — `rex/` is the plans directory.

## Edit the idle sprite

Idle Rex is the 40×40 PNG at `assets/idle.png`, converted pixel-for-pixel into terminal art. `assets/rex.png` is the same drawing, scaled up for this README.

```bash
go run ./cmd/importsprite assets/idle.png --apply
```

`--apply` writes the grid into `internal/mascot/sprites.go` and the sampled RGB values into the `yellow` palette. Restart `rex start` after that. Idle and future poses should stay 40×40.

| Letter | Role |
| --- | --- |
| `k` | outline |
| `y` | body |
| `Y` | highlight |
| `o` | shade |
| `d` | dark |
| `n` | mid |
| `a` | accent |
| `b` | belly |
| `p` | blush |
| `s` | shadow |

Blink, glance, and squat stay transforms on the idle grid. Breakdance and typing are 40×40 sheets (`assets/breakdance.png`, `assets/typing.png`) mapped onto the yellow palette — not blit.

## Layout

```
rex/plans/                  commitable send-off plans (<name>.rex.json)
assets/idle.png             source 40×40 sprite
assets/rex.png              README art (same drawing, scaled)
cmd/rex                     rex start / rex ingest
cmd/importsprite            PNG → CLI pixel art
internal/mascot/            sprites, palettes, half-block renderer
internal/tui/               plans navigator + live tape
internal/watch/             agent event feed (hooks → JSONL → pane)
scripts/install.sh          user install (macOS/Linux)
scripts/install.ps1         user install (Windows)
.github/workflows/ci.yml    GitHub Actions
```

## Tests

```bash
go test ./...
```

CI runs the same suite on Ubuntu, macOS, and Windows across Go 1.22 and 1.24.

## Contributing

1. Install Go 1.22+.
2. Keep mascot rendering in `internal/mascot/` and commands in `internal/cli/`.
3. Add `*_test.go` next to the package for new behavior.
4. Run the pre-PR gate in [agents.md](agents.md) (Workflow) before opening a pull request. If you can't install its tools, run at least `go test ./...`: CI runs the tests on Go 1.22 and 1.24, and reviewers run the rest of the gate.

Code conventions, the Go 1.22 floor, and the branch / PR workflow are in [agents.md](agents.md).

## License

MIT. See [LICENSE](LICENSE).
