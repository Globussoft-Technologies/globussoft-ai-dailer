"""
billing_routes.py — API routes for Callified billing.
"""
from fastapi import APIRouter, Depends, Request, HTTPException
from pydantic import BaseModel
from typing import Optional
from auth import get_current_user
from billing import (
    get_all_plans, get_plan,
    get_org_subscription, create_subscription, cancel_subscription,
    get_usage_summary, get_payment_history,
    create_razorpay_order, verify_razorpay_payment, handle_razorpay_webhook,
)
from email_service import send_payment_receipt
import logging

logger = logging.getLogger("uvicorn.error")

billing_router = APIRouter()


# ─── Pydantic Models ─────────────────────────────────────────────────────────

class SubscriptionCreate(BaseModel):
    plan_id: int

class PaymentVerify(BaseModel):
    razorpay_order_id: str
    razorpay_payment_id: str
    razorpay_signature: str
    plan_id: int

class SubscriptionCancel(BaseModel):
    reason: str = ""


# ─── Plans (public) ──────────────────────────────────────────────────────────

@billing_router.get("/api/billing/plans")
def api_get_plans():
    plans = get_all_plans()
    for p in plans:
        if p.get('features') and isinstance(p['features'], str):
            import json
            p['features'] = json.loads(p['features'])
    return plans


# ─── Subscription ────────────────────────────────────────────────────────────

@billing_router.get("/api/billing/subscription")
def api_get_subscription(current_user: dict = Depends(get_current_user)):
    org_id = current_user.get("org_id")
    sub = get_org_subscription(org_id)
    if sub and sub.get('features') and isinstance(sub['features'], str):
        import json
        sub['features'] = json.loads(sub['features'])
    return sub or {"status": "none"}


@billing_router.post("/api/billing/subscribe")
def api_create_subscription(data: SubscriptionCreate, current_user: dict = Depends(get_current_user)):
    org_id = current_user.get("org_id")
    existing = get_org_subscription(org_id)
    if existing and existing['status'] in ('active', 'trialing'):
        raise HTTPException(400, "Active subscription already exists. Cancel first or upgrade.")
    sub_id = create_subscription(org_id, data.plan_id)
    return {"ok": True, "subscription_id": sub_id}


@billing_router.post("/api/billing/cancel")
def api_cancel_subscription(data: SubscriptionCancel, current_user: dict = Depends(get_current_user)):
    org_id = current_user.get("org_id")
    sub = get_org_subscription(org_id)
    if not sub:
        raise HTTPException(404, "No active subscription")
    cancel_subscription(sub['id'], data.reason)
    return {"ok": True}


# ─── Usage ───────────────────────────────────────────────────────────────────

@billing_router.get("/api/billing/usage")
def api_get_usage(current_user: dict = Depends(get_current_user)):
    org_id = current_user.get("org_id")
    return get_usage_summary(org_id)


# ─── Payments (Razorpay) ─────────────────────────────────────────────────────

@billing_router.post("/api/billing/create-order")
def api_create_order(data: SubscriptionCreate, current_user: dict = Depends(get_current_user)):
    org_id = current_user.get("org_id")
    try:
        order = create_razorpay_order(org_id, data.plan_id)
        return order
    except Exception as e:
        logger.error(f"[BILLING] Create order failed: {e}")
        raise HTTPException(500, f"Failed to create order: {str(e)}")


@billing_router.post("/api/billing/verify-payment")
def api_verify_payment(data: PaymentVerify, current_user: dict = Depends(get_current_user)):
    org_id = current_user.get("org_id")
    try:
        result = verify_razorpay_payment(
            org_id, data.razorpay_order_id, data.razorpay_payment_id,
            data.razorpay_signature, data.plan_id
        )
        # Send payment receipt email
        if result.get("status") == "success":
            try:
                plan = get_plan(data.plan_id)
                user_email = current_user.get("email", "")
                user_name = current_user.get("full_name", "Customer")
                plan_name = plan.get("name", "Plan") if plan else "Plan"
                amount_inr = (plan.get("price_paise", 0) / 100) if plan else 0
                send_payment_receipt(user_email, user_name, plan_name, amount_inr, data.razorpay_payment_id)
            except Exception as e:
                logger.error(f"[BILLING] Payment receipt email failed: {e}")
        return result
    except ValueError as e:
        raise HTTPException(400, str(e))
    except Exception as e:
        logger.error(f"[BILLING] Verify payment failed: {e}")
        raise HTTPException(500, f"Payment verification failed: {str(e)}")


@billing_router.get("/api/billing/payments")
def api_get_payments(current_user: dict = Depends(get_current_user)):
    org_id = current_user.get("org_id")
    return get_payment_history(org_id)


# ─── Razorpay Webhook (no auth — verified by signature) ──────────────────────

@billing_router.post("/api/billing/webhook/razorpay")
async def api_razorpay_webhook(request: Request):
    raw_body = await request.body()
    headers = dict(request.headers)
    try:
        result = handle_razorpay_webhook(headers, raw_body)
        return result
    except ValueError as e:
        raise HTTPException(400, str(e))
