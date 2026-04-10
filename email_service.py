"""
email.py — Email notification module for Callified AI.

Simple SMTP-based email sending with HTML templates.
Uses Python's built-in smtplib — no external dependencies.
"""
import os
import smtplib
from email.mime.text import MIMEText
from email.mime.multipart import MIMEMultipart
import logging

logger = logging.getLogger("uvicorn.error")

# ─── SMTP Config ────────────────────────────────────────────────────────────

SMTP_HOST = os.getenv("SMTP_HOST", "smtp.gmail.com")
SMTP_PORT = int(os.getenv("SMTP_PORT", "587"))
SMTP_USER = os.getenv("SMTP_USER", "")
SMTP_PASSWORD = os.getenv("SMTP_PASSWORD", "")
SMTP_FROM_NAME = os.getenv("SMTP_FROM_NAME", "Callified AI")

APP_URL = os.getenv("APP_URL", "https://test.callified.ai")


# ─── Base HTML wrapper ──────────────────────────────────────────────────────

def _wrap_html(title: str, body_content: str) -> str:
    """Wrap content in a branded HTML email template."""
    return f"""\
<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>{title}</title></head>
<body style="margin:0;padding:0;background:#0f172a;font-family:Arial,Helvetica,sans-serif;">
  <table width="100%" cellpadding="0" cellspacing="0" style="background:#0f172a;padding:40px 0;">
    <tr><td align="center">
      <table width="600" cellpadding="0" cellspacing="0" style="background:#1e293b;border-radius:8px;overflow:hidden;">
        <tr>
          <td style="background:#6366f1;padding:24px 32px;">
            <h1 style="margin:0;color:#f8fafc;font-size:22px;">Callified AI</h1>
          </td>
        </tr>
        <tr>
          <td style="padding:32px;color:#f8fafc;font-size:15px;line-height:1.6;">
            {body_content}
          </td>
        </tr>
        <tr>
          <td style="padding:16px 32px;background:#0f172a;color:#94a3b8;font-size:12px;text-align:center;">
            Callified AI &mdash; AI-Powered Voice Dialer
          </td>
        </tr>
      </table>
    </td></tr>
  </table>
</body>
</html>"""


# ─── Core send function ────────────────────────────────────────────────────

def send_email(to_email: str, subject: str, html_body: str) -> bool:
    """Send an HTML email. Returns True on success."""
    if not SMTP_USER or not SMTP_PASSWORD:
        logger.warning("[EMAIL] SMTP_USER or SMTP_PASSWORD not configured, skipping email")
        return False

    try:
        msg = MIMEMultipart("alternative")
        msg["From"] = f"{SMTP_FROM_NAME} <{SMTP_USER}>"
        msg["To"] = to_email
        msg["Subject"] = subject
        msg.attach(MIMEText(html_body, "html"))

        with smtplib.SMTP(SMTP_HOST, SMTP_PORT) as server:
            server.starttls()
            server.login(SMTP_USER, SMTP_PASSWORD)
            server.sendmail(SMTP_USER, to_email, msg.as_string())

        logger.info(f"[EMAIL] Sent '{subject}' to {to_email}")
        return True
    except Exception as e:
        logger.error(f"[EMAIL] Failed to send '{subject}' to {to_email}: {e}")
        return False


# ─── Template functions ─────────────────────────────────────────────────────

def send_welcome_email(to_email: str, first_name: str, password: str = None):
    """Welcome email for new trial signups."""
    try:
        creds_block = ""
        if password:
            creds_block = f"""\
            <p style="background:#334155;padding:16px;border-radius:6px;margin:16px 0;">
              <strong>Email:</strong> {to_email}<br>
              <strong>Password:</strong> {password}
            </p>"""

        body = f"""\
            <h2 style="color:#a5b4fc;margin-top:0;">Welcome, {first_name}!</h2>
            <p>Your Callified AI account is ready. Start making AI-powered calls in minutes.</p>
            {creds_block}
            <p>
              <a href="{APP_URL}/login" style="display:inline-block;background:#6366f1;color:#f8fafc;
                 padding:12px 28px;border-radius:6px;text-decoration:none;font-weight:bold;">
                Log In to Dashboard
              </a>
            </p>
            <p style="color:#94a3b8;margin-top:24px;">Need help? Just reply to this email.</p>"""

        html = _wrap_html("Welcome to Callified AI", body)
        send_email(to_email, "Welcome to Callified AI", html)
    except Exception as e:
        logger.error(f"[EMAIL] Welcome email failed for {to_email}: {e}")


