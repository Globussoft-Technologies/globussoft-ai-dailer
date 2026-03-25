import paramiko
import os

HOSTNAME = "163.227.174.141"
USERNAME = "empcloud-development"
PASSWORD = "Epm^%$#Dv98g89"

def deploy():
    print("Connecting to secure droplet...")
    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    ssh.connect(HOSTNAME, username=USERNAME, password=PASSWORD)

    sftp = ssh.open_sftp()
    
    # Upload main.py
    local_main = r"c:\Users\Admin\gbs-projects\gbs-ai-dialer\main.py"
    remote_main = "/home/empcloud-development/callified-ai/main.py"
    sftp.put(local_main, remote_main)
    print("Uploaded main.py")
    
    # Upload database.py
    local_db = r"c:\Users\Admin\gbs-projects\gbs-ai-dialer\database.py"
    remote_db = "/home/empcloud-development/callified-ai/database.py"
    sftp.put(local_db, remote_db)
    print("Uploaded database.py")
    
    sftp.close()
    
    # Compress and Upload the React Frontend
    print("Zipping frontend dist...")
    import shutil
    dist_dir = r"c:\Users\Admin\gbs-projects\gbs-ai-dialer\frontend\dist"
    zip_path = r"c:\Users\Admin\gbs-projects\gbs-ai-dialer\dist" # creates dist.zip
    shutil.make_archive(zip_path, 'zip', dist_dir)
    
    # Reopen SFTP to send the ZIP
    sftp = ssh.open_sftp()
    local_zip = zip_path + ".zip"
    remote_zip = "/home/empcloud-development/callified-ai/dist.zip"
    print("Uploading frontend dist.zip...")
    sftp.put(local_zip, remote_zip)
    sftp.close()
    
    # Unzip on the server
    print("Extracting frontend on remote server...")
    ssh.exec_command("cd /home/empcloud-development/callified-ai/frontend && rm -rf dist && mkdir -p dist && unzip -o ../dist.zip -d dist && rm ../dist.zip")
    
    # Clean up local zip
    if os.path.exists(local_zip):
        os.remove(local_zip)
    
    # Restart the daemon
    print("Restarting callified-ai service...")
    ssh.exec_command("echo 'Epm^%$#Dv98g89' | sudo -S systemctl restart callified-ai")
    ssh.close()
    print("Deployment successful!")

if __name__ == "__main__":
    deploy()
