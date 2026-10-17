"""Interactive mascot views."""

from __future__ import annotations

import sys
from typing import Sequence, TextIO

from rex.mascot import (
    DEFAULT_ANIMATION,
    DEFAULT_PALETTE,
    UI,
    get_animation,
    get_sprite,
    paint,
    palette_for,
    render_rows,
)
from rex.mascot.sprites import STAGE_SIZE, fit_stage
from rex.tui.terminal import (
    CLEAR,
    HIDE_CURSOR,
    HOME,
    SHOW_CURSOR,
    live_screen,
    read_key,
    size,
)

FRAME_TICK = 0.08
QUIT_WINDOW_TICKS = 25  # ~2s at FRAME_TICK
GUTTER = 2
MIN_TEXT_COLS = 24
INPUT_ROWS = 3
FOOTER_ROWS = 1
INPUT_PREFIX = " > "
CTRL_C = "\x03"


def _clip(text: str, width: int) -> str:
    if width <= 0:
        return ""
    if len(text) <= width:
        return text + " " * (width - len(text))
    return text[:width]


def _wrap(text: str, width: int) -> list[str]:
    if width <= 0:
        return []
    words = text.split()
    if not words:
        return [""]
    lines: list[str] = []
    current = words[0]
    for word in words[1:]:
        candidate = f"{current} {word}"
        if len(candidate) <= width:
            current = candidate
            continue
        lines.append(current)
        current = word
    lines.append(current)
    wrapped: list[str] = []
    for line in lines:
        if len(line) <= width:
            wrapped.append(line)
        else:
            wrapped.extend(line[i : i + width] for i in range(0, len(line), width))
    return wrapped


def _header(width: int, meta: str, enabled: bool) -> str:
    left = "REX"
    if width < len(left):
        return paint(_clip(left, width), UI["title"], enabled)
    gap = width - len(left) - len(meta)
    if gap < 1:
        return paint(_clip(left, width), UI["title"], enabled)
    return (
        paint(left, UI["title"], enabled)
        + " " * gap
        + paint(meta, UI["muted"], enabled)
    )


def _input_box(draft: str, width: int, enabled: bool) -> list[str]:
    inner = max(0, width - 2)
    room = max(0, inner - len(INPUT_PREFIX))
    visible = draft[-room:] if room else ""
    content = _clip(INPUT_PREFIX + visible, inner)
    top = "╭" + "─" * inner + "╮"
    bot = "╰" + "─" * inner + "╯"
    if width < 4:
        return [_clip("", width) for _ in range(INPUT_ROWS)]
    return [
        paint(top, UI["border"], enabled),
        paint("│", UI["border"], enabled)
        + paint(content, UI["text"], enabled)
        + paint("│", UI["border"], enabled),
        paint(bot, UI["border"], enabled),
    ]


def _text_column(
    *,
    width: int,
    height: int,
    meta: str,
    log: Sequence[str],
    draft: str,
    quit_pending: bool,
    enabled: bool,
) -> list[str]:
    if width <= 0 or height <= 0:
        return [""] * max(0, height)

    footer_text = (
        "press ctrl+c again to exit" if quit_pending else "ctrl+c twice to exit"
    )
    footer_color = UI["error"] if quit_pending else UI["muted"]
    show_box = height >= INPUT_ROWS + FOOTER_ROWS + 3
    body_top = 2
    body_bottom = height - FOOTER_ROWS
    if show_box:
        body_bottom -= INPUT_ROWS

    rows = [""] * height
    rows[0] = _header(width, meta, enabled)
    if height > 1:
        rows[1] = " " * width

    body: list[str] = []
    if log:
        for entry in log:
            body.extend(_wrap(entry, width))
    else:
        body.extend(_wrap("Ready.", width))
    body_h = max(0, body_bottom - body_top)
    visible_body = body[-body_h:] if body_h else []
    for offset, line in enumerate(visible_body):
        rows[body_top + offset] = paint(_clip(line, width), UI["text"], enabled)
    for index in range(body_top, body_bottom):
        if not rows[index]:
            rows[index] = " " * width

    if show_box:
        box = _input_box(draft, width, enabled)
        box_start = height - FOOTER_ROWS - INPUT_ROWS
        for offset, line in enumerate(box):
            rows[box_start + offset] = line

    rows[-1] = paint(_clip(footer_text, width), footer_color, enabled)
    return rows


