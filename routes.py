"""
routes.py — REST API routes for Callified AI Dialer.
All /api/* endpoints for leads, tasks, reports, organizations, products,
knowledge, CRM integrations, pronunciations, recordings, etc.
"""
import os
import io
import csv
import math
import json
import base64
import httpx
from fastapi import APIRouter, Depends, HTTPException, Request, UploadFile, File, Form, BackgroundTasks
from fastapi.responses import StreamingResponse, JSONResponse, FileResponse
from pydantic import BaseModel
from typing import List, Dict, Any, Optional
import logging
logger = logging.getLogger("uvicorn.error")
import string
import secrets
from auth import get_current_user, create_user_direct
from billing import create_subscription, get_growth_plan_id
from database import (
    get_all_leads, get_lead_by_id, create_lead, update_lead, delete_lead,
    get_all_sites, create_punch, get_site_by_id,
    update_lead_status, get_all_tasks, complete_task, get_reports, get_all_whatsapp_logs,
    upload_document, get_documents_by_lead, get_analytics, search_leads, update_lead_note,
    get_active_crm_integrations, update_crm_last_synced,
    get_all_pronunciations, add_pronunciation, delete_pronunciation,
    save_call_transcript, get_transcripts_by_lead,
    create_organization, get_all_organizations, delete_organization,
    create_product, get_products_by_org, update_product, delete_product, get_product_knowledge_context,
    get_org_custom_prompt, save_org_custom_prompt,
    get_org_voice_settings, save_org_voice_settings,
    save_crm_integration,
    log_knowledge_file, update_knowledge_file_status, get_knowledge_files, delete_knowledge_file,
    create_campaign, get_campaigns_by_org, get_campaign_by_id, update_campaign, delete_campaign,
    add_leads_to_campaign, remove_lead_from_campaign, get_campaign_leads, get_campaign_stats,
    get_campaign_voice_settings, save_campaign_voice_settings,
    get_campaign_call_log,
    get_product_prompt, update_product_prompt,
    save_call_review, get_call_reviews_by_campaign, get_call_review_by_transcript,
    create_demo_request, get_all_demo_requests,
    create_scheduled_call, get_scheduled_calls_by_org, update_scheduled_call_status,
    get_retries_by_campaign,
    get_language_analytics, get_scored_leads,
)
import rag
from email_service import send_email, _wrap_html

# ─── Pydantic Models ────────────────────────────────────────────────────────

class LeadCreate(BaseModel):
    first_name: str
    last_name: str = ""
    phone: str
    source: str = "Dashboard"
    interest: Optional[str] = None

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

class CampaignCreate(BaseModel):
    name: str
    product_id: int
    lead_source: Optional[str] = None

class CampaignUpdate(BaseModel):
    name: Optional[str] = None
    status: Optional[str] = None
    lead_source: Optional[str] = None
    product_id: Optional[int] = None

class CampaignLeadsAssign(BaseModel):
    lead_ids: List[int]

class ScheduledCallCreate(BaseModel):
    lead_id: int
    scheduled_time: str
    campaign_id: Optional[int] = None

# ─── Removed Legacy ChromaDB (Using FAISS instead) ─────────

# ─── Helpers ─────────────────────────────────────────────────────────────────

def haversine_distance(lat1, lon1, lat2, lon2):
    R = 6371e3
    phi1 = lat1 * math.pi / 180
    phi2 = lat2 * math.pi / 180
    delta_phi = (lat2 - lat1) * math.pi / 180
    delta_lambda = (lon2 - lon1) * math.pi / 180
    a = math.sin(delta_phi / 2) ** 2 + math.cos(phi1) * math.cos(phi2) * math.sin(delta_lambda / 2) ** 2
    c = 2 * math.atan2(math.sqrt(a), math.sqrt(1 - a))
    return R * c

class DemoRequestCreate(BaseModel):
    first_name: str
    last_name: str = ""
    phone: str = ""
    email: str
    request_type: str = "demo"

# ─── Router ──────────────────────────────────────────────────────────────────

api_router = APIRouter()

# --- Public endpoints (no auth) ---

@api_router.post("/api/public/demo-request")
def api_create_demo_request(data: DemoRequestCreate):
    rid = create_demo_request(data.first_name, data.last_name, data.phone, data.email, data.request_type)

    # Auto-provision trial accounts
    if data.request_type == "trial":
        try:
            # Generate random 8-char password
            alphabet = string.ascii_letters + string.digits
            password = ''.join(secrets.choice(alphabet) for _ in range(8))

            # Create organization
            org_name = f"{data.first_name}'s Organization"
            org_id = create_organization(org_name)

            # Create user
            full_name = f"{data.first_name} {data.last_name}".strip()
            create_user_direct(data.email, password, full_name, org_id)

            # Create trial subscription (Growth plan, 7-day trial)
            plan_id = get_growth_plan_id()
            if plan_id:
                create_subscription(org_id, plan_id)

            return {
                "ok": True, "id": rid,
                "provisioned": True,
                "credentials": {
                    "username": data.email,
                    "password": password,
                    "login_url": "https://test.callified.ai",
                }
            }
        except ValueError as e:
            # Email already registered — return success without credentials
            logger.warning(f"[TRIAL] Auto-provision failed for {data.email}: {e}")
            return {"ok": True, "id": rid, "provisioned": False, "error": str(e)}
        except Exception as e:
            logger.error(f"[TRIAL] Auto-provision error for {data.email}: {e}")
            return {"ok": True, "id": rid, "provisioned": False}

    return {"ok": True, "id": rid}

@api_router.get("/api/demo-requests")
def api_get_demo_requests(current_user: dict = Depends(get_current_user)):
    return get_all_demo_requests()

@api_router.get("/api/debug/logs")
def api_fetch_logs():
    import subprocess
    try:
        res = subprocess.run(["journalctl", "-u", "callified-ai.service", "-n", "150", "--no-pager"], capture_output=True, text=True)
        return {"logs": res.stdout}
    except Exception as e:
        return {"error": str(e)}

# --- Leads ---

@api_router.get("/api/leads")
def api_get_leads(current_user: dict = Depends(get_current_user)):
    return get_all_leads(current_user.get("org_id"))

@api_router.get("/api/leads/export")
def api_export_leads(current_user: dict = Depends(get_current_user)):
    leads = get_all_leads(current_user.get("org_id"))
    stream = io.StringIO()
    writer = csv.writer(stream)
    writer.writerow(["ID", "First Name", "Last Name", "Phone", "Status", "Source", "Follow Up Note", "Created At"])
    for lead in leads:
        note = lead.get('follow_up_note', '')
        if note:
            note = note.replace('\n', ' ')
        writer.writerow([
            lead.get('id', ''), lead.get('first_name', ''), lead.get('last_name', ''),
            lead.get('phone', ''), lead.get('status', ''), lead.get('source', ''),
            note, lead.get('created_at', '')
        ])
    response = StreamingResponse(iter([stream.getvalue()]), media_type="text/csv")
    response.headers["Content-Disposition"] = "attachment; filename=bdrpl_leads_export.csv"
    return response

