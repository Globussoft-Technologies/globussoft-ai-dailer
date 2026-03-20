import os
import json
import base64
import urllib.parse
import asyncio
import httpx
from dotenv import load_dotenv
from fastapi import FastAPI, BackgroundTasks, Request, Body, Header, HTTPException, WebSocket
from fastapi.responses import HTMLResponse, StreamingResponse
from twilio.rest import Client
from deepgram import DeepgramClient, LiveTranscriptionEvents, LiveOptions
from google import genai
from google.genai import types
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel
import io
import csv
import math
from database import init_db, get_all_leads, get_lead_by_id, create_lead, get_all_sites, create_punch, get_site_by_id
from database import update_lead_status, get_all_tasks, complete_task, get_reports, get_all_whatsapp_logs
from database import upload_document, get_documents_by_lead, get_analytics, search_leads, update_lead_note
from database import get_active_crm_integrations, update_crm_last_synced
import importlib
import inspect
from crm_providers import BaseCRM
from datetime import datetime

load_dotenv()
app = FastAPI()

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

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
                        # Mark them as pulled so we don't fetch them again endlessly
                        crm_client.update_lead_status(lead["external_id"], "In Dialer")
                    
                    update_crm_last_synced(provider_name, datetime.now().isoformat())
        except Exception as e:
            print(f"CRM Polling Error: {e}")
            
        await asyncio.sleep(60) # Poll every 60 seconds

EXOTEL_API_KEY = os.getenv("EXOTEL_API_KEY")
EXOTEL_API_TOKEN = os.getenv("EXOTEL_API_TOKEN")
EXOTEL_ACCOUNT_SID = os.getenv("EXOTEL_ACCOUNT_SID", "YOUR_EXOTEL_ACCOUNT_SID")
EXOTEL_CALLER_ID = os.getenv("EXOTEL_CALLER_ID", "YOUR_EXOTEL_NUMBER")

TWILIO_ACCOUNT_SID = os.getenv("TWILIO_ACCOUNT_SID")
TWILIO_AUTH_TOKEN = os.getenv("TWILIO_AUTH_TOKEN")
TWILIO_PHONE_NUMBER = os.getenv("TWILIO_PHONE_NUMBER")

DEFAULT_PROVIDER = os.getenv("DEFAULT_PROVIDER", "twilio").lower()

# SDK Clients will be initialized per-request to prevent startup crashes if keys are missing
dg_client = None
llm_client = None

PUBLIC_URL = os.getenv("PUBLIC_SERVER_URL", "http://localhost:8000")
active_tts_tasks = {}

class LeadCreate(BaseModel):
    first_name: str
    last_name: str = ""
    phone: str
    source: str = "Dashboard"

class PunchCreate(BaseModel):
    agent_name: str
    site_id: int
    lat: float
    lon: float

class LeadStatusUpdate(BaseModel):
    status: str

class NoteCreate(BaseModel):
    note: str

class DocumentCreate(BaseModel):
    file_name: str
    file_url: str

class CRMIntegrationCreate(BaseModel):
    provider: str
    api_key: str
    base_url: str = ""

def haversine_distance(lat1, lon1, lat2, lon2):
    R = 6371e3 # Earth radius in meters
    phi1 = lat1 * math.pi/180
    phi2 = lat2 * math.pi/180
    delta_phi = (lat2-lat1) * math.pi/180
    delta_lambda = (lon2-lon1) * math.pi/180
    a = math.sin(delta_phi/2) * math.sin(delta_phi/2) + \
        math.cos(phi1) * math.cos(phi2) * \
        math.sin(delta_lambda/2) * math.sin(delta_lambda/2)
    c = 2 * math.atan2(math.sqrt(a), math.sqrt(1-a))
    return R * c

@app.get("/api/leads")
def api_get_leads():
    return get_all_leads()

@app.get("/api/leads/export")
def api_export_leads():
    leads = get_all_leads()
    stream = io.StringIO()
    writer = csv.writer(stream)
    
    # Write Header
    writer.writerow(["ID", "First Name", "Last Name", "Phone", "Status", "Source", "Follow Up Note", "Created At"])
    
    # Write Rows
    for lead in leads:
        note = lead.get('follow_up_note', '')
        if note:
            note = note.replace('\n', ' ') # Clean newlines from CSV integrity
        writer.writerow([
            lead.get('id', ''),
            lead.get('first_name', ''),
            lead.get('last_name', ''),
            lead.get('phone', ''),
            lead.get('status', ''),
            lead.get('source', ''),
            note,
            lead.get('created_at', '')
        ])
        
    response = StreamingResponse(iter([stream.getvalue()]), media_type="text/csv")
    response.headers["Content-Disposition"] = "attachment; filename=bdrpl_leads_export.csv"
    return response

