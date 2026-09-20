"""XiaoTianQuant 用户 Python 策略沙箱 worker（pystrat 运行时子进程）。

协议：stdin/stdout JSON-RPC 行协议（每行一个 JSON 对象，无 prompt）：
  请求:  {"id": 1, "method": "load|on_bar|ping", "params": {...}}
  响应:  {"id": 1, "ok": true, "result": {...}}
         {"id": 1, "ok": false, "error": "...", "traceback": "..."}

方法：
  load  {code, params, symbol, interval}  AST 安全校验 → 受限环境 exec 策略
        → 校验 STRATEGY_MANIFEST → 调 initialize(context)。返回规范化 manifest。
  on_bar {bar, state}  刷新 context.position/equity → 调 on_bar(context, bar)。
        返回 {actions, logs, prints}。回调异常作为 ok:false 返回（由 Go 侧
        按错误契约计数）。
  ping  存活探测。

安全：import 白名单（与 Go 侧 ValidateStatic 一致）、危险内建调用与双下划线
访问静态拒绝、受限 builtins、RLIMIT_AS 256MB、stdout 重定向（用户 print 进
prints 缓冲，不污染协议流）。单轮超时由 Go 侧强制（kill 子进程）。
"""

import ast
import json
import sys
import traceback
from types import MappingProxyType

MEMORY_LIMIT_MB = 256

ALLOWED_MODULES = {
    "math", "json", "datetime", "collections", "heapq",
    "itertools", "functools", "statistics",
}

UNSAFE_BUILTINS = {
    "eval", "exec", "compile", "open", "__import__", "getattr",
    "setattr", "delattr", "globals", "vars", "dir", "input",
    "breakpoint", "help", "quit", "exit", "copyright", "credits", "license",
}

SAFE_BUILTINS = {
    "abs", "all", "any", "bin", "bool", "bytearray", "bytes",
    "chr", "complex", "dict", "divmod", "enumerate", "filter",
    "float", "format", "frozenset", "hasattr", "hash", "hex",
    "int", "isinstance", "issubclass", "iter", "len", "list",
    "map", "max", "min", "next", "oct", "ord", "pow", "range",
    "repr", "reversed", "round", "set", "slice", "sorted", "str",
    "sum", "tuple", "type", "zip", "print",
    "Exception", "ValueError", "TypeError", "RuntimeError",
    "ArithmeticError", "LookupError", "IndexError", "KeyError",
    "AttributeError", "ZeroDivisionError", "AssertionError",
}

DANGEROUS_DUNDERS = {
    "__subclasses__", "__bases__", "__globals__", "__code__",
    "__func__", "__closure__", "__class__", "__dict__", "__mro__",
}

REAL_STDOUT = sys.stdout


# ── 静态安全校验（AST，与 Go 侧 ValidateStatic 双层防御） ──

class SecurityVisitor(ast.NodeVisitor):
    def __init__(self):
        self.errors = []

    def visit_Import(self, node):
        for alias in node.names:
            root = alias.name.split(".")[0]
            if root not in ALLOWED_MODULES:
                self.errors.append("禁止 import 模块 %r（白名单：%s）"
                                   % (alias.name, "/".join(sorted(ALLOWED_MODULES))))
        self.generic_visit(node)

    def visit_ImportFrom(self, node):
        if node.level and node.level > 0:
            self.errors.append("禁止相对导入")
        elif node.module:
            root = node.module.split(".")[0]
            if root not in ALLOWED_MODULES:
                self.errors.append("禁止 from %s import ...（白名单：%s）"
                                   % (node.module, "/".join(sorted(ALLOWED_MODULES))))
        self.generic_visit(node)

    def visit_Call(self, node):
        if isinstance(node.func, ast.Name) and node.func.id in UNSAFE_BUILTINS:
            self.errors.append("禁止调用 %s()" % node.func.id)
        self.generic_visit(node)

    def visit_Attribute(self, node):
        if node.attr in DANGEROUS_DUNDERS:
            self.errors.append("禁止访问双下划线属性 %s" % node.attr)
        self.generic_visit(node)


def validate_code_safety(code):
    try:
        tree = ast.parse(code)
    except SyntaxError as e:
        return ["语法错误: %s (line %s)" % (e.msg, e.lineno)]
    visitor = SecurityVisitor()
    visitor.visit(tree)
    return visitor.errors


# ── 受限执行环境 ──