@api_router.get("/api/leads/search")
def api_search_leads(q: str = "", current_user: dict = Depends(get_current_user)):
    if not q:
        return get_all_leads(current_user.get("org_id"))
    return search_leads(q, current_user.get("org_id"))

@api_router.post("/api/leads")
def api_create_lead(lead: LeadCreate, current_user: dict = Depends(get_current_user)):
    try:
        lead_id = create_lead(lead.dict(), current_user.get("org_id"))
        return {"status": "success", "id": lead_id}
    except Exception as e:
        msg = str(e)
        if "Duplicate" in msg and "phone" in msg:
            return {"status": "error", "message": "A lead with this phone number already exists."}
        return {"status": "error", "message": msg}

@api_router.post("/api/leads/import-csv")
async def api_import_csv(current_user: dict = Depends(get_current_user), file: UploadFile = File(...)):
    import logging
    _il = logging.getLogger("uvicorn.error")
    org_id = current_user.get("org_id")
    contents = await file.read()
    text = contents.decode("utf-8-sig")
    reader = csv.DictReader(io.StringIO(text))
    imported = 0
    errors = []
    for i, row in enumerate(reader, start=2):
        r = {k.strip().lower(): (v.strip() if v else "") for k, v in row.items() if k}
        first_name = r.get("first_name") or r.get("first name") or r.get("name") or r.get("lead name") or r.get("contact") or ""
        last_name = r.get("last_name") or r.get("last name") or ""
        phone = r.get("phone") or r.get("phone_number") or r.get("phone number") or r.get("mobile") or r.get("contact number") or ""
        source = r.get("source") or r.get("lead source") or r.get("channel") or "CSV Import"
        if not phone:
            continue
        if not first_name:
            errors.append(f"Row {i}: missing name")
            continue
        try:
            create_lead({"first_name": first_name.strip(), "last_name": last_name.strip(),
                         "phone": phone.strip(), "source": source.strip()}, org_id)
            imported += 1
        except Exception as e:
            errors.append(f"Row {i}: {str(e)[:50]}")
    _il.info(f"[CSV IMPORT] {imported} leads imported, {len(errors)} errors")
    return {"status": "success", "imported": imported, "errors": errors[:10]}

@api_router.get("/api/leads/sample-csv")
def api_sample_csv():
    from starlette.responses import Response
    sample = "first_name,last_name,phone,source\nRahul,Sharma,+919876543210,Website\nPriya,Patel,+919876543211,Google Ads\n"
    return Response(content=sample, media_type="text/csv", headers={"Content-Disposition": "attachment; filename=sample_leads.csv"})

@api_router.put("/api/leads/{lead_id}")
def api_update_lead(lead_id: int, lead: LeadCreate, current_user: dict = Depends(get_current_user)):
    try:
        success = update_lead(lead_id, lead.dict(), current_user.get("org_id"))
        if success:
            return {"status": "success", "message": f"Lead {lead_id} updated"}
        return {"status": "error", "message": "Lead not found"}
    except Exception as e:
        msg = str(e)
        if "Duplicate" in msg and "phone" in msg:
            return {"status": "error", "message": "Another lead with this phone number already exists."}
        return {"status": "error", "message": msg}

@api_router.delete("/api/leads/{lead_id}")
def api_delete_lead(lead_id: int, current_user: dict = Depends(get_current_user)):
    try:
        success = delete_lead(lead_id, current_user.get("org_id"))
        if success:
            return {"status": "success", "message": f"Lead {lead_id} deleted"}
        return {"status": "error", "message": "Lead not found"}
    except Exception as e:
        return {"status": "error", "message": str(e)}

@api_router.put("/api/leads/{lead_id}/status")
def api_update_lead_status(lead_id: int, payload: LeadStatusUpdate, current_user: dict = Depends(get_current_user)):
    update_lead_status(lead_id, payload.status)
    return {"status": "success", "message": f"Lead {lead_id} updated to {payload.status}"}

@api_router.post("/api/leads/{lead_id}/notes")
def api_update_lead_note(lead_id: int, payload: NoteCreate, current_user: dict = Depends(get_current_user)):
    update_lead_note(lead_id, payload.note)
    return {"status": "success"}

@api_router.get("/api/leads/{lead_id}/draft-email")
def api_draft_email(lead_id: int, current_user: dict = Depends(get_current_user)):
    lead = get_lead_by_id(lead_id)
    if not lead:
        raise HTTPException(status_code=404, detail="Lead not found")
    note = lead.get("follow_up_note")
    if not note:
        note = "Interested in exploring the latest property listings."
    prompt = f"""
    You are an expert Sales Consultant. 
    The client's name is {lead.get('first_name', 'Client')} {lead.get('last_name', '')}.
    Your latest timeline note says: "{note}".
    Draft a highly professional, persuasive 3-sentence follow-up email based on this note.
    Return ONLY a JSON object with strictly these keys: "subject", "body". Do not wrap in markdown blocks, just return exact JSON.
    """
    from google import genai
    client = genai.Client(api_key=(os.getenv("GEMINI_API_KEY") or "").strip())
    try:
        response = client.models.generate_content(model="gemini-2.5-flash", contents=prompt)
        text = response.text.replace("```json", "").replace("```", "").strip()
        return json.loads(text)
    except Exception:
        return {
            "subject": f"BDRPL Properties - Following up with {lead.get('first_name')}",
            "body": f"Hi {lead.get('first_name')},\n\nI wanted to follow up regarding our recent conversation. Please let me know when you have a moment to discuss further.\n\nBest regards,\nBDRPL Team"
        }

# --- Tasks, Reports, Analytics ---

@api_router.get("/api/tasks")
def api_get_tasks(current_user: dict = Depends(get_current_user)):
    return get_all_tasks(current_user.get("org_id"))

@api_router.put("/api/tasks/{task_id}/complete")
def api_complete_task(task_id: int, current_user: dict = Depends(get_current_user)):
    complete_task(task_id)
    return {"status": "success"}

@api_router.get("/api/reports")
def api_get_reports(current_user: dict = Depends(get_current_user)):
    return get_reports(current_user.get("org_id"))

@api_router.get("/api/analytics")
def api_get_analytics(current_user: dict = Depends(get_current_user)):
    return get_analytics()

