"""Map Cursor / Claude Code / Codex payloads onto RexEvent."""

from __future__ import annotations

import json
from typing import Any

from rex.watch.events import RexEvent

CURSOR_EVENTS = {
    "afterAgentThought",
    "beforeSubmitPrompt",
    "beforeShellExecution",
    "preToolUse",
    "afterAgentResponse",
    "stop",
}
CLAUDE_EVENTS = {
    "PreToolUse",
    "PostToolUse",
    "UserPromptSubmit",
    "Notification",
    "Stop",
    "SessionStart",
}
CODEX_TYPES = {
    "thread.started",
    "turn.started",
    "turn.completed",
    "turn.failed",
    "item.started",
    "item.updated",
    "item.completed",
    "error",
}


def _as_dict(value: Any) -> dict[str, Any]:
    if isinstance(value, dict):
        return value
    if isinstance(value, str):
        try:
            parsed = json.loads(value)
        except json.JSONDecodeError:
            return {}
        if isinstance(parsed, dict):
            return parsed
    return {}


def _as_command(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, list):
        return " ".join(str(part) for part in value)
    if isinstance(value, dict):
        if "command" in value:
            return _as_command(value["command"])
        if "cmd" in value:
            return _as_command(value["cmd"])
        return json.dumps(value, ensure_ascii=True)
    return str(value)


def _hook_name(payload: dict[str, Any]) -> str:
    return str(
        payload.get("hook_event_name")
        or payload.get("hookEventName")
        or payload.get("event")
        or ""
    )


def detect_source(payload: dict[str, Any], hint: str = "auto") -> str:
    if hint and hint != "auto":
        return hint
    name = _hook_name(payload)
    if name in CLAUDE_EVENTS:
        return "claude"
    if name in CURSOR_EVENTS or "conversation_id" in payload or "generation_id" in payload:
        return "cursor"
    typ = str(payload.get("type") or "")
    if typ in CODEX_TYPES or isinstance(payload.get("item"), dict):
        return "codex"
    if name:
        return "cursor"
    return "cursor"


def _tool_kind(tool: str) -> str:
    lowered = tool.lower()
    if "ask" in lowered or "question" in lowered:
        return "question"
    return "command"


_SKIP_TOOL_KEYS = {
    "content",
    "contents",
    "old_string",
    "new_string",
    "new_text",
    "diff",
}


def _tool_text(tool: str, raw_input: Any) -> str:
    data = _as_dict(raw_input) if not isinstance(raw_input, list) else {}
    if "question" in data and data["question"]:
        return str(data["question"])
    if "prompt" in data and data["prompt"]:
        return str(data["prompt"])
    if "command" in data or "cmd" in data:
        return _as_command(data)
    if "file_path" in data and data["file_path"]:
        return f"{tool} {data['file_path']}"
    if "path" in data and data["path"]:
        return f"{tool} {data['path']}"
    if "query" in data and data["query"]:
        return f"{tool} {data['query']}"
    if any(key in data for key in _SKIP_TOOL_KEYS):
        leftover = {key: value for key, value in data.items() if key not in _SKIP_TOOL_KEYS}
        if leftover:
            return f"{tool} {json.dumps(leftover, ensure_ascii=True)}"
        return tool
    command = _as_command(raw_input)
    if command and command not in {"{}", ""}:
        return command
    if data:
        return f"{tool} {json.dumps(data, ensure_ascii=True)}"
    return tool


def map_cursor(payload: dict[str, Any]) -> RexEvent | None:
    name = _hook_name(payload)
    if name == "beforeSubmitPrompt":
        text = str(payload.get("prompt") or payload.get("text") or "").strip()
        if not text:
            return None
        return RexEvent("cursor", "user", text)
    if name == "afterAgentThought":
        text = str(payload.get("text") or "").strip()
        if not text:
            return None
        return RexEvent("cursor", "thought", text)
    if name == "beforeShellExecution":
        command = _as_command(payload.get("command") or payload.get("tool_input"))
        if not command.strip():
            return None
        return RexEvent("cursor", "command", command.strip(), meta={"tool": "shell"})
    if name == "preToolUse":
        tool = str(payload.get("tool_name") or payload.get("toolName") or "tool")
        text = _tool_text(tool, payload.get("tool_input") or payload.get("toolInput"))
        if not text.strip():
            return None
        return RexEvent("cursor", _tool_kind(tool), text.strip(), meta={"tool": tool})
    if name == "afterAgentResponse":
        text = str(payload.get("text") or "").strip()
        if not text:
            return None
        return RexEvent("cursor", "text", text)
    return None


def map_claude(payload: dict[str, Any]) -> RexEvent | None:
    name = _hook_name(payload)
    if name == "PreToolUse":
        tool = str(payload.get("tool_name") or payload.get("toolName") or "tool")
        text = _tool_text(tool, payload.get("tool_input") or payload.get("toolInput"))
        if not text.strip():
            return None
        return RexEvent("claude", _tool_kind(tool), text.strip(), meta={"tool": tool})
    if name == "PostToolUse":
        tool = str(payload.get("tool_name") or payload.get("toolName") or "tool")
        return RexEvent("claude", "result", tool, meta={"tool": tool})
    if name == "Notification":
        text = str(payload.get("message") or payload.get("text") or "").strip()
        if not text:
            return None
        return RexEvent("claude", "question", text)
    if name == "UserPromptSubmit":
        text = str(payload.get("prompt") or payload.get("text") or "").strip()
        if not text:
            return None
        return RexEvent("claude", "user", text)
    return None


def map_codex(payload: dict[str, Any]) -> RexEvent | None:
    item = payload.get("item")
    if not isinstance(item, dict):
        return None
    item_type = str(item.get("type") or "")
    if item_type in {"reasoning"}:
        text = str(item.get("text") or item.get("content") or "").strip()
        kind = "thought"
    elif item_type in {"agent_message", "agentMessage"}:
        text = str(item.get("text") or "").strip()
        kind = "text"
    elif item_type in {"command_execution", "commandExecution"}:
        text = _as_command(item.get("command")).strip()
        kind = "command"
    else:
        return None
    if not text:
        return None
    return RexEvent("codex", kind, text, meta={"item": item_type})


def map_payload(payload: dict[str, Any], source: str = "auto") -> RexEvent | None:
    resolved = detect_source(payload, source)
    if resolved == "claude":
        return map_claude(payload)
    if resolved == "codex":
        return map_codex(payload)
    return map_cursor(payload)