def build_safe_builtins():
    real_import = __builtins__["__import__"] if isinstance(__builtins__, dict) else __builtins__.__import__

    def safe_import(name, globals=None, locals=None, fromlist=(), level=0):
        root = (name or "").split(".")[0]
        if root not in ALLOWED_MODULES:
            raise ImportError("模块 %r 不在白名单，禁止导入" % name)
        return real_import(name, globals, locals, fromlist, level)

    safe = {}
    for name in SAFE_BUILTINS:
        if name in ("print",):
            continue  # print 由 PrintSink 接管，输出进 prints 缓冲
        builtin = getattr(__builtins__, name, None) if not isinstance(__builtins__, dict) else __builtins__.get(name)
        if builtin is not None:
            safe[name] = builtin
    safe["__import__"] = safe_import
    return safe


class PrintSink:
    """接管 sys.stdout：用户 print 进入缓冲，随响应返回，不污染协议流。"""

    def __init__(self):
        self.buf = []

    def write(self, s):
        if s:
            self.buf.append(s)

    def flush(self):
        pass

    def drain(self):
        text = "".join(self.buf)
        self.buf = []
        return text


class StrategyContext:
    """暴露给策略的 context（docs/PYTHON_STRATEGY_API.md）。

    symbol/interval/params 只读（property 无 setter）；
    position/equity 每根 bar 前由 worker 刷新；跨回调自定义状态允许挂在
    context 任意属性上（freqtrade self.* 风格）。
    """

    def __init__(self, symbol, interval, params):
        self._symbol = symbol
        self._interval = interval
        self._params = MappingProxyType(dict(params))
        self._actions = []
        self._logs = []
        self.position = {}
        self.equity = 0.0

    @property
    def symbol(self):
        return self._symbol

    @property
    def interval(self):
        return self._interval

    @property
    def params(self):
        return self._params

    def buy(self, qty=None, amount=None, price=None):
        action = {"type": "buy"}
        if qty is not None:
            action["qty"] = float(qty)
        if amount is not None:
            action["amount"] = float(amount)
        if price is not None:
            action["price"] = float(price)
        if "qty" not in action and "amount" not in action:
            raise ValueError("buy() 需要 qty 或 amount 至少一个")
        if action.get("qty", 0) <= 0 and action.get("amount", 0) <= 0:
            raise ValueError("buy() 的数量必须为正")
        self._actions.append(action)

    def sell(self, qty=None, amount=None, price=None):
        action = {"type": "sell"}
        if qty is not None:
            action["qty"] = float(qty)
        if amount is not None:
            action["amount"] = float(amount)
        if price is not None:
            action["price"] = float(price)
        if "qty" not in action and "amount" not in action:
            raise ValueError("sell() 需要 qty 或 amount 至少一个")
        if action.get("qty", 0) <= 0 and action.get("amount", 0) <= 0:
            raise ValueError("sell() 的数量必须为正")
        self._actions.append(action)

    def log(self, msg):
        self._logs.append(str(msg))

    def set_stop_loss(self, pct):
        pct = float(pct)
        if pct <= 0 or pct >= 1:
            raise ValueError("set_stop_loss(pct) 要求 0<pct<1（如 0.05 表示 5%）")
        self._actions.append({"type": "set_stop_loss", "pct": pct})

    def set_take_profit(self, pct):
        pct = float(pct)
        if pct <= 0 or pct >= 1:
            raise ValueError("set_take_profit(pct) 要求 0<pct<1")
        self._actions.append({"type": "set_take_profit", "pct": pct})

    def close_position(self):
        self._actions.append({"type": "close_position"})

    def drain(self):
        actions = self._actions
        logs = self._logs
        self._actions = []
        self._logs = []
        return actions, logs


# ── manifest 校验 ──

REQUIRED_MANIFEST_KEYS = ("name", "symbol", "interval", "direction")
ALLOWED_INTERVALS = {"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d"}
ALLOWED_DIRECTIONS = {"long", "short", "both"}


def validate_manifest(manifest):
    errors = []
    if not isinstance(manifest, dict):
        return ["STRATEGY_MANIFEST 必须是 dict"]
    for key in REQUIRED_MANIFEST_KEYS:
        if key not in manifest or manifest[key] in (None, ""):
            errors.append("STRATEGY_MANIFEST 缺少必填字段 %r" % key)
    if errors:
        return errors
    if manifest["interval"] not in ALLOWED_INTERVALS:
        errors.append("STRATEGY_MANIFEST.interval=%r 不受支持" % manifest["interval"])
    if manifest["direction"] not in ALLOWED_DIRECTIONS:
        errors.append("STRATEGY_MANIFEST.direction=%r 必须是 long|short|both" % manifest["direction"])
    params = manifest.get("params", {})
    if params is None:
        params = {}
    if not isinstance(params, dict):
        errors.append("STRATEGY_MANIFEST.params 必须是 dict")
    risk = manifest.get("risk", {}) or {}
    if not isinstance(risk, dict):
        errors.append("STRATEGY_MANIFEST.risk 必须是 dict")
    else:
        for key in ("max_position_pct", "stop_loss_pct", "take_profit_pct"):
            value = risk.get(key, 0) or 0
            if not isinstance(value, (int, float)) or value < 0:
                errors.append("STRATEGY_MANIFEST.risk.%s 必须是非负数字" % key)
    return errors


