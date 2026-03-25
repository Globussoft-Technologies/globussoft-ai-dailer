import os
import call_logger
import json
import base64
import urllib.parse
import asyncio
import httpx
from dotenv import load_dotenv
from fastapi import FastAPI, BackgroundTasks, Request, Body, Header, HTTPException, WebSocket, APIRouter, Depends, UploadFile, File, Form
from fastapi.security import OAuth2PasswordBearer, OAuth2PasswordRequestForm
from fastapi.responses import HTMLResponse, StreamingResponse, JSONResponse, FileResponse
from fastapi.staticfiles import StaticFiles
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
from database import init_db, get_all_leads, get_lead_by_id, create_lead, update_lead, delete_lead, get_all_sites, create_punch, get_site_by_id
from database import update_lead_status, get_all_tasks, complete_task, get_reports, get_all_whatsapp_logs
from database import upload_document, get_documents_by_lead, get_analytics, search_leads, update_lead_note
from database import get_active_crm_integrations, update_crm_last_synced, create_user, get_user_by_email
from database import get_all_pronunciations, add_pronunciation, delete_pronunciation, get_pronunciation_context
from database import save_call_transcript, get_transcripts_by_lead
from database import create_organization, get_all_organizations, delete_organization
from database import create_product, get_products_by_org, update_product, delete_product, get_product_knowledge_context
from database import get_org_custom_prompt, save_org_custom_prompt
from database import get_org_voice_settings, save_org_voice_settings
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
call_logger.setup_logging()
app = FastAPI()

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)



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

class OrgSignup(BaseModel):
    org_name: str
    full_name: str
    email: str
    password: str

class LoginRequest(BaseModel):
    email: str
    password: str



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

EXOTEL_API_KEY = (os.getenv("EXOTEL_API_KEY") or "").strip()
EXOTEL_API_TOKEN = (os.getenv("EXOTEL_API_TOKEN") or "").strip()
EXOTEL_ACCOUNT_SID = (os.getenv("EXOTEL_ACCOUNT_SID") or "YOUR_EXOTEL_ACCOUNT_SID").strip()
EXOTEL_CALLER_ID = (os.getenv("EXOTEL_CALLER_ID") or "YOUR_EXOTEL_NUMBER").strip()

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
def api_get_leads(current_user: dict = Depends(get_current_user)):
    return get_all_leads(current_user.get("org_id"))

@app.get("/api/leads/export")
def api_export_leads(current_user: dict = Depends(get_current_user)):
    leads = get_all_leads(current_user.get("org_id"))
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
def api_search_leads(q: str = "", current_user: dict = Depends(get_current_user)):
    if not q:
        return get_all_leads(current_user.get("org_id"))
    return search_leads(q, current_user.get("org_id"))

@app.post("/api/leads")
def api_create_lead(lead: LeadCreate, current_user: dict = Depends(get_current_user)):
    try:
        lead_id = create_lead(lead.dict(), current_user.get("org_id"))
        return {"status": "success", "id": lead_id}
    except Exception as e:
        return {"status": "error", "message": str(e)}

@app.put("/api/leads/{lead_id}")
def api_update_lead(lead_id: int, lead: LeadCreate, current_user: dict = Depends(get_current_user)):
    try:
        success = update_lead(lead_id, lead.dict(), current_user.get("org_id"))
        if success:
            return {"status": "success", "message": f"Lead {lead_id} updated"}
        return {"status": "error", "message": "Lead not found"}
    except Exception as e:
        return {"status": "error", "message": str(e)}

@app.delete("/api/leads/{lead_id}")
def api_delete_lead(lead_id: int, current_user: dict = Depends(get_current_user)):
    try:
        success = delete_lead(lead_id, current_user.get("org_id"))
        if success:
            return {"status": "success", "message": f"Lead {lead_id} deleted"}
        return {"status": "error", "message": "Lead not found"}
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
        "provider": DEFAULT_PROVIDER,
        "lead_id": lead_id
    })
    return {"status": "success", "message": f"Dialing {lead['first_name']}..."}

@app.get("/api/sites")
def api_get_sites(current_user: dict = Depends(get_current_user)):
    return get_all_sites(current_user.get("org_id"))

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
def api_get_tasks(current_user: dict = Depends(get_current_user)):
    return get_all_tasks(current_user.get("org_id"))

@app.put("/api/tasks/{task_id}/complete")
def api_complete_task(task_id: int):
    complete_task(task_id)
    return {"status": "success"}

@app.get("/api/reports")
def api_get_reports(current_user: dict = Depends(get_current_user)):
    return get_reports(current_user.get("org_id"))

@app.get("/api/whatsapp")
def api_get_whatsapp(current_user: dict = Depends(get_current_user)):
    return get_all_whatsapp_logs(current_user.get("org_id"))

@app.post("/api/leads/{lead_id}/documents")
def api_upload_document(lead_id: int, payload: DocumentCreate):
    upload_document(lead_id, payload.file_name, payload.file_url)
    return {"status": "success", "message": f"{payload.file_name} uploaded successfully."}

@app.get("/api/leads/{lead_id}/documents")
def api_get_documents(lead_id: int):
    return get_documents_by_lead(lead_id)

@app.get("/api/leads/{lead_id}/transcripts")
def api_get_transcripts(lead_id: int):
    return get_transcripts_by_lead(lead_id)

# ─── Organizations & Products API ───

@app.get("/api/organizations")
def api_get_organizations(current_user: dict = Depends(get_current_user)):
    all_orgs = get_all_organizations()
    user_org_id = current_user.get("org_id")
    if user_org_id:
        return [o for o in all_orgs if o["id"] == user_org_id]
    return all_orgs

@app.post("/api/organizations")
def api_create_organization(payload: dict):
    org_id = create_organization(payload.get("name", ""))
    return {"status": "ok", "id": org_id}

@app.delete("/api/organizations/{org_id}")
def api_delete_organization(org_id: int):
    delete_organization(org_id)
    return {"status": "ok"}

@app.get("/api/organizations/{org_id}/products")
def api_get_products(org_id: int):
    return get_products_by_org(org_id)

@app.post("/api/organizations/{org_id}/products")
def api_create_product(org_id: int, payload: dict):
    pid = create_product(org_id, payload.get("name", ""), payload.get("website_url", ""), payload.get("manual_notes", ""))
    return {"status": "ok", "id": pid}

@app.put("/api/products/{product_id}")
def api_update_product(product_id: int, payload: dict):
    update_product(product_id, **{k: v for k, v in payload.items() if k in ('name', 'website_url', 'scraped_info', 'manual_notes')})
    return {"status": "ok"}

@app.delete("/api/products/{product_id}")
def api_delete_product_endpoint(product_id: int):
    delete_product(product_id)
    return {"status": "ok"}

