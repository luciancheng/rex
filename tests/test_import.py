from pathlib import Path

from rex.mascot.png import load_png
from rex.mascot.palettes import PALETTES
from rex.mascot.sprites import IDLE_ROWS, PIXEL_ROLES

ASSET = Path(__file__).resolve().parents[1] / "assets" / "idle.png"


def test_idle_png_is_40x40() -> None:
    image = load_png(str(ASSET))
    assert image.width == 40
    assert image.height == 40


def test_idle_sprite_matches_png_pixels() -> None:
    """Every CLI pixel uses the same RGB as assets/idle.png (white = transparent)."""
    image = load_png(str(ASSET))
    palette = PALETTES["yellow"]
    assert len(IDLE_ROWS) == image.height
    assert len(IDLE_ROWS[0]) == image.width
    for y, row in enumerate(IDLE_ROWS):
        for x, ch in enumerate(row):
            r, g, b, a = image.pixels[y][x]
            if ch == ".":
                assert (r, g, b) == (255, 255, 255) or a < 16
                continue
            role = PIXEL_ROLES[ch]
            assert palette[role] == (r, g, b), f"{x},{y} {ch} {palette[role]} != {(r, g, b)}"


def test_palettes_define_every_role() -> None:
    for name, palette in PALETTES.items():
        for role in PIXEL_ROLES.values():
            assert role in palette, f"{name} missing {role}"
