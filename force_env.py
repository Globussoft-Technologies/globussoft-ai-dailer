import paramiko

host = "163.227.174.141"
username = "empcloud-development"
password = "rSPa3izkYPtAjCFLa5cqPDpsFvV071KN9u"

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect(host, username=username, password=password, timeout=10)

print("Injecting PUBLIC_URL into .env...")
command1 = f"echo 'PUBLIC_URL=https://test.callified.ai' >> ~/callified-ai/.env && echo '{password}' | sudo -S systemctl restart gpt-callified-ai.service 2>&1"
stdin, stdout, stderr = client.exec_command(command1)
print(stdout.read().decode().strip())

print("Pulling latest server logs for any recording failures...")
command2 = "grep -iE 'recording' /home/empcloud-development/callified-ai/logs/uvicorn.error.log | tail -n 50"
stdin2, stdout2, stderr2 = client.exec_command(command2)
print("--- LATEST SERVER LOGS ---")
print(stdout2.read().decode().strip())

client.close()