@app.get("/api/organizations/{org_id}/system-prompt")
def api_get_system_prompt(org_id: int, current_user: dict = Depends(get_current_user)):
    """Return auto-generated product knowledge prompt and any custom override."""
    auto_prompt = get_product_knowledge_context(org_id=org_id)
    custom_prompt = get_org_custom_prompt(org_id)
    return {"auto_generated": auto_prompt, "custom_prompt": custom_prompt}

@app.put("/api/organizations/{org_id}/system-prompt")
def api_save_system_prompt(org_id: int, payload: dict, current_user: dict = Depends(get_current_user)):
    """Save a custom system prompt override for an organization."""
    save_org_custom_prompt(org_id, payload.get("custom_prompt", ""))
    return {"status": "ok"}

@app.get("/api/organizations/{org_id}/voice-settings")
def api_get_voice_settings(org_id: int, current_user: dict = Depends(get_current_user)):
    return get_org_voice_settings(org_id)

@app.put("/api/organizations/{org_id}/voice-settings")
def api_save_voice_settings(org_id: int, payload: dict, current_user: dict = Depends(get_current_user)):
    save_org_voice_settings(org_id, payload.get("tts_provider", "elevenlabs"), payload.get("tts_voice_id", ""), payload.get("tts_language", "hi"))
    return {"status": "ok"}

@app.post("/api/upload-recording")
async def api_upload_recording(current_user: dict = Depends(get_current_user), file: UploadFile = File(...), lead_id: str = Form("")):
    """Upload a client-recorded call (webm from MediaRecorder)."""
    import logging
    _ul = logging.getLogger("uvicorn.error")
    rec_dir = os.path.join(os.path.dirname(__file__), "recordings")
    os.makedirs(rec_dir, exist_ok=True)
    fname = file.filename or f"call_{lead_id}_{int(__import__('time').time())}.webm"
    fpath = os.path.join(rec_dir, fname)
    contents = await file.read()
    with open(fpath, "wb") as f:
        f.write(contents)
    _ul.info(f"[RECORDING] Client upload saved: {fpath} ({len(contents)} bytes)")
    # Update latest transcript for this lead with the recording URL
    if lead_id and lead_id.isdigit():
        rec_url = f"/api/recordings/{fname}"
        try:
            from database import get_conn
            conn = get_conn()
            cur = conn.cursor()
            # Use subquery to find latest transcript ID for this lead
            cur.execute("SELECT id FROM call_transcripts WHERE lead_id = %s ORDER BY id DESC LIMIT 1", (int(lead_id),))
            row = cur.fetchone()
            if row:
                cur.execute("UPDATE call_transcripts SET recording_url = %s WHERE id = %s", (rec_url, row['id']))
                _ul.info(f"[RECORDING] Updated transcript {row['id']} with URL: {rec_url}")
            else:
                _ul.warning(f"[RECORDING] No transcript found for lead {lead_id}")
            conn.commit()
            conn.close()
        except Exception as e:
            _ul.error(f"[RECORDING] DB update error: {e}")
    return {"status": "ok", "url": f"/api/recordings/{fname}"}

@app.post("/api/products/{product_id}/scrape")
async def api_scrape_product_website(product_id: int):
    """Fetch a product's website (or research by name) and use LLM to extract key info."""
    from database import get_products_by_org as _gp
    import logging
    logger = logging.getLogger("uvicorn.error")
    
    # Get current product info
    conn = __import__('database').get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM products WHERE id = %s", (product_id,))
    product = cursor.fetchone()
    conn.close()
    
    if not product:
        return {"status": "error", "message": "Product not found"}
    
    url = (product.get('website_url') or '').strip()
    product_name = product.get('name', '')
    html = ""
    
    # Step 1: Try to fetch website HTML if URL provided
    if url:
        if not url.startswith("http"):
            url = "https://" + url
        try:
            async with httpx.AsyncClient() as client:
                resp = await client.get(url, timeout=15, follow_redirects=True)
                html = resp.text[:15000]
        except Exception as e:
            logger.error(f"[SCRAPE] Failed to fetch {url}: {e}")
            html = ""  # Fall through to name-based research
    
    # Step 2: Use LLM to extract/research product info
    if html:
        scrape_prompt = (
            "You are a product analyst. Given this website HTML, extract the following in a concise format:\n"
            "1. Company name\n"
            "2. What the product/service does (2-3 sentences)\n"
            "3. Key features (bullet points)\n"
            "4. Target audience\n"
            "5. Pricing (if visible)\n"
            "6. Contact info\n\n"
            "Be concise — max 500 words. Only include information that is clearly stated on the page.\n\n"
            f"WEBSITE HTML:\n{html}"
        )
    else:
        # No URL or fetch failed — research by product name
        scrape_prompt = (
            f"You are a product analyst. Research and provide detailed information about the product/service called '{product_name}'.\n"
            f"Provide the following in a concise format:\n"
            f"1. What is {product_name}? (2-3 sentences)\n"
            f"2. Key features and capabilities\n"
            f"3. Target audience / who uses it\n"
            f"4. How it works (brief overview)\n"
            f"5. Key benefits / value proposition\n"
            f"6. Pricing model (if known)\n\n"
            f"Be concise — max 500 words. If you don't have specific info, provide general knowledge about this type of product."
        )
    
    try:
        groq_key = os.getenv("GROQ_API_KEY", "")
        if groq_key:
            async with httpx.AsyncClient() as client:
                scrape_resp = await client.post(
                    "https://api.groq.com/openai/v1/chat/completions",
                    headers={"Authorization": f"Bearer {groq_key}", "Content-Type": "application/json"},
                    json={
                        "model": "llama-3.3-70b-versatile",
                        "messages": [{"role": "user", "content": scrape_prompt}],
                        "max_tokens": 1000,
                        "temperature": 0.3
                    },
                    timeout=30
                )
                scrape_data = scrape_resp.json()
                scraped_info = scrape_data["choices"][0]["message"]["content"]
        else:
            scraped_info = "No LLM API key configured."
    except Exception as e:
        logger.error(f"[SCRAPE] LLM extraction failed: {e}")
        scraped_info = f"LLM extraction failed: {str(e)}"
    
    # Step 3: Save to product
    update_product(product_id, scraped_info=scraped_info)
    
    return {"status": "ok", "scraped_info": scraped_info}


@app.get("/api/analytics")
def api_get_analytics():
    return get_analytics()

@app.get("/api/integrations")
def api_get_integrations(current_user: dict = Depends(get_current_user)):
    active = get_active_crm_integrations(current_user.get("org_id"))
    # Mask API keys for frontend security
    for a in active:
        if a["api_key"] and len(a["api_key"]) > 8:
            a["api_key"] = a["api_key"][:4] + "****" + a["api_key"][-4:]
        elif a["api_key"]:
            a["api_key"] = "****"
    return active

from database import save_crm_integration

@app.post("/api/integrations")
async def create_integration(data: dict, current_user: dict = Depends(get_current_user)):
    provider = data.get("provider")
    credentials = data.get("credentials")
    
    if not provider or not credentials:
        return JSONResponse(status_code=400, content={"error": "provider and credentials are required"})
        
    try:
        # Save integration safely
        from database import save_crm_integration
        save_crm_integration(provider, credentials, current_user.get("org_id"))
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


