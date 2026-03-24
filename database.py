import pymysql
import json
from typing import List, Dict, Optional
from datetime import datetime, timedelta
import random
import os

DB_CONFIG = {
    'host': os.getenv('MYSQL_HOST', 'localhost'),
    'user': os.getenv('MYSQL_USER', 'callified'),
    'password': os.getenv('MYSQL_PASSWORD', 'Callified@2026'),
    'database': os.getenv('MYSQL_DATABASE', 'callified_ai'),
    'charset': 'utf8mb4',
    'cursorclass': pymysql.cursors.DictCursor,
    'autocommit': True
}

def get_conn():
    return pymysql.connect(**DB_CONFIG)

def init_db():
    conn = get_conn()
    cursor = conn.cursor()
    
    cursor.execute('''
        CREATE TABLE IF NOT EXISTS leads (
            id INT AUTO_INCREMENT PRIMARY KEY,
            first_name VARCHAR(255) NOT NULL,
            last_name VARCHAR(255),
            phone VARCHAR(50) NOT NULL UNIQUE,
            source VARCHAR(255),
            status VARCHAR(50) DEFAULT 'new',
            follow_up_note TEXT,
            external_id VARCHAR(255),
            crm_provider VARCHAR(255),
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
    ''')
    
    cursor.execute('''
        CREATE TABLE IF NOT EXISTS calls (
            id INT AUTO_INCREMENT PRIMARY KEY,
            lead_id INT,
            call_sid VARCHAR(255),
            provider VARCHAR(100),
            status VARCHAR(50) DEFAULT 'initiated',
            follow_up_note TEXT,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (lead_id) REFERENCES leads (id) ON DELETE SET NULL
        )
    ''')
    
    cursor.execute('''
        CREATE TABLE IF NOT EXISTS sites (
            id INT AUTO_INCREMENT PRIMARY KEY,
            name VARCHAR(255) NOT NULL,
            lat DOUBLE NOT NULL,
            lon DOUBLE NOT NULL
        )
    ''')
    
    cursor.execute('''
        CREATE TABLE IF NOT EXISTS punches (
            id INT AUTO_INCREMENT PRIMARY KEY,
            agent_name VARCHAR(255),
            site_id INT,
            lat DOUBLE,
            lon DOUBLE,
            status VARCHAR(50),
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (site_id) REFERENCES sites (id) ON DELETE SET NULL
        )
    ''')
    
    cursor.execute('''
        CREATE TABLE IF NOT EXISTS tasks (
            id INT AUTO_INCREMENT PRIMARY KEY,
            lead_id INT,
            department VARCHAR(100) NOT NULL,
            description TEXT NOT NULL,
            status VARCHAR(50) DEFAULT 'Pending',
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (lead_id) REFERENCES leads (id) ON DELETE SET NULL
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS whatsapp_logs (
            id INT AUTO_INCREMENT PRIMARY KEY,
            lead_id INT,
            message TEXT NOT NULL,
            msg_type VARCHAR(100) NOT NULL,
            sent_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (lead_id) REFERENCES leads (id) ON DELETE SET NULL
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS documents (
            id INT AUTO_INCREMENT PRIMARY KEY,
            lead_id INT,
            file_name VARCHAR(255) NOT NULL,
            file_url TEXT NOT NULL,
            uploaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (lead_id) REFERENCES leads (id) ON DELETE SET NULL
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS crm_integrations (
            id INT AUTO_INCREMENT PRIMARY KEY,
            provider VARCHAR(255) NOT NULL UNIQUE,
            credentials TEXT NOT NULL,
            is_active BOOLEAN DEFAULT TRUE,
            last_synced_at VARCHAR(100)
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS users (
            id INT AUTO_INCREMENT PRIMARY KEY,
            email VARCHAR(255) NOT NULL UNIQUE,
            password_hash VARCHAR(255) NOT NULL,
            full_name VARCHAR(255),
            role VARCHAR(50) DEFAULT 'Agent',
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS pronunciation_guide (
            id INT AUTO_INCREMENT PRIMARY KEY,
            word VARCHAR(255) NOT NULL UNIQUE,
            phonetic VARCHAR(255) NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
    ''')
    
    # Insert demo data
    cursor.execute("SELECT count(*) as cnt FROM sites")
    if cursor.fetchone()['cnt'] == 0:
        cursor.execute('''
            INSERT INTO sites (name, lat, lon) 
            VALUES ('BDRPL Kolkata HQ', 22.5726, 88.3639),
                   ('Green Valley Project', 22.5800, 88.4000)
        ''')
    
    # Insert a dummy lead to populate the table for testing
    cursor.execute("SELECT count(*) as cnt FROM leads")
    if cursor.fetchone()['cnt'] == 0:
        try:
            cursor.execute('''
                INSERT INTO leads (first_name, last_name, phone, source)
                VALUES (%s, %s, %s, %s)
            ''', ('Sumit', 'Kumar', '+917406317771', 'Test Entry'))
        except pymysql.IntegrityError:
            pass
    
    conn.close()

