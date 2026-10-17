"""Read one vendor payload (or JSONL) and append RexEvents to the bus."""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any, TextIO

from rex.watch.adapters import map_payload
from rex.watch.bus import append_event, default_event_path
from rex.watch.events import RexEvent

CURSOR_HOOK_EVENTS = (
    "beforeSubmitPrompt",
    "afterAgentThought",
    "beforeShellExecution",
    "preToolUse",
    "afterAgentResponse",
)


def ingest_command() -> str:
    return f'"{sys.executable}" -m rex ingest --source cursor'


def install_cursor_hook(stdout: TextIO) -> int:
    path = Path.home() / ".cursor" / "hooks.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    data: dict[str, Any] = {"version": 1, "hooks": {}}
    if path.exists():
        try:
            loaded = json.loads(path.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            loaded = {}
        if isinstance(loaded, dict):
            data.update(loaded)
            hooks = loaded.get("hooks")
            if isinstance(hooks, dict):
                data["hooks"] = dict(hooks)
    hooks = data.setdefault("hooks", {})
    command = ingest_command()
    for name in CURSOR_HOOK_EVENTS:
        entries = list(hooks.get(name) or [])
        already = any(
            isinstance(entry, dict) and entry.get("command") == command for entry in entries
        )
        if not already:
            entries.append({"command": command})
        hooks[name] = entries
    data["version"] = int(data.get("version") or 1)
    path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
    stdout.write(f"installed cursor hooks at {path}\n")
    stdout.write(f"feed: {default_event_path()}\n")
    return 0


def _payloads_from_raw(raw: str) -> list[dict[str, Any]]:
    stripped = raw.strip()
    if not stripped:
        return []
    try:
        parsed = json.loads(stripped)
    except json.JSONDecodeError:
        parsed = None
    if isinstance(parsed, dict):
        return [parsed]
    if isinstance(parsed, list):
        return [item for item in parsed if isinstance(item, dict)]
    payloads: list[dict[str, Any]] = []
    for line in stripped.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            item = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(item, dict):
            payloads.append(item)
    return payloads


def ingest_raw(raw: str, *, source: str, path: Path) -> list[RexEvent]:
    written: list[RexEvent] = []
    for payload in _payloads_from_raw(raw):
        event = map_payload(payload, source)
        if event is None or not event.text.strip():
            continue
        if append_event(path, event):
            written.append(event)
    return written


def run_ingest(
    *,
    stdin: TextIO,
    stdout: TextIO,
    source: str = "auto",
    install_cursor_hook_flag: bool = False,
    events_path: Path | None = None,
) -> int:
    if install_cursor_hook_flag:
        return install_cursor_hook(stdout)
    path = events_path or default_event_path()
    ingest_raw(stdin.read(), source=source, path=path)
    return 0
