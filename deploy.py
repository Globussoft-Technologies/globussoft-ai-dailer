import paramiko

host = "163.227.174.141"
username = "empcloud-development"
password = "Epm^%$#Dv98g89"

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())

try:
    print("Connecting...")
    client.connect(host, username=username, password=password, timeout=10)
    print("Connected! Running deployment commands...")
    
    command = "cd ~/callified-ai && git pull origin main && echo 'Epm^%$#Dv98g89' | sudo -S systemctl restart callified-ai 2>&1"
    
    stdin, stdout, stderr = client.exec_command(command)
    exit_status = stdout.channel.recv_exit_status()
    
    print("--- STDOUT ---")
    print(stdout.read().decode().strip())
    
    print("--- STDERR ---")
    print(stderr.read().decode().strip())
    
    print(f"--- Exit status: {exit_status} ---")
    
finally:
    client.close()