@api_router.get("/api/analytics/dashboard")
def api_get_analytics_dashboard(current_user: dict = Depends(get_current_user)):
    """Aggregated analytics dashboard with real metrics from calls, transcripts, and reviews."""
    from database import get_conn
    from datetime import datetime, timedelta
    org_id = current_user.get("org_id")
    conn = get_conn()
    cursor = conn.cursor()
    try:
        # Total calls (via leads belonging to this org)
        cursor.execute("""
            SELECT COUNT(*) as cnt FROM calls c
            JOIN leads l ON c.lead_id = l.id
            WHERE l.org_id = %s
        """, (org_id,))
        total_calls = cursor.fetchone()['cnt'] or 0

        # Calls today
        cursor.execute("""
            SELECT COUNT(*) as cnt FROM calls c
            JOIN leads l ON c.lead_id = l.id
            WHERE l.org_id = %s AND DATE(c.created_at) = CURDATE()
        """, (org_id,))
        calls_today = cursor.fetchone()['cnt'] or 0

        # Calls this week
        cursor.execute("""
            SELECT COUNT(*) as cnt FROM calls c
            JOIN leads l ON c.lead_id = l.id
            WHERE l.org_id = %s AND c.created_at >= DATE_SUB(CURDATE(), INTERVAL 7 DAY)
        """, (org_id,))
        calls_this_week = cursor.fetchone()['cnt'] or 0

        # Avg call duration from call_transcripts
        cursor.execute("""
            SELECT COALESCE(AVG(ct.call_duration_s), 0) as avg_dur
            FROM call_transcripts ct
            JOIN leads l ON ct.lead_id = l.id
            WHERE l.org_id = %s AND ct.call_duration_s > 0
        """, (org_id,))
        avg_call_duration_sec = round(cursor.fetchone()['avg_dur'] or 0, 1)

        # Pickup rate: calls with status != 'no-answer' / total
        pickup_rate = 0
        if total_calls > 0:
            cursor.execute("""
                SELECT COUNT(*) as cnt FROM calls c
                JOIN leads l ON c.lead_id = l.id
                WHERE l.org_id = %s AND c.status != 'no-answer'
            """, (org_id,))
            picked_up = cursor.fetchone()['cnt'] or 0
            pickup_rate = round(picked_up / total_calls, 2)

        # Appointment rate from call_reviews
        cursor.execute("""
            SELECT COUNT(*) as total,
                   SUM(CASE WHEN cr.appointment_booked = 1 THEN 1 ELSE 0 END) as booked
            FROM call_reviews cr
            JOIN campaigns camp ON cr.campaign_id = camp.id
            WHERE camp.org_id = %s
        """, (org_id,))
        rev_row = cursor.fetchone()
        rev_total = rev_row['total'] or 0
        rev_booked = rev_row['booked'] or 0
        appointment_rate = round(rev_booked / rev_total, 2) if rev_total > 0 else 0

        # Sentiment breakdown
        cursor.execute("""
            SELECT cr.customer_sentiment as sentiment, COUNT(*) as cnt
            FROM call_reviews cr
            JOIN campaigns camp ON cr.campaign_id = camp.id
            WHERE camp.org_id = %s AND cr.customer_sentiment IS NOT NULL
            GROUP BY cr.customer_sentiment
        """, (org_id,))
        sentiment_breakdown = {"positive": 0, "neutral": 0, "negative": 0}
        for row in cursor.fetchall():
            s = (row['sentiment'] or '').lower()
            if s in sentiment_breakdown:
                sentiment_breakdown[s] = row['cnt']

        # Top failure reasons
        cursor.execute("""
            SELECT cr.failure_reason as reason, COUNT(*) as cnt
            FROM call_reviews cr
            JOIN campaigns camp ON cr.campaign_id = camp.id
            WHERE camp.org_id = %s AND cr.failure_reason IS NOT NULL AND cr.failure_reason != ''
            GROUP BY cr.failure_reason
            ORDER BY cnt DESC
            LIMIT 10
        """, (org_id,))
        top_failure_reasons = [{"reason": r['reason'], "count": r['cnt']} for r in cursor.fetchall()]

        # Daily calls for last 7 days
        cursor.execute("""
            SELECT DATE(c.created_at) as call_date, COUNT(*) as cnt
            FROM calls c
            JOIN leads l ON c.lead_id = l.id
            WHERE l.org_id = %s AND c.created_at >= DATE_SUB(CURDATE(), INTERVAL 6 DAY)
            GROUP BY DATE(c.created_at)
            ORDER BY call_date
        """, (org_id,))
        daily_map = {str(r['call_date']): r['cnt'] for r in cursor.fetchall()}
        daily_calls = []
        for i in range(6, -1, -1):
            d = (datetime.now() - timedelta(days=i)).strftime('%Y-%m-%d')
            daily_calls.append({"date": d, "count": daily_map.get(d, 0)})

        # Campaign performance
        cursor.execute("""
            SELECT camp.id as campaign_id, camp.name,
                   COUNT(DISTINCT ct.id) as calls,
                   SUM(CASE WHEN cr.appointment_booked = 1 THEN 1 ELSE 0 END) as appointments,
                   COALESCE(AVG(cr.quality_score), 0) as avg_score
            FROM campaigns camp
            LEFT JOIN call_transcripts ct ON ct.campaign_id = camp.id
            LEFT JOIN call_reviews cr ON cr.campaign_id = camp.id AND cr.transcript_id = ct.id
            WHERE camp.org_id = %s
            GROUP BY camp.id, camp.name
            ORDER BY calls DESC
            LIMIT 20
        """, (org_id,))
        campaign_performance = []
        for r in cursor.fetchall():
            campaign_performance.append({
                "campaign_id": r['campaign_id'],
                "name": r['name'],
                "calls": r['calls'] or 0,
                "appointments": r['appointments'] or 0,
                "avg_score": round(float(r['avg_score'] or 0), 1),
            })

        return {
            "total_calls": total_calls,
            "calls_today": calls_today,
            "calls_this_week": calls_this_week,
            "avg_call_duration_sec": float(avg_call_duration_sec),
            "pickup_rate": pickup_rate,
            "appointment_rate": appointment_rate,
            "sentiment_breakdown": sentiment_breakdown,
            "top_failure_reasons": top_failure_reasons,
            "daily_calls": daily_calls,
            "campaign_performance": campaign_performance,
        }
    finally:
        conn.close()

@api_router.get("/api/analytics/languages")
def api_get_language_analytics(current_user: dict = Depends(get_current_user)):
    """Performance metrics broken down by TTS language."""
    org_id = current_user.get("org_id")
    return get_language_analytics(org_id)

@api_router.get("/api/leads/scored")
def api_get_scored_leads(current_user: dict = Depends(get_current_user)):
    """Leads ranked by composite lead score (quality + sentiment + appointment)."""
    org_id = current_user.get("org_id")
    return get_scored_leads(org_id)

@api_router.get("/api/whatsapp")
def api_get_whatsapp(current_user: dict = Depends(get_current_user)):
    return get_all_whatsapp_logs(current_user.get("org_id"))

