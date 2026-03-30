"""
auth.py — Authentication module for Callified AI Dialer.
Handles JWT tokens, password hashing, login, signup, and user retrieval.
"""
import os
import time
import threading
import jwt
from typing import Optional
from datetime import datetime, timedelta
from fastapi import APIRouter, Depends, HTTPException, Request
from fastapi.security import OAuth2PasswordBearer
from passlib.context import CryptContext
from pydantic import BaseModel
from database import get_user_by_email, create_user, create_organization, get_all_organizations

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
