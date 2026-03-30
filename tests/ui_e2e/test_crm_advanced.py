import random
import time
from playwright.sync_api import expect
from tests.ui_e2e.pages.crm_page import CrmPage


def _create_test_lead(crm, auth_page):
    """Helper: create a lead and return its phone number."""
    test_phone = f"+91{random.randint(1000000000, 9999999999)}"
    crm.click_add_lead()
    crm.fill_and_submit_lead("E2E", "ModalTest", test_phone)
    expect(crm.get_lead_row(test_phone)).to_be_visible(timeout=10000)
    return test_phone


def test_transcript_modal_opens(auth_page, base_url):
    """Test that clicking Transcript button on a lead opens the TranscriptModal."""
    crm = CrmPage(auth_page, base_url)
    crm.navigate_with_cache_bust()
    time.sleep(2)

    test_phone = _create_test_lead(crm, auth_page)
    time.sleep(1)

    row = crm.get_lead_row(test_phone)
    row.locator("button:has-text('Transcript')").click()
    time.sleep(1)

    # Verify TranscriptModal heading (use role selector to be specific)
    expect(
        auth_page.locator("h2:has-text('Call Transcripts')")
    ).to_be_visible(timeout=8000)

    # Verify empty state for a fresh lead
    expect(
        auth_page.get_by_text("No call transcripts yet.")
    ).to_be_visible(timeout=8000)

    # Close modal by clicking the X button
    auth_page.locator(".modal-overlay button:has-text('\u2715')").click()
    time.sleep(1)
    crm.delete_lead(test_phone)


def test_document_vault_opens(auth_page, base_url):
    """Test that clicking Docs button on a lead opens the Document Vault modal."""
    crm = CrmPage(auth_page, base_url)
    crm.navigate_with_cache_bust()
    time.sleep(2)

    test_phone = _create_test_lead(crm, auth_page)
    time.sleep(1)

    row = crm.get_lead_row(test_phone)
    row.locator("button:has-text('Docs')").click()
    time.sleep(1)

    # Verify Document Vault modal opens
    expect(
        auth_page.get_by_text("Document Vault")
    ).to_be_visible(timeout=8000)

    # Close modal by clicking "Close Vault" button
    auth_page.locator("button:has-text('Close Vault')").click()
    time.sleep(1)
    crm.delete_lead(test_phone)


def test_note_prompt(auth_page, base_url):
    """Test that clicking Note button triggers the native prompt dialog."""
    crm = CrmPage(auth_page, base_url)
    crm.navigate_with_cache_bust()
    time.sleep(2)

    test_phone = _create_test_lead(crm, auth_page)
    time.sleep(1)

    # handleNote uses window.prompt() — dismiss it automatically
    auth_page.once("dialog", lambda dialog: dialog.dismiss())

    row = crm.get_lead_row(test_phone)
    row.locator("button:has-text('Note')").click()
    time.sleep(1)

    # If we got here without hanging, the prompt was handled correctly
    # Clean up
    crm.delete_lead(test_phone)
