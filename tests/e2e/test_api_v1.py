import pytest
import os
import requests

# End-to-End API Integration Suite running entirely against Remote Infrastructure

@pytest.fixture(scope="module")
def api_client(remote_client):
    """
    Session-level initialization: Ensure that tests have an organic lead space natively on test.callified.ai
    """
    return remote_client

def test_debug_health(api_client):
    res = api_client.get("/api/debug/health")
    assert res.status_code == 200
    assert res.json().get("status") in ["ok", "healthy"]

def test_lead_lifecycle(api_client):
    # 1. Create a Test Lead mapped specifically for remote integration testing
    payload = {
        "first_name": "E2ETestUser",
        "last_name": "Autogen",
        "phone": "+919999999000",
        "source": "E2E_Runner",
        "interest": "High",
        "org_id": 1,
        "status": "new"
    }
    create_res = api_client.post("/api/leads", json=payload)
    
    # If it's already created due to earlier failure, delete and try again
    if create_res.status_code != 200 or "error" in (create_res.json().get("status", "")):
        all_leads = api_client.get("/api/leads").json()
        for ld in all_leads:
            if ld.get("phone") == "+919999999000":
                api_client.delete(f"/api/leads/{ld['id']}")
        create_res = api_client.post("/api/leads", json=payload)

    assert create_res.status_code == 200, f"Failed: {create_res.text}"
    lead_id = create_res.json().get("id")
    assert lead_id is not None

    # 2. Get Leads & Validate
    leads_res = api_client.get("/api/leads")
    assert leads_res.status_code == 200
    assert any(l["id"] == lead_id for l in leads_res.json())

    # 3. Update Status natively via API
    up_res = api_client.put(f"/api/leads/{lead_id}/status", json={"status": "Warm"})
    assert up_res.status_code == 200

    # 4. Delete the Lead natively to clean up test data
    del_res = api_client.delete(f"/api/leads/{lead_id}")
    assert del_res.status_code == 200

def test_metrics_endpoints(api_client):
    # Server endpoints
    assert api_client.get("/api/reports").status_code == 200
    assert api_client.get("/api/analytics").status_code == 200
    assert api_client.get("/api/organizations").status_code == 200

def test_organization_lifecycle(api_client):
    # Testing creation and destruction of a remote organization
    payload = {"name": "E2E_Test_Corp"}
    org_res = api_client.post("/api/organizations", json=payload)
    if org_res.status_code == 200:
        org_id = org_res.json().get("id")
        
        # Test Products specific to org
        p_res = api_client.post(f"/api/organizations/{org_id}/products", json={"name": "E2E_Product", "website_url": ""})
        if p_res.status_code == 200:
            p_id = p_res.json().get("id")
            api_client.delete(f"/api/products/{p_id}")

        # Delete org natively
        assert api_client.delete(f"/api/organizations/{org_id}").status_code == 200

def test_mobile_api_integrity(api_client):
    # Ensure Mobile App dependencies (Agent login/punches etc) work on the remote network
    assert api_client.get("/api/mobile/leads").status_code == 200
    assert api_client.get("/api/mobile/analytics").status_code == 200
    assert api_client.get("/api/mobile/tasks").status_code == 200

    punch_payload = {"agent_name": "E2E_Agent", "site_id": 9999, "lat": 10.0, "lon": 10.0}
    # It might properly reject 9999 because site doesn't exist, preserving db integrity.
    # As long as it throws exactly an API contract error, the route is proven alive.
    ans = api_client.post("/api/mobile/punch", json=punch_payload)
    assert ans.status_code in [200, 400, 404]

def test_settings_overrides(api_client):
    target_org = 1 # We use org 1
    sys_res = api_client.get(f"/api/organizations/{target_org}/system-prompt")
    assert sys_res.status_code == 200
    
    # We won't maliciously mutate the exact system-prompt for tests so we just confirm GETs
    voice_res = api_client.get(f"/api/organizations/{target_org}/voice-settings")
    assert voice_res.status_code == 200
