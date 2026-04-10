"""
scheduler.py — Background task that polls for scheduled calls and triggers dials.
Runs every 60 seconds, picks up pending calls whose scheduled_time has passed,
and initiates them via the existing dial system.
"""
import asyncio
import logging
from database import get_pending_scheduled_calls, update_scheduled_call_status, get_campaign_by_id, get_campaign_voice_settings
from dial_routes import initiate_call, DEFAULT_PROVIDER
from call_guard import is_calling_allowed, get_org_timezone

logger = logging.getLogger("uvicorn.error")


async def run_scheduler():
    """Infinite loop that checks for due scheduled calls every 60 seconds."""
    while True:
        try:
            pending = get_pending_scheduled_calls()
            # Check TRAI calling hours before processing any calls
            for sc in pending:
                tz = get_org_timezone(sc.get("org_id"))
                guard = is_calling_allowed(tz)
                if not guard["allowed"]:
                    logger.info(f"[SCHEDULER] Scheduled call {sc['id']} deferred — outside calling hours ({guard['current_time']} {tz})")
                    continue
                try:
                    update_scheduled_call_status(sc["id"], "dialing")
                    call_data = {
                        "name": sc["first_name"],
                        "phone_number": sc["phone"],
                        "interest": sc.get("interest") or sc.get("source", "our platform"),
                        "provider": DEFAULT_PROVIDER,
                        "lead_id": sc["lead_id"],
                    }
                    # If tied to a campaign, pull campaign voice settings
                    if sc.get("campaign_id"):
                        call_data["campaign_id"] = sc["campaign_id"]
                        campaign = get_campaign_by_id(sc["campaign_id"])
                        if campaign:
                            call_data["product_id"] = campaign.get("product_id")
                            call_data["interest"] = campaign.get("product_name", call_data["interest"])
                            voice = get_campaign_voice_settings(sc["campaign_id"], sc.get("org_id"))
                            if voice.get("tts_provider"):
                                call_data["tts_provider"] = voice["tts_provider"]
                            if voice.get("tts_voice_id"):
                                call_data["tts_voice_id"] = voice["tts_voice_id"]
                            if voice.get("tts_language"):
                                call_data["tts_language"] = voice["tts_language"]

                    await initiate_call(call_data)
                    update_scheduled_call_status(sc["id"], "completed")
                    logger.info(f"[SCHEDULER] Dialed scheduled call {sc['id']} -> {sc['phone']}")
                except Exception as e:
                    logger.error(f"[SCHEDULER] Failed scheduled call {sc['id']}: {e}")
                    update_scheduled_call_status(sc["id"], "failed")
        except Exception as e:
            logger.error(f"[SCHEDULER] Polling error: {e}")
        await asyncio.sleep(60)
