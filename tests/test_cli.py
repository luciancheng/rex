import io
import re
from unittest import mock

from rex.cli import build_parser, main
from rex.mascot import STAGE_SIZE, fit_stage, get_sprite, palette_for, render_rows
from rex.tui import compose_frame

ANSI = re.compile(r"\033\[[0-9;]*[A-Za-z]")


def test_help() -> None:
    stdout = io.StringIO()
    with mock.patch("sys.stdout", stdout):
        try:
            main(["--help"])
        except SystemExit as caught:
            assert caught.code == 0
        else:
            raise AssertionError("expected SystemExit")
    text = stdout.getvalue()
    assert "dinosaur" in text
    assert "rex start" in text


def test_no_command_prints_help() -> None:
    stdout = io.StringIO()
    code = main([], stdout=stdout)
    assert code == 0
    assert "rex start" in stdout.getvalue()


def test_start_static_card() -> None:
    stdout = io.StringIO()
    code = main(["start", "--static", "--no-color"], stdout=stdout)
    assert code == 0
    assert "REX" in stdout.getvalue()
    assert "idle" in stdout.getvalue()


def test_start_noninteractive_prints_card() -> None:
    stdout = io.StringIO()
    stdin = io.StringIO()
    code = main(["start", "--no-color"], stdin=stdin, stdout=stdout)
    assert code == 0
    assert "REX" in stdout.getvalue()


def test_unknown_command_is_error() -> None:
    stdout = io.StringIO()
    stderr = io.StringIO()
    with mock.patch("sys.stdout", stdout), mock.patch("sys.stderr", stderr):
        try:
            main(["demo"])
        except SystemExit as caught:
            assert caught.code == 2
        else:
            raise AssertionError("expected SystemExit")


def test_compose_frame_puts_text_right_of_mascot() -> None:
    frame = compose_frame(
        "idle",
        "yellow",
        enabled=False,
        columns=80,
        lines=24,
        animation_name="idle",
    )
    line = ANSI.sub("", frame.split("\n")[0])
    assert "REX" in line
    assert line.index("REX") >= STAGE_SIZE
    assert "ctrl+c" in frame
    assert "q quit" not in frame
    assert "Ready." in frame
    assert "╭" in frame


def test_compose_frame_shows_agent_events() -> None:
    from rex.watch.events import RexEvent

    frame = compose_frame(
        "idle",
        "yellow",
        enabled=False,
        columns=80,
        lines=24,
        animation_name="idle",
        log=(
            RexEvent("cursor", "thought", "I should inspect sprites.py"),
            RexEvent("cursor", "command", "pytest -q"),
            RexEvent("cursor", "question", "run tests?"),
        ),
        status="thinking",
    )
    assert "I should inspect sprites.py" in frame
    assert "$ pytest -q" in frame
    assert "? run tests?" in frame
    assert "── thinking" in frame
    assert "── run" in frame
    assert "── ask" in frame
    assert "thinking" in ANSI.sub("", frame.split("\n")[0])
    assert "Ready." not in frame


def test_compose_frame_formats_markdown_and_user_prompt() -> None:
    from rex.watch.events import RexEvent

    frame = compose_frame(
        "idle",
        "yellow",
        enabled=False,
        columns=80,
        lines=24,
        animation_name="idle",
        log=(
            RexEvent("cursor", "user", "can we format this"),
            RexEvent(
                "cursor",
                "text",
                "## Summary\n- first item\n- **second** item\n",
            ),
        ),
    )
    plain = ANSI.sub("", frame)
    assert "can we format this" in plain
    assert "you ──" not in plain
    assert "Summary" in plain
    assert "• first item" in plain
    assert "second item" in plain
    assert "##" not in plain
    assert "**" not in plain
    for line in plain.split("\n"):
        if "can we format this" in line:
            assert line.rstrip().endswith("can we format this")


def test_compose_frame_scrolls_older_events() -> None:
    from rex.watch.events import RexEvent

    events = tuple(
        RexEvent("cursor", "thought", f"block-{index:02d}") for index in range(30)
    )
    live = compose_frame(
        "idle",
        "yellow",
        enabled=False,
        columns=80,
        lines=24,
        animation_name="idle",
        log=events,
        scroll=0,
    )
    older = compose_frame(
        "idle",
        "yellow",
        enabled=False,
        columns=80,
        lines=24,
        animation_name="idle",
        log=events,
        scroll=40,
    )
    assert "block-29" in live
    assert "block-00" in older
    assert "↑↓" in older


def test_compose_frame_stays_top_aligned_when_short() -> None:
    frame = compose_frame(
        "idle",
        "yellow",
        enabled=False,
        columns=80,
        lines=6,
        animation_name="idle",
    )
    line = ANSI.sub("", frame.split("\n")[0])
    assert "REX" in line
    assert line.index("REX") >= STAGE_SIZE
    assert len(frame.split("\n")) <= 7


def test_compose_frame_keeps_full_colored_sprite() -> None:
    frame = compose_frame(
        "idle",
        "yellow",
        enabled=True,
        columns=80,
        lines=24,
        animation_name="idle",
    )
    body = "\n".join(
        render_rows(fit_stage(get_sprite("idle").rows), palette_for("yellow"), True)
    )
    # True-color pixels are long ANSI sequences. Truncating by column count
    # would leave a thin smear instead of the whole sprite.
    assert frame.count("▀") == body.count("▀")
    assert frame.count("▄") == body.count("▄")


def test_compose_frame_double_ctrl_c_hint() -> None:
    idle = compose_frame(
        "idle",
        "yellow",
        enabled=False,
        columns=80,
        lines=24,
        animation_name="idle",
    )
    armed = compose_frame(
        "idle",
        "yellow",
        enabled=False,
        columns=80,
        lines=24,
        animation_name="idle",
        quit_pending=True,
    )
    assert "press ctrl+c again to exit" in armed
    assert "press ctrl+c again to exit" not in idle
