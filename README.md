# REX

Small dino. Big opinions.

REX is a terminal companion with a pixel-art dinosaur mascot. Leave it running next to Cursor (or later Claude Code / Codex) and it shows what the agent is thinking and which commands it wants to run.

```
rex start
```

Works on macOS, Linux, and Windows 10+ in a modern terminal (Terminal.app, iTerm, Windows Terminal, VS Code). You need Python 3.9 or newer once, for the installer. After that, type `rex` from any folder.

## Install (just use it)

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.sh | bash
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.ps1 | iex
```

That creates a private environment under `~/.rex`, puts `rex` on your PATH, and wires Cursor so this machine's agent chat is watched. Open a new terminal, then `rex start` from anywhere.

If you already have [pipx](https://pipx.pypa.io/):

```bash
pipx install git+https://github.com/luciancheng/rex.git
rex ingest --install-cursor-hook
rex start
```

## Usage

Keep `rex start` open, then talk to the Cursor agent as usual. Thoughts, tool calls, and questions show up in the pane to the right of the dino. Ctrl+C twice to quit.

```bash
rex start                         # live view
rex start --static                # print one frame and exit
rex start --color blue            # try another palette
rex ingest --install-cursor-hook  # reinstall the Cursor watcher
rex --help
```

`NO_COLOR=1` disables ANSI color. Pipes and CI print a static frame instead of opening the live view.

Idle and future poses should be 40×40.

## Install (hack on the code)

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

Idle Rex is the 40×40 PNG at `assets/idle.png`, converted pixel-for-pixel into terminal art.

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

## Project layout

```
assets/idle.png             source 40x40 sprite
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
