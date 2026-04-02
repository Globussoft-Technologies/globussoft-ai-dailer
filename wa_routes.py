"""
wa_routes.py -- WhatsApp webhook + API routes for Callified AI Dialer.

Webhook endpoints (NO auth -- called by providers):
  POST /wa/webhook/gupshup
  POST /wa/webhook/wati
  POST /wa/webhook/aisensei
  POST /wa/webhook/interakt
  POST /wa/webhook/meta
  GET  /wa/webhook/meta  (Meta verification challenge)

API endpoints (auth required):
  GET    /api/wa/conversations
  GET    /api/wa/conversations/{contact_phone}/messages
  POST   /api/wa/send
  GET    /api/wa/config
  POST   /api/wa/config
  PUT    /api/wa/config/{config_id}
  DELETE /api/wa/config/{config_id}
  POST   /api/wa/toggle-ai/{contact_phone}
"""

import logging
import json
from typing import Optional

from fastapi import APIRouter, Depends, HTTPException, Request, BackgroundTasks, Query
from fastapi.responses import JSONResponse, PlainTextResponse
from pydantic import BaseModel

from auth import get_current_user
from database import (
    get_wa_channel_configs,
    get_wa_channel_configs_by_provider,
    get_wa_channel_config_by_id,
    get_wa_channel_config_by_phone,
    create_wa_channel_config,
    update_wa_channel_config,
    delete_wa_channel_config,
    save_wa_message,
    get_wa_conversations_list,
    get_wa_chat_history,
    get_wa_message_by_provider_id,
    link_wa_conversation_to_lead,
)
from wa_provider import get_wa_provider, WaIncomingMessage
import wa_agent

logger = logging.getLogger("uvicorn.error")

wa_router = APIRouter()


# ═══════════════════════════════════════════════════════════════════════════════
# Pydantic models
# ═══════════════════════════════════════════════════════════════════════════════

class WaSendBody(BaseModel):
    contact_phone: str
    message: str
    channel_config_id: int


class WaConfigCreate(BaseModel):
    provider: str
    phone_number: str
    credentials: dict
    default_product_id: Optional[int] = None


class WaConfigUpdate(BaseModel):
    provider: Optional[str] = None
    phone_number: Optional[str] = None
    credentials: Optional[dict] = None
    default_product_id: Optional[int] = None
    is_active: Optional[bool] = None
    auto_reply_enabled: Optional[bool] = None
    welcome_template: Optional[str] = None
    business_hours_json: Optional[dict] = None


class WaToggleAI(BaseModel):
    paused: bool


# ═══════════════════════════════════════════════════════════════════════════════
# Shared webhook dispatcher
# ═══════════════════════════════════════════════════════════════════════════════

def _find_channel_config(provider_name: str, parsed_msg: WaIncomingMessage):
    """Find the channel config that matches this provider and the destination phone."""
    configs = get_wa_channel_configs_by_provider(provider_name)
    if not configs:
        return None
    if len(configs) == 1:
        return configs[0]
    # If multiple configs for the same provider, try matching by phone
    # (the phone the message was sent TO is in the config's phone_number)
    # For most providers, we can only match by provider; return first active.
    return configs[0]


async def _process_and_reply(
    org_id: int,
    channel_config: dict,
    sender_phone: str,
    sender_name: str,
    message_text: str,
    provider_message_id: str,
):
    """Background task: call AI agent, save outbound message, send via provider."""
    try:
        # 1. Call wa_agent for AI response
        response_text = await wa_agent.handle_incoming_message(
            org_id=org_id,
            channel_config=channel_config,
            sender_phone=sender_phone,
            sender_name=sender_name or "",
            message_text=message_text or "",
            provider_message_id=provider_message_id or "",
        )

        if not response_text:
            logger.info(f"[WA] No AI response for {sender_phone}, skipping send")
            return

        # 2. Save outbound message to DB
        config_id = channel_config["id"]
        # Auto-link lead
        lead = wa_agent._find_lead_by_phone(org_id, sender_phone)
        lead_id = lead["id"] if lead else None

        save_wa_message(
            org_id=org_id,
            channel_config_id=config_id,
            contact_phone=sender_phone,
            contact_name=sender_name or "",
            direction="outbound",
            message_type="text",
            content=response_text,
            provider_message_id=None,
            is_ai_generated=True,
            ai_model="gemini",
            lead_id=lead_id,
        )

        # 3. Send via provider
        creds = channel_config.get("credentials", {})
        provider = get_wa_provider(channel_config["provider"], **creds)
        result = await provider.send_text(sender_phone, response_text)

        if result.success:
            logger.info(f"[WA] Sent AI reply to {sender_phone} via {channel_config['provider']}")
        else:
            logger.error(f"[WA] Failed to send to {sender_phone}: {result.error}")

        # 4. Mark inbound as read
        try:
            await provider.mark_as_read(provider_message_id)
        except Exception as e:
            logger.debug(f"[WA] mark_as_read failed (non-critical): {e}")

    except Exception as e:
        logger.error(f"[WA] _process_and_reply error for {sender_phone}: {e}", exc_info=True)


