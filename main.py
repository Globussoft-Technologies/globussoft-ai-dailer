"""
main.py — Callified AI Dialer Application Entry Point.
This is a thin orchestrator that imports and mounts all modules.

Modules:
  - auth.py          → JWT authentication, login, signup
  - routes.py        → All REST API endpoints
  - tts.py           → Text-to-Speech synthesis
  - ws_handler.py    → WebSocket media stream, STT, LLM pipeline
  - live_logs.py     → SSE live log streaming
  - llm_provider.py  → LLM provider (Groq + Gemini fallback)
  - database.py      → Database schema and queries
  - call_logger.py   → Call event logging
"""
import os
import asyncio
import importlib
import inspect
import urllib.parse
import httpx
import base64
from datetime import datetime
from dotenv import load_dotenv
from fastapi import FastAPI, Request, BackgroundTasks, WebSocket
from fastapi.responses import HTMLResponse, JSONResponse, FileResponse
from fastapi.staticfiles import StaticFiles
from fastapi.middleware.cors import CORSMiddleware

import call_logger
from database import init_db, get_lead_by_id, get_active_crm_integrations, update_crm_last_synced, create_lead
from crm_providers import BaseCRM

# ─── App Setup ───────────────────────────────────────────────────────────────

load_dotenv()
call_logger.setup_logging()
app = FastAPI()

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# ─── Import & Mount Modules ─────────────────────────────────────────────────

from auth import auth_router, get_current_user
from routes import api_router, mobile_api, LeadCreate, PunchCreate, LeadStatusUpdate
from live_logs import live_logs_router
from ws_handler import (
    handle_media_stream, sandbox_stream, monitor_call,
    active_tts_tasks, monitor_connections, twilio_websockets,
)
import redis_store

app.include_router(auth_router)
app.include_router(api_router)
app.include_router(live_logs_router)
app.include_router(mobile_api)

# ─── Startup ─────────────────────────────────────────────────────────────────

@app.on_event("startup")
async def on_startup():
    init_db()
    asyncio.create_task(poll_crm_leads())

async def poll_crm_leads():
    while True:
        try:
            active_crms = get_active_crm_integrations()
            for crm in active_crms:
                provider_name = crm["provider"].lower().replace(" ", "").replace("-", "")
                credentials = crm.get("credentials", {})
                crm_client = None
                try:
                    module = importlib.import_module(f"crm_providers.{provider_name}")
                    for name, obj in inspect.getmembers(module, inspect.isclass):
                        if issubclass(obj, BaseCRM) and obj is not BaseCRM:
                            crm_client = obj(**credentials)
                            break
                except Exception as e:
                    print(f"Error loading CRM {provider_name}: {e}")
                if crm_client:
                    new_leads = crm_client.fetch_new_leads()
                    for lead in new_leads:
                        lead["crm_provider"] = provider_name
                        create_lead(lead)
                        crm_client.update_lead_status(lead["external_id"], "In Dialer")
                    update_crm_last_synced(provider_name, datetime.now().isoformat())
        except Exception as e:
            print(f"CRM Polling Error: {e}")
        await asyncio.sleep(60)

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

_app_start_time = __import__('time').time()

# ─── Dial Endpoints ──────────────────────────────────────────────────────────

def send_whatsapp_message(to_phone: str, body: str):
    if not TWILIO_ACCOUNT_SID or not TWILIO_AUTH_TOKEN:
        return
    try:
        from twilio.rest import Client
        client = Client(TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN)
        if not to_phone.startswith("whatsapp:"):
            if not to_phone.startswith("+"):
                to_phone = "+91" + to_phone[-10:]
            to_phone = "whatsapp:" + to_phone
        from_phone = "whatsapp:" + TWILIO_PHONE_NUMBER
        msg = client.messages.create(body=body, from_=from_phone, to=to_phone)
        from database import create_whatsapp_log
        create_whatsapp_log(to_phone, body, "Omnichannel Brochure Trigger")
    except Exception as e:
        print(f"Failed to send whatsapp: {e}")

last_dial_result = {}

async def initiate_call(lead: dict):
    provider = lead.get("provider", "twilio")
    phone_clean = lead.get("phone_number", "").lstrip("+")
    redis_store.set_pending_call("latest", {
        "name": lead.get("name", "Customer"),
        "interest": lead.get("interest", "our platform"),
        "phone": phone_clean,
        "lead_id": lead.get("lead_id")
    })
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
        try:
            dial_json = resp.json()
            exotel_sid = dial_json.get("Call", {}).get("Sid", "")
            if exotel_sid:
                latest = redis_store.get_pending_call("latest")
                latest["exotel_call_sid"] = exotel_sid
                redis_store.set_pending_call("latest", latest)
                redis_store.set_pending_call(exotel_sid, latest)
                logger.info(f"[DIAL] Stored Exotel Call SID mapped: {exotel_sid}")
        except Exception:
            pass
    except Exception as e:
        logger.error(f"[DIAL] Failed to trigger Exotel call: {e}")
        call_logger.call_event(phone_clean, "DIAL_ERROR", str(e))
        last_dial_result.update({"status": "error", "error": str(e)})

