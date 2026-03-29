import time
from tests.ui_e2e.pages.crm_page import CrmPage
from playwright.sync_api import expect

def test_add_edit_delete_lead(auth_page, base_url):
    crm_page = CrmPage(auth_page, base_url)
    auth_page.goto(base_url)
    
    # Optional wait if dashboard takes time
    time.sleep(2)
    
    import random
    crm_page.click_add_lead()
    test_phone = f"+91{random.randint(100000000, 999999999)}"
    
    crm_page.fill_and_submit_lead("E2E", "TestSubject", test_phone)
    
    # Wait for table to reflect the new lead.
    # The lead row should be visible
    row = crm_page.get_lead_row(test_phone)
    expect(row).to_be_visible(timeout=8000)
    
    # Edit the lead
    crm_page.edit_lead(test_phone, "EditedSubject")
    
    # Wait for the edit to reflect
    expect(auth_page.locator(f"tr:has-text('{test_phone}')").locator("td:nth-child(1)")).to_contain_text("EditedSubject")
    
    # Now Delete the lead
    crm_page.delete_lead(test_phone)
    
    # Wait for the row to disappear
    expect(crm_page.get_lead_row(test_phone)).to_be_hidden(timeout=10000)