async def _dispatch_incoming(provider_name: str, request: Request, background_tasks: BackgroundTasks):
    """Shared logic for all provider webhooks."""
    body = await request.body()
    headers = dict(request.headers)

    # Parse body as JSON (or form data for some providers)
    try:
        payload = json.loads(body) if body else {}
    except (json.JSONDecodeError, UnicodeDecodeError):
        # Some providers (e.g. Gupshup) send form-encoded data
        try:
            form = await request.form()
            payload = dict(form)
            # Gupshup sends a "payload" field as JSON string
            if "payload" in payload and isinstance(payload["payload"], str):
                payload = json.loads(payload["payload"])
        except Exception:
            logger.warning(f"[WA] Could not parse webhook body for {provider_name}")
            return JSONResponse({"status": "error", "detail": "unparseable body"}, status_code=400)

    logger.info(f"[WA] Webhook received from {provider_name}: {json.dumps(payload)[:500]}")

    # Find matching channel config
    config = _find_channel_config(provider_name, None)
    if not config:
        logger.warning(f"[WA] No active channel config found for provider={provider_name}")
        return JSONResponse({"status": "ok", "detail": "no config"})

    org_id = config["org_id"]
    config_id = config["id"]

    # Instantiate provider and validate webhook
    creds = config.get("credentials", {})
    provider = get_wa_provider(provider_name, **creds)

    if not provider.validate_webhook(headers, body):
        logger.warning(f"[WA] Webhook validation failed for {provider_name}")
        return JSONResponse({"status": "error", "detail": "validation failed"}, status_code=403)

    # Parse message
    parsed = provider.parse_incoming_message(payload)
    if not parsed:
        logger.info(f"[WA] No parseable message from {provider_name} (status update or empty)")
        return JSONResponse({"status": "ok", "detail": "no message"})

    if not parsed.text and parsed.message_type == "text":
        logger.info(f"[WA] Empty text message from {parsed.sender_phone}, skipping")
        return JSONResponse({"status": "ok"})

    # Check for duplicate
    if parsed.provider_message_id:
        existing = get_wa_message_by_provider_id(parsed.provider_message_id)
        if existing:
            logger.info(f"[WA] Duplicate message {parsed.provider_message_id}, skipping")
            return JSONResponse({"status": "ok", "detail": "duplicate"})

    # Auto-link lead by phone
    lead = wa_agent._find_lead_by_phone(org_id, parsed.sender_phone)
    lead_id = lead["id"] if lead else None

    # Save inbound message to DB
    save_wa_message(
        org_id=org_id,
        channel_config_id=config_id,
        contact_phone=parsed.sender_phone,
        contact_name=parsed.sender_name or "",
        direction="inbound",
        message_type=parsed.message_type,
        content=parsed.text or "",
        media_url=parsed.media_url,
        provider_message_id=parsed.provider_message_id,
        is_ai_generated=False,
        lead_id=lead_id,
    )

    if lead_id:
        link_wa_conversation_to_lead(org_id, parsed.sender_phone, lead_id)

    logger.info(f"[WA] Saved inbound from {parsed.sender_phone} via {provider_name}")

    # Background task: generate AI response and reply
    if parsed.text:
        background_tasks.add_task(
            _process_and_reply,
            org_id=org_id,
            channel_config=config,
            sender_phone=parsed.sender_phone,
            sender_name=parsed.sender_name or "",
            message_text=parsed.text,
            provider_message_id=parsed.provider_message_id or "",
        )

    return JSONResponse({"status": "ok"})


# ═══════════════════════════════════════════════════════════════════════════════
# Webhook endpoints (NO auth)
# ═══════════════════════════════════════════════════════════════════════════════

