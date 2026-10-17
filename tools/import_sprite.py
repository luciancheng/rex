#!/usr/bin/env python3
"""Analyze a PNG sprite and turn it into REX CLI pixel art.

Reads every pixel, lists the unique palette, maps each color to a letter,
and can print or write the idle sprite.

Examples:

    python tools/import_sprite.py assets/idle.png
    python tools/import_sprite.py assets/idle.png --apply
"""

from __future__ import annotations

import argparse
import sys
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SRC = ROOT / "src"
if str(SRC) not in sys.path:
    sys.path.insert(0, str(SRC))

from rex.mascot.png import load_png  # noqa: E402

# Prefer readable letters when a color looks like one of these roles.
ROLE_LETTERS = (
    ("outline", "k"),
    ("dark", "d"),
    ("mid", "n"),
    ("accent", "a"),
    ("body", "y"),
    ("highlight", "Y"),
    ("shade", "o"),
    ("blush", "p"),
    ("belly", "b"),
    ("shadow", "s"),
    ("eye", "e"),
    ("white", "w"),
    ("mark", "m"),
    ("green", "g"),
    ("red", "r"),
    ("teal", "t"),
)


def _is_transparent(pixel: tuple[int, int, int, int], background: tuple[int, int, int] | None) -> bool:
    r, g, b, a = pixel
    if a < 16:
        return True
    if background is not None and (r, g, b) == background:
        return True
    return False


def _guess_background(pixels: tuple, *, use_white: bool) -> tuple[int, int, int] | None:
    opaque = Counter(
        (r, g, b) for row in pixels for r, g, b, a in row if a >= 16
    )
    if not opaque:
        return None
    most_common, _ = opaque.most_common(1)[0]
    if use_white and most_common == (255, 255, 255):
        return most_common
    if use_white:
        return (255, 255, 255) if (255, 255, 255) in opaque else most_common
    return most_common


def _luminance(rgb: tuple[int, int, int]) -> float:
    r, g, b = rgb
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def _assign_letters(
    counts: Counter[tuple[int, int, int]],
) -> dict[tuple[int, int, int], str]:
    """Map each unique RGB to a stable letter. Visuals stay exact regardless of names."""
    remaining = set(counts)
    assigned: dict[tuple[int, int, int], str] = {}

    def take(role: str, color: tuple[int, int, int] | None) -> None:
        if color is None or color not in remaining:
            return
        assigned[color] = dict(ROLE_LETTERS)[role]
        remaining.remove(color)

    take("outline", min(remaining, key=_luminance) if remaining else None)
    bluish = [c for c in remaining if c[2] >= c[0] and c[2] >= c[1]]
    take("shadow", min(bluish, key=_luminance) if bluish else None)
    take("body", max(remaining, key=lambda c: counts[c]) if remaining else None)
    take("highlight", max(remaining, key=_luminance) if remaining else None)
    pink = [c for c in remaining if c[0] > 200 and c[2] > 55 and c[1] < 160]
    take("blush", max(pink, key=lambda c: c[2]) if pink else None)
    # Darkest leftover oranges through lightest: dark, shade, mid, accent, belly.
    leftovers = sorted(remaining, key=_luminance)
    roles_left = ["dark", "shade", "mid", "accent", "belly", "eye", "white", "mark", "green", "red", "teal"]
    for color, role in zip(leftovers, roles_left):
        take(role, color)
    if remaining:
        raise ValueError("too many unique colors for the available pixel letters")
    return assigned


