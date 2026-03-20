import httpx
from typing import List, Dict
import json
from . import BaseCRM

class SalesforceCRM(BaseCRM):
    def __init__(self, api_key: str, base_url: str = ""):
        super().__init__(api_key, base_url) # For SF, api_key is the Bearer token, base_url is the instance URL
        self.headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json"
        }
        # e.g., https://your-instance.my.salesforce.com/services/data/v58.0
        self.base_api_url = f"{self.base_url.rstrip('/')}/services/data/v58.0"

    def fetch_new_leads(self) -> List[Dict]:
        """
        Fetches Leads from Salesforce that have a phone number.
        """
        query = "SELECT Id, FirstName, LastName, Phone FROM Lead WHERE Phone != NULL AND Status = 'Open - Not Contacted' LIMIT 50"
        url = f"{self.base_api_url}/query/?q={query.replace(' ', '+')}"
        
        leads = []
        try:
            with httpx.Client() as client:
                res = client.get(url, headers=self.headers)
                if res.status_code == 200:
                    data = res.json()
                    for lead in data.get('records', []):
                        leads.append({
                            "external_id": lead.get('Id'),
                            "first_name": lead.get('FirstName', 'Unknown'),
                            "last_name": lead.get('LastName', ''),
                            "phone": lead.get('Phone', ''),
                            "source": "Salesforce"
                        })
        except Exception as e:
            print(f"Salesforce fetch error: {e}")
            
        return leads

    def update_lead_status(self, external_id: str, status: str) -> bool:
        """
        Updates the lead status in Salesforce.
        """
        url = f"{self.base_api_url}/sobjects/Lead/{external_id}"
        payload = {
            "Status": status # Maps to SF lead status (Working - Contacted, Closed - Converted, etc)
        }
        try:
            with httpx.Client() as client:
                res = client.patch(url, headers=self.headers, json=payload)
                return res.status_code in [200, 204]
        except Exception as e:
            print(f"Salesforce update status error: {e}")
            return False

    def log_call(self, external_id: str, transcript: str, summary: str) -> bool:
        """
        Logs a completed Task (Call) under the Lead in Salesforce.
        """
        url = f"{self.base_api_url}/sobjects/Task/"
        
        payload = {
            "Subject": "AI Sales Call",
            "Status": "Completed",
            "Priority": "Normal",
            "WhoId": external_id, # Associate with the Lead
            "Description": f"AI Dialer Summary:\n{summary}\n\nFull Transcript:\n{transcript}",
            "TaskSubtype": "Call"
        }
        
        try:
            with httpx.Client() as client:
                res = client.post(url, headers=self.headers, json=payload)
                return res.status_code in [200, 201]
        except Exception as e:
            print(f"Salesforce log call error: {e}")
            return False
