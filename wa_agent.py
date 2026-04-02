"""
wa_agent.py — WhatsApp AI Agent for Callified.

Handles incoming WhatsApp messages:
1. Checks auto-reply + pause state
2. Deduplicates by provider message ID
3. Auto-links to leads by phone number
4. Loads conversation history
5. Builds prompt via wa_prompt_builder
6. Optionally adds RAG context
7. Calls LLM for response
8. Detects [HUMAN_TAKEOVER] and sets pause flag

The caller (webhook route) handles saving messages to DB and sending via provider.
"""

import logging
from typing import Optional

import llm_provider
from database import get_conn
import redis_store
import wa_prompt_builder

logger = logging.getLogger("uvicorn.error")

# Redis key TTLs
WA_PAUSE_TTL = 86400  # 24 hours — human takeover pause
WA_DEDUP_TTL = 300    # 5 minutes — message deduplication window


# ─── Helper: Redis get/set via redis_store internals ──────────────────────────

def _redis_client():
    """Get the redis client from redis_store."""
    return redis_store._get_client()


def _is_paused(org_id: int, phone: str) -> bool:
    """Check if AI is paused for this contact (human takeover active)."""
    r = _redis_client()
    if not r:
        return False
    return r.exists(f"wa:paused:{org_id}:{phone}") > 0


def _set_paused(org_id: int, phone: str):
    """Pause AI replies for this contact (human takeover)."""
    r = _redis_client()
    if r:
        r.setex(f"wa:paused:{org_id}:{phone}", WA_PAUSE_TTL, "1")
        logger.info(f"[WA] AI paused for {phone} in org {org_id} (human takeover)")


def _is_duplicate(provider_message_id: str) -> bool:
    """Check if we've already processed this provider message ID."""
    r = _redis_client()
    if not r:
        # Fallback: check DB
        try:
            conn = get_conn()
            cursor = conn.cursor()
            cursor.execute(
                "SELECT id FROM wa_conversations WHERE provider_message_id = %s LIMIT 1",
                (provider_message_id,)
            )
            row = cursor.fetchone()
            conn.close()
            return row is not None
        except Exception:
            return False
    # Try to set a dedup key; returns False if already exists
    key = f"wa:dedup:{provider_message_id}"
    was_set = r.set(key, "1", ex=WA_DEDUP_TTL, nx=True)
    return not was_set  # if set failed, it's a duplicate


def _find_lead_by_phone(org_id: int, phone: str) -> Optional[dict]:
    """Try to find an existing lead by phone number."""
    try:
        conn = get_conn()
        cursor = conn.cursor()
        # Normalize: try exact match first, then last 10 digits
        cursor.execute(
            "SELECT id, first_name, last_name FROM leads WHERE org_id = %s AND phone = %s LIMIT 1",
            (org_id, phone)
        )
        row = cursor.fetchone()
        if not row and len(phone) > 10:
            # Try matching last 10 digits
            phone_suffix = phone[-10:]
            cursor.execute(
                "SELECT id, first_name, last_name FROM leads WHERE org_id = %s AND phone LIKE %s LIMIT 1",
                (org_id, f"%{phone_suffix}")
            )
            row = cursor.fetchone()
        conn.close()
        return row
    except Exception as e:
        logger.warning(f"[WA] Lead lookup failed: {e}")
        return None


def _load_chat_history(org_id: int, phone: str, limit: int = 20) -> list:
    """Load recent messages from wa_conversations for LLM context.

    Returns list in Gemini chat format:
        [{"role": "user"|"model", "parts": [{"text": "..."}]}]
    """
    try:
        conn = get_conn()
        cursor = conn.cursor()
        cursor.execute('''
            SELECT direction, message_text
            FROM wa_conversations
            WHERE org_id = %s AND phone = %s AND message_text IS NOT NULL
            ORDER BY created_at DESC
            LIMIT %s
        ''', (org_id, phone, limit))
        rows = cursor.fetchall()
        conn.close()

        # Rows are newest-first, reverse for chronological order
        rows.reverse()

        history = []
        for row in rows:
            direction = row.get("direction", "inbound")
            text = row.get("message_text", "")
            if not text:
                continue
            role = "user" if direction == "inbound" else "model"
            history.append({"role": role, "parts": [{"text": text}]})

        return history
    except Exception as e:
        logger.warning(f"[WA] Failed to load chat history: {e}")
        return []


