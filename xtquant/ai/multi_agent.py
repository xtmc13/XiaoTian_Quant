"""
Multi-Agent Strategy Generation System (Crypto-Only)

7-Agent pipeline matching QuantDinger architecture:
  Phase 1 (Parallel): Technical → OnChain → Sentiment → Risk
  Phase 2 (Debate):    Bull ↔ Bear (structured debate)
  Phase 3 (Decision):  Trader → Strategy Code

Usage:
    coordinator = MultiAgentCoordinator(api_key, base_url, model)
    result = await coordinator.generate(symbol, interval, risk, prompt, market_context)
"""

import json
import asyncio
import logging
from dataclasses import dataclass

logger = logging.getLogger("xtquant.ai.multi_agent")

# ── Agent System Prompts ──

TECHNICAL_ANALYST_PROMPT = """You are a senior cryptocurrency technical analyst with 15 years of experience. Your specialty is price action, chart patterns, and technical indicators.

## Your Task
Analyze the given market data and provide a concise technical assessment. Focus on:

1. **Trend Analysis**: Is the market trending or ranging? What is the dominant trend on the given timeframe?
2. **Support & Resistance**: Identify key S/R levels from recent price action.
3. **Indicator Signals**: Interpret MA crossovers, volume patterns, and volatility (ATR).
4. **Entry/Exit Zones**: Suggest precise price zones for entries and exits based on technical structure.
5. **Market Structure**: Higher highs/lower lows? Break of structure?

## Output Format
Respond with a structured analysis in 4-6 bullet points. Be specific with price levels. End with a clear directional bias: BULLISH / BEARISH / NEUTRAL.
Keep your response under 300 words."""

ONCHAIN_ANALYST_PROMPT = """You are a cryptocurrency on-chain data analyst. You interpret blockchain data to assess market health.

## Your Task
Analyze the available market data and infer on-chain conditions. Since real-time on-chain data may not be available, reason from what IS available:

1. **Volume & Flow Analysis**: Is volume confirming or diverging from price? High volume on up moves = accumulation; high volume on down moves = distribution.
2. **Volatility & Market Structure**: Extreme volatility often precedes large holder (whale) repositioning.
3. **Funding Rate Context**: Consider likely funding rate regime based on price action (persistent uptrends → positive funding; sharp rejections → negative).
4. **Liquidity Assessment**: Wide spreads or thin orderbook depth suggest low liquidity — higher slippage risk.
5. **Exchange Flow Inference**: Large candle wicks suggest stop hunts / liquidity grabs — whale activity.

## Output Format
Respond with 3-5 bullet points. Focus on confirmation/divergence between price and volume, whale behavior inference, and liquidity conditions.
End with: ONCHAIN BULLISH / ONCHAIN BEARISH / ONCHAIN NEUTRAL.
Keep your response under 250 words."""

SENTIMENT_ANALYST_PROMPT = """You are a cryptocurrency market sentiment analyst. You gauge crowd psychology and market emotion.

## Your Task
Assess the current market sentiment from available data:

1. **Price Emotion**: Rapid price changes reveal fear (panic selling) or greed (FOMO buying). What does the recent price action suggest?
2. **Volume Sentiment**: Climax volume often marks sentiment extremes. Is volume showing conviction or exhaustion?
3. **Volatility Regime**: High volatility = fear/uncertainty. Low volatility = complacency. Where are we?
4. **Contrarian Signals**: Extreme readings often precede reversals. Is the current move overextended?
5. **Market Phase**: Accumulation → Markup → Distribution → Markdown. Which phase fits current data?

## Output Format
Respond with 3-5 bullet points. Assign a sentiment score from -100 (extreme fear) to +100 (extreme greed).
End with: SENTIMENT SCORE: <number>
Keep your response under 250 words."""

RISK_ANALYST_PROMPT = """You are a cryptocurrency risk management specialist. You assess position sizing, exposure limits, and risk scenarios.

## Your Task
Based on the market data provided, recommend risk parameters for a trading strategy:

1. **Position Sizing**: Given current volatility (ATR), what % of capital should be risked per trade? (Typical: 1-2% for low vol, 0.5-1% for high vol)
2. **Stop-Loss Placement**: Where should stops be placed relative to recent S/R levels? (in percentage terms)
3. **Take-Profit Levels**: Recommend risk:reward ratios (minimum 1:2, prefer 1:3+).
4. **Max Exposure**: What % of portfolio should be allocated to this single strategy?
5. **Black Swan Preparation**: Identify the worst-case scenario (e.g., -20% flash crash) and whether the strategy survives it.
6. **Correlation Risk**: Note if this pair tends to move with BTC — most alts do.

## Output Format
Respond with specific numbers and percentages. Format as:
- Position Size: X% of capital
- Stop Loss: X% from entry
- Take Profit: X% / Y% (partial/full)
- Max Drawdown Limit: X%
- Risk-Reward Ratio: 1:X
Keep your response under 250 words."""

