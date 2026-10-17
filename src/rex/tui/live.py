"""Interactive mascot views."""

from __future__ import annotations

import sys
from pathlib import Path
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
from rex.watch.bus import EventTail, default_event_path
from rex.watch.events import RexEvent
from rex.watch.format import PROSE_KINDS, is_duplicate, markdown_lines

FRAME_TICK = 0.08
QUIT_WINDOW_TICKS = 25  # ~2s at FRAME_TICK
GUTTER = 2
MIN_TEXT_COLS = 24
INPUT_ROWS = 3
FOOTER_ROWS = 1
INPUT_PREFIX = " > "
CTRL_C = "\x03"
MAX_LOG = 120
KIND_COLOR = {
    "thought": "muted",
    "command": "accent",
    "question": "error",
    "text": "text",
    "user": "you",
    "result": "muted",
    "error": "error",
    "idle": "muted",
}
KIND_STATUS = {
    "thought": "thinking",
    "command": "running",
    "question": "ask",
    "text": "idle",
    "user": "idle",
    "result": "idle",
    "error": "error",
    "idle": "idle",
}
KIND_LABEL = {
    "thought": "thinking",
    "command": "run",
    "question": "ask",
    "text": "reply",
    "user": "you",
    "result": "done",
    "error": "error",
    "idle": "idle",
}


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


def _as_log_entry(entry: object) -> tuple[str, str]:
    if isinstance(entry, RexEvent):
        if entry.kind in PROSE_KINDS:
            return entry.kind, entry.text
        return entry.kind, entry.display_text()
    if isinstance(entry, tuple) and len(entry) == 2:
        return str(entry[0]), str(entry[1])
    return "text", str(entry)


def _rule(label: str, width: int) -> str:
    prefix = f"── {label} "
    if len(prefix) >= width:
        return _clip(label, width)
    return prefix + ("─" * (width - len(prefix)))


def _hard_wrap(text: str, width: int) -> list[str]:
    if width <= 0:
        return []
    if not text:
        return [""]
    return [text[index : index + width] for index in range(0, len(text), width)]


