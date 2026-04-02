"""
main.py — Callified AI Dialer Application Entry Point.
This is a thin orchestrator that imports and mounts all modules.

Modules:
  - auth.py          → JWT authentication, login, signup
  - routes.py        → All REST API endpoints
  - dial_routes.py   → Dial endpoints (Twilio, Exotel, campaign dialing)
  - webhook_routes.py→ Webhook handlers (CRM, Twilio/Exotel status, recordings)
  - tts.py           → Text-to-Speech synthesis
  - ws_handler.py    → WebSocket media stream, STT, LLM pipeline
  - live_logs.py     → SSE live log streaming
  - llm_provider.py  → LLM provider (Groq + Gemini fallback)
  - database.py      → Database schema and queries
  - call_logger.py   → Call event logging
"""
import os
import json
import asyncio
import importlib
import inspect
from datetime import datetime
from dotenv import load_dotenv
from fastapi import FastAPI, Request, WebSocket
from fastapi.responses import HTMLResponse, JSONResponse, FileResponse
from fastapi.staticfiles import StaticFiles
from fastapi.middleware.cors import CORSMiddleware

import call_logger
from database import init_db, get_active_crm_integrations, update_crm_last_synced, create_lead
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
from dial_routes import dial_router, last_dial_result
from webhook_routes import webhook_router
from wa_routes import wa_router

app.include_router(auth_router)
app.include_router(api_router)
app.include_router(live_logs_router)
app.include_router(mobile_api)
app.include_router(dial_router)
app.include_router(webhook_router)
app.include_router(wa_router)

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

# ─── Telephony Config (kept for send_whatsapp_message and debug endpoints) ──

TWILIO_ACCOUNT_SID = os.getenv("TWILIO_ACCOUNT_SID")
TWILIO_AUTH_TOKEN = os.getenv("TWILIO_AUTH_TOKEN")
TWILIO_PHONE_NUMBER = os.getenv("TWILIO_PHONE_NUMBER")

_app_start_time = __import__('time').time()

# ─── WhatsApp Helper ────────────────────────────────────────────────────────

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
