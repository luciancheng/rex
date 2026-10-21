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
  <a href="https://github.com/luciancheng/rex/actions/workflows/ci.yml"><img src="https://github.com/luciancheng/rex/actions/workflows/ci.yml/badge.svg?branch=dev" alt="CI" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/luciancheng/rex" alt="License: MIT" /></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/luciancheng/rex" alt="Go version" /></a>
  <a href="https://github.com/luciancheng/rex/stargazers"><img src="https://img.shields.io/github/stars/luciancheng/rex" alt="GitHub stars" /></a>
</p>

<p align="center">macOS · Linux · Windows 10+</p>

Rex is a terminal sidecar for developers who work with an AI coding agent: Cursor, Claude Code, Codex or OpenCode. You keep talking to your agent as usual. Rex sits in a second terminal with a pixel-art dino and shows two things:
- **the plan**: a JSON file in your repo that breaks the work into PR-sized tasks;
- **the tape**: what the agent is doing right now, as it happens (its thoughts, commands, edits, questions, replies and your prompts).

Rex is **watch-only**. It never runs tools, approves them, steers the agent or sends it anything. When you want the agent to start a task, Rex builds a send-off brief from the plan for you to paste into the chat.

```
REX plan                            idle · rex/plans/cuj/sendoff-demo.rex.json
▸ card-highlight  Selected plan card is one block (active)
── Send-off demo · active · 1/3 ──────────────────────────────────────────────
  last ✓ plans-ui: Home cards, folder browser, duplicate names refused

  ✓ plans-ui        Plans navigator · Create, open and browse rex/plans in th…
▌ ▸ card-highlight  Selected plan card is one block · Bar, border and fill co…
  · load-outline    Outline view · Opening a plan shows…  after card-highlight
╭────────────────────────────────────────────────────────────────────────────╮
│ > (s⏎ sendoff · d⏎ done · p⏎ pin · ?⏎ help)                                │
╰────────────────────────────────────────────────────────────────────────────╯
outline · tab tape · esc plans · ⏎ detail · ?⏎ help
```

## Requirements

