"""
webhook_dispatch.py — Async webhook dispatcher for Callified AI Dialer.
Fires HTTP POST to registered webhook URLs when events occur.
"""
import hmac
import hashlib
import json
import logging
from datetime import datetime, timezone

import httpx

from database import get_active_webhooks_for_event, log_webhook_delivery

logger = logging.getLogger("uvicorn.error")


async def dispatch_webhook(org_id: int, event: str, data: dict):
    """Fire webhooks for an event. Called from anywhere in the app."""
    try:
        webhooks = get_active_webhooks_for_event(org_id, event)
    except Exception as e:
        logger.error(f"[WEBHOOK] Failed to fetch webhooks for org={org_id} event={event}: {e}")
        return

    if not webhooks:
        return

    payload = {
        "event": event,
        "data": data,
        "timestamp": datetime.now(timezone.utc).isoformat(),
    }
    payload_bytes = json.dumps(payload, ensure_ascii=False).encode("utf-8")

    for wh in webhooks:
        try:
            headers = {"Content-Type": "application/json"}

            # HMAC-SHA256 signature if secret is configured
            if wh.get("secret"):
                signature = hmac.new(
                    wh["secret"].encode("utf-8"),
                    payload_bytes,
                    hashlib.sha256,
                ).hexdigest()
                headers["X-Callified-Signature"] = signature

            async with httpx.AsyncClient(timeout=10.0) as client:
                resp = await client.post(wh["url"], content=payload_bytes, headers=headers)

            log_webhook_delivery(
                webhook_id=wh["id"],
                event=event,
                payload=payload,
                response_status=resp.status_code,
                response_body=resp.text[:2000] if resp.text else "",
            )
            logger.info(f"[WEBHOOK] Delivered {event} to {wh['url']} -> {resp.status_code}")

        except Exception as e:
            # Log the failure but never crash the caller
            try:
                log_webhook_delivery(
                    webhook_id=wh["id"],
                    event=event,
                    payload=payload,
                    response_status=0,
                    response_body=str(e)[:2000],
                )
            except Exception:
                pass
            logger.error(f"[WEBHOOK] Failed to deliver {event} to {wh['url']}: {e}")
