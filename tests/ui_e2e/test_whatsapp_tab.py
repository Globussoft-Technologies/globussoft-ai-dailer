import time
from playwright.sync_api import expect


def test_whatsapp_tab_loads(auth_page, base_url):
    """Test that the WhatsApp tab loads and shows the main heading."""
    auth_page.goto(f"{base_url}?_cb={int(time.time())}")
    time.sleep(2)

    auth_page.locator('button:has-text("WhatsApp")').first.click()
    auth_page.wait_for_load_state("networkidle")
    time.sleep(1)

    expect(
        auth_page.get_by_text("WhatsApp Outbound Automated Logs")
    ).to_be_visible(timeout=8000)


def test_whatsapp_empty_state(auth_page, base_url):
    """Test that empty state message is visible when no logs exist."""
    auth_page.goto(f"{base_url}?_cb={int(time.time())}")
    time.sleep(2)

    auth_page.locator('button:has-text("WhatsApp")').first.click()
    auth_page.wait_for_load_state("networkidle")
    time.sleep(1)

    # Exact text from WhatsAppTab.jsx
    expect(
        auth_page.get_by_text("No WhatsApp triggers sent yet")
    ).to_be_visible(timeout=8000)