def get_all_leads() -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM leads ORDER BY id DESC")
    rows = cursor.fetchall()
    conn.close()
    return rows

def search_leads(query: str) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    search_term = f"%{query}%"
    cursor.execute('''
        SELECT * FROM leads 
        WHERE first_name LIKE %s OR last_name LIKE %s OR phone LIKE %s
        ORDER BY id DESC
    ''', (search_term, search_term, search_term))
    rows = cursor.fetchall()
    conn.close()
    return rows

def get_lead_by_id(lead_id: int) -> Dict:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM leads WHERE id = %s", (lead_id,))
    row = cursor.fetchone()
    conn.close()
    return row

def create_lead(data: dict):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        INSERT INTO leads (first_name, last_name, phone, source, external_id, crm_provider)
        VALUES (%s, %s, %s, %s, %s, %s)
    ''', (
        data.get('first_name'), 
        data.get('last_name', ''), 
        data.get('phone'), 
        data.get('source', 'Dashboard'),
        data.get('external_id'),
        data.get('crm_provider')
    ))
    last_id = cursor.lastrowid
    conn.close()
    return last_id

def update_lead(lead_id: int, data: dict):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        UPDATE leads SET first_name = %s, last_name = %s, phone = %s, source = %s
        WHERE id = %s
    ''', (
        data.get('first_name'),
        data.get('last_name', ''),
        data.get('phone'),
        data.get('source', 'Dashboard'),
        lead_id
    ))
    affected = cursor.rowcount
    conn.close()
    return affected > 0

