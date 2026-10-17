"""Low-level terminal control for interactive mascot views."""

from __future__ import annotations

import os
import select
import sys
import termios
import tty
from collections.abc import Iterator
from contextlib import contextmanager
from typing import TextIO

HIDE_CURSOR = "\033[?25l"
SHOW_CURSOR = "\033[?25h"
ALT_ENTER = "\033[?1049h"
ALT_LEAVE = "\033[?1049l"
WRAP_OFF = "\033[?7l"
WRAP_ON = "\033[?7h"
HOME = "\033[H"
CLEAR = "\033[2J"
CLEAR_DOWN = "\033[J"
RESET = "\033[0m"


def size() -> tuple[int, int]:
    try:
        columns, lines = os.get_terminal_size()
    except OSError:
        return 80, 24
    return columns, lines


@contextmanager
def raw_mode(stream: TextIO) -> Iterator[None]:
    fd = stream.fileno()
    original = termios.tcgetattr(fd)
    try:
        # Like cbreak, but ISIG off so Ctrl+C is a key instead of SIGINT.
        # Do not use setraw: that disables OPOST, so \n no longer returns
        # to column 0 and every row after the first gets clipped.
        mode = termios.tcgetattr(fd)
        mode[tty.LFLAG] &= ~(termios.ECHO | termios.ICANON | termios.ISIG)
        mode[tty.CC][termios.VMIN] = 1
        mode[tty.CC][termios.VTIME] = 0
        termios.tcsetattr(fd, termios.TCSADRAIN, mode)
        yield
    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, original)


@contextmanager
def live_screen(stream: TextIO) -> Iterator[None]:
    stream.write(ALT_ENTER + WRAP_OFF + HIDE_CURSOR + CLEAR + HOME)
    stream.flush()
    try:
        with raw_mode(stream):
            yield
    finally:
        stream.write(WRAP_ON + SHOW_CURSOR + ALT_LEAVE + RESET)
        stream.flush()


def read_key(stream: TextIO, timeout: float) -> str | None:
    fd = stream.fileno()
    ready, _, _ = select.select([fd], [], [], timeout)
    if not ready:
        return None
    ch = os.read(fd, 1).decode("utf-8", errors="ignore")
    if ch != "\x1b":
        return ch
    extra = b""
    if select.select([fd], [], [], 0.02)[0]:
        extra += os.read(fd, 8)
    seq = ch + extra.decode("utf-8", errors="ignore")
    return {
        "\x1b[A": "up",
        "\x1b[B": "down",
        "\x1b[C": "right",
        "\x1b[D": "left",
        "\x1b": "esc",
    }.get(seq, seq)


def restore_on_interrupt(stream: TextIO | None = None) -> None:
    stream = stream or sys.stdout
    if not getattr(stream, "isatty", lambda: False)():
        return
    stream.write(SHOW_CURSOR + RESET)
    stream.flush()
