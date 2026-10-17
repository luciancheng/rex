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


def test_start_color_flag() -> None:
    args = build_parser().parse_args(["start", "--color", "blue"])
    assert args.color == "blue"


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
