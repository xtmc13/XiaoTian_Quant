"""
Strategy Compiler — Compiles JSON strategy configuration into executable Python code.

Patterns adapted from QuantDinger's strategy_compiler.py.
Supports: EMA, MACD, RSI, Bollinger, SuperTrend, KDJ, MA cross strategies.
"""

from typing import Dict, Any, List


class StrategyCompiler:
    """Compile JSON strategy config into executable Python backtest code."""

    def compile(self, config: Dict[str, Any]) -> str:
        """Main entry point: config dict → Python source code string."""
        name = config.get("name", "Generated Strategy")
        entry_rules = config.get("entry_rules", [])
        position_config = config.get("position_config", {})
        pyramiding_rules = config.get("pyramiding_rules", {})
        risk_management = config.get("risk_management", {})
        exit_rules = config.get("exit_rules", [])

        code = self._header(name)
        code += self._parameters(position_config, pyramiding_rules, risk_management)
        code += self._indicators(entry_rules)
        code += self._entry_logic(entry_rules)
        code += self._exit_logic(exit_rules)
        code += self._core_loop(position_config, pyramiding_rules, risk_management)
        code += self._output(name, entry_rules)
        return code

    def _header(self, name: str) -> str:
        return f'''"""
Generated Strategy: {name}
Auto-compiled by XiaoTianQuant Strategy Compiler.
"""
import pandas as pd
import numpy as np

def get_val(arr, i, default=0):
    """Safe array access."""
    if i < 0 or i >= len(arr):
        return default
    return arr[i]

def run_strategy(df: pd.DataFrame, params: dict = None) -> dict:
    """Execute the strategy on the given DataFrame."""
    if params is None:
        params = {{}}

'''

    def _parameters(self, pos: dict, pyr: dict, risk: dict) -> str:
        initial_size = pos.get("initial_size_pct", 10) / 100.0
        leverage = pos.get("leverage", 1)
        max_pyramiding = pos.get("max_pyramiding", 0)
        pyr_enabled = pyr.get("enabled", False)
        add_size = pyr.get("size_pct", 0) / 100.0 if pyr_enabled else 0
        add_threshold = pyr.get("value", 0) / 100.0
        stop_loss = risk.get("stop_loss", {})
        sl_pct = stop_loss.get("value", 0) / 100.0 if stop_loss.get("enabled") else 0.0
        trailing = risk.get("trailing_stop", {})
        ts_activation = trailing.get("activation_profit", 0) / 100.0 if trailing.get("enabled") else 0.0
        ts_callback = trailing.get("callback_pct", 0) / 100.0 if trailing.get("enabled") else 0.0
        tp = risk.get("take_profit", {})
        tp_pct = tp.get("value", 0) / 100.0 if tp.get("enabled") else 0.0

        return f"""    # ── Parameters ──
    initial_position_pct = {initial_size}
    leverage = {leverage}
    max_pyramiding = {max_pyramiding}
    add_position_pct = {add_size}
    add_threshold_pct = {add_threshold}
    stop_loss_pct = {sl_pct}
    take_profit_pct = {tp_pct}
    trailing_activation = {ts_activation}
    trailing_callback = {ts_callback}

"""

    def _indicators(self, rules: List[dict]) -> str:
        code = "    # ── Indicators ──\n"
        calculated = set()

        for rule in rules:
            ind = rule.get("indicator", "")
            params = rule.get("params", {})

            if ind == "ema":
                p = params.get("period", 20)
                key = f"ema_{p}"
                if key not in calculated:
                    code += f"    df['ema_{p}'] = df['close'].ewm(span={p}, adjust=False).mean()\n"
                    calculated.add(key)

            elif ind == "sma":
                p = params.get("period", 20)
                key = f"sma_{p}"
                if key not in calculated:
                    code += f"    df['sma_{p}'] = df['close'].rolling(window={p}).mean()\n"
                    calculated.add(key)

            elif ind == "rsi":
                p = params.get("period", 14)
                key = f"rsi_{p}"
                if key not in calculated:
                    code += self._rsi_codegen(p, key)
                    calculated.add(key)

            elif ind == "macd":
                f = params.get("fast", 12)
                s = params.get("slow", 26)
                sig = params.get("signal", 9)
                key = f"macd_{f}_{s}_{sig}"
                if key not in calculated:
                    code += f"""    exp1 = df['close'].ewm(span={f}, adjust=False).mean()
    exp2 = df['close'].ewm(span={s}, adjust=False).mean()
    df['macd_{f}_{s}_{sig}_line'] = exp1 - exp2
    df['macd_{f}_{s}_{sig}_signal'] = df['macd_{f}_{s}_{sig}_line'].ewm(span={sig}, adjust=False).mean()
    df['macd_{f}_{s}_{sig}_hist'] = df['macd_{f}_{s}_{sig}_line'] - df['macd_{f}_{s}_{sig}_signal']
"""
                    calculated.add(key)

            elif ind == "bollinger":
                p = params.get("period", 20)
                sd = params.get("std_dev", 2.0)
                key = f"bb_{p}_{sd}"
                if key not in calculated:
                    code += f"""    sma = df['close'].rolling(window={p}).mean()
    std = df['close'].rolling(window={p}).std()
    df['bb_upper'] = sma + ({sd} * std)
    df['bb_lower'] = sma - ({sd} * std)
    df['bb_mid'] = sma
"""
                    calculated.add(key)

            elif ind == "atr":
                p = params.get("period", 14)
                key = f"atr_{p}"
                if key not in calculated:
                    code += f"""    high, low, close = df['high'], df['low'], df['close']
    tr = pd.concat([
        high - low,
        (high - close.shift()).abs(),
        (low - close.shift()).abs()
    ], axis=1).max(axis=1)
    df['atr'] = tr.ewm(alpha=1/{p}, adjust=False).mean()
"""
                    calculated.add(key)

            elif ind == "kdj":
                p = params.get("period", 9)
                sig_p = params.get("signal_period", 3)
                key = f"kdj_{p}_{sig_p}"
                if key not in calculated:
                    code += self._kdj_codegen(p, sig_p)
                    calculated.add(key)

        return code + "\n"

    def _rsi_codegen(self, period: int, key: str) -> str:
        # Wilder RSI: initial SMA seed for first N bars, then Wilder EMA thereafter
        return f"""    delta = df['close'].diff()
    gain = delta.clip(lower=0)
    loss = -delta.clip(upper=0)
    avg_gain = gain.copy()
    avg_loss = loss.copy()
    avg_gain.iloc[:{period}] = gain.iloc[:{period}].mean()
    avg_loss.iloc[:{period}] = loss.iloc[:{period}].mean()
    for i in range({period}, len(avg_gain)):
        avg_gain.iloc[i] = (avg_gain.iloc[i-1] * ({period}-1) + gain.iloc[i]) / {period}
        avg_loss.iloc[i] = (avg_loss.iloc[i-1] * ({period}-1) + loss.iloc[i]) / {period}
    rs = avg_gain / avg_loss.replace(0, 1e-10)
    df['{key}'] = 100 - (100 / (1 + rs))
"""

    def _kdj_codegen(self, period: int, signal: int) -> str:
        # CN terminal standard: K/D=50 seed + explicit loop matching 同花顺/东方财富
        return f"""    low_n = df['low'].rolling(window={period}).min()
    high_n = df['high'].rolling(window={period}).max()
    rsv = (df['close'] - low_n) / (high_n - low_n + 1e-10) * 100
    k = [50.0] * len(df)
    d = [50.0] * len(df)
    alpha_k = 1.0 / {signal}
    alpha_d = 1.0 / {signal}
    for i in range({period}, len(df)):
        k[i] = rsv.iloc[i] * alpha_k + k[i-1] * (1 - alpha_k)
        d[i] = k[i] * alpha_d + d[i-1] * (1 - alpha_d)
    df['kdj_k'] = k
    df['kdj_d'] = d
    df['kdj_j'] = [3 * k[i] - 2 * d[i] for i in range(len(k))]
"""

    def _entry_logic(self, rules: List[dict]) -> str:
        code = "    # ── Entry Signals ──\n"
        code += "    df['entry_long'] = False\n    df['entry_short'] = False\n"

        conditions_long = []
        conditions_short = []

        for rule in rules:
            ind = rule.get("indicator", "")
            params = rule.get("params", {})
            operator = rule.get("operator", "")
            direction = rule.get("direction", "long")

            if ind == "ema":
                p = params.get("period", 20)
                col = f"df['ema_{p}']"
                cond = self._operator_to_condition(operator, col)
                if direction in ("long", "both"):
                    conditions_long.append(cond)
                if direction in ("short", "both"):
                    conditions_short.append(f"not ({cond})")

            elif ind == "rsi":
                p = params.get("period", 14)
                thresh = params.get("threshold", 30)
                col = f"df['rsi_{p}']"
                if operator == "<":
                    conditions_long.append(f"({col} < {thresh})")
                    conditions_short.append(f"({col} > {100 - thresh})")
                elif operator == ">":
                    conditions_long.append(f"({col} > {thresh})")
                    conditions_short.append(f"({col} < {100 - thresh})")

            elif ind == "macd":
                f = params.get("fast", 12)
                s = params.get("slow", 26)
                sig = params.get("signal", 9)
                ml = f"df['macd_{f}_{s}_{sig}_line']"
                ms = f"df['macd_{f}_{s}_{sig}_signal']"
                mls = f"df['macd_{f}_{s}_{sig}_line'].shift(1)"
                mss = f"df['macd_{f}_{s}_{sig}_signal'].shift(1)"
                op = rule.get("operator", "")
                if op in ("cross_up", "diff_gt_dea", ""):
                    cond_long = f"({ml} > {ms}) & ({mls} <= {mss})"
                elif op == "cross_down":
                    cond_long = f"({ml} < {ms}) & ({mls} >= {mss})"
                else:
                    cond_long = f"({ml} > {ms})"
                cond_short = f"({ml} < {ms}) & ({mls} >= {mss})"
                conditions_long.append(f"({cond_long})")
                conditions_short.append(f"({cond_short})")

            elif ind == "bollinger":
                if operator == "lower_breakout":
                    conditions_long.append("(df['close'] <= df['bb_lower'])")
                elif operator == "upper_breakout":
                    conditions_short.append("(df['close'] >= df['bb_upper'])")
                elif operator == "mid_cross_up":
                    conditions_long.append("(df['close'] > df['bb_mid']) & (df['close'].shift(1) <= df['bb_mid'].shift(1))")

            elif ind == "kdj":
                cond_long = "(df['kdj_k'] > df['kdj_d']) & (df['kdj_k'].shift(1) <= df['kdj_d'].shift(1))"
                cond_short = "(df['kdj_k'] < df['kdj_d']) & (df['kdj_k'].shift(1) >= df['kdj_d'].shift(1))"
                conditions_long.append(f"({cond_long})")
                conditions_short.append(f"({cond_short})")

        if conditions_long:
            code += f"    df['entry_long'] = {' & '.join(conditions_long)}\n"
        if conditions_short:
            code += f"    df['entry_short'] = {' & '.join(conditions_short)}\n"

        return code + "\n"

    def _operator_to_condition(self, op: str, col: str) -> str:
        if op == "price_above":
            return f"(df['close'] > {col})"
        elif op == "price_below":
            return f"(df['close'] < {col})"
        elif op == "cross_up":
            return f"(df['close'] > {col}) & (df['close'].shift(1) <= {col}.shift(1))"
        elif op == "cross_down":
            return f"(df['close'] < {col}) & (df['close'].shift(1) >= {col}.shift(1))"
        elif op == "golden_cross":
            return f"({col} > df['close']) & ({col}.shift(1) <= df['close'].shift(1))"
        return f"(df['close'] > {col})"

    def _exit_logic(self, rules: List[dict]) -> str:
        if not rules:
            return "    # ── Exit Signals (using built-in risk management) ──\n\n"
        code = "    # ── Exit Signals ──\n"
        code += "    df['exit_long'] = False\n    df['exit_short'] = False\n"

        conditions_long = []
        conditions_short = []

        for rule in rules:
            ind = rule.get("indicator", "")
            params = rule.get("params", {})
            operator = rule.get("operator", "")
            direction = rule.get("direction", "long")

            if ind == "rsi":
                p = params.get("period", 14)
                thresh = params.get("threshold", 70)
                col = f"df['rsi_{p}']"
                if direction in ("long", "both"):
                    conditions_long.append(f"({col} > {thresh})")
                if direction in ("short", "both"):
                    conditions_short.append(f"({col} < {100 - thresh})")

            elif ind == "ema":
                p = params.get("period", 20)
                col = f"df['ema_{p}']"
                if direction in ("long", "both"):
                    conditions_long.append(f"(df['close'] < {col})")
                if direction in ("short", "both"):
                    conditions_short.append(f"(df['close'] > {col})")

        if conditions_long:
            code += f"    df['exit_long'] = {' | '.join(conditions_long)}\n"
        if conditions_short:
            code += f"    df['exit_short'] = {' | '.join(conditions_short)}\n"

        return code + "\n"

    def _core_loop(self, pos: dict, pyr: dict, risk: dict) -> str:
        return """    # ── Backtest Loop ──
    n = len(df)
    position = 0  # 0=none, 1=long, -1=short
    position_count = 0
    avg_entry_price = 0.0
    last_add_price = 0.0
    highest_price = 0.0
    entry_bar = 0

    trades = []
    equity = []
    initial_capital = 100000.0
    capital = initial_capital
    quantity = 0.0

    close_arr = df['close'].values
    high_arr = df['high'].values
    low_arr = df['low'].values

    for i in range(n):
        cur_close = close_arr[i]
        cur_high = high_arr[i]
        cur_low = low_arr[i]

        if position == 1:  # Long
            if cur_high > highest_price:
                highest_price = cur_high

            profit_pct = (highest_price - avg_entry_price) / avg_entry_price
            cur_profit_pct = (cur_close - avg_entry_price) / avg_entry_price

            # Stop Loss
            if stop_loss_pct > 0:
                loss_pct = (avg_entry_price - cur_low) / avg_entry_price
                if loss_pct >= stop_loss_pct:
                    exit_price = avg_entry_price * (1 - stop_loss_pct)
                    trades.append({'bar': i, 'side': 'sell', 'price': exit_price,
                        'qty': quantity, 'reason': 'stop_loss',
                        'pnl': (exit_price - avg_entry_price) * quantity})
                    capital += exit_price * quantity * 0.999
                    position = 0; quantity = 0; continue

            # Take Profit
            if take_profit_pct > 0 and cur_profit_pct >= take_profit_pct:
                trades.append({'bar': i, 'side': 'sell', 'price': cur_close,
                    'qty': quantity, 'reason': 'take_profit',
                    'pnl': (cur_close - avg_entry_price) * quantity})
                capital += cur_close * quantity * 0.999
                position = 0; quantity = 0; continue

            # Trailing Stop
            if trailing_activation > 0 and profit_pct >= trailing_activation:
                drawdown = (highest_price - cur_close) / avg_entry_price
                if drawdown >= trailing_callback:
                    trades.append({'bar': i, 'side': 'sell', 'price': cur_close,
                        'qty': quantity, 'reason': 'trailing_stop',
                        'pnl': (cur_close - avg_entry_price) * quantity})
                    capital += cur_close * quantity * 0.999
                    position = 0; quantity = 0; continue

            # Signal exit
            if df['exit_long'].values[i] if 'exit_long' in df.columns else False:
                trades.append({'bar': i, 'side': 'sell', 'price': cur_close,
                    'qty': quantity, 'reason': 'signal_exit',
                    'pnl': (cur_close - avg_entry_price) * quantity})
                capital += cur_close * quantity * 0.999
                position = 0; quantity = 0; continue

        elif position == -1:  # Short
            if highest_price == 0:
                highest_price = avg_entry_price
            if cur_low < highest_price:
                highest_price = cur_low

            profit_pct = (avg_entry_price - highest_price) / avg_entry_price
            cur_profit_pct = (avg_entry_price - cur_close) / avg_entry_price

            # Stop Loss
            if stop_loss_pct > 0:
                loss_pct = (cur_high - avg_entry_price) / avg_entry_price
                if loss_pct >= stop_loss_pct:
                    exit_price = avg_entry_price * (1 + stop_loss_pct)
                    trades.append({'bar': i, 'side': 'buy', 'price': exit_price,
                        'qty': quantity, 'reason': 'stop_loss',
                        'pnl': (avg_entry_price - exit_price) * quantity})
                    capital -= exit_price * quantity * 1.001
                    position = 0; quantity = 0; continue

            # Trailing Stop
            if trailing_activation > 0 and profit_pct >= trailing_activation:
                drawdown = (cur_close - highest_price) / avg_entry_price
                if drawdown >= trailing_callback:
                    trades.append({'bar': i, 'side': 'buy', 'price': cur_close,
                        'qty': quantity, 'reason': 'trailing_stop',
                        'pnl': (avg_entry_price - cur_close) * quantity})
                    capital -= cur_close * quantity * 1.001
                    position = 0; quantity = 0; continue

            # Signal exit
            if df['exit_short'].values[i] if 'exit_short' in df.columns else False:
                trades.append({'bar': i, 'side': 'buy', 'price': cur_close,
                    'qty': quantity, 'reason': 'signal_exit',
                    'pnl': (avg_entry_price - cur_close) * quantity})
                capital -= cur_close * quantity * 1.001
                position = 0; quantity = 0; continue

        else:  # No position
            if df['entry_long'].values[i]:
                position = 1
                avg_entry_price = cur_close
                last_add_price = cur_close
                highest_price = cur_close
                quantity = (capital * initial_position_pct) / cur_close
                capital -= cur_close * quantity * 1.001
                trades.append({'bar': i, 'side': 'buy', 'price': cur_close,
                    'qty': quantity, 'reason': 'entry_long',
                    'pnl': 0})

            elif df['entry_short'].values[i]:
                position = -1
                avg_entry_price = cur_close
                last_add_price = cur_close
                highest_price = cur_close
                quantity = (capital * initial_position_pct) / cur_close
                capital += cur_close * quantity * 0.999  # Receive sale proceeds minus fee
                trades.append({'bar': i, 'side': 'sell', 'price': cur_close,
                    'qty': quantity, 'reason': 'entry_short',
                    'pnl': 0})

        equity.append({
            'bar': i,
            'time': int(df.index[i].timestamp() * 1000) if hasattr(df.index[i], 'timestamp') else i,
            'equity': capital + (quantity * cur_close if position == 1 else
                                 quantity * (avg_entry_price - cur_close) if position == -1 else 0)
        })

    return {'trades': trades, 'equity': equity, 'final_capital': capital}

"""

    def _output(self, name: str, rules: List[dict]) -> str:
        return f"""
# ── Execute ──
result = run_strategy(df)
output = {{
    'name': '{name}',
    'trades': result['trades'],
    'equity_curve': result['equity'],
    'final_equity': result['final_capital'],
}}
"""


# Convenience function
def compile_strategy(config: Dict[str, Any]) -> str:
    """Compile a JSON strategy configuration into executable Python code."""
    return StrategyCompiler().compile(config)
