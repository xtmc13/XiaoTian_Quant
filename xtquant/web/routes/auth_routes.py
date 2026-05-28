"""
Authentication routes — Login, Registration, Token management.
"""

import os
import logging
from fastapi import APIRouter, HTTPException, Depends
from pydantic import BaseModel

from xtquant.auth import (
    generate_token, verify_password, hash_password, User, get_current_user, require_admin
)

logger = logging.getLogger("xtquant.web.auth")
router = APIRouter(prefix="/api/auth", tags=["auth"])


# ── Request / Response Models ──

class LoginRequest(BaseModel):
    username: str
    password: str

class RegisterRequest(BaseModel):
    username: str
    password: str
    nickname: str = ""
    email: str = ""

class TokenResponse(BaseModel):
    access_token: str
    token_type: str = "bearer"
    user: dict

class UserInfo(BaseModel):
    id: int
    username: str
    role: str
    nickname: str = ""


# ── Database access (works with both SQLite and PostgreSQL) ──

async def _get_db():
    """Get database connection — tries PostgreSQL first, falls back to SQLite."""
    from xtquant.db.postgres import is_postgres_available

    if await is_postgres_available():
        return "postgres"
    else:
        return "sqlite"


async def _find_user(username: str) -> dict:
    """Find a user by username across both DB backends."""
    # Try SQLite first (default)
    try:
        from xtquant.db.database import get_db
        db = await get_db()
        cursor = await db.execute(
            "SELECT id, username, password_hash, nickname, role, token_version FROM xt_users WHERE username = ?",
            (username,)
        )
        row = await cursor.fetchone()
        if row:
            return dict(row)
    except Exception:
        pass

    # Try PostgreSQL
    try:
        from xtquant.db.postgres import execute_sql
        rows = await execute_sql(
            "SELECT id, username, password_hash, nickname, role, token_version FROM xt_users WHERE username = $1",
            username
        )
        if rows:
            return rows[0]
    except Exception:
        pass

    return None


async def _create_user(username: str, password: str, nickname: str = "", email: str = "",
                       role: str = "user") -> int:
    """Create a new user. Returns user_id."""
    pw_hash = hash_password(password)

    try:
        from xtquant.db.database import get_db
        db = await get_db()
        cursor = await db.execute(
            "INSERT INTO xt_users (username, password_hash, nickname, email, role) VALUES (?, ?, ?, ?, ?)",
            (username, pw_hash, nickname, email, role)
        )
        await db.commit()
        return cursor.lastrowid
    except Exception:
        pass

    try:
        from xtquant.db.postgres import execute_sql
        rows = await execute_sql(
            "INSERT INTO xt_users (username, password_hash, nickname, email, role) VALUES ($1, $2, $3, $4, $5) RETURNING id",
            username, pw_hash, nickname, email, role
        )
        if rows:
            return rows[0]["id"]
    except Exception:
        pass

    raise HTTPException(status_code=500, detail="Failed to create user")


# ── Routes ──

@router.post("/login", response_model=TokenResponse)
async def login(req: LoginRequest):
    """Authenticate and get JWT token."""
    # Dev mode: default admin
    dev_user = os.getenv("ADMIN_USER", "")
    dev_pass = os.getenv("ADMIN_PASSWORD", "")

    if dev_user and dev_pass and req.username == dev_user and req.password == dev_pass:
        user = User(id=1, username=dev_user, role="admin", token_version=1)
        token = generate_token(user)
        return TokenResponse(
            access_token=token,
            user={"id": 1, "username": dev_user, "role": "admin", "nickname": "Admin"}
        )

    # Database lookup
    row = await _find_user(req.username)
    if not row:
        raise HTTPException(status_code=401, detail="Invalid username or password")

    if not verify_password(req.password, row.get("password_hash", "")):
        raise HTTPException(status_code=401, detail="Invalid username or password")

    user = User(
        id=row["id"],
        username=row["username"],
        role=row.get("role", "user"),
        token_version=row.get("token_version", 1),
    )
    token = generate_token(user)

    return TokenResponse(
        access_token=token,
        user={
            "id": user.id,
            "username": user.username,
            "role": user.role,
            "nickname": row.get("nickname", ""),
        }
    )


@router.post("/register", response_model=TokenResponse)
async def register(req: RegisterRequest):
    """Register a new user account."""
    if len(req.username) < 3:
        raise HTTPException(status_code=400, detail="Username must be at least 3 characters")
    if len(req.password) < 6:
        raise HTTPException(status_code=400, detail="Password must be at least 6 characters")

    existing = await _find_user(req.username)
    if existing:
        raise HTTPException(status_code=409, detail="Username already exists")

    user_id = await _create_user(req.username, req.password, req.nickname, req.email)

    user = User(id=user_id, username=req.username, role="user", token_version=1)
    token = generate_token(user)

    return TokenResponse(
        access_token=token,
        user={"id": user_id, "username": req.username, "role": "user", "nickname": req.nickname}
    )


@router.get("/me", response_model=UserInfo)
async def get_me(user: User = Depends(get_current_user)):
    """Get current user info."""
    return UserInfo(id=user.id, username=user.username, role=user.role)


@router.post("/refresh")
async def refresh_token(user: User = Depends(get_current_user)):
    """Get a new token with fresh expiry."""
    token = generate_token(user)
    return {"access_token": token, "token_type": "bearer"}


@router.get("/admin/users")
async def list_users(admin: User = Depends(require_admin)):
    """Admin: List all users."""
    try:
        from xtquant.db.database import get_db
        db = await get_db()
        cursor = await db.execute(
            "SELECT id, username, nickname, email, role, is_active, created_at FROM xt_users"
        )
        rows = await cursor.fetchall()
        return [dict(r) for r in rows]
    except Exception:
        pass

    try:
        from xtquant.db.postgres import execute_sql
        return await execute_sql(
            "SELECT id, username, nickname, email, role, is_active, created_at FROM xt_users"
        )
    except Exception:
        pass

    return []
