"""
auth.py — Authentication module for Callified AI Dialer.
Handles JWT tokens, password hashing, login, signup, and user retrieval.
"""
import os
import time
import secrets
import logging
import threading
import jwt
from typing import Optional
from datetime import datetime, timedelta
from fastapi import APIRouter, Depends, HTTPException, Request
from fastapi.security import OAuth2PasswordBearer
from passlib.context import CryptContext
from pydantic import BaseModel
from database import (
    get_user_by_email, create_user, create_organization, get_all_organizations,
    create_reset_token, get_valid_reset_token, mark_token_used, update_user_password,
    validate_api_key,
)
from email_service import send_email, _wrap_html, APP_URL

logger = logging.getLogger("uvicorn.error")

# ─── Config ──────────────────────────────────────────────────────────────────

SECRET_KEY = os.getenv("JWT_SECRET_KEY", "your-secret-key-replace-in-production")
ALGORITHM = "HS256"
ACCESS_TOKEN_EXPIRE_MINUTES = 60 * 24 * 7  # 7 days

pwd_context = CryptContext(schemes=["bcrypt"], deprecated="auto")
oauth2_scheme = OAuth2PasswordBearer(tokenUrl="/api/auth/login")

# ─── Rate Limiter ───────────────────────────────────────────────────────────

class RateLimiter:
    """Simple in-memory rate limiter keyed by IP address."""

    def __init__(self):
        self._hits: dict[str, list[float]] = {}
        self._lock = threading.Lock()

    def is_rate_limited(self, key: str, limit: int, window: int) -> bool:
        """Return True if *key* has exceeded *limit* requests in *window* seconds."""
        now = time.time()
        with self._lock:
            timestamps = self._hits.get(key, [])
            # Prune entries older than the window
            timestamps = [t for t in timestamps if now - t < window]
            if len(timestamps) >= limit:
                self._hits[key] = timestamps
                return True
            timestamps.append(now)
            self._hits[key] = timestamps
            return False

    def cleanup(self, window: int = 60):
        """Remove all entries older than *window* seconds."""
        now = time.time()
        with self._lock:
            keys_to_delete = []
            for key, timestamps in self._hits.items():
                self._hits[key] = [t for t in timestamps if now - t < window]
                if not self._hits[key]:
                    keys_to_delete.append(key)
            for key in keys_to_delete:
                del self._hits[key]

_rate_limiter = RateLimiter()

def check_rate_limit(request: Request, limit: int, window: int = 60):
    """Raise HTTP 429 if the client IP exceeds the allowed rate."""
    client_ip = request.client.host if request.client else "unknown"
    endpoint = request.url.path
    key = f"{client_ip}:{endpoint}"
    if _rate_limiter.is_rate_limited(key, limit, window):
        # Opportunistic cleanup of stale entries
        _rate_limiter.cleanup(window)
        raise HTTPException(
            status_code=429,
            detail=f"Too many requests. Limit is {limit} per {window}s. Please try again later.",
        )

# ─── Helpers ─────────────────────────────────────────────────────────────────

def verify_password(plain_password, hashed_password):
    return pwd_context.verify(plain_password, hashed_password)

def get_password_hash(password):
    return pwd_context.hash(password)

def create_user_direct(username: str, password: str, full_name: str, org_id: int) -> dict:
    """Create a user programmatically (for trial auto-provisioning).
    Returns dict with user_id, email, and org_id."""
    existing = get_user_by_email(username)
    if existing:
        raise ValueError("Email already registered")
    hashed = get_password_hash(password)
    user_id = create_user(username, hashed, full_name, role="Admin", org_id=org_id)
    return {"user_id": user_id, "email": username, "org_id": org_id}

def create_access_token(data: dict, expires_delta: Optional[timedelta] = None):
    to_encode = data.copy()
    if expires_delta:
        expire = datetime.utcnow() + expires_delta
    else:
        expire = datetime.utcnow() + timedelta(minutes=15)
    to_encode.update({"exp": expire})
    encoded_jwt = jwt.encode(to_encode, SECRET_KEY, algorithm=ALGORITHM)
    return encoded_jwt

async def get_current_user(token: str = Depends(oauth2_scheme)):
    credentials_exception = HTTPException(
        status_code=401,
        detail="Could not validate credentials",
        headers={"WWW-Authenticate": "Bearer"},
    )
    try:
        payload = jwt.decode(token, SECRET_KEY, algorithms=[ALGORITHM])
        email: str = payload.get("sub")
        if email is None:
            raise credentials_exception
    except jwt.PyJWTError:
        raise credentials_exception
    user = get_user_by_email(email)
    if user is None:
        raise credentials_exception
    return user

async def get_current_user_or_api_key(request: Request):
    """Try JWT auth first, then fall back to API key in Authorization: Bearer header.

    If the Bearer token starts with 'cal_', validate it as an API key and return
    a synthetic user dict with org_id and role='ApiKey'.  Otherwise delegate to
    the normal JWT-based get_current_user flow.
    """
    auth_header = request.headers.get("Authorization", "")
    if auth_header.startswith("Bearer "):
        token = auth_header[7:]
        if token.startswith("cal_"):
            api_key_row = validate_api_key(token)
            if api_key_row is None:
                raise HTTPException(
                    status_code=401,
                    detail="Invalid or revoked API key",
                    headers={"WWW-Authenticate": "Bearer"},
                )
            return {
                "id": None,
                "org_id": api_key_row["org_id"],
                "role": "ApiKey",
                "email": f"apikey:{api_key_row['name']}",
                "full_name": api_key_row["name"],
                "_api_key_id": api_key_row["id"],
            }
    # Fall back to normal JWT auth
    return await get_current_user(await oauth2_scheme(request))

