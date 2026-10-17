"""Color palette for the mascot and UI chrome.

Edit the ``yellow`` RGB triples to recolor the idle sprite. That is the look
``rex start`` uses. Sampled from ``assets/idle.png``.
"""

from __future__ import annotations

Palette = dict[str, tuple[int, int, int]]

DEFAULT_PALETTE = "yellow"

PALETTES: dict[str, Palette] = {
    "yellow": {
        "outline": (0, 0, 0),
        "body": (253, 207, 66),
        "highlight": (255, 219, 110),
        "shade": (253, 150, 47),
        "dark": (250, 113, 50),
        "mid": (247, 156, 51),
        "accent": (255, 158, 49),
        "belly": (254, 162, 46),
        "eye": (0, 0, 0),
        "white": (255, 255, 255),
        "blush": (253, 133, 66),
        "shadow": (41, 58, 82),
        "mark": (236, 240, 248),
        "green": (88, 214, 108),
        "red": (232, 78, 78),
        "teal": (86, 168, 220),
    },
}

UI = {
    "title": (244, 196, 66),
    "muted": (132, 148, 172),
    "text": (226, 232, 240),
    "border": (52, 68, 92),
    "accent": (244, 196, 66),
    "error": (232, 78, 78),
    "you": (92, 196, 232),
}
