import paramiko
import sys

host = "163.227.174.141"
username = "empcloud-development"
password = "Epm^%$#Dv98g89"

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())

script_content = """
import pymysql

try:
    conn = pymysql.connect(
        host='localhost',
        user='callified',
        password='Callified@2026',
        database='callified_ai',
        cursorclass=pymysql.cursors.DictCursor
    )
    cursor = conn.cursor()
    
    # Check leads org_id
    cursor.execute('SHOW COLUMNS FROM leads LIKE "org_id"')
    if not cursor.fetchone():
        print("Adding org_id to leads...")
        cursor.execute("ALTER TABLE leads ADD COLUMN org_id INT NULL DEFAULT NULL")
        cursor.execute("ALTER TABLE leads ADD CONSTRAINT fk_leads_org FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE")
    else:
        print("leads.org_id already exists")
        
    # Check sites org_id
    cursor.execute('SHOW COLUMNS FROM sites LIKE "org_id"')
    if not cursor.fetchone():
        print("Adding org_id to sites...")
        cursor.execute("ALTER TABLE sites ADD COLUMN org_id INT NULL DEFAULT NULL")
        cursor.execute("ALTER TABLE sites ADD CONSTRAINT fk_sites_org FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE")
    else:
        print("sites.org_id already exists")
        
    # Check crm_integrations org_id
    cursor.execute('SHOW COLUMNS FROM crm_integrations LIKE "org_id"')
    if not cursor.fetchone():
        print("Adding org_id to crm_integrations...")
        cursor.execute("ALTER TABLE crm_integrations ADD COLUMN org_id INT NULL DEFAULT NULL")
        cursor.execute("ALTER TABLE crm_integrations ADD CONSTRAINT fk_crm_org FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE")
    else:
        print("crm_integrations.org_id already exists")
        
    conn.commit()
    conn.close()
    print("Database migration successful!")
    
except Exception as e:
    print(f"Migration error: {e}")
"""

try:
    client.connect(host, username=username, password=password, timeout=10)
    print("Connected. Writing migration script...")
    
    # Write the script
    sftp = client.open_sftp()
    with sftp.file('/tmp/migrate_db.py', 'w') as f:
        f.write(script_content)
    sftp.close()
    
    print("Executing migration script...")
    stdin, stdout, stderr = client.exec_command("python3 /tmp/migrate_db.py")
    exit_status = stdout.channel.recv_exit_status()
    
    print("--- STDOUT ---")
    print(stdout.read().decode().strip())
    print("--- STDERR ---")
    print(stderr.read().decode().strip())
    
finally:
    client.close()
