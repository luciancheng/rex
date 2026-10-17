# REX

Small dino. Big opinions.

REX is a terminal companion with a pixel-art dinosaur mascot. The product direction is still evolving; this repo is the CLI foundation, visual system, and mascot renderer.

```
rex start
```

## Requirements

- Python 3.9 or newer
- macOS or Linux
- A terminal that supports UTF-8 (true color is used when available)

No extra runtime packages are required. REX uses the Python standard library only.

## Install

Clone the repo, create a virtualenv, and install in editable mode:

```bash
git clone https://github.com/luciancheng/rex.git
cd rex
python3 -m venv .venv
source .venv/bin/activate
pip install -e .
rex start
```

On Windows PowerShell, activate with `.venv\Scripts\Activate.ps1`.

### Install with development tools

If you are going to run tests or work on the code:

```bash
pip install -e ".[dev]"
```

That extra installs:

| Extra | Packages | What they are for |
| --- | --- | --- |
| `dev` | `pytest`, `pytest-cov` | unit tests and coverage |

### One-off run without installing the `rex` command

```bash
PYTHONPATH=src python3 -m rex start --static --no-color
```

## Usage

```bash
rex start              # live view; ctrl+c twice to quit
rex start --static     # print one frame and exit
rex start --color blue # try another palette
rex --help
```

The live view pins Rex in a 40×40 pixel slot on the left. Everything to the right is a text pane with a prompt. Type and press enter to pin a line there. The idle drawing is still 32×32; extra space is for later poses.

`NO_COLOR=1` disables ANSI color. Pipes and CI print a static frame instead of opening the live view.

## Edit the idle sprite

Idle Rex is the 32×32 PNG at `assets/idle.png`, converted pixel-for-pixel into terminal art.

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
assets/idle.png             source 32x32 sprite
tools/import_sprite.py      PNG → CLI pixel art
src/rex/cli.py              commands (rex start)
src/rex/mascot/sprites.py   idle pixel grid
src/rex/mascot/palettes.py  RGB colors
src/rex/mascot/png.py       PNG decoder used by the importer
src/rex/mascot/catalog.py   sprite + animation registry
src/rex/mascot/render.py    pixels → terminal cells
src/rex/tui/                live viewer
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

CI runs the same suite on Ubuntu and macOS across Python 3.9, 3.11, and 3.13.

## Contributing

1. Create a virtualenv and `pip install -e ".[dev]"`.
2. Keep mascot rendering in `src/rex/mascot/` and commands in `src/rex/cli.py`.
3. Add tests under `tests/` for new behavior.
4. Run `pytest` before opening a pull request.

## License

MIT. See [LICENSE](LICENSE).
