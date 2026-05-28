"""
XiaoTianQuant Web Server — Full trading terminal with all API endpoints.
Start with: python run_web.py
"""
import sys
import os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import uvicorn
import logging
from fastapi import FastAPI
from fastapi.responses import HTMLResponse, Response
from fastapi.middleware.cors import CORSMiddleware

from xtquant.web.app import INDEX_HTML

# ── Route modules ──
from xtquant.web.routes.config_routes import router as config_router
from xtquant.web.routes.strategy_routes import router as strategy_router
from xtquant.web.routes.ai_routes import router as ai_router
from xtquant.web.routes.order_routes import router as order_router
from xtquant.web.routes.market_routes import router as market_router
from xtquant.web.routes.agent_routes import router as agent_router
from xtquant.web.routes.ws_routes import router as ws_router
from xtquant.web.routes.ai_analysis_routes import router as ai_analysis_router
from xtquant.web.routes.auth_routes import router as auth_router
from xtquant.web.routes.dashboard_routes import router as dashboard_router

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("run_web")

# ── App ──
app = FastAPI(title="XiaoTianQuant v2.0", docs_url=None, redoc_url=None)
app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"])

# Mount all route modules
app.include_router(config_router)
app.include_router(strategy_router)
app.include_router(ai_router)
app.include_router(order_router)
app.include_router(market_router)
app.include_router(agent_router)
app.include_router(ws_router)
app.include_router(ai_analysis_router)
app.include_router(auth_router)
app.include_router(dashboard_router)


# ── Startup / Shutdown ──
@app.on_event("startup")
async def on_startup():
    """Initialize database and services on startup."""
    logger.info("Initializing database...")

    # Initialize SQLite tables
    try:
        from xtquant.db.database import get_db, init_db
        db = await get_db()
        await init_db()
    except Exception as e:
        logger.warning(f"SQLite init skipped: {e}")

    # Initialize PostgreSQL if configured
    try:
        from xtquant.db.postgres import is_postgres_available
        if await is_postgres_available():
            logger.info("PostgreSQL available")
    except Exception:
        pass

    # Initialize Redis cache
    try:
        from xtquant.utils.cache import get_client
        client = await get_client()
        if client:
            logger.info("Redis cache connected")
    except Exception:
        pass

    # Initialize trading engine (lightweight, for dashboard/API data)
    try:
        from xtquant.core.engine import TradingEngine
        from xtquant.core.clock import Clock
        from xtquant.web.routes.shared import set_engine
        engine = TradingEngine(config={}, clock=Clock(mode="live"))
        await engine.start()
        set_engine(engine)
        logger.info("Trading engine started for web API")
    except Exception as e:
        logger.warning(f"Trading engine init skipped: {e}")
        pass

    # Ensure JWT secret is set
    import os, secrets
    if not os.environ.get("SECRET_KEY"):
        os.environ["SECRET_KEY"] = secrets.token_hex(32)
        logger.info("Generated random SECRET_KEY for JWT")

    logger.info("XiaoTianQuant v2.0 ready")


@app.on_event("shutdown")
async def on_shutdown():
    """Clean up resources on shutdown."""
    try:
        from xtquant.db.postgres import close_pool
        await close_pool()
    except Exception:
        pass
    try:
        from xtquant.utils.cache import close_client
        await close_client()
    except Exception:
        pass
    logger.info("XiaoTianQuant shutdown complete")


# ── Page ──
@app.get("/", response_class=HTMLResponse)
async def index():
    return Response(content=INDEX_HTML, media_type="text/html",
                    headers={"Cache-Control": "no-cache, no-store, must-revalidate",
                             "Pragma": "no-cache", "Expires": "0"})


if __name__ == "__main__":
    logger.info("Starting XiaoTianQuant web server on http://0.0.0.0:8080")
    uvicorn.run(app, host="0.0.0.0", port=8080, log_level="warning")
