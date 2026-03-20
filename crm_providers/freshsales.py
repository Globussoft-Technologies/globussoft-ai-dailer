import httpx
from typing import List, Dict
import json
from . import BaseCRM

class FreshsalesCRM(BaseCRM):
    def __init__(self, **credentials):
        super().__init__(**credentials)
        self.base_api_url = self.base_url.rstrip('/') if self.base_url else "https://domain.myfreshworks.com/crm/sales/api"
        self.headers = {"Authorization": f"Token token={self.api_key}", "Content-Type": "application/json"}

    def fetch_new_leads(self) -> List[Dict]:
        leads = []
        try:
            with httpx.Client() as client:
               res = client.get(f"{self.base_api_url}/contacts/filters/1", headers=self.headers) # Assuming filter 1 is new leads
                if res.status_code == 200:
                    for item in res.json().get('contacts', []):
                        if item.get('mobile_number') or item.get('work_number'):
                            leads.append({"external_id": item['id'], "first_name": item.get('first_name',''), "last_name": item.get('last_name',''), "phone": item.get('mobile_number') or item.get('work_number'), "source": "Freshsales"})
        except Exception as e:
            print(f"{self.__class__.__name__} Fetch Error: {e}")
        return leads

    def update_lead_status(self, external_id: str, status: str) -> bool:
        try:
            with httpx.Client() as client:
               url = f"{self.base_api_url}/contacts/{external_id}"
                res = client.put(url, headers=self.headers, json={"contact": {"custom_field": {"lead_status": status}}})
                return res.status_code == 200
        except Exception as e:
            return False

    def log_call(self, external_id: str, transcript: str, summary: str) -> bool:
        try:
            with httpx.Client() as client:
               url = f"{self.base_api_url}/contacts/{external_id}/activities"
                res = client.post(url, headers=self.headers, json={"activity": {"title": "AI Agent Call", "notes": summary, "targetable_type": "Contact", "targetable_id": external_id}})
                return res.status_code == 200
        except Exception as e:
            return False
