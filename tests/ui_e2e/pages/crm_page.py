from playwright.sync_api import expect
from tests.ui_e2e.pages.base_page import BasePage

class CrmPage(BasePage):
    def navigate_to_tab(self):
        # TopNav has a button "CRM & Dashboard"
        self.page.click("button >> text='CRM'")
        
    def click_add_lead(self):
        # Click the "Add Lead" button inside the CRM tab
        self.page.click("button:has-text('Add Lead')")
        
    def fill_and_submit_lead(self, first_name, last_name, phone):
        # The modal uses placeholders for identification because name attrs were missing on the backend build
        self.page.fill('input[placeholder="e.g. John"]', first_name)
        self.page.fill('input[placeholder="e.g. Doe"]', last_name)
        self.page.fill('input[placeholder="+917406317771"]', phone)
        self.page.click('button:has-text("Save Lead")')
        
    def get_lead_row(self, phone):
        # Search the table for the lead
        return self.page.locator(f"tr:has-text('{phone}')")
        
    def edit_lead(self, phone, new_last_name):
        row = self.get_lead_row(phone)
        row.locator('button:has-text("Edit")').click()
        # Ensure we are editing
        self.page.wait_for_selector('h2:has-text("Edit Lead")', timeout=5000)
        # Edit Modal doesn't have placeholders, but it is the second modal input.
        # So we can use sequential locators or ID. 
        # Alternatively, find the label and get the next input.
        self.page.locator("label:has-text('Last Name') + input").fill(new_last_name)
        self.page.click('button:has-text("Update Lead")')
        
    def delete_lead(self, phone):
        row = self.get_lead_row(phone)
        # Playwright auto-accepts dialogs if we bind an event before clicking
        self.page.once("dialog", lambda dialog: dialog.accept())
        row.locator('button:has-text("🗑️")').click()
