import os
import time
import random
from playwright.sync_api import sync_playwright, expect

def random_digits(n):
    return "".join(str(random.randint(0,9)) for _ in range(n))

def run_tests():
    print("Starting Deep E2E Features Test on https://test.callified.ai/")
    results = {}
    
    with sync_playwright() as p:
        # We need mock permissions for mic and location
        browser = p.chromium.launch(headless=True, args=[
            "--use-fake-ui-for-media-stream",
            "--use-fake-device-for-media-stream",
        ])
        context = browser.new_context(viewport={'width': 1280, 'height': 800})
        context.grant_permissions(['geolocation', 'microphone'])
        context.set_geolocation({"latitude": 37.7749, "longitude": -122.4194})
        
        page = context.new_page()

        # Handle all dialogs automatically
        page.on("dialog", lambda dialog: dialog.accept("Automated Testing Note"))

        # 1. Navigation & Auth Negative + Positive
        print("\n1. Testing Auth & Sign Up Error Paths...")
        page.goto("https://test.callified.ai/")
        try:
            page.wait_for_selector('input[type="email"]', timeout=5000)
            
            # NEGATIVE TEST: Wait for 'Login' panel / attempt bad credentials
            page.fill('input[type="email"]', 'bad_user123@globussoft.com')
            page.fill('input[type="password"]', 'wrongpass1')
            page.click('button[type="submit"]')
            # Expect button to still be visible (login failed)
            expect(page.locator('button[type="submit"]')).to_be_visible(timeout=5000)
            results['Auth - Negative Log In'] = 'PASS'
            print(" -> PASS: Tested invalid login boundary.")
            
            # POSITIVE TEST: Sign Up
            page.click("button:has-text('Sign Up')")
            time.sleep(0.5)
            test_email = f"deep_e2e_{int(time.time())}@globussoft.com"
            page.fill('input[placeholder="e.g. Globussoft"]', "Deep E2E Org")
            page.fill('input[placeholder="e.g. Sumit Kumar"]', "Deep Tester")
            page.fill('input[type="email"]', test_email)
            page.fill('input[type="password"]', "TestPass!123")
            page.click('button[type="submit"]')
            
            page.wait_for_selector('.header', timeout=17000)
            results['Auth - Sign Up'] = 'PASS'
            print(f" -> PASS: Successfully signed up as {test_email}.")
        except Exception as e:
            results['Auth - Sign Up'] = f'FAIL: {e}'
            print(f" -> FAIL: {e}")

        time.sleep(2)

        # 2. Ops Tab
        print("\n2. Testing Ops Tab...")
        try:
            page.click("button:has-text('📋 Ops & Tasks')")
            time.sleep(1)
            # Just verify that the Ops tab loads correctly
            expect(page.locator("h2:has-text('Internal Cross-Department Tasks')")).to_be_visible(timeout=5000)
            results['Ops Tab - Validation'] = 'PASS'
            print(" -> PASS: Validated Ops Tab loads.")
        except Exception as e:
            results['Ops Tab - Validation'] = f'FAIL: {e}'
            print(f" -> FAIL: Ops validation {e}")

        # 3. CRM Tab - Create Lead, Docs, Email, Search Filtering
        print("\n3. Testing deep CRM Features...")
        phone_num1 = f"+9198{random_digits(8)}"
        phone_num2 = f"+9199{random_digits(8)}"
        
        try:
            page.click("button:has-text('📊 CRM')")
            time.sleep(1)
            
            # Add Lead 1
            page.click("button:has-text('Add Lead')")
            page.locator("input[placeholder='e.g. John']").fill("Deep1")
            page.locator("input[placeholder='e.g. Doe']").fill("Lead Alpha")
            page.locator("input[type='tel']").fill(phone_num1)
            # Source field may be hidden/omitted
            page.click("button:has-text('Save Lead')")
            expect(page.locator(f"tr:has-text('{phone_num1}')").first).to_be_visible(timeout=10000)
            
            # Add Lead 2
            page.click("button:has-text('Add Lead')")
            page.locator("input[placeholder='e.g. John']").fill("Deep2")
            page.locator("input[placeholder='e.g. Doe']").fill("Lead Beta")
            page.locator("input[type='tel']").fill(phone_num2)
            page.click("button:has-text('Save Lead')")
            expect(page.locator(f"tr:has-text('{phone_num2}')").first).to_be_visible(timeout=10000)

            # Search Features
            page.fill("input[placeholder='🔍 Search Leads by Name or Phone...']", "Deep2")
            time.sleep(1.5)
            # Expect only lead 2 to be visible
            expect(page.locator(f"tr:has-text('{phone_num2}')").first).to_be_visible(timeout=5000)
            expect(page.locator(f"tr:has-text('{phone_num1}')").first).to_be_hidden(timeout=5000)
            results['CRM - Fuzzy Search'] = 'PASS'
            print(" -> PASS: Fuzzy search filtered leads successfully.")
            
            # Reset Search
            page.fill("input[placeholder='🔍 Search Leads by Name or Phone...']", "")
            time.sleep(1.5)

            # AI Email Generator
            try:
                page.locator(f"tr:has-text('{phone_num1}')").first.locator("button:has-text('📧 AI Email')").click()
                expect(page.locator("h2:has-text('✨ GenAI Drafted Email')")).to_be_visible(timeout=30000)
                try:
                    page.click("button:has-text('Close')")
                except:
                    pass
                results['CRM - AI Email'] = 'PASS'
                print(" -> PASS: AI Email Generation successful.")
            except Exception as e:
                results['CRM - AI Email'] = f'FAIL: {e}'
                print(f" -> FAIL: AI Email Generator {e}")

            # File Uploads / Document Vault
            page.locator(f"tr:has-text('{phone_num1}')").first.locator("button:has-text('📁 Docs')").click()
            # Wait for modal overlay
            time.sleep(1)
            
            # Since there's no actual input type=file, we just fill the URL and Name (Custom document vault component handles URLs for attachments)
            page.fill('input[placeholder="Document name (e.g. Contract)"]', "Test PDF")
            page.fill('input[placeholder="https://..."]', "https://example.com/test.pdf")
            page.click("button:has-text('Attach Link')")
            
            # Wait for list to update
            expect(page.locator(f"a:has-text('Test PDF')").first).to_be_visible(timeout=5000)
            page.click("button:has-text('Close Vault')")
            results['CRM - Document Vault'] = 'PASS'
            print(" -> PASS: Simulated document attachment in vault.")
            
        except Exception as e:
            page.screenshot(path="crm_failure_screenshot.png")
            results['CRM - Missing CRM Step'] = f'FAIL: {e}'
            print(f" -> FAIL: Deep CRM Error {e}")

        # 4. Sandbox AI Voice & Active Mic
        print("\n4. Testing AI Sandbox Websocket Mocking...")
        try:
            page.click("button:has-text('🎯 AI Sandbox')")
            time.sleep(1)
            # Start Simulation
            page.click("button:has-text('🎙️ Start Simulation')")
            
            # Wait for mic active indicator
            expect(page.locator("span:has-text('Active 🟢')").first).to_be_visible(timeout=10000)
            # Stop simulation
            page.click("button:has-text('⏹️ Stop')")
            expect(page.locator("span:has-text('Off 🔴')").first).to_be_visible(timeout=5000)
            
            results['Sandbox - Mic + WS Simulation'] = 'PASS'
            print(" -> PASS: Simulated Browser Microphone Media Stream and WS Lifecycle.")
            
        except Exception as e:
            results['Sandbox - WS Simulation'] = f'FAIL: {e}'
            print(f" -> FAIL: Sandbox WS Error {e}")


        print("\n--- DEEP TEST RESULTS SUMMARY ---")
        for k, v in results.items():
            print(f"{k}: {v.split(':')[0] if v.startswith('FAIL') else 'PASS'}")

        browser.close()

if __name__ == "__main__":
    run_tests()
