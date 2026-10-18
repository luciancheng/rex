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

REX is a watch-only sidecar. It does not run tools, approve them, or steer the agent. You talk to Cursor as usual. Rex holds the plan you are sending off, and the pane next to the dino shows thoughts, edits, shell, questions, and your prompts as they happen.

Plans live in the repo at `rex/plans/` as JSON named `<name>.rex.json`. Commit that folder. Teammates open the same breakdown, pick a plan, and watch the same send-off.

Claude Code and Codex mappers exist; Cursor is the one that's wired today. Works in Terminal.app, iTerm, Windows Terminal, and VS Code. Install a `rex` binary once — after that it works from any folder.

## Send off a project

1. **Break the work down** in `rex/plans/`. Folders are for grouping (`rex/plans/onboarding/auth.rex.json`). Duplicate names are refused.
2. **`rex start`** from the repo. Create a plan, open one, or jump to the latest. Tab is the live tape. The header stays on the current plan path.
3. **Leave him running** and prompt Cursor. Rex watches. Idle when nothing is in flight; typing while the agent is working. Ctrl+C twice to quit.

Opening a plan selects it in Rex. It does not inject the JSON into the agent — you still send the work off in chat. The file is the shared map, not a script.

## Plan files

One file is one unit of work (a feature, a fix, a migration), not the whole repo. Folders are human hierarchy. Inside the file, tasks are a **flat list** with `parent` for outline and `depends_on` for order.

```json
{
  "version": 1,
  "id": "file-storage",
  "title": "Plan file storage and navigator",
  "goal": "People can create, open, and share plans under rex/plans.",
  "status": "active",
  "scope": {
    "in": ["JSON plan format", "folder browser"],
    "out": ["agent auto-loads the plan"]
  },
  "tasks": [
    {
      "id": "card-highlight",
      "title": "Selected plan card is one block",
      "parent": "plans-ui",
      "status": "done",
      "scope": {
        "in": ["home cards", "browse rows"],
        "out": ["chat log chrome"]
      },
      "needs": ["Cyan ▌ only marked the top border"],
      "does": ["Bar, border, and fill cover the whole card"],
      "depends_on": ["plans-ui"],
      "verify": [
        "Open latest: ▌ on all three card rows",
        "go test ./internal/tui -run PlanCardSelection"
      ],
      "files": [
        "internal/tui/plans.go",
        "internal/tui/live.go",
        "internal/tui/plans_test.go"
      ]
    }
  ]
}
```

| Field | Meaning |
| --- | --- |
| `scope.in` / `scope.out` | Hard edges so a task does not eat the next one |
| `needs` | Preconditions — facts and blockers, not the work |
| `does` | Accomplishments — what is true when it is done |
| `depends_on` | Other task ids that should be done first |
| `verify` | How a stranger would check success |
| `files` | Probable paths (hints, not a lockfile) |

Status is `pending`, `active`, `done`, `blocked`, or `skipped`. Write it down; do not infer it. Empty `{}` is a valid stub until you fill the breakdown. New files from the TUI start that way.

Do not put agent prompts, auto-approve lists, or diffs in the plan. That is steering. Keep ids as readable slugs so git blame and deps stay human.

## Install

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.sh | bash
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.ps1 | iex
```

That builds a binary into `~/.local/bin`, puts `rex` on your PATH, and wires Cursor so this machine's agent chat is watched. You need [Go 1.22+](https://go.dev/dl/). Open a new terminal, then `rex start` from the repo you want to send off.

Already have Go?

```bash
go install github.com/luciancheng/rex/cmd/rex@latest
rex ingest --install-cursor-hook
rex start
```

## Usage

```bash
rex start                         # plans, then the live tape
rex start --static                # print one frame and exit
rex ingest --install-cursor-hook  # reinstall the Cursor watcher
rex --help
```

Tab switches plans ↔ chat. In the navigator: create, open, open latest, recents; browse folders and `.rex.json` files. Type `breakdance` in the chat box to play the dance sheet once. `NO_COLOR=1` disables ANSI color. Pipes and CI print a static frame instead of opening the live view.

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
4. Run `go test ./...` before opening a pull request.

## License

MIT. See [LICENSE](LICENSE).
