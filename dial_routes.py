"""
dial_routes.py — Dial endpoints extracted from main.py.
Handles initiating calls via Twilio and Exotel, single-lead and campaign dialing.
"""
import os
import json
import asyncio
import urllib.parse
import httpx
import base64
from datetime import datetime
from fastapi import APIRouter, BackgroundTasks

import call_logger
import redis_store
from database import get_lead_by_id, update_lead_status, save_call_transcript, is_dnd_number
from billing import get_usage_summary

# ─── Telephony Config ────────────────────────────────────────────────────────

EXOTEL_API_KEY = (os.getenv("EXOTEL_API_KEY") or "").strip()
EXOTEL_API_TOKEN = (os.getenv("EXOTEL_API_TOKEN") or "").strip()
EXOTEL_ACCOUNT_SID = (os.getenv("EXOTEL_ACCOUNT_SID") or "YOUR_EXOTEL_ACCOUNT_SID").strip()
EXOTEL_CALLER_ID = (os.getenv("EXOTEL_CALLER_ID") or "YOUR_EXOTEL_NUMBER").strip()

TWILIO_ACCOUNT_SID = os.getenv("TWILIO_ACCOUNT_SID")
TWILIO_AUTH_TOKEN = os.getenv("TWILIO_AUTH_TOKEN")
TWILIO_PHONE_NUMBER = os.getenv("TWILIO_PHONE_NUMBER")

DEFAULT_PROVIDER = os.getenv("DEFAULT_PROVIDER", "twilio").lower()
PUBLIC_URL = os.getenv("PUBLIC_SERVER_URL", "http://localhost:8000")

dial_router = APIRouter()
last_dial_result = {}

# ─── Core Dial Functions ─────────────────────────────────────────────────────

async def initiate_call(lead: dict):
    provider = lead.get("provider", "twilio")
    phone_clean = lead.get("phone_number", "").lstrip("+")
    pending = {
        "name": lead.get("name", "Customer"),
        "interest": lead.get("interest", "our platform"),
        "phone": phone_clean,
        "lead_id": lead.get("lead_id"),
    }
    if lead.get("campaign_id"):
        pending["campaign_id"] = lead["campaign_id"]
    if lead.get("product_id"):
        pending["product_id"] = lead["product_id"]
    if lead.get("tts_provider"):
        pending["tts_provider"] = lead["tts_provider"]
    if lead.get("tts_voice_id"):
        pending["tts_voice_id"] = lead["tts_voice_id"]
    if lead.get("tts_language"):
        pending["tts_language"] = lead["tts_language"]
    redis_store.set_pending_call("latest", pending)
    # Also store by phone number for concurrent dial support
    redis_store.set_pending_call(f"phone:{phone_clean}", pending)
    # Store by last 10 digits too (for matching)
    if len(phone_clean) > 10:
        redis_store.set_pending_call(f"phone:{phone_clean[-10:]}", pending)
    if provider == "twilio":
        await dial_twilio(lead)
    elif provider == "exotel":
        await dial_exotel(lead)

async def dial_twilio(lead: dict):
    if not TWILIO_ACCOUNT_SID or not TWILIO_AUTH_TOKEN:
        return
    from twilio.rest import Client
    client = Client(TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN)
    twiml_url = f"{PUBLIC_URL}/webhook/twilio?name={urllib.parse.quote(lead['name'])}&interest={urllib.parse.quote(lead['interest'])}&phone={urllib.parse.quote(lead['phone_number'])}"
    try:
        call = client.calls.create(url=twiml_url, to=lead["phone_number"], from_=TWILIO_PHONE_NUMBER,
                                   status_callback=f"{PUBLIC_URL}/webhook/twilio/status",
                                   status_callback_event=['completed', 'no-answer', 'busy', 'failed', 'canceled'])
        print(f"Twilio Call Triggered. SID: {call.sid}")
    except Exception as e:
        print(f"Failed to trigger Twilio call: {e}")

