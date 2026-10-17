import io
import os
from unittest import mock

from rex.mascot import get_sprite, color_enabled, render_sprite
from rex.tui import print_card


def test_no_color_has_no_ansi() -> None:
    output = render_sprite(get_sprite("idle"), enabled=False)
    assert "\033" not in output
    assert len(output.splitlines()) > 4


def test_color_uses_truecolor() -> None:
    output = render_sprite(get_sprite("idle"), color="yellow", enabled=True)
    assert "\033[38;2;" in output


def test_unknown_palette() -> None:
    try:
        render_sprite(get_sprite("idle"), color="sparkle")
    except ValueError as exc:
        assert "unknown color" in str(exc)
    else:
        raise AssertionError("expected ValueError")


def test_no_color_env() -> None:
    with mock.patch.dict(os.environ, {"NO_COLOR": "1"}, clear=False):
        assert color_enabled() is False


def test_print_card_includes_identity() -> None:
    buf = io.StringIO()
    print_card("idle", "yellow", enabled=False, stdout=buf)
    text = buf.getvalue()
    assert "REX" in text
    assert "idle" in text
