import httpx
from typing import List, Dict
import json
from . import BaseCRM

class ZohoCRM(BaseCRM):
    def __init__(self, api_key: str, base_url: str = ""):
        super().__init__(api_key, base_url) # For Zoho, api_key is the access token. base_url might be zohoapis.com
        self.headers = {
            "Authorization": f"Zoho-oauthtoken {self.api_key}",
            "Content-Type": "application/json"
        }
        domain = self.base_url.rstrip('/') if self.base_url else "https://www.zohoapis.com"
        self.base_api_url = f"{domain}/crm/v2"

    def fetch_new_leads(self) -> List[Dict]:
        """
        Fetches Leads from Zoho CRM.
        """
        url = f"{self.base_api_url}/Leads?fields=id,First_Name,Last_Name,Phone,Lead_Status&per_page=50"
        
        leads = []
        try:
            with httpx.Client() as client:
                res = client.get(url, headers=self.headers)
                if res.status_code == 200:
                    data = res.json()
                    for lead in data.get('data', []):
                        if lead.get('Phone'):
                            leads.append({
                                "external_id": lead.get('id'),
                                "first_name": lead.get('First_Name', 'Unknown'),
                                "last_name": lead.get('Last_Name', ''),
                                "phone": lead.get('Phone', ''),
                                "source": "Zoho"
                            })
        except Exception as e:
            print(f"Zoho fetch error: {e}")
            
        return leads

    def update_lead_status(self, external_id: str, status: str) -> bool:
        """
        Updates the lead status in Zoho CRM.
        """
        url = f"{self.base_api_url}/Leads"
        payload = {
            "data": [
                {
                    "id": external_id,
                    "Lead_Status": status
                }
            ]
        }
        try:
            with httpx.Client() as client:
                res = client.put(url, headers=self.headers, json=payload)
                return res.status_code == 200
        except Exception as e:
            print(f"Zoho update status error: {e}")
            return False

    def log_call(self, external_id: str, transcript: str, summary: str) -> bool:
        """
        Logs a Call task in Zoho CRM associating it to the Lead.
        """
        url = f"{self.base_api_url}/Calls"
        
        payload = {
            "data": [
                {
                    "Subject": "AI Sales Call",
                    "Call_Type": "Outbound",
                    "Call_Purpose": "Prospecting",
                    "Description": f"AI Dialer Summary:\n{summary}\n\nFull Transcript:\n{transcript}",
                    "$se_module": "Leads",
                    "What_Id": external_id
                }
            ]
        }
        
        try:
            with httpx.Client() as client:
                res = client.post(url, headers=self.headers, json=payload)
                return res.status_code in [200, 201]
        except Exception as e:
            print(f"Zoho log call error: {e}")
            return False
