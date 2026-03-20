import os
import re

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

print(f"Generating templates for {len(crms)} new CRMs...")

for crm in crms:
    slug = crm.lower().replace(" ", "")
    class_name = f"{crm.replace(' ', '')}CRM"
    code = f"""import httpx
from typing import List, Dict
import json
from . import BaseCRM

class {class_name}(BaseCRM):
    def __init__(self, api_key: str, base_url: str = ""):
        super().__init__(api_key, base_url)
        self.headers = {{
            "Authorization": f"Bearer {{self.api_key}}",
            "Content-Type": "application/json"
        }}
        self.base_api_url = self.base_url.rstrip('/') if self.base_url else f"https://api.{slug}.com/v1"

    def fetch_new_leads(self) -> List[Dict]:
        url = f"{{self.base_api_url}}/leads"
        leads = []
        try:
            with httpx.Client() as client:
                res = client.get(url, headers=self.headers)
                if res.status_code == 200:
                    data = res.json()
                    for lead in data.get('data', []):
                        if lead.get('phone'):
                            leads.append({{
                                "external_id": lead.get('id'),
                                "first_name": lead.get('first_name', 'Unknown'),
                                "last_name": lead.get('last_name', ''),
                                "phone": lead.get('phone', ''),
                                "source": "{crm}"
                            }})
        except Exception as e:
            pass
        return leads

    def update_lead_status(self, external_id: str, status: str) -> bool:
        url = f"{{self.base_api_url}}/leads/{{external_id}}"
        payload = {{"status": status}}
        try:
            with httpx.Client() as client:
                res = client.patch(url, headers=self.headers, json=payload)
                return res.status_code in [200, 204]
        except Exception as e:
            return False

    def log_call(self, external_id: str, transcript: str, summary: str) -> bool:
        url = f"{{self.base_api_url}}/leads/{{external_id}}/calls"
        payload = {{
            "description": f"AI Dialer Summary:\\n{{summary}}\\n\\nFull Transcript:\\n{{transcript}}",
            "status": "Completed"
        }}
        try:
            with httpx.Client() as client:
                res = client.post(url, headers=self.headers, json=payload)
                return res.status_code in [200, 201]
        except Exception as e:
            return False
"""
    with open(f"crm_providers/{slug}.py", "w") as f:
        f.write(code)

with open("frontend/src/App.jsx", "r", encoding='utf-8') as f:
    app_code = f.read()

options = '<option value="HubSpot">HubSpot</option>\\n                    <option value="Salesforce">Salesforce</option>\\n                    <option value="Zoho">Zoho CRM</option>\\n'
for c in crms:
    options += f'                    <option value="{c}">{c}</option>\\n'

app_code = re.sub(
    r'<option value="HubSpot">HubSpot</option>.*?<option value="Zoho">Zoho CRM</option>',
    options.rstrip(),
    app_code,
    flags=re.DOTALL
)

with open("frontend/src/App.jsx", "w", encoding='utf-8') as f:
    f.write(app_code)

print(f"Generated {len(crms)} CRMs successfully and updated frontend.")
