import time
from playwright.sync_api import expect
from tests.ui_e2e.pages.settings_page import SettingsPage


def test_add_delete_product(auth_page, base_url):
    """Test adding and deleting a product in the Settings tab."""
    settings = SettingsPage(auth_page, base_url)
    settings.navigate_with_cache_bust()
    time.sleep(1)

    settings.go_to_settings()
    time.sleep(1)

    prod_name = f"E2E Product {int(time.time())}"
    settings.add_product(prod_name)
    time.sleep(2)

    # Verify it appeared (product name shown as span text)
    expect(auth_page.locator(f"span:text-is('{prod_name}')")).to_be_visible(timeout=8000)

    # Delete it
    settings.delete_product(prod_name)
    time.sleep(1)

    # Verify it's gone
    expect(auth_page.locator(f"span:text-is('{prod_name}')")).to_be_hidden(timeout=8000)


def test_add_delete_pronunciation(auth_page, base_url):
    """Test adding and removing a pronunciation rule."""
    settings = SettingsPage(auth_page, base_url)
    settings.navigate_with_cache_bust()
    time.sleep(1)

    settings.go_to_settings()
    time.sleep(1)

    word = f"E2EWord{int(time.time())}"
    phonetic = "Ee Two Ee Word"

    settings.add_pronunciation_rule(word, phonetic)

    # Should appear in the table
    expect(auth_page.locator(f"tr:has-text('{word}')")).to_be_visible(timeout=8000)

    # Delete it
    settings.delete_pronunciation_rule(word)

    # Should be gone
    expect(auth_page.locator(f"tr:has-text('{word}')")).to_be_hidden(timeout=8000)
