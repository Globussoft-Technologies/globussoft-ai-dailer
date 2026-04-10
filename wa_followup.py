"""
wa_followup.py -- WhatsApp follow-up messages triggered by call outcomes.

Sends a WhatsApp confirmation when the AI agent books an appointment during a call.
Uses the org's configured WhatsApp channel; skips silently if none configured.
"""

import logging
from database import get_lead_by_id, get_campaign_by_id, get_wa_channel_configs, save_wa_message
from wa_provider import get_wa_provider

logger = logging.getLogger("uvicorn.error")


async def send_appointment_confirmation(lead_id: int, campaign_id: int):
    """Send WhatsApp appointment confirmation to a lead after a successful booking.

    Looks up the lead's org, finds an active WA channel config, and sends a
    simple text message.  Fails silently if no WA channel is configured.
    """
    # --- Gather lead + campaign info ---
    lead = get_lead_by_id(lead_id)
    if not lead:
        logger.warning(f"[WA_FOLLOWUP] Lead {lead_id} not found, skipping")
        return

    phone = lead.get("phone", "")
    if not phone:
        logger.warning(f"[WA_FOLLOWUP] Lead {lead_id} has no phone, skipping")
        return

    org_id = lead.get("org_id")
    if not org_id:
        logger.warning(f"[WA_FOLLOWUP] Lead {lead_id} has no org_id, skipping")
        return

    lead_name = (lead.get("first_name") or "").strip()
    if not lead_name:
        lead_name = "there"

    campaign = get_campaign_by_id(campaign_id) if campaign_id else None
    company_name = (campaign.get("product_name") or "our team") if campaign else "our team"

    # --- Find an active WA channel config for this org ---
    configs = get_wa_channel_configs(org_id)
    active_config = None
    for cfg in configs:
        if cfg.get("is_active", True):
            active_config = cfg
            break

    if not active_config:
        logger.info(f"[WA_FOLLOWUP] No active WA channel for org {org_id}, skipping appointment confirmation")
        return

    # --- Build and send message ---
    message = (
        f"Hi {lead_name},\n\n"
        f"Thank you for your interest in {company_name}! "
        f"Your meeting has been confirmed. Our team will call you at the scheduled time.\n\n"
        f"If you need to reschedule, reply to this message.\n\n"
        f"- {company_name} Team"
    )

    creds = active_config.get("credentials", {})
    provider = get_wa_provider(active_config["provider"], **creds)
    result = await provider.send_text(phone, message)

    if result.success:
        # Save outbound message to WA conversation history
        save_wa_message(
            org_id=org_id,
            channel_config_id=active_config["id"],
            contact_phone=phone,
            contact_name=lead_name,
            direction="outbound",
            message_type="text",
            content=message,
            is_ai_generated=True,
            ai_model="system",
            lead_id=lead_id,
        )
        logger.info(f"[WA_FOLLOWUP] Appointment confirmation sent to {phone} (lead {lead_id})")
    else:
        logger.error(f"[WA_FOLLOWUP] Failed to send to {phone}: {result.error}")
