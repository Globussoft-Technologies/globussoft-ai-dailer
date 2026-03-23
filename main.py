import os
import json
import base64
import urllib.parse
import asyncio
import httpx
from dotenv import load_dotenv
from fastapi import FastAPI, BackgroundTasks, Request, Body, Header, HTTPException, WebSocket, APIRouter, Depends, UploadFile, File
from fastapi.security import OAuth2PasswordBearer, OAuth2PasswordRequestForm
from fastapi.responses import HTMLResponse, StreamingResponse, JSONResponse
from passlib.context import CryptContext
import jwt
from typing import Optional
from datetime import datetime, timedelta
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
from database import get_active_crm_integrations, update_crm_last_synced, create_user, get_user_by_email
import importlib
import inspect
from crm_providers import BaseCRM
from datetime import datetime

try:
    import chromadb
    import fitz
    chroma_client = chromadb.PersistentClient(path="./chroma_db")
    knowledge_collection = chroma_client.get_or_create_collection(name="bdrpl_knowledge")
except ImportError:
    chroma_client = None
    knowledge_collection = None


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

def send_whatsapp_message(to_phone: str, body: str):
    if not TWILIO_ACCOUNT_SID or not TWILIO_AUTH_TOKEN: return
    try:
        client = Client(TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN)
        if not to_phone.startswith("whatsapp:"):
            if not to_phone.startswith("+"):
                to_phone = "+91" + to_phone[-10:]
            to_phone = "whatsapp:" + to_phone
        from_phone = "whatsapp:" + TWILIO_PHONE_NUMBER
        if not from_phone.startswith("whatsapp:+"):
            print("WARNING: TWILIO_PHONE_NUMBER does not start with +, assuming sandbox mode formatting.")
        msg = client.messages.create(body=body, from_=from_phone, to=to_phone)
        from database import create_whatsapp_log
        create_whatsapp_log(to_phone, body, "Omnichannel Brochure Trigger")
        print(f"WhatsApp sent: {msg.sid}")
    except Exception as e:
        print(f"Failed to send whatsapp: {e}")

DEFAULT_PROVIDER = os.getenv("DEFAULT_PROVIDER", "twilio").lower()

# SDK Clients will be initialized per-request to prevent startup crashes if keys are missing
dg_client = None
llm_client = None

PUBLIC_URL = os.getenv("PUBLIC_SERVER_URL", "http://localhost:8000")
active_tts_tasks = {}
monitor_connections: dict[str, set[WebSocket]] = {}
whisper_queues: dict[str, list[str]] = {}
takeover_active: dict[str, bool] = {}
twilio_websockets: dict[str, WebSocket] = {}

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

@app.post("/api/knowledge/upload")
async def upload_knowledge(file: UploadFile = File(...)):
    if not knowledge_collection or not fitz:
        raise HTTPException(status_code=500, detail="RAG dependencies (chromadb, PyMuPDF) not installed.")
    if not file.filename.endswith('.pdf'):
        raise HTTPException(status_code=400, detail="Only PDFs are supported.")
    
    content = await file.read()
    doc = fitz.open("pdf", content)
    
    text = ""
    for page in doc:
        text += page.get_text() + "\n"
    
    chunks = [c.strip() for c in text.split('\n\n') if len(c.strip()) > 50]
    if not chunks:
        return {"status": "error", "message": "No text found in PDF"}
        
    documents, metadatas, ids, embeddings = [], [], [], []
    import google.generativeai as gai
    gai.configure(api_key=os.getenv("GEMINI_API_KEY", "dummy"))
    
    for i, chunk in enumerate(chunks):
        try:
            res = gai.embed_content(model="models/text-embedding-004", content=chunk, task_type="retrieval_document")
            embeddings.append(res['embedding'])
            documents.append(chunk)
            metadatas.append({"source": file.filename, "chunk": i})
            ids.append(f"{file.filename}_{i}")
        except Exception as e:
            print(f"Embedding error: {e}")
            
    if documents:
        knowledge_collection.add(embeddings=embeddings, documents=documents, metadatas=metadatas, ids=ids)
    return {"status": "success", "chunks_added": len(documents), "filename": file.filename}

