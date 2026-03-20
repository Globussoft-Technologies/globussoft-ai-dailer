import httpx
from typing import List, Dict
import json
from . import BaseCRM

class GoldMineCRM(BaseCRM):
    def __init__(self, **credentials):
        super().__init__(**credentials)
        self.base_api_url = self.base_url.rstrip('/') if self.base_url else "https://api.goldmine.com/v1"
        self.headers = {"Authorization": f"Bearer {self.api_key}", "Content-Type": "application/json"}

    def fetch_new_leads(self) -> List[Dict]:
        leads = []
        try:
            with httpx.Client() as client:
               res = client.get(f"{self.base_api_url}/leads", headers=self.headers)
                if res.status_code == 200:
                    for item in res.json().get('data', []):
                        if item.get('phone'):
                            leads.append({"external_id": item['id'], "first_name": item.get('first_name',''), "last_name": item.get('last_name',''), "phone": item['phone'], "source": "GoldMine"})
        except Exception as e:
            print(f"{self.__class__.__name__} Fetch Error: {e}")
        return leads

    def update_lead_status(self, external_id: str, status: str) -> bool:
        try:
            with httpx.Client() as client:
               url = f"{self.base_api_url}/leads/{external_id}"
                res = client.patch(url, headers=self.headers, json={"status": status})
                return res.status_code in [200, 204]
        except Exception as e:
            return False

    def log_call(self, external_id: str, transcript: str, summary: str) -> bool:
        try:
            with httpx.Client() as client:
               url = f"{self.base_api_url}/leads/{external_id}/calls"
                res = client.post(url, headers=self.headers, json={"description": summary, "transcript": transcript})
                return res.status_code in [200, 201]
        except Exception as e:
            return False
