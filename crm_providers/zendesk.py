import httpx
from typing import List, Dict
import json
from . import BaseCRM

class ZendeskCRM(BaseCRM):
    def __init__(self, **credentials):
        super().__init__(**credentials)
        self.base_api_url = self.base_url.rstrip('/') if self.base_url else "https://api.getbase.com/v2" # Zendesk Sell (Base CRM)
        self.headers = {"Accept": "application/json", "Authorization": f"Bearer {self.api_key}"}

    def fetch_new_leads(self) -> List[Dict]:
        leads = []
        try:
            with httpx.Client() as client:
               res = client.get(f"{self.base_api_url}/leads", headers=self.headers)
                if res.status_code == 200:
                    for item in res.json().get('items', []):
                        data = item.get('data', {})
                        if data.get('mobile') or data.get('phone'):
                            leads.append({"external_id": data['id'], "first_name": data.get('first_name',''), "last_name": data.get('last_name',''), "phone": data.get('mobile') or data.get('phone'), "source": "Zendesk"})
        except Exception as e:
            print(f"{self.__class__.__name__} Fetch Error: {e}")
        return leads

    def update_lead_status(self, external_id: str, status: str) -> bool:
        try:
            with httpx.Client() as client:
               url = f"{self.base_api_url}/leads/{external_id}"
                res = client.put(url, headers=self.headers, json={"data": {"status": status}})
                return res.status_code == 200
        except Exception as e:
            return False

    def log_call(self, external_id: str, transcript: str, summary: str) -> bool:
        try:
            with httpx.Client() as client:
               url = f"{self.base_api_url}/notes"
                res = client.post(url, headers=self.headers, json={"data": {"resource_type": "lead", "resource_id": external_id, "content": summary}})
                return res.status_code == 200
        except Exception as e:
            return False
