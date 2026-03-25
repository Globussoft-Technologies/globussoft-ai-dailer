import paramiko
import os

HOSTNAME = "163.227.174.141"
USERNAME = "empcloud-development"
PASSWORD = "Epm^%$#Dv98g89"

def update_voice_id():
    print("Connecting to secure droplet...")
    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    ssh.connect(HOSTNAME, username=USERNAME, password=PASSWORD)

    env_path = "/home/empcloud-development/callified-ai/.env"
    
    # 1. Read current .env
    stdin, stdout, stderr = ssh.exec_command(f"cat {env_path}")
    env_content = stdout.read().decode('utf-8')
    
    # 2. Update Voice ID
    lines = env_content.split('\n')
    new_lines = []
    
    found_voice = False
    
    for line in lines:
        if line.startswith("SMALLEST_VOICE_ID="):
            new_lines.append("SMALLEST_VOICE_ID=ashish")
            found_voice = True
        else:
            if line.strip() != "":
                new_lines.append(line)
                
    if not found_voice:
        new_lines.append("SMALLEST_VOICE_ID=ashish")
        
    final_env = "\n".join(new_lines) + "\n"
    
    # 3. Write back to server securely
    sftp = ssh.open_sftp()
    with sftp.file(env_path, 'w') as f:
        f.write(final_env)
    sftp.close()
    
    print("Voice ID updated to 'ashish'! Restarting Callified-AI daemon...")
    
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
    update_voice_id()
