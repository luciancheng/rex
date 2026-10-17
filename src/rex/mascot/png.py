"""Minimal PNG decoder for sprite import (stdlib only)."""

from __future__ import annotations

import struct
import zlib
from collections.abc import Sequence
from dataclasses import dataclass


@dataclass(frozen=True)
class PngImage:
    width: int
    height: int
    pixels: tuple[tuple[tuple[int, int, int, int], ...], ...]  # rows of RGBA


def load_png(path: str) -> PngImage:
    data = open(path, "rb").read()
    if data[:8] != b"\x89PNG\r\n\x1a\n":
        raise ValueError(f"{path} is not a PNG")
    width = height = bit_depth = color_type = None
    idat = b""
    palette: list[tuple[int, int, int]] = []
    alpha_by_index: list[int] = []
    i = 8
    while i < len(data):
        length = struct.unpack(">I", data[i : i + 4])[0]
        tag = data[i + 4 : i + 8]
        chunk = data[i + 8 : i + 8 + length]
        if tag == b"IHDR":
            width, height, bit_depth, color_type, *_ = struct.unpack(">IIBBBBB", chunk)
        elif tag == b"PLTE":
            palette = [tuple(chunk[n : n + 3]) for n in range(0, len(chunk), 3)]  # type: ignore[misc]
        elif tag == b"tRNS":
            alpha_by_index = list(chunk)
        elif tag == b"IDAT":
            idat += chunk
        elif tag == b"IEND":
            break
        i += 12 + length
    if width is None or height is None or bit_depth is None or color_type is None:
        raise ValueError(f"{path} is missing IHDR")
    raw = zlib.decompress(idat)
    channels = {0: 1, 2: 3, 3: 1, 4: 2, 6: 4}[color_type]
    row_bytes = (width * bit_depth * channels + 7) // 8
    filter_bpp = max(1, (bit_depth * channels + 7) // 8)
    rows: list[bytes] = []
    prev = bytearray(row_bytes)
    offset = 0
    for _ in range(height):
        filter_type = raw[offset]
        scan = bytearray(raw[offset + 1 : offset + 1 + row_bytes])
        offset += 1 + row_bytes
        _paeth_unfilter(filter_type, scan, prev, filter_bpp)
        rows.append(bytes(scan))
        prev = scan
    pixels = _to_rgba(
        rows,
        width=width,
        height=height,
        bit_depth=bit_depth,
        color_type=color_type,
        palette=palette,
        alpha_by_index=alpha_by_index,
    )
    return PngImage(width=width, height=height, pixels=pixels)


def _paeth_unfilter(filter_type: int, scan: bytearray, prev: Sequence[int], bpp: int) -> None:
    if filter_type == 0:
        return
    for x, value in enumerate(scan):
        left = scan[x - bpp] if x >= bpp else 0
        up = prev[x]
        up_left = prev[x - bpp] if x >= bpp else 0
        if filter_type == 1:
            recon = left
        elif filter_type == 2:
            recon = up
        elif filter_type == 3:
            recon = (left + up) // 2
        elif filter_type == 4:
            estimate = left + up - up_left
            distances = (abs(estimate - left), abs(estimate - up), abs(estimate - up_left))
            recon = (left, up, up_left)[distances.index(min(distances))]
        else:
            raise ValueError(f"unsupported PNG filter {filter_type}")
        scan[x] = (value + recon) & 255


def _to_rgba(
    rows: Sequence[bytes],
    *,
    width: int,
    height: int,
    bit_depth: int,
    color_type: int,
    palette: Sequence[tuple[int, int, int]],
    alpha_by_index: Sequence[int],
) -> tuple[tuple[tuple[int, int, int, int], ...], ...]:
    out: list[tuple[tuple[int, int, int, int], ...]] = []
    for y in range(height):
        samples = _unpack_samples(rows[y], width=width, bit_depth=bit_depth, channels=_channels(color_type))
        row: list[tuple[int, int, int, int]] = []
        if color_type == 3:
            for index in samples:
                r, g, b = palette[index]
                a = alpha_by_index[index] if index < len(alpha_by_index) else 255
                row.append((r, g, b, a))
        elif color_type == 2:
            for i in range(0, len(samples), 3):
                row.append((samples[i], samples[i + 1], samples[i + 2], 255))
        elif color_type == 6:
            for i in range(0, len(samples), 4):
                row.append((samples[i], samples[i + 1], samples[i + 2], samples[i + 3]))
        else:
            raise ValueError(f"unsupported PNG color type {color_type}")
        out.append(tuple(row))
    return tuple(out)


def _channels(color_type: int) -> int:
    return {0: 1, 2: 3, 3: 1, 4: 2, 6: 4}[color_type]


def _unpack_samples(scan: bytes, *, width: int, bit_depth: int, channels: int) -> list[int]:
    if bit_depth == 8:
        return list(scan[: width * channels])
    if bit_depth not in {1, 2, 4}:
        raise ValueError(f"unsupported bit depth {bit_depth}")
    values: list[int] = []
    mask = (1 << bit_depth) - 1
    per_byte = 8 // bit_depth
    for byte in scan:
        for shift in range(per_byte - 1, -1, -1):
            values.append((byte >> (shift * bit_depth)) & mask)
            if len(values) == width * channels:
                return values
    return values[: width * channels]