# Store lead info for WebSocket greeting lookup (Exotel doesn't forward ExoML params)
pending_call_info = {}

async def initiate_call(lead: dict):
    provider = lead.get("provider", "twilio")
    # Store lead info so WebSocket handler can look it up
    phone_clean = lead.get("phone_number", "").lstrip("+")
    pending_call_info["latest"] = {
        "name": lead.get("name", "Customer"),
        "interest": lead.get("interest", "our platform"),
        "phone": phone_clean,
        "lead_id": lead.get("lead_id")
    }
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
            url=twiml_url, 
            to=lead["phone_number"], 
            from_=TWILIO_PHONE_NUMBER,
            status_callback=f"{PUBLIC_URL}/webhook/twilio/status",
            status_callback_event=['completed', 'no-answer', 'busy', 'failed', 'canceled']
        )
        print(f"Twilio Call Triggered. SID: {call.sid}")
    except Exception as e:
        print(f"Failed to trigger Twilio call: {e}")

# Debug: store last Exotel dial result for remote inspection
last_dial_result = {}

async def dial_exotel(lead: dict):
    import logging
    import urllib.parse
    import base64 as _b64
    from datetime import datetime
    global last_dial_result
    logger = logging.getLogger("uvicorn.error")
    # Use the Exotel Landing Flow App which has the Voicebot applet
    # configured to connect to our wss://test.callified.ai/media-stream
    exotel_app_id = os.getenv("EXOTEL_APP_ID", "1210468")
    # Use base ExoML URL only — query params cause double-encoding in form POST
    # Lead info is available via pending_call_info for WebSocket greeting lookup
    exoml_url = f"http://my.exotel.com/exoml/start/{exotel_app_id}"
    # Normalize phone for Exotel: needs digits with 91 country code prefix
    phone_clean = lead["phone_number"].strip().lstrip("+")
    # Ensure 91 prefix: if 10-digit number, prepend 91
    if len(phone_clean) == 10 and not phone_clean.startswith("0"):
        phone_clean = "91" + phone_clean
    logger.info(f"Phone normalized: '{lead['phone_number']}' -> '{phone_clean}'")
    url = f"https://api.exotel.com/v1/Accounts/{EXOTEL_ACCOUNT_SID}/Calls/connect.json"
    data = {
        "From": phone_clean,
        "CallerId": EXOTEL_CALLER_ID,
        "Url": exoml_url,
        "CallType": "trans",
        "StatusCallback": f"{PUBLIC_URL}/webhook/exotel/status"
    }
    logger.info(f"[DIAL] Exotel attempt: From={phone_clean}, ExoML={exoml_url}")
    call_logger.call_event(phone_clean, "DIAL_INITIATED", f"From={phone_clean}, Url={exoml_url}")
    last_dial_result = {"timestamp": datetime.now().isoformat(), "phone": phone_clean, "url": url, "exoml": exoml_url, "status": "pending"}
    try:
        # Build Basic auth header exactly as Exotel confirmed working
        creds = f"{EXOTEL_API_KEY}:{EXOTEL_API_TOKEN}"
        auth_b64 = _b64.b64encode(creds.encode()).decode()
        headers = {
            "Content-Type": "application/x-www-form-urlencoded",
            "Authorization": f"Basic {auth_b64}",
        }
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.post(url, data=data, headers=headers)
        logger.info(f"[DIAL] Exotel response ({resp.status_code}): {resp.text[:300]}")
        call_logger.call_event(phone_clean, "DIAL_RESPONSE", f"status={resp.status_code}", response=resp.text[:200])
        last_dial_result.update({"status": resp.status_code, "response": resp.text[:500]})
        # Extract and store the Exotel Call SID for recording fetch later
        try:
            dial_json = resp.json()
            exotel_sid = dial_json.get("Call", {}).get("Sid", "")
            if exotel_sid:
                pending_call_info["latest"]["exotel_call_sid"] = exotel_sid
                logger.info(f"[DIAL] Stored Exotel Call SID: {exotel_sid}")
        except Exception:
            pass
        if resp.status_code != 200:
            logger.error(f"Exotel API error {resp.status_code}: {resp.text[:500]}")
    except Exception as e:
        logger.error(f"[DIAL] Failed to trigger Exotel call: {e}")
        call_logger.call_event(phone_clean, "DIAL_ERROR", str(e))
        last_dial_result.update({"status": "error", "error": str(e)})

@app.get("/api/debug/last-dial")
def debug_last_dial():
    return last_dial_result

@app.get("/api/debug/logs")
def debug_logs(n: int = 100, level: str = "", keyword: str = ""):
    """Return last N log entries. Filter by ?level=ERROR or ?keyword=TTS"""
    return call_logger.get_logs(n=n, level=level or None, keyword=keyword or None)

@app.get("/api/debug/call-timeline")
def debug_call_timeline(n: int = 5):
    """Return last N call timelines with per-event timestamps."""
    return call_logger.get_timelines(n=n)

@app.get("/api/debug/health")
def debug_health():
    """Quick health check with pipeline status."""
    import time
    return {
        "status": "ok",
        "uptime_s": round(time.time() - _app_start_time, 1),
        "active_calls": len(call_logger._active_timelines),
        "total_logs": len(call_logger._log_buffer),
        "last_dial": last_dial_result.get("status", "none"),
    }

_app_start_time = __import__('time').time()

# --- PRONUNCIATION GUIDE API ---

@app.get("/api/pronunciation")
def get_pronunciations():
    return get_all_pronunciations()

@app.post("/api/pronunciation")
async def create_pronunciation(request: Request):
    data = await request.json()
    word = data.get("word", "").strip()
    phonetic = data.get("phonetic", "").strip()
    if not word or not phonetic:
        return {"error": "word and phonetic are required"}
    add_pronunciation(word, phonetic)
    return {"status": "ok", "word": word, "phonetic": phonetic}

@app.delete("/api/pronunciation/{pronunciation_id}")
def remove_pronunciation(pronunciation_id: int):
    ok = delete_pronunciation(pronunciation_id)
    return {"status": "ok" if ok else "not_found"}



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


# Module-level dict to collect TTS audio for recording per stream
_tts_recording_buffers: dict = {}

