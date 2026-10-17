"""Mascot sprites, palettes, and terminal rendering."""

from rex.mascot.catalog import (
    ANIMATIONS,
    DEFAULT_ANIMATION,
    DEFAULT_SPRITE,
    SPRITES,
    Animation,
    Sprite,
    get_animation,
    get_sprite,
    register_animation,
    register_sprite,
)
from rex.mascot.palettes import DEFAULT_PALETTE, PALETTES, UI
from rex.mascot.render import color_enabled, paint, palette_for, render_rows, render_sprite
from rex.mascot.sprites import (  # noqa: F401  IDLE registers idle sprite + animation
    STAGE_SIZE,
    IDLE,
    fit_stage,
)

__all__ = [
    "ANIMATIONS",
    "DEFAULT_ANIMATION",
    "DEFAULT_PALETTE",
    "DEFAULT_SPRITE",
    "PALETTES",
    "SPRITES",
    "STAGE_SIZE",
    "UI",
    "Animation",
    "Sprite",
    "color_enabled",
    "fit_stage",
    "get_animation",
    "get_sprite",
    "paint",
    "palette_for",
    "register_animation",
    "register_sprite",
    "render_rows",
    "render_sprite",
]
