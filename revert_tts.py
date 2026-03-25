import paramiko

HOSTNAME = "163.227.174.141"
USERNAME = "empcloud-development"
PASSWORD = "Epm^%$#Dv98g89"

def revert_tts_provider():
    print("Connecting to secure droplet...")
    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    ssh.connect(HOSTNAME, username=USERNAME, password=PASSWORD)

    env_path = "/home/empcloud-development/callified-ai/.env"
    
    stdin, stdout, stderr = ssh.exec_command(f"cat {env_path}")
    env_content = stdout.read().decode('utf-8')
    
    lines = env_content.split('\n')
    new_lines = []
    
    for line in lines:
        if line.startswith("TTS_PROVIDER="):
            new_lines.append("TTS_PROVIDER=elevenlabs")
        else:
            if line.strip() != "":
                new_lines.append(line)
                
    final_env = "\n".join(new_lines) + "\n"
    
    sftp = ssh.open_sftp()
    with sftp.file(env_path, 'w') as f:
        f.write(final_env)
    sftp.close()
    
    print("Provider formally reverted to 'elevenlabs'! Restarting...")
    
    stdin, stdout, stderr = ssh.exec_command("echo 'Epm^%$#Dv98g89' | sudo -S systemctl restart callified-ai")
    ssh.close()
    print("Done!")

if __name__ == "__main__":
    revert_tts_provider()
