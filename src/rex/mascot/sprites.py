"""Pixel art for REX.

How to edit the idle dino
-------------------------
1. Drawing: change the ``IDLE_ROWS`` grid below. Each character is one pixel.
2. Size: add or remove characters and lines. The canvas is whatever you type.
3. Colors: map letters to RGB in ``src/rex/mascot/palettes.py``.

To rebuild idle from a PNG:

    python tools/import_sprite.py assets/idle.png --apply

Pixel letters (spaces become transparent):

    .  transparent    k  outline      y  body         Y  highlight
    o  shade          d  dark         n  mid          a  accent
    b  belly          p  blush        s  shadow       e  eye
    w  white          m  marks        g  green        r  red
    t  teal

Live view stage
---------------
``rex start`` draws the mascot in a 40×40 pixel slot on the left of the
terminal. Idle and future poses should be 40×40. Smaller drawings are
padded; larger ones clip from the bottom-right. The column to the right is text.

How to add more later
---------------------
    EXTRA = register_sprite(Sprite("wave", pixels("...")))
    register_animation(Animation("idle", frames=(("idle", 20), ("wave", 3))))
"""

from __future__ import annotations

from typing import Iterable

from rex.mascot.catalog import Animation, Sprite, register_animation, register_sprite

PIXEL_ROLES = {
    "k": "outline",
    "y": "body",
    "Y": "highlight",
    "o": "shade",
    "d": "dark",
    "n": "mid",
    "a": "accent",
    "b": "belly",
    "e": "eye",
    "w": "white",
    "p": "blush",
    "s": "shadow",
    "m": "mark",
    "g": "green",
    "r": "red",
    "t": "teal",
}


def pixels(*rows: str) -> tuple[str, ...]:
    """Build a sprite grid. Spaces are transparent. Rows are padded to match."""
    cleaned = [row.replace(" ", ".") for row in rows]
    width = max((len(row) for row in cleaned), default=0)
    return tuple(row.ljust(width, ".") for row in cleaned)


STAGE_SIZE = 40


def fit_stage(
    rows: tuple[str, ...],
    width: int = STAGE_SIZE,
    height: int = STAGE_SIZE,
) -> tuple[str, ...]:
    """Pad or clip a sprite into the live-view stage. Keeps the top-left."""
    fitted: list[str] = []
    for y in range(max(0, height)):
        if y < len(rows):
            row = rows[y][:width] if width >= 0 else ""
            fitted.append(row + "." * (width - len(row)))
        else:
            fitted.append("." * width)
    return tuple(fitted)


def sprite_size(rows: Iterable[str]) -> tuple[int, int]:
    materialised = list(rows)
    if not materialised:
        return 0, 0
    width = len(materialised[0])
    if any(len(row) != width for row in materialised):
        raise ValueError("sprite rows must be the same width")
    return width, len(materialised)


def _validate_pixels(name: str, rows: tuple[str, ...]) -> None:
    for y, row in enumerate(rows):
        for x, ch in enumerate(row):
            if ch != "." and ch not in PIXEL_ROLES:
                raise ValueError(f"{name}[{y},{x}] has unknown pixel {ch!r}")


def paint_pixels(base: tuple[str, ...], updates: dict[tuple[int, int], str]) -> tuple[str, ...]:
    """Copy a sprite and set ``(x, y) -> character`` pixels."""
    grid = [list(row) for row in base]
    for (x, y), ch in updates.items():
        grid[y][x] = ch
    return tuple("".join(row) for row in grid)


def shift_region(
    base: tuple[str, ...],
    *,
    xs: range,
    ys: range,
    dx: int = 0,
    dy: int = 0,
) -> tuple[str, ...]:
    """Move a rectangle of pixels. Empty cells do not overwrite the destination."""
    grid = [list(row) for row in base]
    chunk = [[grid[y][x] for x in xs] for y in ys]
    for y in ys:
        for x in xs:
            grid[y][x] = "."
    height = len(grid)
    width = len(grid[0]) if grid else 0
    for row_i, y in enumerate(ys):
        ny = y + dy
        if ny < 0 or ny >= height:
            continue
        for col_i, x in enumerate(xs):
            nx = x + dx
            if nx < 0 or nx >= width:
                continue
            ch = chunk[row_i][col_i]
            if ch != ".":
                grid[ny][nx] = ch
    return tuple("".join(row) for row in grid)