@app.get("/api/leads/search")
def api_search_leads(q: str = ""):
    if not q:
        return get_all_leads()
    return search_leads(q)

@app.post("/api/leads")
def api_create_lead(lead: LeadCreate):
    try:
        lead_id = create_lead(lead.dict())
        return {"status": "success", "id": lead_id}
    except Exception as e:
        return {"status": "error", "message": str(e)}

@app.post("/api/dial/{lead_id}")
async def api_dial_lead(lead_id: int, background_tasks: BackgroundTasks):
    lead = get_lead_by_id(lead_id)
    if not lead:
        return {"status": "error", "message": "Lead not found"}
    
    background_tasks.add_task(initiate_call, {
        "name": lead["first_name"],
        "phone_number": lead["phone"],
        "interest": lead["source"],
        "provider": DEFAULT_PROVIDER
    })
    return {"status": "success", "message": f"Dialing {lead['first_name']}..."}

@app.get("/api/sites")
def api_get_sites():
    return get_all_sites()

@app.post("/api/punch")
def api_punch(punch: PunchCreate):
    site = get_site_by_id(punch.site_id)
    if not site:
        return {"status": "error", "message": "Invalid site."}
    
    distance_m = haversine_distance(punch.lat, punch.lon, site["lat"], site["lon"])
    
    if distance_m <= 500:
        punch_status = "Valid"
    else:
        punch_status = "Out of Bounds"
        
    create_punch(punch.agent_name, punch.site_id, punch.lat, punch.lon, punch_status)
    return {
        "status": "success", 
        "punch_status": punch_status,
        "distance_m": round(distance_m, 2),
        "site_name": site["name"]
    }

@app.put("/api/leads/{lead_id}/status")
def api_update_lead_status(lead_id: int, payload: LeadStatusUpdate):
    update_lead_status(lead_id, payload.status)
    return {"status": "success", "message": f"Lead {lead_id} updated to {payload.status}"}

@app.post("/api/leads/{lead_id}/notes")
def api_update_lead_note(lead_id: int, payload: NoteCreate):
    update_lead_note(lead_id, payload.note)
    return {"status": "success"}

@app.get("/api/leads/{lead_id}/draft-email")
def api_draft_email(lead_id: int):
    lead = get_lead_by_id(lead_id)
    if not lead:
        raise HTTPException(status_code=404, detail="Lead not found")
        
    note = lead.get("follow_up_note")
    if not note: 
        note = "Interested in exploring the latest property listings."
    
    prompt = f"""
    You are an expert Real Estate Consultant at BDRPL. 
    The client's name is {lead.get('first_name', 'Client')} {lead.get('last_name', '')}.
    Your latest timeline note says: "{note}".
    
    Draft a highly professional, persuasive 3-sentence follow-up email based on this note.
    Return ONLY a JSON object with strictly these keys: "subject", "body". Do not wrap in markdown blocks, just return exact JSON.
    """
    
    import google.generativeai as genai
    genai.configure(api_key=os.getenv("GEMINI_API_KEY"))
    model = genai.GenerativeModel("gemini-1.5-flash")
    
    try:
        response = model.generate_content(prompt)
        text = response.text.replace("```json", "").replace("```", "").strip()
        import json
        return json.loads(text)
    except Exception as e:
        return {
            "subject": f"BDRPL Properties - Following up with {lead.get('first_name')}", 
            "body": f"Hi {lead.get('first_name')},\n\nI wanted to follow up regarding our recent conversation. Please let me know when you have a moment to discuss further.\n\nBest regards,\nBDRPL Team"
        }

@app.get("/api/tasks")
def api_get_tasks():
    return get_all_tasks()

@app.put("/api/tasks/{task_id}/complete")
def api_complete_task(task_id: int):
    complete_task(task_id)
    return {"status": "success"}

@app.get("/api/reports")
def api_get_reports():
    return get_reports()

@app.get("/api/whatsapp")
def api_get_whatsapp():
    return get_all_whatsapp_logs()