def delete_lead(lead_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("DELETE FROM leads WHERE id = %s", (lead_id,))
    affected = cursor.rowcount
    conn.close()
    return affected > 0

def update_call_note(call_sid: str, note: str, phone: str = ""):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE calls SET follow_up_note = %s WHERE call_sid = %s", (note, call_sid))
    if cursor.rowcount == 0:
        cursor.execute("INSERT INTO calls (call_sid, follow_up_note) VALUES (%s, %s)", (call_sid, note))
        
    if phone:
        phone_str = str(phone)
        cursor.execute("UPDATE leads SET status = 'Summarized', follow_up_note = %s WHERE phone LIKE %s", (note, f"%{phone_str}%"))
    conn.close()

def get_all_sites() -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM sites ORDER BY name")
    rows = cursor.fetchall()
    conn.close()
    return rows

def create_punch(agent_name: str, site_id: int, lat: float, lon: float, status: str):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        INSERT INTO punches (agent_name, site_id, lat, lon, status)
        VALUES (%s, %s, %s, %s, %s)
    ''', (agent_name, site_id, lat, lon, status))
    conn.close()
    return True

def get_site_by_id(site_id: int) -> Dict:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM sites WHERE id = %s", (site_id,))
    row = cursor.fetchone()
    conn.close()
    return row

# --- WORKFLOW & TASKS ---

def update_lead_note(lead_id: int, note: str):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE leads SET follow_up_note = %s WHERE id = %s", (note, lead_id))
    conn.close()
    return True

def update_lead_status(lead_id: int, status: str):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE leads SET status = %s WHERE id = %s", (status, lead_id))
    
    # Cross-Department Automation Rule
    if status == 'Closed':
        cursor.execute("SELECT COUNT(*) as cnt FROM tasks WHERE lead_id = %s", (lead_id,))
        count = cursor.fetchone()['cnt']
        if count == 0:
            departments = [
                ('Legal', 'Verify Sales Agreement & Land Deeds.'),
                ('Accounts', 'Process Initial Deposit & KYC clearance.'),
                ('Housing Loan', 'Reach out for optional mortgage pre-approval.')
            ]
            for dept, desc in departments:
                cursor.execute('''
                    INSERT INTO tasks (lead_id, department, description)
                    VALUES (%s, %s, %s)
                ''', (lead_id, dept, desc))
    
    # WhatsApp Automation Nudge Rule
    if status == 'Warm':
        cursor.execute("SELECT COUNT(*) as cnt FROM whatsapp_logs WHERE lead_id = %s AND msg_type = 'Brochure'", (lead_id,))
        count = cursor.fetchone()['cnt']
        if count == 0:
            cursor.execute("SELECT * FROM leads WHERE id = %s", (lead_id,))
            lead = cursor.fetchone()
            if lead:
                msg = f"Hi {lead['first_name']}, thanks for your interest! 🏡 Here is the e-brochure for the priority BDRPL properties we discussed: https://bdrpl.com/brochures/latest.pdf. Let us know if you want to schedule a Site Visit!"
                cursor.execute('''
                    INSERT INTO whatsapp_logs (lead_id, message, msg_type)
                    VALUES (%s, %s, %s)
                ''', (lead_id, msg, 'Brochure'))

    conn.close()
    return True

def get_all_tasks() -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT t.*, l.first_name, l.last_name FROM tasks t JOIN leads l ON t.lead_id = l.id ORDER BY t.status DESC, t.id DESC")
    rows = cursor.fetchall()
    conn.close()
    return rows

def complete_task(task_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE tasks SET status = 'Complete' WHERE id = %s", (task_id,))
    conn.close()
    return True

def get_reports() -> Dict:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT COUNT(*) as cnt FROM leads")
    total_leads = cursor.fetchone()['cnt']
    cursor.execute("SELECT COUNT(*) as cnt FROM leads WHERE status = 'Closed'")
    closed_deals = cursor.fetchone()['cnt']
    cursor.execute("SELECT COUNT(*) as cnt FROM punches WHERE status = 'Valid'")
    total_punches = cursor.fetchone()['cnt']
    cursor.execute("SELECT COUNT(*) as cnt FROM tasks WHERE status = 'Pending'")
    pending_tasks = cursor.fetchone()['cnt']
    conn.close()
    return {
        "total_leads": total_leads,
        "closed_deals": closed_deals,
        "valid_site_punches": total_punches,
        "pending_internal_tasks": pending_tasks
    }

def get_all_whatsapp_logs() -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT w.*, l.first_name, l.last_name, l.phone 
        FROM whatsapp_logs w 
        JOIN leads l ON w.lead_id = l.id 
        ORDER BY w.sent_at DESC
    ''')
    rows = cursor.fetchall()
    conn.close()
    return rows

# --- DOCUMENT VAULT ---

def upload_document(lead_id: int, file_name: str, file_url: str):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        INSERT INTO documents (lead_id, file_name, file_url)
        VALUES (%s, %s, %s)
    ''', (lead_id, file_name, file_url))
    conn.close()
    return True

def get_documents_by_lead(lead_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT * FROM documents WHERE lead_id = %s ORDER BY uploaded_at DESC
    ''', (lead_id,))
    rows = cursor.fetchall()
    conn.close()
    return rows

# --- ANALYTICS DASHBOARD ---

