import paramiko

host = "163.227.174.141"
username = "empcloud-development"
password = "rSPa3izkYPtAjCFLa5cqPDpsFvV071KN9u"

print("--- EXECUTING NATIVE GIT DIAGNOSTICS ---")
client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect(host, username=username, password=password, timeout=10)

_, stdout, _ = client.exec_command("cd /home/empcloud-development/callified-ai && git status")
print("\n--- GIT STATUS ---")
print(stdout.read().decode().strip())

print("\n--- FORCING HARD RESET & PULL ---")
_, stdout2, stderr2 = client.exec_command("cd /home/empcloud-development/callified-ai && git fetch origin && git reset --hard origin/main")
print("STDOUT:", stdout2.read().decode().strip())
print("STDERR:", stderr2.read().decode().strip())

client.exec_command("echo 'rSPa3izkYPtAjCFLa5cqPDpsFvV071KN9u' | sudo -S systemctl restart callified-ai.service")

print("\n--- RESTART EXECUTED ---")
client.close()
