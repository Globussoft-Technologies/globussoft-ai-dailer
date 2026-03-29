import time
from tests.ui_e2e.pages.settings_page import SettingsPage
from playwright.sync_api import expect

def test_add_delete_product(auth_page, base_url):
    settings = SettingsPage(auth_page, base_url)
    auth_page.goto(base_url)
    time.sleep(1)
    
    settings.go_to_settings()
    
    prod_name = "E2E Automated Product"
    settings.add_product(prod_name)
    
    # Check it appeared
    expect(auth_page.locator(f"text='{prod_name}'")).to_be_visible(timeout=5000)
    
    settings.delete_product(prod_name)
    
    # Should be gone
    expect(auth_page.locator(f"text='{prod_name}'")).to_be_hidden(timeout=5000)