@app.post("/api/leads/{lead_id}/documents")
def api_upload_document(lead_id: int, payload: DocumentCreate):
    upload_document(lead_id, payload.file_name, payload.file_url)
    return {"status": "success", "message": f"{payload.file_name} uploaded successfully."}

@app.get("/api/leads/{lead_id}/documents")
def api_get_documents(lead_id: int):
    return get_documents_by_lead(lead_id)

@app.get("/api/analytics")
def api_get_analytics():
    return get_analytics()

@app.get("/api/integrations")
def api_get_integrations():
    active = get_active_crm_integrations()
    # Mask API keys for frontend security
    for a in active:
        if a["api_key"] and len(a["api_key"]) > 8:
            a["api_key"] = a["api_key"][:4] + "****" + a["api_key"][-4:]
        elif a["api_key"]:
            a["api_key"] = "****"
    return active

from database import save_crm_integration

@app.post("/api/integrations")
async def create_integration(data: dict):
    provider = data.get("provider")
    credentials = data.get("credentials")
    
    if not provider or not credentials:
        return JSONResponse(status_code=400, content={"error": "provider and credentials are required"})
        
    try:
        # Save integration safely
        from database import save_crm_integration
        save_crm_integration(provider, credentials)
        return {"status": "success"}
    except Exception as e:
        print(f"Error saving integration: {e}")
        return JSONResponse(status_code=500, content={"error": str(e)})

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

    background_tasks.add_task(
        initiate_call,
        {
            "name": lead_data.get("first_name", "Customer"),
            "phone_number": phone,
            "interest": lead_data.get("source", "our website"),
            "provider": lead_data.get("provider", DEFAULT_PROVIDER).lower()
        },
    )
    return {"status": "success"}


async def initiate_call(lead: dict):
    provider = lead.get("provider", "twilio")
    if provider == "twilio":
        await dial_twilio(lead)
    elif provider == "exotel":
        await dial_exotel(lead)
    else:
        print(f"Unknown provider: {provider}")

async def dial_twilio(lead: dict):
    if not TWILIO_ACCOUNT_SID or not TWILIO_AUTH_TOKEN:
        print("Twilio credentials missing.")
        return
    client = Client(TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN)
    twiml_url = (
        f"{PUBLIC_URL}/webhook/twilio"
        f"?name={urllib.parse.quote(lead['name'])}"
        f"&interest={urllib.parse.quote(lead['interest'])}"
    )
    try:
        call = client.calls.create(
            url=twiml_url, to=lead["phone_number"], from_=TWILIO_PHONE_NUMBER
        )
        print(f"Twilio Call Triggered. SID: {call.sid}")
    except Exception as e:
        print(f"Failed to trigger Twilio call: {e}")

async def dial_exotel(lead: dict):
    exoml_url = (
        f"{PUBLIC_URL}/webhook/exotel"
        f"?name={urllib.parse.quote(lead['name'])}"
        f"&interest={urllib.parse.quote(lead['interest'])}"
    )
    url = f"https://api.exotel.com/v1/Accounts/{EXOTEL_ACCOUNT_SID}/Calls/connect.json"
    data = {
        "From": lead["phone_number"],
        "CallerId": EXOTEL_CALLER_ID,
        "Url": exoml_url,
        "CallType": "trans"
    }
    async with httpx.AsyncClient() as client:
        try:
            response = await client.post(
                url, data=data, auth=(EXOTEL_API_KEY, EXOTEL_API_TOKEN)
            )
            print(f"Exotel Call Triggered. Status: {response.status_code}, Response: {response.text}")
        except Exception as e:
            print(f"Failed to trigger Exotel call: {e}")


@app.post("/webhook/{provider}")
@app.get("/webhook/{provider}")
async def dynamic_webhook(provider: str, request: Request):
    host = PUBLIC_URL.replace("https://", "").replace("http://", "")
    name = urllib.parse.quote(request.query_params.get("name", ""))
    interest = urllib.parse.quote(request.query_params.get("interest", ""))
    ws_url = f"wss://{host}/media-stream?name={name}&interest={interest}"
    
    # Both Twilio and Plivo respond perfectly to <Response><Connect><Stream url=""/></Connect></Response>
    # Exotel expects VoiceBot applet routing typically, but this provides a fallback XML payload.
    return HTMLResponse(
        content=f'<Response><Connect><Stream url="{ws_url}" /></Connect></Response>',
        media_type="application/xml",
    )