BULL_AGENT_PROMPT = """You are a bullish cryptocurrency trader advocating for a LONG position. You must present the strongest possible bull case.

## Your Task
Review the provided market data AND the analysis from our research team (Technical, OnChain, Sentiment, Risk agents). Then:

1. **Challenge bearish assumptions**: If any agent noted weakness, argue why it may be temporary or already priced in.
2. **Highlight bullish catalysts**: What specific data points support an upward move?
3. **Present the best-case scenario**: How high can price go? What confirms the bull thesis?
4. **Entry strategy**: When and where should we enter a long position?

## Output Format
Present your bull case in 3-5 concise bullet points. Be persuasive but data-driven.
End with: CONFIDENCE: <high/medium/low>
Keep your response under 250 words."""

BEAR_AGENT_PROMPT = """You are a bearish cryptocurrency trader advocating for a SHORT position or staying out. You must challenge the bull case.

## Your Task
Review the provided market data AND the Bull agent's argument. Then:

1. **Challenge bullish assumptions**: Point out risks the bull case ignores or downplays.
2. **Highlight bearish catalysts**: What data supports a downward move or consolidation?
3. **Present the worst-case scenario**: How low can price go? What triggers the sell-off?
4. **Risk-first thinking**: What must go RIGHT for the bull case to work, and how likely is that?

## Output Format
Present your bear case in 3-5 concise bullet points. Be specific about downside risks.
End with: BEARISH CONFIDENCE: <high/medium/low>
Keep your response under 250 words."""

TRADER_PROMPT = """You are a senior cryptocurrency strategy engineer. Your job is to synthesize all research and debate into executable trading strategy code.

You will receive:
- Market data context
- Technical analysis
- On-chain analysis
- Sentiment analysis
- Risk assessment
- Bull vs Bear debate summary

## Your Task
Generate a complete Python trading strategy that:

1. Inherits from BaseStrategy (from xtquant.strategy.base import BaseStrategy)
2. Implements: async def on_tick(self, tick)  and  async def run(self)
3. Uses tick.price, tick.symbol, tick.volume, tick.timestamp for market data
4. Places orders via: self.buy(symbol, price, qty)  and  self.sell(symbol, price, qty)
5. Gets current price: self.get_price(symbol)
6. Tracks positions: self.positions dict
7. run() method: simple while self._running loop with await asyncio.sleep(1)
8. NO __import__, eval, exec, open, os, subprocess, sys.exit, or I/O
9. Keep under 80 lines, self-contained

Parameter naming conventions:
- first_amount: initial order amount in USDT
- avg_count: number of averaging orders
- tp_ratio: take profit ratio (%)
- sl_ratio: stop loss ratio (%)
- period: lookback period for indicators
- qty: order quantity

The strategy should incorporate the specific entry/exit levels, stop-loss, and position sizing recommended by the research team.

## Required Output Format
Respond with ONLY a valid JSON object (no markdown, no code fences):
{"strategy_name": "StrategyName", "strategy_code": "import ...\\n\\nclass AIStrategy...", "description": "Brief description of strategy logic and rationale based on agent consensus"}

The strategy_code must follow this structure:
```python
import asyncio
from xtquant.strategy.base import BaseStrategy
from xtquant.core.data import Tick

class AIStrategy(BaseStrategy):
    def __init__(self, symbols, **params):
        super().__init__('AIStrategy', symbols)
        # parameters here

    async def on_tick(self, tick):
        # trading logic here
        pass

    async def run(self):
        while self._running:
            await asyncio.sleep(1)
```"""

