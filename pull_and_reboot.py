import paramiko

import os
from dotenv import load_dotenv

load_dotenv()
host = os.environ.get("DEPLOY_HOST")
username = os.environ.get("DEPLOY_USER")
password = os.environ.get("DEPLOY_PASS")

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect(host, username=username, password=password, timeout=10)

print("PULLING NEW CODE AND RESTARTING SERVICE...")
command1 = f"cd ~/callified-ai && git reset --hard && git clean -fd && git pull origin main && sudo -S systemctl restart callified-ai.service"
stdin, stdout, stderr = client.exec_command(command1, get_pty=True)
stdin.write(password + "\n")
stdin.flush()
output = stdout.read().decode().strip()
print(output)

client.close()
