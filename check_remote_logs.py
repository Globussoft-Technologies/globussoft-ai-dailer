import paramiko
import sys

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect('163.227.174.141', username='empcloud-development', password='Epm^%$#Dv98g89', timeout=10)

command = "echo 'Epm^%$#Dv98g89' | sudo -S journalctl -u callified-ai -n 50 --no-pager"
stdin, stdout, stderr = client.exec_command(command)

print("--- STDOUT ---")
print(stdout.read().decode())
client.close()