@app.get("/api/knowledge")
def get_knowledge_files():
    if not knowledge_collection: return []
    data = knowledge_collection.get()
    sources = set()
    if data and data.get('metadatas'):
        for meta in data['metadatas']:
            sources.add(meta.get("source"))
    return [{"filename": s} for s in sources]


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
        f"&phone={urllib.parse.quote(lead['phone_number'])}"
    )
    try:
        call = client.calls.create(
            url=twiml_url, to=lead["phone_number"], from_=TWILIO_PHONE_NUMBER
        )
        print(f"Twilio Call Triggered. SID: {call.sid}")
    except Exception as e:
        print(f"Failed to trigger Twilio call: {e}")

async def dial_exotel(lead: dict):
    import logging
    logger = logging.getLogger("uvicorn.error")
    # Use the Exotel Landing Flow App which has the Voicebot applet
    # configured to connect to our wss://test.callified.ai/media-stream
    exotel_app_id = os.getenv("EXOTEL_APP_ID", "1210468")
    exoml_url = f"http://my.exotel.com/exoml/start/{exotel_app_id}"
    url = f"https://api.exotel.com/v1/Accounts/{EXOTEL_ACCOUNT_SID}/Calls/connect.json"
    data = {
        "From": lead["phone_number"],
        "CallerId": EXOTEL_CALLER_ID,
        "Url": exoml_url,
        "CallType": "trans"
    }
    logger.info(f"Exotel dial attempt: URL={url}, From={lead['phone_number']}, CallerId={EXOTEL_CALLER_ID}, ExoML={exoml_url}")
    async with httpx.AsyncClient() as client:
        try:
            response = await client.post(
                url, data=data, auth=(EXOTEL_API_KEY, EXOTEL_API_TOKEN)
            )
            logger.info(f"Exotel Call Response: Status={response.status_code}, Body={response.text}")
        except Exception as e:
            logger.error(f"Failed to trigger Exotel call: {e}")



@app.post("/webhook/{provider}")
@app.get("/webhook/{provider}")
async def dynamic_webhook(provider: str, request: Request):
    host = PUBLIC_URL.replace("https://", "").replace("http://", "")
    name = urllib.parse.quote(request.query_params.get("name", ""))
    interest = urllib.parse.quote(request.query_params.get("interest", ""))
    phone = urllib.parse.quote(request.query_params.get("phone", ""))
    ws_url = f"wss://{host}/media-stream?name={name}&interest={interest}&phone={phone}"
    
    return HTMLResponse(
        content=f'<Response><Connect><Stream url="{ws_url}" /></Connect></Response>',
        media_type="application/xml",
    )


async def synthesize_and_send_audio(
    text: str, stream_sid: str, websocket: WebSocket
):
    import logging
    tts_logger = logging.getLogger("uvicorn.error")
    tts_logger.info(f"TTS START: text='{text[:60]}...', sid={stream_sid}")
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
    is_exotel = not stream_sid.startswith("SM")  # Exotel stream_sids are hex, Twilio starts with SM
    tts_logger.info(f"TTS: is_exotel={is_exotel}, voice_id={os.getenv('ELEVENLABS_VOICE_ID')}")
    try:
        async with httpx.AsyncClient(timeout=30.0) as client:
            async with client.stream(
                "POST", url, json=payload, headers=headers
            ) as response:
                tts_logger.info(f"TTS ElevenLabs response status: {response.status_code}")
                if response.status_code != 200:
                    body = await response.aread()
                    tts_logger.error(f"TTS ElevenLabs error: {body[:200]}")
                    return
                chunk_count = 0
                async for chunk in response.aiter_bytes(chunk_size=4000):
                    if chunk:
                        chunk_count += 1
                        if is_exotel:
                            # Exotel: send raw binary mulaw audio
                            await websocket.send_bytes(chunk)
                        else:
                            # Twilio: send JSON-wrapped base64 audio
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
                tts_logger.info(f"TTS DONE: sent {chunk_count} audio chunks to stream {stream_sid}")
    except asyncio.CancelledError:
        tts_logger.info("TTS cancelled (barge-in)")
    except Exception as e:
        tts_logger.error(f"TTS ERROR: {e}")


