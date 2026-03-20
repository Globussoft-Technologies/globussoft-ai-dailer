import os
import sys
import time
from dotenv import load_dotenv
from google import genai
from google.genai import types

env_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), '.env')
load_dotenv(env_path)
api_key = os.getenv("GEMINI_API_KEY")
if not api_key:
    print("No GEMINI_API_KEY found in .env")
    sys.exit(1)

client = genai.Client(api_key=api_key)

crms = [
    "Pipedrive", "ActiveCampaign", "Freshsales", "Monday", "Keap", "Zendesk", "Bitrix24", "Insightly", 
    "Copper", "Nimble", "Nutshell", "Capsule", "AgileCRM", "SugarCRM", "Vtiger", "Apptivo", "Creatio", 
    "Maximizer", "Salesflare", "Close", "Pipeline", "ReallySimpleSystems", "EngageBay", "Ontraport", 
    "Kustomer", "Dynamics365", "OracleCX", "SAPCRM", "NetSuite", "SageCRM", "Pegasystems", "InforCRM", 
    "Workbooks", "Kintone", "Scoro", "Odoo", "Streak", "LessAnnoyingCRM", "Daylite", "ConvergeHub", 
    "Claritysoft", "AmoCRM", "BenchmarkONE", "Bigin", "BoomTown", "BuddyCRM", "Bullhorn", "CiviCRM", 
    "ClientLook", "ClientSuccess", "ClientTether", "CommandCenter", "ConnectWise", "Contactually", 
    "Corezoid", "CRMNext", "Daycos", "DealerSocket", "Efficy", "Enquire", "Entrata", "Epsilon", 
    "EspoCRM", "Exact", "Flowlu", "FollowUpBoss", "Front", "Funnel", "Genesis", "GoHighLevel", 
    "GoldMine", "GreenRope", "Highrise", "iContact", "Infusionsoft", "IxactContact", "Jobber", 
    "Junxure", "Kaseya", "Kixie", "Klaviyo", "Kommo", "LeadSquared", "LionDesk", "Lusha", "Mailchimp", 
    "Marketo", "Membrain", "MethodCRM", "MightyCRM", "Mindbody", "Mixpanel", "Navatar", "NetHunt", 
    "NexTravel", "Nurture", "OnePageCRM", "Pipeliner", "Planhat", "Podio"
]

base_prompt = """
You are an expert Python Backend Engineer building an integration module for a Generative AI Dialer.
Write a production-ready Python file for '{crm}' CRM.
It must inherit from `. BaseCRM` exactly like this: `from . import BaseCRM`.
The class MUST be named precisely `{class_name}`.

The file must implement the 3 abstract methods of BaseCRM:
1. `fetch_new_leads(self) -> List[Dict]`: Retrieve contacts/leads that have phone numbers. Return formatted dicts: {{"external_id": "...", "first_name": "...", "last_name": "...", "phone": "...", "source": "{crm}"}}
2. `update_lead_status(self, external_id: str, status: str) -> bool`: Update the lead's status/stage in {crm}.
3. `log_call(self, external_id: str, transcript: str, summary: str) -> bool`: Log a Completed Call Activity / Note / Engagement to the lead's timeline in {crm}.

CRITICAL:
- Use `httpx`.
- Infer the industry-standard actual Base URL for {crm}'s API (e.g., https://api.pipedrive.com/v1). If instance-dependent, parse `self.base_url` properly.
- Use {crm}'s real payload structures for Contacts/Leads and Notes/Activities based on your training data.
- If it uses Bearer token, use `self.api_key`. If it uses Basic Auth, use HTTP Basic. If API Key goes in query param, do that.
- Wrap HTTP calls in `try...except Exception:` blocks so crashes don't bring down the main thread.
- ONLY output the raw valid Python code. NO markdown wrapping (` ```python `). NO explanations. Start immediately with `import httpx`.
"""

print(f"Initiating autonomous AI generation of {len(crms)} CRM modules...")

count = 0
for crm in crms:
    slug = crm.lower().replace(" ", "")
    class_name = f"{crm.replace(' ', '')}CRM"
    target_file = f"crm_providers/{slug}.py"
    
    # Check if we already generated it previously with custom code (skip if it looks fleshed out, wait no we want to overwrite the bulk stubs!)
    
    prompt = base_prompt.format(crm=crm, class_name=class_name)
    
    try:
        response = client.models.generate_content(
            model="gemini-2.5-flash",
            contents=prompt,
        )
        
        # Clean potential markdown if model disobeyed
        code = response.text.replace("```python", "").replace("```", "").strip()
        
        with open(target_file, "w") as f:
            f.write(code)
            
        print(f"[{count+1}/{len(crms)}] ✅ Successfully coded integration for {crm}!")
        count += 1
        
        # Brief pause to avoid rate limits
        time.sleep(2)
        
    except Exception as e:
        print(f"[{count+1}/{len(crms)}] ❌ Failed to generate {crm}: {e}")

print("Autonomous CRM build complete!")
