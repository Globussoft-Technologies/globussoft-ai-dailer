import paramiko

host = "163.227.174.141"
username = "empcloud-development"
password = "rSPa3izkYPtAjCFLa5cqPDpsFvV071KN9u"

print("--- EXECUTING NATIVE DOWNLOAD SCRIPT ON SECURE SERVER ---")
client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect(host, username=username, password=password, timeout=10)

script_payload = '''
import asyncio
import os
import sys
import logging

logging.basicConfig(level=logging.DEBUG, stream=sys.stdout)

sys.path.append('/home/empcloud-development/callified-ai')
from main import process_recording

async def test_native():
    print('Initiating process_recording natively...')
    try:
        await process_recording(
            'https://www.soundhelix.com/examples/mp3/SoundHelix-Song-1.mp3',
            'MOCK_SID_100',
            '+919999999999'
        )
        print('Success! process_recording finished natively.')
    except Exception as e:
        print(f'FATAL: {e}')

if __name__ == '__main__':
    asyncio.run(test_native())
'''

# Use EOF hermetic block to prevent ANY bash parsing of quotes!
cmd = f"""cat << 'EOF' > /home/empcloud-development/callified-ai/test_native.py
{script_payload}
EOF
"""

client.exec_command(cmd)

import time
time.sleep(1)
_, stdout, stderr = client.exec_command('cd /home/empcloud-development/callified-ai && /home/empcloud-development/callified-ai/venv/bin/python test_native.py')

out = stdout.read().decode().strip()
err = stderr.read().decode().strip()

print("STDOUT:")
print(out)
print("STDERR:")
print(err)

client.exec_command("ls -la /home/empcloud-development/callified-ai/recordings/ > /tmp/ls.txt")
_, stdout2, _ = client.exec_command("cat /tmp/ls.txt")
print("\n--- LS OUTPUT ---")
print(stdout2.read().decode().strip())

client.close()