async def dial_exotel(lead: dict):
    import logging
    global last_dial_result
    logger = logging.getLogger("uvicorn.error")
    exotel_app_id = os.getenv("EXOTEL_APP_ID", "1210468")
    exoml_url = f"http://my.exotel.com/exoml/start/{exotel_app_id}"
    phone_clean = lead["phone_number"].strip().lstrip("+")
    if len(phone_clean) == 10 and not phone_clean.startswith("0"):
        phone_clean = "91" + phone_clean
    logger.info(f"Phone normalized: '{lead['phone_number']}' -> '{phone_clean}'")
    url = f"https://api.exotel.com/v1/Accounts/{EXOTEL_ACCOUNT_SID}/Calls/connect.json"
    data = {"From": phone_clean, "CallerId": EXOTEL_CALLER_ID, "Url": exoml_url, "CallType": "trans", "StatusCallback": f"{PUBLIC_URL}/webhook/exotel/status"}
    logger.info(f"[DIAL] Exotel attempt: From={phone_clean}, ExoML={exoml_url}")
    call_logger.call_event(phone_clean, "DIAL_INITIATED", f"From={phone_clean}, Url={exoml_url}")
    last_dial_result = {"timestamp": datetime.now().isoformat(), "phone": phone_clean, "url": url, "exoml": exoml_url, "status": "pending"}
    try:
        creds = f"{EXOTEL_API_KEY}:{EXOTEL_API_TOKEN}"
        auth_b64 = base64.b64encode(creds.encode()).decode()
        headers = {"Content-Type": "application/x-www-form-urlencoded", "Authorization": f"Basic {auth_b64}"}
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.post(url, data=data, headers=headers)
        logger.info(f"[DIAL] Exotel response ({resp.status_code}): {resp.text[:300]}")
        call_logger.call_event(phone_clean, "DIAL_RESPONSE", f"status={resp.status_code}", response=resp.text[:200])
        last_dial_result.update({"status": resp.status_code, "response": resp.text[:500]})

        # Handle DND/NDNC blocked calls
        if resp.status_code == 403 and "NDNC" in resp.text:
            logger.warning(f"[DIAL] DND blocked: {phone_clean}")
            lead_id = lead.get("lead_id")
            if lead_id:
                save_call_transcript(
                    lead_id=lead_id,
                    transcript_json=json.dumps([{"role": "System", "text": "Call blocked — this number is registered on TRAI NDNC (Do Not Call) registry. Exotel cannot connect to DND numbers without compliance approval."}], ensure_ascii=False),
                    recording_url=None,
                    call_duration_s=0,
                    campaign_id=lead.get("campaign_id"),
                )
                update_lead_status(lead_id, "DND Blocked")
                from database import update_lead_note
                update_lead_note(lead_id, "⛔ DND Blocked — This number is on TRAI NDNC (Do Not Call) registry. Exotel cannot dial DND numbers without compliance approval. Submit letterhead + CRM screenshot to Exotel to enable DND calling.")
            return

        try:
            dial_json = resp.json()
            exotel_sid = dial_json.get("Call", {}).get("Sid", "")
            if exotel_sid:
                latest = redis_store.get_pending_call("latest")
                latest["exotel_call_sid"] = exotel_sid
                redis_store.set_pending_call("latest", latest)
                redis_store.set_pending_call(exotel_sid, latest)
                # Update phone-keyed entry too
                redis_store.set_pending_call(f"phone:{phone_clean}", latest)
                if len(phone_clean) > 10:
                    redis_store.set_pending_call(f"phone:{phone_clean[-10:]}", latest)
                logger.info(f"[DIAL] Stored Exotel Call SID mapped: {exotel_sid}")
        except Exception:
            pass
    except Exception as e:
        logger.error(f"[DIAL] Failed to trigger Exotel call: {e}")
        call_logger.call_event(phone_clean, "DIAL_ERROR", str(e))
        last_dial_result.update({"status": "error", "error": str(e)})

# ─── Dial Endpoints ──────────────────────────────────────────────────────────

@dial_router.post("/api/dial/{lead_id}")
async def api_dial_lead(lead_id: int, background_tasks: BackgroundTasks):
    lead = get_lead_by_id(lead_id)
    if not lead:
        return {"status": "error", "message": "Lead not found"}
    # DND check
    org_id = lead.get("org_id")
    if org_id and is_dnd_number(org_id, lead["phone"]):
        return {"status": "error", "message": "This number is on the DND list and cannot be called."}
    # Enforce plan minute limits
    if org_id:
        try:
            usage = get_usage_summary(org_id)
            if usage.get("has_subscription") and usage.get("minutes_remaining", 1) <= 0:
                return {"status": "error", "message": "No minutes remaining. Please upgrade your plan."}
        except Exception:
            pass  # Don't block calls if billing check fails
    background_tasks.add_task(initiate_call, {
        "name": lead["first_name"], "phone_number": lead["phone"],
        "interest": lead.get("interest") or lead["source"],
        "provider": DEFAULT_PROVIDER, "lead_id": lead_id
    })
    return {"status": "success", "message": f"Dialing {lead['first_name']}..."}