@app.websocket("/media-stream")
async def handle_media_stream(websocket: WebSocket):
    await websocket.accept()

    lead_name = websocket.query_params.get("name", "Customer")
    interest = websocket.query_params.get("interest", "our platform")
    lead_phone = websocket.query_params.get("phone", "")
    stream_sid = None
    is_exotel_stream = False
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
    loop = asyncio.get_event_loop()

    def on_speech_started(self, **kwargs):
        """Barge-in: cancel TTS when user starts speaking."""
        if stream_sid:
            asyncio.run_coroutine_threadsafe(
                websocket.send_text(
                    json.dumps({"event": "clear", "streamSid": stream_sid})
                ),
                loop,
            )
        if stream_sid in active_tts_tasks and not active_tts_tasks[stream_sid].done():
            active_tts_tasks[stream_sid].cancel()

    def on_message(self, result, **kwargs):
        """Handle final transcription → LLM → TTS pipeline."""
        sentence = result.channel.alternatives[0].transcript
        if sentence and result.is_final:
            chat_history.append({"role": "user", "parts": [{"text": sentence}]})

            async def _process_transcript():
                if stream_sid:
                    for monitor in monitor_connections.get(stream_sid, set()):
                        try:
                            await monitor.send_json({"type": "transcript", "role": "user", "text": sentence})
                        except Exception:
                            pass

                    if takeover_active.get(stream_sid, False):
                        return  # Skip LLM generation if human took over

                    pending = whisper_queues.get(stream_sid, [])
                    if pending:
                        for whisper in pending:
                            chat_history.append({"role": "user", "parts": [{"text": f"Manager Whisper: {whisper}. Acknowledge this implicitly in your next response."}]})
                        pending.clear()

                # RAG Retrieval
                rag_context = ""
                if knowledge_collection:
                    try:
                        import google.generativeai as gai
                        gai.configure(api_key=os.getenv("GEMINI_API_KEY", "dummy"))
                        res = gai.embed_content(model="models/text-embedding-004", content=sentence, task_type="retrieval_query")
                        query_emb = res['embedding']
                        results = knowledge_collection.query(query_embeddings=[query_emb], n_results=2)
                        if results and results.get('documents') and results['documents'][0]:
                            docs = results['documents'][0]
                            rag_context = "\n[KNOWLEDGE BASE RELEVANT INFO]:\n" + "\n---\n".join(docs)
                    except Exception as e:
                        print(f"RAG error: {e}")

                final_system_instruction = dynamic_context + rag_context

                try:
                    response = await llm_client.aio.models.generate_content(
                        model="gemini-2.5-flash",
                        contents=chat_history,
                        config=types.GenerateContentConfig(
                            system_instruction=final_system_instruction
                        ),
                    )

                    chat_history.append(
                        {"role": "model", "parts": [{"text": response.text}]}
                    )

                    if stream_sid:
                        for monitor in monitor_connections.get(stream_sid, set()):
                            try:
                                await monitor.send_json({"type": "transcript", "role": "agent", "text": response.text})
                            except Exception:
                                pass
                except Exception as e:
                    print(f"Error fetching response from Gemini: {e}")
                    return

                if stream_sid:
                    active_tts_tasks[stream_sid] = asyncio.create_task(
                        synthesize_and_send_audio(response.text, stream_sid, websocket)
                    )

            asyncio.run_coroutine_threadsafe(_process_transcript(), loop)

    dg_connection.on(LiveTranscriptionEvents.SpeechStarted, on_speech_started)
    dg_connection.on(LiveTranscriptionEvents.Transcript, on_message)

    dg_connection.start(
        LiveOptions(
            model="nova-3",
            language="en-IN",
            encoding="mulaw",
            sample_rate=8000,
            channels=1,
            endpointing=True,
        )
    )

    import logging
    import json as _json
    import uuid as _uuid
    ws_logger = logging.getLogger("uvicorn.error")
    ws_logger.info(f"Media stream handler started for {lead_name}")
    greeting_sent = False

    try:
        while True:
            try:
                msg = await websocket.receive()
            except Exception as e:
                print(f"Websocket connection closed or error: {e}")
                break

            if msg.get("type") == "websocket.disconnect":
                break

            # Handle binary frames (Exotel sends raw audio bytes)
            if "bytes" in msg and msg["bytes"]:
                audio_data = msg["bytes"]
                # Generate a stream_sid for Exotel if we don't have one
                if not stream_sid:
                    stream_sid = f"exotel-{_uuid.uuid4().hex[:12]}"
                    twilio_websockets[stream_sid] = websocket
                    monitor_connections[stream_sid] = set()
                    whisper_queues[stream_sid] = []
                    takeover_active[stream_sid] = False
                    ws_logger.info(f"Exotel binary stream started, sid={stream_sid}")

                # Send greeting on first audio frame
                if not greeting_sent:
                    greeting_sent = True
                    active_tts_tasks[stream_sid] = asyncio.create_task(
                        synthesize_and_send_audio(
                            f"Hi {lead_name}, I saw you requested info about {interest}. How can I help?",
                            stream_sid,
                            websocket,
                        )
                    )

                # Forward raw audio to Deepgram
                dg_connection.send(audio_data)

            # Handle text frames (Twilio sends JSON, Exotel may send JSON metadata)
            elif "text" in msg and msg["text"]:
                try:
                    data = _json.loads(msg["text"])
                except Exception as e:
                    ws_logger.warning(f"Failed to parse WS text: {e}")
                    continue

                ws_logger.info(f"WS text message received: {str(data)[:200]}")

                # Twilio/Exotel start event
                if data.get("event") == "connected":
                    ws_logger.info("Exotel WebSocket connected event received")
                    continue
                elif data.get("event") == "start":
                    # Exotel uses 'stream_sid' at top level, Twilio uses 'start.streamSid'
                    stream_sid = (
                        data.get("stream_sid")
                        or data.get("start", {}).get("streamSid")
                        or f"exotel-{_uuid.uuid4().hex[:12]}"
                    )
                    if data.get("stream_sid"):
                        is_exotel_stream = True
                    ws_logger.info(f"Stream started: sid={stream_sid}, exotel={is_exotel_stream}")
                    twilio_websockets[stream_sid] = websocket
                    monitor_connections[stream_sid] = set()
                    whisper_queues[stream_sid] = []
                    takeover_active[stream_sid] = False

                    if not greeting_sent:
                        greeting_sent = True
                        ws_logger.info(f"GREETING: Triggering TTS greeting for stream {stream_sid}")
                        active_tts_tasks[stream_sid] = asyncio.create_task(
                            synthesize_and_send_audio(
                                f"Hi {lead_name}, I saw you requested info about {interest}. How can I help?",
                                stream_sid,
                                websocket,
                            )
                        )
                elif data.get("event") == "media":
                    dg_connection.send(
                        base64.b64decode(data["media"]["payload"])
                    )
                elif data.get("event") == "stop":
                    print("Media stream stopped.")
                    break
                else:
                    # Exotel or unknown JSON — setup stream if needed
                    if not stream_sid:
                        stream_sid = f"exotel-{_uuid.uuid4().hex[:12]}"
                        twilio_websockets[stream_sid] = websocket
                        monitor_connections[stream_sid] = set()
                        whisper_queues[stream_sid] = []
                        takeover_active[stream_sid] = False
                        ws_logger.info(f"Exotel text stream started, sid={stream_sid}")
                    if not greeting_sent:
                        greeting_sent = True
                        active_tts_tasks[stream_sid] = asyncio.create_task(
                            synthesize_and_send_audio(
                                f"Hi {lead_name}, I saw you requested info about {interest}. How can I help?",
                                stream_sid,
                                websocket,
                            )
                        )
    except Exception as e:
        print(f"Error in media stream handler: {e}")
    finally:
        if stream_sid and stream_sid in twilio_websockets:
            del twilio_websockets[stream_sid]
        try:
            dg_connection.finish()
        except Exception:
            pass
        try:
            await websocket.close()
        except Exception:
            pass
        
        # Omnichannel Summary & WhatsApp Trigger
        if len(chat_history) > 2:
            try:
                transcript_text = "\n".join([f"{m['role']}: {m['parts'][0]['text']}" for m in chat_history if isinstance(m, dict) and 'parts' in m])
                summary_prompt = "You are a sales evaluator. Analyze the transcript. Return strictly a valid JSON object with: {'sentiment': 'Cold/Warm/Hot', 'requires_brochure': true/false, 'note': 'short summary of next steps'}. If the lead asks for details, pricing, or a brochure, set requires_brochure to true."
                res = await llm_client.aio.models.generate_content(
                    model="gemini-2.5-flash", 
                    contents=transcript_text,
                    config=types.GenerateContentConfig(system_instruction=summary_prompt)
                )
                import json
                text = res.text.replace("```json", "").replace("```", "").strip()
                outcome = json.loads(text)
                
                if lead_phone and outcome.get("requires_brochure"):
                    send_whatsapp_message(lead_phone, f"Hi {lead_name}, thanks for taking the time to speak just now. Here is the highly detailed property brochure you requested as discussed: https://globussoft.ai/bdrpl/luxury_brochure.pdf\n\nLet me know if you have any questions!\n\n- Globussoft AI Reception")
                
                if lead_phone:
                    from database import update_call_note
                    update_call_note("ws_" + str(stream_sid), outcome.get("note", "Call completed via Dialer."), lead_phone)
            except Exception as e:
                print(f"Omnichannel intent trigger error: {e}")

