from abc import ABC, abstractmethod
from typing import List, Dict

class BaseCRM(ABC):
    """
    Abstract Base Class for CRM Integrations.
    Defines the standard interface that all CRM providers must implement.
    """
    
    def __init__(self, **credentials):
        self.credentials = credentials
        # Deprecated simple extraction for backwards compatibility if needed, 
        # but modern subclasses will extract exactly what they need from self.credentials
        self.api_key = credentials.get("api_key", "")
        self.base_url = credentials.get("base_url", "")

    @abstractmethod
    def fetch_new_leads(self) -> List[Dict]:
        """
        Retrieves untested/unqualified leads from the CRM.
        Must return a list of dicts formatted as:
        {
            "first_name": "John",
            "last_name": "Doe",
            "phone": "+1234567890",
            "source": "HubSpot",
            "external_id": "123456"
        }
        """
        pass

    @abstractmethod
    def update_lead_status(self, external_id: str, status: str) -> bool:
        """
        Pushes back qualification logic to the CRM.
        """
        pass

    @abstractmethod
    def log_call(self, external_id: str, transcript: str, summary: str) -> bool:
        """
        Pushes physical call log and AI summary back to the CRM.
        """
        pass