# --- Sites & Punch ---

@api_router.get("/api/sites")
def api_get_sites(current_user: dict = Depends(get_current_user)):
    return get_all_sites(current_user.get("org_id"))

@api_router.post("/api/punch")
def api_punch(punch: PunchCreate, current_user: dict = Depends(get_current_user)):
    site = get_site_by_id(punch.site_id, current_user.get("org_id"))
    if not site:
        return {"status": "error", "message": "Invalid site."}
    distance_m = haversine_distance(punch.lat, punch.lon, site["lat"], site["lon"])
    punch_status = "Valid" if distance_m <= 500 else "Out of Bounds"
    create_punch(punch.agent_name, punch.site_id, punch.lat, punch.lon, punch_status)
    return {"status": "success", "punch_status": punch_status, "distance_m": round(distance_m, 2), "site_name": site["name"]}

# --- Documents & Transcripts ---

@api_router.post("/api/leads/{lead_id}/documents")
def api_upload_document(lead_id: int, payload: DocumentCreate, current_user: dict = Depends(get_current_user)):
    upload_document(lead_id, payload.file_name, payload.file_url)
    return {"status": "success", "message": f"{payload.file_name} uploaded successfully."}

@api_router.get("/api/leads/{lead_id}/documents")
def api_get_documents(lead_id: int, current_user: dict = Depends(get_current_user)):
    return get_documents_by_lead(lead_id)

@api_router.get("/api/leads/{lead_id}/transcripts")
def api_get_transcripts(lead_id: int, current_user: dict = Depends(get_current_user)):
    return get_transcripts_by_lead(lead_id)

# --- Organizations & Products ---

@api_router.get("/api/organizations")
def api_get_organizations(current_user: dict = Depends(get_current_user)):
    all_orgs = get_all_organizations()
    user_org_id = current_user.get("org_id")
    if user_org_id:
        return [o for o in all_orgs if o["id"] == user_org_id]
    return all_orgs

@api_router.post("/api/organizations")
def api_create_organization(payload: dict):
    org_id = create_organization(payload.get("name", ""))
    return {"status": "ok", "id": org_id}

@api_router.delete("/api/organizations/{org_id}")
def api_delete_organization(org_id: int, current_user: dict = Depends(get_current_user)):
    delete_organization(org_id)
    return {"status": "ok"}

@api_router.put("/api/organizations/{org_id}/timezone")
def api_update_org_timezone(org_id: int, payload: dict, current_user: dict = Depends(get_current_user)):
    tz = payload.get("timezone", "Asia/Kolkata")
    from database import get_conn
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE organizations SET timezone = %s WHERE id = %s", (tz, org_id))
    conn.close()
    return {"status": "ok", "timezone": tz}

@api_router.get("/api/organizations/{org_id}/products")
def api_get_products(org_id: int, current_user: dict = Depends(get_current_user)):
    return get_products_by_org(org_id)

@api_router.post("/api/organizations/{org_id}/products")
def api_create_product(org_id: int, payload: dict, current_user: dict = Depends(get_current_user)):
    pid = create_product(org_id, payload.get("name", ""), payload.get("website_url", ""), payload.get("manual_notes", ""))
    return {"status": "ok", "id": pid}

@api_router.put("/api/products/{product_id}")
def api_update_product(product_id: int, payload: dict, current_user: dict = Depends(get_current_user)):
    fields = {k: v for k, v in payload.items() if k in ('name', 'website_url', 'scraped_info', 'manual_notes')}
    logger.info(f"[API] UPDATE product {product_id}: fields={list(fields.keys())}, user={current_user.get('email')}")
    update_product(product_id, **fields)
    return {"status": "ok"}

@api_router.delete("/api/products/{product_id}")
def api_delete_product_endpoint(product_id: int, current_user: dict = Depends(get_current_user)):
    delete_product(product_id)
    return {"status": "ok"}

@api_router.post("/api/products/{product_id}/scrape")
async def api_scrape_product_website(product_id: int, current_user: dict = Depends(get_current_user)):
    import logging
    logger = logging.getLogger("uvicorn.error")
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
    if url:
        if not url.startswith("http"):
            url = "https://" + url
        try:
            async with httpx.AsyncClient() as client:
                resp = await client.get(url, timeout=15, follow_redirects=True)
                html = resp.text[:15000]
        except Exception as e:
            logger.error(f"[SCRAPE] Failed to fetch {url}: {e}")
    if html:
        scrape_prompt = (
            "You are a product analyst. Given this website HTML, extract the following in a concise format:\n"
            "1. Company name\n2. What the product/service does (2-3 sentences)\n3. Key features (bullet points)\n"
            "4. Target audience\n5. Pricing (if visible)\n6. Contact info\n\n"
            f"Be concise — max 500 words.\n\nWEBSITE HTML:\n{html}"
        )
    else:
        scrape_prompt = (
            f"You are a product analyst. Research and provide detailed information about '{product_name}'.\n"
            f"1. What is {product_name}?\n2. Key features\n3. Target audience\n4. How it works\n"
            f"5. Key benefits\n6. Pricing model (if known)\n\nBe concise — max 500 words."
        )
    try:
        groq_key = os.getenv("GROQ_API_KEY", "")
        if groq_key:
            async with httpx.AsyncClient() as client:
                scrape_resp = await client.post(
                    "https://api.groq.com/openai/v1/chat/completions",
                    headers={"Authorization": f"Bearer {groq_key}", "Content-Type": "application/json"},
                    json={"model": "llama-3.3-70b-versatile", "messages": [{"role": "user", "content": scrape_prompt}], "max_tokens": 1000, "temperature": 0.3},
                    timeout=30
                )
                scraped_info = scrape_resp.json()["choices"][0]["message"]["content"]
        else:
            scraped_info = "No LLM API key configured."
    except Exception as e:
        logger.error(f"[SCRAPE] LLM extraction failed: {e}")
        scraped_info = f"LLM extraction failed: {str(e)}"
    update_product(product_id, scraped_info=scraped_info)
    return {"status": "ok", "scraped_info": scraped_info}

@api_router.get("/api/products/{product_id}/prompt")
def api_get_product_prompt(product_id: int, current_user: dict = Depends(get_current_user)):
    return get_product_prompt(product_id)

@api_router.put("/api/products/{product_id}/prompt")
def api_save_product_prompt(product_id: int, payload: dict, current_user: dict = Depends(get_current_user)):
    persona = payload.get("agent_persona", "")
    flow = payload.get("call_flow_instructions", "")
    logger.info(f"[API] SAVE prompt for product {product_id}: persona={len(persona)} chars, flow={len(flow)} chars, user={current_user.get('email')}")
    update_product_prompt(product_id, persona, flow)
    return {"status": "ok"}

