"""Append-only JSONL bus. Hooks write; the live view tails."""

from __future__ import annotations

import os
from pathlib import Path

from rex.watch.events import RexEvent
from rex.watch.format import is_duplicate

ENV_PATH = "REX_EVENTS"
_TAIL_BYTES = 65536
_RECENT = 12


def default_event_path() -> Path:
    override = os.environ.get(ENV_PATH)
    if override:
        return Path(override).expanduser()
    return Path.home() / ".rex" / "session.jsonl"


def last_events(path: Path, limit: int = _RECENT) -> list[RexEvent]:
    if not path.exists() or limit <= 0:
        return []
    data = path.read_bytes()
    chunk = data[-_TAIL_BYTES:].decode("utf-8", errors="ignore")
    lines = [line for line in chunk.splitlines() if line.strip()][-limit:]
    events: list[RexEvent] = []
    for line in lines:
        try:
            events.append(RexEvent.from_json(line))
        except (ValueError, TypeError, KeyError):
            continue
    return events


def append_event(path: Path, event: RexEvent) -> bool:
    """Write ``event`` unless it duplicates a recent line. Returns True if written."""
    if is_duplicate(event, last_events(path)):
        return False
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as handle:
        handle.write(event.to_json() + "\n")
        handle.flush()
    return True


class EventTail:
    """Read new JSONL lines since the last poll."""

    def __init__(self, path: Path, *, from_end: bool = False) -> None:
        self.path = path
        self._offset = 0
        if from_end and path.exists():
            self._offset = path.stat().st_size

    def poll(self) -> list[RexEvent]:
        if not self.path.exists():
            self._offset = 0
            return []
        size = self.path.stat().st_size
        if size < self._offset:
            self._offset = 0
        with self.path.open("r", encoding="utf-8") as handle:
            handle.seek(self._offset)
            chunk = handle.read()
            self._offset = handle.tell()
        events: list[RexEvent] = []
        for line in chunk.splitlines():
            raw = line.strip()
            if not raw:
                continue
            try:
                events.append(RexEvent.from_json(raw))
            except (ValueError, TypeError, KeyError):
                continue
        return events
