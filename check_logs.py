import paramiko
import os

env_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), ".env")
with open(env_path) as f:
    for line in f:
        if "=" in line and not line.startswith("#"):
            k, v = line.strip().split("=", 1)
            os.environ[k] = v

HOSTNAME = os.environ.get("DEPLOY_HOST", "163.227.174.141")
USERNAME = os.environ.get("DEPLOY_USER", "empcloud-development")
PASSWORD = os.environ.get("DEPLOY_PASS")

ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect(HOSTNAME, username=USERNAME, password=PASSWORD)

def exec_sudo(cmd):
    stdin, stdout, stderr = ssh.exec_command(f"sudo -S {cmd}")
    stdin.write(PASSWORD + '\n')
    stdin.flush()
    print(f"Connecting to {HOSTNAME} to fetch dialed conversation traces...")
    
    # Filter journal specifically for STT results and LLM boundaries
import paramiko
import os

env_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), ".env")
with open(env_path) as f:
    for line in f:
        if "=" in line and not line.startswith("#"):
            k, v = line.strip().split("=", 1)
            os.environ[k] = v

HOSTNAME = os.environ.get("DEPLOY_HOST", "163.227.174.141")
USERNAME = os.environ.get("DEPLOY_USER", "empcloud-development")
PASSWORD = os.environ.get("DEPLOY_PASS")

ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect(HOSTNAME, username=USERNAME, password=PASSWORD)

def exec_sudo(cmd):
    stdin, stdout, stderr = ssh.exec_command(f"sudo -S {cmd}")
    stdin.write(PASSWORD + '\n')
    stdin.flush()
    print(f"Connecting to {HOSTNAME} to fetch dialed conversation traces...")
    
    # Filter journal specifically for STT results and LLM boundaries
    # Using python's paramiko to pull the logs cleanly
    command = "mysql -u empcloud-development -pE#mp@123_ cloud -e 'SELECT id, email, full_name, role FROM users LIMIT 10;'"
    print(f"Connecting to {HOSTNAME} to fetch users...")
    
    stdin, stdout, stderr = ssh.exec_command(command)
    output = stdout.read().decode('utf-8')
    print(output)
    print(stderr.read().decode())

exec_sudo("systemctl status callified-ai.service --no-pager")
exec_sudo("journalctl -u callified-ai.service -n 1000 --no-pager | grep -i 'error\|except\|llm\|warn\|fail'")

ssh.close()
