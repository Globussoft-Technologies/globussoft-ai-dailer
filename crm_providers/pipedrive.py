import httpx
from typing import List, Dict
import json
from . import BaseCRM

class PipedriveCRM(BaseCRM):
    def __init__(self, **credentials):
        super().__init__(**credentials)
        self.base_api_url = self.base_url.rstrip('/') if self.base_url else "https://api.pipedrive.com/v1"
        self.headers = {"Accept": "application/json"}

    def fetch_new_leads(self) -> List[Dict]:
        leads = []
        try:
            with httpx.Client() as client:
               res = client.get(f"{self.base_api_url}/persons?api_token={self.api_key}", headers=self.headers)
                if res.status_code == 200:
                    for item in res.json().get('data', []) or []:
                        if item.get('phone') and len(item['phone']) > 0:
                            leads.append({"external_id": str(item['id']), "first_name": item.get('first_name', item.get('name','')), "last_name": item.get('last_name',''), "phone": item['phone'][0].get('value',''), "source": "Pipedrive"})
        except Exception as e:
            print(f"{self.__class__.__name__} Fetch Error: {e}")
        return leads

    def update_lead_status(self, external_id: str, status: str) -> bool:
        try:
            with httpx.Client() as client:
               url = f"{self.base_api_url}/persons/{external_id}?api_token={self.api_key}"
                res = client.put(url, headers=self.headers, json={"label": status})
                return res.status_code == 200
        except Exception as e:
            return False

    def log_call(self, external_id: str, transcript: str, summary: str) -> bool:
        try:
            with httpx.Client() as client:
               url = f"{self.base_api_url}/activities?api_token={self.api_key}"
                res = client.post(url, headers=self.headers, json={"subject": "AI Call", "type": "call", "person_id": int(external_id), "note": f"{summary}<br><br>{transcript}", "done": 1})
                return res.status_code == 201
        except Exception as e:
            return False
