"""
call_guard.py — TRAI calling-hours enforcement.
Indian telecom regulations prohibit calls before 9 AM or after 9 PM.
"""
from datetime import datetime
from zoneinfo import ZoneInfo

CALL_START_HOUR = 9   # 9:00 AM
CALL_END_HOUR = 21    # 9:00 PM


def is_calling_allowed(timezone: str = "Asia/Kolkata") -> dict:
    """Check if calling is allowed right now based on TRAI rules.
    Returns {allowed: bool, reason: str, current_hour: int}
    """
    try:
        tz = ZoneInfo(timezone)
    except Exception:
        tz = ZoneInfo("Asia/Kolkata")

    now = datetime.now(tz)
    hour = now.hour

    if hour < CALL_START_HOUR:
        return {
            "allowed": False,
            "reason": f"Too early — calls allowed only after 9:00 AM. Current time: {now.strftime('%I:%M %p')}",
            "current_hour": hour,
            "current_time": now.strftime("%I:%M %p"),
            "timezone": timezone,
        }
    elif hour >= CALL_END_HOUR:
        return {
            "allowed": False,
            "reason": f"Too late — calls allowed only until 9:00 PM. Current time: {now.strftime('%I:%M %p')}",
            "current_hour": hour,
            "current_time": now.strftime("%I:%M %p"),
            "timezone": timezone,
        }
    else:
        return {
            "allowed": True,
            "reason": "Calling hours active (9 AM - 9 PM)",
            "current_hour": hour,
            "current_time": now.strftime("%I:%M %p"),
            "timezone": timezone,
        }


def get_next_allowed_time(timezone: str = "Asia/Kolkata") -> str:
    """If calling not allowed now, return when it will be allowed."""
    try:
        tz = ZoneInfo(timezone)
    except Exception:
        tz = ZoneInfo("Asia/Kolkata")

    now = datetime.now(tz)
    hour = now.hour

    if hour < CALL_START_HOUR:
        return "today at 9:00 AM"
    elif hour >= CALL_END_HOUR:
        return "tomorrow at 9:00 AM"
    else:
        return "now (calling is currently allowed)"


def get_org_timezone(org_id: int) -> str:
    """Fetch the timezone for an organization from the database."""
    if not org_id:
        return "Asia/Kolkata"
    try:
        from database import get_conn
        conn = get_conn()
        cursor = conn.cursor()
        cursor.execute("SELECT timezone FROM organizations WHERE id = %s", (org_id,))
        row = cursor.fetchone()
        conn.close()
        if row and row.get("timezone"):
            return row["timezone"]
    except Exception:
        pass
    return "Asia/Kolkata"