# ─── Main Entry Point ────────────────────────────────────────────────────────

async def handle_incoming_message(
    org_id: int,
    channel_config: dict,
    sender_phone: str,
    sender_name: str,
    message_text: str,
    provider_message_id: str,
) -> Optional[str]:
    """
    Process an incoming WhatsApp message and generate an AI response.

    Args:
        org_id: Organization ID.
        channel_config: Row from wa_channel_config table.
        sender_phone: Customer's phone number (with country code).
        sender_name: Customer's WhatsApp display name.
        message_text: The incoming message text.
        provider_message_id: Unique message ID from the provider (for dedup).

    Returns:
        AI response text (str) if a reply should be sent, or None if no reply needed.
        The caller is responsible for saving messages to DB and sending via provider.
    """
    # 1. Check if auto-reply is enabled
    if not channel_config.get("auto_reply_enabled", False):
        logger.info(f"[WA] Auto-reply disabled for org {org_id}, skipping")
        return None

    # 2. Check if AI is paused for this contact (human takeover)
    if _is_paused(org_id, sender_phone):
        logger.info(f"[WA] AI paused for {sender_phone} (human takeover active), skipping")
        return None

    # 3. Deduplicate by provider message ID
    if _is_duplicate(provider_message_id):
        logger.info(f"[WA] Duplicate message {provider_message_id}, skipping")
        return None

    # 4. Auto-link to lead by phone number
    lead = _find_lead_by_phone(org_id, sender_phone)
    contact_name = sender_name
    if lead:
        # Use lead name if available (more accurate than WhatsApp profile name)
        lead_name = lead.get("first_name", "")
        if lead.get("last_name"):
            lead_name += f" {lead['last_name']}"
        if lead_name.strip():
            contact_name = lead_name.strip()
        logger.info(f"[WA] Linked to lead #{lead['id']}: {contact_name}")

    # 5. Load conversation history (last 20 messages)
    chat_history = _load_chat_history(org_id, sender_phone, limit=20)

    # 6. Add current message to history for LLM
    chat_history.append({"role": "user", "parts": [{"text": message_text}]})

    # 7. Build system prompt
    product_id = channel_config.get("product_id")
    language = channel_config.get("default_language", "en")

    prompt_result = wa_prompt_builder.build_wa_prompt(
        channel_config=channel_config,
        contact_name=contact_name,
        product_id=product_id,
        org_id=org_id,
        language=language,
    )
    system_instruction = prompt_result["system_instruction"]

    # 8. Optionally add RAG context
    try:
        import rag
        rag_context = rag.retrieve_context(message_text, org_id, top_k=2)
        if rag_context:
            system_instruction += (
                f"\n\n[RAG CONTEXT — Additional knowledge retrieved for this query]:\n"
                f"{rag_context}"
            )
    except Exception as e:
        logger.debug(f"[WA] RAG not available or failed: {e}")

    # 9. Call LLM
    try:
        response_text = await llm_provider.generate_response(
            chat_history=chat_history,
            system_instruction=system_instruction,
            max_tokens=500,
        )
    except Exception as e:
        logger.error(f"[WA] LLM generation failed: {e}")
        return None

    if not response_text or not response_text.strip():
        logger.warning("[WA] LLM returned empty response")
        return None

    # 10. Check for [HUMAN_TAKEOVER]
    if "[HUMAN_TAKEOVER]" in response_text:
        _set_paused(org_id, sender_phone)
        # Strip the tag from the customer-facing message
        response_text = response_text.replace("[HUMAN_TAKEOVER]", "").strip()
        logger.info(f"[WA] Human takeover triggered for {sender_phone}")

    # 11. Return AI response (caller handles saving + sending)
    logger.info(f"[WA] AI response for {sender_phone}: {response_text[:100]}...")
    return response_text
