"""Agent Token Manager — SHA-256 hashed bearer tokens stored in SQLite."""

import hashlib
import secrets
import json
import time
import logging
from typing import Optional, List

logger = logging.getLogger("xtquant.agent.token")


class AgentTokenManager:
    def __init__(self, db):
        self.db = db

    def _hash(self, token: str) -> str:
        return hashlib.sha256(token.encode()).hexdigest()

    @staticmethod
    def generate_token() -> tuple:
        raw = "qd_agent_" + secrets.token_hex(16)
        return raw, hashlib.sha256(raw.encode()).hexdigest()

    async def create_token(
        self, name: str, scopes: List[str],
        paper_only: bool = True, rate_limit: int = 60,
        expires_at: int = 0, created_by: str = ""
    ) -> str:
        import uuid
        token_id = str(uuid.uuid4())
        plain, hashed = self.generate_token()
        now = int(time.time())
        await self.db.insert("agent_tokens", {
            "id": token_id,
            "token_hash": hashed,
            "token_name": name,
            "scopes": json.dumps(scopes),
            "paper_only": 1 if paper_only else 0,
            "rate_limit": rate_limit,
            "expires_at": expires_at,
            "created_by": created_by,
            "created_at": now,
        })
        logger.info("[Agent] token created: %s (id=%s scopes=%s)", name, token_id, scopes)
        return plain

    async def validate_token(self, token_str: str) -> Optional[dict]:
        if not token_str:
            return None
        hashed = self._hash(token_str)
        row = await self.db.fetch_one(
            "SELECT * FROM agent_tokens WHERE token_hash=? AND revoked=0", (hashed,)
        )
        if not row:
            return None
        expires = row.get("expires_at", 0)
        if expires and int(time.time()) > expires:
            return None
        scopes_str = row.get("scopes", '["read_market","run_backtest"]')
        try:
            scopes = json.loads(scopes_str)
        except (json.JSONDecodeError, TypeError):
            scopes = ["read_market", "run_backtest"]
        return {
            "token_id": row["id"],
            "token_name": row.get("token_name", ""),
            "scopes": scopes,
            "paper_only": bool(row.get("paper_only", 1)),
            "rate_limit": row.get("rate_limit", 60),
        }

    async def list_tokens(self) -> list:
        rows = await self.db.fetch_all(
            "SELECT * FROM agent_tokens ORDER BY created_at DESC"
        )
        result = []
        for r in (rows or []):
            th = r.get("token_hash", "")
            scopes_str = r.get("scopes", "[]")
            try:
                scopes = json.loads(scopes_str)
            except Exception:
                scopes = []
            result.append({
                "id": r["id"],
                "token_hash": th[:8] + "..." + th[-4:] if len(th) > 14 else th,
                "token_name": r.get("token_name", ""),
                "scopes": scopes,
                "paper_only": bool(r.get("paper_only", 1)),
                "rate_limit": r.get("rate_limit", 60),
                "expires_at": r.get("expires_at", 0),
                "created_by": r.get("created_by", ""),
                "created_at": r.get("created_at", 0),
                "revoked": bool(r.get("revoked", 0)),
                "revoked_at": r.get("revoked_at", 0),
            })
        return result

    async def revoke_token(self, token_id: str) -> bool:
        now = int(time.time())
        await self.db.execute(
            "UPDATE agent_tokens SET revoked=1, revoked_at=? WHERE id=?",
            (now, token_id)
        )
        logger.info("[Agent] token revoked: %s", token_id)
        return True