@dial_router.post("/api/campaigns/{campaign_id}/dial/{lead_id}")
async def api_campaign_dial_lead(campaign_id: int, lead_id: int, background_tasks: BackgroundTasks):
    from database import get_campaign_by_id, get_campaign_voice_settings
    lead = get_lead_by_id(lead_id)
    campaign = get_campaign_by_id(campaign_id)
    if not lead:
        return {"status": "error", "message": "Lead not found"}
    if not campaign:
        return {"status": "error", "message": "Campaign not found"}
    # DND check
    org_id = campaign.get("org_id")
    if org_id and is_dnd_number(org_id, lead["phone"]):
        return {"status": "error", "message": "This number is on the DND list and cannot be called."}
    # Enforce plan minute limits
    if org_id:
        try:
            usage = get_usage_summary(org_id)
            if usage.get("has_subscription") and usage.get("minutes_remaining", 1) <= 0:
                return {"status": "error", "message": "No minutes remaining. Please upgrade your plan."}
        except Exception:
            pass  # Don't block calls if billing check fails
    voice = get_campaign_voice_settings(campaign_id, org_id)
    call_data = {
        "name": lead["first_name"], "phone_number": lead["phone"],
        "interest": campaign.get("product_name", lead.get("interest", "our platform")),
        "provider": DEFAULT_PROVIDER, "lead_id": lead_id,
        "campaign_id": campaign_id, "product_id": campaign.get("product_id"),
    }
    if voice.get("tts_provider"):
        call_data["tts_provider"] = voice["tts_provider"]
    if voice.get("tts_voice_id"):
        call_data["tts_voice_id"] = voice["tts_voice_id"]
    if voice.get("tts_language"):
        call_data["tts_language"] = voice["tts_language"]
    background_tasks.add_task(initiate_call, call_data)
    return {"status": "success", "message": f"Dialing {lead['first_name']} for campaign '{campaign['name']}'..."}

@dial_router.post("/api/campaigns/{campaign_id}/redial-failed")
async def api_campaign_redial_failed(campaign_id: int, background_tasks: BackgroundTasks):
    """Queue all Call Failed leads in a campaign for sequential redialing with 30s delay."""
    from database import get_campaign_by_id, get_campaign_leads, get_campaign_voice_settings
    import logging
    log = logging.getLogger("uvicorn.error")

    campaign = get_campaign_by_id(campaign_id)
    if not campaign:
        return {"status": "error", "message": "Campaign not found"}
    # Enforce plan minute limits
    org_id = campaign.get("org_id")
    if org_id:
        try:
            usage = get_usage_summary(org_id)
            if usage.get("has_subscription") and usage.get("minutes_remaining", 1) <= 0:
                return {"status": "error", "message": "No minutes remaining. Please upgrade your plan."}
        except Exception:
            pass

    leads = get_campaign_leads(campaign_id)
    failed_leads = [l for l in leads if l.get("status", "").startswith("Call Failed")]
    if not failed_leads:
        return {"status": "error", "message": "No failed leads to redial"}

    voice = get_campaign_voice_settings(campaign_id, campaign.get("org_id"))

    async def _redial_queue():
        from live_logs import emit_campaign_event
        emit_campaign_event(campaign_id, "Campaign", "", "started", f"Redialing {len(failed_leads)} failed leads")
        for i, lead in enumerate(failed_leads):
            if i > 0:
                await asyncio.sleep(30)
            # Skip DND numbers
            if org_id and is_dnd_number(org_id, lead['phone']):
                log.info(f"[REDIAL] Skipping DND number: {lead['phone']}")
                emit_campaign_event(campaign_id, lead['first_name'], lead['phone'], "dnd_skipped", "DND list")
                continue
            log.info(f"[REDIAL] {i+1}/{len(failed_leads)}: Dialing {lead['first_name']} ({lead['phone']})")
            emit_campaign_event(campaign_id, lead['first_name'], lead['phone'], "dialing", f"{i+1}/{len(failed_leads)}")
            call_data = {
                "name": lead["first_name"], "phone_number": lead["phone"],
                "interest": campaign.get("product_name", lead.get("interest", "our platform")),
                "provider": DEFAULT_PROVIDER, "lead_id": lead["id"],
                "campaign_id": campaign_id, "product_id": campaign.get("product_id"),
            }
            if voice.get("tts_provider"):
                call_data["tts_provider"] = voice["tts_provider"]
            if voice.get("tts_voice_id"):
                call_data["tts_voice_id"] = voice["tts_voice_id"]
            if voice.get("tts_language"):
                call_data["tts_language"] = voice["tts_language"]
            try:
                await initiate_call(call_data)
            except Exception as e:
                log.error(f"[REDIAL] Failed for {lead['phone']}: {e}")
                emit_campaign_event(campaign_id, lead['first_name'], lead['phone'], "error", str(e)[:50])
        emit_campaign_event(campaign_id, "Campaign", "", "finished", f"Redial complete: {len(failed_leads)} leads")

    background_tasks.add_task(_redial_queue)
    return {"status": "success", "message": f"Redialing {len(failed_leads)} failed leads (30s gap between calls)"}

