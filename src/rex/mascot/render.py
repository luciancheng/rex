"""Turn mascot pixel maps into terminal output."""

from __future__ import annotations

import os
import sys
from collections.abc import Mapping, Sequence
from typing import TextIO

from rex.mascot.catalog import Sprite
from rex.mascot.palettes import PALETTES, Palette
from rex.mascot.sprites import PIXEL_ROLES, sprite_size

RESET = "\033[0m"


def color_enabled(stream: TextIO | None = None, force: bool | None = None) -> bool:
    if force is not None:
        return force
    if os.environ.get("NO_COLOR"):
        return False
    if os.environ.get("FORCE_COLOR"):
        return True
    stream = stream or sys.stdout
    return bool(getattr(stream, "isatty", lambda: False)())


def fg(rgb: tuple[int, int, int]) -> str:
    r, g, b = rgb
    return f"\033[38;2;{r};{g};{b}m"


def bg(rgb: tuple[int, int, int]) -> str:
    r, g, b = rgb
    return f"\033[48;2;{r};{g};{b}m"


def paint(text: str, rgb: tuple[int, int, int], enabled: bool) -> str:
    if not enabled:
        return text
    return f"{fg(rgb)}{text}{RESET}"


def palette_for(name: str) -> Palette:
    try:
        return PALETTES[name]
    except KeyError as exc:
        known = ", ".join(PALETTES)
        raise ValueError(f"unknown color '{name}'. choose from: {known}") from exc


def _pixel_rgb(ch: str, palette: Palette) -> tuple[int, int, int] | None:
    if ch in {".", " "}:
        return None
    try:
        role = PIXEL_ROLES[ch]
    except KeyError as exc:
        raise ValueError(f"unknown pixel '{ch}'") from exc
    return palette[role]


def _pair(upper: str, lower: str, palette: Palette, enabled: bool) -> str:
    up = _pixel_rgb(upper, palette)
    lo = _pixel_rgb(lower, palette)
    if up is None and lo is None:
        return " "
    if not enabled:
        if up is not None and lo is not None:
            return "█"
        if up is not None:
            return "▀"
        return "▄"
    if up is not None and lo is not None:
        if up == lo:
            return f"{fg(up)}{bg(up)}▀{RESET}"
        return f"{fg(up)}{bg(lo)}▀{RESET}"
    if up is not None:
        return f"{fg(up)}▀{RESET}"
    assert lo is not None
    return f"{fg(lo)}▄{RESET}"


def render_rows(
    rows: Sequence[str],
    palette: Mapping[str, tuple[int, int, int]],
    enabled: bool = True,
) -> list[str]:
    width, height = sprite_size(rows)
    if height % 2:
        padded = list(rows) + ["." * width]
    else:
        padded = list(rows)
    pal = dict(palette)
    lines: list[str] = []
    for y in range(0, len(padded), 2):
        parts = [
            _pair(padded[y][x], padded[y + 1][x], pal, enabled) for x in range(width)
        ]
        lines.append("".join(parts))
    return lines


def render_sprite(
    sprite: Sprite | Sequence[str],
    color: str = "yellow",
    enabled: bool = True,
) -> str:
    rows = sprite.rows if isinstance(sprite, Sprite) else sprite
    return "\n".join(render_rows(rows, palette_for(color), enabled))