@api_router.post("/api/products/{product_id}/generate-prompt")
async def api_generate_product_prompt(product_id: int, payload: dict, current_user: dict = Depends(get_current_user)):
    """Use AI to generate a system prompt from a specific product's knowledge + persona + call flow."""
    from database import get_products_by_org
    import os
    from google import genai

    # Get the product info
    products = get_products_by_org(current_user.get("org_id"))
    product = next((p for p in products if p["id"] == product_id), None)
    if not product:
        return {"status": "error", "message": "Product not found"}

    product_info = f"Product: {product['name']}"
    if product.get('scraped_info'):
        product_info += f"\n{product['scraped_info']}"
    if product.get('manual_notes'):
        product_info += f"\nManual Notes: {product['manual_notes']}"

    agent_persona = payload.get("agent_persona", product.get("agent_persona", ""))
    call_flow = payload.get("call_flow_instructions", product.get("call_flow_instructions", ""))

    meta_prompt = f"""You are an expert at writing AI sales agent system prompts for phone calls.

Based on the product knowledge, agent persona, and call flow instructions below, generate a complete system prompt in Devanagari Hindi that the AI voice agent will use during outbound sales calls.

The prompt should:
1. Define the agent's persona using the provided personality traits
2. Include the company name and product info for answering questions
3. Follow the provided call flow steps exactly
4. Handle common objections (not interested, wrong number, pricing questions)
5. Keep responses short (1-2 lines per turn — it's a phone call)
6. Use natural conversational Hindi, not formal/bookish
7. End with [HANGUP] command when call should end

PRODUCT KNOWLEDGE:
{product_info}

AGENT PERSONA:
{agent_persona if agent_persona else "Professional, friendly Hindi-speaking sales agent. Calm, confident, never pushy."}

CALL FLOW INSTRUCTIONS:
{call_flow if call_flow else "Standard qualification call — confirm interest, book appointment with senior representative."}

Generate ONLY the system prompt text in Devanagari Hindi. No explanations or meta-text."""

    try:
        client = genai.Client(api_key=(os.getenv("GEMINI_API_KEY") or "").strip())
        response = client.models.generate_content(model="gemini-2.5-flash", contents=meta_prompt)
        generated = response.text.strip()
        return {"status": "success", "prompt": generated}
    except Exception as e:
        return {"status": "error", "message": str(e)}

@api_router.post("/api/products/{product_id}/generate-persona")
async def api_generate_product_persona(product_id: int, payload: dict, current_user: dict = Depends(get_current_user)):
    """Use AI to generate agent persona + call flow from a product's scraped website info."""
    logger.info(f"[API] generate-persona called for product {product_id} by user {current_user.get('email')}")
    from database import get_products_by_org
    import os
    from google import genai

    products = get_products_by_org(current_user.get("org_id"))
    product = next((p for p in products if p["id"] == product_id), None)
    if not product:
        return {"status": "error", "message": "Product not found"}

    product_info = f"Product: {product['name']}"
    if product.get('scraped_info'):
        product_info += f"\nWebsite Info:\n{product['scraped_info']}"
    if product.get('manual_notes'):
        product_info += f"\nManual Notes: {product['manual_notes']}"

    if not product.get('scraped_info') and not product.get('manual_notes'):
        return {"status": "error", "message": "No website info or manual notes found. Scrape the website first or add manual notes."}

    meta_prompt = f"""You are an expert at designing AI sales agent personas and call flows for outbound phone calls.

Based on the product/company information below, generate TWO things:

1. AGENT PERSONA — A detailed personality description for the AI sales agent. Include:
   - Agent name (Indian name), role, speaking style
   - Personality traits (friendly, professional, etc.)
   - How they should handle the product knowledge
   - Language style (conversational Hindi, mix of Hindi-English as natural)
   - Key rules (one question at a time, short responses, never pushy)

2. CALL FLOW INSTRUCTIONS — Step-by-step conversation flow:
   - Step 1: Greeting + introduce self + company
   - Step 2: Confirm they are the right person
   - Step 3: Pitch the product/service briefly
   - Step 4: Gauge interest / handle objections
   - Step 5: Book appointment or next step
   - Step 6: Polite goodbye
   Include what to do for common objections (not interested, busy, wrong number, pricing).

PRODUCT/COMPANY INFORMATION:
{product_info}

Respond in this exact JSON format (no markdown, no code blocks):
{{"agent_persona": "...", "call_flow_instructions": "..."}}

Write in English. Keep each section detailed but practical (300-500 words each)."""

    try:
        client = genai.Client(api_key=(os.getenv("GEMINI_API_KEY") or "").strip())
        response = client.models.generate_content(model="gemini-2.5-flash", contents=meta_prompt)
        text = response.text.strip()
        # Strip markdown code fences if present
        if text.startswith("```"):
            text = text.split("\n", 1)[1] if "\n" in text else text[3:]
        if text.endswith("```"):
            text = text[:-3].strip()
        if text.startswith("json"):
            text = text[4:].strip()
        import json
        result = json.loads(text)
        logger.info(f"[API] generate-persona SUCCESS for product {product_id}: persona={len(result.get('agent_persona',''))} chars, flow={len(result.get('call_flow_instructions',''))} chars")
        return {"status": "success", "agent_persona": result.get("agent_persona", ""), "call_flow_instructions": result.get("call_flow_instructions", "")}
    except json.JSONDecodeError:
        logger.warning(f"[API] generate-persona JSON parse failed for product {product_id}, returning raw text ({len(text)} chars)")
        return {"status": "success", "agent_persona": text, "call_flow_instructions": ""}
    except Exception as e:
        logger.error(f"[API] generate-persona FAILED for product {product_id}: {e}")
        return {"status": "error", "message": str(e)}

# --- System Prompt & Voice Settings ---

@api_router.get("/api/organizations/{org_id}/system-prompt")
def api_get_system_prompt(org_id: int, current_user: dict = Depends(get_current_user)):
    auto_prompt = get_product_knowledge_context(org_id=org_id)
    custom_prompt = get_org_custom_prompt(org_id)
    return {"auto_generated": auto_prompt, "custom_prompt": custom_prompt}

@api_router.put("/api/organizations/{org_id}/system-prompt")
def api_save_system_prompt(org_id: int, payload: dict, current_user: dict = Depends(get_current_user)):
    save_org_custom_prompt(org_id, payload.get("custom_prompt", ""))
    return {"status": "ok"}

