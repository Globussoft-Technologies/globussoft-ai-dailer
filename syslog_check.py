import paramiko

HOSTNAME = "163.227.174.141"
USERNAME = "empcloud-development"
PASSWORD = "Epm^%$#Dv98g89"

def check():
    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    ssh.connect(HOSTNAME, username=USERNAME, password=PASSWORD)
    
    # Exclude the noisy lines
    cmd = "echo 'Epm^%$#Dv98g89' | sudo -S cat /var/log/syslog | grep uvicorn | grep -v 'WS text message' | tail -n 100"
    stdin, stdout, stderr = ssh.exec_command(cmd)
    print(stdout.read().decode('utf-8'))
    
    ssh.close()

if __name__ == "__main__":
    check()