@app.post("/api/dial/{lead_id}")
async def api_dial_lead(lead_id: int, background_tasks: BackgroundTasks):
    lead = get_lead_by_id(lead_id)
    if not lead:
        return {"status": "error", "message": "Lead not found"}
    background_tasks.add_task(initiate_call, {
        "name": lead["first_name"], "phone_number": lead["phone"],
        "interest": lead.get("interest") or lead["source"],
        "provider": DEFAULT_PROVIDER, "lead_id": lead_id
    })
    return {"status": "success", "message": f"Dialing {lead['first_name']}..."}

# ─── Debug Endpoints ─────────────────────────────────────────────────────────

@app.get("/api/debug/last-dial")
def debug_last_dial():
    return last_dial_result

@app.get("/api/debug/logs")
def debug_logs(n: int = 100, level: str = "", keyword: str = ""):
    return call_logger.get_logs(n=n, level=level or None, keyword=keyword or None)

@app.get("/api/debug/call-timeline")
def debug_call_timeline(n: int = 5):
    return call_logger.get_timelines(n=n)

@app.get("/api/debug/health")
def debug_health():
    import time
    return {
        "status": "ok", "uptime_s": round(time.time() - _app_start_time, 1),
        "active_calls": len(call_logger._active_timelines),
        "total_logs": len(call_logger._log_buffer),
        "last_dial": last_dial_result.get("status", "none"),
    }

# ─── Webhooks ────────────────────────────────────────────────────────────────

@app.post("/crm-webhook")
async def handle_crm_webhook(request: Request, background_tasks: BackgroundTasks):
    try:
        payload = await request.json()
    except Exception:
        return {"status": "error"}
    if "challenge" in payload:
        return {"challenge": payload["challenge"]}
    lead_data = payload.get("event", {}).get("lead", {})
    phone = lead_data.get("phone")
    if not phone:
        return {"status": "ignored"}
    background_tasks.add_task(initiate_call, {
        "name": lead_data.get("first_name", "Customer"), "phone_number": phone,
        "interest": lead_data.get("source", "our website"),
        "provider": lead_data.get("provider", DEFAULT_PROVIDER).lower()
    })
    return {"status": "success"}

@app.post("/webhook/{provider}")
@app.get("/webhook/{provider}")
async def dynamic_webhook(provider: str, request: Request):
    host = PUBLIC_URL.replace("https://", "").replace("http://", "")
    name = urllib.parse.quote(request.query_params.get("name", ""))
    interest = urllib.parse.quote(request.query_params.get("interest", ""))
    phone = urllib.parse.quote(request.query_params.get("phone", ""))
    ws_url = f"wss://{host}/media-stream?name={name}&interest={interest}&phone={phone}"
    return HTMLResponse(content=f'<Response><Connect><Stream url="{ws_url}" /></Connect></Response>', media_type="application/xml")

@app.post("/webhook/twilio/status")
async def twilio_status_webhook(request: Request):
    form = await request.form()
    status = form.get("CallStatus", "")
    phone = form.get("To", "")
    if status.lower() in ['failed', 'busy', 'no-answer', 'canceled']:
        from database import log_call_status
        log_call_status(phone, status, "Twilio Call Error")
    return {"status": "ok"}

@app.post("/webhook/exotel/status")
async def exotel_status_webhook(request: Request, background_tasks: BackgroundTasks):
    import logging
    log = logging.getLogger("uvicorn.error")
    try:
        form = dict(await request.form())
    except Exception:
        try:
            form = await request.json()
        except Exception:
            form = {}
            
    log.error(f"[RAW EXOTEL STATUS] {form}")
    status = form.get("Status", form.get("CallStatus", ""))
    detailed_status = form.get("DetailedStatus", "")
    phone = form.get("To", "")
    call_sid = form.get("CallSid", form.get("call_sid", ""))
    recording_url = form.get("RecordingUrl", form.get("recording_url", ""))

    terminal_error = None
    if detailed_status.lower() in ['busy', 'no-answer', 'failed', 'canceled', 'dnd']:
        terminal_error = detailed_status
    elif status.lower() in ['failed', 'busy', 'no-answer', 'canceled']:
        terminal_error = status
    if terminal_error:
        from database import log_call_status
        log_call_status(phone, terminal_error, "Exotel Call Error")
        
    if recording_url and call_sid:
        log.error(f"[EXOTEL-WEBHOOK] Status payload contained a RecordingUrl for {call_sid}!")
        background_tasks.add_task(process_recording, recording_url, call_sid, phone)
        
    return {"status": "ok"}

