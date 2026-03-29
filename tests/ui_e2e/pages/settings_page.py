from playwright.sync_api import expect
from tests.ui_e2e.pages.base_page import BasePage

class SettingsPage(BasePage):
    def go_to_settings(self):
        self.page.click("button:has-text('Settings')")

    def add_product(self, product_name):
        self.page.click("button:has-text('+ Add Product')")
        # In the settings tab, it's typically an input that shows up.
        # Ensure we wait for it
        self.page.fill("input[placeholder*='e.g., Enterprise Software']", product_name)
        # Hit Enter or the Add button next to it
        self.page.press("input[placeholder*='e.g., Enterprise Software']", "Enter")

    def get_product(self, product_name):
        return self.page.locator(f"div:has-text('{product_name}')").locator(".product-item-actions")

    def delete_product(self, product_name):
        self.page.once("dialog", lambda dialog: dialog.accept())
        prod_actions = self.get_product(product_name)
        prod_actions.locator("button[title='Delete Product']").click()
