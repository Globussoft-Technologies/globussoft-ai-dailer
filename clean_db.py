import paramiko

host = "163.227.174.141"
username = "empcloud-development"
password = "rSPa3izkYPtAjCFLa5cqPDpsFvV071KN9u"

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect(host, username=username, password=password, timeout=10)

print("Truncating the call_transcripts database table...")
_, stdout1, _ = client.exec_command("mysql -u callified -pCallified@2026 callified_ai -e 'TRUNCATE TABLE call_transcripts;'")
print(stdout1.read().decode().strip())

print("Wiping out old raw audio files from the server's attached volume block...")
_, stdout2, _ = client.exec_command("rm -f /home/empcloud-development/callified-ai/recordings/*")
_, stdout3, _ = client.exec_command("rm -f /home/empcloud-development/demo-callified-ai/recordings/*")
print(stdout2.read().decode().strip())
print(stdout3.read().decode().strip())

print("Clean operation completed successfully.")
client.close()
