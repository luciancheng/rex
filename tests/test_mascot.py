from rex.mascot import (
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
from rex.mascot.sprites import STAGE_SIZE, fit_stage, sprite_size


def test_idle_is_registered() -> None:
    sprite = get_sprite("idle")
    assert sprite.name == DEFAULT_SPRITE
    width, height = sprite_size(sprite.rows)
    assert width == sprite.width == STAGE_SIZE
    assert height == sprite.height == STAGE_SIZE


def test_idle_has_dino_features() -> None:
    pixels = "".join(get_sprite("idle").rows)
    assert "k" in pixels
    assert "y" in pixels
    assert "b" in pixels
    assert "d" in pixels
    assert "s" in pixels


def test_idle_animation_has_life() -> None:
    animation = get_animation(DEFAULT_ANIMATION)
    names = {name for name, _hold in animation.frames}
    assert names == {"idle", "idle_blink", "idle_look", "idle_breathe"}
    open_eye = get_sprite("idle").rows
    closed = get_sprite("idle_blink").rows
    looking = get_sprite("idle_look").rows
    assert open_eye[13][25] == "k"
    assert closed[13][25] == "y"
    assert closed[14][25] == "k"
    assert looking[13][26] == "k"
    assert looking[14][26] == "k"
    breathe = get_sprite("idle_breathe").rows
    assert breathe[0] == "." * 40
    assert breathe[-1] == open_eye[-1]
    assert breathe[14][25] == open_eye[13][25]


def test_fit_stage_reserves_40x40() -> None:
    idle = get_sprite("idle").rows
    staged = fit_stage(idle)
    width, height = sprite_size(staged)
    assert width == height == STAGE_SIZE
    assert staged == idle


def test_fit_stage_keeps_top_left_when_clipping() -> None:
    rows = ("abcdefghij", "klmnopqrst")
    clipped = fit_stage(rows, width=4, height=1)
    assert clipped == ("abcd",)


def test_unknown_sprite() -> None:
    try:
        get_sprite("not-a-sprite")
    except ValueError as exc:
        assert "unknown sprite" in str(exc)
    else:
        raise AssertionError("expected ValueError")


def test_register_sprite_and_animation() -> None:
    extra = register_sprite(Sprite("dot", (".e.",)))
    try:
        assert get_sprite("dot") is extra
        anim = register_animation(Animation("blink", (("idle", 2), ("dot", 1))))
        assert ANIMATIONS["blink"] is anim
    finally:
        SPRITES.pop("dot", None)
        ANIMATIONS.pop("blink", None)