def send_payment_receipt(to_email: str, name: str, plan_name: str, amount_inr: float, payment_id: str):
    """Payment confirmation email."""
    try:
        body = f"""\
            <h2 style="color:#a5b4fc;margin-top:0;">Payment Confirmed</h2>
            <p>Hi {name}, your payment has been received.</p>
            <table style="width:100%;background:#334155;border-radius:6px;padding:16px;margin:16px 0;"
                   cellpadding="8" cellspacing="0">
              <tr>
                <td style="color:#94a3b8;">Plan</td>
                <td style="color:#f8fafc;text-align:right;font-weight:bold;">{plan_name}</td>
              </tr>
              <tr>
                <td style="color:#94a3b8;">Amount</td>
                <td style="color:#f8fafc;text-align:right;font-weight:bold;">&#8377;{amount_inr:,.2f}</td>
              </tr>
              <tr>
                <td style="color:#94a3b8;">Payment ID</td>
                <td style="color:#f8fafc;text-align:right;font-size:13px;">{payment_id}</td>
              </tr>
            </table>
            <p style="color:#94a3b8;">Your subscription is now active. Thank you for choosing Callified AI.</p>"""

        html = _wrap_html("Payment Receipt", body)
        send_email(to_email, f"Payment Receipt - {plan_name}", html)
    except Exception as e:
        logger.error(f"[EMAIL] Payment receipt failed for {to_email}: {e}")


def send_appointment_confirmation(to_email: str, lead_name: str, appointment_time: str, agent_name: str):
    """Confirm appointment booked by AI."""
    try:
        body = f"""\
            <h2 style="color:#a5b4fc;margin-top:0;">Appointment Booked</h2>
            <p>An AI agent has booked a new appointment.</p>
            <table style="width:100%;background:#334155;border-radius:6px;padding:16px;margin:16px 0;"
                   cellpadding="8" cellspacing="0">
              <tr>
                <td style="color:#94a3b8;">Lead</td>
                <td style="color:#f8fafc;text-align:right;font-weight:bold;">{lead_name}</td>
              </tr>
              <tr>
                <td style="color:#94a3b8;">Time</td>
                <td style="color:#f8fafc;text-align:right;font-weight:bold;">{appointment_time}</td>
              </tr>
              <tr>
                <td style="color:#94a3b8;">AI Agent</td>
                <td style="color:#f8fafc;text-align:right;">{agent_name}</td>
              </tr>
            </table>
            <p>
              <a href="{APP_URL}/leads" style="display:inline-block;background:#6366f1;color:#f8fafc;
                 padding:12px 28px;border-radius:6px;text-decoration:none;font-weight:bold;">
                View in Dashboard
              </a>
            </p>"""

        html = _wrap_html("Appointment Booked", body)
        send_email(to_email, f"Appointment Booked - {lead_name}", html)
    except Exception as e:
        logger.error(f"[EMAIL] Appointment confirmation failed for {to_email}: {e}")


def send_campaign_summary(to_email: str, campaign_name: str, total_calls: int, appointments: int, avg_score: float):
    """Daily campaign summary."""
    try:
        body = f"""\
            <h2 style="color:#a5b4fc;margin-top:0;">Campaign Summary</h2>
            <p>Here's the summary for <strong>{campaign_name}</strong>.</p>
            <table style="width:100%;background:#334155;border-radius:6px;padding:16px;margin:16px 0;"
                   cellpadding="8" cellspacing="0">
              <tr>
                <td style="color:#94a3b8;">Total Calls</td>
                <td style="color:#f8fafc;text-align:right;font-weight:bold;">{total_calls}</td>
              </tr>
              <tr>
                <td style="color:#94a3b8;">Appointments</td>
                <td style="color:#f8fafc;text-align:right;font-weight:bold;">{appointments}</td>
              </tr>
              <tr>
                <td style="color:#94a3b8;">Avg Score</td>
                <td style="color:#f8fafc;text-align:right;font-weight:bold;">{avg_score:.1f}/10</td>
              </tr>
            </table>
            <p>
              <a href="{APP_URL}/campaigns" style="display:inline-block;background:#6366f1;color:#f8fafc;
                 padding:12px 28px;border-radius:6px;text-decoration:none;font-weight:bold;">
                View Full Report
              </a>
            </p>"""

        html = _wrap_html("Campaign Summary", body)
        send_email(to_email, f"Campaign Summary - {campaign_name}", html)
    except Exception as e:
        logger.error(f"[EMAIL] Campaign summary failed for {to_email}: {e}")
