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

    class RunRequest(BaseModel):
        code: str
        timeout: int = 30
        cwd: str = ""  # 允许列表内的工作目录（B 方案：管理员 /workspace，缺省沙箱根）

    class RunShellRequest(BaseModel):
        command: str
        timeout: int = 30
        cwd: str = ""

    ALLOWED_CWDS = ("/data/agent_files", "/workspace")

    def _effective_ws(cwd: str) -> str:
        """校验并返回有效工作目录：仅允许白名单内的绝对路径（沙箱根/开放工作区）。"""
        cwd = (cwd or "").strip()
        if not cwd:
            return "/data/agent_files"
        for base in ALLOWED_CWDS:
            if cwd == base or cwd.startswith(base + "/"):
                return cwd
        raise ValueError(f"cwd 不在允许列表内: {cwd}")

    class FetchRequest(BaseModel):
        url: str
        timeout: int = 15

    def _ws_snapshot(ws: str):
        """沙箱工作区文件快照 {相对路径: mtime}（跳过隐藏文件），供执行前后 diff。"""
        import os
        out = {}
        for root, _dirs, files in os.walk(ws):
            for f in files:
                if f.startswith("."):
                    continue
                p = os.path.join(root, f)
                rel = os.path.relpath(p, ws)
                try:
                    out[rel] = os.path.getmtime(p)
                except OSError:
                    pass
        return out

    @app.post("/run")
    def run(req: RunRequest):
        """通用代码执行（agent run_python 工具）：工作目录为共享文件沙箱
        /data/agent_files（与网关文件工具同一目录），可 import pandas/numpy/ccxt
        等沙箱预装库。不做 AST 限制——容器即边界。输出截断防撑爆响应。
        附带执行前后文件快照 diff（files.created/modified），前端据此生成产物卡片。"""
        import os
        import subprocess
        import tempfile

        try:
            ws = _effective_ws(getattr(req, "cwd", ""))
        except ValueError as e:
            return {"success": False, "exit_code": None, "stdout": "", "stderr": str(e)}
        os.makedirs(ws, exist_ok=True)
        before = _ws_snapshot(ws)
        timeout = max(1, min(req.timeout or 30, 120))
        fd, path = tempfile.mkstemp(suffix=".py", dir=ws)
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as f:
                f.write(req.code)
            try:
                proc = subprocess.run(
                    [sys.executable, path],
                    cwd=ws,
                    capture_output=True,
                    text=True,
                    timeout=timeout,
                )
                result = {
                    "success": proc.returncode == 0,
                    "exit_code": proc.returncode,
                    "stdout": proc.stdout[-8000:],
                    "stderr": proc.stderr[-4000:],
                }
            except subprocess.TimeoutExpired:
                result = {"success": False, "exit_code": None, "stdout": "", "stderr": f"执行超时（>{timeout}s）"}
            # 执行后快照 diff（排除临时脚本本身；此时 finally 尚未 unlink）
            after = _ws_snapshot(ws)
            temp_rel = os.path.relpath(path, ws)
            created = [k for k in after if k not in before and k != temp_rel]
            modified = [k for k in after if k in before and after[k] != before[k] and k != temp_rel]
            result["files"] = {"created": created, "modified": modified}
            return result
        finally:
            try:
                os.unlink(path)
            except OSError:
                pass

    @app.post("/run-shell")
    def run_shell(req: RunShellRequest):
        """通用 shell 执行（agent run_shell 工具，对标 Kimi Code Bash）：
        bash -lc 在共享沙箱工作区运行，容器即边界。输出截断，附带文件快照 diff。"""
        import os
        import subprocess

        try:
            ws = _effective_ws(getattr(req, "cwd", ""))
        except ValueError as e:
            return {"success": False, "exit_code": None, "stdout": "", "stderr": str(e)}
        os.makedirs(ws, exist_ok=True)
        before = _ws_snapshot(ws)
        timeout = max(1, min(req.timeout or 30, 300))
        command = (req.command or "").strip()
        if not command:
            return {"success": False, "exit_code": None, "stdout": "", "stderr": "command 不能为空"}
        try:
            proc = subprocess.run(
                ["bash", "-lc", command],
                cwd=ws,
                capture_output=True,
                text=True,
                timeout=timeout,
            )
            result = {
                "success": proc.returncode == 0,
                "exit_code": proc.returncode,
                "stdout": proc.stdout[-8000:],
                "stderr": proc.stderr[-4000:],
            }
        except subprocess.TimeoutExpired:
            result = {"success": False, "exit_code": None, "stdout": "", "stderr": f"执行超时（>{timeout}s）"}
        after = _ws_snapshot(ws)
        created = [k for k in after if k not in before]
        modified = [k for k in after if k in before and after[k] != before[k]]
        result["files"] = {"created": created, "modified": modified}
        return result

    @app.post("/fetch")
    def fetch(req: FetchRequest):
        """网页抓取（agent fetch_url 工具，对标 Kimi Code FetchURL）：
        仅 http/https，2MB 上限；HTML 粗略提取正文文本，其余文本类型原样截断。"""
        import re
        import urllib.request

        url = (req.url or "").strip()
        if not url.startswith(("http://", "https://")):
            return {"success": False, "error": "仅支持 http/https URL"}
        timeout = max(1, min(req.timeout or 15, 60))
        try:
            req_obj = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0 (XiaoTianQuant-Agent)"})
            with urllib.request.urlopen(req_obj, timeout=timeout) as resp:
                ctype = (resp.headers.get("Content-Type") or "").lower()
                data = resp.read(2 << 20)
        except Exception as e:  # noqa: BLE001 - 统一回传错误信息给模型
            return {"success": False, "error": f"抓取失败: {e}"}
        if "text/html" in ctype or "application/xhtml" in ctype:
            text = data.decode("utf-8", errors="replace")
            # 粗略正文提取：去脚本/样式/标签
            text = re.sub(r"(?is)<(script|style|noscript)[^>]*>.*?</\1>", " ", text)
            text = re.sub(r"(?s)<[^>]+>", " ", text)
            text = re.sub(r"\s+", " ", text).strip()
            return {"success": True, "url": url, "content_type": ctype, "text": text[:20000]}
        if ctype.startswith("text/") or "json" in ctype or "xml" in ctype or ctype == "":
            return {
                "success": True,
                "url": url,
                "content_type": ctype,
                "text": data.decode("utf-8", errors="replace")[:20000],
            }
        return {"success": False, "error": f"非文本内容（{ctype or 'unknown'}），不支持提取"}

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
