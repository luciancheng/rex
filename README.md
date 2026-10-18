<p align="center">
  <img src="assets/rex.png" width="200" alt="REX — a pixel-art dinosaur mascot" />
</p>

<h1 align="center">REX</h1>

<p align="center">
  <strong>Small dino. Big opinions.</strong>
</p>

<p align="center">
  A terminal companion that sits next to your coding agent<br />
  and turns the firehose into something a human can actually read.
</p>

<p align="center">
  <code>rex start</code>
</p>

<p align="center">
  macOS · Linux · Windows 10+ &nbsp;·&nbsp; Go 1.22+ &nbsp;·&nbsp; MIT
</p>

Leave him running. Talk to Cursor as usual. Thoughts, tools, questions, and your prompts show up in the pane to the right of the dino. Claude Code and Codex mappers exist; Cursor is the one that's wired today.

Works in Terminal.app, iTerm, Windows Terminal, and VS Code. Install a `rex` binary once. After that, `rex` works from any folder — it does not need to share a directory with the agent.

## Install

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.sh | bash
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.ps1 | iex
```

That builds a binary into `~/.local/bin`, puts `rex` on your PATH, and wires Cursor so this machine's agent chat is watched. You need [Go 1.22+](https://go.dev/dl/). Open a new terminal, then `rex start` from anywhere.

Already have Go?

```bash
go install github.com/luciancheng/rex/cmd/rex@latest
rex ingest --install-cursor-hook
rex start
```

## Usage

Ctrl+C twice to quit. Type `breakdance` in the input box to play the dance sheet once.

```bash
rex start                         # live view
rex start --static                # print one frame and exit
rex ingest --install-cursor-hook  # reinstall the Cursor watcher
rex --help
```

`NO_COLOR=1` disables ANSI color. Pipes and CI print a static frame instead of opening the live view.

Idle and future poses should be 40×40.

## Hack on it

```bash
git clone https://github.com/luciancheng/rex.git
cd rex
go test ./...
go run ./cmd/rex start
```

## Edit the idle sprite

Idle Rex is the 40×40 PNG at `assets/idle.png`, converted pixel-for-pixel into terminal art. `assets/rex.png` is the same drawing, scaled up for this README.

Rebuild from a PNG (reads every pixel and the palette):

```bash
go run ./cmd/importsprite assets/idle.png
go run ./cmd/importsprite assets/idle.png --apply
```

`--apply` writes the grid into `internal/mascot/sprites.go` and the sampled RGB values into the `yellow` palette. Restart `rex start` after that.

You can also edit the data by hand:

- **Pixels / size** — `internal/mascot/sprites.go`, the idle `Pixels(...)` grid. Each character is one pixel. Add or remove characters and lines to change size.
- **Colors** — `internal/mascot/palettes.go`, the `yellow` RGB triples.

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

## Add sprites and animations later

The registry lives in `internal/mascot/catalog.go`. `rex start` plays the `idle` animation (blink and glance) inside the 40×40 stage.

```go
blink := mascot.RegisterSprite(mascot.Sprite{Name: "idle_blink", Rows: mascot.Pixels("...")})
mascot.RegisterAnimation(mascot.Animation{
    Name: "idle",
    Frames: []mascot.Frame{{"idle", 20}, {"idle_blink", 3}},
})
```

`Hold` is in live-viewer ticks. A one-frame animation is a still.

## Layout

```
assets/idle.png             source 40×40 sprite
assets/rex.png              README art (same drawing, scaled)
cmd/rex                     rex start / rex ingest
cmd/importsprite            PNG → CLI pixel art
internal/mascot/            sprites, palettes, half-block renderer
internal/tui/               live viewer
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
