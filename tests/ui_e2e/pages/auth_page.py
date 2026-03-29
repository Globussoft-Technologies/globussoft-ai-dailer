from playwright.sync_api import expect
from tests.ui_e2e.pages.base_page import BasePage

class AuthPage(BasePage):
    def login(self, email, password):
        self.page.goto(self.base_url)
        self.page.fill('input[type="email"]', email)
        self.page.fill('input[type="password"]', password)
        self.page.click('button[type="submit"]')
        
    def check_login_success(self):
        expect(self.page.locator(".header")).to_be_visible(timeout=10000)

    def signup(self, org_name, full_name, email, password):
        self.page.goto(self.base_url)
        self.page.click("button:has-text('Sign Up')")
        self.page.fill('input[placeholder="e.g. Globussoft"]', org_name)
        self.page.fill('input[placeholder="e.g. Sumit Kumar"]', full_name)
        self.page.fill('input[type="email"]', email)
        self.page.fill('input[type="password"]', password)
        self.page.click('button[type="submit"]')
        
    def logout(self):
        # Assumes a logout button exists in top header or user menu
        self.page.click("button:has-text('Logout'), button[title='Logout']")
        expect(self.page.locator('input[type="email"]')).to_be_visible(timeout=5000)