@wa_router.post("/wa/webhook/gupshup")
async def webhook_gupshup(request: Request, background_tasks: BackgroundTasks):
    logger.info("[WA] Gupshup webhook hit")
    return await _dispatch_incoming("gupshup", request, background_tasks)


@wa_router.post("/wa/webhook/wati")
async def webhook_wati(request: Request, background_tasks: BackgroundTasks):
    logger.info("[WA] Wati webhook hit")
    return await _dispatch_incoming("wati", request, background_tasks)


@wa_router.post("/wa/webhook/aisensei")
async def webhook_aisensei(request: Request, background_tasks: BackgroundTasks):
    logger.info("[WA] AiSensei webhook hit")
    return await _dispatch_incoming("aisensei", request, background_tasks)


@wa_router.post("/wa/webhook/interakt")
async def webhook_interakt(request: Request, background_tasks: BackgroundTasks):
    logger.info("[WA] Interakt webhook hit")
    return await _dispatch_incoming("interakt", request, background_tasks)


@wa_router.post("/wa/webhook/meta")
async def webhook_meta_post(request: Request, background_tasks: BackgroundTasks):
    logger.info("[WA] Meta webhook hit (POST)")
    return await _dispatch_incoming("meta", request, background_tasks)


@wa_router.get("/wa/webhook/meta")
async def webhook_meta_verify(request: Request):
    """Meta webhook verification challenge (GET request)."""
    mode = request.query_params.get("hub.mode")
    token = request.query_params.get("hub.verify_token")
    challenge = request.query_params.get("hub.challenge")

    logger.info(f"[WA] Meta verification: mode={mode}, token={token}")

    if mode == "subscribe":
        # Look up the verify token from any active Meta config
        configs = get_wa_channel_configs_by_provider("meta")
        for cfg in configs:
            creds = cfg.get("credentials", {})
            expected_token = creds.get("verify_token", "")
            if expected_token and token == expected_token:
                logger.info("[WA] Meta webhook verified successfully")
                return PlainTextResponse(challenge or "")
        logger.warning("[WA] Meta verification token mismatch")
        raise HTTPException(status_code=403, detail="Verification failed")

    raise HTTPException(status_code=400, detail="Invalid verification request")


# ═══════════════════════════════════════════════════════════════════════════════
# API endpoints (auth required)
# ═══════════════════════════════════════════════════════════════════════════════

@wa_router.get("/api/wa/conversations")
async def list_conversations(current_user: dict = Depends(get_current_user)):
    """List all conversations grouped by contact."""
    org_id = current_user.get("org_id")
    if not org_id:
        raise HTTPException(status_code=400, detail="No org_id on user")
    conversations = get_wa_conversations_list(org_id)
    logger.info(f"[WA] Listed {len(conversations)} conversations for org {org_id}")
    return {"conversations": conversations}


@wa_router.get("/api/wa/conversations/{contact_phone}/messages")
async def get_conversation_messages(
    contact_phone: str,
    limit: int = Query(50, ge=1, le=200),
    offset: int = Query(0, ge=0),
    current_user: dict = Depends(get_current_user),
):
    """Get chat history for a specific contact."""
    org_id = current_user.get("org_id")
    if not org_id:
        raise HTTPException(status_code=400, detail="No org_id on user")
    messages = get_wa_chat_history(org_id, contact_phone, limit=limit, offset=offset)
    logger.info(f"[WA] Fetched {len(messages)} messages for {contact_phone} in org {org_id}")
    return {"messages": messages, "contact_phone": contact_phone}


@wa_router.post("/api/wa/send")
async def send_message(body: WaSendBody, current_user: dict = Depends(get_current_user)):
    """Manual send from human agent."""
    org_id = current_user.get("org_id")
    if not org_id:
        raise HTTPException(status_code=400, detail="No org_id on user")

    config = get_wa_channel_config_by_id(body.channel_config_id)
    if not config or config.get("org_id") != org_id:
        raise HTTPException(status_code=404, detail="Channel config not found")

    # Save outbound message
    lead = wa_agent._find_lead_by_phone(org_id, body.contact_phone)
    lead_id = lead["id"] if lead else None

    msg_id = save_wa_message(
        org_id=org_id,
        channel_config_id=config["id"],
        contact_phone=body.contact_phone,
        contact_name="",
        direction="outbound",
        message_type="text",
        content=body.message,
        is_ai_generated=False,
        lead_id=lead_id,
    )

    # Send via provider
    creds = config.get("credentials", {})
    provider = get_wa_provider(config["provider"], **creds)
    result = await provider.send_text(body.contact_phone, body.message)

    if result.success:
        logger.info(f"[WA] Manual message sent to {body.contact_phone} by user {current_user.get('email')}")
        return {"status": "sent", "message_id": msg_id, "provider_message_id": result.provider_message_id}
    else:
        logger.error(f"[WA] Manual send failed to {body.contact_phone}: {result.error}")
        raise HTTPException(status_code=502, detail=f"Provider send failed: {result.error}")


