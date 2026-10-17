"""Turn agent markdown into short terminal lines, and fingerprint events."""

from __future__ import annotations

import re
from typing import Sequence

from rex.watch.events import RexEvent

PROSE_KINDS = {"thought", "text", "user"}
_HEADING = re.compile(r"^#{1,6}\s+")
_UL = re.compile(r"^(\s*)([-*+])\s+")
_OL = re.compile(r"^(\s*)(\d+)\.\s+")
_LINK = re.compile(r"\[([^\]]+)\]\([^)]+\)")
_BOLD = re.compile(r"(\*\*|__)(.+?)\1")
_ITALIC = re.compile(r"(?<!\*)\*(?!\*)(.+?)(?<!\*)\*(?!\*)")
_CODE = re.compile(r"`([^`]+)`")
_RULE = re.compile(r"^\s*([-*_]\s*){3,}$")


def normalize_text(text: str) -> str:
    return " ".join(text.split())


def is_duplicate(
    event: RexEvent,
    recent: Sequence[RexEvent],
    *,
    window: float = 4.0,
) -> bool:
    """True when this event is a repeat of something already on the bus."""
    key = normalize_text(event.text)
    if not key:
        return True
    for prev in reversed(recent):
        if prev.kind != event.kind:
            continue
        if normalize_text(prev.text) != key:
            continue
        if event.kind == "thought":
            return True
        if abs(event.ts - prev.ts) <= window:
            return True
    return False


def markdown_lines(text: str) -> list[str]:
    """Flatten markdown into readable terminal rows. Code stays indented."""
    if not text or not str(text).strip():
        return [""]
    out: list[str] = []
    in_fence = False
    for raw in str(text).replace("\r\n", "\n").split("\n"):
        stripped = raw.strip()
        if stripped.startswith("```"):
            in_fence = not in_fence
            continue
        if in_fence:
            out.append("│ " + raw.rstrip())
            continue
        if _RULE.match(raw):
            if out and out[-1] != "":
                out.append("")
            continue
        line = _inline(_block(raw.rstrip()))
        out.append(line)
    return _compact(out)


def _block(line: str) -> str:
    if _HEADING.match(line):
        return _HEADING.sub("", line).strip()
    match = _UL.match(line)
    if match:
        indent = "  " * (len(match.group(1).replace("\t", "  ")) // 2)
        return f"{indent}• {line[match.end():]}"
    match = _OL.match(line)
    if match:
        indent = "  " * (len(match.group(1).replace("\t", "  ")) // 2)
        return f"{indent}{match.group(2)}. {line[match.end():]}"
    return line


def _inline(line: str) -> str:
    line = _LINK.sub(r"\1", line)
    line = _CODE.sub(r"\1", line)
    line = _BOLD.sub(r"\2", line)
    line = _ITALIC.sub(r"\1", line)
    return line


def _compact(lines: list[str]) -> list[str]:
    compacted: list[str] = []
    blank = False
    for line in lines:
        empty = not line.strip()
        if empty:
            if compacted and not blank:
                compacted.append("")
            blank = True
            continue
        compacted.append(line)
        blank = False
    return compacted or [""]
