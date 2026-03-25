import paramiko
import time

HOSTNAME = "163.227.174.141"
USERNAME = "empcloud-development"
PASSWORD = "Epm^%$#Dv98g89"

def check():
    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    ssh.connect(HOSTNAME, username=USERNAME, password=PASSWORD)
    
    # We use a proper pty to feed sudo password
    channel = ssh.invoke_shell()
    channel.send('sudo journalctl -u callified-ai.service -n 50 --no-pager\n')
    time.sleep(1)
    channel.send('Epm^%$#Dv98g89\n')
    time.sleep(2)
    
    output = ""
    while channel.recv_ready():
        output += channel.recv(4096).decode('utf-8', errors='ignore')
        
    print(output)
    ssh.close()

if __name__ == "__main__":
    check()
