import paramiko
import requests
import time

host = "163.227.174.141"
username = "empcloud-development"
password = "rSPa3izkYPtAjCFLa5cqPDpsFvV071KN9u"

print("--- INJECTING SYNTHETIC TRACE FILE ---")
client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect(host, username=username, password=password, timeout=10)

client.exec_command("echo 'CONFIRM_TARGET' > /home/empcloud-development/callified-ai/frontend/dist/ping.txt")
time.sleep(1)

print("\n--- QUERYING CLOUDFLARE DOMAIN ---")
try:
    r = requests.get("https://test.callified.ai/api/ping") # we'll just check if ping.txt works natively if static routed, or just hit the domain!
    
    r2 = requests.get("https://test.callified.ai/ping.txt")
    print(f"Domain Ping.txt Response [{r2.status_code}]: {r2.text.strip()}")
except Exception as e:
    print(f"HTTP Failed: {e}")

client.close()
