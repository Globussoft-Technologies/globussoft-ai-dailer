import paramiko

host = "163.227.174.141"
username = "empcloud-development"
password = "rSPa3izkYPtAjCFLa5cqPDpsFvV071KN9u"

print("--- EXECUTING NATIVE DOWNLOAD SCRIPT FOR ADMIN LEAD ---")
client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect(host, username=username, password=password, timeout=10)

script_payload = '''
import asyncio
import os
import sys

sys.path.append('/home/empcloud-development/callified-ai')
from database import get_conn
from main import process_recording

async def test_native():
    print('Initiating process_recording natively...')
    
    # Inject an empty transcript for +917406317771
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute("SELECT id FROM leads WHERE phone LIKE '%7406317771%' ORDER BY id DESC LIMIT 1")
    lead = cursor.fetchone()
    
    if lead:
        cursor.execute("INSERT INTO call_transcripts (lead_id, transcript) VALUES (%s, %s)", (lead['id'], '[]'))
        conn.commit()
    conn.close()

    try:
        await process_recording(
            'https://www.soundhelix.com/examples/mp3/SoundHelix-Song-1.mp3',
            'ADMIN_TEST_CALL_1',
            '+917406317771'
        )
        print('Success! process_recording finished natively.')
    except Exception as e:
        print(f'FATAL: {e}')

if __name__ == '__main__':
    asyncio.run(test_native())
'''

cmd = f"""cat << 'EOF' > /home/empcloud-development/callified-ai/test_admin.py
{script_payload}
EOF
"""

client.exec_command(cmd)

import time
time.sleep(1)
_, stdout, stderr = client.exec_command('cd /home/empcloud-development/callified-ai && /home/empcloud-development/callified-ai/venv/bin/python test_admin.py')

print(stdout.read().decode().strip())
print(stderr.read().decode().strip())
client.close()
