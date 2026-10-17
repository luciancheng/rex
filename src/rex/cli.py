"""Command-line entry point for REX."""

from __future__ import annotations

import argparse
import sys
from typing import Sequence, TextIO

from rex import __version__
from rex.mascot import DEFAULT_PALETTE, PALETTES, color_enabled
from rex.tui import print_card, restore_on_interrupt, run_loop


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="rex",
        description="REX — a terminal companion with a dinosaur mascot.",
        epilog=(
            "examples:\n"
            "  rex start\n"
            "  rex start --static\n"
            "  rex start --color blue"
        ),
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--version",
        action="version",
        version=f"rex {__version__}",
    )

    sub = parser.add_subparsers(dest="command")
    start = sub.add_parser("start", help="open the companion UI")
    start.add_argument(
        "-c",
        "--color",
        choices=tuple(PALETTES),
        default=DEFAULT_PALETTE,
        help=f"palette (default: {DEFAULT_PALETTE})",
    )
    start.add_argument(
        "--no-color",
        action="store_true",
        help="disable ANSI color",
    )
    start.add_argument(
        "--static",
        action="store_true",
        help="print one frame and exit",
    )
    return parser


def _interactive(stdin: TextIO, stdout: TextIO) -> bool:
    return bool(stdout.isatty() and stdin.isatty())


def main(
    argv: Sequence[str] | None = None,
    stdin: TextIO = sys.stdin,
    stdout: TextIO = sys.stdout,
    stderr: TextIO = sys.stderr,
) -> int:
    parser = build_parser()
    args = parser.parse_args(list(argv) if argv is not None else None)

    try:
        if args.command != "start":
            parser.print_help(stdout)
            return 0

        enabled = False if args.no_color else color_enabled(stdout)
        if args.static or not _interactive(stdin, stdout):
            print_card("idle", args.color, enabled=enabled, stdout=stdout)
            return 0
        return run_loop(
            color=args.color,
            enabled=enabled,
            animation="idle",
            stdin=stdin,
            stdout=stdout,
        )
    except ValueError as exc:
        stderr.write(f"rex: {exc}\n")
        return 1
    except KeyboardInterrupt:
        restore_on_interrupt(stdout)
        stdout.write("\n")
        return 130
    except BrokenPipeError:
        return 0


if __name__ == "__main__":
    raise SystemExit(main())