def compose_frame(
    sprite_name: str,
    color: str,
    *,
    enabled: bool,
    columns: int,
    lines: int,
    animation_name: str,
    draft: str = "",
    log: Sequence[str] = (),
    quit_pending: bool = False,
) -> str:
    columns = max(1, columns)
    lines = max(1, lines)
    mascot_cols = min(STAGE_SIZE, columns)
    text_width = columns - mascot_cols - GUTTER
    if text_width < MIN_TEXT_COLS:
        text_width = 0
        mascot_cols = min(STAGE_SIZE, columns)

    sprite = get_sprite(sprite_name)
    staged = fit_stage(sprite.rows)
    if mascot_cols < STAGE_SIZE:
        staged = tuple(row[:mascot_cols] for row in staged)
    mascot = render_rows(staged, palette_for(color), enabled)
    blank_mascot = " " * mascot_cols
    text = _text_column(
        width=text_width,
        height=lines,
        meta=animation_name,
        log=log,
        draft=draft,
        quit_pending=quit_pending,
        enabled=enabled,
    )

    out: list[str] = []
    for index in range(lines):
        left = mascot[index] if index < len(mascot) else blank_mascot
        if text_width <= 0:
            out.append(left)
            continue
        out.append(left + (" " * GUTTER) + text[index])

    # CR+LF so each row starts at column 0 even if output processing is off.
    frame = HOME + CLEAR + "\r\n".join(out)
    if text_width >= 4 and lines >= INPUT_ROWS + FOOTER_ROWS + 3:
        inner = text_width - 2
        room = max(0, inner - len(INPUT_PREFIX))
        visible = draft[-room:] if room else ""
        cursor_row = lines - 2
        cursor_col = mascot_cols + GUTTER + 1 + 1 + len(INPUT_PREFIX) + len(visible)
        frame += f"\033[{cursor_row};{cursor_col}H{SHOW_CURSOR}"
    else:
        frame += HIDE_CURSOR
    return frame


def print_card(
    sprite_name: str,
    color: str,
    *,
    enabled: bool,
    stdout: TextIO = sys.stdout,
) -> None:
    sprite = get_sprite(sprite_name)
    rows = render_rows(sprite.rows, palette_for(color), enabled)
    title = paint("REX", UI["title"], enabled)
    tagline = paint("Small dino. Big opinions.", UI["muted"], enabled)
    stdout.write("\n")
    stdout.write(f"  {title}   {tagline}\n\n")
    for row in rows:
        stdout.write(f"  {row}\n")
    stdout.write(f"\n  {paint(sprite.name, UI['muted'], enabled)}\n\n")
    stdout.flush()


def _advance(
    frames: tuple[tuple[str, int], ...], index: int, hold: int
) -> tuple[int, int]:
    hold -= 1
    if hold > 0:
        return index, hold
    index = (index + 1) % len(frames)
    return index, frames[index][1]


def run_loop(
    *,
    color: str = DEFAULT_PALETTE,
    enabled: bool,
    animation: str = DEFAULT_ANIMATION,
    stdin: TextIO = sys.stdin,
    stdout: TextIO = sys.stdout,
) -> int:
    sequence = get_animation(animation)
    frame_index = 0
    hold = sequence.frames[0][1]
    sprite_name = sequence.frames[0][0]
    columns, lines = size()
    animated = len(sequence.frames) > 1
    draft = ""
    log: list[str] = []
    quit_pending = False
    quit_ticks = 0

    def draw() -> None:
        stdout.write(
            compose_frame(
                sprite_name,
                color,
                enabled=enabled,
                columns=columns,
                lines=lines,
                animation_name=sequence.name,
                draft=draft,
                log=log,
                quit_pending=quit_pending,
            )
        )
        stdout.flush()

    with live_screen(stdout):
        draw()
        while True:
            key = read_key(stdin, FRAME_TICK)
            changed = False
            if key == CTRL_C:
                if quit_pending:
                    return 0
                quit_pending = True
                quit_ticks = QUIT_WINDOW_TICKS
                changed = True
            elif key is None:
                if quit_pending:
                    quit_ticks -= 1
                    if quit_ticks <= 0:
                        quit_pending = False
                        changed = True
                if animated:
                    frame_index, hold = _advance(sequence.frames, frame_index, hold)
                    next_name = sequence.frames[frame_index][0]
                    if next_name != sprite_name:
                        sprite_name = next_name
                        changed = True
            elif key in {"\r", "\n"}:
                quit_pending = False
                if draft.strip():
                    log.append(draft.strip())
                    draft = ""
                    changed = True
            elif key in {"\x7f", "\x08"}:
                quit_pending = False
                if draft:
                    draft = draft[:-1]
                    changed = True
            elif len(key) == 1 and key.isprintable():
                quit_pending = False
                if len(draft) < 256:
                    draft += key
                    changed = True
            else:
                if quit_pending:
                    quit_pending = False
                    changed = True
            new_columns, new_lines = size()
            if (new_columns, new_lines) != (columns, lines):
                columns, lines = new_columns, new_lines
                changed = True
            if changed:
                draw()
