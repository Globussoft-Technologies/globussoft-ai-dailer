import os
import time
import pytest
import requests
from playwright.sync_api import Page, expect

BASE_URL = os.getenv("E2E_BASE_URL", "https://test.callified.ai")
TEST_SESSION_ID = int(time.time())
TEST_USER_EMAIL = os.getenv("E2E_USER_EMAIL", f"e2esession_{TEST_SESSION_ID}@globussoft.com")
TEST_USER_PW = os.getenv("E2E_USER_PASSWORD", "AutoTest!2026")

# Track whether the session user has been created yet
_user_created = False
_auth_token = None


def navigate_with_cache_bust(page, url="/"):
    """Navigate with a cache-busting query param to avoid stale Cloudflare cache."""
    separator = "&" if "?" in url else "?"
    page.goto(f"{url}{separator}_cb={int(time.time())}")


@pytest.fixture(scope="session")
def base_url():
    return BASE_URL


@pytest.fixture
def auth_context(browser, base_url):
    """
    Creates a new browser context. Signs up on first call, logs in on subsequent calls.
    Returns the authenticated context.
    """
    global _user_created, _auth_token

    context = browser.new_context(base_url=base_url, viewport={"width": 1280, "height": 800})
    page = context.new_page()
    navigate_with_cache_bust(page, base_url)

    # Wait for React app to render
    page.wait_for_selector('input[type="email"]', timeout=15000)

    if not _user_created:
        # First time: sign up
        page.locator("button:has-text('Sign Up')").first.click()
        page.wait_for_selector('input[placeholder="e.g. Globussoft"]', timeout=5000)
        page.fill('input[placeholder="e.g. Globussoft"]', "Automated Testing Org")
        page.fill('input[placeholder="e.g. Sumit Kumar"]', "Automated Tester")
        page.fill('input[type="email"]', TEST_USER_EMAIL)
        page.fill('input[type="password"]', TEST_USER_PW)
        page.locator("button.btn-primary").click()
        page.wait_for_selector("button:has-text('Logout')", timeout=15000)

        # Grab auth token from API for cleanup later
        try:
            resp = requests.post(f"{base_url}/api/auth/login", json={
                "email": TEST_USER_EMAIL, "password": TEST_USER_PW
            })
            if resp.status_code == 200:
                _auth_token = resp.json().get("access_token")
        except Exception:
            pass

        _user_created = True
    else:
        # Subsequent: login with existing credentials
        page.locator("button:has-text('Login')").first.click()
        page.fill('input[type="email"]', TEST_USER_EMAIL)
        page.fill('input[type="password"]', TEST_USER_PW)
        page.locator("button.btn-primary").click()
        page.wait_for_selector("button:has-text('Logout')", timeout=15000)

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


def pytest_sessionfinish(session, exitstatus):
    """Auto-cleanup: delete E2E test data after the test session completes."""
    if not _auth_token:
        return
    headers = {"Authorization": f"Bearer {_auth_token}"}
    try:
        # Delete test leads created during this session
        resp = requests.get(f"{BASE_URL}/api/leads", headers=headers)
        if resp.status_code == 200:
            for lead in resp.json():
                name = (lead.get("first_name") or "")
                if name.startswith("E2E") or name.startswith("SearchTest"):
                    requests.delete(f"{BASE_URL}/api/leads/{lead['id']}", headers=headers)
    except Exception:
        pass
