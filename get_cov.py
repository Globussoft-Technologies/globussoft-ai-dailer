import paramiko

host = "163.227.174.141"
username = "empcloud-development"
password = "rSPa3izkYPtAjCFLa5cqPDpsFvV071KN9u"

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect(host, username=username, password=password, timeout=10)

stdin, stdout, stderr = client.exec_command(
    "grep -E 'main\.py|routes\.py|tts\.py|ws_handler\.py|database\.py' /home/empcloud-development/callified-ai/cov.txt"
)
print(stdout.read().decode())
client.close()
