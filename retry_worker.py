"""
retry_worker.py -- Background worker that processes the call_retries queue.
Runs every 120 seconds, picks up due retries, and triggers re-dials.
"""
import asyncio
import logging

from database import (
    get_pending_retries, update_retry_status,
    get_campaign_by_id, get_campaign_voice_settings,
    is_dnd_number,
)

logger = logging.getLogger("uvicorn.error")

# Default provider from env (same as dial_routes)
import os
DEFAULT_PROVIDER = os.getenv("DEFAULT_PROVIDER", "twilio").lower()


async def retry_worker_loop():
    """Continuously poll for due retries and trigger dials."""
    # Import here to avoid circular imports at module level
    from dial_routes import initiate_call
    from live_logs import emit_campaign_event

    logger.info("[RETRY-WORKER] Started — polling every 120s")

    while True:
        try:
            pending = get_pending_retries()
            if pending:
                logger.info(f"[RETRY-WORKER] Found {len(pending)} due retries")

            for retry in pending:
                retry_id = retry["id"]
                lead_id = retry["lead_id"]
                campaign_id = retry.get("campaign_id")
                attempt = retry["attempt_number"]
                max_attempts = retry["max_attempts"]

                # Skip DND numbers
                org_id = retry.get("org_id")
                if org_id and is_dnd_number(org_id, retry.get("phone", "")):
                    update_retry_status(retry_id, "cancelled", attempt)
                    logger.info(f"[RETRY-WORKER] Cancelled retry {retry_id} for lead {lead_id} — DND number")
                    if campaign_id:
                        emit_campaign_event(
                            campaign_id, retry.get("first_name", "Lead"),
                            retry.get("phone", ""), "dnd_skipped",
                            "DND list — retry cancelled"
                        )
                    continue

                # If this attempt would exceed max, mark exhausted
                if attempt >= max_attempts:
                    update_retry_status(retry_id, "exhausted", attempt)
                    logger.info(f"[RETRY-WORKER] Lead {lead_id} exhausted after {attempt} attempts")
                    if campaign_id:
                        emit_campaign_event(
                            campaign_id, retry.get("first_name", "Lead"),
                            retry.get("phone", ""), "retry_exhausted",
                            f"Exhausted after {attempt} attempts"
                        )
                    continue

                # Mark as dialing
                update_retry_status(retry_id, "dialing", attempt)

                # Build call data
                call_data = {
                    "name": retry.get("first_name", "Customer"),
                    "phone_number": retry.get("phone", ""),
                    "interest": retry.get("interest", "our platform"),
                    "provider": DEFAULT_PROVIDER,
                    "lead_id": lead_id,
                }

                if campaign_id:
                    call_data["campaign_id"] = campaign_id
                    # Pull campaign voice settings
                    campaign = get_campaign_by_id(campaign_id)
                    if campaign:
                        call_data["interest"] = campaign.get("product_name", call_data["interest"])
                        call_data["product_id"] = campaign.get("product_id")
                        voice = get_campaign_voice_settings(campaign_id, campaign.get("org_id"))
                        if voice.get("tts_provider"):
                            call_data["tts_provider"] = voice["tts_provider"]
                        if voice.get("tts_voice_id"):
                            call_data["tts_voice_id"] = voice["tts_voice_id"]
                        if voice.get("tts_language"):
                            call_data["tts_language"] = voice["tts_language"]

                try:
                    logger.info(
                        f"[RETRY-WORKER] Dialing lead {lead_id} "
                        f"(attempt {attempt}/{max_attempts}, phone={retry.get('phone')})"
                    )
                    if campaign_id:
                        emit_campaign_event(
                            campaign_id, retry.get("first_name", "Lead"),
                            retry.get("phone", ""), "retry_dialing",
                            f"Auto-retry attempt {attempt}/{max_attempts}"
                        )
                    await initiate_call(call_data)

                    # After dialing, mark completed (the next retry will be created
                    # by recording_service if this call also fails)
                    update_retry_status(retry_id, "completed", attempt)

                    # Wait 30s between retry dials to avoid Exotel rate limits
                    await asyncio.sleep(30)

                except Exception as dial_err:
                    logger.error(f"[RETRY-WORKER] Dial failed for lead {lead_id}: {dial_err}")
                    # Revert to pending so it gets picked up next cycle
                    update_retry_status(retry_id, "pending", attempt)

        except Exception as e:
            logger.error(f"[RETRY-WORKER] Error in retry loop: {e}")

        await asyncio.sleep(120)
