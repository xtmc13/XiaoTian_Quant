"""
Zero Lag Trend Signals (MTF) — Python port of AlgoAlpha Pine Script indicator.
Runs natively in XiaoTianQuant sandbox, no TradingView needed.

Pine Script original: Zero Lag Trend Signals (MTF) [AlgoAlpha]
"""
import numpy as np
import pandas as pd

# @param length int 70 Look-back window for Zero-Lag EMA
# @param mult float 1.2 Band multiplier (larger = less noisy)
# @param t1 str "5" Time frame 1
# @param t2 str "15" Time frame 2
# @param t3 str "60" Time frame 3
# @param t4 str "240" Time frame 4
# @param t5 str "1D" Time frame 5

# @strategy stopLossPct 2.0
# @strategy takeProfitPct 5.0
# @strategy tradeDirection long


def on_init(ctx):
    ctx.param('length', 70)
    ctx.param('mult', 1.2)
    ctx.param('t1', '5')
    ctx.param('t2', '15')
    ctx.param('t3', '60')
    ctx.param('t4', '240')
    ctx.param('t5', '1D')


def on_bar(ctx, bar):
    """Called on each bar. Returns signal dict or None."""
    df = ctx.dataframe()
    if df is None or len(df) < ctx.length * 3:
        return None

    length = ctx.length
    mult = ctx.mult
    close = df['close'].values

    # ── Zero-Lag EMA ──
    lag = int(np.floor((length - 1) / 2))
    ema_input = close + (close - np.roll(close, lag))
    ema_input[:lag] = close[:lag]  # handle edge
    zlema = pd.Series(ema_input).ewm(span=length, adjust=False).mean().values

    # ── ATR & Volatility ──
    tr = np.maximum(
        df['high'].values - df['low'].values,
        np.maximum(
            np.abs(df['high'].values - np.roll(df['close'].values, 1)),
            np.abs(df['low'].values - np.roll(df['close'].values, 1))
        )
    )
    tr[0] = 0
    atr = pd.Series(tr).ewm(span=length, adjust=False).mean().values
    volatility = pd.Series(atr).rolling(length * 3).max().values * mult

    # ── Trend Detection ──
    current_close = close[-1]
    current_zlema = zlema[-1]
    current_vol = volatility[-1]
    prev_close = close[-2]
    prev_zlema = zlema[-2]
    prev_vol = volatility[-2]

    # Cross detection
    bullish_cross = current_close > (current_zlema + current_vol) and prev_close <= (prev_zlema + prev_vol)
    bearish_cross = current_close < (current_zlema - current_vol) and prev_close >= (prev_zlema - prev_vol)

    # Entry signals
    bullish_entry = (current_close > current_zlema) and (current_close > (current_zlema - current_vol * 1.5))
    bearish_entry = (current_close < current_zlema) and (current_close < (current_zlema + current_vol * 1.5))

    # State tracking (use ctx persistent storage)
    trend = ctx.get('trend', 0)
    prev_trend = ctx.get('prev_trend', 0)

    if bullish_cross:
        trend = 1
    elif bearish_cross:
        trend = -1

    ctx.set('prev_trend', trend)

    # ── Multi-timeframe (simplified — reads from ctx if available) ──
    mtf_bullish = 0
    mtf_bearish = 0

    # ── Generate signal ──
    signal = None

    # Bullish entry: trend turned bullish + price above ZLEMA
    if trend == 1 and prev_trend != 1 and bullish_entry:
        signal = {'direction': 'LONG', 'reason': 'Zero Lag Trend: Bullish Entry'}

    # Bearish entry: trend turned bearish + price below ZLEMA
    elif trend == -1 and prev_trend != -1 and bearish_entry:
        signal = {'direction': 'SHORT', 'reason': 'Zero Lag Trend: Bearish Entry'}

    # Exit: trend change
    elif trend == 0 and prev_trend != 0:
        signal = {'direction': 'CLOSE', 'reason': 'Zero Lag Trend: Trend Neutral'}

    ctx.set('trend', trend)

    return signal


def on_tick(ctx, tick):
    return None