async def synthesize_and_send_audio(
    text: str, stream_sid: str, websocket: WebSocket
):
    url = (
        f"https://api.elevenlabs.io/v1/text-to-speech/"
        f"{os.getenv('ELEVENLABS_VOICE_ID')}/stream?output_format=ulaw_8000"
    )
    headers = {"xi-api-key": os.getenv("ELEVENLABS_API_KEY")}
    payload = {
        "text": text,
        "model_id": "eleven_turbo_v2",
        "voice_settings": {"stability": 0.5, "similarity_boost": 0.5},
    }
    try:
        async with httpx.AsyncClient() as client:
            async with client.stream(
                "POST", url, json=payload, headers=headers
            ) as response:
                async for chunk in response.aiter_bytes(chunk_size=4000):
                    if chunk:
                        await websocket.send_text(
                            json.dumps(
                                {
                                    "event": "media",
                                    "streamSid": stream_sid,
                                    "media": {
                                        "payload": base64.b64encode(chunk).decode(
                                            "utf-8"
                                        )
                                    },
                                }
                            )
                        )
    except asyncio.CancelledError:
        pass


@app.websocket("/media-stream")
async def handle_media_stream(websocket: WebSocket):
    await websocket.accept()

    lead_name = websocket.query_params.get("name", "Customer")
    interest = websocket.query_params.get("interest", "our platform")
    stream_sid = None
    chat_history = []

    dynamic_context = (
        f"You are an AI sales SDR speaking to {lead_name}. "
        f"Keep answers under 2 sentences. "
        f"First sentence MUST be: "
        f"'Hi {lead_name}, I saw you requested info about {interest}. How can I help?'"
    )

    global dg_client, llm_client
    if not dg_client:
        dg_client = DeepgramClient(os.getenv("DEEPGRAM_API_KEY", "dummy"))
    if not llm_client:
        llm_client = genai.Client(api_key=os.getenv("GEMINI_API_KEY", "dummy"))

    dg_connection = dg_client.listen.websocket.v("1")

    async def on_speech_started(self, **kwargs):
        """Barge-in: cancel TTS when user starts speaking."""
        if stream_sid:
            await websocket.send_text(
                json.dumps({"event": "clear", "streamSid": stream_sid})
            )
        if stream_sid in active_tts_tasks and not active_tts_tasks[stream_sid].done():
            active_tts_tasks[stream_sid].cancel()

    async def on_message(self, result, **kwargs):
        """Handle final transcription → LLM → TTS pipeline."""
        sentence = result.channel.alternatives[0].transcript
        if sentence and result.is_final:
            chat_history.append({"role": "user", "parts": [{"text": sentence}]})

            try:
                response = await llm_client.aio.models.generate_content(
                    model="gemini-2.5-flash",
                    contents=chat_history,
                    config=types.GenerateContentConfig(
                        system_instruction=dynamic_context
                    ),
                )

                chat_history.append(
                    {"role": "model", "parts": [{"text": response.text}]}
                )
            except Exception as e:
                print(f"Error fetching response from Gemini: {e}")
                return

            if stream_sid:
                active_tts_tasks[stream_sid] = asyncio.create_task(
                    synthesize_and_send_audio(response.text, stream_sid, websocket)
                )

    dg_connection.on(LiveTranscriptionEvents.SpeechStarted, on_speech_started)
    dg_connection.on(LiveTranscriptionEvents.Transcript, on_message)

    await dg_connection.start(
        LiveOptions(
            model="nova-3",
            language="en-IN",
            encoding="mulaw",
            sample_rate=8000,
            channels=1,
            endpointing=True,
        )
    )

    try:
        while True:
            try:
                message = await websocket.receive_text()
                data = json.loads(message)
            except Exception as e:
                print(f"Websocket connection closed or error: {e}")
                break

            if data["event"] == "start":
                stream_sid = data["start"]["streamSid"]
                # Send the initial greeting
                active_tts_tasks[stream_sid] = asyncio.create_task(
                    synthesize_and_send_audio(
                        f"Hi {lead_name}, I saw you requested info about {interest}. How can I help?",
                        stream_sid,
                        websocket,
                    )
                )
            elif data["event"] == "media":
                await dg_connection.send(
                    base64.b64decode(data["media"]["payload"])
                )
            elif data["event"] == "stop":
                print("Media stream stopped.")
                break
    except Exception as e:
        print(f"Error in media stream handler: {e}")
    finally:
        await dg_connection.finish()
        await websocket.close()

