import paramiko

host = "163.227.174.141"
username = "empcloud-development"
password = "rSPa3izkYPtAjCFLa5cqPDpsFvV071KN9u"

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect(host, username=username, password=password, timeout=10)

print("--- NGINX ACCESS LOGS ---")
_, stdout, _ = client.exec_command("cat /var/log/nginx/access.log | grep -iE 'webhook|recording|2026' | tail -n 20")
print(stdout.read().decode().strip())

print("--- JOURNALCTL SYSTEM OUT ---")
_, stdout, _ = client.exec_command("sudo journalctl -u callified-ai.service -n 50 --no-pager")
print(stdout.read().decode().strip())

client.close()
