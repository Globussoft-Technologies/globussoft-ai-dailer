import paramiko

HOSTNAME = "163.227.174.141"
USERNAME = "empcloud-development"
PASSWORD = "Epm^%$#Dv98g89"

def check_logs():
    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    ssh.connect(HOSTNAME, username=USERNAME, password=PASSWORD)
    
    stdin, stdout, stderr = ssh.exec_command("sudo journalctl -u callified-ai.service -n 100 --no-pager")
    print(stdout.read().decode('utf-8'))
    
    ssh.close()

if __name__ == "__main__":
    check_logs()
