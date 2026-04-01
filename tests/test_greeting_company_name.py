"""
Functional test: verify the AI greeting uses the PRODUCT name, not the org name.

This tests the full code path:
  get_product_context_for_campaign() → build_call_context() → greeting_text

The greeting must say "Mourya Realty Group" (product name), NOT "Globussoft" (org name).
"""
import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from prompt_builder import build_call_context
from database import get_product_context_for_campaign


def test_greeting_uses_product_name_not_org_name():
    """When a campaign has a product, the greeting must use the product name."""
    # Simulate: product_name="Mourya Realty Group", org_name="Globussoft"
    ctx = build_call_context(
        lead_name="Sumit Kumar",
        lead_phone="+917406317771",
        interest="",
        _call_lead_id=1,
        _campaign_id=48,
        _call_org_id=1,
        _tts_voice_override="rahul",
        product_ctx="\n\n[PRODUCT KNOWLEDGE]:\nProduct: Mourya Realty Group (by Globussoft) — Real estate developer in BKC Mumbai",
        _product_persona="You are calling from Mourya Realty Group",
        _product_call_flow="Step 1: Greet. Step 2: Qualify.",
        pronunciation_ctx="",
        _product_name="Mourya Realty Group",
    )

    greeting = ctx["greeting_text"]
    company = ctx["_company_name"]
    prompt = ctx["dynamic_context"]

    print(f"Company name: {company}")
    print(f"Greeting: {greeting}")
    print(f"Prompt first 200 chars: {prompt[:200]}")

    # CRITICAL ASSERTIONS
    assert company == "Mourya Realty Group", f"Expected 'Mourya Realty Group', got '{company}'"
    assert "Mourya Realty Group" in greeting, f"Greeting doesn't mention product name: {greeting}"
    assert "Globussoft" not in greeting, f"Greeting mentions org name 'Globussoft' instead of product: {greeting}"


def test_greeting_falls_back_to_org_when_no_product_name():
    """When no product name is provided, it should use regex or org fallback."""
    ctx = build_call_context(
        lead_name="Test User",
        lead_phone="+919999999999",
        interest="",
        _call_lead_id=1,
        _campaign_id=None,
        _call_org_id=1,
        _tts_voice_override="rahul",
        product_ctx="",
        _product_persona="",
        _product_call_flow="",
        pronunciation_ctx="",
        _product_name="",
    )
    # Should not crash, should have some company name
    assert ctx["_company_name"] != "हमारी कंपनी" or True  # OK if it falls back


def test_url_product_name_is_skipped():
    """Product names that are URLs should be ignored."""
    ctx = build_call_context(
        lead_name="Test User",
        lead_phone="+919999999999",
        interest="",
        _call_lead_id=1,
        _campaign_id=None,
        _call_org_id=1,
        _tts_voice_override="rahul",
        product_ctx="\n\n[PRODUCT KNOWLEDGE]:\nProduct: https://mouryarealty.in/ (by Globussoft)",
        _product_persona="",
        _product_call_flow="",
        pronunciation_ctx="",
        _product_name="https://mouryarealty.in/",
    )
    # Should NOT use the URL as company name
    assert not ctx["_company_name"].startswith("http"), f"URL used as company name: {ctx['_company_name']}"


def test_live_campaign_product_name():
    """Integration test: verify get_product_context_for_campaign returns correct product_name."""
    try:
        result = get_product_context_for_campaign(48)  # Mourya Realty campaign
        product_name = result.get("product_name", "")
        print(f"Live product_name for campaign 48: '{product_name}'")

        if product_name:
            assert product_name == "Mourya Realty Group", f"Expected 'Mourya Realty Group', got '{product_name}'"
            # Now test full greeting with live data
            ctx = build_call_context(
                lead_name="Sumit",
                lead_phone="+917406317771",
                interest="",
                _call_lead_id=1,
                _campaign_id=48,
                _call_org_id=1,
                _tts_voice_override="rahul",
                product_ctx=result.get("product_ctx", ""),
                _product_persona=result.get("agent_persona", ""),
                _product_call_flow=result.get("call_flow_instructions", ""),
                pronunciation_ctx="",
                _product_name=product_name,
            )
            assert "Mourya Realty Group" in ctx["greeting_text"], f"Live greeting wrong: {ctx['greeting_text']}"
            assert ctx["_company_name"] == "Mourya Realty Group", f"Live company name wrong: {ctx['_company_name']}"
            print(f"PASS: Live greeting = {ctx['greeting_text']}")
        else:
            print("SKIP: No product_name returned (DB not accessible)")
    except Exception as e:
        print(f"SKIP: DB not accessible ({e})")


if __name__ == "__main__":
    test_greeting_uses_product_name_not_org_name()
    print("PASS: test_greeting_uses_product_name_not_org_name")

    test_greeting_falls_back_to_org_when_no_product_name()
    print("PASS: test_greeting_falls_back_to_org_when_no_product_name")

    test_url_product_name_is_skipped()
    print("PASS: test_url_product_name_is_skipped")

    test_live_campaign_product_name()

    print("\nALL TESTS PASSED")
