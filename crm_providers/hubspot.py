import httpx
from typing import List, Dict
from datetime import datetime, timezone
import json
from . import BaseCRM

class HubSpotCRM(BaseCRM):
    def __init__(self, api_key: str, base_url: str = ""):
        super().__init__(api_key, base_url)
        self.headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json"
        }
        self.base_uri = "https://api.hubapi.com"

    def fetch_new_leads(self) -> List[Dict]:
        """
        Fetches contacts from HubSpot that have a phone number but no 'dialer_status' yet.
        """
        # We query the contacts API to get new leads.
        # In HubSpot, we filter for contacts with phone numbers.
        url = f"{self.base_uri}/crm/v3/objects/contacts/search"
        payload = {
            "filterGroups": [
                {
                    "filters": [
                        {
                            "propertyName": "phone",
                            "operator": "HAS_PROPERTY"
                        }
                    ]
                }
            ],
            "properties": ["firstname", "lastname", "phone", "lifecyclestage", "lead_status"],
            "limit": 50
        }
        
        leads = []
        try:
            with httpx.Client() as client:
                res = client.post(url, headers=self.headers, json=payload)
                if res.status_code == 200:
                    data = res.json()
                    for contact in data.get('results', []):
                        props = contact.get('properties', {})
                        leads.append({
                            "external_id": contact.get('id'),
                            "first_name": props.get('firstname', 'Unknown'),
                            "last_name": props.get('lastname', ''),
                            "phone": props.get('phone', ''),
                            "source": "HubSpot"
                        })
        except Exception as e:
            print(f"HubSpot fetch error: {e}")
            
        return leads

    def update_lead_status(self, external_id: str, status: str) -> bool:
        """
        Updates the contact's lead status in HubSpot.
        """
        url = f"{self.base_uri}/crm/v3/objects/contacts/{external_id}"
        payload = {
            "properties": {
                "hs_lead_status": status.upper() # Maps to HubSpot lead status (NEW, OPEN, IN_PROGRESS, OPEN_DEAL, UNQUALIFIED)
            }
        }
        try:
            with httpx.Client() as client:
                res = client.patch(url, headers=self.headers, json=payload)
                return res.status_code in [200, 204]
        except Exception as e:
            print(f"HubSpot update status error: {e}")
            return False

    def log_call(self, external_id: str, transcript: str, summary: str) -> bool:
        """
        Logs an Engagement (Call) on the HubSpot contact.
        """
        # Create an engagement object (call) and associate it with the contact.
        url = f"{self.base_uri}/crm/v3/objects/calls"
        
        call_body = f"AI Dialer Summary:\n{summary}\n\nFull Transcript:\n{transcript}"
        
        payload = {
            "properties": {
                "hs_call_body": call_body,
                "hs_call_title": "AI Sales Call",
                "hs_call_status": "COMPLETED",
                "hs_timestamp": datetime.now(timezone.utc).isoformat()
            },
            "associations": [
                {
                    "to": {"id": external_id},
                    "types": [
                        {
                            "associationCategory": "HUBSPOT_DEFINED",
                            "associationTypeId": 194 # Call to Contact association type
                        }
                    ]
                }
            ]
        }
        
        try:
            with httpx.Client() as client:
                res = client.post(url, headers=self.headers, json=payload)
                return res.status_code in [200, 201]
        except Exception as e:
            print(f"HubSpot log call error: {e}")
            return False
