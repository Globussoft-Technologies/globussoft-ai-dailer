import asyncio
from crm_providers.hubspot import HubSpotCRM
from crm_providers.salesforce import SalesforceCRM
from crm_providers.zoho import ZohoCRM

def test_hubspot():
    print("Testing HubSpot...")
    crm = HubSpotCRM(api_key="dummy_token")
    leads = crm.fetch_new_leads()
    print(f"HubSpot Leads Fetched: {len(leads)}")
    res = crm.log_call("123", "Hello", "Summary")
    print(f"HubSpot Log Call Result: {res}")

def test_salesforce():
    print("Testing Salesforce...")
    crm = SalesforceCRM(api_key="dummy_token", base_url="https://instance.my.salesforce.com")
    leads = crm.fetch_new_leads()
    print(f"Salesforce Leads Fetched: {len(leads)}")
    res = crm.log_call("123", "Hello", "Summary")
    print(f"Salesforce Log Call Result: {res}")

def test_zoho():
    print("Testing Zoho...")
    crm = ZohoCRM(api_key="dummy_token")
    leads = crm.fetch_new_leads()
    print(f"Zoho Leads Fetched: {len(leads)}")
    res = crm.log_call("123", "Hello", "Summary")
    print(f"Zoho Log Call Result: {res}")

if __name__ == "__main__":
    test_hubspot()
    test_salesforce()
    test_zoho()
    print("All CRM verifications ran without unhandled exceptions.")