- [Go 1.22+](https://go.dev/dl/). There are no prebuilt binaries yet, so both installs build from source.
- macOS, Linux or Windows 10+, and a terminal at least 24 columns wide (66+ to show the dino). It works in Terminal.app, iTerm, Windows Terminal and the VS Code terminal.
- A git repo for your plans (outside one, Rex says where plans would go).
- Optional, on Linux: `wl-copy`, `xclip` or `xsel` so Rex can copy briefs to the clipboard.

## Install

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/luciancheng/rex/HEAD/scripts/install.sh | bash
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/luciancheng/rex/HEAD/scripts/install.ps1 | iex
```

The script builds `rex` into `~/.local/bin`. PowerShell adds that folder to your user PATH; the shell script prints the `export PATH=…` line when it isn't on yours yet. If Cursor is installed, the script wires it. For Claude Code, Codex and OpenCode it prints the one command that wires each one it finds. To install a specific tag, branch or commit, set `REX_REF`, for example `curl -fsSL …/install.sh | REX_REF=dev bash`.

Or with `go install`:

```bash
go install github.com/luciancheng/rex/cmd/rex@latest
```

This puts `rex` in `$(go env GOPATH)/bin`. Then wire your agent (see [Agents](#agents)).

## Quick start

1. Wire your agent (see [Agents](#agents)), open a new terminal, `cd` into your repo and run `rex start`.
2. Type a plan name and press ⏎. Rex creates `rex/plans/<name>.rex.json` and opens its outline.
3. `b⏎` copies a planning brief. Paste it into your agent and it fills in the plan; the outline updates as the file changes. (`rex ingest --install-skill` teaches Claude Code, Cursor and OpenCode the plan format, so they can plan without the brief.)
4. `p⏎` pins the task to work on, and `s⏎` copies a send-off for it. Paste that into a fresh agent chat.
5. Press Tab to watch the tape while the agent works. When it's done, Tab back and `d⏎` marks the task done, prefilled from what the agent said and what Rex saw.
6. Ctrl+C twice quits.

If the agent edits files outside the pinned task, or you ask for something the plan's `scope.out` rules out, Rex shows **drift** on the status line and the tape. It never corrects the agent.

## Agents

Rex watches agents through their hooks: each wired agent runs `rex ingest` on its events and appends them to a local feed. Apart from the install script wiring Cursor, nothing is wired until you run `rex ingest --install`.

| Agent | Wire it | Rex edits | Notes |
| --- | --- | --- | --- |
| Cursor | the install script, or `rex ingest --install cursor` | `~/.cursor/hooks.json` | Cursor reports only the final reply of each turn. |
| Claude Code | `rex ingest --install claude` (add `--project` for this repo only) | `~/.claude/settings.json` (or `$CLAUDE_CONFIG_DIR`), or this repo's git-excluded `.claude/settings.local.json` | |
| Codex | `rex ingest --install codex` | `~/.codex/hooks.json` (or `$CODEX_HOME`) | Then trust the hooks once in Codex's `/hooks`, and again after moving `rex` (the hook names its path). |
| OpenCode | `rex ingest --install opencode` | `~/.config/opencode/plugins/rex.js` (or `$XDG_CONFIG_HOME`) | Restart OpenCode to load the plugin. |

- `rex ingest --install auto` wires every agent found on this machine.
- Your other hooks and settings are kept byte for byte, and the previous file is saved next to it as `.bak`.
- `rex ingest --install-skill` installs the `rex-plan` skill (`~/.claude/skills/rex-plan`, or `--project` for this repo; with `CLAUDE_CONFIG_DIR` set it goes there, where only Claude Code reads it). It teaches Claude Code, Cursor and OpenCode the plan format, stacked PRs and a review pass before each PR. Codex doesn't read Claude's skills folder: with Codex, use `b⏎` and `s⏎`.

## Keys

Nothing fires until you press ⏎: typed words are verbs.

| Key or verb | Does |
| --- | --- |
| Tab | switch between the plan and the tape |
| ⏎ | open the selection, or run what you typed |
| Esc | clear what you typed, then go up one level |
| ↑ ↓ · PgUp PgDn | move, or scroll the tape |
| ← → | plans: up a folder, into a folder or a task's detail |
| Home End | first and last; on the tape, oldest and live (also Ctrl+A, Ctrl+E) |
| Ctrl+U · Ctrl+W | clear the line · delete a word |
| Ctrl+L | repaint |
| Ctrl+C twice | quit |
| `p⏎` · `pin <id>⏎` | pin the selected task · pin any task |
| `d⏎` | mark the pinned task done |
| `s⏎` | copy a send-off for the pinned task (`c⏎` copies again, `o⏎` prints it plain) |
| `b⏎` | copy a planning brief |
| `?⏎` | every key and verb, and feed health |
| `breakdance⏎` | one dance |

On the plans home, typing finds a plan or names a new one; start a verb with `/` there (`/breakdance`). Anything else you type on the tape is a local note that never reaches your agent. The full key map is in [docs/ux.md](docs/ux.md).

## Plans

Plans live in the repo at `rex/plans/` as `<name>.rex.json`; folders group them (`rex/plans/onboarding/auth.rex.json`). A plan is a goal, hard scope edges, and a flat list of PR-sized tasks, with `parent` for the outline and `depends_on` for order. A plan can also be a stack of PRs, one per task, and the send-off then says which branch to cut and which PR to stack on. The format is [docs/plan-format.md](docs/plan-format.md).

Commit the `rex/plans/` folder so teammates open the same plans from git. A plan that is a stack of PRs is committed before the stack starts or after it lands, not on its branches (see [Stacks](docs/plan-format.md#stacks)).

## What Rex stores

Rex makes no network connections, and everything stays on this machine:

- **`~/.rex/session.jsonl`** (and the `session.jsonl.lock` next to it): the feed every wired agent appends to. It holds your prompts, the agent's thoughts and replies, the commands it runs, file paths and working folders, from every repo on this machine. File bodies and diffs are left out, and each event's text is capped at 32 KB. Only your user can read it (`0600`, in a `0700` folder). It only grows; delete it whenever you like. Set `REX_EVENTS` to keep it somewhere else.
- **`~/.rex/state.json`**: the last plan you opened in each repo, and when each task was pinned.
- **Your repo**: plans in `rex/plans/`, written only when you act in Rex. Create and New folder write the new plan or folder (and `rex/plans/.gitkeep` the first time); pin and done write task statuses, a task's completion fields and the plan's own status.
- **Agent configs**: only the files in the [Agents](#agents) table, each with a `.bak` copy of what was there, and only when you run `--install`. `--install claude --project` also adds two lines to the repo's `.git/info/exclude`, and `--install-skill` writes the `rex-plan` skill folder.

Copying a brief runs your system's clipboard tool (`pbcopy`, `wl-copy`, `xclip`, `xsel`, `clip.exe`); over SSH, or when no tool works, it asks your terminal to copy (OSC 52). The install script downloads Rex's source through Go and git.

## Configuration

| Setting | Effect |
| --- | --- |
| `REX_EVENTS` | where the feed lives (default `~/.rex/session.jsonl`). Hooks read it from the agent's environment. |
| `rex start --events <path>` · `rex ingest --events <path>` | the same, for one run |
| `NO_COLOR` · `rex start --no-color` | no colour |
| `FORCE_COLOR` | colour even when the output isn't a terminal |
| `TERM=dumb` · `rex start --static` | print one static frame and exit (pipes and CI do this too) |
| `CLAUDE_CONFIG_DIR` · `CODEX_HOME` · `XDG_CONFIG_HOME` | where Claude Code's, Codex's and OpenCode's configs live |
| `REX_REF` · `REX_REPO` · `REX_BIN` | for the install script: what to build, from which repo, into which folder |

## Uninstall

1. Take Rex out of each agent you wired. Your other hooks are left alone.
   ```bash
   rex ingest --uninstall cursor
   rex ingest --uninstall claude      # in each repo you wired with --project: rex ingest --uninstall claude --project
   rex ingest --uninstall codex
   rex ingest --uninstall opencode
   rex ingest --uninstall-skill       # if you installed it (add --project in each repo)
   ```
2. Then delete the binary: `rm "$(command -v rex)"` (PowerShell: `Remove-Item ~\.local\bin\rex.exe`, then take that folder out of your user PATH). Do this last: hooks call `rex` by its path, so deleting it first leaves them pointing at nothing.
3. Delete Rex's data: `rm -rf ~/.rex` (PowerShell: `Remove-Item -Recurse -Force ~\.rex`), and the file `REX_EVENTS` points to if you set it. The `.bak` files next to your agent configs still hold Rex's hooks: delete them, don't restore them. Your plans in `rex/plans/` are yours to keep or delete.

## Troubleshooting

- **The tape stays empty.** Type `?⏎`. The feed block shows the feed path, the last event, and whether each agent is wired, with the command that wires it. Start a new agent session after wiring.
- **Codex shows nothing.** Trust the Rex hooks in Codex's `/hooks`, and again after moving `rex`.
- **Events stopped after moving `rex`, or installing it to a different folder.** Hooks call the binary by its path. Run the `rex ingest --install <agent>` command again.
- **Events land in another file.** Hooks use the agent's `REX_EVENTS`, not your shell's. Set it in both places, or in neither.
- **No dino, or "widen".** The dino needs 66+ columns. Under 24 columns of text Rex asks you to widen the terminal.
- **"sent (OSC 52) · may be blocked".** Rex asked your terminal to copy, which it does over SSH or when no clipboard tool works. Allow clipboard access in your terminal (in tmux, `set -g set-clipboard on`), or use `o⏎` to print the brief plain and copy it by hand. On a local Linux desktop, installing `wl-copy`, `xclip` or `xsel` avoids it.
- **"rex/ is a file here · plans off".** A file named `rex` sits at the repo root, where the plans folder goes. Move it.

## Contributing

Contributions are welcome: see [CONTRIBUTING.md](CONTRIBUTING.md), and [AGENTS.md](AGENTS.md) for the code conventions, the workflow and the pre-PR gate. To hack on it:

```bash
git clone https://github.com/luciancheng/rex.git && cd rex
go test ./...
go run ./cmd/rex start
go build -o dist/rex ./cmd/rex     # not -o rex: rex/ is the plans folder
```

Please report security issues privately: see [SECURITY.md](SECURITY.md).

## License

MIT. See [LICENSE](LICENSE).

The idle dino (`assets/idle.png`, and `assets/rex.png` scaled from it) was drawn by hand by Lucian Cheng and is MIT like the code. The breakdance and typing sheets (`assets/breakdance.png`, `assets/typing.png`) were made with an AI image generator.
