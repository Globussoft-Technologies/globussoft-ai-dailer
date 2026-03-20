import httpx
from typing import List, Dict
import json
from . import BaseCRM

class ActiveCampaignCRM(BaseCRM):
    def __init__(self, **credentials):
        super().__init__(**credentials)
        self.base_api_url = self.base_url.rstrip('/') if self.base_url else "https://youraccount.api-us1.com/api/3"
        self.headers = {"Api-Token": self.api_key, "Content-Type": "application/json"}

    def fetch_new_leads(self) -> List[Dict]:
        leads = []
        try:
            with httpx.Client() as client:
               res = client.get(f"{self.base_api_url}/contacts", headers=self.headers)
                if res.status_code == 200:
                    for item in res.json().get('contacts', []):
                        if item.get('phone'):
                            leads.append({"external_id": item['id'], "first_name": item.get('firstName',''), "last_name": item.get('lastName',''), "phone": item['phone'], "source": "ActiveCampaign"})
        except Exception as e:
            print(f"{self.__class__.__name__} Fetch Error: {e}")
        return leads

    def update_lead_status(self, external_id: str, status: str) -> bool:
        try:
            with httpx.Client() as client:
               url = f"{self.base_api_url}/contacts/{external_id}"
                res = client.put(url, headers=self.headers, json={"contact": {"fieldValues": [{"field": "status", "value": status}]}})
                return res.status_code == 200
        except Exception as e:
            return False

    def log_call(self, external_id: str, transcript: str, summary: str) -> bool:
        try:
            with httpx.Client() as client:
               url = f"{self.base_api_url}/notes"
                res = client.post(url, headers=self.headers, json={"note": {"note": f"AI Call:\n{summary}", "relid": external_id, "reltype": "Subscriber"}})
                return res.status_code == 201
        except Exception as e:
            return False
