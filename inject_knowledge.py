import paramiko
import json

HOSTNAME = "163.227.174.141"
USERNAME = "empcloud-development"
PASSWORD = "Epm^%$#Dv98g89"

adsgpt_knowledge = """AdsGPT is a powerful AI Ad Creative and Copy Generator that accelerates marketing results. 
Key features: 
- Launch 100 Meta Ads in under 60 seconds.
- No Designer, No Copywriter, No Guesswork required.
- Instant Ad Copy Generation with AI-Powered Creativity.
- Platform-Specific Optimization and Multi-Platform Coverage.
- Keep Track of Your Creations with a User-Friendly Dashboard and Single Ad Analytics View.
- Geographic Ad Breakdown, Cost-Effective Solution, and Data-Driven Insights.
It helps marketers gain a competitive advantage and enhance targeting precision effortlessly.
"""

def inject():
    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    ssh.connect(HOSTNAME, username=USERNAME, password=PASSWORD)
    
    script = f'''
import sys
sys.path.append("/home/empcloud-development/callified-ai")
import database

# 1. Ensure an organization exists
orgs = database.get_all_organizations()
if not orgs:
    org_id = database.create_organization("Globussoft")
else:
    org_id = orgs[0]["id"]

# 2. Check if product exists to avoid duplicates
existing = database.get_products_by_org(org_id)
found = False
for p in existing:
    if p["name"].lower() == "adsgpt":
        found = True
        database.update_product(p["id"], manual_notes={repr(adsgpt_knowledge)})
        print("Updated existing AdsGPT product.")
        break

if not found:
    database.create_product(org_id, "AdsGPT", "https://adsgpt.io/", {repr(adsgpt_knowledge)})
    print("Injected AdsGPT product knowledge into database!")
'''

    # Save to remote
    with ssh.open_sftp() as sftp:
        with sftp.open("/home/empcloud-development/callified-ai/inject_knowledge.py", "w") as f:
            f.write(script)
            
    # Run remote script
    stdin, stdout, stderr = ssh.exec_command('cd /home/empcloud-development/callified-ai && /usr/bin/python3 inject_knowledge.py')
    print(stdout.read().decode('utf-8'))
    print(stderr.read().decode('utf-8'))
    
    ssh.close()

if __name__ == "__main__":
    inject()
