import paramiko
import os

HOSTNAME = "163.227.174.141"
USERNAME = "empcloud-development"
PASSWORD = "Epm^%$#Dv98g89"

def update_remote_env():
    print("Connecting to secure droplet...")
    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    ssh.connect(HOSTNAME, username=USERNAME, password=PASSWORD)

    env_path = "/home/empcloud-development/callified-ai/.env"
    
    # 1. Read current .env
    stdin, stdout, stderr = ssh.exec_command(f"cat {env_path}")
    env_content = stdout.read().decode('utf-8')
    
    # 2. Update or append keys
    lines = env_content.split('\n')
    new_lines = []
    
    found_tts = False
    found_smallest = False
    
    for line in lines:
        if line.startswith("TTS_PROVIDER="):
            new_lines.append("TTS_PROVIDER=smallest")
            found_tts = True
        elif line.startswith("SMALLEST_API_KEY="):
            new_lines.append("SMALLEST_API_KEY=sk_fae0151e37fa3c9e13258188b932326a")
            found_smallest = True
        elif line.startswith("SMALLEST_VOICE_ID="):
            new_lines.append("SMALLEST_VOICE_ID=emily")
        else:
            if line.strip() != "":
                new_lines.append(line)
                
    if not found_tts:
        new_lines.append("TTS_PROVIDER=smallest")
    if not found_smallest:
        new_lines.append("SMALLEST_API_KEY=sk_fae0151e37fa3c9e13258188b932326a")
        new_lines.append("SMALLEST_VOICE_ID=emily")
        
    final_env = "\n".join(new_lines) + "\n"
    
    # 3. Write back to server securely
    sftp = ssh.open_sftp()
    with sftp.file(env_path, 'w') as f:
        f.write(final_env)
    sftp.close()
    
    print("Credentials successfully updated! Restarting Callified-AI daemon...")
    
    # 4. Restart service
    cmd = "echo 'Epm^%$#Dv98g89' | sudo -S systemctl restart callified-ai"
    stdin, stdout, stderr = ssh.exec_command(cmd)
    
    print("Daemon restart status:", stdout.read().decode())
    err = stderr.read().decode()
    if err and "[sudo]" not in err:
        print("Sudo errors:", err)
        
    ssh.close()
    print("Done!")

if __name__ == "__main__":
    update_remote_env()