@app.api_route("/exotel/recording-ready", methods=["GET", "POST"])
async def handle_exotel_recording(request: Request, background_tasks: BackgroundTasks):
    if request.method == "POST":
        try:
            form_data = dict(await request.form())
        except Exception:
            try:
                form_data = await request.json()
            except Exception:
                form_data = {}
    else:
        form_data = dict(request.query_params)
        
    recording_url = form_data.get("RecordingUrl", form_data.get("recording_url", ""))
    call_sid = form_data.get("CallSid", form_data.get("call_sid", ""))
    to_phone = form_data.get("To", form_data.get("to_phone", ""))
    
    print(f"[EXOTEL-WEBHOOK] /recording-ready Hit! RecordingUrl={recording_url}, CallSid={call_sid}")
    
    if recording_url and call_sid:
        background_tasks.add_task(process_recording, recording_url, call_sid, to_phone)
    return {"status": "success"}

async def process_recording(recording_url: str, call_sid: str, phone: str):
    import os
    import time
    import logging
    from database import get_conn
    log = logging.getLogger("uvicorn.error")

    log.error(f"Downloading recording for {call_sid} from {recording_url}")
    async with httpx.AsyncClient(timeout=60.0, follow_redirects=True) as client:
        try:
            # Exotel recordings typically don't require API auth on direct links, but follow redirects
            resp = await client.get(recording_url)
            audio_bytes = resp.content
            
            # Save file physically
            os.makedirs("/home/empcloud-development/callified-ai/recordings", exist_ok=True)
            mp3_filename = f"call_{call_sid}_{int(time.time() * 1000)}.mp3"
            mp3_path = os.path.join("/home/empcloud-development/callified-ai/recordings", mp3_filename)
            with open(mp3_path, "wb") as f:
                f.write(audio_bytes)
                
            public_audio_url = f"/api/recordings/{mp3_filename}"
            log.error(f"[WEBHOOK SAVED] Successfully wrote {len(audio_bytes)} bytes to {mp3_path}")
            
            # Update Database!
            conn = get_conn()
            cursor = conn.cursor()
            cursor.execute('''
                UPDATE call_transcripts 
                SET recording_url = %s
                WHERE id = (
                    SELECT t.id FROM (
                        SELECT ct.id FROM call_transcripts ct
                        JOIN leads l ON ct.lead_id = l.id
                        WHERE l.phone LIKE %s
                        ORDER BY ct.created_at DESC LIMIT 1
                    ) as t
                )
            ''', (public_audio_url, f"%{phone[-10:]}%" if len(phone) >= 10 else f"%{phone}%"))
            conn.commit()
            log.error(f"[WEBHOOK DB SYNC] Attached {public_audio_url} to phone {phone}")
            
        except Exception as e:
            log.error(f"Failed to download recording: {e}")
            import traceback
            log.error(traceback.format_exc())
            return
            
    try:
        llm = genai.Client(api_key=os.getenv("GEMINI_API_KEY", "dummy"))
        reply = await llm.aio.models.generate_content(
            model="gemini-2.5-flash", contents=transcript,
            config=types.GenerateContentConfig(system_instruction="You are a professional AI assistant. Analyze the sales call transcript and produce a structured Follow-Up Note.")
        )
        summary = reply.text
    except Exception as e:
        print("Summarization failed:", e)
        return
    from database import update_call_note, get_conn
    update_call_note(call_sid, summary, phone)


# ─── WebSocket Endpoints ─────────────────────────────────────────────────────

@app.websocket("/media-stream")
async def ws_media_stream(websocket: WebSocket):
    # handle_media_stream already calls websocket.accept()
    await handle_media_stream(websocket)

@app.websocket("/ws/sandbox")
async def ws_sandbox(websocket: WebSocket):
    await sandbox_stream(websocket)

@app.websocket("/ws/monitor/{stream_sid}")
async def ws_monitor(websocket: WebSocket, stream_sid: str):
    await monitor_call(websocket, stream_sid)

# ─── Static Files (SPA) ─────────────────────────────────────────────────────

_dist_dir = os.path.join(os.path.dirname(__file__), "frontend", "dist")
_assets_dir = os.path.join(_dist_dir, "assets")
if os.path.isdir(_assets_dir):
    app.mount("/assets", StaticFiles(directory=_assets_dir), name="static-assets")

@app.get("/{full_path:path}")
async def serve_spa(full_path: str):
    file_path = os.path.join(_dist_dir, full_path)
    if full_path and os.path.isfile(file_path):
        return FileResponse(file_path)
    index = os.path.join(_dist_dir, "index.html")
    if os.path.isfile(index):
        return FileResponse(index)
    return JSONResponse({"detail": "Frontend not built"}, status_code=404)