@api_router.post("/api/organizations/{org_id}/generate-prompt")
async def api_generate_prompt(org_id: int, payload: dict, current_user: dict = Depends(get_current_user)):
    """Use AI to generate a system prompt from product knowledge + call flow instructions."""
    import httpx
    product_info = get_product_knowledge_context(org_id=org_id)
    agent_persona = payload.get("agent_persona", "")
    call_flow = payload.get("call_flow", "")
    language = payload.get("language", "Hindi")

    meta_prompt = f"""You are an expert at writing AI sales agent system prompts for phone calls.

Based on the product knowledge and call flow instructions below, generate a complete system prompt in Devanagari Hindi that the AI voice agent will use during outbound sales calls.

The prompt should:
1. Define the agent's persona (friendly, professional sales caller)
2. Include the company name and product info
3. Define a clear call flow (greeting → qualification → appointment booking → goodbye)
4. Handle common objections (not interested, wrong number, ask about pricing)
5. Keep responses short (1-2 lines per turn — it's a phone call)
6. Use natural conversational Hindi, not formal/bookish
7. End with [HANGUP] command when call should end

PRODUCT KNOWLEDGE:
{product_info}

AGENT PERSONA:
{agent_persona if agent_persona else "Professional, friendly Hindi-speaking sales agent. Calm, confident, never pushy."}

CALL FLOW INSTRUCTIONS:
{call_flow if call_flow else "Standard qualification call — confirm interest, book appointment with senior representative."}

LANGUAGE: {language}

Generate ONLY the system prompt text. No explanations."""

    try:
        import os
        from google import genai
        client = genai.Client(api_key=(os.getenv("GEMINI_API_KEY") or "").strip())
        response = client.models.generate_content(
            model="gemini-2.5-flash",
            contents=meta_prompt
        )
        generated = response.text.strip()
        return {"status": "success", "prompt": generated}
    except Exception as e:
        return {"status": "error", "message": str(e)}

@api_router.get("/api/organizations/{org_id}/voice-settings")
def api_get_voice_settings(org_id: int, current_user: dict = Depends(get_current_user)):
    return get_org_voice_settings(org_id)

@api_router.put("/api/organizations/{org_id}/voice-settings")
def api_save_voice_settings(org_id: int, payload: dict, current_user: dict = Depends(get_current_user)):
    save_org_voice_settings(org_id, payload.get("tts_provider", "elevenlabs"), payload.get("tts_voice_id", ""), payload.get("tts_language", "hi"))
    return {"status": "ok"}

# --- Recordings ---

@api_router.post("/api/upload-recording")
async def api_upload_recording(current_user: dict = Depends(get_current_user), file: UploadFile = File(...), lead_id: str = Form("")):
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
    if lead_id and lead_id.isdigit():
        import asyncio
        rec_url = f"/api/recordings/{fname}"
        try:
            from database import get_conn
            # Poll up to 3 seconds for the transcript to be created by the ws_handler
            for _ in range(6):
                conn = get_conn()
                cur = conn.cursor()
                cur.execute("SELECT id, recording_url FROM call_transcripts WHERE lead_id = %s ORDER BY id DESC LIMIT 1", (int(lead_id),))
                row = cur.fetchone()
                
                # Check if the latest transcript is missing a recording URL (meaning it's the new one)
                is_missing = False
                if row:
                    if isinstance(row, dict) and not row.get('recording_url'):
                        is_missing = True
                    elif isinstance(row, tuple) and not row[1]:
                        is_missing = True
                        row = {'id': row[0]}
                        
                if is_missing:
                    cur.execute("UPDATE call_transcripts SET recording_url = %s WHERE id = %s", (rec_url, row['id']))
                    conn.commit()
                    conn.close()
                    _ul.info(f"[RECORDING] Updated transcript {row['id']} with URL: {rec_url}")
                    break
                conn.close()
                await asyncio.sleep(0.5)
        except Exception as e:
            _ul.error(f"[RECORDING] DB update error: {e}")
    return {"status": "ok", "url": f"/api/recordings/{fname}"}

@api_router.get("/api/recordings/{filename}")
async def serve_recording(filename: str):
    import re
    if not re.match(r'^(call|exotel)_[\w]+\.(wav|webm|mp3|ogg)$', filename):
        return JSONResponse(status_code=404, content={"error": "Not found"})
    rec_dir = os.path.join(os.path.dirname(__file__), "recordings")
    file_path = os.path.join(rec_dir, filename)
    if not os.path.isfile(file_path):
        return JSONResponse(status_code=404, content={"error": "Recording not found"})
    media_types = {".webm": "audio/webm", ".wav": "audio/wav", ".mp3": "audio/mpeg", ".ogg": "audio/ogg"}
    ext = os.path.splitext(filename)[1]
    media_type = media_types.get(ext, "application/octet-stream")
    return FileResponse(file_path, media_type=media_type, filename=filename)

# --- CRM Integrations ---

@api_router.get("/api/integrations")
def api_get_integrations(current_user: dict = Depends(get_current_user)):
    active = get_active_crm_integrations(current_user.get("org_id"))
    for a in active:
        if a["api_key"] and len(a["api_key"]) > 8:
            a["api_key"] = a["api_key"][:4] + "****" + a["api_key"][-4:]
        elif a["api_key"]:
            a["api_key"] = "****"
    return active

@api_router.post("/api/integrations")
async def create_integration(data: dict, current_user: dict = Depends(get_current_user)):
    provider = data.get("provider")
    credentials = data.get("credentials")
    if not provider or not credentials:
        return JSONResponse(status_code=400, content={"error": "provider and credentials are required"})
    try:
        save_crm_integration(provider, credentials, current_user.get("org_id"))
        return {"status": "success"}
    except Exception as e:
        return JSONResponse(status_code=500, content={"error": str(e)})

# --- Knowledge / RAG ---

def process_uploaded_pdf(filepath: str, org_id: int, filename: str, file_id: int):
    import logging
    _log = logging.getLogger("uvicorn.error")
    try:
        chunks_added = rag.ingest_pdf(filepath, org_id, filename)
        update_knowledge_file_status(file_id, "Active", chunks_added)
        _log.info(f"RAG INGESTION SUCCESS: {filename} mapped to {chunks_added} FAISS chunks.")
    except Exception as e:
        _log.error(f"RAG INGESTION FAILED: {filename} - {e}")
        update_knowledge_file_status(file_id, "Failed", 0)
    finally:
        if os.path.exists(filepath):
            os.remove(filepath)

@api_router.post("/api/knowledge/upload")
async def upload_knowledge(
    background_tasks: BackgroundTasks, 
    file: UploadFile = File(...), 
    current_user: dict = Depends(get_current_user)
):
    if not file.filename.endswith('.pdf'):
        raise HTTPException(status_code=400, detail="Only PDFs are supported.")
        
    org_id = current_user.get("org_id")
    if not org_id:
        raise HTTPException(status_code=400, detail="No organization linked")

    # Save temp file
    temp_dir = os.path.join(os.path.dirname(__file__), "temp_uploads")
    os.makedirs(temp_dir, exist_ok=True)
    temp_path = os.path.join(temp_dir, f"{org_id}_{file.filename}")
    
    contents = await file.read()
    with open(temp_path, "wb") as f:
        f.write(contents)
        
    # Log to DB instantly
    file_id = log_knowledge_file(org_id, file.filename, "Processing", 0)
    
    # Process purely in background using FAISS/ML arrays
    background_tasks.add_task(process_uploaded_pdf, temp_path, org_id, file.filename, file_id)
    
    return {"status": "success", "message": "File is being processed automatically in the background.", "file_id": file_id}