# ─── Pydantic Models ────────────────────────────────────────────────────────

class UserCreate(BaseModel):
    email: str
    password: str
    full_name: str
    role: str = "Agent"

class OrgSignup(BaseModel):
    org_name: str
    full_name: str
    email: str
    password: str

class LoginRequest(BaseModel):
    email: str
    password: str

class ForgotPasswordRequest(BaseModel):
    email: str

class ResetPasswordRequest(BaseModel):
    token: str
    new_password: str

# ─── Router ──────────────────────────────────────────────────────────────────

auth_router = APIRouter()

@auth_router.post("/api/auth/signup")
def signup(data: OrgSignup, request: Request):
    """Create organization + admin user in one step."""
    check_rate_limit(request, limit=5, window=60)
    existing = get_user_by_email(data.email)
    if existing:
        raise HTTPException(status_code=400, detail="Email already registered")
    org_id = create_organization(data.org_name)
    hashed = get_password_hash(data.password)
    user_id = create_user(data.email, hashed, data.full_name, role="Admin", org_id=org_id)
    token = create_access_token(data={"sub": data.email, "org_id": org_id}, expires_delta=timedelta(minutes=ACCESS_TOKEN_EXPIRE_MINUTES))
    return {
        "access_token": token, "token_type": "bearer",
        "user": {"id": user_id, "email": data.email, "full_name": data.full_name, "role": "Admin", "org_id": org_id, "org_name": data.org_name}
    }

@auth_router.post("/api/auth/login")
def login(data: LoginRequest, request: Request):
    check_rate_limit(request, limit=10, window=60)
    user = get_user_by_email(data.email)
    if not user or not verify_password(data.password, user["password_hash"]):
        raise HTTPException(status_code=401, detail="Incorrect email or password")
    org_id = user.get("org_id")
    org_name = ""
    if org_id:
        orgs = get_all_organizations()
        org = next((o for o in orgs if o["id"] == org_id), None)
        org_name = org["name"] if org else ""
    token = create_access_token(data={"sub": user["email"], "org_id": org_id}, expires_delta=timedelta(minutes=ACCESS_TOKEN_EXPIRE_MINUTES))
    return {
        "access_token": token, "token_type": "bearer",
        "user": {"id": user["id"], "email": user["email"], "full_name": user.get("full_name", ""), "role": user.get("role", "Admin"), "org_id": org_id, "org_name": org_name}
    }

@auth_router.get("/api/auth/me")
async def get_me(current_user: dict = Depends(get_current_user)):
    org_id = current_user.get("org_id")
    org_name = ""
    if org_id:
        orgs = get_all_organizations()
        org = next((o for o in orgs if o["id"] == org_id), None)
        org_name = org["name"] if org else ""
    return {
        "id": current_user["id"], "email": current_user["email"],
        "full_name": current_user.get("full_name", ""), "role": current_user.get("role"),
        "org_id": org_id, "org_name": org_name
    }

@auth_router.post("/api/auth/forgot-password")
def forgot_password(data: ForgotPasswordRequest, request: Request):
    """Send a password reset link. Always returns success to avoid leaking user existence."""
    check_rate_limit(request, limit=5, window=60)
    user = get_user_by_email(data.email)
    if user:
        token = secrets.token_urlsafe(32)
        expires_at = datetime.utcnow() + timedelta(hours=1)
        create_reset_token(user["id"], token, expires_at)
        reset_link = f"{APP_URL}/reset-password?token={token}"
        body = f"""\
            <h2 style="color:#a5b4fc;margin-top:0;">Reset Your Password</h2>
            <p>We received a request to reset your password. Click the button below to set a new password.</p>
            <p>
              <a href="{reset_link}" style="display:inline-block;background:#6366f1;color:#f8fafc;
                 padding:12px 28px;border-radius:6px;text-decoration:none;font-weight:bold;">
                Reset Password
              </a>
            </p>
            <p style="color:#94a3b8;margin-top:24px;font-size:13px;">
              This link expires in 1 hour. If you didn't request this, you can safely ignore this email.
            </p>"""
        html = _wrap_html("Password Reset", body)
        send_email(user["email"], "Reset Your Password - Callified AI", html)
        logger.info(f"[AUTH] Password reset email sent to {data.email}")
    else:
        logger.info(f"[AUTH] Password reset requested for unknown email: {data.email}")
    return {"message": "If an account with that email exists, a reset link has been sent."}

@auth_router.post("/api/auth/reset-password")
def reset_password(data: ResetPasswordRequest, request: Request):
    """Reset password using a valid token."""
    check_rate_limit(request, limit=5, window=60)
    token_row = get_valid_reset_token(data.token)
    if not token_row:
        raise HTTPException(status_code=400, detail="Invalid or expired reset token")
    if len(data.new_password) < 6:
        raise HTTPException(status_code=400, detail="Password must be at least 6 characters")
    hashed = get_password_hash(data.new_password)
    update_user_password(token_row["user_id"], hashed)
    mark_token_used(token_row["id"])
    logger.info(f"[AUTH] Password reset completed for user_id={token_row['user_id']}")
    return {"message": "Password has been reset successfully. You can now log in."}
