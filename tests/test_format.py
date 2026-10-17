from rex.tui.live import build_log_view, extend_log
from rex.watch.events import RexEvent
from rex.watch.format import is_duplicate, markdown_lines


def test_markdown_lines_headings_lists_and_code() -> None:
    lines = markdown_lines(
        "## Summary\n"
        "Here is **bold** and `inline` plus a [link](https://example.com).\n"
        "\n"
        "- first\n"
        "- second\n"
        "\n"
        "```python\n"
        "print('hi')\n"
        "```\n"
    )
    assert "Summary" in lines
    assert any(line.startswith("Here is bold and inline plus a link") for line in lines)
    assert "• first" in lines
    assert "• second" in lines
    assert any(line.startswith("│ ") and "print" in line for line in lines)
    assert not any("**" in line or "##" in line or "```" in line for line in lines)


def test_duplicate_thoughts_match_even_later() -> None:
    first = RexEvent("cursor", "thought", "Check the idle sprite.", ts=1.0)
    again = RexEvent("cursor", "thought", "Check  the idle sprite.", ts=9.0)
    assert is_duplicate(again, [first])
    other = RexEvent("cursor", "thought", "Something else.", ts=9.0)
    assert not is_duplicate(other, [first])


def test_extend_log_drops_repeated_thoughts() -> None:
    log: list[RexEvent] = []
    first = RexEvent("cursor", "thought", "Looking at live.py")
    added = extend_log(log, [first, RexEvent("cursor", "thought", "Looking at live.py")])
    assert added == 1
    assert len(log) == 1


def test_user_prompt_is_right_aligned() -> None:
    view = build_log_view(
        [RexEvent("cursor", "user", "format this please")],
        width=36,
    )
    assert view[0][0] == "user"
    assert not any("──" in line or line.strip().lower() == "you" for _, line in view)
    body = [line for kind, line in view if kind == "user" and "format this please" in line]
    assert body
    assert body[0].rstrip().endswith("format this please")
    assert body[0].startswith(" ")


def test_user_prompt_box_is_left_aligned_inside() -> None:
    prompt = "one two three four five six seven eight nine ten eleven"
    width = 40
    view = build_log_view([RexEvent("cursor", "user", prompt)], width=width)
    body = [line for kind, line in view if kind == "user" and line.strip()]
    assert len(body) > 1
    half = width // 2
    starts = [len(line) - len(line.lstrip()) for line in body]
    assert len(set(starts)) == 1
    start = starts[0]
    assert start >= width - half
    for line in body:
        assert line[start] != " "
        assert len(line.rstrip()) - start <= half
        assert len(line.rstrip()) <= width
