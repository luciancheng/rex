"""Registry for sprites and animations.

Add a new drawing with ``register_sprite``. Add a sequence of drawings with
``register_animation``. ``rex start`` plays the ``idle`` animation.
"""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class Sprite:
    """One still drawing. ``rows`` is a grid of character pixels."""

    name: str
    rows: tuple[str, ...]

    @property
    def width(self) -> int:
        return len(self.rows[0]) if self.rows else 0

    @property
    def height(self) -> int:
        return len(self.rows)


@dataclass(frozen=True)
class Animation:
    """Named sequence of sprites.

    Each frame is ``(sprite_name, hold_ticks)``. Tick length is owned by the
    live viewer. A one-frame animation is a still sprite.
    """

    name: str
    frames: tuple[tuple[str, int], ...]


SPRITES: dict[str, Sprite] = {}
ANIMATIONS: dict[str, Animation] = {}

DEFAULT_SPRITE = "idle"
DEFAULT_ANIMATION = "idle"


def register_sprite(sprite: Sprite) -> Sprite:
    if not sprite.rows:
        raise ValueError(f"sprite '{sprite.name}' has no rows")
    width = sprite.width
    if any(len(row) != width for row in sprite.rows):
        raise ValueError(f"sprite '{sprite.name}' rows must be the same width")
    SPRITES[sprite.name] = sprite
    return sprite


def register_animation(animation: Animation) -> Animation:
    if not animation.frames:
        raise ValueError(f"animation '{animation.name}' has no frames")
    for sprite_name, hold in animation.frames:
        if sprite_name not in SPRITES:
            raise ValueError(
                f"animation '{animation.name}' references unknown sprite '{sprite_name}'"
            )
        if hold < 1:
            raise ValueError(
                f"animation '{animation.name}' frame '{sprite_name}' hold must be >= 1"
            )
    ANIMATIONS[animation.name] = animation
    return animation


def get_sprite(name: str) -> Sprite:
    try:
        return SPRITES[name]
    except KeyError as exc:
        known = ", ".join(sorted(SPRITES)) or "(none)"
        raise ValueError(f"unknown sprite '{name}'. known: {known}") from exc


def get_animation(name: str) -> Animation:
    try:
        return ANIMATIONS[name]
    except KeyError as exc:
        known = ", ".join(sorted(ANIMATIONS)) or "(none)"
        raise ValueError(f"unknown animation '{name}'. known: {known}") from exc