@app.websocket("/ws/sandbox")
async def sandbox_stream(websocket: WebSocket):
    await websocket.accept()
    dg = DeepgramClient(os.getenv("DEEPGRAM_API_KEY", "dummy"))
    dg_conn = dg.listen.websocket.v("1")
    llm = genai.Client(api_key=os.getenv("GEMINI_API_KEY", "dummy"))
    chat_hist = []
    
    async def on_message(self, result, **kwargs):
        sentence = result.channel.alternatives[0].transcript
        if sentence and result.is_final:
            chat_hist.append({"role": "user", "parts": [{"text": sentence}]})
            await websocket.send_json({"type": "transcript", "role": "user", "text": sentence})
            try:
                response = await llm.aio.models.generate_content(
                    model="gemini-2.5-flash",
                    contents=chat_hist,
                    config=types.GenerateContentConfig(system_instruction="You are in AI sandbox test mode. A sales manager is interacting with you. Be extremely aggressive answering sales objections, keeping answers to one line.")
                )
                chat_hist.append({"role": "model", "parts": [{"text": response.text}]})
                
                # Fetch Audio Bytes
                url = f"https://api.elevenlabs.io/v1/text-to-speech/{os.getenv('ELEVENLABS_VOICE_ID')}/stream?output_format=mp3_44100_128"
                headers = {"xi-api-key": os.getenv("ELEVENLABS_API_KEY")}
                payload = {"text": response.text, "model_id": "eleven_turbo_v2"}
                async with httpx.AsyncClient() as client:
                    async with client.stream("POST", url, json=payload, headers=headers) as resp:
                        async for chunk in resp.aiter_bytes(chunk_size=4000):
                            if chunk:
                                await websocket.send_json({"type": "audio", "payload": base64.b64encode(chunk).decode('utf-8')})
                
                await websocket.send_json({"type": "transcript", "role": "agent", "text": response.text})
            except Exception as e:
                pass
                
    dg_conn.on(LiveTranscriptionEvents.Transcript, on_message)
    await dg_conn.start(LiveOptions(
        model="nova-3", language="en-US", encoding="linear16", sample_rate=16000, channels=1, endpointing=True
    ))
    
    try:
        while True:
            data = await websocket.receive_json()
            if data.get("type") == "audio_chunk":
                raw_bytes = base64.b64decode(data["payload"])
                await dg_conn.send(raw_bytes)
    except Exception as e:
        pass
    finally:
        await dg_conn.finish()
        await websocket.close()

