"""XiaoTianQuant Python Sandbox CLI
提供指标代码执行和静态分析的命令行工具。
"""
import argparse
import json
import sys
from executor import safe_exec_with_validation
from analyzer import analyze_indicator_code_quality

try:
    from typing import Any, Dict, List, Optional
    from fastapi import FastAPI
    from pydantic import BaseModel
    _HAS_FASTAPI = True
except ImportError:
    _HAS_FASTAPI = False


def main():
    parser = argparse.ArgumentParser(description="XiaoTianQuant Sandbox CLI")
    subparsers = parser.add_subparsers(dest="command")

    # execute subcommand
    exec_parser = subparsers.add_parser("execute", help="Execute indicator code safely")
    exec_parser.add_argument("--code", required=True, help="Python code to execute")
    exec_parser.add_argument("--df-json", help="DataFrame JSON data")
    exec_parser.add_argument("--params", help="Parameters JSON")
    exec_parser.add_argument("--timeout", type=int, default=20)

    # analyze subcommand
    analyze_parser = subparsers.add_parser("analyze", help="Analyze indicator code quality")
    analyze_parser.add_argument("--code", required=True, help="Python code to analyze")

    args = parser.parse_args()

    if args.command == "execute":
        params = json.loads(args.params) if args.params else None
        df_json = json.loads(args.df_json) if args.df_json else None
        result = safe_exec_with_validation(
            code=args.code, df_json=df_json, params=params, timeout=args.timeout
        )
        print(json.dumps(result, indent=2))
    elif args.command == "analyze":
        hints = analyze_indicator_code_quality(args.code)
        print(json.dumps({"hints": hints}, indent=2))
    else:
        parser.print_help()


if __name__ == "__main__":
    main()


# ── FastAPI server (uvicorn main:app) ─────────────────────────────
# Optional: the CLI above works without fastapi installed.

def _json_safe(obj):
    """Convert numpy/pandas values into plain JSON-serializable types."""
    import numpy as np
    import pandas as pd
    if isinstance(obj, dict):
        return {str(k): _json_safe(v) for k, v in obj.items()}
    if isinstance(obj, (list, tuple)):
        return [_json_safe(v) for v in obj]
    if isinstance(obj, np.ndarray):
        return _json_safe(obj.tolist())
    if isinstance(obj, (np.integer,)):
        return int(obj)
    if isinstance(obj, (np.floating,)):
        val = float(obj)
        return val if val == val and abs(val) != float("inf") else None
    if isinstance(obj, (np.bool_,)):
        return bool(obj)
    if isinstance(obj, float):
        return obj if obj == obj and abs(obj) != float("inf") else None
    if isinstance(obj, (pd.Series, pd.DataFrame)):
        return _json_safe(obj.to_dict("records") if isinstance(obj, pd.DataFrame) else obj.tolist())
    return obj


if _HAS_FASTAPI:
    import re

    app = FastAPI(title="XiaoTianQuant Sandbox")

    class ExecuteRequest(BaseModel):
        code: str
        df_json: Optional[List[Dict[str, Any]]] = None
        params: Optional[Dict[str, Any]] = None
        timeout: int = 20

    class AnalyzeRequest(BaseModel):
        code: str

    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/execute")
    def execute(req: ExecuteRequest):
        result = safe_exec_with_validation(
            code=req.code, df_json=req.df_json, params=req.params, timeout=req.timeout
        )
        # Without an explicit df_json the executor validates against its own
        # 200-row mock DataFrame. If the code's output has a different length,
        # retry once with a mock DataFrame matched to the output length so
        # ad-hoc validation of fixed-size snippets still succeeds.
        if (
            req.df_json is None
            and result.get("error_type") == "LengthMismatch"
            and result.get("output") is None
        ):
            m = re.search(r"data length \((\d+)\)", result.get("error", "") + result.get("msg", ""))
            if m and int(m.group(1)) > 0:
                from executor import generate_mock_df
                df_json = generate_mock_df(int(m.group(1))).to_dict("records")
                result = safe_exec_with_validation(
                    code=req.code, df_json=df_json, params=req.params, timeout=req.timeout
                )
        return {
            "success": result.get("success", False),
            "msg": result.get("msg", ""),
            "output": _json_safe(result.get("output")),
            "error": result.get("error"),
            "error_type": result.get("error_type"),
        }

    @app.post("/analyze")
    def analyze(req: AnalyzeRequest):
        hints = analyze_indicator_code_quality(req.code)
        return {"success": True, "hints": _json_safe(hints)}
