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
            org_id INT,
            first_name VARCHAR(255) NOT NULL,
            last_name VARCHAR(255),
            phone VARCHAR(50) NOT NULL,
            source VARCHAR(255),
            status VARCHAR(50) DEFAULT 'new',
            follow_up_note TEXT,
            external_id VARCHAR(255),
            crm_provider VARCHAR(255),
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            interest VARCHAR(255),
            UNIQUE KEY phone_org (phone, org_id),
            FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE CASCADE
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
            org_id INT,
            name VARCHAR(255) NOT NULL,
            lat DOUBLE NOT NULL,
            lon DOUBLE NOT NULL,
            FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE SET NULL
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
            org_id INT,
            provider VARCHAR(255) NOT NULL,
            credentials TEXT NOT NULL,
            is_active BOOLEAN DEFAULT TRUE,
            last_synced_at VARCHAR(100),
            FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE CASCADE
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS users (
            id INT AUTO_INCREMENT PRIMARY KEY,
            org_id INT,
            email VARCHAR(255) NOT NULL UNIQUE,
            password_hash VARCHAR(255) NOT NULL,
            full_name VARCHAR(255),
            role VARCHAR(50) DEFAULT 'Admin',
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE SET NULL
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

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS call_transcripts (
            id INT AUTO_INCREMENT PRIMARY KEY,
            lead_id INT,
            campaign_id INT,
            transcript JSON NOT NULL,
            recording_url TEXT,
            call_duration_s FLOAT DEFAULT 0,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (lead_id) REFERENCES leads (id) ON DELETE SET NULL
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS organizations (
            id INT AUTO_INCREMENT PRIMARY KEY,
            name VARCHAR(255) NOT NULL,
            timezone VARCHAR(100) DEFAULT 'Asia/Kolkata',
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS products (
            id INT AUTO_INCREMENT PRIMARY KEY,
            org_id INT NOT NULL,
            name VARCHAR(255) NOT NULL,
            website_url TEXT,
            scraped_info LONGTEXT,
            manual_notes LONGTEXT,
            agent_persona LONGTEXT,
            call_flow_instructions LONGTEXT,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE CASCADE
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS campaigns (
            id INT AUTO_INCREMENT PRIMARY KEY,
            org_id INT NOT NULL,
            product_id INT NOT NULL,
            name VARCHAR(255) NOT NULL,
            status VARCHAR(50) DEFAULT 'active',
            tts_provider VARCHAR(50) DEFAULT NULL,
            tts_voice_id VARCHAR(255) DEFAULT NULL,
            tts_language VARCHAR(10) DEFAULT NULL,
            lead_source VARCHAR(100) DEFAULT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE CASCADE,
            FOREIGN KEY (product_id) REFERENCES products (id) ON DELETE CASCADE
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS campaign_leads (
            id INT AUTO_INCREMENT PRIMARY KEY,
            campaign_id INT NOT NULL,
            lead_id INT NOT NULL,
            added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            UNIQUE KEY campaign_lead_unique (campaign_id, lead_id),
            FOREIGN KEY (campaign_id) REFERENCES campaigns (id) ON DELETE CASCADE,
            FOREIGN KEY (lead_id) REFERENCES leads (id) ON DELETE CASCADE
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS knowledge_base (
            id INT AUTO_INCREMENT PRIMARY KEY,
            org_id INT NOT NULL,
            filename VARCHAR(255) NOT NULL,
            chunk_count INT DEFAULT 0,
            status VARCHAR(50) DEFAULT 'Processing',
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE CASCADE
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
    
    # --- Migrations for existing tables ---
    try:
        cursor.execute("ALTER TABLE organizations ADD COLUMN timezone VARCHAR(100) DEFAULT 'Asia/Kolkata'")
    except Exception:
        pass  # Column already exists
    try:
        cursor.execute("ALTER TABLE call_transcripts ADD COLUMN campaign_id INT DEFAULT NULL")
    except Exception:
        pass  # Column already exists

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS call_reviews (
            id INT AUTO_INCREMENT PRIMARY KEY,
            transcript_id INT,
            campaign_id INT,
            lead_id INT,
            quality_score INT DEFAULT 0,
            appointment_booked BOOLEAN DEFAULT FALSE,
            customer_sentiment VARCHAR(50),
            failure_reason TEXT,
            what_went_well TEXT,
            what_went_wrong TEXT,
            prompt_improvement_suggestion TEXT,
            raw_analysis JSON,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (transcript_id) REFERENCES call_transcripts (id) ON DELETE CASCADE
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS wa_channel_config (
            id INT AUTO_INCREMENT PRIMARY KEY,
            org_id INT NOT NULL,
            provider VARCHAR(50) NOT NULL,
            phone_number VARCHAR(50) NOT NULL,
            credentials JSON NOT NULL,
            default_product_id INT DEFAULT NULL,
            is_active BOOLEAN DEFAULT TRUE,
            webhook_verified BOOLEAN DEFAULT FALSE,
            auto_reply_enabled BOOLEAN DEFAULT TRUE,
            welcome_template VARCHAR(255) DEFAULT NULL,
            business_hours_json JSON DEFAULT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
            UNIQUE KEY org_provider_phone (org_id, provider, phone_number),
            FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE CASCADE
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS wa_conversations (
            id INT AUTO_INCREMENT PRIMARY KEY,
            org_id INT NOT NULL,
            channel_config_id INT NOT NULL,
            lead_id INT DEFAULT NULL,
            contact_phone VARCHAR(50) NOT NULL,
            contact_name VARCHAR(255) DEFAULT NULL,
            direction ENUM('inbound','outbound') NOT NULL,
            message_type VARCHAR(50) NOT NULL DEFAULT 'text',
            content TEXT,
            media_url TEXT DEFAULT NULL,
            provider_message_id VARCHAR(255) DEFAULT NULL,
            is_ai_generated BOOLEAN DEFAULT FALSE,
            ai_model VARCHAR(100) DEFAULT NULL,
            status VARCHAR(50) DEFAULT 'sent',
            metadata JSON DEFAULT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            INDEX idx_org_contact (org_id, contact_phone),
            INDEX idx_channel (channel_config_id),
            INDEX idx_lead (lead_id),
            INDEX idx_created (created_at),
            FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE CASCADE,
            FOREIGN KEY (channel_config_id) REFERENCES wa_channel_config (id) ON DELETE CASCADE
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS demo_requests (
            id INT AUTO_INCREMENT PRIMARY KEY,
            first_name VARCHAR(255) NOT NULL,
            last_name VARCHAR(255),
            phone VARCHAR(50),
            email VARCHAR(255) NOT NULL,
            request_type ENUM('demo','trial') NOT NULL DEFAULT 'demo',
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS scheduled_calls (
            id INT AUTO_INCREMENT PRIMARY KEY,
            org_id INT NOT NULL,
            campaign_id INT,
            lead_id INT NOT NULL,
            scheduled_time DATETIME NOT NULL,
            status ENUM('pending','dialing','completed','failed','cancelled') DEFAULT 'pending',
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE CASCADE,
            FOREIGN KEY (lead_id) REFERENCES leads (id) ON DELETE CASCADE,
            INDEX idx_scheduled_pending (status, scheduled_time)
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS call_retries (
            id INT AUTO_INCREMENT PRIMARY KEY,
            org_id INT NOT NULL,
            campaign_id INT,
            lead_id INT NOT NULL,
            attempt_number INT DEFAULT 1,
            max_attempts INT DEFAULT 3,
            retry_after DATETIME NOT NULL,
            status ENUM('pending','dialing','completed','exhausted','cancelled') DEFAULT 'pending',
            last_call_status VARCHAR(50),
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE CASCADE,
            FOREIGN KEY (lead_id) REFERENCES leads (id) ON DELETE CASCADE,
            INDEX idx_retry_pending (status, retry_after)
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS password_reset_tokens (
            id INT AUTO_INCREMENT PRIMARY KEY,
            user_id INT NOT NULL,
            token VARCHAR(255) NOT NULL UNIQUE,
            expires_at DATETIME NOT NULL,
            used BOOLEAN DEFAULT FALSE,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS webhooks (
            id INT AUTO_INCREMENT PRIMARY KEY,
            org_id INT NOT NULL,
            url TEXT NOT NULL,
            events JSON NOT NULL,
            secret VARCHAR(255),
            is_active BOOLEAN DEFAULT TRUE,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE CASCADE
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS webhook_logs (
            id INT AUTO_INCREMENT PRIMARY KEY,
            webhook_id INT NOT NULL,
            event VARCHAR(100) NOT NULL,
            payload JSON,
            response_status INT,
            response_body TEXT,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (webhook_id) REFERENCES webhooks (id) ON DELETE CASCADE
        )
    ''')

    cursor.execute('''
        CREATE TABLE IF NOT EXISTS dnd_numbers (
            id INT AUTO_INCREMENT PRIMARY KEY,
            org_id INT NOT NULL,
            phone VARCHAR(50) NOT NULL,
            source ENUM('manual','ndnc','customer_request') DEFAULT 'manual',
            added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            UNIQUE KEY org_phone (org_id, phone),
            FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE CASCADE,
            INDEX idx_phone (phone)
        )
    ''')

    conn.close()

# ─── Phone Normalization ───────────────────────────────────────────────────

def normalize_phone(phone: str) -> str:
    """Strip +, 91 prefix, spaces, dashes. Keep last 10 digits."""
    phone = phone.replace("+", "").replace(" ", "").replace("-", "")
    # Strip leading country code 91
    if len(phone) > 10 and phone.startswith("91"):
        phone = phone[2:]
    # Keep last 10 digits
    return phone[-10:] if len(phone) >= 10 else phone

# ─── DND Helpers ───────────────────────────────────────────────────────────

def add_dnd_number(org_id: int, phone: str, source: str = 'manual'):
    phone = normalize_phone(phone)
    conn = get_conn()
    cursor = conn.cursor()
    try:
        cursor.execute(
            "INSERT IGNORE INTO dnd_numbers (org_id, phone, source) VALUES (%s, %s, %s)",
            (org_id, phone, source)
        )
    finally:
        conn.close()


def add_dnd_numbers_bulk(org_id: int, phones: list, source: str = 'manual') -> int:
    """Bulk insert phones into DND list. Returns count of newly added numbers."""
    if not phones:
        return 0
    normalized = list({normalize_phone(p) for p in phones if p and p.strip()})
    if not normalized:
        return 0
    conn = get_conn()
    cursor = conn.cursor()
    try:
        values = [(org_id, p, source) for p in normalized]
        cursor.executemany(
            "INSERT IGNORE INTO dnd_numbers (org_id, phone, source) VALUES (%s, %s, %s)",
            values
        )
        return cursor.rowcount
    finally:
        conn.close()


def is_dnd_number(org_id: int, phone: str) -> bool:
    phone = normalize_phone(phone)
    conn = get_conn()
    cursor = conn.cursor()
    try:
        cursor.execute(
            "SELECT 1 FROM dnd_numbers WHERE org_id = %s AND phone = %s LIMIT 1",
            (org_id, phone)
        )
        return cursor.fetchone() is not None
    finally:
        conn.close()


def remove_dnd_number(org_id: int, phone: str):
    phone = normalize_phone(phone)
    conn = get_conn()
    cursor = conn.cursor()
    try:
        cursor.execute(
            "DELETE FROM dnd_numbers WHERE org_id = %s AND phone = %s",
            (org_id, phone)
        )
    finally:
        conn.close()


def get_dnd_count(org_id: int) -> int:
    conn = get_conn()
    cursor = conn.cursor()
    try:
        cursor.execute("SELECT COUNT(*) as cnt FROM dnd_numbers WHERE org_id = %s", (org_id,))
        return cursor.fetchone()['cnt']
    finally:
        conn.close()


def get_dnd_numbers(org_id: int, limit: int = 100, offset: int = 0) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    try:
        cursor.execute(
            "SELECT * FROM dnd_numbers WHERE org_id = %s ORDER BY added_at DESC LIMIT %s OFFSET %s",
            (org_id, limit, offset)
        )
        return cursor.fetchall()
    finally:
        conn.close()


def get_all_leads(org_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM leads WHERE org_id = %s ORDER BY id DESC", (org_id,))
    rows = cursor.fetchall()
    conn.close()
    return rows

def search_leads(query: str, org_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    search_term = f"%{query}%"
    cursor.execute('''
        SELECT * FROM leads 
        WHERE org_id = %s AND (first_name LIKE %s OR last_name LIKE %s OR phone LIKE %s)
        ORDER BY id DESC
    ''', (org_id, search_term, search_term, search_term))
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

def create_lead(data: dict, org_id: Optional[int] = None):
    conn = get_conn()
    cursor = conn.cursor()
    # Auto-populate interest from org's first product if not provided
    interest = data.get('interest')
    if not interest and org_id:
        cursor.execute('SELECT name FROM products WHERE org_id = %s LIMIT 1', (org_id,))
        prod = cursor.fetchone()
        if prod:
            interest = prod['name']
    cursor.execute('''
        INSERT INTO leads (first_name, last_name, phone, source, interest, external_id, crm_provider, org_id)
        VALUES (%s, %s, %s, %s, %s, %s, %s, %s)
    ''', (
        data.get('first_name'), 
        data.get('last_name', ''), 
        data.get('phone'), 
        data.get('source', 'Dashboard'),
        interest,
        data.get('external_id'),
        data.get('crm_provider'),
        org_id
    ))
    last_id = cursor.lastrowid
    conn.close()
    return last_id

def update_lead(lead_id: int, data: dict, org_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    
    if org_id is not None:
        cursor.execute('''
            UPDATE leads SET first_name = %s, last_name = %s, phone = %s, source = %s, interest = COALESCE(%s, interest)
            WHERE id = %s AND org_id = %s
        ''', (
            data.get('first_name'),
            data.get('last_name', ''),
            data.get('phone'),
            data.get('source', 'Dashboard'),
            data.get('interest'),
            lead_id,
            org_id
        ))
    else:
        cursor.execute('''
            UPDATE leads SET first_name = %s, last_name = %s, phone = %s, source = %s, interest = COALESCE(%s, interest)
            WHERE id = %s
        ''', (
            data.get('first_name'),
            data.get('last_name', ''),
            data.get('phone'),
            data.get('source', 'Dashboard'),
            data.get('interest'),
            lead_id
        ))
    
    
    conn.close()
    return True

def delete_lead(lead_id: int, org_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    if org_id is not None:
        cursor.execute("DELETE FROM leads WHERE id = %s AND org_id = %s", (lead_id, org_id))
    else:
        cursor.execute("DELETE FROM leads WHERE id = %s", (lead_id,))
    conn.close()
    return True

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

def log_call_status(phone: str, call_status: str, error_msg: str = ""):
    conn = get_conn()
    cursor = conn.cursor()
    phone_clean = "".join(filter(str.isdigit, str(phone)))
    if len(phone_clean) > 10:
        phone_clean = phone_clean[-10:]

    status_label = f"Call Failed ({call_status})"
    if call_status.lower() in ["completed", "in-progress", "ringing", "answered"]:
        status_label = "Calling..."

    # Only update status — never touch follow_up_note
    # The AI summary is more valuable than "Telecom Status: NO-ANSWER" spam
    cursor.execute("UPDATE leads SET status = %s WHERE phone LIKE %s", (status_label, f"%{phone_clean}%"))

    conn.close()

def get_all_sites(org_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM sites WHERE org_id = %s ORDER BY name", (org_id,))
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

def get_site_by_id(site_id: int, org_id: int) -> Dict:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM sites WHERE id = %s AND org_id = %s", (site_id, org_id))
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

def get_all_tasks(org_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT t.*, l.first_name, l.last_name FROM tasks t JOIN leads l ON t.lead_id = l.id WHERE l.org_id = %s ORDER BY t.status DESC, t.id DESC", (org_id,))
    rows = cursor.fetchall()
    conn.close()
    return rows

def complete_task(task_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE tasks SET status = 'Complete' WHERE id = %s", (task_id,))
    conn.close()
    return True

def get_reports(org_id: int) -> Dict:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT COUNT(*) as cnt FROM leads WHERE org_id = %s", (org_id,))
    total_leads = cursor.fetchone()['cnt']
    cursor.execute("SELECT COUNT(*) as cnt FROM leads WHERE status = 'Closed' AND org_id = %s", (org_id,))
    closed_deals = cursor.fetchone()['cnt']
    cursor.execute("SELECT COUNT(*) as cnt FROM punches p JOIN sites s ON p.site_id = s.id WHERE p.status = 'Valid' AND s.org_id = %s", (org_id,))
    total_punches = cursor.fetchone()['cnt']
    cursor.execute("SELECT COUNT(*) as cnt FROM tasks t JOIN leads l ON t.lead_id = l.id WHERE t.status = 'Pending' AND l.org_id = %s", (org_id,))
    pending_tasks = cursor.fetchone()['cnt']
    conn.close()
    return {
        "total_leads": total_leads,
        "closed_deals": closed_deals,
        "valid_site_punches": total_punches,
        "pending_internal_tasks": pending_tasks
    }

def get_all_whatsapp_logs(org_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT w.*, l.first_name, l.last_name, l.phone 
        FROM whatsapp_logs w 
        JOIN leads l ON w.lead_id = l.id 
        WHERE l.org_id = %s
        ORDER BY w.sent_at DESC
    ''', (org_id,))
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

def get_all_crm_integrations(org_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM crm_integrations WHERE org_id = %s", (org_id,))
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

def get_active_crm_integrations(org_id: int = None) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    if org_id:
        cursor.execute("SELECT * FROM crm_integrations WHERE is_active = 1 AND org_id = %s", (org_id,))
    else:
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

def save_crm_integration(provider: str, credentials: dict, org_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    val = json.dumps(credentials)
    cursor.execute("SELECT id FROM crm_integrations WHERE provider = %s AND org_id = %s", (provider, org_id))
    existing = cursor.fetchone()
    if existing:
        cursor.execute("UPDATE crm_integrations SET credentials = %s, is_active = 1 WHERE provider = %s AND org_id = %s", 
                    (val, provider, org_id))
    else:
        cursor.execute("INSERT INTO crm_integrations (org_id, provider, credentials) VALUES (%s, %s, %s)", 
                    (org_id, provider, val))
    conn.close()
    return True

def update_crm_last_synced(provider: str, sync_time: str):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE crm_integrations SET last_synced_at = %s WHERE provider = %s", (sync_time, provider))
    conn.close()
    return True

# --- USERS & AUTH ---

def create_user(email: str, password_hash: str, full_name: str, role: str = 'Admin', org_id: int = None) -> int:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        INSERT INTO users (email, password_hash, full_name, role, org_id)
        VALUES (%s, %s, %s, %s, %s)
    ''', (email, password_hash, full_name, role, org_id))
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

def create_reset_token(user_id: int, token: str, expires_at) -> int:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        INSERT INTO password_reset_tokens (user_id, token, expires_at)
        VALUES (%s, %s, %s)
    ''', (user_id, token, expires_at))
    last_id = cursor.lastrowid
    conn.close()
    return last_id

def get_valid_reset_token(token: str) -> Optional[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT * FROM password_reset_tokens
        WHERE token = %s AND used = FALSE AND expires_at > NOW()
    ''', (token,))
    row = cursor.fetchone()
    conn.close()
    return row

def mark_token_used(token_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE password_reset_tokens SET used = TRUE WHERE id = %s", (token_id,))
    conn.close()

def update_user_password(user_id: int, password_hash: str):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE users SET password_hash = %s WHERE id = %s", (password_hash, user_id))
    conn.close()

# --- CAMPAIGNS ---

def create_campaign(org_id: int, product_id: int, name: str, lead_source: str = None):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO campaigns (org_id, product_id, name, lead_source) VALUES (%s, %s, %s, %s)",
        (org_id, product_id, name, lead_source)
    )
    last_id = cursor.lastrowid
    conn.close()
    return last_id


def get_campaigns_by_org(org_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT c.*, p.name as product_name
        FROM campaigns c
        JOIN products p ON c.product_id = p.id
        WHERE c.org_id = %s
        ORDER BY c.created_at DESC
    ''', (org_id,))
    rows = cursor.fetchall()
    conn.close()
    return rows


def get_campaign_by_id(campaign_id: int) -> Dict:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT c.*, p.name as product_name
        FROM campaigns c
        JOIN products p ON c.product_id = p.id
        WHERE c.id = %s
    ''', (campaign_id,))
    row = cursor.fetchone()
    conn.close()
    return row


def update_campaign(campaign_id: int, name: str = None, status: str = None, lead_source: str = None, product_id: int = None):
    conn = get_conn()
    cursor = conn.cursor()
    if name:
        cursor.execute("UPDATE campaigns SET name = %s WHERE id = %s", (name, campaign_id))
    if status:
        cursor.execute("UPDATE campaigns SET status = %s WHERE id = %s", (status, campaign_id))
    if lead_source is not None:
        cursor.execute("UPDATE campaigns SET lead_source = %s WHERE id = %s", (lead_source or None, campaign_id))
    if product_id is not None:
        cursor.execute("UPDATE campaigns SET product_id = %s WHERE id = %s", (product_id, campaign_id))
    conn.close()
    return True


def delete_campaign(campaign_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("DELETE FROM campaigns WHERE id = %s", (campaign_id,))
    affected = cursor.rowcount
    conn.close()
    return affected > 0


def add_leads_to_campaign(campaign_id: int, lead_ids: List[int]):
    conn = get_conn()
    cursor = conn.cursor()
    added = 0
    for lid in lead_ids:
        try:
            cursor.execute(
                "INSERT IGNORE INTO campaign_leads (campaign_id, lead_id) VALUES (%s, %s)",
                (campaign_id, lid)
            )
            added += cursor.rowcount
        except Exception:
            pass
    conn.close()
    return added


def remove_lead_from_campaign(campaign_id: int, lead_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "DELETE FROM campaign_leads WHERE campaign_id = %s AND lead_id = %s",
        (campaign_id, lead_id)
    )
    affected = cursor.rowcount
    conn.close()
    return affected > 0


def get_campaign_leads(campaign_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT l.*,
            (SELECT COUNT(*) FROM call_transcripts ct WHERE ct.lead_id = l.id AND ct.campaign_id = cl.campaign_id AND ct.call_duration_s > 5) as transcript_count,
            (SELECT COUNT(*) FROM call_transcripts ct WHERE ct.lead_id = l.id AND ct.campaign_id = cl.campaign_id AND ct.recording_url IS NOT NULL AND ct.recording_url != '') as recording_count,
            (SELECT COUNT(*) FROM call_transcripts ct WHERE ct.lead_id = l.id AND ct.campaign_id = cl.campaign_id) as dial_attempts
        FROM leads l
        JOIN campaign_leads cl ON l.id = cl.lead_id
        WHERE cl.campaign_id = %s
        ORDER BY l.id DESC
    ''', (campaign_id,))
    rows = cursor.fetchall()
    conn.close()
    return rows


def get_campaign_call_log(campaign_id: int) -> List[Dict]:
    """Get Exotel-style call log for all leads in a campaign."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT
            ct.id,
            l.first_name,
            l.last_name,
            l.phone,
            l.source,
            l.status as lead_status,
            ct.call_duration_s,
            ct.recording_url,
            ct.created_at,
            CASE
                WHEN ct.call_duration_s > 30 AND l.status IN ('Summarized', 'Closed') THEN 'Completed'
                WHEN ct.call_duration_s > 5 THEN 'Connected'
                WHEN l.status LIKE 'Call Failed (busy)%%' THEN 'Busy'
                WHEN l.status LIKE 'Call Failed (failed)%%' THEN 'Failed'
                WHEN l.status LIKE 'DND%%' THEN 'DND Blocked'
                ELSE 'No Answer'
            END as outcome
        FROM call_transcripts ct
        JOIN leads l ON ct.lead_id = l.id
        JOIN campaign_leads cl ON l.id = cl.lead_id AND cl.campaign_id = %s
        WHERE ct.campaign_id = %s
        ORDER BY ct.created_at DESC
    ''', (campaign_id, campaign_id))
    rows = cursor.fetchall()
    conn.close()
    return rows


def get_campaign_stats(campaign_id: int) -> Dict:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT COUNT(*) as cnt FROM campaign_leads WHERE campaign_id = %s", (campaign_id,))
    total = cursor.fetchone()['cnt']
    cursor.execute('''
        SELECT COUNT(*) as cnt FROM leads l
        JOIN campaign_leads cl ON l.id = cl.lead_id
        WHERE cl.campaign_id = %s AND l.status NOT IN ('new')
    ''', (campaign_id,))
    called = cursor.fetchone()['cnt']
    cursor.execute('''
        SELECT COUNT(*) as cnt FROM leads l
        JOIN campaign_leads cl ON l.id = cl.lead_id
        WHERE cl.campaign_id = %s AND l.status IN ('Warm', 'Summarized', 'Closed')
    ''', (campaign_id,))
    qualified = cursor.fetchone()['cnt']
    cursor.execute('''
        SELECT COUNT(*) as cnt FROM leads l
        JOIN campaign_leads cl ON l.id = cl.lead_id
        WHERE cl.campaign_id = %s AND l.status IN ('Summarized', 'Closed')
    ''', (campaign_id,))
    appointments = cursor.fetchone()['cnt']
    conn.close()
    return {"total": total, "called": called, "qualified": qualified, "appointments": appointments}


def get_campaign_voice_settings(campaign_id: int, org_id: int = None) -> Dict:
    """Get voice settings for a campaign, falling back to org defaults."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT tts_provider, tts_voice_id, tts_language, org_id FROM campaigns WHERE id = %s", (campaign_id,))
    row = cursor.fetchone()
    if not row:
        conn.close()
        return {}
    # If campaign has its own settings, use them
    if row.get('tts_provider') and row.get('tts_voice_id'):
        conn.close()
        return {"tts_provider": row['tts_provider'], "tts_voice_id": row['tts_voice_id'], "tts_language": row.get('tts_language', 'hi')}
    # Fall back to org settings
    _org = org_id or row.get('org_id')
    if _org:
        result = get_org_voice_settings(_org)
        conn.close()
        return result
    conn.close()
    return {}


def save_campaign_voice_settings(campaign_id: int, tts_provider: str, tts_voice_id: str, tts_language: str):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "UPDATE campaigns SET tts_provider = %s, tts_voice_id = %s, tts_language = %s WHERE id = %s",
        (tts_provider or None, tts_voice_id or None, tts_language or None, campaign_id)
    )
    conn.close()
    return True


def get_product_context_for_campaign(campaign_id: int) -> dict:
    """Get the specific product's knowledge + persona + call flow for a campaign.
    Returns dict with keys: product_ctx, agent_persona, call_flow_instructions, product_name, org_name"""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT p.name, p.scraped_info, p.manual_notes, p.agent_persona, p.call_flow_instructions, o.name as org_name
        FROM campaigns c
        JOIN products p ON c.product_id = p.id
        JOIN organizations o ON c.org_id = o.id
        WHERE c.id = %s
    ''', (campaign_id,))
    row = cursor.fetchone()
    conn.close()
    if not row:
        return {"product_ctx": "", "agent_persona": "", "call_flow_instructions": "", "product_name": "", "org_name": ""}
    info = f"Product: {row['name']} (by {row['org_name']})"
    if row.get('scraped_info'):
        info += f" — {row['scraped_info']}"
    if row.get('manual_notes'):
        info += f" | Admin notes: {row['manual_notes']}"
    product_ctx = "\n\n[PRODUCT KNOWLEDGE - Yeh information use karo jab user product ke baare mein puchhe]:\n" + info
    return {
        "product_ctx": product_ctx,
        "agent_persona": row.get("agent_persona") or "",
        "call_flow_instructions": row.get("call_flow_instructions") or "",
        "product_name": row.get("name", ""),
        "org_name": row.get("org_name", ""),
    }


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


# ─── Call Transcripts ───

def save_call_transcript(lead_id, transcript_json: str, recording_url: str = None, call_duration_s: float = 0, campaign_id: int = None):
    """Save a complete call transcript for a lead."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO call_transcripts (lead_id, campaign_id, transcript, recording_url, call_duration_s) VALUES (%s, %s, %s, %s, %s)",
        (lead_id, campaign_id, transcript_json, recording_url, call_duration_s)
    )
    conn.close()
    return cursor.lastrowid

def get_transcripts_by_lead(lead_id: int):
    """Get all call transcripts for a specific lead."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "SELECT id, lead_id, transcript, recording_url, call_duration_s, created_at FROM call_transcripts WHERE lead_id = %s ORDER BY created_at DESC",
        (lead_id,)
    )
    rows = cursor.fetchall()
    conn.close()
    import json
    result = []
    for r in rows:
        t = r.copy()
        # Parse transcript JSON string back to list
        if isinstance(t['transcript'], str):
            try:
                t['transcript'] = json.loads(t['transcript'])
            except Exception:
                pass
        result.append(t)
    return result

# ─── Organizations & Products ───

def create_organization(name: str):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("INSERT INTO organizations (name) VALUES (%s)", (name,))
    org_id = cursor.lastrowid
    conn.close()
    return org_id

def get_all_organizations():
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM organizations ORDER BY id DESC")
    rows = cursor.fetchall()
    conn.close()
    return rows

def delete_organization(org_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("DELETE FROM organizations WHERE id = %s", (org_id,))
    conn.close()
    return True

def create_product(org_id: int, name: str, website_url='', manual_notes=''):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO products (org_id, name, website_url, manual_notes) VALUES (%s, %s, %s, %s)",
        (org_id, name, website_url, manual_notes)
    )
    pid = cursor.lastrowid
    conn.close()
    return pid

def get_products_by_org(org_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM products WHERE org_id = %s ORDER BY id DESC", (org_id,))
    rows = cursor.fetchall()
    conn.close()
    return rows

def update_product(product_id: int, **kwargs):
    conn = get_conn()
    cursor = conn.cursor()
    parts, vals = [], []
    for k in ('name', 'website_url', 'scraped_info', 'manual_notes'):
        if k in kwargs and kwargs[k] is not None:
            parts.append(f"{k} = %s"); vals.append(kwargs[k])
    if parts:
        vals.append(product_id)
        cursor.execute(f"UPDATE products SET {', '.join(parts)} WHERE id = %s", vals)
    conn.close()
    return True

def delete_product(product_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("DELETE FROM products WHERE id = %s", (product_id,))
    conn.close()
    return True

def get_all_products():
    """Get all products across all organizations for system prompt."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "SELECT p.*, o.name as org_name FROM products p JOIN organizations o ON p.org_id = o.id ORDER BY o.name, p.name"
    )
    rows = cursor.fetchall()
    conn.close()
    return rows

def get_product_knowledge_context(org_id=None) -> str:
    """Build product knowledge string for injecting into LLM system prompt."""
    products = get_all_products()
    if org_id:
        products = [p for p in products if p.get('org_id') == org_id]
    if not products:
        return ""
    parts = []
    for p in products:
        info = f"Product: {p['name']} (by {p['org_name']})"
        if p.get('scraped_info'):
            info += f" — {p['scraped_info']}"
        if p.get('manual_notes'):
            info += f" | Admin notes: {p['manual_notes']}"
        parts.append(info)
    return "\n\n[PRODUCT KNOWLEDGE - Yeh information use karo jab user product ke baare mein puchhe]:\n" + "\n".join(parts)

def get_product_prompt(product_id: int) -> Dict:
    """Get agent persona + call flow for a specific product."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT agent_persona, call_flow_instructions FROM products WHERE id = %s", (product_id,))
    row = cursor.fetchone()
    conn.close()
    if not row:
        return {}
    return {"agent_persona": row.get("agent_persona") or "", "call_flow_instructions": row.get("call_flow_instructions") or ""}


def update_product_prompt(product_id: int, agent_persona: str, call_flow_instructions: str):
    """Save agent persona + call flow for a product."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "UPDATE products SET agent_persona = %s, call_flow_instructions = %s WHERE id = %s",
        (agent_persona or None, call_flow_instructions or None, product_id)
    )
    conn.close()
    return True


def get_org_custom_prompt(org_id: int) -> str:
    """Get the custom system prompt for an organization, or empty string."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT custom_system_prompt FROM organizations WHERE id = %s", (org_id,))
    row = cursor.fetchone()
    conn.close()
    return (row.get('custom_system_prompt') or '') if row else ''

def save_org_custom_prompt(org_id: int, prompt_text: str):
    """Save a custom system prompt override for an organization."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE organizations SET custom_system_prompt = %s WHERE id = %s", (prompt_text, org_id))
    conn.close()
    return True

def get_org_voice_settings(org_id: int) -> dict:
    """Get TTS provider, voice ID, and language for an organization."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT tts_provider, tts_voice_id, tts_language FROM organizations WHERE id = %s", (org_id,))
    row = cursor.fetchone()
    conn.close()
    if not row:
        return {"tts_provider": "elevenlabs", "tts_voice_id": None, "tts_language": "hi"}
    return {"tts_provider": row.get("tts_provider") or "elevenlabs", "tts_voice_id": row.get("tts_voice_id"), "tts_language": row.get("tts_language") or "hi"}

def save_org_voice_settings(org_id: int, tts_provider: str, tts_voice_id: str, tts_language: str = "hi"):
    """Save TTS voice settings for an organization."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE organizations SET tts_provider = %s, tts_voice_id = %s, tts_language = %s WHERE id = %s", (tts_provider, tts_voice_id, tts_language, org_id))
    conn.close()
    return True

# ─── Knowledge Base (RAG) ───

def log_knowledge_file(org_id: int, filename: str, status: str = 'Processing', chunk_count: int = 0):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("INSERT INTO knowledge_base (org_id, filename, chunk_count, status) VALUES (%s, %s, %s, %s)",
                   (org_id, filename, chunk_count, status))
    fid = cursor.lastrowid
    conn.close()
    return fid

def update_knowledge_file_status(fid: int, status: str, chunk_count: int = 0):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE knowledge_base SET status = %s, chunk_count = %s WHERE id = %s", (status, chunk_count, fid))
    conn.close()
    return True

def get_knowledge_files(org_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM knowledge_base WHERE org_id = %s ORDER BY created_at DESC", (org_id,))
    rows = cursor.fetchall()
    conn.close()
    return rows

def delete_knowledge_file(fid: int, org_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("DELETE FROM knowledge_base WHERE id = %s AND org_id = %s", (fid, org_id))
    affected = cursor.rowcount
    conn.close()
    return affected > 0


# --- CALL REVIEWS ---

def save_call_review(transcript_id: int, campaign_id: int, lead_id: int, analysis: dict):
    """Save a Gemini-generated call quality review."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        INSERT INTO call_reviews (transcript_id, campaign_id, lead_id,
            quality_score, appointment_booked, customer_sentiment,
            failure_reason, what_went_well, what_went_wrong,
            prompt_improvement_suggestion, raw_analysis)
        VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
    ''', (
        transcript_id, campaign_id, lead_id,
        analysis.get('quality_score', 0),
        analysis.get('appointment_booked', False),
        analysis.get('customer_sentiment', 'neutral'),
        analysis.get('failure_reason'),
        analysis.get('what_went_well'),
        analysis.get('what_went_wrong'),
        analysis.get('prompt_improvement_suggestion'),
        json.dumps(analysis, ensure_ascii=False),
    ))
    last_id = cursor.lastrowid
    conn.close()
    return last_id


def get_call_reviews_by_campaign(campaign_id: int) -> List[Dict]:
    """Get all call reviews for a campaign, joined with lead info."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT cr.*, l.first_name, l.last_name, l.phone
        FROM call_reviews cr
        LEFT JOIN leads l ON cr.lead_id = l.id
        WHERE cr.campaign_id = %s
        ORDER BY cr.created_at DESC
    ''', (campaign_id,))
    rows = cursor.fetchall()
    conn.close()
    return rows


def get_call_review_by_transcript(transcript_id: int) -> Optional[Dict]:
    """Get the review for a specific transcript."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM call_reviews WHERE transcript_id = %s", (transcript_id,))
    row = cursor.fetchone()
    conn.close()
    return row


# ─── WhatsApp Channel Config ───

def create_wa_channel_config(org_id: int, provider: str, phone_number: str, credentials: dict, default_product_id: int = None) -> int:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO wa_channel_config (org_id, provider, phone_number, credentials, default_product_id) VALUES (%s, %s, %s, %s, %s)",
        (org_id, provider, phone_number, json.dumps(credentials), default_product_id)
    )
    last_id = cursor.lastrowid
    conn.close()
    return last_id


def get_wa_channel_configs(org_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM wa_channel_config WHERE org_id = %s ORDER BY id DESC", (org_id,))
    rows = cursor.fetchall()
    conn.close()
    for r in rows:
        if isinstance(r.get('credentials'), str):
            try:
                r['credentials'] = json.loads(r['credentials'])
            except Exception:
                pass
    return rows


def get_wa_channel_config_by_id(config_id: int) -> Optional[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM wa_channel_config WHERE id = %s", (config_id,))
    row = cursor.fetchone()
    conn.close()
    if row and isinstance(row.get('credentials'), str):
        try:
            row['credentials'] = json.loads(row['credentials'])
        except Exception:
            pass
    return row


def get_wa_channel_config_by_phone(phone_number: str) -> Optional[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM wa_channel_config WHERE phone_number = %s AND is_active = 1", (phone_number,))
    row = cursor.fetchone()
    conn.close()
    if row and isinstance(row.get('credentials'), str):
        try:
            row['credentials'] = json.loads(row['credentials'])
        except Exception:
            pass
    return row


def update_wa_channel_config(config_id: int, **fields) -> bool:
    conn = get_conn()
    cursor = conn.cursor()
    allowed = ('provider', 'phone_number', 'credentials', 'default_product_id',
               'is_active', 'webhook_verified', 'auto_reply_enabled',
               'welcome_template', 'business_hours_json')
    parts, vals = [], []
    for k in allowed:
        if k in fields:
            v = fields[k]
            if k in ('credentials', 'business_hours_json') and isinstance(v, dict):
                v = json.dumps(v)
            parts.append(f"{k} = %s")
            vals.append(v)
    if parts:
        vals.append(config_id)
        cursor.execute(f"UPDATE wa_channel_config SET {', '.join(parts)} WHERE id = %s", vals)
    conn.close()
    return True


def delete_wa_channel_config(config_id: int) -> bool:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("DELETE FROM wa_channel_config WHERE id = %s", (config_id,))
    affected = cursor.rowcount
    conn.close()
    return affected > 0


def get_wa_channel_configs_by_provider(provider: str) -> List[Dict]:
    """Get all active channel configs for a given provider (across all orgs)."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "SELECT * FROM wa_channel_config WHERE provider = %s AND is_active = 1 ORDER BY id",
        (provider,)
    )
    rows = cursor.fetchall()
    conn.close()
    for r in rows:
        if isinstance(r.get('credentials'), str):
            try:
                r['credentials'] = json.loads(r['credentials'])
            except Exception:
                pass
    return rows


# ─── WhatsApp Conversations ───

def save_wa_message(org_id: int, channel_config_id: int, contact_phone: str, contact_name: str,
                    direction: str, message_type: str, content: str, media_url: str = None,
                    provider_message_id: str = None, is_ai_generated: bool = False,
                    ai_model: str = None, lead_id: int = None, metadata: dict = None) -> int:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        INSERT INTO wa_conversations
            (org_id, channel_config_id, lead_id, contact_phone, contact_name, direction,
             message_type, content, media_url, provider_message_id, is_ai_generated,
             ai_model, metadata)
        VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
    ''', (org_id, channel_config_id, lead_id, contact_phone, contact_name, direction,
          message_type, content, media_url, provider_message_id, is_ai_generated,
          ai_model, json.dumps(metadata) if metadata else None))
    last_id = cursor.lastrowid
    conn.close()
    return last_id


def get_wa_conversations_list(org_id: int) -> List[Dict]:
    """Get unique conversations grouped by contact, with latest message."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT wc.contact_phone, wc.contact_name, wc.lead_id,
               wc.content as last_message, wc.direction as last_direction,
               wc.created_at as last_message_at,
               COUNT(*) as message_count
        FROM wa_conversations wc
        INNER JOIN (
            SELECT contact_phone, MAX(id) as max_id
            FROM wa_conversations
            WHERE org_id = %s
            GROUP BY contact_phone
        ) latest ON wc.id = latest.max_id
        WHERE wc.org_id = %s
        GROUP BY wc.contact_phone, wc.contact_name, wc.lead_id,
                 wc.content, wc.direction, wc.created_at
        ORDER BY wc.created_at DESC
    ''', (org_id, org_id))
    rows = cursor.fetchall()
    conn.close()
    return rows


def get_wa_chat_history(org_id: int, contact_phone: str, limit: int = 50, offset: int = 0) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT * FROM wa_conversations
        WHERE org_id = %s AND contact_phone = %s
        ORDER BY created_at ASC
        LIMIT %s OFFSET %s
    ''', (org_id, contact_phone, limit, offset))
    rows = cursor.fetchall()
    conn.close()
    for r in rows:
        if isinstance(r.get('metadata'), str):
            try:
                r['metadata'] = json.loads(r['metadata'])
            except Exception:
                pass
    return rows


def get_wa_message_by_provider_id(provider_message_id: str) -> Optional[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM wa_conversations WHERE provider_message_id = %s", (provider_message_id,))
    row = cursor.fetchone()
    conn.close()
    return row


def update_wa_message_status(provider_message_id: str, status: str) -> bool:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE wa_conversations SET status = %s WHERE provider_message_id = %s", (status, provider_message_id))
    affected = cursor.rowcount
    conn.close()
    return affected > 0


def link_wa_conversation_to_lead(org_id: int, contact_phone: str, lead_id: int) -> bool:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "UPDATE wa_conversations SET lead_id = %s WHERE org_id = %s AND contact_phone = %s",
        (lead_id, org_id, contact_phone)
    )
    affected = cursor.rowcount
    conn.close()
    return affected > 0


def create_demo_request(first_name: str, last_name: str, phone: str, email: str, request_type: str) -> int:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO demo_requests (first_name, last_name, phone, email, request_type) VALUES (%s, %s, %s, %s, %s)",
        (first_name, last_name or None, phone or None, email, request_type)
    )
    rid = cursor.lastrowid
    conn.close()
    return rid


def get_all_demo_requests() -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM demo_requests ORDER BY created_at DESC")
    rows = cursor.fetchall()
    conn.close()
    return rows


# ─── Scheduled Calls ────────────────────────────────────────────────────────

def create_scheduled_call(org_id: int, lead_id: int, scheduled_time: str, campaign_id: int = None) -> int:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO scheduled_calls (org_id, lead_id, scheduled_time, campaign_id) VALUES (%s, %s, %s, %s)",
        (org_id, lead_id, scheduled_time, campaign_id)
    )
    call_id = cursor.lastrowid
    conn.close()
    return call_id


def get_pending_scheduled_calls() -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT sc.*, l.first_name, l.phone, l.interest, l.source
        FROM scheduled_calls sc
        JOIN leads l ON sc.lead_id = l.id
        WHERE sc.status = 'pending' AND sc.scheduled_time <= NOW()
        ORDER BY sc.scheduled_time ASC
    ''')
    rows = cursor.fetchall()
    conn.close()
    return rows


def update_scheduled_call_status(call_id: int, status: str):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("UPDATE scheduled_calls SET status = %s WHERE id = %s", (status, call_id))
    conn.close()


def get_scheduled_calls_by_org(org_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT sc.*, l.first_name, l.phone
        FROM scheduled_calls sc
        JOIN leads l ON sc.lead_id = l.id
        WHERE sc.org_id = %s
        ORDER BY sc.scheduled_time DESC
    ''', (org_id,))
    rows = cursor.fetchall()
    conn.close()
    return rows


# ─── Call Retries ─────────────────────────────────────────────────────────────

def create_retry(org_id: int, lead_id: int, campaign_id: int = None, last_call_status: str = None,
                 attempt_number: int = 1, max_attempts: int = 3, retry_delay_minutes: int = 120) -> int:
    conn = get_conn()
    cursor = conn.cursor()
    retry_after = datetime.now() + timedelta(minutes=retry_delay_minutes)
    cursor.execute(
        """INSERT INTO call_retries (org_id, campaign_id, lead_id, attempt_number, max_attempts,
           retry_after, status, last_call_status)
           VALUES (%s, %s, %s, %s, %s, %s, 'pending', %s)""",
        (org_id, campaign_id, lead_id, attempt_number, max_attempts, retry_after, last_call_status)
    )
    rid = cursor.lastrowid
    conn.close()
    return rid


def get_pending_retries() -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        """SELECT cr.*, l.first_name, l.last_name, l.phone, l.interest
           FROM call_retries cr
           JOIN leads l ON cr.lead_id = l.id
           WHERE cr.status = 'pending' AND cr.retry_after <= NOW()
           ORDER BY cr.retry_after ASC"""
    )
    rows = cursor.fetchall()
    conn.close()
    return rows


def update_retry_status(retry_id: int, status: str, attempt_number: int = None):
    conn = get_conn()
    cursor = conn.cursor()
    if attempt_number is not None:
        cursor.execute(
            "UPDATE call_retries SET status = %s, attempt_number = %s WHERE id = %s",
            (status, attempt_number, retry_id)
        )
    else:
        cursor.execute(
            "UPDATE call_retries SET status = %s WHERE id = %s",
            (status, retry_id)
        )
    conn.close()


def get_retries_by_campaign(campaign_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        """SELECT cr.*, l.first_name, l.last_name, l.phone
           FROM call_retries cr
           JOIN leads l ON cr.lead_id = l.id
           WHERE cr.campaign_id = %s
           ORDER BY cr.created_at DESC""",
        (campaign_id,)
    )
    rows = cursor.fetchall()
    conn.close()
    return rows


def get_language_analytics(org_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("""
        SELECT c.tts_language AS language,
               COUNT(DISTINCT ct.id) AS total_calls,
               COALESCE(AVG(cr.quality_score), 0) AS avg_score,
               SUM(CASE WHEN cr.appointment_booked = 1 THEN 1 ELSE 0 END) AS appointments,
               COALESCE(AVG(ct.call_duration_s), 0) AS avg_duration
        FROM campaigns c
        JOIN call_transcripts ct ON ct.campaign_id = c.id
        LEFT JOIN call_reviews cr ON cr.transcript_id = ct.id
        WHERE c.org_id = %s AND c.tts_language IS NOT NULL
        GROUP BY c.tts_language
        ORDER BY total_calls DESC
    """, (org_id,))
    rows = cursor.fetchall()
    conn.close()
    result = []
    for r in rows:
        total = r['total_calls'] or 0
        appts = int(r['appointments'] or 0)
        result.append({
            'language': r['language'],
            'total_calls': total,
            'avg_score': round(float(r['avg_score'] or 0), 1),
            'appointments': appts,
            'conversion_rate': round(appts / total * 100, 1) if total > 0 else 0,
            'avg_duration': round(float(r['avg_duration'] or 0), 1),
        })
    return result


def get_scored_leads(org_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("""
        SELECT l.id, l.first_name, l.last_name, l.phone, l.source, l.status,
               cr.quality_score, cr.customer_sentiment, cr.appointment_booked
        FROM leads l
        LEFT JOIN (
            SELECT cr1.*
            FROM call_reviews cr1
            INNER JOIN (
                SELECT lead_id, MAX(id) AS max_id
                FROM call_reviews
                GROUP BY lead_id
            ) cr2 ON cr1.id = cr2.max_id
        ) cr ON cr.lead_id = l.id
        WHERE l.org_id = %s
    """, (org_id,))
    rows = cursor.fetchall()
    conn.close()

    scored = []
    for r in rows:
        # Quality score component: 0-10 scale, weight 40% -> max 40 points
        qs = float(r['quality_score'] or 0)
        quality_component = (qs / 10.0) * 40

        # Sentiment component: weight 30% -> max 30 points
        sentiment = (r['customer_sentiment'] or '').lower()
        if sentiment == 'positive':
            sentiment_component = (3 / 3.0) * 30  # 30
        elif sentiment == 'neutral':
            sentiment_component = (1 / 3.0) * 30  # 10
        elif sentiment == 'negative':
            sentiment_component = 0
        else:
            sentiment_component = 0

        # Appointment component: weight 30% -> max 30 points
        appt_component = 30 if r['appointment_booked'] else 0

        score = round(quality_component + sentiment_component + appt_component)
        score = max(0, min(100, score))

        scored.append({
            'id': r['id'],
            'first_name': r['first_name'],
            'last_name': r['last_name'],
            'phone': r['phone'],
            'source': r['source'],
            'status': r['status'],
            'quality_score': int(qs),
            'sentiment': sentiment or None,
            'appointment_booked': bool(r['appointment_booked']),
            'lead_score': score,
        })

    scored.sort(key=lambda x: x['lead_score'], reverse=True)
    return scored


def has_pending_or_exhausted_retry(lead_id: int) -> Dict:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "SELECT status, attempt_number, max_attempts FROM call_retries WHERE lead_id = %s ORDER BY created_at DESC LIMIT 1",
        (lead_id,)
    )
    row = cursor.fetchone()
    conn.close()
    if not row:
        return {'has_pending': False, 'is_exhausted': False}
    return {
        'has_pending': row['status'] == 'pending',
        'is_exhausted': row['status'] == 'exhausted' or row['attempt_number'] >= row['max_attempts'],
        'attempt_number': row['attempt_number'],
        'max_attempts': row['max_attempts'],
    }


# ─── Webhooks ───────────────────────────────────────────────────────────────

def create_webhook(org_id: int, url: str, events: list, secret: str = None) -> int:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO webhooks (org_id, url, events, secret) VALUES (%s, %s, %s, %s)",
        (org_id, url, json.dumps(events), secret)
    )
    last_id = cursor.lastrowid
    conn.close()
    return last_id


def get_webhooks_by_org(org_id: int) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT id, org_id, url, events, is_active, created_at FROM webhooks WHERE org_id = %s ORDER BY id DESC", (org_id,))
    rows = cursor.fetchall()
    conn.close()
    for r in rows:
        if r.get('events') and isinstance(r['events'], str):
            r['events'] = json.loads(r['events'])
    return rows


def delete_webhook(webhook_id: int):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("DELETE FROM webhooks WHERE id = %s", (webhook_id,))
    conn.close()


def get_active_webhooks_for_event(org_id: int, event: str) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "SELECT * FROM webhooks WHERE org_id = %s AND is_active = TRUE AND JSON_CONTAINS(events, %s)",
        (org_id, json.dumps(event))
    )
    rows = cursor.fetchall()
    conn.close()
    for r in rows:
        if r.get('events') and isinstance(r['events'], str):
            r['events'] = json.loads(r['events'])
    return rows


def log_webhook_delivery(webhook_id: int, event: str, payload: dict, response_status: int, response_body: str):
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO webhook_logs (webhook_id, event, payload, response_status, response_body) VALUES (%s, %s, %s, %s, %s)",
        (webhook_id, event, json.dumps(payload, ensure_ascii=False), response_status, response_body[:2000] if response_body else None)
    )
    conn.close()


def get_webhook_logs(webhook_id: int, limit: int = 50) -> List[Dict]:
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute(
        "SELECT * FROM webhook_logs WHERE webhook_id = %s ORDER BY created_at DESC LIMIT %s",
        (webhook_id, limit)
    )
    rows = cursor.fetchall()
    conn.close()
    for r in rows:
        if r.get('payload') and isinstance(r['payload'], str):
            r['payload'] = json.loads(r['payload'])
    return rows
