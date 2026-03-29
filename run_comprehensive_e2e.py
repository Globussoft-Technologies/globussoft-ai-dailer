import os
import time
import uuid
import random
from playwright.sync_api import sync_playwright, expect

def random_digits(n):
    return "".join(str(random.randint(0,9)) for _ in range(n))

def run_tests():
    print("Starting Comprehensive E2E Features Test on https://test.callified.ai/")
    results = {}
    
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={'width': 1280, 'height': 800})
        context.grant_permissions(['geolocation'])
        context.set_geolocation({"latitude": 37.7749, "longitude": -122.4194})
        page = context.new_page()
        page.on("dialog", lambda dialog: dialog.accept("Automated Testing Note"))

        # 1. Navigation & Auth
        print("\n1. Testing Auth & Sign Up...")
        page.goto("https://test.callified.ai/")
        try:
            page.wait_for_selector('input[type="email"]', timeout=5000)
            page.click("button:has-text('Sign Up')")
            test_email = f"e2e_{int(time.time())}@globussoft.com"
            page.fill('input[placeholder="e.g. Globussoft"]', "Automated E2E Org")
            page.fill('input[placeholder="e.g. Sumit Kumar"]', "E2E Tester")
            page.fill('input[type="email"]', test_email)
            page.fill('input[type="password"]', "TestPass!123")
            page.click('button[type="submit"]')
            page.wait_for_selector('.header', timeout=15000)
            results['Auth & Signup'] = 'PASS'
            print(" -> PASS: Successfully signed up and logged in.")
        except Exception as e:
            results['Auth & Signup'] = f'FAIL: {e}'
            print(f" -> FAIL: {e}")

        time.sleep(2)

        # 2. Settings Tab
        print("\n2. Testing Settings Tab Features...")
        try:
            page.click("button:has-text('⚙️ Settings')")
            time.sleep(1)
            
            # Product
            product_name = f"Test Product {uuid.uuid4().hex[:6]}"
            page.click("button:has-text('+ Add Product')")
            page.fill('input[placeholder="Product name (e.g. AdsGPT)..."]', product_name)
            page.click("button:has-text('Add')")
            expect(page.locator(f"text='{product_name}'").first).to_be_visible(timeout=5000)
            results['Settings - Add Product'] = 'PASS'
            print(" -> PASS: Added new product.")
        except Exception as e:
            results['Settings - Add Product'] = f'FAIL: {e}'
            print(f" -> FAIL: Add Product")

        try:
            page.fill("textarea", "You are an AI sales assistant. Be extremely concise.")
            page.click("button:has-text('💾 Save Prompt')")
            results['Settings - Update Prompt'] = 'PASS'
            print(" -> PASS: Updated System Prompt.")
        except Exception as e:
            results['Settings - Update Prompt'] = f'FAIL: {e}'
            print(f" -> FAIL: Update Prompt")

        try:
            page.fill('input[placeholder="e.g. Adsgpt"]', "Globussoft")
            page.fill('input[placeholder="e.g. Ads G P T"]', "Glow-bus-soft")
            page.click("button:has-text('+ Add Rule')")
            expect(page.locator(f"text='Globussoft'").first).to_be_visible(timeout=5000)
            results['Settings - Pronunciation Guide'] = 'PASS'
            print(" -> PASS: Added pronunciation rule.")
        except Exception as e:
            results['Settings - Pronunciation Guide'] = f'FAIL: {e}'
            print(f" -> FAIL: Pronunciation Guide")

        # 3. CRM Tab
        print("\n3. Testing CRM Tab Features...")
        phone_num = f"+9198{random_digits(8)}"
        try:
            page.click("button:has-text('📊 CRM')")
            time.sleep(1)
            
            page.click("button:has-text('Add Lead')")
            page.fill("input[placeholder='First Name']", "E2E")
            page.fill("input[placeholder='Last Name']", "Lead")
            page.fill("input[placeholder='Phone']", phone_num)
            page.select_option("select", "Web Form")
            page.click("button:has-text('Save Lead')")
            
            expect(page.locator(f"tr:has-text('{phone_num}')").first).to_be_visible(timeout=10000)
            results['CRM - Add Lead'] = 'PASS'
            print(" -> PASS: Lead successfully created.")
        except Exception as e:
            results['CRM - Add Lead'] = f'FAIL: {e}'
            print(f" -> FAIL: Add Lead")

        time.sleep(1)

        try:
            page.locator(f"tr:has-text('{phone_num}')").first.locator("button[title='Edit']").click()
            page.fill("input[placeholder='First Name']", "E2E_Edited")
            page.click("button:has-text('Save Modifications')")
            expect(page.locator(f"tr:has-text('{phone_num}')").first.locator("td:nth-child(1)")).to_contain_text("E2E_Edited")
            results['CRM - Edit Lead'] = 'PASS'
            print(" -> PASS: Lead successfully edited.")
        except Exception as e:
            results['CRM - Edit Lead'] = f'FAIL: {e}'
            print(f" -> FAIL: Edit Lead")

        # 4. Integrations Tab
        print("\n4. Testing Integrations Tab Features...")
        try:
            page.click("button:has-text('🔌 Integrations')")
            time.sleep(1)
            page.select_option("select", label="HubSpot")
            page.locator("input[type='password']").fill("fake_hubspot_key_123")
            page.click("button:has-text('Save Configuration')")
            results['Integrations - Add HubSpot'] = 'PASS'
            print(" -> PASS: Processed HubSpot integration save.")
        except Exception as e:
            results['Integrations - Add HubSpot'] = f'FAIL: {e}'
            print(f" -> FAIL: Add HubSpot")

        # 5. Ops Tab
        print("\n5. Testing Field Ops Tab Features...")
        try:
            page.click("button:has-text('📋 Ops & Tasks')")
            time.sleep(1)
            page.fill("input[placeholder='Your Full Name']", "E2E Field Agent")
            page.select_option("select", index=1)
            page.click("button:has-text('Punch In / Check Location')")
            results['Ops - Field Check-In'] = 'PASS'
            print(" -> PASS: Field operation check-in triggered.")
        except Exception as e:
            results['Ops - Field Check-In'] = f'FAIL: {e}'
            print(f" -> FAIL: Ops Check-In")

        # 6. Delete Lead to cleanup
        print("\n6. Cleaning up CRM module...")
        try:
            page.click("button:has-text('📊 CRM')")
            time.sleep(1)
            page.locator(f"tr:has-text('{phone_num}')").first.locator("button[title='Delete']").click()
            expect(page.locator(f"tr:has-text('{phone_num}')").first).to_be_hidden(timeout=10000)
            results['CRM - Delete Lead'] = 'PASS'
            print(" -> PASS: Lead successfully deleted.")
        except Exception as e:
            results['CRM - Delete Lead'] = f'FAIL: {e}'
            print(f" -> FAIL: Delete Lead")

        print("\n--- TEST RESULTS SUMMARY ---")
        for k, v in results.items():
            print(f"{k}: {v.split(':')[0] if v.startswith('FAIL') else 'PASS'}")

        browser.close()

if __name__ == "__main__":
    run_tests()
