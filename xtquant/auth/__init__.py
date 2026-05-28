"""
JWT Authentication for XiaoTianQuant.
Supports multi-user with role-based access control (RBAC).

Patterns adapted from QuantDinger's auth system.
"""

import os
import time
import hashlib
import secrets
from typing import Optional, Dict, Any
from dataclasses import dataclass

import jwt
from fastapi import Request, HTTPException, Depends
from fastapi.security import HTTPBearer, HTTPAuthorizationCredentials

security_scheme = HTTPBearer(auto_error=False)

# ── Token Management ──

SECRET_KEY = os.getenv("SECRET_KEY", "xtquant-dev-secret-change-in-production")
TOKEN_EXPIRY_DAYS = int(os.getenv("TOKEN_EXPIRY_DAYS", "7"))
ALGORITHM = "HS256"


@dataclass
class User:
    id: int
    username: str
    role: str  # admin, manager, user, viewer
    token_version: int = 1


def hash_password(password: str) -> str:
    """Hash a password using bcrypt (fallback: sha256 for simplicity)."""
    try:
        import bcrypt
        return bcrypt.hashpw(password.encode(), bcrypt.gensalt()).decode()
    except ImportError:
        salt = secrets.token_hex(16)
        return f"sha256${salt}${hashlib.sha256((password + salt).encode()).hexdigest()}"


def verify_password(password: str, password_hash: str) -> bool:
    """Verify a password against its hash."""
    try:
        if password_hash.startswith("$2b$") or password_hash.startswith("$2a$"):
            import bcrypt
            return bcrypt.checkpw(password.encode(), password_hash.encode())
        if password_hash.startswith("sha256$"):
            parts = password_hash.split("$")
            if len(parts) == 3:
                salt, h = parts[1], parts[2]
                return h == hashlib.sha256((password + salt).encode()).hexdigest()
    except Exception:
        pass
    return False


def generate_token(user: User) -> str:
    """Generate a JWT for the given user."""
    now = int(time.time())
    payload = {
        "sub": user.username,
        "user_id": user.id,
        "role": user.role,
        "token_version": user.token_version,
        "iat": now,
        "exp": now + TOKEN_EXPIRY_DAYS * 86400,
        "jti": secrets.token_hex(8),
    }
    return jwt.encode(payload, SECRET_KEY, algorithm=ALGORITHM)


def verify_token(token: str) -> Optional[Dict[str, Any]]:
    """Verify a JWT and return its payload, or None if invalid."""
    try:
        return jwt.decode(token, SECRET_KEY, algorithms=[ALGORITHM])
    except (jwt.ExpiredSignatureError, jwt.InvalidTokenError):
        return None


# ── FastAPI Dependencies ──

async def get_current_user(
    request: Request,
    credentials: Optional[HTTPAuthorizationCredentials] = Depends(security_scheme),
) -> User:
    """FastAPI dependency: extract and validate the current user from Bearer token."""
    token = None
    if credentials:
        token = credentials.credentials
    if not token:
        # Also check query param for WebSocket compatibility
        token = request.query_params.get("token")

    if not token:
        raise HTTPException(status_code=401, detail="Authentication required")

    payload = verify_token(token)
    if not payload:
        raise HTTPException(status_code=401, detail="Invalid or expired token")

    return User(
        id=payload["user_id"],
        username=payload["sub"],
        role=payload.get("role", "user"),
        token_version=payload.get("token_version", 1),
    )


async def get_optional_user(
    request: Request,
    credentials: Optional[HTTPAuthorizationCredentials] = Depends(security_scheme),
) -> Optional[User]:
    """FastAPI dependency: extract user if token present, otherwise None."""
    try:
        return await get_current_user(request, credentials)
    except HTTPException:
        return None


def require_role(*roles: str):
    """FastAPI dependency factory: require one of the given roles."""

    async def role_check(user: User = Depends(get_current_user)) -> User:
        if user.role not in roles:
            raise HTTPException(status_code=403, detail=f"Requires one of: {', '.join(roles)}")
        return user

    return role_check


require_admin = require_role("admin")
require_manager = require_role("admin", "manager")


# ── Agent Token (API Key) Management ──

def generate_agent_token(name: str = "", scopes: list = None) -> str:
    """Generate a long-lived agent API token."""
    raw = f"xt_{secrets.token_hex(32)}"
    token_hash = hashlib.sha256(raw.encode()).hexdigest()
    return raw, token_hash


def verify_agent_token(token: str, stored_hash: str) -> bool:
    """Verify an agent API token against its stored hash."""
    return hashlib.sha256(token.encode()).hexdigest() == stored_hash


# ── Token Blacklist (invalidation) ──

_token_blacklist: set = set()  # jti values of revoked tokens


def revoke_token(jti: str):
    """Revoke a specific token by its JWT ID."""
    _token_blacklist.add(jti)


def is_token_revoked(jti: str) -> bool:
    return jti in _token_blacklist