@app.websocket("/ws/monitor/{stream_sid}")
async def monitor_call(websocket: WebSocket, stream_sid: str):
    await websocket.accept()
    if stream_sid not in monitor_connections:
        monitor_connections[stream_sid] = set()
    monitor_connections[stream_sid].add(websocket)
    
    try:
        while True:
            data = await websocket.receive_json()
            if data.get("action") == "whisper":
                q = whisper_queues.setdefault(stream_sid, [])
                q.append(data.get("text", ""))
            elif data.get("action") == "takeover":
                takeover_active[stream_sid] = True
                # Cancel active TTS
                if stream_sid in active_tts_tasks and not active_tts_tasks[stream_sid].done():
                    active_tts_tasks[stream_sid].cancel()
            elif data.get("action") == "audio_chunk" and takeover_active.get(stream_sid, False):
                # Manager mic stream -> Twilio
                target_ws = twilio_websockets.get(stream_sid)
                if target_ws:
                    await target_ws.send_text(json.dumps({
                        "event": "media",
                        "streamSid": stream_sid,
                        "media": { "payload": data.get("payload") }
                    }))
    except Exception as e:
        pass
    finally:
        if stream_sid in monitor_connections and websocket in monitor_connections[stream_sid]:
            monitor_connections[stream_sid].remove(websocket)


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

# --- AUTHENTICATION & MOBILE APIS ---

SECRET_KEY = os.getenv("JWT_SECRET_KEY", "your-secret-key-replace-in-production")
ALGORITHM = "HS256"
ACCESS_TOKEN_EXPIRE_MINUTES = 60 * 24 * 7 # 7 days