@api_router.get("/api/knowledge")
def api_get_knowledge(current_user: dict = Depends(get_current_user)):
    org_id = current_user.get("org_id")
    return get_knowledge_files(org_id)

@api_router.delete("/api/knowledge/{file_id}")
def api_delete_knowledge(file_id: int, filename: str, current_user: dict = Depends(get_current_user)):
    org_id = current_user.get("org_id")
    # Native pure delete
    rag.remove_file_from_index(filename, org_id)
    delete_knowledge_file(file_id, org_id)
    return {"status": "success"}

# --- Pronunciation Guide ---

@api_router.get("/api/pronunciation")
def get_pronunciations(current_user: dict = Depends(get_current_user)):
    return get_all_pronunciations()

@api_router.post("/api/pronunciation")
async def create_pronunciation_endpoint(request: Request, current_user: dict = Depends(get_current_user)):
    data = await request.json()
    word = data.get("word", "").strip()
    phonetic = data.get("phonetic", "").strip()
    if not word or not phonetic:
        return {"error": "word and phonetic are required"}
    add_pronunciation(word, phonetic)
    return {"status": "ok", "word": word, "phonetic": phonetic}

@api_router.delete("/api/pronunciation/{pronunciation_id}")
def remove_pronunciation(pronunciation_id: int, current_user: dict = Depends(get_current_user)):
    ok = delete_pronunciation(pronunciation_id)
    return {"status": "ok" if ok else "not_found"}

# --- Campaigns ---

@api_router.get("/api/campaigns")
def api_get_campaigns(current_user: dict = Depends(get_current_user)):
    campaigns = get_campaigns_by_org(current_user.get("org_id"))
    # Include stats for each campaign
    for c in campaigns:
        c["stats"] = get_campaign_stats(c["id"])
    return campaigns

@api_router.post("/api/campaigns")
def api_create_campaign(data: CampaignCreate, current_user: dict = Depends(get_current_user)):
    org_id = current_user.get("org_id")
    campaign_id = create_campaign(org_id, data.product_id, data.name, data.lead_source)
    return {"status": "success", "id": campaign_id}

@api_router.get("/api/campaigns/{campaign_id}")
def api_get_campaign(campaign_id: int, current_user: dict = Depends(get_current_user)):
    campaign = get_campaign_by_id(campaign_id)
    if not campaign:
        raise HTTPException(status_code=404, detail="Campaign not found")
    stats = get_campaign_stats(campaign_id)
    voice = get_campaign_voice_settings(campaign_id, current_user.get("org_id"))
    return {**campaign, "stats": stats, "voice_settings": voice}

@api_router.put("/api/campaigns/{campaign_id}")
def api_update_campaign(campaign_id: int, data: CampaignUpdate, current_user: dict = Depends(get_current_user)):
    update_campaign(campaign_id, name=data.name, status=data.status, lead_source=data.lead_source, product_id=data.product_id)
    return {"status": "success"}

@api_router.delete("/api/campaigns/{campaign_id}")
def api_delete_campaign(campaign_id: int, current_user: dict = Depends(get_current_user)):
    ok = delete_campaign(campaign_id)
    return {"status": "ok" if ok else "not_found"}

@api_router.get("/api/campaigns/{campaign_id}/leads")
def api_get_campaign_leads(campaign_id: int, current_user: dict = Depends(get_current_user)):
    return get_campaign_leads(campaign_id)

@api_router.post("/api/campaigns/{campaign_id}/leads")
def api_add_campaign_leads(campaign_id: int, data: CampaignLeadsAssign, current_user: dict = Depends(get_current_user)):
    added = add_leads_to_campaign(campaign_id, data.lead_ids)
    return {"status": "success", "added": added}

@api_router.delete("/api/campaigns/{campaign_id}/leads/{lead_id}")
def api_remove_campaign_lead(campaign_id: int, lead_id: int, current_user: dict = Depends(get_current_user)):
    ok = remove_lead_from_campaign(campaign_id, lead_id)
    return {"status": "ok" if ok else "not_found"}

@api_router.get("/api/campaigns/{campaign_id}/stats")
def api_get_campaign_stats(campaign_id: int, current_user: dict = Depends(get_current_user)):
    return get_campaign_stats(campaign_id)

@api_router.get("/api/campaigns/{campaign_id}/call-log")
def api_get_campaign_call_log(campaign_id: int, current_user: dict = Depends(get_current_user)):
    return get_campaign_call_log(campaign_id)

@api_router.get("/api/campaigns/{campaign_id}/retries")
def api_get_campaign_retries(campaign_id: int, current_user: dict = Depends(get_current_user)):
    """Get the auto-retry queue for a campaign."""
    retries = get_retries_by_campaign(campaign_id)
    # Serialize datetime fields for JSON
    for r in retries:
        for key in ('retry_after', 'created_at'):
            if r.get(key) and hasattr(r[key], 'isoformat'):
                r[key] = r[key].isoformat()
    return retries

@api_router.get("/api/campaigns/{campaign_id}/call-reviews")
def api_get_campaign_call_reviews(campaign_id: int, current_user: dict = Depends(get_current_user)):
    return get_call_reviews_by_campaign(campaign_id)

@api_router.get("/api/transcripts/{transcript_id}/review")
def api_get_transcript_review(transcript_id: int, current_user: dict = Depends(get_current_user)):
    review = get_call_review_by_transcript(transcript_id)
    if not review:
        raise HTTPException(status_code=404, detail="No review found for this transcript")
    return review