# ── 运行时状态 ──

STATE = {
    "globals": None,
    "context": None,
    "manifest": None,
    "sink": None,
    "loaded": False,
}


def apply_memory_limit():
    try:
        import resource
        limit = MEMORY_LIMIT_MB * 1024 * 1024
        resource.setrlimit(resource.RLIMIT_AS, (limit, limit))
    except Exception:
        # Windows / 无权限时降级：文档注明内存限制依赖 POSIX RLIMIT_AS。
        pass


def handle_load(params):
    code = params.get("code") or ""
    if not code.strip():
        return None, ["策略代码为空"]
    errors = validate_code_safety(code)
    if errors:
        return None, errors

    overrides = params.get("params") or {}
    symbol = params.get("symbol") or ""
    interval = params.get("interval") or "15m"

    sink = PrintSink()
    g = {
        "__builtins__": build_safe_builtins(),
        "__name__": "__pystrat__",
    }
    old_stdout = sys.stdout
    sys.stdout = sink
    try:
        exec(compile(code, "<strategy>", "exec"), g)
    except Exception as e:
        sys.stdout = old_stdout
        return None, ["策略加载执行失败: %s: %s" % (type(e).__name__, e)]
    sys.stdout = old_stdout

    manifest = g.get("STRATEGY_MANIFEST")
    errors = validate_manifest(manifest)
    if errors:
        return None, errors

    merged = dict(manifest.get("params") or {})
    merged.update(overrides)

    on_bar_fn = g.get("on_bar")
    if not callable(on_bar_fn):
        return None, ["策略缺少 on_bar(context, bar) 回调"]

    context = StrategyContext(symbol, interval, merged)
    init_fn = g.get("initialize")
    if callable(init_fn):
        sys.stdout = sink
        try:
            init_fn(context)
        except Exception as e:
            sys.stdout = old_stdout
            return None, ["initialize(context) 抛异常: %s: %s" % (type(e).__name__, e)]
        sys.stdout = old_stdout

    STATE["globals"] = g
    STATE["context"] = context
    STATE["manifest"] = manifest
    STATE["sink"] = sink
    STATE["loaded"] = True
    return manifest, None


def handle_on_bar(params):
    if not STATE["loaded"]:
        return None, ["策略未加载"]
    bar = params.get("bar") or {}
    state = params.get("state") or {}
    context = STATE["context"]
    sink = STATE["sink"]

    position = state.get("position") or {}
    context.position = dict(position) if isinstance(position, dict) else {}
    try:
        context.equity = float(state.get("equity") or 0)
    except (TypeError, ValueError):
        context.equity = 0.0

    on_bar_fn = STATE["globals"].get("on_bar")
    old_stdout = sys.stdout
    sys.stdout = sink
    try:
        on_bar_fn(context, dict(bar))
    except Exception as e:
        sys.stdout = old_stdout
        tb = traceback.format_exc()
        return None, ["on_bar 抛异常: %s: %s" % (type(e).__name__, e), tb]
    sys.stdout = old_stdout

    actions, logs = context.drain()
    prints = sink.drain()
    return {"actions": actions, "logs": logs, "prints": prints.splitlines()}, None


HANDLERS = {
    "load": handle_load,
    "on_bar": handle_on_bar,
}


def main():
    apply_memory_limit()
    for raw in sys.stdin:
        raw = raw.strip()
        if not raw:
            continue
        try:
            req = json.loads(raw)
        except ValueError:
            REAL_STDOUT.write(json.dumps({"id": 0, "ok": False, "error": "bad json"}) + "\n")
            REAL_STDOUT.flush()
            continue
        req_id = req.get("id", 0)
        method = req.get("method", "")
        if method == "ping":
            result, errors = {"pong": True}, None
        else:
            handler = HANDLERS.get(method)
            if handler is None:
                result, errors = None, ["unknown method %r" % method]
            else:
                result, errors = handler(req.get("params") or {})
        if errors:
            resp = {"id": req_id, "ok": False, "error": errors[0]}
            if len(errors) > 1 and errors[1]:
                resp["traceback"] = errors[1]
        else:
            resp = {"id": req_id, "ok": True, "result": result}
        REAL_STDOUT.write(json.dumps(resp) + "\n")
        REAL_STDOUT.flush()


if __name__ == "__main__":
    main()
