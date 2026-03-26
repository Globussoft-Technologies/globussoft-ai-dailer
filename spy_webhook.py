import paramiko

c = paramiko.SSHClient()
c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect("163.227.174.141", username="empcloud-development", password="rSPa3izkYPtAjCFLa5cqPDpsFvV071KN9u")

print("Checking remote recordings folder...")
_, stdout, _ = c.exec_command("find /home/empcloud-development/callified-ai/recordings/ -type f")
print(stdout.read().decode().strip())

print("Checking raw server logs for ANY webhook hit...")
_, stdout, _ = c.exec_command("grep -A 2 -B 2 -i '/webhook/exotel' /home/empcloud-development/callified-ai/logs/*.log | tail -n 50")
print(stdout.read().decode().strip())

c.close()