def nudge(
    base: tuple[str, ...],
    dx: int = 0,
    dy: int = 0,
    *,
    keep_bottom: int = 2,
) -> tuple[str, ...]:
    """Shift the body a few pixels; keep the ground shadow planted."""
    if (dx == 0 and dy == 0) or keep_bottom >= len(base):
        return base
    body = list(base[:-keep_bottom])
    shadow = list(base[-keep_bottom:])
    empty = "." * len(base[0])
    if dy > 0:
        body = [empty] * dy + body[:-dy]
    elif dy < 0:
        body = body[-dy:] + [empty] * (-dy)
    if dx > 0:
        body = ["." * dx + row[:-dx] for row in body]
    elif dx < 0:
        body = [row[-dx:] + "." * (-dx) for row in body]
    return tuple(body + shadow)


def shift_body(base: tuple[str, ...], dy: int, *, keep_bottom: int = 2) -> tuple[str, ...]:
    """Nudge the body vertically; keep the ground shadow planted."""
    return nudge(base, dy=dy, keep_bottom=keep_bottom)


# Imported 1:1 from assets/idle.png (40x40, 10 opaque colors).
IDLE_ROWS = pixels(
    "........................................",
    "........................................",
    "........................................",
    "........................................",
    "........................................",
    ".....................kkk................",
    "....................kdnnk...............",
    ".................kkkdkkkkkkkkk..........",
    "................knndkayyyyyyyykk........",
    "...............knndkayyyyyyyyyYyk.......",
    "...............kndkayyyyyyyyyyyYyk......",
    "...............kkkayyyyyyyyyyyyyyk......",
    "..............kndkayyyyyyyyyyyyyyyk.....",
    ".............kndkoayyyyyykyyyyyyyyk.....",
    ".............kndkoayyyyyykyyyyyyyyk.....",
    "..............kdkoayyyyppyyyyyyyyyk.....",
    "..............kkkoayyyyyyyyyyyyyyk......",
    ".............kndkoayyyyyyyyyyyyyyk......",
    "............kndkoobayyyyyyyyyyyyk.......",
    "............kndkooobayyyyaakkkkk........",
    "............kndkoooobaaaabbbk...........",
    "............kkkooooobbbbbbyybk..........",
    "...........kndkoobbbbbbbbyyyyk..........",
    "..........kndkkoobbbbbbbyyyyyk..........",
    ".........kkndkooobbbbbbbbbyyyk..........",
    ".....kk.knkdkooooooooooooobbk...........",
    "....kyyknddkooooobbbbkooooobk...........",
    "....koyykkkooooobbbbbbkoooobk...........",
    ".....koyyyooooobbbbbbbkooook............",
    ".....kdoooooooobbbbbbbkkkkkk............",
    "......kdddoooookbbbbbbkbbbbk............",
    ".......kkkdddkkkkbbbbddkbbddk...........",
    "..........kkkkkskddddddkddddk...........",
    "........ss.ssssskkkkkkkskkkkkss.ss......",
    ".............sssssssssssssssss..........",
    "........................................",
    "........................................",
    "........................................",
    "........................................",
    "........................................",
)

_validate_pixels("idle", IDLE_ROWS)
IDLE = register_sprite(Sprite("idle", IDLE_ROWS))

# Eye is a 2px-tall bar at (25, 13-14). A blink just drops the top pixel.
IDLE_BLINK_ROWS = paint_pixels(IDLE_ROWS, {(25, 13): "y"})
# Glance toward the snout.
IDLE_LOOK_ROWS = paint_pixels(
    IDLE_ROWS,
    {(25, 13): "y", (25, 14): "y", (26, 13): "k", (26, 14): "k"},
)
# Soft squat. Shadow rows 33-39 stay planted.
IDLE_BREATHE_ROWS = shift_body(IDLE_ROWS, dy=1, keep_bottom=7)

for name, rows in (
    ("idle_blink", IDLE_BLINK_ROWS),
    ("idle_look", IDLE_LOOK_ROWS),
    ("idle_breathe", IDLE_BREATHE_ROWS),
):
    _validate_pixels(name, rows)
    register_sprite(Sprite(name, rows))

# Tick length is 0.08s. Keep motion slow so it reads as idle, not a cartoon.
register_animation(
    Animation(
        "idle",
        frames=(
            ("idle", 24),
            ("idle_breathe", 12),
            ("idle", 16),
            ("idle_blink", 3),
            ("idle", 18),
            ("idle_look", 14),
            ("idle", 12),
            ("idle_breathe", 10),
            ("idle", 16),
            ("idle_blink", 3),
            ("idle", 14),
            ("idle_look", 12),
        ),
    )
)
