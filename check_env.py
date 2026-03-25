import paramiko

ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect('163.227.174.141', username='empcloud-development', password='Epm^%$#Dv98g89')

_, stdout, _ = ssh.exec_command("echo 'Epm^%$#Dv98g89' | sudo -S journalctl -u callified-ai -n 200")
logs = stdout.read().decode()

for line in logs.split('\n'):
    if "TTS" in line:
        print(line)

ssh.close()
