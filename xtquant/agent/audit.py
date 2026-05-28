"""Audit logger for Agent Gateway — every agent call is recorded."""

import time
import logging

logger = logging.getLogger("xtquant.agent.audit")


async def log_audit(db, token_id="", token_name="", endpoint="", method="",
                    params_summary="", status_code=0, ip=""):
    try:
        now = int(time.time())
        summary = (params_summary or "")[:500]
        await db.insert("agent_audit_log", {
            "token_id": token_id,
            "token_name": token_name,
            "endpoint": endpoint,
            "method": method,
            "params_summary": summary,
            "status_code": status_code,
            "ip": ip or "",
            "created_at": now,
        })
    except Exception as e:
        logger.warning("[Agent] audit log write failed: %s", e)
