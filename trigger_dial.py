import paramiko

HOSTNAME = "163.227.174.141"
USERNAME = "empcloud-development"
PASSWORD = "Epm^%$#Dv98g89"

def dial():
    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    ssh.connect(HOSTNAME, username=USERNAME, password=PASSWORD)
    
    script = '''
import sys
sys.path.append("/home/empcloud-development/callified-ai")
import database

conn = database.get_conn()
cursor = conn.cursor()
cursor.execute("SELECT * FROM leads WHERE LOWER(first_name) LIKE '%sumit%'")
leads = cursor.fetchall()
conn.close()

target = leads[-1] if leads else None

if target:
    print(f"Found Sumit! ID: {target['id']}, Phone: {target['phone']}")
    import urllib.request
    # Use 8001 which is the active Uvicorn worker from systemctl
    req = urllib.request.Request(f"http://127.0.0.1:8001/api/dial/{target['id']}", method="POST")
    try:
        with urllib.request.urlopen(req) as response:
            print(f"Dialed Lead {target['id']}:", response.read().decode())
    except Exception as e:
        print(f"Error dialing: {e}")
else:
    print("WARNING: Sumit not found!")
'''
    with ssh.open_sftp() as sftp:
        with sftp.open("/home/empcloud-development/callified-ai/trigger_dial.py", "w") as f:
            f.write(script)
            
    stdin, stdout, stderr = ssh.exec_command('cd /home/empcloud-development/callified-ai && /usr/bin/python3 trigger_dial.py')
    print(stdout.read().decode('utf-8'))
    print(stderr.read().decode('utf-8'))
    
    ssh.close()

if __name__ == "__main__":
    dial()
