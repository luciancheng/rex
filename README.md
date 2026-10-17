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
  macOS · Linux · Windows 10+ &nbsp;·&nbsp; Python 3.9+ &nbsp;·&nbsp; MIT
</p>

Leave him running. Talk to Cursor as usual. Thoughts, tools, questions, and your prompts show up in the pane to the right of the dino. Claude Code and Codex mappers exist; Cursor is the one that's wired today.

Works in Terminal.app, iTerm, Windows Terminal, and VS Code. Install Python 3.9+ once. After that, `rex` works from any folder — it does not need to share a directory with the agent.

## Install

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.sh | bash
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.ps1 | iex
```

That creates a private environment under `~/.rex`, puts `rex` on your PATH, and wires Cursor so this machine's agent chat is watched. Open a new terminal, then `rex start` from anywhere.

Already have [pipx](https://pipx.pypa.io/)?

```bash
pipx install git+https://github.com/luciancheng/rex.git
rex ingest --install-cursor-hook
rex start
```

## Usage

Ctrl+C twice to quit.

```bash
rex start                         # live view
rex start --static                # print one frame and exit
rex start --color blue            # try another palette
rex ingest --install-cursor-hook  # reinstall the Cursor watcher
rex --help
```

`NO_COLOR=1` disables ANSI color. Pipes and CI print a static frame instead of opening the live view.

Idle and future poses should be 40×40.

## Hack on it

```bash
git clone https://github.com/luciancheng/rex.git
cd rex
python3 -m venv .venv
source .venv/bin/activate
pip install -e ".[dev]"
rex start
```

On Windows PowerShell, activate with `.venv\Scripts\Activate.ps1`. The `dev` extra is `pytest` and `pytest-cov`.

## Edit the idle sprite

Idle Rex is the 40×40 PNG at `assets/idle.png`, converted pixel-for-pixel into terminal art. `assets/rex.png` is the same drawing, scaled up for this README.

Rebuild from a PNG (reads every pixel and the palette):

```bash
python tools/import_sprite.py assets/idle.png
python tools/import_sprite.py assets/idle.png --apply
```

`--apply` writes the grid into `src/rex/mascot/sprites.py` and the sampled RGB values into the `yellow` palette. Restart `rex start` after that.

You can also edit the data by hand:

- **Pixels / size** — `src/rex/mascot/sprites.py`, `IDLE_ROWS`. Each character is one pixel. Add or remove characters and lines to change size.
- **Colors** — `src/rex/mascot/palettes.py`, the `yellow` RGB triples.

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

The registry lives in `src/rex/mascot/catalog.py`. `rex start` plays the `idle` animation (blink and glance) inside the 40×40 stage.

```python
from rex.mascot.catalog import Animation, Sprite, register_animation, register_sprite
from rex.mascot.sprites import pixels

BLINK = register_sprite(Sprite("idle_blink", pixels("...")))
register_animation(
    Animation("idle", frames=(("idle", 20), ("idle_blink", 3))),
)
```

`hold` is in live-viewer ticks. A one-frame animation is a still.

## Layout

```
assets/idle.png             source 40×40 sprite
assets/rex.png              README art (same drawing, scaled)
tools/import_sprite.py      PNG → CLI pixel art
src/rex/cli.py              commands (rex start)
src/rex/mascot/sprites.py   idle pixel grid
src/rex/mascot/palettes.py  RGB colors
src/rex/mascot/png.py       PNG decoder used by the importer
src/rex/mascot/catalog.py   sprite + animation registry
src/rex/mascot/render.py    pixels → terminal cells
src/rex/tui/                live viewer
src/rex/watch/              agent event feed (hooks → JSONL → pane)
scripts/install.sh          user install (macOS/Linux)
scripts/install.ps1         user install (Windows)
tests/                      pytest suite
.github/workflows/ci.yml    GitHub Actions
```

## Tests

```bash
pip install -e ".[dev]"
pytest
```

With coverage:

```bash
pytest --cov --cov-report=term-missing
```

CI runs the same suite on Ubuntu, macOS, and Windows across Python 3.9, 3.11, and 3.13.

## Contributing

1. Create a virtualenv and `pip install -e ".[dev]"`.
2. Keep mascot rendering in `src/rex/mascot/` and commands in `src/rex/cli.py`.
3. Add tests under `tests/` for new behavior.
4. Run `pytest` before opening a pull request.

## License

MIT. See [LICENSE](LICENSE).