def analyze(path: Path, *, white_is_transparent: bool) -> tuple[list[str], dict[str, tuple[int, int, int]], list[tuple[str, tuple[int, int, int], int]]]:
    image = load_png(str(path))
    background = _guess_background(image.pixels, use_white=white_is_transparent)
    counts: Counter[tuple[int, int, int]] = Counter()
    for row in image.pixels:
        for pixel in row:
            if _is_transparent(pixel, background):
                continue
            counts[pixel[:3]] += 1

    color_to_letter = _assign_letters(counts)
    letter_to_rgb = {letter: rgb for rgb, letter in color_to_letter.items()}
    summary = [
        (color_to_letter[rgb], rgb, count) for rgb, count in counts.most_common()
    ]

    rows: list[str] = []
    for row in image.pixels:
        chars: list[str] = []
        for pixel in row:
            if _is_transparent(pixel, background):
                chars.append(".")
            else:
                chars.append(color_to_letter[pixel[:3]])
        rows.append("".join(chars))
    return rows, letter_to_rgb, summary


def _format_rows(rows: list[str]) -> str:
    lines = ["IDLE_ROWS = pixels("]
    for row in rows:
        lines.append(f'    "{row}",')
    lines.append(")")
    return "\n".join(lines)


def _replace_block(text: str, begin: str, end: str, replacement: str) -> str:
    start = text.index(begin)
    stop = text.index(end, start)
    return text[:start] + replacement + text[stop + len(end) :]


def apply_idle(rows: list[str], letter_to_rgb: dict[str, tuple[int, int, int]]) -> None:
    sprites_path = SRC / "rex" / "mascot" / "sprites.py"
    palettes_path = SRC / "rex" / "mascot" / "palettes.py"
    sprites = sprites_path.read_text()
    sprites = _replace_block(sprites, "IDLE_ROWS = pixels(", ")\n", _format_rows(rows) + "\n")
    sprites_path.write_text(sprites)

    palettes = palettes_path.read_text()
    yellow_start = palettes.index('"yellow": {')
    yellow_open = palettes.index("{", yellow_start)
    depth = 0
    yellow_end = None
    for i, ch in enumerate(palettes[yellow_open:], yellow_open):
        if ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                yellow_end = i
                break
    if yellow_end is None:
        raise RuntimeError("could not find yellow palette")
    existing_block = palettes[yellow_open : yellow_end + 1]
    # Keep keys the palette already has; overwrite those we sampled.
    updated = existing_block
    role_for_letter = {letter: role for role, letter in ROLE_LETTERS}
    for letter, rgb in letter_to_rgb.items():
        role = role_for_letter[letter]
        needle_start = f'"{role}": ('
        if needle_start not in updated:
            # insert before closing brace
            updated = updated[:-1] + f'        "{role}": {rgb},\n    ' + "}"
            continue
        key_at = updated.index(needle_start)
        value_end = updated.index(")", key_at)
        updated = updated[:key_at] + f'"{role}": {rgb}' + updated[value_end + 1 :]
    palettes = palettes[:yellow_open] + updated + palettes[yellow_end + 1 :]
    palettes_path.write_text(palettes)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Convert a PNG sprite into REX CLI art.")
    parser.add_argument("png", type=Path, help="path to a PNG sprite, ideally 40x40 pixel art")
    parser.add_argument(
        "--keep-background",
        action="store_true",
        help="do not treat white / the most common color as transparent",
    )
    parser.add_argument(
        "--apply",
        action="store_true",
        help="write the result into the idle sprite and yellow palette",
    )
    args = parser.parse_args(argv)

    rows, letter_to_rgb, summary = analyze(
        args.png,
        white_is_transparent=not args.keep_background,
    )
    image = load_png(str(args.png))
    print(f"{args.png}  {image.width}x{image.height}")
    print(f"opaque colors: {len(summary)}")
    print()
    print("letter   count   rgb")
    for letter, rgb, count in summary:
        print(f"  {letter}      {count:4d}   {rgb}")
    print()
    print("grid:")
    for row in rows:
        print(row)
    print()
    print(_format_rows(rows))
    if args.apply:
        apply_idle(rows, letter_to_rgb)
        print(f"\nwrote idle sprite to {SRC / 'rex' / 'mascot' / 'sprites.py'}")
        print(f"wrote yellow palette to {SRC / 'rex' / 'mascot' / 'palettes.py'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