@dial_router.post("/api/campaigns/{campaign_id}/dial-all")
async def api_campaign_dial_all(campaign_id: int, background_tasks: BackgroundTasks, force: bool = False):
    """Queue leads in a campaign for sequential dialing. force=true dials ALL regardless of status."""
    from database import get_campaign_by_id, get_campaign_leads, get_campaign_voice_settings
    import logging
    log = logging.getLogger("uvicorn.error")

    campaign = get_campaign_by_id(campaign_id)
    if not campaign:
        return {"status": "error", "message": "Campaign not found"}
    # Enforce plan minute limits
    org_id = campaign.get("org_id")
    if org_id:
        try:
            usage = get_usage_summary(org_id)
            if usage.get("has_subscription") and usage.get("minutes_remaining", 1) <= 0:
                return {"status": "error", "message": "No minutes remaining. Please upgrade your plan."}
        except Exception:
            pass

    leads = get_campaign_leads(campaign_id)
    if force:
        dialable = leads
    else:
        dialable = [l for l in leads if l.get("status", "new") in ("new", "New")]
    if not dialable:
        return {"status": "error", "message": "No leads to dial"}

    voice = get_campaign_voice_settings(campaign_id, campaign.get("org_id"))

    async def _dial_all_queue():
        from live_logs import emit_campaign_event
        emit_campaign_event(campaign_id, "Campaign", "", "started", f"Dialing {len(dialable)} new leads")
        for i, lead in enumerate(dialable):
            if i > 0:
                await asyncio.sleep(30)
            # Skip DND numbers
            if org_id and is_dnd_number(org_id, lead['phone']):
                log.info(f"[DIAL-ALL] Skipping DND number: {lead['phone']}")
                emit_campaign_event(campaign_id, lead['first_name'], lead['phone'], "dnd_skipped", "DND list")
                continue
            log.info(f"[DIAL-ALL] {i+1}/{len(dialable)}: Dialing {lead['first_name']} ({lead['phone']})")
            emit_campaign_event(campaign_id, lead['first_name'], lead['phone'], "dialing", f"{i+1}/{len(dialable)}")
            call_data = {
                "name": lead["first_name"], "phone_number": lead["phone"],
                "interest": campaign.get("product_name", lead.get("interest", "our platform")),
                "provider": DEFAULT_PROVIDER, "lead_id": lead["id"],
                "campaign_id": campaign_id, "product_id": campaign.get("product_id"),
            }
            if voice.get("tts_provider"):
                call_data["tts_provider"] = voice["tts_provider"]
            if voice.get("tts_voice_id"):
                call_data["tts_voice_id"] = voice["tts_voice_id"]
            if voice.get("tts_language"):
                call_data["tts_language"] = voice["tts_language"]
            try:
                await initiate_call(call_data)
            except Exception as e:
                log.error(f"[DIAL-ALL] Failed for {lead['phone']}: {e}")
                emit_campaign_event(campaign_id, lead['first_name'], lead['phone'], "error", str(e)[:50])
        emit_campaign_event(campaign_id, "Campaign", "", "finished", f"Dial complete: {len(dialable)} leads")
        log.info(f"[DIAL-ALL] Campaign {campaign_id} dial-all complete: {len(dialable)} leads")

    background_tasks.add_task(_dial_all_queue)
    return {"status": "success", "message": f"Dialing {len(dialable)} new leads (30s gap between calls)"}