def _body_rows(kind: str, text: str, width: int, inner: int) -> list[str]:
    align_right = kind == "user"
    wrap_width = inner
    if align_right:
        wrap_width = max(8, width // 2)
    chunks = markdown_lines(text) if kind in PROSE_KINDS else [text]
    rows: list[str] = []
    for chunk in chunks:
        if not chunk.strip():
            rows.append(" " * width if align_right else "")
            continue
        wrapped = (
            _hard_wrap(chunk, wrap_width)
            if chunk.startswith("│ ")
            else _wrap(chunk, wrap_width)
        ) or [""]
        for row in wrapped:
            rows.append(row.rjust(width) if align_right else f"  {row}")
    return rows or ([""] if not align_right else [" " * width])


def build_log_view(log: Sequence[object], width: int) -> list[tuple[str, str]]:
    """Turn events into labeled blocks: (style, text)."""
    if width <= 0:
        return []
    if not log:
        return [("idle", "Ready. Watching for agent events.")]

    groups: list[tuple[str, list[str]]] = []
    for entry in log:
        kind, text = _as_log_entry(entry)
        if groups and groups[-1][0] == kind:
            groups[-1][1].append(text)
        else:
            groups.append((kind, [text]))

    inner = max(1, width - 2)
    lines: list[tuple[str, str]] = []
    for index, (kind, texts) in enumerate(groups):
        if index:
            lines.append(("gap", ""))
        if kind != "user":
            label = KIND_LABEL.get(kind, kind)
            lines.append((kind, _rule(label, width)))
        for text in texts:
            for row in _body_rows(kind, text, width, inner):
                lines.append((kind, row))
    return lines


def extend_log(log: list[RexEvent], incoming: Sequence[RexEvent]) -> int:
    """Append new events, skipping duplicates already in the live buffer."""
    added = 0
    for event in incoming:
        if is_duplicate(event, log[-12:]):
            continue
        log.append(event)
        added += 1
    if len(log) > MAX_LOG:
        del log[:-MAX_LOG]
    return added


def pane_body_height(term_lines: int) -> int:
    show_box = term_lines >= INPUT_ROWS + FOOTER_ROWS + 3
    height = term_lines - 2 - FOOTER_ROWS
    if show_box:
        height -= INPUT_ROWS
    return max(0, height)


def pane_text_width(columns: int) -> int:
    mascot_cols = min(STAGE_SIZE, max(1, columns))
    text_width = max(1, columns) - mascot_cols - GUTTER
    if text_width < MIN_TEXT_COLS:
        return 0
    return text_width


def clamp_scroll(total: int, view_h: int, scroll: int) -> int:
    return max(0, min(scroll, max(0, total - view_h)))


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
    log: Sequence[object],
    draft: str,
    quit_pending: bool,
    enabled: bool,
    scroll: int = 0,
) -> list[str]:
    if width <= 0 or height <= 0:
        return [""] * max(0, height)

    show_box = height >= INPUT_ROWS + FOOTER_ROWS + 3
    body_top = 2
    body_bottom = height - FOOTER_ROWS
    if show_box:
        body_bottom -= INPUT_ROWS
    body_h = max(0, body_bottom - body_top)

    body = build_log_view(log, width)
    scroll = clamp_scroll(len(body), body_h, scroll)
    if body_h:
        end = len(body) - scroll
        start = max(0, end - body_h)
        visible_body = body[start:end]
    else:
        visible_body = []

    if quit_pending:
        footer_text = "press ctrl+c again to exit"
        footer_color = UI["error"]
    elif scroll:
        footer_text = f"↑↓ {scroll} above live · ctrl+e bottom · ctrl+c twice"
        footer_color = UI["muted"]
    else:
        footer_text = "↑↓ scroll · ctrl+e bottom · ctrl+c twice"
        footer_color = UI["muted"]

    rows = [""] * height
    rows[0] = _header(width, meta, enabled)
    if height > 1:
        rows[1] = " " * width

    for offset, (kind, line) in enumerate(visible_body):
        if kind == "gap":
            rows[body_top + offset] = " " * width
            continue
        color = UI[KIND_COLOR.get(kind, "text")]
        rows[body_top + offset] = paint(_clip(line, width), color, enabled)
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
    log: Sequence[object] = (),
    quit_pending: bool = False,
    status: str | None = None,
    scroll: int = 0,
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
        meta=status or animation_name,
        log=log,
        draft=draft,
        quit_pending=quit_pending,
        enabled=enabled,
        scroll=scroll,
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
    events_path: Path | str | None = None,
) -> int:
    sequence = get_animation(animation)
    frame_index = 0
    hold = sequence.frames[0][1]
    sprite_name = sequence.frames[0][0]
    columns, lines = size()
    animated = len(sequence.frames) > 1
    draft = ""
    log: list[object] = []
    quit_pending = False
    quit_ticks = 0
    status = sequence.name
    scroll = 0
    tail = EventTail(
        Path(events_path) if events_path else default_event_path(),
        from_end=True,
    )

    def _metrics() -> tuple[int, int]:
        width = pane_text_width(columns)
        return width, pane_body_height(lines)

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
                status=status,
                scroll=scroll,
            )
        )
        stdout.flush()

    with live_screen(stdout):
        draw()
        while True:
            try:
                key = read_key(stdin, FRAME_TICK)
            except KeyboardInterrupt:
                key = CTRL_C
            changed = False
            fresh = tail.poll()
            if fresh:
                text_w, _ = _metrics()
                before = len(build_log_view(log, text_w)) if text_w else 0
                added = extend_log(log, fresh)
                after = len(build_log_view(log, text_w)) if text_w else 0
                if added:
                    if scroll:
                        scroll += max(0, after - before)
                    status = KIND_STATUS.get(fresh[-1].kind, sequence.name)
                    changed = True
            text_w, body_h = _metrics()
            total_lines = len(build_log_view(log, text_w)) if text_w else 0
            max_scroll = clamp_scroll(total_lines, body_h, total_lines)
            if key == "up":
                scroll = min(max_scroll, scroll + 1)
                changed = True
            elif key == "down":
                scroll = max(0, scroll - 1)
                changed = True
            elif key == "pageup":
                scroll = min(max_scroll, scroll + max(1, body_h))
                changed = True
            elif key == "pagedown":
                scroll = max(0, scroll - max(1, body_h))
                changed = True
            elif key == "home":
                scroll = max_scroll
                changed = True
            elif key == "end":
                scroll = 0
                changed = True
            elif key == CTRL_C:
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
                    log.append(RexEvent("user", "text", draft.strip()))
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
            text_w, body_h = _metrics()
            total_lines = len(build_log_view(log, text_w)) if text_w else 0
            clamped = clamp_scroll(total_lines, body_h, scroll)
            if clamped != scroll:
                scroll = clamped
                changed = True
            if changed:
                draw()
