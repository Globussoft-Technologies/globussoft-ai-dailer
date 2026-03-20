import httpx
from typing import List, Dict
import json
from . import BaseCRM

class CloseCRM(BaseCRM):
    def __init__(self, **credentials):
        super().__init__(**credentials)
        self.base_api_url = "https://api.close.com/api/v1"
        import base64
        auth_str = base64.b64encode(f"{self.api_key}:".encode()).decode()
        self.headers = {"Authorization": f"Basic {auth_str}", "Content-Type": "application/json"}

    def fetch_new_leads(self) -> List[Dict]:
        leads = []
        try:
            with httpx.Client() as client:
               res = client.get(f"{self.base_api_url}/lead/", headers=self.headers)
                if res.status_code == 200:
                    for item in res.json().get('data', []):
                        contacts = item.get('contacts', [])
                        if contacts and contacts[0].get('phones'):
                            leads.append({"external_id": item['id'], "first_name": contacts[0].get('name',''), "last_name": '', "phone": contacts[0]['phones'][0]['phone'], "source": "Close"})
        except Exception as e:
            print(f"{self.__class__.__name__} Fetch Error: {e}")
        return leads

    def update_lead_status(self, external_id: str, status: str) -> bool:
        try:
            with httpx.Client() as client:
               url = f"{self.base_api_url}/lead/{external_id}/"
                res = client.put(url, headers=self.headers, json={"status_id": status})
                return res.status_code == 200
        except Exception as e:
            return False

    def log_call(self, external_id: str, transcript: str, summary: str) -> bool:
        try:
            with httpx.Client() as client:
               url = f"{self.base_api_url}/activity/call/"
                res = client.post(url, headers=self.headers, json={"lead_id": external_id, "note": summary, "status": "completed"})
                return res.status_code == 200
        except Exception as e:
            return False