@app.post("/exotel/recording-ready")
async def handle_exotel_recording(request: Request, background_tasks: BackgroundTasks):
    body = await request.body()
    body_str = body.decode("utf-8")
    form_data = urllib.parse.parse_qs(body_str)
    
    recording_url = form_data.get("RecordingUrl", [""])[0] if "RecordingUrl" in form_data else None
    call_sid = form_data.get("CallSid", [""])[0] if "CallSid" in form_data else None
    to_phone = form_data.get("To", [""])[0] if "To" in form_data else ""
    
    if recording_url and call_sid:
        background_tasks.add_task(process_recording, recording_url, call_sid, to_phone)
    
    return {"status": "success"}

async def process_recording(recording_url: str, call_sid: str, phone: str):
    print(f"Downloading recording for {call_sid}...")
    async with httpx.AsyncClient() as client:
        try:
            resp = await client.get(recording_url, auth=(EXOTEL_API_KEY, EXOTEL_API_TOKEN), follow_redirects=True)
            audio_data = resp.content
        except Exception as e:
            print("Failed to download recording:", e)
            return

    print("Transcribing recording via Deepgram Nova-3...")
    url = "https://api.deepgram.com/v1/listen?model=nova-3&language=en-IN&smart_format=true"
    headers = {"Authorization": f"Token {os.getenv('DEEPGRAM_API_KEY')}"}
    async with httpx.AsyncClient(timeout=120) as client:
        try:
            resp = await client.post(url, content=audio_data, headers=headers)
            dg_data = resp.json()
            transcript = dg_data["results"]["channels"][0]["alternatives"][0]["transcript"]
        except Exception as e:
            print("Transcription failed:", e)
            return

    if not transcript:
        return

    print("Summarizing transcript via Gemini-2.5-Flash...")
    real_estate_prompt = """
    You are an AI assistant for a Real Estate Brokerage (BDRPL) in Kolkata.
    Analyze the following sales call transcript and produce a structured 'Follow-Up Note' for the CRM.
    Format your response cleanly in Markdown. Extract:
    1. Client Sentiment (Cold, Warm, Hot)
    2. Budget/Requirement
    3. Property Pitched (1st Sale, 2nd Sale, Rental)
    4. Next Steps / Action Items
    """
    
    try:
        global llm_client
        if not llm_client:
            llm_client = genai.Client(api_key=os.getenv("GEMINI_API_KEY", "dummy"))
            
        reply = await llm_client.aio.models.generate_content(
            model="gemini-2.5-flash",
            contents=transcript,
            config=types.GenerateContentConfig(system_instruction=real_estate_prompt)
        )
        summary = reply.text
    except Exception as e:
        print("Summarization failed:", e)
        return

    from database import update_call_note, DB_PATH
    import sqlite3
    update_call_note(call_sid, summary, phone)
    print(f"✅ Follow-up note successfully generated and injected into local DB for {call_sid}!")

    # Push to external CRM if applicable
    with sqlite3.connect(DB_PATH) as conn:
        conn.row_factory = sqlite3.Row
        lead = conn.execute("SELECT external_id, crm_provider FROM leads WHERE phone LIKE ?", (f"%{phone}%",)).fetchone()
        if lead and lead["crm_provider"] and lead["external_id"]:
            import json
            crm_info = conn.execute("SELECT credentials FROM crm_integrations WHERE provider = ?", (lead["crm_provider"],)).fetchone()
            if crm_info:
                crm_client = None
                p_name = lead["crm_provider"].lower().replace(" ", "").replace("-", "")
                try:
                    creds = json.loads(crm_info["credentials"]) if crm_info["credentials"] else {}
                    module = importlib.import_module(f"crm_providers.{p_name}")
                    for name, obj in inspect.getmembers(module, inspect.isclass):
                        if issubclass(obj, BaseCRM) and obj is not BaseCRM:
                            crm_client = obj(**creds)
                            break
                except Exception as e:
                    print(f"Error loading CRM callback {p_name}: {e}")
                
                if crm_client:
                    crm_client.log_call(lead["external_id"], transcript, summary)
                    
                    if "Hot" in summary or "Warm" in summary:
                        crm_client.update_lead_status(lead["external_id"], "Qualified")
                    else:
                        crm_client.update_lead_status(lead["external_id"], "Unqualified")
                    print(f"✅ Successfully pushed call outcome to external CRM ({p_name})!")