# ─── Config CRUD ─────────────────────────────────────────────────────────────

@wa_router.get("/api/wa/config")
async def get_configs(current_user: dict = Depends(get_current_user)):
    """Get org's WhatsApp channel configs."""
    org_id = current_user.get("org_id")
    if not org_id:
        raise HTTPException(status_code=400, detail="No org_id on user")
    configs = get_wa_channel_configs(org_id)
    logger.info(f"[WA] Listed {len(configs)} configs for org {org_id}")
    return {"configs": configs}


@wa_router.post("/api/wa/config")
async def create_config(body: WaConfigCreate, current_user: dict = Depends(get_current_user)):
    """Create a new WhatsApp channel config."""
    org_id = current_user.get("org_id")
    if not org_id:
        raise HTTPException(status_code=400, detail="No org_id on user")

    if body.provider not in ("gupshup", "wati", "aisensei", "interakt", "meta"):
        raise HTTPException(status_code=400, detail=f"Unknown provider: {body.provider}")

    config_id = create_wa_channel_config(
        org_id=org_id,
        provider=body.provider,
        phone_number=body.phone_number,
        credentials=body.credentials,
        default_product_id=body.default_product_id,
    )
    logger.info(f"[WA] Created config #{config_id} ({body.provider}) for org {org_id}")
    return {"config_id": config_id, "status": "created"}


@wa_router.put("/api/wa/config/{config_id}")
async def update_config(config_id: int, body: WaConfigUpdate, current_user: dict = Depends(get_current_user)):
    """Update an existing WhatsApp channel config."""
    org_id = current_user.get("org_id")
    if not org_id:
        raise HTTPException(status_code=400, detail="No org_id on user")

    existing = get_wa_channel_config_by_id(config_id)
    if not existing or existing.get("org_id") != org_id:
        raise HTTPException(status_code=404, detail="Config not found")

    fields = body.dict(exclude_none=True)
    if not fields:
        raise HTTPException(status_code=400, detail="No fields to update")

    update_wa_channel_config(config_id, **fields)
    logger.info(f"[WA] Updated config #{config_id} for org {org_id}: {list(fields.keys())}")
    return {"status": "updated", "config_id": config_id}


@wa_router.delete("/api/wa/config/{config_id}")
async def delete_config(config_id: int, current_user: dict = Depends(get_current_user)):
    """Delete a WhatsApp channel config."""
    org_id = current_user.get("org_id")
    if not org_id:
        raise HTTPException(status_code=400, detail="No org_id on user")

    existing = get_wa_channel_config_by_id(config_id)
    if not existing or existing.get("org_id") != org_id:
        raise HTTPException(status_code=404, detail="Config not found")

    deleted = delete_wa_channel_config(config_id)
    if deleted:
        logger.info(f"[WA] Deleted config #{config_id} for org {org_id}")
        return {"status": "deleted"}
    raise HTTPException(status_code=404, detail="Config not found")


# ─── Toggle AI for a contact ─────────────────────────────────────────────────

@wa_router.post("/api/wa/toggle-ai/{contact_phone}")
async def toggle_ai(contact_phone: str, body: WaToggleAI, current_user: dict = Depends(get_current_user)):
    """Pause or resume AI auto-replies for a specific contact."""
    org_id = current_user.get("org_id")
    if not org_id:
        raise HTTPException(status_code=400, detail="No org_id on user")

    if body.paused:
        wa_agent._set_paused(org_id, contact_phone)
        logger.info(f"[WA] AI paused for {contact_phone} in org {org_id}")
        return {"status": "paused", "contact_phone": contact_phone}
    else:
        # Resume: delete the pause key from Redis
        r = wa_agent._redis_client()
        if r:
            r.delete(f"wa:paused:{org_id}:{contact_phone}")
        logger.info(f"[WA] AI resumed for {contact_phone} in org {org_id}")
        return {"status": "resumed", "contact_phone": contact_phone}
