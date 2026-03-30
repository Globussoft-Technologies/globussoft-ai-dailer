"""
redis_store.py — Redis-backed session store for WebSocket call state.

Migrates serializable call state (pending call info, takeover flags, whisper
queues) to Redis so the system can scale beyond a single process. Non-serializable
state (asyncio.Task, WebSocket connections) stays in-memory per process.

Falls back gracefully to in-memory dicts if Redis is unavailable.
"""

import os
import json
import logging
import redis

logger = logging.getLogger("redis_store")

REDIS_URL = os.getenv("REDIS_URL", "redis://localhost:6379/1")
KEY_PREFIX = "callified:"
CALL_TTL = 3600  # 1 hour TTL for call data (auto-cleanup)

_pool = None


def _get_client() -> redis.Redis | None:
    """Get a Redis client, returning None if unavailable."""
    global _pool
    if _pool is None:
        try:
            _pool = redis.from_url(REDIS_URL, decode_responses=True, socket_connect_timeout=2)
            _pool.ping()
            logger.info("Redis connected: %s", REDIS_URL)
        except Exception as e:
            logger.warning("Redis unavailable, falling back to in-memory: %s", e)
            _pool = False  # sentinel: don't retry every call
            return None
    if _pool is False:
        return None
    return _pool


# ─── Pending Call Info ───────────────────────────────────────────────────────
# Stores pre-call metadata (name, phone, lead_id, interest, exotel_call_sid)

_mem_pending = {}


def set_pending_call(key: str, info: dict):
    r = _get_client()
    if r:
        r.setex(f"{KEY_PREFIX}pending:{key}", CALL_TTL, json.dumps(info))
    else:
        _mem_pending[key] = info


def get_pending_call(key: str) -> dict:
    r = _get_client()
    if r:
        val = r.get(f"{KEY_PREFIX}pending:{key}")
        return json.loads(val) if val else {}
    return _mem_pending.get(key, {})


def delete_pending_call(key: str):
    r = _get_client()
    if r:
        r.delete(f"{KEY_PREFIX}pending:{key}")
    else:
        _mem_pending.pop(key, None)


# ─── Takeover Flags ─────────────────────────────────────────────────────────
# Boolean per stream_sid indicating if a manager has taken over the call

_mem_takeover = {}


def set_takeover(stream_sid: str, active: bool):
    r = _get_client()
    if r:
        r.setex(f"{KEY_PREFIX}takeover:{stream_sid}", CALL_TTL, "1" if active else "0")
    else:
        _mem_takeover[stream_sid] = active


def get_takeover(stream_sid: str) -> bool:
    r = _get_client()
    if r:
        val = r.get(f"{KEY_PREFIX}takeover:{stream_sid}")
        return val == "1"
    return _mem_takeover.get(stream_sid, False)


def delete_takeover(stream_sid: str):
    r = _get_client()
    if r:
        r.delete(f"{KEY_PREFIX}takeover:{stream_sid}")
    else:
        _mem_takeover.pop(stream_sid, None)


# ─── Whisper Queues ──────────────────────────────────────────────────────────
# List of manager whisper messages per stream_sid

_mem_whisper = {}


def push_whisper(stream_sid: str, message: str):
    r = _get_client()
    if r:
        key = f"{KEY_PREFIX}whisper:{stream_sid}"
        r.rpush(key, message)
        r.expire(key, CALL_TTL)
    else:
        _mem_whisper.setdefault(stream_sid, []).append(message)


def pop_all_whispers(stream_sid: str) -> list[str]:
    r = _get_client()
    if r:
        key = f"{KEY_PREFIX}whisper:{stream_sid}"
        pipe = r.pipeline()
        pipe.lrange(key, 0, -1)
        pipe.delete(key)
        results = pipe.execute()
        return results[0] if results[0] else []
    msgs = _mem_whisper.pop(stream_sid, [])
    return msgs


def delete_whispers(stream_sid: str):
    r = _get_client()
    if r:
        r.delete(f"{KEY_PREFIX}whisper:{stream_sid}")
    else:
        _mem_whisper.pop(stream_sid, None)


# ─── Cleanup ─────────────────────────────────────────────────────────────────

def cleanup_call(stream_sid: str):
    """Remove all Redis state for a completed call."""
    delete_takeover(stream_sid)
    delete_whispers(stream_sid)
