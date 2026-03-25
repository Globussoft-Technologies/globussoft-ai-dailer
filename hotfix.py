import paramiko

HOSTNAME = "163.227.174.141"
USERNAME = "empcloud-development"
PASSWORD = "Epm^%$#Dv98g89"

def apply_hotfix():
    print("Connecting to secure droplet...")
    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    ssh.connect(HOSTNAME, username=USERNAME, password=PASSWORD)

    file_path = "/home/empcloud-development/callified-ai/main.py"
    
    stdin, stdout, stderr = ssh.exec_command(f"cat {file_path}")
    content = stdout.read().decode('utf-8')
    
    # Simple replace for the Twilio block inside the ElevenLabs else branch
    # specifically line 908 which is:
    # "stream_sid": stream_sid,
    # right after "event": "media"
    
    content = content.replace('"stream_sid": stream_sid,', '"streamSid": stream_sid,')
    
    sftp = ssh.open_sftp()
    with sftp.file(file_path, 'w') as f:
        f.write(content)
    sftp.close()
    
    # Just restart the daemon
    ssh.exec_command("echo 'Epm^%$#Dv98g89' | sudo -S systemctl restart callified-ai")
    ssh.close()
    print("Hotfix deployed!")

if __name__ == "__main__":
    apply_hotfix()