pwd_context = CryptContext(schemes=["bcrypt"], deprecated="auto")
oauth2_scheme = OAuth2PasswordBearer(tokenUrl="/api/auth/login")

def verify_password(plain_password, hashed_password):
    return pwd_context.verify(plain_password, hashed_password)

def get_password_hash(password):
    return pwd_context.hash(password)

def create_access_token(data: dict, expires_delta: Optional[timedelta] = None):
    to_encode = data.copy()
    if expires_delta:
        expire = datetime.utcnow() + expires_delta
    else:
        expire = datetime.utcnow() + timedelta(minutes=15)
    to_encode.update({"exp": expire})
    encoded_jwt = jwt.encode(to_encode, SECRET_KEY, algorithm=ALGORITHM)
    return encoded_jwt

async def get_current_user(token: str = Depends(oauth2_scheme)):
    credentials_exception = HTTPException(
        status_code=401,
        detail="Could not validate credentials",
        headers={"WWW-Authenticate": "Bearer"},
    )
    try:
        payload = jwt.decode(token, SECRET_KEY, algorithms=[ALGORITHM])
        email: str = payload.get("sub")
        if email is None:
            raise credentials_exception
    except jwt.PyJWTError:
        raise credentials_exception
    user = get_user_by_email(email)
    if user is None:
        raise credentials_exception
    return user

class UserCreate(BaseModel):
    email: str
    password: str
    full_name: str
    role: str = "Agent"

@app.post("/api/auth/register")
def register_user(user: UserCreate):
    existing = get_user_by_email(user.email)
    if existing:
        raise HTTPException(status_code=400, detail="Email already registered")
    
    hashed_password = get_password_hash(user.password)
    user_id = create_user(user.email, hashed_password, user.full_name, user.role)
    return {"status": "success", "id": user_id, "message": "User created effectively."}

@app.post("/api/auth/login")
def login(form_data: OAuth2PasswordRequestForm = Depends()):
    user = get_user_by_email(form_data.username) # OAuth2 uses 'username', we map to email
    if not user or not verify_password(form_data.password, user["password_hash"]):
        raise HTTPException(status_code=401, detail="Incorrect email or password", headers={"WWW-Authenticate": "Bearer"})
    
    access_token_expires = timedelta(minutes=ACCESS_TOKEN_EXPIRE_MINUTES)
    access_token = create_access_token(
        data={"sub": user["email"]}, expires_delta=access_token_expires
    )
    return {"access_token": access_token, "token_type": "bearer", "role": user.get("role")}

mobile_api = APIRouter(prefix="/api/mobile", tags=["Mobile Routes"])

@mobile_api.get("/leads")
def mobile_get_leads(current_user: dict = Depends(get_current_user)):
    return get_all_leads()

@mobile_api.post("/leads")
def mobile_create_lead(lead: LeadCreate, current_user: dict = Depends(get_current_user)):
    try:
        lead_id = create_lead(lead.dict())
        return {"status": "success", "id": lead_id}
    except Exception as e:
        return {"status": "error", "message": str(e)}

@mobile_api.put("/leads/{lead_id}/status")
def mobile_update_lead_status(lead_id: int, payload: LeadStatusUpdate, current_user: dict = Depends(get_current_user)):
    update_lead_status(lead_id, payload.status)
    return {"status": "success", "message": f"Lead {lead_id} updated to {payload.status}"}

@mobile_api.post("/dial/{lead_id}")
async def mobile_dial_lead(lead_id: int, background_tasks: BackgroundTasks, current_user: dict = Depends(get_current_user)):
    return await api_dial_lead(lead_id, background_tasks)

@mobile_api.get("/analytics")
def mobile_get_analytics(current_user: dict = Depends(get_current_user)):
    return get_analytics()

@mobile_api.post("/punch")
def mobile_punch(punch: PunchCreate, current_user: dict = Depends(get_current_user)):
    return api_punch(punch)

@mobile_api.get("/tasks")
def mobile_get_tasks(current_user: dict = Depends(get_current_user)):
    return get_all_tasks()

@mobile_api.put("/tasks/{task_id}/complete")
def mobile_complete_task(task_id: int, current_user: dict = Depends(get_current_user)):
    complete_task(task_id)
    return {"status": "success"}

app.include_router(mobile_api)
