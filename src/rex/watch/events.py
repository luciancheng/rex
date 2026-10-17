"""Canonical agent events. Adapters map vendor payloads into this shape."""

from __future__ import annotations

import json
import time
from dataclasses import dataclass, field
from typing import Any

KINDS = ("thought", "text", "user", "command", "question", "result", "idle", "error")
SOURCES = ("cursor", "claude", "codex", "user", "demo")


@dataclass(frozen=True)
class RexEvent:
    source: str
    kind: str
    text: str
    ts: float = field(default_factory=time.time)
    meta: dict[str, Any] = field(default_factory=dict)

    def to_json(self) -> str:
        return json.dumps(
            {
                "source": self.source,
                "kind": self.kind,
                "text": self.text,
                "ts": self.ts,
                "meta": self.meta,
            },
            ensure_ascii=True,
        )

    @classmethod
    def from_json(cls, line: str) -> "RexEvent":
        data = json.loads(line)
        return cls(
            source=str(data.get("source") or "demo"),
            kind=str(data.get("kind") or "text"),
            text=str(data.get("text") or ""),
            ts=float(data.get("ts") or 0),
            meta=dict(data.get("meta") or {}),
        )

    def display_text(self) -> str:
        if self.kind in {"thought", "text", "user"}:
            return self.text
        text = " ".join(self.text.split())
        if self.kind == "command":
            return f"$ {text}"
        if self.kind == "question":
            return f"? {text}"
        return text
