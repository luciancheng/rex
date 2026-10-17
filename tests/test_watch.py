import io
import json
from pathlib import Path

from rex.cli import main
from rex.watch.adapters import map_payload
from rex.watch.bus import EventTail, append_event
from rex.watch.events import RexEvent


def test_cursor_user_prompt() -> None:
    event = map_payload(
        {
            "hook_event_name": "beforeSubmitPrompt",
            "prompt": "can we format this",
        }
    )
    assert event is not None
    assert event.kind == "user"
    assert event.text == "can we format this"


def test_cursor_write_skips_file_body() -> None:
    event = map_payload(
        {
            "hook_event_name": "preToolUse",
            "tool_name": "Write",
            "tool_input": {
                "path": "/tmp/note.md",
                "contents": "# huge dump\n" + ("x" * 200),
            },
        }
    )
    assert event is not None
    assert event.text == "Write /tmp/note.md"
    assert "huge dump" not in event.text


def test_duplicate_thought_is_not_appended(tmp_path: Path) -> None:
    feed = tmp_path / "session.jsonl"
    first = RexEvent("cursor", "thought", "Looking at sprites.py")
    assert append_event(feed, first) is True
    assert append_event(feed, RexEvent("cursor", "thought", "Looking at sprites.py")) is False
    lines = feed.read_text(encoding="utf-8").strip().splitlines()
    assert len(lines) == 1


def test_cursor_thought_and_shell() -> None:
    thought = map_payload(
        {"hook_event_name": "afterAgentThought", "text": "Check the idle sprite."}
    )
    assert thought is not None
    assert thought.kind == "thought"
    assert "idle sprite" in thought.text
    command = map_payload(
        {"hook_event_name": "beforeShellExecution", "command": "pytest -q"}
    )
    assert command is not None
    assert command.kind == "command"
    assert command.text == "pytest -q"


def test_cursor_read_tool_shows_path() -> None:
    event = map_payload(
        {
            "hook_event_name": "preToolUse",
            "tool_name": "Read",
            "tool_input": {"file_path": "/tmp/idle.png"},
        }
    )
    assert event is not None
    assert event.kind == "command"
    assert event.text == "Read /tmp/idle.png"


def test_cursor_question_tool() -> None:
    event = map_payload(
        {
            "hook_event_name": "preToolUse",
            "tool_name": "AskQuestion",
            "tool_input": {"question": "Install the hook?"},
        }
    )
    assert event is not None
    assert event.kind == "question"
    assert "Install the hook?" in event.text


def test_claude_and_codex_map() -> None:
    claude = map_payload(
        {
            "hook_event_name": "PreToolUse",
            "tool_name": "Bash",
            "tool_input": {"command": "ls"},
        },
        "auto",
    )
    assert claude is not None
    assert claude.source == "claude"
    assert claude.kind == "command"
    codex = map_payload(
        {
            "type": "item.started",
            "item": {"type": "reasoning", "text": "Looking at the file."},
        }
    )
    assert codex is not None
    assert codex.source == "codex"
    assert codex.kind == "thought"


def test_event_tail_reads_new_lines(tmp_path: Path) -> None:
    feed = tmp_path / "session.jsonl"
    tail = EventTail(feed)
    assert tail.poll() == []
    append_event(feed, RexEvent("cursor", "thought", "hello"))
    first = tail.poll()
    assert len(first) == 1
    assert first[0].text == "hello"
    assert tail.poll() == []
    append_event(feed, RexEvent("cursor", "command", "echo hi"))
    second = tail.poll()
    assert second[0].kind == "command"


def test_event_tail_can_skip_backlog(tmp_path: Path) -> None:
    feed = tmp_path / "session.jsonl"
    append_event(feed, RexEvent("cursor", "thought", "old"))
    tail = EventTail(feed, from_end=True)
    assert tail.poll() == []
    append_event(feed, RexEvent("cursor", "thought", "new"))
    fresh = tail.poll()
    assert [event.text for event in fresh] == ["new"]


def test_ingest_cli_appends(tmp_path: Path) -> None:
    feed = tmp_path / "session.jsonl"
    payload = json.dumps(
        {"hook_event_name": "afterAgentThought", "text": "from the hook"}
    )
    stdin = io.StringIO(payload)
    stdout = io.StringIO()
    code = main(
        ["ingest", "--source", "cursor", "--events", str(feed)],
        stdin=stdin,
        stdout=stdout,
    )
    assert code == 0
    lines = feed.read_text(encoding="utf-8").strip().splitlines()
    assert len(lines) == 1
    assert "from the hook" in lines[0]