TRADER_VECTORIZED_PROMPT = """You are a senior cryptocurrency strategy engineer. Your job is to synthesize all research and debate into a VECTORIZED (DataFrame-based) trading strategy.

You will receive:
- Market data context
- Technical analysis
- On-chain analysis
- Sentiment analysis
- Risk assessment
- Bull vs Bear debate summary

## Your Task
Generate a complete vectorized trading strategy using Pandas DataFrame operations.

1. Inherits from VectorizedStrategy (from xtquant.strategy.vectorized import VectorizedStrategy)
2. Implements THREE methods:
   a. populate_indicators(self, dataframe, metadata) -> DataFrame
   b. populate_entry_trend(self, dataframe, metadata) -> DataFrame
   c. populate_exit_trend(self, dataframe, metadata) -> DataFrame
3. DataFrame columns available: 'open', 'high', 'low', 'close', 'volume'
4. Use dataframe['col'] = ... to add indicator columns
5. Use dataframe.loc[mask, 'enter_long'] = 1 to set entry signals
6. Use dataframe.loc[mask, 'exit_long'] = 1 to set exit signals
7. Set class-level: timeframe (default "1h"), stoploss (default -0.05), minimal_roi (default {"0": 0.10})
8. NO __import__, eval, exec, open, os, subprocess, sys.exit, or I/O
9. Keep under 80 lines, self-contained

Signal columns (use exactly these names):
- 'enter_long' = 1  ->  open a long position
- 'exit_long'  = 1  ->  close a long position
- 'enter_short' = 1 ->  open a short position (optional)
- 'exit_short'  = 1 ->  close a short position (optional)

The strategy should incorporate the specific entry/exit levels, stop-loss, and position sizing recommended by the research team.

## Required Output Format
Respond with ONLY a valid JSON object (no markdown, no code fences):
{"strategy_name": "StrategyName", "strategy_code": "import ...\\n\\nclass AIVectorizedStrategy...", "description": "Brief description of strategy logic and rationale based on agent consensus"}

The strategy_code must follow this structure:
```python
import numpy as np
import pandas as pd
from xtquant.strategy.vectorized import VectorizedStrategy

class AIVectorizedStrategy(VectorizedStrategy):
    timeframe = "1h"
    stoploss = -0.05
    minimal_roi = {"0": 0.10}

    def __init__(self, symbols=None, **params):
        super().__init__('AIVectorizedStrategy', symbols or ["BTCUSDT"], params)

    def populate_indicators(self, dataframe, metadata=None):
        return dataframe

    def populate_entry_trend(self, dataframe, metadata=None):
        dataframe['enter_long'] = 0
        return dataframe

    def populate_exit_trend(self, dataframe, metadata=None):
        dataframe['exit_long'] = 0
        return dataframe
```"""


@dataclass
class AgentResult:
    agent: str
    content: str
    success: bool = True
    error: str = ""


@dataclass
class MultiAgentResult:
    strategy_name: str = ""
    strategy_code: str = ""
    description: str = ""
    technical_analysis: str = ""
    onchain_analysis: str = ""
    sentiment_analysis: str = ""
    risk_assessment: str = ""
    bull_argument: str = ""
    bear_argument: str = ""
    debate_summary: str = ""
    success: bool = False
    error: str = ""


