"""
XiaoTianQuant Python Sandbox Service
Provides safe execution and static analysis for indicator Python code.
"""

import os
import sys
import traceback
from typing import Any, Dict, List, Optional

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

from executor import safe_exec_with_validation
from analyzer import analyze_indicator_code_quality

app = FastAPI(title="XiaoTianQuant Sandbox", version="2.0.0")


# ── Request/Response Models ───────────────────────────────────────

class ExecuteRequest(BaseModel):
    code: str
    df_json: Optional[List[Dict[str, Any]]] = None
    params: Optional[Dict[str, Any]] = None
    timeout: int = 20


class ExecuteResponse(BaseModel):
    success: bool
    msg: str
    output: Optional[Dict[str, Any]] = None
    error: Optional[str] = None
    error_type: Optional[str] = None


class AnalyzeRequest(BaseModel):
    code: str


class AnalyzeResponse(BaseModel):
    success: bool
    hints: List[Dict[str, Any]]


# ── Endpoints ─────────────────────────────────────────────────────

@app.get("/health")
def health() -> Dict[str, str]:
    return {"status": "ok", "service": "sandbox"}


@app.post("/execute", response_model=ExecuteResponse)
def execute(req: ExecuteRequest) -> ExecuteResponse:
    """
    Safely execute indicator Python code in a sandboxed environment.
    Returns the output dict (plots, signals) or an error.
    """
    if not req.code or not req.code.strip():
        return ExecuteResponse(success=False, msg="Code is empty", error_type="EmptyCode")

    try:
        result = safe_exec_with_validation(
            code=req.code,
            df_json=req.df_json,
            params=req.params,
            timeout=req.timeout,
        )
        return ExecuteResponse(
            success=result.get("success", False),
            msg=result.get("msg", ""),
            output=result.get("output"),
            error=result.get("error"),
            error_type=result.get("error_type"),
        )
    except Exception as e:
        traceback_str = traceback.format_exc()
        return ExecuteResponse(
            success=False,
            msg=f"Sandbox internal error: {str(e)}",
            error=traceback_str,
            error_type="SandboxInternalError",
        )


@app.post("/analyze", response_model=AnalyzeResponse)
def analyze(req: AnalyzeRequest) -> AnalyzeResponse:
    """
    Perform static analysis on indicator code without executing it.
    Returns code quality hints.
    """
    if not req.code:
        return AnalyzeResponse(success=False, hints=[])

    try:
        hints = analyze_indicator_code_quality(req.code)
        return AnalyzeResponse(success=True, hints=hints)
    except Exception as e:
        return AnalyzeResponse(
            success=False,
            hints=[{
                "severity": "error",
                "code": "ANALYZER_CRASH",
                "params": {"detail": str(e)},
            }],
        )


# ── Main ──────────────────────────────────────────────────────────

if __name__ == "__main__":
    port = int(os.environ.get("SANDBOX_PORT", "9000"))
    uvicorn.run(app, host="0.0.0.0", port=port, log_level="info")
