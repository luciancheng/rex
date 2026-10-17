# REX — for agents and contributors

Terminal sidecar with a pixel-art dinosaur. Mascot on the left, ingestible agent tape on the right. Watch-only: it does not run, approve, or steer the coding agent.

`rex start` works from any directory. The feed is `~/.rex/session.jsonl` (override with `REX_EVENTS`).

Install and user docs live in `README.md`. This file is how to change the code.

## Do not

- Add palettes or a `--color` flag. Yellow from `assets/idle.png` is the only look.
- Add dependencies, frameworks, or a config system without a concrete need. Python 3.9+ stdlib.
- Scrape transcripts or use the Cursor SDK to watch the in-IDE agent. Hooks only.
- Steer, inject prompts, or auto-approve tools unless the human asked for that this turn.
- Invent a custom `.rex` language. If a plan file happens, it is JSON named `plan.rex`.
- Dump Write bodies, duplicate thoughts, or flatten markdown into one blob in the pane.
- Giant ASCII, extra emoji, or a web UI squeezed into the terminal.
- Assume Claude Code or Codex are wired. Mappers exist; only Cursor hooks are installed.
- Build `plan.rex`, drift detection, or extra mascot poses unless asked this turn.

Idle and future poses are **40×40**. Ctrl+C twice to quit. Respect `NO_COLOR`. macOS, Linux, and Windows 10+ (VT).

## Repo map

| Area | Path |
| --- | --- |
| CLI | `src/rex/cli.py` |
| Mascot | `src/rex/mascot/` |
| Live UI / keys | `src/rex/tui/` |
| Event feed | `src/rex/watch/` |
| Idle PNG | `assets/idle.png` |
| README art | `assets/rex.png` (scaled idle) |
| Importer | `tools/import_sprite.py` |
| Tests | `tests/` |

## Mascot

Keep rendering separate from watch/TUI logic.

To change idle: replace `assets/idle.png`, then:

```bash
python tools/import_sprite.py assets/idle.png --apply
```

That writes `IDLE_ROWS` and the yellow palette 1:1. **Leave blink, glance, and squat as transforms** on `IDLE_ROWS` (`paint_pixels` / `shift_body`). Do not hand-edit a second grid that drifts from the PNG.

Blink drops the top eye pixel (the face `k` at column 25, rows 13–14 on the current sprite). Squat is `shift_body(..., dy=1, keep_bottom=7)` so the shadow stays planted.

If the README hero should match, rebuild `assets/rex.png` from `idle.png` with nearest-neighbor scale.

Animation must stay slow. Never let it eat input or overwrite the log.

## Watch feed

Cursor hooks (`~/.cursor/hooks.json`) spawn `python -m rex ingest --source cursor`. Adapters map vendor JSON to `RexEvent`; `append_event` dedupes; `EventTail(from_end=True)` paints new lines.

New harness = adapter (+ installer). Do not special-case vendors in `live.py`.

Kinds: `thought`, `text`, `user`, `command`, `question`, `result`, `idle`, `error`.

## TUI

- Left ~40 columns: mascot. Right: labeled log blocks (`thinking`, `run`, `ask`, `reply`).
- User prompts: no `YOU` rule. Invisible column, at most half the pane, **right-aligned as a box**, **left-aligned text inside**. Color `UI["you"]`.
- Markdown → headings, lists, stripped emphasis, fenced code as `│ ` lines.
- Keys: ↑↓, page, Home / Ctrl+A (oldest), End / Ctrl+E (live). Composer does not send to the agent.
- Clip ANSI by cells, not `len()`. Crop from the bottom; keep the mascot top-left.

## Commands and tests

```bash
pip install -e ".[dev]"
pytest
rex start
```

CI: GitHub Actions, Ubuntu/macOS/Windows, Python 3.9/3.11/3.13 (no macOS/Windows 3.9).

After sprite or TUI changes, restart a running `rex start`. Tests do not replace looking at the terminal.

## Definition of done

- Follows this file and any PNG the human attached.
- `pytest` passes.
- Terminal state restores on quit / Ctrl+C.
- No speculative product, extra commands, or architecture for a feature that was not requested.
