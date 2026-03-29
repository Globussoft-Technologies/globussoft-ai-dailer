import os
import time
import pytest
from playwright.sync_api import Page, expect

BASE_URL = os.getenv("E2E_BASE_URL", "https://test.callified.ai")
# Use a static-ish fixture email for the lifetime of this test session so state is shared if parallelized
TEST_SESSION_ID = int(time.time())
TEST_USER_EMAIL = os.getenv("E2E_USER_EMAIL", f"e2esession_{TEST_SESSION_ID}@globussoft.com")
TEST_USER_PW = os.getenv("E2E_USER_PASSWORD", "AutoTest!2026")

@pytest.fixture(scope="session")
def base_url():
    return BASE_URL

@pytest.fixture
def auth_context(browser, base_url):
    """
    Creates a new browser context and logs the user in.
    Returns the context which can be used to spawn pages already authenticated.
    """
    context = browser.new_context(base_url=base_url, viewport={'width': 1280, 'height': 800})
    page = context.new_page()
    page.goto("/")
    
    # We might be on Login or Dashboard. Check if email input exists.
    try:
        # Wait up to 3 seconds for email input
        page.wait_for_selector('input[type="email"]', timeout=3000)
    except Exception:
        # Already logged in or something went wrong. Let's return.
        pass

    if page.is_visible('input[type="email"]'):
        # Just sign up dynamically. It logs you in automatically!
        page.click("button:has-text('Sign Up')")
        page.fill('input[placeholder="e.g. Globussoft"]', "Automated Testing Org")
        page.fill('input[placeholder="e.g. Sumit Kumar"]', "Automated Tester")
        page.fill('input[type="email"]', TEST_USER_EMAIL)
        page.fill('input[type="password"]', TEST_USER_PW)
        page.click('button[type="submit"]')
        page.wait_for_selector('.header', timeout=10000)

    # Save storage state to a file if needed, or just yield the authenticated context
    yield context
    context.close()

@pytest.fixture
def auth_page(auth_context):
    """
    Yields a page object that is already authenticated.
    """
    page = auth_context.new_page()
    yield page
    page.close()