class MultiAgentCoordinator:
    """Orchestrates the 7-agent strategy generation pipeline."""

    def __init__(self, api_key: str, base_url: str, model: str = "gpt-4o"):
        self.api_key = api_key
        self.base_url = base_url.rstrip("/")
        self.model = model

    async def _call_ai(self, system_prompt: str, user_message: str,
                       temperature: float = 0.4, max_tokens: int = 1024) -> str:
        """Call the AI model and return text content."""
        import urllib.request
        import urllib.error

        payload = {
            "model": self.model,
            "messages": [
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": user_message},
            ],
            "temperature": temperature,
            "max_tokens": max_tokens,
        }

        url = f"{self.base_url}/chat/completions"
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }

        try:
            data = json.dumps(payload).encode("utf-8")
            req = urllib.request.Request(url, data=data, headers=headers)
            resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 90)
            text = resp.read().decode("utf-8")
            if resp.status >= 400:
                logger.error(f"[MultiAgent] AI error {resp.status}: {text[:300]}")
                return f"[Error: HTTP {resp.status}]"
            result = json.loads(text)
            choices = result.get("choices", [])
            if choices:
                return choices[0].get("message", {}).get("content", "[No response]")
            return "[No response]"
        except Exception as e:
            logger.error(f"[MultiAgent] AI call failed: {e}")
            return f"[Error: {str(e)[:200]}]"

    def _build_market_summary(self, symbol: str, interval: str, risk: str,
                               prompt: str, market_context: str) -> str:
        """Build a unified context document for all agents."""
        return f"""## Trading Task
- Symbol: {symbol}
- Interval: {interval}
- Risk Level: {risk}
- User Request: {prompt}

{market_context}"""

    # ── Phase 1: Parallel Analysis ──

    async def analyze_phase1(self, context: str) -> dict:
        """Run Technical, OnChain, Sentiment, and Risk agents in parallel."""
        tasks = {
            "technical": self._call_ai(TECHNICAL_ANALYST_PROMPT,
                f"{context}\n\nProvide your technical analysis as specified."),
            "onchain": self._call_ai(ONCHAIN_ANALYST_PROMPT,
                f"{context}\n\nProvide your on-chain inference as specified."),
            "sentiment": self._call_ai(SENTIMENT_ANALYST_PROMPT,
                f"{context}\n\nProvide your sentiment assessment as specified."),
            "risk": self._call_ai(RISK_ANALYST_PROMPT,
                f"{context}\n\nProvide your risk assessment as specified."),
        }
        results = {}
        for name, coro in tasks.items():
            try:
                results[name] = await coro
                logger.info(f"[MultiAgent] {name} agent completed")
            except Exception as e:
                results[name] = f"[Error: {e}]"
                logger.error(f"[MultiAgent] {name} agent failed: {e}")
        return results

    # ── Phase 2: Bull vs Bear Debate ──

    async def debate_phase2(self, context: str, phase1: dict) -> dict:
        """Run Bull agent, then Bear agent responds to Bull + research."""
        analysis_summary = f"""## Research Team Analysis

### Technical Analyst:
{phase1.get('technical', 'N/A')}

### OnChain Analyst:
{phase1.get('onchain', 'N/A')}

### Sentiment Analyst:
{phase1.get('sentiment', 'N/A')}

### Risk Analyst:
{phase1.get('risk', 'N/A')}"""

        # Bull goes first
        bull_prompt = f"""{context}

{analysis_summary}

Present your bull case for a LONG position."""
        bull_response = await self._call_ai(BULL_AGENT_PROMPT, bull_prompt, temperature=0.5)
        logger.info("[MultiAgent] Bull agent completed")

        # Bear responds to Bull
        bear_prompt = f"""{context}

{analysis_summary}

## Bull Argument:
{bull_response}

Challenge the bull case and present your bear argument."""
        bear_response = await self._call_ai(BEAR_AGENT_PROMPT, bear_prompt, temperature=0.5)
        logger.info("[MultiAgent] Bear agent completed")

        return {"bull": bull_response, "bear": bear_response}

    # ── Phase 3: Trader Code Generation ──

    async def generate_phase3(self, context: str, phase1: dict,
                               debate: dict,
                               strategy_type: str = "event_driven") -> dict:
        """Trader agent synthesizes all analysis into strategy code."""
        synthesis = f"""{context}

## Research Team Consensus

### Technical Analysis:
{phase1.get('technical', 'N/A')}

### OnChain Analysis:
{phase1.get('onchain', 'N/A')}

### Sentiment Analysis:
{phase1.get('sentiment', 'N/A')}

### Risk Parameters:
{phase1.get('risk', 'N/A')}

## Bull vs Bear Debate

### Bull Case:
{debate.get('bull', 'N/A')}

### Bear Rebuttal:
{debate.get('bear', 'N/A')}

---

Synthesize all of the above into a complete trading strategy. Generate the strategy code NOW.
Respond with ONLY a valid JSON object."""

        trader_prompt = TRADER_VECTORIZED_PROMPT if strategy_type == "vectorized" else TRADER_PROMPT
        raw = await self._call_ai(trader_prompt, synthesis, temperature=0.3, max_tokens=4096)
        logger.info("[MultiAgent] Trader agent completed")

        # Parse JSON from trader response
        content = raw.strip()
        # Remove markdown fences
        if content.startswith("```"):
            lines = content.split("\n")
            lines = lines[1:]
            if lines and lines[-1].strip() == "```":
                lines = lines[:-1]
            content = "\n".join(lines)

        # Extract JSON object
        import re
        json_match = re.search(r'\{.*\}', content, re.DOTALL)
        if json_match:
            content = json_match.group(0)

        try:
            result = json.loads(content)
            return result
        except json.JSONDecodeError:
            logger.error(f"[MultiAgent] Trader response not valid JSON: {content[:500]}")
            return {"strategy_name": "AIStrategy",
                    "strategy_code": "# Parse error - raw response:\n" + raw,
                    "description": "Failed to parse trader output as JSON"}

    # ── Main Pipeline ──

    async def generate(self, symbol: str, interval: str, risk: str,
                       prompt: str, market_context: str = "",
                       strategy_type: str = "event_driven") -> MultiAgentResult:
        """Run the full multi-agent pipeline and return a strategy.

        Args:
            strategy_type: 'event_driven' (BaseStrategy) or 'vectorized' (VectorizedStrategy)
        """
        result = MultiAgentResult()
        context = self._build_market_summary(symbol, interval, risk, prompt, market_context)

        try:
            # Phase 1: Parallel analysis
            logger.info("[MultiAgent] Phase 1: Running 4 analysis agents in parallel...")
            phase1 = await self.analyze_phase1(context)
            result.technical_analysis = phase1.get("technical", "")
            result.onchain_analysis = phase1.get("onchain", "")
            result.sentiment_analysis = phase1.get("sentiment", "")
            result.risk_assessment = phase1.get("risk", "")

            # Phase 2: Bull vs Bear debate
            logger.info("[MultiAgent] Phase 2: Bull vs Bear debate...")
            debate = await self.debate_phase2(context, phase1)
            result.bull_argument = debate.get("bull", "")
            result.bear_argument = debate.get("bear", "")
            result.debate_summary = f"BULL: {debate.get('bull', '')[:300]}\n\nBEAR: {debate.get('bear', '')[:300]}"

            # Phase 3: Trader generates code
            logger.info("[MultiAgent] Phase 3: Trader generating strategy code...")
            trader_result = await self.generate_phase3(context, phase1, debate, strategy_type)
            result.strategy_name = trader_result.get("strategy_name", "MultiAgentStrategy")
            result.strategy_code = trader_result.get("strategy_code", "")
            result.description = trader_result.get("description", "")

            required_pattern = "VectorizedStrategy" if strategy_type == "vectorized" else "BaseStrategy"
            result.success = bool(result.strategy_code and required_pattern in result.strategy_code)
            if not result.success:
                result.error = "Trader did not produce valid strategy code"

        except Exception as e:
            logger.error(f"[MultiAgent] Pipeline failed: {e}")
            result.error = str(e)

        return result

    # ── Closed-loop Optimization (Phase 4) ──

    async def optimize(self, symbol: str, interval: str, risk: str,
                       prompt: str, market_context: str = "",
                       backtest_fn=None, max_iterations: int = 3,
                       strategy_type: str = "event_driven") -> MultiAgentResult:
        """Iteratively generate, backtest, and refine strategies.

        Args:
            backtest_fn: async callable(strategy_code, config) -> dict with backtest metrics
            max_iterations: Max refinement iterations (default 3)
            strategy_type: 'event_driven' or 'vectorized'
        """
        best_result = None
        best_metrics = None
        iteration_history = []

        for iteration in range(max_iterations):
            logger.info(f"[MultiAgent] Optimization iteration {iteration + 1}/{max_iterations}")

            # Build feedback context from previous iteration
            feedback_context = market_context
            if iteration > 0 and backtest_fn and best_result:
                try:
                    bt_metrics = await backtest_fn(
                        best_result.strategy_code,
                        {"symbol": symbol, "fee_rate": 0.001, "slippage": 0.0005,
                         "num_bars": 500, "params": {},
                         "strategy_type": strategy_type}
                    )
                    report = bt_metrics.get("report", {})
                    feedback_context = f"""{market_context}

## Previous Iteration Feedback
The previous strategy had these backtest results:
- Total Return: {report.get('total_return_pct', 'N/A')}%
- Max Drawdown: {report.get('max_drawdown_pct', 'N/A')}%
- Sharpe Ratio: {report.get('sharpe_ratio', 'N/A')}
- Win Rate: {report.get('win_rate_pct', 'N/A')}%
- Total Trades: {report.get('num_trades', 'N/A')}

Please improve: better risk-adjusted returns, lower drawdown, more consistent PnL."""
                    iteration_history.append({"iteration": iteration, "report": report})
                    if best_metrics is None or report.get('total_return_pct', -999) > best_metrics.get('total_return_pct', -999):
                        best_metrics = report
                except Exception as e:
                    logger.warning(f"[MultiAgent] Backtest in optimization failed: {e}")

            # Run full multi-agent pipeline with feedback
            current = await self.generate(symbol, interval, risk, prompt, feedback_context, strategy_type)

            if current.success:
                if best_result is None:
                    best_result = current
                elif backtest_fn:
                    # Keep the one with better backtest
                    try:
                        bt = await backtest_fn(
                            current.strategy_code,
                            {"symbol": symbol, "fee_rate": 0.001, "slippage": 0.0005,
                             "num_bars": 500, "params": {},
                             "strategy_type": strategy_type}
                        )
                        curr_return = bt.get("report", {}).get("total_return_pct", -999)
                        if best_metrics is None or curr_return > best_metrics.get("total_return_pct", -999):
                            best_result = current
                            best_metrics = bt.get("report", {})
                    except Exception:
                        pass
                else:
                    best_result = current  # Without backtest, keep last successful result

        if best_result is None:
            best_result = MultiAgentResult(error="All optimization iterations failed")

        # Attach iteration history
        if iteration_history:
            best_result.description += f"\n\nOptimized over {len(iteration_history)} iterations."
            setattr(best_result, 'iteration_history', iteration_history)

        return best_result