@api_router.get("/api/campaigns/{campaign_id}/call-insights")
def api_get_campaign_call_insights(campaign_id: int, current_user: dict = Depends(get_current_user)):
    """Aggregate call reviews into campaign-level insights."""
    reviews = get_call_reviews_by_campaign(campaign_id)
    if not reviews:
        return {"avg_quality_score": 0, "appointment_rate": 0, "total_reviews": 0,
                "sentiment_breakdown": {}, "top_failure_reasons": [], "top_improvements": []}

    total = len(reviews)
    avg_score = round(sum(r.get('quality_score', 0) for r in reviews) / total, 1)
    booked = sum(1 for r in reviews if r.get('appointment_booked'))
    appointment_rate = round((booked / total) * 100, 1)

    sentiments = {}
    for r in reviews:
        s = r.get('customer_sentiment', 'unknown')
        sentiments[s] = sentiments.get(s, 0) + 1

    # Top failure reasons (deduplicate loosely)
    failure_reasons = {}
    for r in reviews:
        reason = r.get('failure_reason')
        if reason and reason.lower() not in ('null', 'none', ''):
            failure_reasons[reason] = failure_reasons.get(reason, 0) + 1
    top_failures = sorted(failure_reasons.items(), key=lambda x: -x[1])[:5]

    # Top improvement suggestions
    improvements = {}
    for r in reviews:
        sug = r.get('prompt_improvement_suggestion')
        if sug and sug.lower() not in ('null', 'none', ''):
            improvements[sug] = improvements.get(sug, 0) + 1
    top_improvements = sorted(improvements.items(), key=lambda x: -x[1])[:5]

    return {
        "avg_quality_score": avg_score,
        "appointment_rate": appointment_rate,
        "total_reviews": total,
        "appointments_booked": booked,
        "sentiment_breakdown": sentiments,
        "top_failure_reasons": [{"reason": r, "count": c} for r, c in top_failures],
        "top_improvements": [{"suggestion": s, "count": c} for s, c in top_improvements],
    }

@api_router.get("/api/campaigns/{campaign_id}/voice-settings")
def api_get_campaign_voice(campaign_id: int, current_user: dict = Depends(get_current_user)):
    return get_campaign_voice_settings(campaign_id, current_user.get("org_id"))

@api_router.put("/api/campaigns/{campaign_id}/voice-settings")
def api_save_campaign_voice(campaign_id: int, payload: dict, current_user: dict = Depends(get_current_user)):
    save_campaign_voice_settings(campaign_id, payload.get("tts_provider"), payload.get("tts_voice_id"), payload.get("tts_language"))
    return {"status": "ok"}

@api_router.post("/api/campaigns/{campaign_id}/import-csv")
async def api_campaign_import_csv(campaign_id: int, current_user: dict = Depends(get_current_user), file: UploadFile = File(...)):
    """Import leads from CSV and add them directly to a campaign."""
    import logging
    _il = logging.getLogger("uvicorn.error")
    org_id = current_user.get("org_id")
    contents = await file.read()
    text = contents.decode("utf-8-sig")
    reader = csv.DictReader(io.StringIO(text))
    imported = 0
    errors = []
    lead_ids = []
    for i, row in enumerate(reader, start=2):
        # Normalize keys: strip whitespace, lowercase for matching
        r = {k.strip().lower(): (v.strip() if v else "") for k, v in row.items() if k}
        first_name = r.get("first_name") or r.get("first name") or r.get("name") or r.get("lead name") or r.get("contact") or ""
        last_name = r.get("last_name") or r.get("last name") or ""
        phone = r.get("phone") or r.get("phone_number") or r.get("phone number") or r.get("mobile") or r.get("contact number") or ""
        source = r.get("source") or r.get("lead source") or r.get("channel") or "Campaign Import"
        if not phone:
            continue  # Skip empty rows silently
        if not first_name:
            errors.append(f"Row {i}: missing name")
            continue
        try:
            lead_id = create_lead({"first_name": first_name.strip(), "last_name": last_name.strip(),
                                   "phone": phone.strip(), "source": source.strip()}, org_id)
            lead_ids.append(lead_id)
            imported += 1
        except Exception as e:
            # If duplicate, try to find existing lead and add it
            if "Duplicate" in str(e):
                from database import get_all_leads
                existing = [l for l in get_all_leads(org_id) if l.get("phone", "").strip() == phone.strip()]
                if existing:
                    lead_ids.append(existing[0]["id"])
                    imported += 1
                else:
                    errors.append(f"Row {i}: {str(e)[:50]}")
            else:
                errors.append(f"Row {i}: {str(e)[:50]}")
    # Add all created/found leads to the campaign
    if lead_ids:
        add_leads_to_campaign(campaign_id, lead_ids)
    _il.info(f"[CAMPAIGN CSV IMPORT] campaign={campaign_id}, imported={imported}, added_to_campaign={len(lead_ids)}, errors={len(errors)}")
    return {"status": "success", "imported": imported, "added_to_campaign": len(lead_ids), "errors": errors[:10]}

# ─── Scheduled Calls ────────────────────────────────────────────────────────

@api_router.post("/api/scheduled-calls")
def api_create_scheduled_call(payload: ScheduledCallCreate, current_user: dict = Depends(get_current_user)):
    org_id = current_user.get("org_id")
    if not org_id:
        raise HTTPException(status_code=400, detail="No organization assigned")
    lead = get_lead_by_id(payload.lead_id)
    if not lead:
        raise HTTPException(status_code=404, detail="Lead not found")
    call_id = create_scheduled_call(org_id, payload.lead_id, payload.scheduled_time, payload.campaign_id)
    return {"status": "success", "id": call_id}

@api_router.get("/api/scheduled-calls")
def api_get_scheduled_calls(current_user: dict = Depends(get_current_user)):
    org_id = current_user.get("org_id")
    if not org_id:
        raise HTTPException(status_code=400, detail="No organization assigned")
    return get_scheduled_calls_by_org(org_id)

@api_router.delete("/api/scheduled-calls/{call_id}")
def api_cancel_scheduled_call(call_id: int, current_user: dict = Depends(get_current_user)):
    update_scheduled_call_status(call_id, "cancelled")
    return {"status": "success", "message": "Scheduled call cancelled"}

# ─── Email Test ─────────────────────────────────────────────────────────────

class TestEmailRequest(BaseModel):
    to: str

@api_router.post("/api/test-email")
def api_test_email(data: TestEmailRequest, current_user: dict = Depends(get_current_user)):
    body = _wrap_html("Test Email", """\
        <h2 style="color:#a5b4fc;margin-top:0;">Test Email</h2>
        <p>This is a test email from Callified AI.</p>
        <p>If you received this, your email configuration is working correctly.</p>
        <p style="color:#94a3b8;margin-top:24px;">Sent by: {}</p>""".format(
            current_user.get("email", "unknown")
        ))
    ok = send_email(data.to, "Callified AI - Test Email", body)
    if ok:
        return {"ok": True, "message": f"Test email sent to {data.to}"}
    raise HTTPException(500, "Failed to send test email. Check SMTP configuration.")

# --- Mobile API ---

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

@mobile_api.get("/analytics")
def mobile_get_analytics(current_user: dict = Depends(get_current_user)):
    return get_analytics()

@mobile_api.post("/punch")
def mobile_punch(punch: PunchCreate, current_user: dict = Depends(get_current_user)):
    return api_punch(punch, current_user)

@mobile_api.get("/tasks")
def mobile_get_tasks(current_user: dict = Depends(get_current_user)):
    return get_all_tasks(current_user.get("org_id"))

@mobile_api.put("/tasks/{task_id}/complete")
def mobile_complete_task(task_id: int, current_user: dict = Depends(get_current_user)):
    complete_task(task_id)
    return {"status": "success"}
