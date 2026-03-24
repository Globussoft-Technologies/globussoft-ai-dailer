"""
Structured logging and call timeline tracking for Globussoft AI Dialer.

Provides:
- JSON-structured in-memory ring buffer (last 500 log entries)
- Call timeline tracking (per-call event timestamps)
- HTTP-accessible via /api/debug/* endpoints
"""

import logging
import json
import time
from collections import deque
from datetime import datetime
from typing import Optional


# ─── Ring Buffer Log Handler ─────────────────────────────────────────────────

MAX_LOG_ENTRIES = 500

_log_buffer: deque = deque(maxlen=MAX_LOG_ENTRIES)


class RingBufferHandler(logging.Handler):
    """Stores structured log entries in a fixed-size ring buffer."""

    def emit(self, record: logging.LogRecord):
        entry = {
            "ts": datetime.fromtimestamp(record.created).isoformat(),
            "level": record.levelname,
            "logger": record.name,
            "msg": record.getMessage(),
            "module": record.module,
            "func": record.funcName,
            "line": record.lineno,
        }
        if record.exc_info and record.exc_info[1]:
            entry["error"] = str(record.exc_info[1])
        _log_buffer.append(entry)


def get_logs(n: int = 100, level: Optional[str] = None, keyword: Optional[str] = None) -> list:
    """Return last N log entries, optionally filtered by level or keyword."""
    logs = list(_log_buffer)

    if level:
        level = level.upper()
        logs = [e for e in logs if e["level"] == level]

    if keyword:
        kw = keyword.lower()
        logs = [e for e in logs if kw in e["msg"].lower()]

    return logs[-n:]


# ─── Call Timeline Tracker ───────────────────────────────────────────────────

MAX_TIMELINES = 20

_call_timelines: deque = deque(maxlen=MAX_TIMELINES)
_active_timelines: dict = {}  # stream_sid -> timeline dict


def call_event(stream_sid: str, event: str, detail: str = "", **extra):
    """Record a timestamped event for a call."""
    now = time.time()
    entry = {
        "event": event,
        "detail": detail[:200],
        "ts": datetime.fromtimestamp(now).isoformat(),
        "elapsed_s": 0.0,
        **extra,
    }

    if stream_sid not in _active_timelines:
        _active_timelines[stream_sid] = {
            "stream_sid": stream_sid,
            "started": now,
            "events": [],
        }

    timeline = _active_timelines[stream_sid]
    entry["elapsed_s"] = round(now - timeline["started"], 3)
    timeline["events"].append(entry)


def end_call(stream_sid: str):
    """Finalize a call timeline and move it to the completed list."""
    if stream_sid in _active_timelines:
        timeline = _active_timelines.pop(stream_sid)
        timeline["ended"] = datetime.now().isoformat()
        timeline["duration_s"] = round(time.time() - timeline["started"], 2)
        timeline["started"] = datetime.fromtimestamp(timeline["started"]).isoformat()
        _call_timelines.append(timeline)


def get_timelines(n: int = 5) -> list:
    """Return last N call timelines (completed + active)."""
    completed = list(_call_timelines)[-n:]
    active = []
    for sid, tl in _active_timelines.items():
        active.append({
            **tl,
            "started": datetime.fromtimestamp(tl["started"]).isoformat(),
            "status": "active",
            "duration_s": round(time.time() - tl["started"], 2),
        })
    return active + completed


# ─── Setup ───────────────────────────────────────────────────────────────────

def setup_logging():
    """Install ring buffer handler on the root and uvicorn loggers."""
    handler = RingBufferHandler()
    handler.setLevel(logging.DEBUG)

    # Attach to uvicorn error logger (where our app logs go)
    for logger_name in ["uvicorn.error", "uvicorn.access", "root"]:
        logger = logging.getLogger(logger_name)
        logger.addHandler(handler)

    # Also attach to root logger as fallback
    root = logging.getLogger()
    root.addHandler(handler)
    root.setLevel(logging.INFO)