def get_analytics() -> List[Dict]:
    """Generates a 7-day trailing visual history seeded loosely on actual aggregate CRM numbers."""
    stats = []
    base_date = datetime.now()
    random.seed(base_date.strftime('%Y-%W'))  # Consistent per week for UI stability
    
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT COUNT(*) as cnt FROM calls")
    real_calls = cursor.fetchone()['cnt'] or 15
    cursor.execute("SELECT COUNT(*) as cnt FROM leads WHERE status = 'Closed'")
    real_closed = cursor.fetchone()['cnt'] or 1
    conn.close()

    for i in range(6, -1, -1):
        day_date = base_date - timedelta(days=i)
        
        if i == 0:
            calls = real_calls
            closed = real_closed
        else:
            calls = max(8, real_calls + random.randint(-12, 18))
            closed = max(0, real_closed + random.randint(-2, 2))
            
        stats.append({
            "day": day_date.strftime('%a'),
            "date": day_date.strftime('%m/%d'),
            "calls": calls,
            "closed": closed
        })
        
    return stats


# --- CRM INTEGRATIONS ---

def get_all_crm_integrations() -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM crm_integrations")
    rows = cursor.fetchall()
    conn.close()
    integrations = []
    for row in rows:
        try:
            creds = json.loads(row["credentials"])
        except:
            creds = {}
        integrations.append({
            "id": row["id"],
            "provider": row["provider"],
            "credentials": creds,
            "is_active": row["is_active"],
            "last_synced_at": row["last_synced_at"]
        })
    return integrations

def get_active_crm_integrations() -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM crm_integrations WHERE is_active = 1")
    rows = cursor.fetchall()
    conn.close()
    integrations = []
    for row in rows:
        try:
            creds = json.loads(row["credentials"])
        except:
            creds = {}
        integrations.append({
            "id": row["id"],
            "provider": row["provider"],
            "credentials": creds,
            "is_active": row["is_active"],
            "last_synced_at": row["last_synced_at"]
        })
    return integrations

def save_crm_integration(provider: str, credentials: dict):
    conn = get_conn()
    cursor = conn.cursor()
    val = json.dumps(credentials)
    cursor.execute("SELECT id FROM crm_integrations WHERE provider = %s", (provider,))
    existing = cursor.fetchone()
    if existing:
        cursor.execute("UPDATE crm_integrations SET credentials = %s, is_active = 1 WHERE provider = %s", 
                    (val, provider))
    else:
        cursor.execute("INSERT INTO crm_integrations (provider, credentials) VALUES (%s, %s)", 
                    (provider, val))
    conn.close()
    return True

def update_crm_last_synced(provider: str, sync_time: str):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE crm_integrations SET last_synced_at = %s WHERE provider = %s", (sync_time, provider))
    conn.close()
    return True

# --- USERS & AUTH ---

def create_user(email: str, password_hash: str, full_name: str, role: str = 'Agent') -> int:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        INSERT INTO users (email, password_hash, full_name, role)
        VALUES (%s, %s, %s, %s)
    ''', (email, password_hash, full_name, role))
    last_id = cursor.lastrowid
    conn.close()
    return last_id

def get_user_by_email(email: str) -> Optional[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM users WHERE email = %s", (email,))
    row = cursor.fetchone()
    conn.close()
    return row

# --- PRONUNCIATION GUIDE ---

def get_all_pronunciations() -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM pronunciation_guide ORDER BY word")
    rows = cursor.fetchall()
    conn.close()
    return rows

def add_pronunciation(word: str, phonetic: str):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT id FROM pronunciation_guide WHERE word = %s", (word,))
    existing = cursor.fetchone()
    if existing:
        cursor.execute("UPDATE pronunciation_guide SET phonetic = %s WHERE word = %s", (phonetic, word))
    else:
        cursor.execute("INSERT INTO pronunciation_guide (word, phonetic) VALUES (%s, %s)", (word, phonetic))
    conn.close()
    return True

def delete_pronunciation(pronunciation_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("DELETE FROM pronunciation_guide WHERE id = %s", (pronunciation_id,))
    affected = cursor.rowcount
    conn.close()
    return affected > 0

def get_pronunciation_context() -> str:
    """Build a pronunciation guide string for injecting into LLM system prompt."""
    rows = get_all_pronunciations()
    if not rows:
        return ""
    lines = [f"'{r['word']}' ko '{r['phonetic']}' bolna hai" for r in rows]
    return "\n[PRONUNCIATION GUIDE - in baat karo toh ye words aise bolo]: " + ", ".join(lines) + "."

