"""Vendor-neutral agent event feed for the live view."""

from rex.watch.bus import EventTail, append_event, default_event_path
from rex.watch.events import RexEvent

__all__ = ["EventTail", "RexEvent", "append_event", "default_event_path"]
