import socket
import requests
import paramiko

host = "163.227.174.141"
username = "empcloud-development"
password = "rSPa3izkYPtAjCFLa5cqPDpsFvV071KN9u"

print("--- DNS RESOLUTION ---")
try:
    ip = socket.gethostbyname("test.callified.ai")
    print(f"test.callified.ai resolves to: {ip}")
except Exception as e:
    print(f"DNS Resolution Failed: {e}")

print("\n--- EXTRACTING KERNEL SYS LOGS ---")
client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect(host, username=username, password=password, timeout=10)

client.exec_command("echo 'rSPa3izkYPtAjCFLa5cqPDpsFvV071KN9u' | sudo -S journalctl -u callified-ai.service -n 150 --no-pager > /tmp/jlog.txt")
import time; time.sleep(2)
_, stdout, _ = client.exec_command("cat /tmp/jlog.txt")
print(stdout.read().decode().strip()[-2000:])

client.exec_command("ls -la /home/empcloud-development/callified-ai/recordings/ > /tmp/ls.txt")
_, stdout2, _ = client.exec_command("cat /tmp/ls.txt")
print("\n--- LS OUTPUT ---")
print(stdout2.read().decode().strip())

client.close()
