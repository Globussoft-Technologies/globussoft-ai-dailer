import paramiko

host = "163.227.174.141"
username = "empcloud-development"
password = "rSPa3izkYPtAjCFLa5cqPDpsFvV071KN9u"

print("--- EXECUTING NATIVE VENV MODULE CHECK ---")
client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect(host, username=username, password=password, timeout=10)

cmd = "/home/empcloud-development/callified-ai/venv/bin/python -c \"import faiss; import sentence_transformers; print('DEPENDENCIES_OK')\""
_, stdout, stderr = client.exec_command(cmd)

out = stdout.read().decode().strip()
err = stderr.read().decode().strip()

print("STDOUT:", out)
print("STDERR:", err)

if "DEPENDENCIES_OK" in out:
    print("--- RESTARTING SYSTEMD SERVICE ---")
    client.exec_command("echo 'rSPa3izkYPtAjCFLa5cqPDpsFvV071KN9u' | sudo -S systemctl restart callified-ai.service")
    import time
    time.sleep(2)
    print("--- SERVICE RESTARTED ---")

client.close()
