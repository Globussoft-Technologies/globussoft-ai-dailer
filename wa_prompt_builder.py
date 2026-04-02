"""
wa_prompt_builder.py — Builds system prompts for WhatsApp AI conversations.

Unlike the voice prompt_builder, this version:
- Allows WhatsApp formatting (*bold*, _italic_)
- Allows 2-3 sentences per response
- No [HANGUP] command, no voice gender hints, no pronunciation guide
- Supports [HUMAN_TAKEOVER] for escalation
"""

import logging
from database import get_conn

logger = logging.getLogger("uvicorn.error")


def _get_product_info(product_id: int) -> dict:
    """Load product persona, knowledge, and org info for a product."""
    conn = get_conn()
    cursor = conn.cursor()
    cursor.execute('''
        SELECT p.name, p.scraped_info, p.manual_notes, p.agent_persona, o.name as org_name
        FROM products p
        JOIN organizations o ON p.org_id = o.id
        WHERE p.id = %s
    ''', (product_id,))
    row = cursor.fetchone()
    conn.close()
    if not row:
        return {}
    return {
        "product_name": row.get("name", ""),
        "scraped_info": row.get("scraped_info") or "",
        "manual_notes": row.get("manual_notes") or "",
        "agent_persona": row.get("agent_persona") or "",
        "org_name": row.get("org_name", ""),
    }


def _get_org_name(org_id: int) -> str:
    """Fetch organization name by ID."""
    try:
        conn = get_conn()
        cursor = conn.cursor()
        cursor.execute("SELECT name FROM organizations WHERE id = %s", (org_id,))
        row = cursor.fetchone()
        conn.close()
        return row["name"] if row else ""
    except Exception:
        return ""


def build_wa_prompt(
    channel_config: dict,
    contact_name: str,
    product_id: int = None,
    org_id: int = None,
    language: str = "en",
) -> dict:
    """
    Build the WhatsApp AI system prompt.

    Args:
        channel_config: Row from wa_channel_config table.
        contact_name: Customer's display name.
        product_id: Optional product ID for persona/knowledge loading.
        org_id: Organization ID.
        language: Default language hint (en/hi/mr). AI auto-detects from customer messages.

    Returns:
        {"system_instruction": str, "agent_name": str}
    """
    # --- Agent name ---
    agent_name = channel_config.get("agent_name") or "AI Assistant"

    # --- Company name ---
    company_name = ""
    product_info = {}

    if product_id:
        product_info = _get_product_info(product_id)
        company_name = product_info.get("org_name", "")

    if not company_name and org_id:
        company_name = _get_org_name(org_id)

    if not company_name:
        company_name = "our company"

    # --- Customer first name ---
    first_name = contact_name.split()[0] if contact_name and contact_name.strip() else "there"

    # --- Product knowledge block ---
    product_knowledge = ""
    if product_info:
        knowledge_parts = []
        if product_info.get("product_name"):
            knowledge_parts.append(f"Product: {product_info['product_name']}")
        if product_info.get("scraped_info"):
            knowledge_parts.append(product_info["scraped_info"])
        if product_info.get("manual_notes"):
            knowledge_parts.append(f"Admin notes: {product_info['manual_notes']}")
        if knowledge_parts:
            product_knowledge = (
                "\n\n[PRODUCT KNOWLEDGE — Use this info when customer asks about the product]:\n"
                + "\n".join(knowledge_parts)
            )

    # --- Persona block ---
    persona_block = ""
    if product_info.get("agent_persona"):
        # Use the product-specific persona, substituting placeholders
        persona_block = product_info["agent_persona"]
        persona_block = persona_block.replace("{{first_name}}", first_name)
        persona_block = persona_block.replace("{{company}}", company_name)
        persona_block = persona_block.replace("{{agent_name}}", agent_name)
    else:
        persona_block = (
            f"You are {agent_name}, a friendly and helpful assistant from {company_name}.\n"
            f"You are chatting with {first_name}."
        )

    # --- Language instruction ---
    language_hints = {
        "hi": (
            "Default language: Hindi (Devanagari + conversational Hinglish).\n"
            "If the customer writes in Hindi, reply in Hindi. If in English, reply in English.\n"
            "Match the customer's language naturally."
        ),
        "mr": (
            "Default language: Marathi (Devanagari + conversational style).\n"
            "If the customer writes in Marathi, reply in Marathi. If in English, reply in English.\n"
            "Match the customer's language naturally."
        ),
        "en": (
            "Default language: English.\n"
            "If the customer writes in Hindi, reply in Hindi. If in Marathi, reply in Marathi.\n"
            "Always respond in the same language the customer uses."
        ),
    }
    lang_instruction = language_hints.get(language, language_hints["en"])

    # --- Build full system instruction ---
    system_instruction = f"""{persona_block}

## CHANNEL
You are chatting on WhatsApp, not making a phone call. This is a text conversation.

## LANGUAGE
{lang_instruction}

## RESPONSE STYLE
- Keep responses to 2-3 sentences max. Be concise but complete.
- Use the customer's first name: *{first_name}*
- Be conversational and friendly, like texting a colleague.
- You may use WhatsApp formatting: *bold* for emphasis, _italic_ for subtle notes.
- Do NOT use numbered lists, bullet points, or long paragraphs.
- Do NOT send voice notes or images — text only.

## RULE #1 — NEVER HALLUCINATE (MOST CRITICAL)
- NEVER fabricate addresses, phone numbers, office timings, pricing, or any factual info.
- ONLY use information from the PRODUCT KNOWLEDGE section below. Do NOT make up anything else.
- If the customer asks something you don't know, say: "Let me check and get back to you on that."
- If pricing is asked and not in your knowledge, say: "I'll connect you with our team for exact pricing."
- WRONG: "Our office is at 123 Main Street" (FABRICATED!)
- WRONG: "The price starts at 50,000" (FABRICATED if not in product knowledge!)
- RIGHT: "Let me get our team to share the exact details with you."

## RULE #2 — ESCALATION
- If the customer asks to speak to a human, a manager, or says "connect me to someone":
  Output [HUMAN_TAKEOVER] at the end of your message.
  Example: "Sure {first_name}, let me connect you with our team right away. [HUMAN_TAKEOVER]"
- If the customer is angry, frustrated, or the conversation is going in circles after 3+ attempts:
  Offer human help and output [HUMAN_TAKEOVER].

## RULE #3 — STAY ON TOPIC
- You represent {company_name}. Stay focused on the product/service.
- Do not engage in unrelated conversations (politics, personal advice, etc.).
- Politely redirect: "I'm here to help with {company_name}'s services. How can I assist you?"

## RULE #4 — DO NOT ASK FOR INFO YOU ALREADY HAVE
- You already know the customer's name is {first_name}. Do NOT ask for their name again.
- Do NOT ask for their phone number — you're already chatting with them on WhatsApp.
{product_knowledge}
"""

    return {
        "system_instruction": system_instruction.strip(),
        "agent_name": agent_name,
    }