async def synthesize_and_send_audio(
    text: str, stream_sid: str, websocket: WebSocket,
    tts_provider_override: str = None, tts_voice_override: str = None, tts_language_override: str = None
):
    import logging
    import struct
    import audioop
    tts_logger = logging.getLogger("uvicorn.error")
    tts_logger.info(f"TTS START: text='{text[:60]}...', sid={stream_sid}")
    is_browser_sim = stream_sid.startswith("web_sim_")
    is_exotel = not stream_sid.startswith("SM") and not is_browser_sim
    # Browser sim needs raw PCM just like Exotel (not u-law)
    needs_raw_pcm = is_exotel or is_browser_sim
    
    tts_provider = (tts_provider_override or os.getenv("TTS_PROVIDER", "elevenlabs")).lower()
    
    if tts_provider == "smallest":
        # Smallest AI Lightning V3 TTS
        url = "https://waves-api.smallest.ai/api/v1/lightning/get_speech"
        headers = {
            "Authorization": f"Bearer {os.getenv('SMALLEST_API_KEY')}",
            "Content-Type": "application/json"
        }
        payload = {
            "text": text,
            "voice_id": tts_voice_override or os.getenv("SMALLEST_VOICE_ID", "emily"),
            "sample_rate": 8000,
            "add_wav_header": False,
            "speed": 1.0
        }
        tts_logger.info(f"TTS: provider=SmallestAI, is_exotel={is_exotel}, is_browser_sim={is_browser_sim}")
        try:
            async with httpx.AsyncClient(timeout=30.0) as client:
                async with client.stream("POST", url, json=payload, headers=headers) as response:
                    if response.status_code != 200:
                        body = await response.aread()
                        tts_logger.error(f"TTS SmallestAI error: {body[:200]}")
                        return
                    chunk_count = 0
                    async for chunk in response.aiter_bytes(chunk_size=1024):
                        if chunk:
                            if needs_raw_pcm:
                                # Exotel and Browser Sim both want raw 8kHz Linear PCM (16-bit)
                                b64_chunk = base64.b64encode(chunk).decode('utf-8')
                            else:
                                # Twilio wants 8kHz u-law
                                ulaw_chunk = audioop.lin2ulaw(chunk, 2)
                                b64_chunk = base64.b64encode(ulaw_chunk).decode('utf-8')
                                
                            await websocket.send_text(json.dumps({
                                "event": "media",
                                "stream_sid": stream_sid,
                                "media": {"payload": b64_chunk}
                            }))
                            chunk_count += 1
                    tts_logger.info(f"TTS SmallestAI END: sent {chunk_count} chunks.")
        except Exception as e:
            tts_logger.error(f"TTS SmallestAI Exception: {e}")
            
    else:
        # ElevenLabs TTS Fallback/Default
        if needs_raw_pcm:
            output_format = "pcm_16000"  # Downsampled to 8kHz inline for both Exotel and Browser
        else:
            output_format = "ulaw_8000"
            
        url = (
            f"https://api.elevenlabs.io/v1/text-to-speech/"
            f"{tts_voice_override or os.getenv('ELEVENLABS_VOICE_ID')}/stream?output_format={output_format}&optimize_streaming_latency=3"
        )
        headers = {"xi-api-key": os.getenv("ELEVENLABS_API_KEY")}
        payload = {
            "text": text,
            "model_id": "eleven_turbo_v2_5",
            "language_code": tts_language_override or "hi",
            "voice_settings": {
                "stability": 0.35, 
                "similarity_boost": 0.85,
                "style": 0.1,
                "use_speaker_boost": True
            },
        }
        tts_logger.info(f"TTS: provider=ElevenLabs, is_exotel={is_exotel}, is_browser_sim={is_browser_sim}, format={output_format}")
        
        try:
            async with httpx.AsyncClient(timeout=30.0) as client:
                async with client.stream(
                    "POST", url, json=payload, headers=headers
                ) as response:
                    if response.status_code != 200:
                        body = await response.aread()
                        tts_logger.error(f"TTS ElevenLabs error: {body[:200]}")
                        return
                    chunk_count = 0
                    pcm_buffer = b""
                    audio_state = None
                    async for chunk in response.aiter_bytes(chunk_size=640):
                        if chunk:
                            if needs_raw_pcm:
                                # Downsample 16kHz to 8kHz for ElevenLabs PCM output
                                pcm_buffer += chunk
                                usable = len(pcm_buffer) - (len(pcm_buffer) % 4)
                                if usable >= 1280:  # 1280 bytes of 16kHz 16-bit = 640 samples = 40ms
                                    raw = pcm_buffer[:usable]
                                    pcm_buffer = pcm_buffer[usable:]
                                    import audioop
                                    downsampled, audio_state = audioop.ratecv(raw, 2, 1, 16000, 8000, audio_state)
                                    b64_chunk = base64.b64encode(downsampled).decode('utf-8')
                                    await websocket.send_text(json.dumps({
                                        "event": "media",
                                        "stream_sid": stream_sid,
                                        "media": {"payload": b64_chunk}
                                    }))
                                    # Capture for call recording (with timestamp)
                                    if stream_sid in _tts_recording_buffers:
                                        import time as _tts_t
                                        _tts_recording_buffers[stream_sid].append((_tts_t.time(), downsampled))
                                    chunk_count += 1
                                    # Output is 640 bytes of 8kHz 16-bit = 320 samples = 40ms of audio.
                                    await asyncio.sleep(0.020)  # Pace to 20ms (2x realtime) to build jitter buffer gracefully
                            else:
                                await websocket.send_text(json.dumps({
                                    "event": "media",
                                    "streamSid": stream_sid,
                                    "media": {"payload": base64.b64encode(chunk).decode('utf-8')}
                                }))
                                # 640 bytes of 8kHz ulaw = 640 samples = 80ms audio. Pace slightly under to prevent lag.
                                await asyncio.sleep(0.070)
                                chunk_count += 1
                    tts_logger.info(f"TTS ElevenLabs END: sent {chunk_count} chunks.")
        except asyncio.CancelledError:
            tts_logger.info("TTS ElevenLabs cancelled (barge-in)")
        except Exception as e:
            tts_logger.error(f"TTS ElevenLabs Exception: {e}")

