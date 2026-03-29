import os
import time
from tests.ui_e2e.pages.auth_page import AuthPage

def test_valid_login(browser, base_url):
    # Do not use auth_page fixture here so we start unauthenticated
    page = browser.new_page()
    auth_pg = AuthPage(page, base_url)
    
    email = os.getenv("E2E_USER_EMAIL", f"e2e_tester_{int(time.time())}@globussoft.com")
    pw = os.getenv("E2E_USER_PASSWORD", "E2eTestUser!2026")
    
    auth_pg.signup("E2E Org", "E2E Tester", email, pw)
    try:
        auth_pg.check_login_success()
    except Exception as e:
        print("\n\n--- PAGE HTML ---\n")
        print(page.content())
        raise e
    page.close()
