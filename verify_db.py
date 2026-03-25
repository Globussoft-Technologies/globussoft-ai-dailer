import pymysql
import os
from dotenv import load_dotenv

load_dotenv('c:/Users/Admin/gbs-projects/gbs-ai-dialer/.env')

try:
    conn = pymysql.connect(
        host=os.getenv('DB_HOST', 'localhost'),
        user=os.getenv('DB_USER', 'root'),
        password=os.getenv('DB_PASS', ''),
        database=os.getenv('DB_NAME', 'dialer'),
        cursorclass=pymysql.cursors.DictCursor
    )
    cursor = conn.cursor()
    cursor.execute('SHOW COLUMNS FROM leads LIKE "org_id"')
    result = cursor.fetchone()
    print('leads.org_id exists:', result is not None)
    
    cursor.execute('SHOW COLUMNS FROM sites LIKE "org_id"')
    print('sites.org_id exists:', cursor.fetchone() is not None)
    
    cursor.execute('SHOW COLUMNS FROM crm_integrations LIKE "org_id"')
    print('crm_integrations.org_id exists:', cursor.fetchone() is not None)

    conn.close()
    print("DB connection and verification successful.")
except Exception as e:
    print("Database error:", e)