@app.websocket("/media-stream")
async def handle_media_stream(websocket: WebSocket):
    await websocket.accept()

    # Try query params first, then fall back to pending_call_info from dial
    lead_name = websocket.query_params.get("name", "") or ""
    interest = websocket.query_params.get("interest", "") or ""
    lead_phone = websocket.query_params.get("phone", "") or ""
    _call_lead_id = None
    # Extract lead_id from query params (browser sim) or pending_call_info (Exotel)
    _qp_lead_id = websocket.query_params.get("lead_id", "")
    if _qp_lead_id and _qp_lead_id.isdigit():
        _call_lead_id = int(_qp_lead_id)
    # Voice override from sandbox/sim call query params
    _tts_provider_override = websocket.query_params.get("tts_provider", None) or None
    _tts_voice_override = websocket.query_params.get("voice", None) or None
    _tts_language_override = websocket.query_params.get("tts_language", None) or None
    # If no override passed, look up org voice settings from DB
    if not _tts_voice_override:
        try:
            from database import get_conn as _gc
            _vc = _gc()
            _vcur = _vc.cursor()
            # Find org_id from the lead or from the first user
            _org_for_voice = None
            if _call_lead_id:
                _vcur.execute("SELECT org_id FROM leads WHERE id = %s", (_call_lead_id,))
                _lr = _vcur.fetchone()
                if _lr and _lr.get('org_id'):
                    _org_for_voice = _lr['org_id']
            if not _org_for_voice:
                _vcur.execute("SELECT org_id FROM users LIMIT 1")
                _ur = _vcur.fetchone()
                if _ur: _org_for_voice = _ur.get('org_id')
            _vc.close()
            if _org_for_voice:
                _vs = get_org_voice_settings(_org_for_voice)
                if _vs.get('tts_voice_id'):
                    _tts_voice_override = _vs['tts_voice_id']
                    _tts_provider_override = _vs.get('tts_provider', 'elevenlabs')
                _tts_language_override = _tts_language_override or _vs.get('tts_language', 'hi')
        except Exception:
            pass
    if not lead_name or lead_name == "Customer":
        info = pending_call_info.get("latest", {})
        lead_name = info.get("name", "Customer")
        interest = info.get("interest", "our platform") if not interest else interest
        lead_phone = info.get("phone", "") if not lead_phone else lead_phone
        if not _call_lead_id:
            _call_lead_id = info.get("lead_id")
    _exotel_call_sid = (pending_call_info.get("latest", {}).get("exotel_call_sid") or "")
    _call_start_time = __import__('time').time()
    stream_sid = None
    is_exotel_stream = False
    chat_history = []
    _llm_lock = asyncio.Lock()  # Turn guard: only one LLM call at a time
    _last_transcript_time = [0.0]  # Debounce timer for rapid transcripts
    _debounce_delay = 0.4  # seconds to wait before processing
    # Audio recording buffers (collect 8kHz 16-bit PCM for WAV)
    _recording_mic_chunks = []   # (timestamp, bytes) raw mic audio from browser/Exotel
    _recording_tts_chunks = []   # (timestamp, bytes) TTS audio sent back to caller

    # Load pronunciation guide for TTS-correct product names
    pronunciation_ctx = get_pronunciation_context()

    # Load product knowledge for system prompt (prefer custom prompt if set)
    product_ctx = ""
    try:
        # Try to get org-specific prompt from users table
        _user_conn = __import__('database').get_conn()
        _user_cursor = _user_conn.cursor()
        _user_cursor.execute("SELECT u.org_id FROM users u JOIN leads l ON 1=1 WHERE l.id = %s LIMIT 1", (_call_lead_id,))
        _user_row = _user_cursor.fetchone()
        _call_org_id = _user_row.get('org_id') if _user_row else None
        _user_conn.close()
        if _call_org_id:
            custom = get_org_custom_prompt(_call_org_id)
            if custom.strip():
                product_ctx = "\n\n[PRODUCT KNOWLEDGE]:\n" + custom
            else:
                product_ctx = get_product_knowledge_context(org_id=_call_org_id)
        else:
            product_ctx = get_product_knowledge_context()
    except Exception:
        product_ctx = get_product_knowledge_context()

    dynamic_context = (
        f"Tum Arjun ho — ek friendly, professional lead qualifier. Tum {lead_name} ko call kar rahe ho. "
        f"Tumhare records mein hai ki unhone {interest} ke baare mein ek form bhara tha website par. "
        f"\n\nTUMHARA ROLE: Tum lead qualify karne ke liye call kar rahe ho aur product ke baare mein briefly bata sakte ho. "
        f"Tumhara kaam hai: "
        f"(a) Confirm karo ki unhone sach mein form bhara tha ya nahi. "
        f"(b) Agar haan, toh puchho ki unhe abhi bhi interest hai ya nahi. "
        f"(c) Agar user PRODUCT ke baare mein puchhe ('kya hai ye?', 'ye kya karta hai?'), toh PRODUCT KNOWLEDGE section se "
        f"    1-2 line mein briefly batao ki product kya hai aur kya karta hai. PHIR bolo ki 'isse detail mein humara senior representative samjhayega, "
        f"    main aapke liye ek meeting schedule kar deta hoon.' "
        f"(d) Agar interest hai, toh ek appointment/callback schedule karo — puchho ki kab free hain, "
        f"    aur bolo ki humara senior representative unhe us time call karega. "
        f"\n\nCONVERSATION RULES: "
        f"1. SUNNA PEHLE: User jo bole, pehle samjho, phir respond karo. User ki baat ignore mat karo. "
        f"2. FORM DENIAL: Agar user bole 'maine form nahi bhara', 'yaad nahi', 'nahi kiya' — toh MAAN JAO. "
        f"   Bolo: 'achha sorry, ho sakta hai galti se number aa gaya ho, aapko disturb karne ke liye maafi, aapka din acha ho.' Conversation politely end karo. "
        f"3. NO INTEREST: Agar user bole 'interest nahi hai', 'nahi chahiye' — respectfully bolo 'bilkul samajh gaya, koi baat nahi, thank you for your time.' End karo. "
        f"4. INTERESTED: Agar user interested hai, toh puchho 'achha great, toh aap kab free honge? Humara representative aapko detail mein baat karega.' "
        f"   Unka preferred time le lo aur bolo ki call scheduled hai. "
        f"5. SHORT RESPONSES: 1-2 sentences max. Phone pe chhota bolo, jaise real person baat karta hai. "
        f"IMPORTANT LANGUAGE RULE: SIRF ROMAN HINDI/ENGLISH likhna hai. KABHI BHI Devanagari script mat use karo. "
        f"Hamesha Roman letters mein likho jaise: Namaste, Achha, Bilkul. "
        f"6. LANGUAGE: Natural Hindi-Hinglish. 'haan', 'bilkul', 'achha', 'zaroor' use karo. "
        f"7. NO FORMATTING: Kabhi markdown, asterisks, bold, bullets mat use karo. "
        f"8. IDENTITY: Tum Arjun ho, ek human. Kabhi mat bolo ki tum AI ho. "
        f"9. NO REPETITION: Jo bol chuke ho dubara mat bolo. Conversation aage le jaao. "
        f"10. NATURAL FLOW: Agar user ne sirf 'achha' ya 'haan' bola toh features mat bolo — puchho 'toh aapko abhi bhi interest hai? Agar haan toh main ek appointment schedule kar deta hoon.'"
        f"{pronunciation_ctx}"
        f"{product_ctx}"
    )

    global dg_client, llm_client
    if not dg_client:
        dg_client = DeepgramClient(os.getenv("DEEPGRAM_API_KEY", "dummy"))
    if not llm_client:
        llm_client = genai.Client(api_key=(os.getenv("GEMINI_API_KEY") or "dummy").strip())

    dg_connection = dg_client.listen.websocket.v("1")
    loop = asyncio.get_event_loop()

    def on_error(self, error, **kwargs):
        import logging
        logging.getLogger("uvicorn.error").error(f"[STT ERROR] Deepgram fired an error: {error}")

    def on_speech_started(self, **kwargs):
        """Barge-in: cancel TTS when user starts speaking."""
        import json as _json
        if stream_sid:
            asyncio.run_coroutine_threadsafe(
                websocket.send_text(
                    _json.dumps({"event": "clear", "streamSid": stream_sid})
                ),
                loop,
            )
        if stream_sid in active_tts_tasks and not active_tts_tasks[stream_sid].done():
            loop.call_soon_threadsafe(active_tts_tasks[stream_sid].cancel)

    def on_message(self, result, **kwargs):
        """Handle final transcription → LLM → TTS pipeline."""
        sentence = result.channel.alternatives[0].transcript
        if sentence and result.is_final:
            import logging
            conv_logger = logging.getLogger("uvicorn.error")
            conv_logger.info(f"[STT] USER SAID: {sentence}")
            if stream_sid:
                call_logger.call_event(stream_sid, "STT_TRANSCRIPT", sentence[:100])
            # Send user transcript to client for live display
            try:
                asyncio.run_coroutine_threadsafe(websocket.send_json({"event": "user_speech", "text": sentence}), loop)
            except Exception:
                pass
            chat_history.append({"role": "user", "parts": [{"text": sentence}]})

            async def _process_transcript():
                try:
                    import time as _time
                    t_start = _time.time()
                    _last_transcript_time[0] = t_start

                    # Debounce: wait before processing to batch rapid transcripts
                    await asyncio.sleep(_debounce_delay)

                    # If a newer transcript arrived during the wait, skip this one
                    if _last_transcript_time[0] != t_start:
                        import logging
                        logging.getLogger("uvicorn.error").info(f"[DEBOUNCE] Skipping older transcript — newer one pending.")
                        return

                    # Turn guard: skip if another LLM call is already in flight
                    if _llm_lock.locked():
                        import logging
                        logging.getLogger("uvicorn.error").info(f"[TURN_GUARD] Skipping — LLM already processing.")
                        return

                    async with _llm_lock:
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

                        # RAG Retrieval — skip if no knowledge base loaded
                        rag_context = ""
                        if knowledge_collection and knowledge_collection.count() > 0:
                            try:
                                import google.generativeai as gai
                                gai.configure(api_key=os.getenv("GEMINI_API_KEY", "dummy"))
                                loop = asyncio.get_event_loop()
                                res = await loop.run_in_executor(
                                    None,
                                    lambda: gai.embed_content(model="models/text-embedding-004", content=sentence, task_type="retrieval_query")
                                )
                                query_emb = res['embedding']
                                results = knowledge_collection.query(query_embeddings=[query_emb], n_results=2)
                                if results and results.get('documents') and results['documents'][0]:
                                    docs = results['documents'][0]
                                    rag_context = "\n[KNOWLEDGE BASE RELEVANT INFO]:\n" + "\n---\n".join(docs)
                            except Exception as e:
                                print(f"RAG error: {e}")

                        t_pre_llm = _time.time()
                        final_system_instruction = dynamic_context + rag_context

                        try:
                            import llm_provider
                            response_text = await llm_provider.generate_response(
                                chat_history=chat_history,
                                system_instruction=final_system_instruction,
                                max_tokens=150,
                            )
                            t_post_llm = _time.time()

                            chat_history.append(
                                {"role": "model", "parts": [{"text": response_text}]}
                            )
                            conv_logger.info(f"[LLM] AI RESPONSE: {response_text[:200]}")
                            # Send AI response transcript to client for live display
                            try:
                                await websocket.send_json({"event": "llm_response", "text": response_text})
                            except Exception:
                                pass
                            if stream_sid:
                                call_logger.call_event(stream_sid, "LLM_RESPONSE", response_text[:100], llm_time_s=round(t_post_llm - t_pre_llm, 3))

                            if stream_sid:
                                for monitor in monitor_connections.get(stream_sid, set()):
                                    try:
                                        await monitor.send_json({"type": "transcript", "role": "agent", "text": response_text})
                                    except Exception:
                                        pass
                        except Exception as e:
                            import traceback
                            conv_logger.error(f"Error fetching LLM response: {e}")
                            conv_logger.error(traceback.format_exc())
                            return

                        if stream_sid:
                            import re
                            clean_text = re.sub(r'[\*\_\#\`\~\>\|]', '', response_text)
                            clean_text = re.sub(r'\[([^\]]+)\]\([^)]+\)', r'\1', clean_text)
                            clean_text = clean_text.strip()
                            conv_logger.info(f"TIMING: pre_llm={t_pre_llm - t_start:.2f}s, llm={t_post_llm - t_pre_llm:.2f}s, total_to_tts={_time.time() - t_start:.2f}s")
                            # Cancel any still-running TTS before starting new one
                            if stream_sid in active_tts_tasks and not active_tts_tasks[stream_sid].done():
                                active_tts_tasks[stream_sid].cancel()
                                try:
                                    await active_tts_tasks[stream_sid]
                                except (asyncio.CancelledError, Exception):
                                    pass
                            active_tts_tasks[stream_sid] = asyncio.create_task(
                                synthesize_and_send_audio(clean_text, stream_sid, websocket, _tts_provider_override, _tts_voice_override, _tts_language_override)
                            )
                except Exception as _crash:
                    import logging
                    import traceback
                    logging.getLogger("uvicorn.error").error(f"[SYSTEM FATAL] _process_transcript SILENT CRASH: {_crash}")
                    logging.getLogger("uvicorn.error").error(traceback.format_exc())

            asyncio.run_coroutine_threadsafe(_process_transcript(), loop)

    dg_connection.on(LiveTranscriptionEvents.SpeechStarted, on_speech_started)
    dg_connection.on(LiveTranscriptionEvents.Transcript, on_message)

    dg_connection.start(
        LiveOptions(
            model="nova-3",
            language="hi",
            encoding="linear16",
            sample_rate=8000,
            channels=1,
            endpointing=300,
            interim_results=True,
            utterance_end_ms=1000,
            vad_events=True,
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
                ws_logger.error(f"[WS RECV ERROR] Connection lost: {e}")
                break

            if msg.get("type") == "websocket.disconnect":
                ws_logger.info(f"[WS DISCONNECT] Client sent disconnect frame, sid={stream_sid}")
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
                    ws_logger.info(f"[WS] Exotel binary stream started, sid={stream_sid}")
                    call_logger.call_event(stream_sid, "WS_CONNECTED", f"name={lead_name}, phone={lead_phone}")
                    _tts_recording_buffers[stream_sid] = []

                # Send greeting on first audio frame
                if not greeting_sent:
                    greeting_sent = True
                    greeting_text = f"हैलो {lead_name} जी? मैं AdsGPT से बात कर रहा हूँ, आपने हमारे AI platform के regarding enquiry की थी?"
                    # Add greeting to chat history so LLM knows what it already said
                    chat_history.append({"role": "model", "parts": [{"text": greeting_text}]})
                    ws_logger.info(f"[GREETING] Sending greeting for {lead_name}")
                    call_logger.call_event(stream_sid, "GREETING_SENT", f"to={lead_name}")
                    active_tts_tasks[stream_sid] = asyncio.create_task(
                        synthesize_and_send_audio(
                            greeting_text,
                            stream_sid,
                            websocket,
                            _tts_provider_override,
                            _tts_voice_override,
                            _tts_language_override,
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
                    if stream_sid.startswith("web_sim_"):
                        is_exotel_stream = False
                        ws_logger.info(f"[BROWSER SIM] Detected web simulator stream, sid={stream_sid}")
                    elif data.get("stream_sid"):
                        is_exotel_stream = True
                    ws_logger.info(f"Stream started: sid={stream_sid}, exotel={is_exotel_stream}")
                    twilio_websockets[stream_sid] = websocket
                    monitor_connections[stream_sid] = set()
                    whisper_queues[stream_sid] = []
                    takeover_active[stream_sid] = False
                    _tts_recording_buffers[stream_sid] = []

                    if not greeting_sent:
                        greeting_sent = True
                        ws_logger.info(f"GREETING: Triggering TTS greeting for stream {stream_sid}")
                        active_tts_tasks[stream_sid] = asyncio.create_task(
                            synthesize_and_send_audio(
                                f"हैलो {lead_name} जी? मैं AdsGPT से बात कर रहा हूँ, आपने हमारे AI platform के regarding enquiry की थी?",
                                stream_sid,
                                websocket,
                                _tts_provider_override,
                                _tts_voice_override,
                                _tts_language_override,
                            )
                        )
                elif data.get("event") == "media":
                    raw_audio = base64.b64decode(data["media"]["payload"])
                    dg_connection.send(raw_audio)
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
                    _tts_recording_buffers.setdefault(stream_sid, [])
                    if not greeting_sent:
                        greeting_sent = True
                        active_tts_tasks[stream_sid] = asyncio.create_task(
                            synthesize_and_send_audio(
                                f"हैलो {lead_name} जी? मैं AdsGPT से बात कर रहा हूँ, आपने हमारे AI platform के regarding enquiry की थी?",
                                stream_sid,
                                websocket,
                                _tts_provider_override,
                                _tts_voice_override,
                                _tts_language_override,
                            )
                        )
    except Exception as e:
        import logging as _log
        _log.getLogger("uvicorn.error").error(f"[WS] Error in media stream: {e}")
        if stream_sid:
            call_logger.call_event(stream_sid, "WS_ERROR", str(e))
    finally:
        import logging as _flog
        _flog.getLogger("uvicorn.error").info(f"[WS CLOSED] sid={stream_sid}, turns={len(chat_history)}, exotel={is_exotel_stream}")
        if stream_sid:
            call_logger.call_event(stream_sid, "WS_DISCONNECTED", f"turns={len(chat_history)}")
            call_logger.end_call(stream_sid)
            # Save transcript to DB for CRM review
            if _call_lead_id and chat_history:
                try:
                    import json as _json
                    import time as _t
                    transcript_turns = []
                    for msg in chat_history:
                        role = "AI" if msg.get("role") == "model" else "User"
                        text = ""
                        parts = msg.get("parts", [])
                        if parts and isinstance(parts[0], dict):
                            text = parts[0].get("text", "")
                        elif parts and isinstance(parts[0], str):
                            text = parts[0]
                        if text:
                            transcript_turns.append({"role": role, "text": text})
                    
                    # Fetch/save recording URL
                    recording_url = None
                    if _exotel_call_sid:
                        try:
                            import logging as _rlog
                            _rlog.getLogger("uvicorn.error").info(f"[RECORDING] Fetching for SID: {_exotel_call_sid}")
                            creds = f"{EXOTEL_API_KEY}:{EXOTEL_API_TOKEN}"
                            auth_b64 = base64.b64encode(creds.encode()).decode()
                            rec_url = f"https://api.exotel.com/v1/Accounts/{EXOTEL_ACCOUNT_SID}/Calls/{_exotel_call_sid}/Recording"
                            async with httpx.AsyncClient(timeout=10.0) as _hc:
                                rec_resp = await _hc.get(rec_url, headers={"Authorization": f"Basic {auth_b64}"})
                            if rec_resp.status_code == 200:
                                rec_data = rec_resp.json()
                                recording_url = rec_data.get("Recording", {}).get("RecordingUrl") or rec_data.get("RecordingUrl")
                                _rlog.getLogger("uvicorn.error").info(f"[RECORDING] Got URL: {recording_url}")
                            else:
                                _rlog.getLogger("uvicorn.error").warning(f"[RECORDING] Exotel returned {rec_resp.status_code}: {rec_resp.text[:200]}")
                        except Exception as _re:
                            import logging as _rlog2
                            _rlog2.getLogger("uvicorn.error").error(f"[RECORDING] Error fetching: {_re}")

                    # Save local WAV recording from TTS chunks (browser sim or fallback)
                    _rec_chunks = _tts_recording_buffers.get(stream_sid, [])
                    if not recording_url and (_rec_chunks or _recording_mic_chunks):
                        try:
                            import wave as _wave
                            import time as _t2
                            _rec_dir = os.path.join(os.path.dirname(__file__), "recordings")
                            os.makedirs(_rec_dir, exist_ok=True)
                            _wav_name = f"call_{_call_lead_id}_{int(_call_start_time)}.wav"
                            _wav_path = os.path.join(_rec_dir, _wav_name)
                            
                            # Time-aligned recording at 8kHz 16-bit mono
                            RATE = 8000
                            SAMPLE_BYTES = 2  # 16-bit
                            
                            # Calculate total duration from timestamps
                            all_ts = [ts for ts, _ in _recording_mic_chunks] + [ts for ts, _ in _rec_chunks]
                            if not all_ts:
                                raise ValueError("No audio data")
                            t_start = min(all_ts)
                            t_end = max(all_ts) + 0.5  # add 0.5s buffer
                            total_samples = int((t_end - t_start) * RATE)
                            
                            # Create two buffers: mic and tts
                            import array as _arr
                            mic_buf = _arr.array('h', [0] * total_samples)
                            tts_buf = _arr.array('h', [0] * total_samples)
                            
                            # Place mic chunks at correct positions
                            for ts, chunk_bytes in _recording_mic_chunks:
                                offset = int((ts - t_start) * RATE)
                                n_samples = len(chunk_bytes) // SAMPLE_BYTES
                                for j in range(min(n_samples, total_samples - offset)):
                                    idx = offset + j
                                    if 0 <= idx < total_samples:
                                        val = int.from_bytes(chunk_bytes[j*2:j*2+2], 'little', signed=True)
                                        mic_buf[idx] = max(-32768, min(32767, mic_buf[idx] + val))
                            
                            # Place TTS chunks at correct positions
                            for ts, chunk_bytes in _rec_chunks:
                                offset = int((ts - t_start) * RATE)
                                n_samples = len(chunk_bytes) // SAMPLE_BYTES
                                for j in range(min(n_samples, total_samples - offset)):
                                    idx = offset + j
                                    if 0 <= idx < total_samples:
                                        val = int.from_bytes(chunk_bytes[j*2:j*2+2], 'little', signed=True)
                                        tts_buf[idx] = max(-32768, min(32767, tts_buf[idx] + val))
                            
                            # Mix both buffers
                            mixed = _arr.array('h', [0] * total_samples)
                            for i in range(total_samples):
                                mixed[i] = max(-32768, min(32767, mic_buf[i] + tts_buf[i]))
                            
                            with _wave.open(_wav_path, "wb") as wf:
                                wf.setnchannels(1)
                                wf.setsampwidth(2)
                                wf.setframerate(RATE)
                                wf.writeframes(mixed.tobytes())
                            recording_url = f"/api/recordings/{_wav_name}"
                            import logging as _wavlog
                            _wavlog.getLogger("uvicorn.error").info(f"[RECORDING] Saved local WAV: {_wav_path}")
                        except Exception as _we:
                            import logging as _wavlog2
                            _wavlog2.getLogger("uvicorn.error").error(f"[RECORDING] WAV save error: {_we}")
                    
                    call_duration = round(_t.time() - _call_start_time, 1)
                    if transcript_turns:
                        save_call_transcript(
                            lead_id=_call_lead_id,
                            transcript_json=_json.dumps(transcript_turns, ensure_ascii=False),
                            recording_url=recording_url,
                            call_duration_s=call_duration
                        )
                except Exception as _te:
                    import logging as _tlog
                    import traceback
                    _tlog.getLogger("uvicorn.error").error(f"[TRANSCRIPT] Error saving: {_te}\n{traceback.format_exc()}")
        # Cleanup recording buffer
        if stream_sid and stream_sid in _tts_recording_buffers:
            del _tts_recording_buffers[stream_sid]
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
async def exotel_status_webhook(request: Request):
    form = await request.form()
    status = form.get("Status", form.get("CallStatus", ""))
    detailed_status = form.get("DetailedStatus", "")
    phone = form.get("To", "")
    
    terminal_error = None
    if detailed_status.lower() in ['busy', 'no-answer', 'failed', 'canceled', 'dnd']:
        terminal_error = detailed_status
    elif status.lower() in ['failed', 'busy', 'no-answer', 'canceled']:
        terminal_error = status
        
    if terminal_error:
        from database import log_call_status
        log_call_status(phone, terminal_error, "Exotel Call Error")
        
    return {"status": "ok"}

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

@app.post("/api/auth/signup")
def signup(data: OrgSignup):
    """Create organization + admin user in one step."""
    existing = get_user_by_email(data.email)
    if existing:
        raise HTTPException(status_code=400, detail="Email already registered")
    # Create org first
    org_id = create_organization(data.org_name)
    # Create admin user linked to org
    hashed = get_password_hash(data.password)
    user_id = create_user(data.email, hashed, data.full_name, role="Admin", org_id=org_id)
    # Auto-login: return token
    token = create_access_token(data={"sub": data.email, "org_id": org_id}, expires_delta=timedelta(minutes=ACCESS_TOKEN_EXPIRE_MINUTES))
    return {
        "access_token": token, "token_type": "bearer",
        "user": {"id": user_id, "email": data.email, "full_name": data.full_name, "role": "Admin", "org_id": org_id, "org_name": data.org_name}
    }

@app.post("/api/auth/login")
def login(data: LoginRequest):
    user = get_user_by_email(data.email)
    if not user or not verify_password(data.password, user["password_hash"]):
        raise HTTPException(status_code=401, detail="Incorrect email or password")
    org_id = user.get("org_id")
    org_name = ""
    if org_id:
        orgs = get_all_organizations()
        org = next((o for o in orgs if o["id"] == org_id), None)
        org_name = org["name"] if org else ""
    token = create_access_token(data={"sub": user["email"], "org_id": org_id}, expires_delta=timedelta(minutes=ACCESS_TOKEN_EXPIRE_MINUTES))
    return {
        "access_token": token, "token_type": "bearer",
        "user": {"id": user["id"], "email": user["email"], "full_name": user.get("full_name", ""), "role": user.get("role", "Admin"), "org_id": org_id, "org_name": org_name}
    }

@app.get("/api/auth/me")
async def get_me(current_user: dict = Depends(get_current_user)):
    org_id = current_user.get("org_id")
    org_name = ""
    if org_id:
        orgs = get_all_organizations()
        org = next((o for o in orgs if o["id"] == org_id), None)
        org_name = org["name"] if org else ""
    return {
        "id": current_user["id"], "email": current_user["email"],
        "full_name": current_user.get("full_name", ""), "role": current_user.get("role"),
        "org_id": org_id, "org_name": org_name
    }

mobile_api = APIRouter(prefix="/api/mobile", tags=["Mobile Routes"])

@mobile_api.get("/leads")
def mobile_get_leads(current_user: dict = Depends(get_current_user)):
    return get_all_leads(current_user.get("org_id"))

@mobile_api.post("/leads")
def mobile_create_lead(lead: LeadCreate, current_user: dict = Depends(get_current_user)):
    try:
        lead_id = create_lead(lead.dict(), current_user.get("org_id"))
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
    return get_all_tasks(current_user.get("org_id"))

@mobile_api.put("/tasks/{task_id}/complete")
def mobile_complete_task(task_id: int, current_user: dict = Depends(get_current_user)):
    complete_task(task_id)
    return {"status": "success"}

app.include_router(mobile_api)

# --- RECORDINGS SERVE ---
@app.get("/api/recordings/{filename}")
async def serve_recording(filename: str):
    """Serve WAV/WebM recording files for call playback."""
    import re
    # Sanitize filename to prevent directory traversal
    if not re.match(r'^call_\d+_\d+\.(wav|webm)$', filename):
        from fastapi.responses import JSONResponse
        return JSONResponse(status_code=404, content={"error": "Not found"})
    rec_dir = os.path.join(os.path.dirname(__file__), "recordings")
    file_path = os.path.join(rec_dir, filename)
    if not os.path.isfile(file_path):
        from fastapi.responses import JSONResponse
        return JSONResponse(status_code=404, content={"error": "Recording not found"})
    media_type = "audio/webm" if filename.endswith(".webm") else "audio/wav"
    return FileResponse(file_path, media_type=media_type, filename=filename)

# --- STATIC FILE SERVING (SPA) ---
_dist_dir = os.path.join(os.path.dirname(__file__), "frontend", "dist")
_assets_dir = os.path.join(_dist_dir, "assets")
if os.path.isdir(_assets_dir):
    app.mount("/assets", StaticFiles(directory=_assets_dir), name="static-assets")

@app.get("/{full_path:path}")
async def serve_spa(full_path: str):
    """Catch-all: serve frontend dist files or fall back to index.html for SPA routing."""
    file_path = os.path.join(_dist_dir, full_path)
    if full_path and os.path.isfile(file_path):
        return FileResponse(file_path)
    index = os.path.join(_dist_dir, "index.html")
    if os.path.isfile(index):
        return FileResponse(index)
    return JSONResponse({"detail": "Frontend not built"}, status_code=404)
