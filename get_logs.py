import paramiko

HOSTNAME = "163.227.174.141"
USERNAME = "empcloud-development"
PASSWORD = "Epm^%$#Dv98g89"

def check_logs():
    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    ssh.connect(HOSTNAME, username=USERNAME, password=PASSWORD)
    
    # Run curl locally on the server to its own uvicorn instance
    stdin, stdout, stderr = ssh.exec_command("curl -s http://127.0.0.1:8000/api/debug/logs")
    print("Logs from server:")
    print(stdout.read().decode('utf-8'))
    
    ssh.close()

if __name__ == "__main__":
    check_logs()
