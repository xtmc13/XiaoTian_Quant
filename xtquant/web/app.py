"""
Web service — complete multi-page trading terminal
Inspired by Qbot's feature set: Dashboard / Trading / Backtest / Strategies / Settings
"""

import os
import asyncio
import time
import json
import random
import math
import logging
from typing import Optional, List, Dict, Any
from pathlib import Path

from ..ai.generator import AIStrategyGenerator
from ..agent.gateway import register_agent_gateway
from ..agent.token import AgentTokenManager

try:
    from fastapi import FastAPI, WebSocket, WebSocketDisconnect, Request
    from fastapi.responses import HTMLResponse, JSONResponse
    from fastapi.middleware.cors import CORSMiddleware
    import uvicorn
    HAS_FASTAPI = True
except ImportError:
    HAS_FASTAPI = False

logger = logging.getLogger("xtquant.web")

# ─────────────────────────────────────────────
#  Full HTML/CSS/JS — Single Page Application
# ─────────────────────────────────────────────
INDEX_HTML = r"""<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1.0">
<title>小天量化 v2.0 — Quantitative Trading Terminal</title>
<script src="https://unpkg.com/lightweight-charts@4.1.0/dist/lightweight-charts.standalone.production.js"></script>
<script src="https://cdn.jsdelivr.net/npm/echarts@5.5.0/dist/echarts.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/klinecharts@9.6.0/dist/klinecharts.min.js"></script>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/@klinecharts/pro@0.1.1/dist/klinecharts-pro.css">
<script src="https://cdn.jsdelivr.net/npm/@klinecharts/pro@0.1.1/dist/klinecharts-pro.umd.js"></script>
<script src="https://cdn.jsdelivr.net/npm/monaco-editor@0.45.0/min/vs/loader.js"></script>
<style>
:root{--bg:#f6f8fa;--card:#ffffff;--border:#d0d7de;--blue:#0969da;--green:#1a7f37;--red:#cf222e;--yel:#9a6700;--muted:#656d76;--hover:#eaeef2}
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:'Segoe UI',-apple-system,sans-serif;background:var(--bg);color:#1f2328;height:100vh;overflow:hidden}
a{color:var(--blue);text-decoration:none}

/* ── Top Bar ── */
#topbar{background:var(--card);border-bottom:1px solid var(--border);height:44px;display:flex;align-items:center;padding:0 16px;justify-content:space-between;font-size:12px}
#topbar h1{font-size:15px;color:var(--blue);font-weight:600;letter-spacing:-.3px}
.nav{display:flex;gap:2px}
.nav a{padding:6px 16px;border-radius:6px;font-size:12px;color:var(--muted);cursor:pointer;transition:all .15s}
.nav a:hover{color:#1f2328;background:var(--hover)}
.nav a.active{color:#1f2328;background:#1f6feb}
.status-row{display:flex;align-items:center;gap:12px;font-size:11px;color:var(--muted)}
.status-row b{color:#1f2328}
.dot{width:7px;height:7px;border-radius:50%;display:inline-block}
.dot.g{background:var(--green)}.dot.r{background:var(--red)}.dot.y{background:var(--yel)}

/* ── Layout ── */
#app{display:flex;flex-direction:column;height:calc(100vh - 44px)}
.page{display:none;flex:1;overflow:hidden}
.page.active{display:flex}

/* ── Dashboard Grid ── */
.dash-kpi-card{background:var(--card);border:1px solid var(--border);border-radius:8px;padding:10px 14px;position:relative;overflow:hidden}
.dash-kpi-card.clickable{cursor:pointer}
.dk-label{font-size:9px;color:var(--muted);text-transform:uppercase;letter-spacing:.5px}
.dk-value{font-size:22px;font-weight:700;margin-top:4px;color:#1f2328}
.dk-value.dd-val{color:var(--red)}
.dk-sub{font-size:10px;color:var(--muted);margin-top:2px}
.dk-ring{position:absolute;top:8px;right:10px;width:40px;height:40px}
.dk-ring svg{width:100%;height:100%;transform:rotate(-90deg)}
.dk-ring-bg{fill:none;stroke:var(--border);stroke-width:2.5}
.dk-ring-fg{fill:none;stroke:var(--green);stroke-width:2.5;stroke-linecap:round}
.dash-widget{background:var(--card);border:1px solid var(--border);border-radius:8px;padding:10px;overflow:hidden}
.dw-header{font-size:10px;color:var(--muted);text-transform:uppercase;letter-spacing:.5px;margin-bottom:6px;font-weight:600}
.dash-cal-day{display:inline-block;width:13%;text-align:center;padding:2px 0;border-radius:3px;margin:1px;font-family:monospace;font-size:10px}
.dash-cal-day.profit{background:rgba(26,127,55,.2);color:var(--green);font-weight:600}
.dash-cal-day.loss{background:rgba(207,34,46,.15);color:var(--red);font-weight:600}
.dash-cal-day.zero{color:var(--muted)}
.dash-cal-header{display:flex;justify-content:space-around;font-size:9px;color:var(--muted);margin-bottom:4px}
.rank-card{display:flex;align-items:center;gap:10px;padding:8px;border-bottom:1px solid var(--border);transition:background .15s}
.rank-card:hover{background:var(--hover)}
.rank-card.rank-top{background:linear-gradient(90deg,rgba(212,175,55,.08) 0%,transparent 100%)}
.rank-badge{width:26px;height:26px;border-radius:50%;display:flex;align-items:center;justify-content:center;font-size:11px;font-weight:700;flex-shrink:0}
.rank-badge.r1{background:#d4af37;color:#fff}
.rank-badge.r2{background:#a0a0a0;color:#fff}
.rank-badge.r3{background:#cd7f32;color:#fff}
.rank-badge.rn{background:var(--hover);color:var(--muted)}
.rank-info{flex:1;min-width:0}
.rank-name{font-size:12px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.rank-stats{display:flex;gap:12px;font-size:10px;color:var(--muted);margin-top:2px}
.rank-pnl{font-family:monospace;font-size:12px;font-weight:700;flex-shrink:0}
.dash-grid{display:grid;grid-template-columns:1fr 1fr 1fr;grid-template-rows:auto 1fr 1fr;gap:1px;background:var(--border);flex:1;overflow:hidden}
.dash-grid>*{background:var(--card);overflow:auto}
.kpi-row{grid-column:1/-1;display:flex;gap:1px;background:var(--border)}
.kpi{flex:1;background:var(--card);padding:12px 16px}
.kpi .label{font-size:9px;color:var(--muted);text-transform:uppercase;letter-spacing:.5px}
.kpi .value{font-size:20px;font-weight:700;margin-top:2px}
.kpi .sub{font-size:10px;color:var(--muted);margin-top:1px}
.kpi-chart{grid-column:1/3;grid-row:2/4;position:relative}
.kpi-orders{grid-column:3;grid-row:2;overflow:hidden}
.kpi-risk{grid-column:3;grid-row:3;overflow:hidden}

/* ── Trading Layout ── */
.trade-layout{display:grid;grid-template-columns:1fr 280px 300px;grid-template-rows:1fr;gap:2px;background:var(--border);flex:1;overflow:hidden;min-height:0}
.trade-layout>*{background:var(--card);overflow:hidden;display:flex;flex-direction:column}
.trade-chart-panel{padding:8px;min-width:0}
.trade-ob-panel{min-width:0}
.trade-form-panel{padding:10px;overflow-y:auto;min-width:0}

/* ── Bottom Panel (Orders/History/Positions) ── */
.trade-bottom{background:var(--card);border-top:2px solid var(--border);flex-shrink:0;display:flex;flex-direction:column;position:relative}
.trade-bottom .trade-tabs{flex-shrink:0}
.trade-bottom-content{flex:1;overflow:auto;font-size:10px;min-height:0}
.trade-bottom-bar{display:flex;justify-content:space-between;align-items:center;padding:4px 10px;border-top:1px solid var(--border);flex-shrink:0;font-size:9px}
.pos-card{background:var(--bg);border:1px solid var(--border);border-radius:8px;padding:10px 12px;min-width:180px;flex-shrink:0}
.pc-symbol{font-size:13px;font-weight:700}.pc-side{font-size:9px;padding:1px 6px;border-radius:4px;margin-left:6px}
.pc-side.long{background:rgba(26,127,55,.15);color:var(--green)}.pc-side.short{background:rgba(207,34,46,.15);color:var(--red)}
.pc-row{display:flex;justify-content:space-between;font-size:10px;margin-top:4px;color:var(--muted)}
.pc-pnl{font-size:14px;font-weight:700;margin-top:6px}
/* Bot cards */
.bot-card{background:var(--card);border:1px solid var(--border);border-radius:10px;padding:12px;min-width:170px;flex-shrink:0;cursor:pointer;display:flex;align-items:center;gap:10px;transition:all .15s}
.bot-card:hover{border-color:var(--blue);box-shadow:0 2px 8px rgba(0,0,0,.08)}
.bot-card-icon{width:36px;height:36px;border-radius:10px;display:flex;align-items:center;justify-content:center;color:#fff;font-weight:700;font-size:14px;flex-shrink:0}
.bot-card-body{flex:1;min-width:0}
.bot-card-name{font-size:12px;font-weight:600;color:#1f2328}
.bot-card-desc{font-size:10px;color:var(--muted);margin-top:2px}
.bot-card-tag{font-size:8px;padding:1px 6px;border-radius:8px;position:absolute;top:8px;right:8px}
.tag-ai{background:rgba(9,105,218,.15);color:var(--blue)}
.tag-low{background:rgba(26,127,55,.12);color:var(--green)}
.tag-medium{background:rgba(210,153,34,.12);color:var(--yel)}
.tag-high{background:rgba(207,34,46,.12);color:var(--red)}
.ai-chip{display:inline-block;padding:4px 10px;background:var(--bg);border:1px solid var(--border);border-radius:14px;font-size:10px;color:var(--muted);cursor:pointer;transition:all .15s}
.ai-chip:hover{border-color:var(--blue);color:var(--blue);background:rgba(9,105,218,.06)}
@keyframes aiPulse{0%,100%{opacity:1;transform:scale(1)}50%{opacity:.6;transform:scale(1.1)}}
.ai-dot{width:6px;height:6px;border-radius:50%;background:var(--blue);animation:aiPulse 1.2s infinite}
.chart-ind-btn{font-size:9px;padding:3px 8px;background:transparent;border:1px solid var(--border);border-radius:4px;color:var(--muted);cursor:pointer;transition:all .15s}
.chart-ind-btn:hover,.chart-ind-btn.active{border-color:var(--blue);color:var(--blue);background:rgba(9,105,218,.06)}
.bottom-resize-handle{position:absolute;top:-4px;left:0;right:0;height:6px;cursor:ns-resize;z-index:10}
.bottom-resize-handle:hover{background:var(--blue);opacity:.3}

/* ── Orderbook Tabs ── */
.ob-tabs{display:flex;flex-shrink:0;padding:0 8px;border-bottom:1px solid var(--border)}
.ob-tab{flex:1;padding:8px 4px;font-size:10px;text-align:center;color:var(--muted);cursor:pointer;border-bottom:2px solid transparent;transition:all .15s;user-select:none;font-weight:600}
.ob-tab:hover{color:#1f2328}
.ob-tab.active{color:#1f2328;border-bottom-color:var(--blue)}

/* ── Trade Form Inputs ── */
.tf-input-wrap{margin-bottom:10px}
.tf-label{font-size:10px;color:var(--muted);margin-bottom:4px;display:flex;justify-content:space-between;align-items:center}
.tf-label .coin{color:#1f2328;font-size:9px}
.tf-input{width:100%;padding:9px 10px;font-size:13px;font-family:'Cascadia Code',monospace;background:#f6f8fa;border:1px solid var(--border);border-radius:4px;color:#1f2328;outline:none;box-sizing:border-box;transition:border-color .15s,box-shadow .15s}
.tf-input:focus{border-color:var(--blue);box-shadow:0 0 0 2px rgba(88,166,255,.15)}
.tf-input:disabled{opacity:.5;background:rgba(48,54,61,.15)}

/* ── Trade: Quick Pct Buttons ── */
.pct-btns{display:flex;gap:4px;margin-bottom:12px}
.pct-btn{flex:1;padding:6px 0;font-size:10px;border:1px solid rgba(48,54,61,.4);border-radius:4px;background:rgba(48,54,61,.15);color:var(--muted);cursor:pointer;text-align:center;transition:all .15s;font-weight:500}
.pct-btn:hover{color:#1f2328;border-color:var(--muted);background:rgba(48,54,61,.35)}

/* ── Trade: Order Book (TradingView-style) ── */
.ob-header{display:grid;grid-template-columns:1fr 1fr 1fr;padding:4px 10px;font-size:10px;color:var(--muted);border-bottom:1px solid rgba(48,54,61,.3);font-weight:600}
.ob-header span:nth-child(2),.ob-header span:nth-child(3){text-align:right}
.ob-row{display:grid;grid-template-columns:1fr 1fr 1fr;padding:0 10px;font-size:11px;cursor:pointer;font-family:'Cascadia Code',monospace;position:relative;height:20px;align-items:center;transition:background .08s}
.ob-row:hover{background:rgba(255,255,255,.04)}
.ob-row .ob-price{position:relative;z-index:1}
.ob-row .ob-amount,.ob-row .ob-cumulative{text-align:right;position:relative;z-index:1}
.ob-row .ob-bar{position:absolute;right:0;top:0;height:100%;opacity:.15;transition:width .3s}
.ob-row.ask .ob-price{color:var(--red)}
.ob-row.ask .ob-bar{background:var(--red)}
.ob-row.bid .ob-price{color:var(--green)}
.ob-row.bid .ob-bar{background:var(--green)}
.ob-spread-bar{display:flex;align-items:center;justify-content:space-between;padding:6px 10px;border-top:1px solid rgba(48,54,61,.5);border-bottom:1px solid rgba(48,54,61,.5);flex-shrink:0;font-family:'Cascadia Code',monospace}
.ob-spread-bar .price{font-size:20px;font-weight:700}
.ob-spread-bar .spread{font-size:11px;color:var(--muted)}

/* ── Trade: Ticker Strip ── */
.ticker-strip{display:flex;align-items:center;justify-content:space-between;padding:8px 16px;background:var(--card);border-bottom:1px solid var(--border);flex-shrink:0}
.ticker-strip select{font-family:inherit}

/* ── Trade: Buy/Sell Toggle ── */
.bn-toggle{display:flex;margin-bottom:12px}
.bn-buy-btn,.bn-sell-btn{flex:1;padding:10px 0;font-size:13px;font-weight:700;border:1px solid transparent;cursor:pointer;transition:all .15s;text-align:center;color:var(--muted);background:rgba(48,54,61,.25)}
.bn-buy-btn{border-radius:4px 0 0 4px}
.bn-sell-btn{border-radius:0 4px 4px 0}
.bn-buy-btn.active{background:#2ea043;color:#1f2328;border-color:#2ea043;box-shadow:0 0 8px rgba(46,160,67,.25)}
.bn-sell-btn.active{background:#da3633;color:#1f2328;border-color:#da3633;box-shadow:0 0 8px rgba(218,54,51,.25)}
.bn-buy-btn:hover:not(.active){color:#1f2328;background:rgba(48,54,61,.5)}
.bn-sell-btn:hover:not(.active){color:#1f2328;background:rgba(48,54,61,.5)}

/* ── Trade: Submit Button ── */
.bn-submit-btn{width:100%;padding:12px 0;font-size:14px;font-weight:700;border:none;border-radius:4px;cursor:pointer;color:#1f2328;transition:all .15s;letter-spacing:.5px}
.bn-submit-btn.buy{background:#2ea043}
.bn-submit-btn.buy:hover{background:#3cbc4c}
.bn-submit-btn.sell{background:#da3633}
.bn-submit-btn.sell:hover{background:#f24a47}
.bn-submit-btn:disabled{opacity:.4;cursor:not-allowed}

/* ── Trade: Best Bid/Ask Quick Buttons ── */
.best-btn{padding:8px 10px;font-size:10px;font-weight:700;border:1px solid transparent;border-radius:4px;cursor:pointer;transition:all .12s;white-space:nowrap;font-family:'Cascadia Code',monospace}
.best-btn.bid{background:rgba(63,185,80,.12);color:var(--green);border-color:rgba(63,185,80,.25)}
.best-btn.bid:hover{background:rgba(63,185,80,.25);border-color:var(--green)}
.best-btn.ask{background:rgba(248,81,73,.12);color:var(--red);border-color:rgba(248,81,73,.25)}
.best-btn.ask:hover{background:rgba(248,81,73,.25);border-color:var(--red)}

/* ── Trade: Balance + Preview ── */
.tf-balance{display:flex;justify-content:space-between;align-items:center;font-size:10px;color:var(--muted);margin-bottom:12px;padding:6px 10px;background:rgba(48,54,61,.15);border-radius:4px}
.tf-balance b{color:#1f2328;font-size:11px}
.tf-preview{background:rgba(22,27,34,.6);border:1px solid rgba(48,54,61,.4);border-radius:6px;padding:10px;margin-bottom:12px;font-size:11px}
.tf-preview-row{display:flex;justify-content:space-between;align-items:center;padding:2px 0}
.tf-preview-row .pv-val{font-family:'Cascadia Code',monospace}
.tf-preview-row.divider{border-top:1px solid rgba(48,54,61,.3);margin-top:4px;padding-top:6px;font-weight:700;font-size:13px}

/* ── Trade: Chart Toolbar ── */
.chart-toolbar{display:flex;align-items:center;justify-content:space-between;margin-bottom:6px;flex-shrink:0}
.tv-interval{padding:3px 8px;font-size:10px;border:1px solid transparent;border-radius:3px;background:transparent;color:var(--muted);cursor:pointer;transition:all .15s}
.tv-interval:hover{color:#1f2328;background:var(--hover)}
.tv-interval.active{color:var(--blue);background:rgba(88,166,255,.1);border-color:rgba(88,166,255,.2);font-weight:600}
.tv-tool-btn{padding:3px 7px;font-size:12px;border:1px solid var(--border);border-radius:3px;background:transparent;color:var(--muted);cursor:pointer;transition:all .15s}
.tv-tool-btn:hover{color:#1f2328;background:var(--hover)}

/* ── Drawing Tool Buttons ── */
.draw-sep{width:1px;height:18px;background:var(--border);margin:0 3px}
.draw-btn{padding:3px 7px;font-size:12px;border:1px solid transparent;border-radius:3px;background:transparent;color:var(--muted);cursor:pointer;transition:all .15s;line-height:1;font-family:inherit}
.draw-btn:hover{color:#1f2328;background:var(--hover);border-color:var(--border)}
.draw-btn.active{color:var(--blue);background:rgba(88,166,255,.12);border-color:rgba(88,166,255,.25)}
.draw-btn.danger{color:var(--muted)}
.draw-btn.danger:hover{color:var(--red);border-color:rgba(248,81,73,.3);background:rgba(248,81,73,.08)}

/* ── Trade: Percentage Slider ── */
.pct-slider{margin-bottom:8px}
.pct-track{position:relative;height:4px;background:var(--border);border-radius:2px;margin-bottom:6px;cursor:pointer}
.pct-fill{position:absolute;left:0;top:0;height:100%;background:var(--blue);border-radius:2px;transition:width .2s}
.pct-thumb{position:absolute;top:50%;transform:translate(-50%,-50%);width:12px;height:12px;background:#fff;border-radius:50%;box-shadow:0 1px 3px rgba(0,0,0,.3);transition:left .2s}
.pct-labels{display:flex;justify-content:space-between;font-size:9px;color:var(--muted)}
.pct-labels span{cursor:pointer;transition:color .1s}
.pct-labels span:hover{color:#1f2328}

/* ── Trade: Right Panel Tabs ── */
.trade-tabs{display:flex;border-bottom:1px solid var(--border);flex-shrink:0}
.trade-tab{flex:1;padding:8px 4px;font-size:10px;text-align:center;color:var(--muted);cursor:pointer;border-bottom:2px solid transparent;transition:all .15s;user-select:none;font-weight:600}
.trade-tab:hover{color:#1f2328}
.trade-tab.active{color:#1f2328;border-bottom-color:var(--blue)}

/* ── Trade: Position Card ── */
.pos-row{padding:8px 12px;border-bottom:1px solid var(--border);font-size:10px}
.pos-row .pos-symbol{font-weight:600;font-size:12px}
.pos-row .pos-pnl{font-weight:700}
.pos-row .pos-pnl.up{color:var(--green)}
.pos-row .pos-pnl.down{color:var(--red)}

/* ── Backtest Layout ── */
.bt-layout{display:grid;grid-template-columns:320px 1fr;grid-template-rows:1fr 1fr;gap:1px;background:var(--border);flex:1;overflow:hidden}
.bt-layout>*{background:var(--card);overflow:auto}

/* ── Common ── */
.pad{padding:12px}
.section-title{font-size:10px;color:var(--muted);text-transform:uppercase;letter-spacing:.8px;margin-bottom:10px;font-weight:600}
table{width:100%;border-collapse:collapse;font-size:10px}
th{text-align:left;padding:6px 10px;color:var(--muted);font-weight:400;border-bottom:1px solid var(--border);position:sticky;top:0;background:var(--card);z-index:1}
td{padding:4px 10px;border-bottom:1px solid rgba(48,54,61,.3);font-family:'Cascadia Code',monospace;font-size:9px}
tr:hover td{background:var(--hover)}
.g{color:var(--green)}.r{color:var(--red)}.y{color:var(--yel)}.m{color:var(--muted)}

.btn{padding:6px 16px;border-radius:5px;border:1px solid var(--border);background:#eaeef2;color:#1f2328;font-size:11px;cursor:pointer;transition:all .15s}
.btn:hover{background:#d0d7de}
.btn-p{background:#1f6feb;border-color:#1f6feb;color:#1f2328}.btn-p:hover{background:#388bfd}
.btn-g{background:#238636;border-color:#238636;color:#1f2328}.btn-g:hover{background:#2ea043}
.btn-r{background:#da3633;border-color:#da3633;color:#1f2328}.btn-r:hover{background:#f85149}
.btn-y{background:#9e6a03;border-color:#d29922;color:#1f2328}.btn-y:hover{background:#bb8009}
.code-ok{color:#3fb950!important}
.btn-xs{padding:2px 7px;font-size:9px;border-radius:3px}

input,select{background:var(--bg);border:1px solid var(--border);color:#1f2328;padding:7px 10px;border-radius:5px;font-size:11px;font-family:'Cascadia Code',monospace;width:100%}
input:focus,select:focus{outline:none;border-color:var(--blue)}
.f-row{display:flex;gap:8px;margin-bottom:10px}
.f-row>*{flex:1}
.f-label{font-size:9px;color:var(--muted);text-transform:uppercase;margin-bottom:3px;letter-spacing:.3px}

/* ── Toast ── */
#toast{position:fixed;top:54px;right:20px;padding:10px 18px;border-radius:6px;font-size:12px;z-index:999;display:none;pointer-events:none}
#toast.show{display:block}
#toast.ok{background:#238636;color:#1f2328}
#toast.err{background:#da3633;color:#1f2328}

/* ── Modal ── */
.modal-overlay{display:none;position:fixed;inset:0;background:rgba(0,0,0,.55);z-index:998;align-items:center;justify-content:center}
.modal-overlay.show{display:flex}
.modal{background:var(--card);border:1px solid var(--border);border-radius:12px;padding:24px;min-width:420px;max-width:520px;max-height:80vh;overflow-y:auto}
.modal h3{margin-bottom:16px;font-size:15px}
.modal .btn-row{display:flex;gap:8px;justify-content:flex-end;margin-top:16px}

/* ── Strategy Page: Three-Column Layout ── */
.strategy-page{display:flex;gap:16px;flex:1;padding:12px;overflow:hidden}
.strategy-sidebar{width:170px;flex-shrink:0;display:flex;flex-direction:column;gap:2px}
.strategy-sidebar .sidebar-label{font-size:9px;color:var(--muted);text-transform:uppercase;letter-spacing:.5px;padding:6px 10px 2px}
.strategy-sidebar .sidebar-tab{padding:9px 12px;border-radius:6px;font-size:12px;color:var(--muted);cursor:pointer;transition:all .15s;border:1px solid transparent}
.strategy-sidebar .sidebar-tab:hover{color:#1f2328;background:var(--hover)}
.strategy-sidebar .sidebar-tab.active{color:#1f2328;background:#1f6feb;border-color:#1f6feb;font-weight:600}

.strategy-middle{width:200px;flex-shrink:0;display:flex;flex-direction:column;gap:8px;overflow-y:auto}
.strategy-middle .middle-section{background:var(--card);border:1px solid var(--border);border-radius:8px;padding:12px}
.strategy-middle .section-label{font-size:9px;color:var(--muted);text-transform:uppercase;letter-spacing:.5px;margin-bottom:8px}
.strategy-middle .kpi-card{margin-bottom:10px}
.strategy-middle .kpi-card:last-child{margin-bottom:0}
.strategy-middle .kpi-card .klabel{font-size:9px;color:var(--muted)}
.strategy-middle .kpi-card .kvalue{font-size:16px;font-weight:700;margin-top:2px}
.strategy-middle .kpi-card .ksub{font-size:10px;color:var(--muted)}

.strategy-main{flex:1;display:flex;flex-direction:column;min-width:0;overflow:hidden}

/* ── Sub Pages ── */
.sub-page{display:none;flex:1;overflow:hidden;flex-direction:column}
.sub-page.active{display:flex}

/* ── Toggle Switch ── */
.toggle-switch{position:relative;width:44px;height:24px;background-color:#2D3748;border-radius:12px;cursor:pointer;transition:background-color 0.3s ease;display:inline-block;flex-shrink:0}
.toggle-switch.active{background-color:#10B981}
.toggle-switch::after{content:'';position:absolute;top:2px;left:2px;width:20px;height:20px;background-color:#FFFFFF;border-radius:50%;transition:left 0.3s ease}
.toggle-switch.active::after{left:22px}
.toggle-input{display:none}

/* ── Filter Bar ── */
.filter-bar{display:flex;gap:8px;padding:8px 10px;flex-shrink:0;flex-wrap:wrap;align-items:center;background:var(--card);border:1px solid var(--border);border-radius:8px 8px 0 0}
.filter-bar input,.filter-bar select{width:auto;min-width:90px;padding:5px 8px;font-size:10px}

/* ── Status Tabs ── */
.status-tabs{display:flex;gap:2px}
.st-tab{padding:4px 10px;font-size:10px;color:var(--muted);cursor:pointer;border:1px solid var(--border);border-radius:3px;transition:all .15s}
.st-tab:hover{color:#1f2328;border-color:var(--muted)}
.st-tab.active{background:var(--blue);color:#1f2328;border-color:var(--blue)}

/* ── Stats Bar ── */
.stats-bar{display:flex;gap:18px;padding:6px 12px;font-size:10px;color:var(--muted);flex-shrink:0;background:var(--card);border-left:1px solid var(--border);border-right:1px solid var(--border)}
.stats-bar b{color:#1f2328}

/* ── Table ── */
.table-container table{width:100%;border-collapse:collapse}
.table-container td,.table-container th{padding:8px 10px;text-align:left;border-bottom:1px solid rgba(48,54,61,.3);white-space:nowrap}
.table-container td.coin-cell{max-width:120px;overflow:hidden;text-overflow:ellipsis}
.table-container th{font-size:9px;color:var(--muted);text-transform:uppercase;letter-spacing:.4px;position:sticky;top:0;background:var(--card);z-index:1}
.table-container td{font-size:11px}
.table-container tr:hover td{background:rgba(31,111,235,.04)}
.status-running{color:#3fb950;font-weight:600}
.status-monitoring{color:#d29922;font-weight:600}
.status-stopped{color:#8b949e}
.status-history{color:#d29922}

/* ── Empty State ── */
.empty-state{display:flex;flex-direction:column;align-items:center;justify-content:center;padding:48px 20px;color:var(--muted);text-align:center;flex:1}
.empty-state .empty-icon{font-size:32px;margin-bottom:12px;opacity:.4}
.empty-state .empty-title{font-size:13px;color:#1f2328;margin-bottom:6px}
.empty-state .empty-desc{font-size:11px;margin-bottom:16px}

/* ── Toolbar ── */
.toolbar{display:flex;gap:8px;padding:8px 12px;flex-shrink:0;justify-content:space-between;align-items:center;background:var(--card);border:1px solid var(--border);border-top:none;border-radius:0 0 8px 8px}
.toolbar .btn:disabled{opacity:.35;cursor:not-allowed;pointer-events:none}

/* ── Form Layout ── */
.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:12px}
.form-grid .full{grid-column:1/-1}
.form-section{margin-bottom:18px}
.form-section-title{font-size:11px;color:var(--blue);font-weight:600;margin-bottom:8px;padding-bottom:5px;border-bottom:1px solid var(--border);text-transform:uppercase;letter-spacing:.3px}
.form-inline{display:flex;align-items:center;gap:8px;margin-bottom:6px}
.form-inline label{font-size:10px;color:var(--muted);white-space:nowrap;min-width:70px}

/* ── Radio Chip ── */
.radio-chip{position:relative;cursor:pointer}
.radio-chip input{position:absolute;opacity:0}
.radio-chip span{display:inline-block;padding:5px 12px;border:1px solid var(--border);border-radius:5px;font-size:10px;color:var(--muted);background:var(--bg);transition:all .15s;white-space:nowrap}
.radio-chip input:checked+span{background:#1f6feb;border-color:#1f6feb;color:#1f2328}
.radio-chip:hover span{border-color:var(--muted)}

/* ── Wider Modal ── */
.modal.wide{min-width:560px;max-width:700px}

/* ── Template Card ── */
.tpl-card{background:var(--bg);border:1px solid var(--border);border-radius:8px;padding:12px;width:210px;flex-shrink:0}
.tpl-card .tpl-name{font-size:12px;font-weight:600;margin-bottom:4px}
.tpl-card .tpl-meta{font-size:9px;color:var(--muted)}
.tpl-card .tpl-actions{margin-top:8px;display:flex;gap:4px}

/* ── Settings Page ── */
.settings-page-title{font-size:20px;font-weight:700;margin-bottom:20px;color:#1f2328}
.settings-module{margin-bottom:28px}
.settings-module-title{font-size:13px;font-weight:600;color:var(--muted);text-transform:uppercase;letter-spacing:.5px;margin-bottom:12px;padding-bottom:8px;border-bottom:1px solid var(--border)}
.settings-card-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(380px,1fr));gap:12px}

.scard{background:var(--card);border:1px solid var(--border);border-radius:10px;overflow:hidden;transition:border-color .2s}
.scard:hover{border-color:#d0d7de}
.scard-header{display:flex;align-items:center;gap:10px;padding:12px 14px;border-bottom:1px solid rgba(48,54,61,.4);background:rgba(255,255,255,.015)}
.scard-icon{width:32px;height:32px;border-radius:8px;display:flex;align-items:center;justify-content:center;font-weight:700;font-size:15px;flex-shrink:0}
.scard-name{font-size:14px;font-weight:600;color:#ddf4ff;flex:1}
.scard-status{font-size:10px;padding:3px 8px;border-radius:10px;font-weight:500}
.scard-status.ok{background:rgba(76,175,80,.18);color:#4CAF50}
.scard-status.err{background:rgba(244,67,54,.18);color:#F44336}
.scard-status.testing{background:rgba(255,193,7,.18);color:#FFC107}
.scard-body{padding:12px 14px}
.scard-body .f-label{margin-bottom:3px}
.scard-body input{margin-bottom:0}
.scard-body select{margin-bottom:0}
.scard-desc{font-size:11px;color:var(--muted);line-height:1.5;margin-bottom:8px}
.scard-desc code{background:rgba(48,54,61,.5);color:#1f2328;padding:1px 5px;border-radius:4px;font-size:10px}

.sc-pw-row{position:relative;display:flex;align-items:center;margin-bottom:8px}
.sc-pw-row input{flex:1;padding-right:32px;margin-bottom:0}
.sc-eye{position:absolute;right:4px;background:none;border:none;color:var(--muted);cursor:pointer;font-size:13px;padding:4px 6px;line-height:1;transition:color .15s}
.sc-eye:hover{color:#1f2328}

.sc-row{display:flex;gap:10px;margin-bottom:8px;align-items:flex-end}
.sc-actions{display:flex;gap:8px;align-items:center;margin-top:10px;padding-top:10px;border-top:1px solid rgba(48,54,61,.3)}
.sc-btn-test{background:#eaeef2;color:#1f2328;border-color:var(--border)}
.sc-btn-test:hover{background:#d0d7de}
.sc-msg{font-size:10px;margin-left:4px}
.sc-msg.ok{color:#4CAF50}
.sc-msg.err{color:#F44336}

/* ── Agent Token ── */
.tg-label{display:inline-flex;align-items:center;gap:4px;font-size:10px;color:var(--muted);cursor:pointer;user-select:none}
.tg-label input[type=checkbox]{width:auto;margin:0}
#agent-token-list table{font-size:10px;width:100%}
#agent-token-list td{padding:6px 10px;font-family:'Cascadia Code',monospace;font-size:9px}
#agent-token-list th{padding:6px 10px;text-align:left;color:var(--muted);font-weight:400;border-bottom:1px solid var(--border);font-size:9px;text-transform:uppercase}
.token-masked{color:var(--muted)}
.token-scope-tag{display:inline-block;padding:1px 6px;background:rgba(31,111,235,.15);color:var(--blue);border-radius:3px;font-size:9px;margin:1px 2px}
.token-revoked{text-decoration:line-through;color:var(--red);opacity:.6}

/* ── Settings Tabs ── */
.stabs{display:flex;gap:0;margin-bottom:18px;border-bottom:1px solid var(--border)}
.stab-item{padding:8px 18px;font-size:12px;color:var(--muted);cursor:pointer;border-bottom:2px solid transparent;transition:all .15s;user-select:none}
.stab-item:hover{color:#1f2328}
.stab-item.active{color:#1f2328;border-bottom-color:#1f6feb;font-weight:600}
.stab-content{display:none}
.stab-content.active{display:block}

/* ── Agent Wizard ── */
.ag-wizard{max-width:720px}
.ag-wizard-steps{display:flex;gap:0;margin-bottom:24px;counter-reset:step}
.ag-ws{flex:1;text-align:center;position:relative;padding-top:28px;font-size:10px;color:var(--muted)}
.ag-ws::before{content:'';position:absolute;top:8px;left:50%;transform:translateX(-50%);width:18px;height:18px;border-radius:50%;border:2px solid var(--border);background:var(--bg);z-index:1;transition:all .2s}
.ag-ws::after{content:'';position:absolute;top:16px;left:0;right:0;height:2px;background:var(--border);transition:background .2s}
.ag-ws:first-child::after{left:50%}
.ag-ws:last-child::after{right:50%}
.ag-ws.done{color:var(--green)}
.ag-ws.done::before{border-color:var(--green);background:var(--green)}
.ag-ws.done::after{background:var(--green)}
.ag-ws.active{color:#1f2328;font-weight:600}
.ag-ws.active::before{border-color:var(--blue);box-shadow:0 0 0 3px rgba(88,166,255,.2)}
.ag-wizard-body{margin-bottom:16px}
.ag-wizard-footer{display:flex;gap:8px;justify-content:flex-end;padding-top:12px;border-top:1px solid var(--border)}
.ag-template-cards{display:grid;grid-template-columns:repeat(4,1fr);gap:8px;margin-bottom:12px}
.ag-tmpl{padding:10px;border:1px solid var(--border);border-radius:8px;cursor:pointer;text-align:center;transition:all .15s;background:var(--bg)}
.ag-tmpl:hover{border-color:var(--blue)}
.ag-tmpl.sel{border-color:var(--blue);background:rgba(31,111,235,.08)}
.ag-tmpl .ag-tmpl-icon{font-size:18px;margin-bottom:4px}
.ag-tmpl .ag-tmpl-name{font-size:11px;font-weight:600;margin-bottom:2px}
.ag-tmpl .ag-tmpl-desc{font-size:9px;color:var(--muted)}
.ag-tool-cards{display:grid;grid-template-columns:repeat(2,1fr);gap:8px}
.ag-tool{padding:12px;border:1px solid var(--border);border-radius:8px;cursor:pointer;transition:all .15s;background:var(--bg)}
.ag-tool:hover{border-color:var(--blue)}
.ag-tool.sel{border-color:var(--blue);background:rgba(31,111,235,.08)}
.ag-tool .ag-tool-icon{font-size:16px;margin-bottom:4px;font-weight:700}
.ag-tool .ag-tool-name{font-size:11px;font-weight:600;margin-bottom:2px}
.ag-tool .ag-tool-path{font-size:9px;color:var(--muted);font-family:'Cascadia Code',monospace}
.ag-config-preview{background:var(--bg);border:1px solid var(--border);border-radius:8px;padding:12px;font-family:'Cascadia Code',monospace;font-size:10px;white-space:pre-wrap;max-height:300px;overflow:auto;color:#1f2328}
.ag-skip{font-size:10px;color:var(--muted);cursor:pointer;text-decoration:underline}
.ag-skip:hover{color:#1f2328}
.ag-token-result{background:#238636;color:#1f2328;padding:12px;border-radius:6px;margin:10px 0;font-size:12px}
.ag-token-result code{font-family:'Cascadia Code',monospace;font-size:12px;word-break:break-all}

/* ── Risk Cards ── */
.risk-cards{display:grid;grid-template-columns:repeat(3,1fr);gap:6px;margin-bottom:8px}
.risk-card{background:var(--bg);border:2px solid var(--border);border-radius:8px;padding:8px 6px;text-align:center;cursor:pointer;transition:all .2s;user-select:none}
.risk-card:hover{border-color:var(--muted)}
.risk-card.active{border-color:var(--blue);background:rgba(31,111,235,.06);box-shadow:0 0 8px rgba(31,111,235,.12)}
.risk-card .risk-icon{font-size:16px;display:block;margin-bottom:2px}
.risk-card .risk-name{font-size:10px;font-weight:600;display:block}
.risk-card .risk-desc{font-size:8px;color:var(--muted);display:block;line-height:1.4}
.risk-card .risk-tag{font-size:7px;color:var(--yel);display:block;margin-top:2px}

/* ── Risk Preview Bars ── */
.risk-bar-row{display:flex;align-items:center;gap:6px;margin-bottom:3px}
.risk-bar-row span:first-child{width:50px;flex-shrink:0;color:var(--muted)}
.risk-bar-fill{height:6px;background:var(--blue);border-radius:3px;transition:width .3s;min-width:2px}
.risk-bar-fill.dd{background:var(--red)}
.risk-bar-fill.ret{background:var(--green)}
.risk-bar-val{font-weight:600;font-size:9px;width:45px;text-align:right}

/* ── AI Mode Bar ── */
.ai-mode-bar{display:flex;gap:3px;margin-bottom:8px}
.ai-mode-btn{flex:1;padding:5px 2px;font-size:9px;border:1px solid var(--border);border-radius:5px;background:var(--bg);color:var(--muted);cursor:pointer;transition:all .15s;text-align:center}
.ai-mode-btn:hover{border-color:var(--muted);color:#1f2328}
.ai-mode-btn.active{border-color:var(--blue);background:rgba(31,111,235,.1);color:var(--blue);font-weight:600}

/* ── Agent Chips ── */
.agent-chip{display:flex;align-items:center;gap:4px;padding:4px 8px;border-radius:4px;background:var(--bg);border:1px solid var(--border);font-size:10px;color:var(--muted)}
.agent-dot{width:6px;height:6px;border-radius:50%;background:var(--border)}
.agent-chip.active{border-color:var(--blue);color:var(--blue)}
.agent-chip.active .agent-dot{background:var(--blue);animation:pulse 1s infinite}
.agent-chip.done{border-color:var(--green);color:var(--green)}
.agent-chip.done .agent-dot{background:var(--green)}
.agent-chip.error{border-color:var(--red);color:var(--red)}
.agent-chip.error .agent-dot{background:var(--red)}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:.3}}

/* ── AI Preview Card ── */
.preview-header{margin-bottom:4px}
.preview-metrics{display:flex;gap:12px;flex-wrap:wrap}
.preview-metrics span{font-size:10px;color:var(--muted)}
.preview-metrics b{color:#1f2328}

/* ── AI Wizard Steps ── */
.ai-wizard-steps{display:flex;gap:0;margin-bottom:12px}
.ai-ws{flex:1;text-align:center;font-size:8px;color:var(--muted);position:relative;padding-top:18px}
.ai-ws span{display:inline-flex;width:16px;height:16px;border-radius:50%;border:1px solid var(--border);align-items:center;justify-content:center;font-size:8px;position:absolute;top:0;left:50%;transform:translateX(-50%);background:var(--bg);z-index:1;transition:all .2s}
.ai-ws::after{content:'';position:absolute;top:7px;left:0;right:0;height:1px;background:var(--border);z-index:0}
.ai-ws:first-child::after{left:50%}
.ai-ws:last-child::after{right:50%}
.ai-ws.done{color:var(--green)}
.ai-ws.done span{border-color:var(--green);background:var(--green);color:#1f2328}
.ai-ws.active{color:var(--blue);font-weight:600}
.ai-ws.active span{border-color:var(--blue);box-shadow:0 0 0 2px rgba(88,166,255,.2)}

/* ── Inspiration ── */
.ai-inspiration{margin-top:8px}
.ai-inspiration summary{font-size:10px;color:var(--muted);cursor:pointer}
.ai-inspiration summary:hover{color:#1f2328}
.inspiration-tags{display:flex;gap:4px;flex-wrap:wrap;margin:6px 0}
.inspiration-tags span{font-size:9px;padding:2px 8px;background:var(--bg);border:1px solid var(--border);border-radius:10px;color:var(--muted);cursor:pointer}
.inspiration-tags span:hover{color:#1f2328;border-color:var(--muted)}
.inspiration-hint{font-size:9px;color:var(--muted);font-style:italic}

/* ── Spinner ── */
@keyframes ai-spin{to{transform:rotate(360deg)}}
.ai-spinner{display:inline-block;width:12px;height:12px;border:2px solid rgba(255,255,255,.2);border-top-color:#1f2328;border-radius:50%;animation:ai-spin .6s linear infinite;vertical-align:middle;margin-right:4px}

/* ── Native Strategy ── */
.native-mode-tabs{display:flex;gap:0;margin-bottom:12px;border-radius:6px;overflow:hidden;border:1px solid var(--border)}
.native-mode-tab{flex:1;padding:7px 0;font-size:11px;border:none;background:var(--bg);color:var(--muted);cursor:pointer;transition:all .15s;text-align:center}
.native-mode-tab:hover{color:#1f2328}
.native-mode-tab.active{background:var(--blue);color:#1f2328;font-weight:600}
.native-param-row{display:flex;align-items:center;gap:6px;margin-bottom:3px;font-size:10px}
.native-param-row label{width:50px;flex-shrink:0;color:var(--muted)}
.native-param-row input{width:60px;padding:2px 4px;font-size:10px;margin:0}

/* ── Floating Chat Ball & Panel ── */
#agent-ball{position:fixed;bottom:24px;left:24px;width:52px;height:52px;background:var(--blue);border-radius:50%;cursor:grab;z-index:997;box-shadow:0 4px 16px rgba(31,111,235,.45);display:flex;align-items:center;justify-content:center;user-select:none;touch-action:none}
#agent-ball:active{cursor:grabbing}
#agent-ball.dragging{opacity:.85;transform:scale(1.08);transition:none}
#agent-ball:hover{box-shadow:0 6px 24px rgba(31,111,235,.6)}
#agent-ball svg{width:26px;height:26px;fill:#fff}
#agent-ball .ball-pulse{position:absolute;inset:-4px;border-radius:50%;border:2px solid var(--blue);opacity:0;animation:ballPulse 2s ease-in-out infinite}
@keyframes ballPulse{0%{transform:scale(1);opacity:.6}100%{transform:scale(1.5);opacity:0}}

#agent-panel{position:fixed;bottom:92px;right:24px;width:380px;height:520px;background:var(--card);border:1px solid var(--border);border-radius:12px;box-shadow:0 8px 40px rgba(0,0,0,.5);z-index:996;display:none;flex-direction:column;overflow:hidden}
#agent-panel.open{display:flex}
.agent-panel-header{display:flex;align-items:center;justify-content:space-between;padding:10px 14px;border-bottom:1px solid var(--border);background:var(--bg);flex-shrink:0;cursor:move;user-select:none}
.agent-panel-header .agent-title{font-size:13px;font-weight:700;color:var(--accent);display:flex;align-items:center;gap:6px}
.agent-panel-header .agent-actions{display:flex;gap:4px}
.agent-panel-header .agent-actions button{background:none;border:none;color:var(--muted);cursor:pointer;font-size:16px;padding:2px 6px;border-radius:4px;line-height:1}
.agent-panel-header .agent-actions button:hover{color:#1f2328;background:var(--border)}

#agent-messages{flex:1;overflow-y:auto;padding:12px;display:flex;flex-direction:column;gap:10px}
.agent-msg{max-width:85%;padding:8px 12px;border-radius:10px;font-size:11px;line-height:1.55;word-break:break-word;white-space:pre-wrap}
.agent-msg.user{align-self:flex-end;background:var(--blue);color:#1f2328;border-bottom-right-radius:3px}
.agent-msg.agent{align-self:flex-start;background:var(--bg);border:1px solid var(--border);color:#1f2328;border-bottom-left-radius:3px}
.agent-msg.typing{align-self:flex-start;background:var(--bg);border:1px solid var(--border);color:var(--muted);font-style:italic;display:flex;align-items:center;gap:4px}
.agent-typing-dot{width:5px;height:5px;background:var(--muted);border-radius:50%;animation:typingBounce 1.4s ease-in-out infinite}
.agent-typing-dot:nth-child(2){animation-delay:0.2s}
.agent-typing-dot:nth-child(3){animation-delay:0.4s}
@keyframes typingBounce{0%,60%,100%{transform:translateY(0)}30%{transform:translateY(-4px)}}

#agent-input-row{display:flex;gap:6px;padding:10px 12px;border-top:1px solid var(--border);background:var(--bg);flex-shrink:0}
#agent-input-row textarea{flex:1;resize:none;background:var(--card);border:1px solid var(--border);color:#1f2328;padding:8px 10px;border-radius:8px;font-size:11px;font-family:inherit;height:38px;max-height:80px;line-height:1.4}
#agent-input-row textarea:focus{outline:none;border-color:var(--blue)}
#agent-input-row button{background:var(--blue);color:#1f2328;border:none;border-radius:8px;padding:0 14px;font-size:11px;font-weight:600;cursor:pointer;transition:opacity .15s;white-space:nowrap}
#agent-input-row button:hover{opacity:.85}
#agent-input-row button:disabled{opacity:.4;cursor:not-allowed}

@media(max-width:440px){#agent-panel{width:calc(100vw - 16px);right:8px;height:60vh} #agent-ball{bottom:16px;right:16px}}

/* ── AI Opportunity Page ── */
.aio-card{background:var(--card);border:1px solid var(--border);border-radius:8px;padding:10px 14px;text-align:center}
.aio-card-label{font-size:9px;color:var(--muted);text-transform:uppercase;margin-bottom:4px}
.aio-card-val{font-size:18px;font-weight:700;color:#1f2328}
.aio-card-val.up{color:var(--green)}
.aio-card-val.down{color:var(--red)}

#aio-result .analysis-section{margin-bottom:12px}
#aio-result .analysis-section h3{font-size:13px;font-weight:600;margin:0 0 6px 0;color:var(--accent);border-bottom:1px solid var(--border);padding-bottom:4px}
#aio-result .analysis-section p{font-size:11px;color:var(--muted);line-height:1.6;margin:4px 0}
#aio-result .signal-tag{display:inline-block;padding:2px 8px;border-radius:3px;font-size:10px;font-weight:600;margin:2px}
#aio-result .signal-tag.bullish{background:rgba(35,134,54,.15);color:var(--green);border:1px solid rgba(35,134,54,.3)}
#aio-result .signal-tag.bearish{background:rgba(248,81,73,.15);color:var(--red);border:1px solid rgba(248,81,73,.3)}
#aio-result .signal-tag.neutral{background:rgba(210,153,34,.15);color:var(--yel);border:1px solid rgba(210,153,34,.3)}

.aio-quick-btn{background:var(--bg);border:1px solid var(--border);color:var(--muted);padding:4px 10px;border-radius:14px;font-size:10px;cursor:pointer;transition:all .15s;white-space:nowrap}
.aio-quick-btn:hover{color:#1f2328;border-color:var(--blue);background:rgba(31,111,235,.08)}

/* AIO Chat Messages */
.aio-msg{display:flex;margin-bottom:2px}
.aio-msg.user{justify-content:flex-end}
.aio-msg.assistant{justify-content:flex-start}
.aio-msg .aio-msg-content{max-width:90%;padding:8px 12px;border-radius:10px;font-size:11px;line-height:1.55;word-break:break-word}
.aio-msg.user .aio-msg-content{background:var(--blue);color:#1f2328;border-bottom-right-radius:3px}
.aio-msg.assistant .aio-msg-content{background:var(--bg);border:1px solid var(--border);color:#1f2328;border-bottom-left-radius:3px}

/* Typing dots animation */
.typing-dots{display:inline-flex;gap:3px;margin-left:4px;vertical-align:middle}
.typing-dots::before,.typing-dots::after{content:'';width:4px;height:4px;background:var(--muted);border-radius:50%;animation:tdBounce 1.4s ease-in-out infinite}
.typing-dots::before{content:'.';animation-delay:0.2s;font-size:0;width:4px;height:4px;background:var(--muted);border-radius:50%;display:inline-block}
.typing-dots{content:'. . .';letter-spacing:2px}
@keyframes tdBounce{0%,60%,100%{opacity:.2}30%{opacity:1}}

/* ── Floating Account Widget (bottom-left) ── */
.account-float-widget{position:fixed;bottom:12px;left:12px;width:170px;background:var(--card);border:1px solid var(--border);border-radius:8px;z-index:990;box-shadow:0 2px 12px rgba(0,0,0,.3);transition:transform .2s}
.account-float-widget.collapsed .afw-body{display:none}
.account-float-widget.collapsed{border-radius:8px}
.afw-header{padding:5px 12px;cursor:pointer;display:flex;justify-content:space-between;align-items:center;border-bottom:1px solid var(--border);font-size:10px;user-select:none}
.afw-header:hover{background:var(--hover)}
.afw-body{padding:8px 10px;font-size:10px}
.afw-body .kpi-card{padding:6px 8px}
.afw-body .klabel{font-size:9px}
.afw-body .kvalue{font-size:14px}

/* ── AI Page Top Bar ── */
.ai-topbar{display:flex;align-items:center;justify-content:space-between;padding:8px 12px;background:var(--card);border-bottom:1px solid var(--border);flex-shrink:0}
.ai-topbar-left{display:flex;align-items:center;gap:8px}
.ai-symbol-badge{font-size:14px;font-weight:700;color:#e6edf3}
.ai-symbol-name{font-size:10px;color:var(--muted);max-width:80px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.ai-symbol-chevron{font-size:10px;color:var(--muted);margin-left:1px;transition:transform .2s;line-height:1}
.ai-exchange-tag{font-size:9px;background:rgba(56,139,253,.12);color:var(--blue);padding:2px 8px;border-radius:4px;font-weight:500}
.ai-symbol-picker{display:flex;align-items:center;gap:5px;cursor:pointer;padding:4px 8px;border-radius:8px;transition:all .15s;border:1px solid transparent;background:none;font-family:inherit;color:inherit}
.ai-symbol-picker:hover{background:var(--bg);border-color:var(--border)}
.ai-exchange-selector{display:flex;align-items:center;gap:2px;cursor:pointer;padding:3px 6px;border-radius:6px;transition:all .15s;border:1px solid transparent;background:none;font-family:inherit;color:inherit}
.ai-exchange-selector:hover{background:var(--bg);border-color:var(--border)}
/* ── Symbol Search Dropdown (Redesigned) ── */
.sym-search-overlay{position:fixed;top:0;left:0;right:0;bottom:0;z-index:9999;background:transparent}
.sym-search-dropdown{position:fixed;z-index:10000;background:#ffffff;border:1px solid #d0d7de;border-radius:12px;width:300px;box-shadow:0 12px 40px rgba(0,0,0,.6);overflow:hidden;animation:symFadeIn .15s ease}
@keyframes symFadeIn{from{opacity:0;transform:translateY(-4px)}to{opacity:1;transform:translateY(0)}}
.sym-search-header{padding:10px 12px;border-bottom:1px solid #eaeef2}
.sym-search-input-wrap{display:flex;align-items:center;gap:8px;background:#f6f8fa;border:1px solid #d0d7de;border-radius:8px;padding:6px 10px;transition:border-color .2s}
.sym-search-input-wrap:focus-within{border-color:var(--blue)}
.sym-search-icon{color:var(--muted);font-size:14px;flex-shrink:0}
.sym-search-input{flex:1;background:none;border:none;color:#e6edf3;font-size:12px;font-family:inherit;outline:none;min-width:0}
.sym-search-input::placeholder{color:#484f58}
.sym-search-hot{padding:8px 12px;border-bottom:1px solid #eaeef2}
.sym-search-hot-label{font-size:9px;color:#484f58;text-transform:uppercase;letter-spacing:.5px;margin-bottom:6px}
.sym-hot-chips{display:flex;flex-wrap:wrap;gap:4px}
.sym-hot-chip{font-size:10px;padding:3px 8px;background:#f6f8fa;border:1px solid #d0d7de;border-radius:12px;color:#1f2328;cursor:pointer;transition:all .15s;font-weight:500}
.sym-hot-chip:hover{background:rgba(56,139,253,.12);border-color:var(--blue);color:var(--blue)}
.sym-hot-chip.active{background:rgba(56,139,253,.18);border-color:var(--blue);color:var(--blue)}
.sym-search-list-header{padding:6px 12px;font-size:9px;color:#484f58;text-transform:uppercase;letter-spacing:.5px;border-bottom:1px solid rgba(48,54,61,.3)}
.sym-search-list{max-height:240px;overflow-y:auto}
.sym-search-item{display:flex;align-items:center;gap:10px;padding:7px 12px;cursor:pointer;transition:background .08s}
.sym-search-item:hover{background:rgba(56,139,253,.06)}
.sym-search-item.active{background:rgba(56,139,253,.12)}
.sym-search-item .sym-dot{width:6px;height:6px;border-radius:50%;flex-shrink:0;opacity:.7}
.sym-search-item .sym-pair{font-size:12px;font-weight:600;color:#e6edf3;min-width:75px;letter-spacing:.3px}
.sym-search-item .sym-pair .sym-quote{color:var(--muted);font-weight:400}
.sym-search-item .sym-coin-name{font-size:10px;color:var(--muted);flex:1}
.sym-search-item .sym-exchange{font-size:8px;color:#484f58;text-transform:uppercase}
.sym-search-empty{padding:24px;text-align:center;color:var(--muted);font-size:11px}
.sym-search-empty .sym-empty-icon{font-size:28px;margin-bottom:6px;opacity:.6}
/* Exchange dropdown (redesigned) */
.ex-dropdown{position:fixed;z-index:10000;background:#ffffff;border:1px solid #d0d7de;border-radius:10px;width:190px;box-shadow:0 12px 40px rgba(0,0,0,.6);overflow:hidden;animation:symFadeIn .15s ease}
.ex-dropdown-header{padding:8px 12px;font-size:9px;color:#484f58;text-transform:uppercase;letter-spacing:.5px;border-bottom:1px solid #eaeef2}
.ex-dropdown-item{display:flex;align-items:center;justify-content:space-between;padding:8px 12px;cursor:pointer;font-size:11px;color:#1f2328;transition:background .08s}
.ex-dropdown-item:hover{background:rgba(56,139,253,.06)}
.ex-dropdown-item.active{background:rgba(56,139,253,.12);color:#1f2328}
.ex-dropdown-item .ex-dot{width:6px;height:6px;border-radius:50%;flex-shrink:0;margin-right:8px}
.ex-dropdown-item .ex-dot.connected{background:var(--green)}
.ex-dropdown-item .ex-dot.disconnected{background:var(--red)}
.ex-dropdown-item .ex-name{flex:1;font-weight:500}
.ex-dropdown-item .ex-badge{font-size:8px;padding:1px 6px;border-radius:8px;font-weight:500}
.ex-dropdown-item .ex-badge.live{background:rgba(63,185,80,.15);color:var(--green)}
.ex-dropdown-item .ex-badge.test{background:rgba(210,168,255,.15);color:#d2a8ff}
.ex-dropdown-item .ex-badge.nokeys{background:rgba(248,81,73,.12);color:var(--red)}
.ai-interval-bar{display:flex;gap:2px;background:var(--bg);border-radius:6px;padding:2px}
.ai-int-btn{background:none;border:none;color:var(--muted);padding:4px 10px;font-size:10px;font-weight:600;cursor:pointer;border-radius:4px;transition:all .15s}
.ai-int-btn:hover{color:#1f2328}
.ai-int-btn.active{background:var(--blue);color:#1f2328}

/* ── AI Page Main Layout ── */
.ai2-main{display:flex;gap:0;flex:1;min-height:0;overflow:hidden}
.ai2-left{width:420px;flex-shrink:0;display:flex;flex-direction:column;border-right:1px solid var(--border);overflow:hidden}
.ai2-code-panel{flex:1;overflow-y:auto;background:var(--card);display:flex;flex-direction:column}
.ai2-code-header{display:flex;justify-content:space-between;align-items:center;padding:6px 12px;border-bottom:1px solid var(--border);background:var(--bg);flex-shrink:0}
#ai2-code-display{flex:1;overflow:auto;padding:10px 12px;font-size:10px;font-family:'Cascadia Code',Consolas,monospace;line-height:1.5;color:#1f2328;margin:0;white-space:pre-wrap;word-break:break-all;background:#f6f8fa;counter-reset:line}
#ai2-code-display .code-comment{color:#8b949e;font-style:italic}
#ai2-code-display .code-keyword{color:#ff7b72}
#ai2-code-display .code-string{color:#a5d6ff}
#ai2-code-display .code-number{color:#79c0ff}
#ai2-code-display .code-decorator{color:#d2a8ff}
#ai2-code-display .code-type{color:#ffa657}
#ai2-code-display .code-builtin{color:#7ee787}

.ai2-prompt-panel{padding:8px 12px;border-top:1px solid var(--border);background:var(--card);flex-shrink:0}

.ai2-chart-panel{flex:1;display:flex;flex-direction:column;background:var(--card);min-width:0;overflow:hidden}
.ai2-chart-header{display:flex;align-items:center;gap:12px;padding:6px 12px;border-bottom:1px solid var(--border);background:var(--bg);flex-shrink:0}

/* ── AI Page Bottom: Backtest ── */
.ai2-bottom{flex-shrink:0;border-top:1px solid var(--border);background:var(--card)}
.ai2-bt-tabs{display:flex;gap:0;border-bottom:1px solid var(--border);padding:0 12px}
.ai2-bt-tab{padding:6px 14px;font-size:11px;font-weight:600;color:var(--muted);cursor:pointer;border-bottom:2px solid transparent;transition:all .15s}
.ai2-bt-tab:hover{color:#1f2328}
.ai2-bt-tab.active{color:var(--blue);border-bottom-color:var(--blue)}
.ai2-bt-content{display:none;padding:10px 12px}
.ai2-bt-content.active{display:block}
.ai2-bt-field{display:flex;flex-direction:column;gap:2px}
.ai2-bt-field label{font-size:9px;color:var(--muted);text-transform:uppercase}
.ai2-bt-field input,.ai2-bt-field select{font-size:11px;padding:3px 6px;background:var(--bg);border:1px solid var(--border);color:#1f2328;border-radius:4px}
.ai2-bt-field input[type="date"]{color-scheme:dark}
.ai2-strat-list{max-height:240px;overflow-y:auto}
.ai2-strategies-section{border-top:1px solid var(--border);background:var(--card)}
.ai2-section-header{display:flex;align-items:center;justify-content:space-between;padding:8px 12px;border-bottom:1px solid var(--border);background:var(--bg)}
.ai2-strategy-list{padding:0;max-height:260px;overflow-y:auto}
.ai2-strategy-empty{padding:20px 12px;text-align:center;font-size:10px;color:var(--muted)}
.ai2-strategy-item{display:flex;align-items:center;justify-content:space-between;padding:8px 12px;border-bottom:1px solid var(--border);transition:background .12s}
.ai2-strategy-item:hover{background:var(--bg)}
.ai2-strategy-item:last-child{border-bottom:none}
.ai2-strat-info{flex:1;min-width:0}
.ai2-strat-name{font-size:11px;font-weight:600;color:#1f2328;display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.ai2-strat-meta{font-size:9px;color:var(--muted);display:block;margin-top:2px}
.ai2-strat-metric{font-size:9px;margin-right:8px;font-weight:500}
.ai2-strat-actions{display:flex;gap:4px;flex-shrink:0}

/* ── AI3 Multi-Model Analysis Layout ── */
.ai3-layout{display:flex;gap:0;flex:1;min-height:0;overflow:hidden}
.ai3-model-panel{width:15%;min-width:200px;flex-shrink:0;background:var(--card);border-right:1px solid var(--border);overflow-y:auto;padding:10px;display:flex;flex-direction:column}
.ai3-section-title{font-size:9px;font-weight:700;color:var(--muted);text-transform:uppercase;margin:10px 0 5px;letter-spacing:.5px}
.ai3-section-title:first-child{margin-top:0}
.ai3-model-item{display:flex;align-items:center;gap:7px;padding:5px 7px;border-radius:5px;margin-bottom:3px;background:var(--bg);cursor:pointer;transition:all .15s;border:1px solid transparent}
.ai3-model-item:hover{background:#1a2332;border-color:#d0d7de}
.ai3-model-item.disabled{opacity:.45}
.ai3-model-color{width:8px;height:8px;border-radius:50%;flex-shrink:0}
.ai3-model-name{font-size:10px;font-weight:600;color:#1f2328;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.ai3-model-status{font-size:8px;padding:1px 5px;border-radius:6px;flex-shrink:0}
.ai3-model-status.ok{background:rgba(63,185,80,.2);color:#3fb950}
.ai3-model-status.warn{background:rgba(210,153,34,.2);color:#d2991d}
.ai3-model-weight{display:none;margin-top:3px}
.ai3-model-item.enabled .ai3-model-weight{display:block}
.ai3-weight-slider{width:100%;height:3px;-webkit-appearance:none;appearance:none;background:#eaeef2;border-radius:2px;outline:none}
.ai3-weight-slider::-webkit-slider-thumb{-webkit-appearance:none;width:10px;height:10px;border-radius:50%;background:var(--accent);cursor:pointer}
.ai3-preset-bar{display:flex;gap:4px;margin-top:6px;flex-wrap:wrap}
.ai3-preset-btn{font-size:8px;padding:2px 7px;border-radius:3px;border:1px solid var(--border);background:var(--bg);color:var(--muted);cursor:pointer;transition:all .15s}
.ai3-preset-btn:hover{color:#1f2328;border-color:#58a6ff}
.ai3-chart-area{flex:1;display:flex;flex-direction:column;background:var(--card);min-width:0;overflow:hidden}
.ai3-consensus{display:flex;gap:10px;align-items:center;padding:6px 12px;border-top:1px solid var(--border);background:var(--bg);flex-shrink:0}
.ai3-consensus-label{font-size:10px;font-weight:700;color:var(--muted);white-space:nowrap}
.ai3-consensus-val{font-size:12px;font-weight:700}
.ai3-consensus-val.bullish{color:#3fb950}
.ai3-consensus-val.bearish{color:#f85149}
.ai3-consensus-val.neutral{color:#d2991d}
.ai3-vote-bar-wrap{flex:1;height:5px;border-radius:3px;background:#eaeef2;display:flex;overflow:hidden;min-width:70px}
.ai3-vote-bar-bullish{background:#3fb950;height:100%;transition:width .4s}
.ai3-vote-bar-bearish{background:#f85149;height:100%;transition:width .4s}
.ai3-vote-bar-neutral{background:#484f58;height:100%;transition:width .4s}
.ai3-quick-prompts{display:flex;gap:5px;padding:5px 12px;flex-wrap:wrap;flex-shrink:0}
.ai3-qp-btn{font-size:9px;padding:2px 8px;border-radius:10px;border:1px solid var(--border);background:var(--bg);color:var(--muted);cursor:pointer;transition:all .15s}
.ai3-qp-btn:hover{color:#1f2328;border-color:var(--accent)}
.ai3-chat-area{width:25%;min-width:270px;flex-shrink:0;background:var(--card);border-left:1px solid var(--border);display:flex;flex-direction:column;overflow:hidden}
.ai3-chat-messages{flex:1;overflow-y:auto;padding:8px}
.ai3-chat-empty{text-align:center;color:var(--muted);font-size:10px;padding:20px 0}
.ai3-chat-bubble{margin-bottom:6px;padding:7px 9px;border-radius:7px;font-size:10px;line-height:1.5;background:var(--bg);border-left:3px solid transparent}
.ai3-chat-bubble-header{display:flex;align-items:center;gap:5px;margin-bottom:3px}
.ai3-chat-bubble-name{font-weight:700;font-size:10px}
.ai3-chat-bubble-sentiment{font-size:8px;padding:1px 5px;border-radius:6px;margin-left:auto}
.ai3-chat-bubble-sentiment.bullish{background:rgba(63,185,80,.2);color:#3fb950}
.ai3-chat-bubble-sentiment.bearish{background:rgba(248,81,73,.2);color:#f85149}
.ai3-chat-bubble-sentiment.neutral{background:rgba(139,148,158,.2);color:#8b949e}
.ai3-chat-bubble-body{color:#8b949e}
.ai3-autotrade{padding:8px 12px;border-top:1px solid var(--border);background:var(--bg);flex-shrink:0}
.ai3-at-row{display:flex;align-items:center;gap:6px;margin-bottom:5px}
.ai3-at-row label{font-size:9px;color:var(--muted);white-space:nowrap}
.ai3-at-row input[type="range"]{flex:1;height:3px;-webkit-appearance:none;appearance:none;background:#eaeef2;border-radius:2px;outline:none}
.ai3-at-row input[type="range"]::-webkit-slider-thumb{-webkit-appearance:none;width:10px;height:10px;border-radius:50%;background:var(--accent);cursor:pointer}
.ai3-at-row input[type="number"]{width:50px;font-size:9px;padding:2px 4px;background:var(--bg);border:1px solid var(--border);color:#1f2328;border-radius:4px}
.ai3-chat-input{display:flex;gap:5px;padding:7px 10px;border-top:1px solid var(--border);flex-shrink:0}
.ai3-chat-input textarea{flex:1;background:var(--bg);border:1px solid var(--border);color:#1f2328;padding:5px 7px;border-radius:6px;font-size:10px;resize:none;font-family:inherit;height:30px}
.ai3-chat-input textarea:focus{outline:none;border-color:var(--accent)}
.ai3-btn{font-size:10px;padding:4px 10px;border-radius:5px;border:1px solid var(--border);background:var(--accent);color:#1f2328;cursor:pointer;font-weight:600;white-space:nowrap;transition:all .15s}
.ai3-btn:hover{opacity:.85}
.ai3-btn:disabled{opacity:.4;cursor:not-allowed}
.ai3-save-btn{font-size:9px;padding:3px 8px;border-radius:4px;border:1px solid var(--border);background:var(--bg);color:var(--muted);cursor:pointer;white-space:nowrap;margin-top:6px}
.ai3-save-btn:hover{color:#1f2328;border-color:var(--green)}

.ai2-strat-item{display:flex;align-items:center;gap:10px;padding:8px 10px;border:1px solid var(--border);border-radius:6px;margin-bottom:6px;background:var(--bg);cursor:pointer;transition:all .12s}
.ai2-strat-item:hover{border-color:var(--blue);background:#ffffff}
.ai2-strat-item .strat-info{flex:1;min-width:0}
.ai2-strat-item .strat-name{font-size:12px;font-weight:600;color:#e6edf3;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.ai2-strat-item .strat-meta{font-size:9px;color:var(--muted);margin-top:2px}
.ai2-strat-item .strat-meta span{margin-right:10px}
.ai2-strat-item .strat-actions{display:flex;gap:4px;flex-shrink:0}
.ai2-strat-empty{text-align:center;padding:20px;color:var(--muted);font-size:11px}

/* BT Metric Cards */
.bt-metric{background:var(--bg);border-radius:6px;padding:8px;text-align:center}
.bt-m-label{font-size:8px;color:var(--muted);text-transform:uppercase;margin-bottom:2px}
.bt-m-val{font-size:15px;font-weight:700;color:#1f2328}
.bt-m-val.g{color:var(--green)}
.bt-m-val.r{color:var(--red)}
</style>
</head>
<body>

<div id="topbar">
  <div style="display:flex;align-items:center;gap:18px">
    <h1>小天量化 v2.0</h1>
    <div class="nav">
      <a class="active" data-page="dash" data-i18n="nav.dash">Dashboard</a>
      <a data-page="trade" data-i18n="nav.trade">Trading</a>
      <a data-page="ai_oppty" data-i18n="nav.ai_oppty">AI分析</a>
      <a data-page="ai_generate" data-i18n="nav.ai_generate">Agent策略生成</a>
      <a data-page="backtest" data-i18n="nav.backtest">Backtest</a>
      <a data-page="strategy" data-i18n="nav.strategy">Strategies</a>
      
    </div>
  </div>
  <div class="status-row">
    <span><span class="dot g" id="ws-dot"></span> <span id="ws-text" data-i18n="label.connecting">connecting</span></span>
    <span>Equity <b id="hdr-equity">--</b></span>
    <span>P&L <b id="hdr-pnl">--</b></span>
    <span id="hdr-time"></span>
    <button id="lang-toggle" onclick="toggleLang()" style="background:transparent;border:1px solid var(--border);color:var(--muted);padding:2px 8px;border-radius:4px;font-size:10px;cursor:pointer">EN</button>
	    <button id="settings-icon-btn" onclick="navToPage('settings')" title="Settings" style="background:transparent;border:1px solid var(--border);color:var(--muted);padding:2px 8px;border-radius:4px;font-size:14px;cursor:pointer;line-height:1;transition:color .15s" onmouseenter="this.style.color='#1f2328'" onmouseleave="this.style.color='var(--muted)'">&#9881;</button>
  </div>
</div>

<div id="app">
<!-- ═══════════════════════════════════════════ DASHBOARD ═══════════════════════════════════════════ -->
<div class="page active" id="pg-dash">
<div style="display:flex;flex-direction:column;flex:1;overflow-y:auto;padding:12px;gap:10px">
  <div class="dash-kpi-row" style="display:grid;grid-template-columns:repeat(6,1fr);gap:8px">
    <div class="dash-kpi-card kpi-primary"><div class="dk-label">Total Equity</div><div class="dk-value" id="dk-equity">--</div><div class="dk-sub">PnL <span id="dk-pnl">--</span></div></div>
    <div class="dash-kpi-card kpi-winrate" style="position:relative"><div class="dk-label">Win Rate</div><div class="dk-value" id="dk-winrate">--</div><div class="dk-sub"><span id="dk-wins">--</span>W / <span id="dk-losses">--</span>L</div><div class="dk-ring"><svg viewBox="0 0 36 36"><path class="dk-ring-bg" d="M18 2.0845 a 15.9155 15.9155 0 0 1 0 31.831 a 15.9155 15.9155 0 0 1 0 -31.831"/><path class="dk-ring-fg" id="dk-ring-fg" stroke-dasharray="0,100" d="M18 2.0845 a 15.9155 15.9155 0 0 1 0 31.831 a 15.9155 15.9155 0 0 1 0 -31.831"/></svg></div></div>
    <div class="dash-kpi-card kpi-factor"><div class="dk-label">P/F Ratio</div><div class="dk-value" id="dk-pf">--</div><div class="dk-sub">Avg Win <span id="dk-avgwin">--</span></div></div>
    <div class="dash-kpi-card kpi-dd"><div class="dk-label">Max Drawdown</div><div class="dk-value dd-val" id="dk-maxdd">--</div><div class="dk-sub">$<span id="dk-dd-amt">--</span></div></div>
    <div class="dash-kpi-card kpi-trades"><div class="dk-label">Total Trades</div><div class="dk-value" id="dk-trades">--</div><div class="dk-sub">Avg <span id="dk-avgdaily">--</span>/day</div></div>
    <div class="dash-kpi-card kpi-strats clickable" onclick="navToPage('"strategy"')"><div class="dk-label">Running</div><div class="dk-value" id="dk-running">--</div><div class="dk-sub"><span id="dk-totalstrats">--</span> total</div></div>
  </div>
  <div style="display:grid;grid-template-columns:2fr 1fr;gap:8px">
    <div class="dash-widget"><div class="dw-header">Equity Curve + Daily PnL</div><div id="dash-equity-echart" style="height:260px"></div></div>
    <div class="dash-widget"><div class="dw-header">Strategy PnL</div><div id="dash-pie-echart" style="height:260px"></div></div>
  </div>
  <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px">
    <div class="dash-widget"><div class="dw-header">Drawdown Curve</div><div id="dash-dd-echart" style="height:200px"></div></div>
    <div class="dash-widget"><div class="dw-header">Trade Hours</div><div id="dash-hourly-echart" style="height:200px"></div></div>
  </div>
  <!-- Calendar + Strategy Ranking -->
  <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px">
    <div class="dash-widget">
      <div class="dw-header" style="display:flex;justify-content:space-between;align-items:center">
        <span>Profit Calendar</span>
        <span style="display:flex;gap:4px;align-items:center">
          <button onclick="dashPrevMonth()" style="background:transparent;border:none;color:var(--blue);cursor:pointer;font-size:14px">&lt;</button>
          <span id="dash-cal-label" style="font-size:11px">--</span>
          <button onclick="dashNextMonth()" style="background:transparent;border:none;color:var(--blue);cursor:pointer;font-size:14px">&gt;</button>
        </span>
      </div>
      <div id="dash-calendar" style="min-height:180px;font-size:10px"></div>
    </div>
    <div class="dash-widget" style="max-height:280px;overflow-y:auto">
      <div class="dw-header">Strategy Ranking</div>
      <div id="dash-ranking"></div>
    </div>
  </div>
</div>
</div>

<!-- ═══════════════════════════════════════════ TRADING ═══════════════════════════════════════════ -->
<div class="page" id="pg-trade" style="flex-direction:column">
  <!-- ═══ MAIN 3-COLUMN GRID (Chart | Orderbook | TradePanel) ═══ -->
  <div class="trade-layout" id="trade-grid">
    <!-- COL 1: CHART ─────────────────────────────── -->
    <div class="trade-chart-panel">
      <div id="trade-chart" style="flex:1;min-height:0;background:#ffffff"></div>
    </div>

    <!-- COL 2: ORDERBOOK ─────────────────────────── -->
    <div class="trade-ob-panel">
      <div class="ob-tabs">
        <span class="ob-tab active" onclick="switchOBTab('book')">订单簿</span>
        <span class="ob-tab" onclick="switchOBTab('trades')">成交</span>
      </div>
      <div class="ob-header" id="ob-book-header"><span>价格</span><span>数量</span><span>累计</span></div>
      <div id="ob-asks" style="flex:1;overflow:hidden;min-height:0"></div>
      <div class="ob-spread-bar">
        <span id="ob-price-val" class="price" style="color:var(--green)">--</span>
        <span id="ob-spread-val" class="spread">价差 --</span>
      </div>
      <div id="ob-bids" style="flex:1;overflow:hidden;min-height:0"></div>
      <div id="ob-trades-view" style="flex:1;overflow:hidden;font-size:10px;font-family:'Cascadia Code',monospace;display:none">
        <div class="ob-header"><span>价格</span><span>数量</span><span>时间</span></div>
        <div id="recent-trades" style="overflow:auto;flex:1"><div class="m" style="text-align:center;padding:16px">等待数据...</div></div>
      </div>
    </div>

    <!-- COL 3: TRADE PANEL ───────────────────────── -->
    <div class="trade-form-panel">
      <!-- Buy/Sell Toggle -->
      <div class="bn-toggle">
        <button class="bn-buy-btn active" onclick="setTradeSide('BUY')" id="tf-buy-btn">买入</button>
        <button class="bn-sell-btn" onclick="setTradeSide('SELL')" id="tf-sell-btn">卖出</button>
      </div>

      <!-- Available Balance -->
      <div class="tf-balance">
        <span>可用</span>
        <b id="tf-available">0.00 USDT</b>
      </div>

      <!-- Order Type Tabs -->
      <div class="trade-tabs">
        <span class="trade-tab active" data-tt="LIMIT" onclick="setTradeType('LIMIT')">限价</span>
        <span class="trade-tab" data-tt="MARKET" onclick="setTradeType('MARKET')">市价</span>
        <span class="trade-tab" data-tt="STOP" onclick="setTradeType('STOP')">止损</span>
      </div>
      <input type="hidden" id="tf-type" value="LIMIT">

      <!-- Price -->
      <div class="tf-input-wrap" id="tf-price-wrap">
        <div class="tf-label"><span>价格</span><span class="coin">USDT</span></div>
        <div style="display:flex;gap:4px">
          <input class="tf-input" id="tf-price" placeholder="0.00" oninput="updateOrderPreview()" style="flex:1">
          <button class="best-btn bid" onclick="fillBestBid()" title="Best Bid">Bid</button>
          <button class="best-btn ask" onclick="fillBestAsk()" title="Best Ask">Ask</button>
        </div>
      </div>

      <!-- Quantity -->
      <div class="tf-input-wrap">
        <div class="tf-label"><span>数量</span><span class="coin" id="tf-qty-coin">BTC</span></div>
        <input class="tf-input" id="tf-qty" placeholder="0.000" oninput="updateOrderPreview()">
      </div>

      <!-- Percentage Quick Buttons -->
      <div class="pct-btns">
        <button class="pct-btn" onclick="setQtyPct(25)">25%</button>
        <button class="pct-btn" onclick="setQtyPct(50)">50%</button>
        <button class="pct-btn" onclick="setQtyPct(75)">75%</button>
        <button class="pct-btn" onclick="setQtyPct(100)">100%</button>
      </div>

      <!-- Order Preview -->
      <div class="tf-preview" id="tf-preview" style="display:none">
        <div class="tf-preview-row"><span>成交金额</span><span class="pv-val" id="tf-pv-total">--</span></div>
        <div class="tf-preview-row divider"><span>手续费 (0.1%)</span><span class="pv-val" id="tf-pv-fee">--</span></div>
      </div>

      <!-- Submit -->
      <button class="bn-submit-btn buy" onclick="placeOrder()" id="tf-submit-btn">买入 BTC</button>
      <div id="tf-msg" style="font-size:10px;margin-top:8px;text-align:center"></div>
    </div>
  </div>

  <!-- ═══ BOTTOM PANEL: Orders / History / Positions / Trades ═══ -->
  <div class="trade-bottom" id="trade-bottom" style="height:200px">
    <div class="bottom-resize-handle" id="bottom-resize" onmousedown="startBottomResize(event)"></div>
    <div class="trade-tabs">
      <span class="trade-tab active" data-tab="orders" onclick="switchTradeTab('orders')">当前委托 <span id="t-order-tab-count" style="color:var(--muted)">0</span></span>
      <span class="trade-tab" data-tab="history" onclick="switchTradeTab('history')">历史委托</span>
      <span class="trade-tab" data-tab="positions" onclick="switchTradeTab('positions')">持仓</span>
      <span class="trade-tab" data-tab="trades" onclick="switchTradeTab('trades')">成交记录</span>
    </div>
    <!-- Orders -->
    <div id="trade-tab-orders" class="trade-bottom-content">
      <table style="width:100%"><thead><tr><th>时间</th><th>交易对</th><th>方向</th><th>价格</th><th>数量</th><th>成交</th><th>状态</th><th></th></tr></thead>
      <tbody id="t-orders"><tr><td colspan="8" class="m" style="text-align:center;padding:16px">暂无活跃订单</td></tr></tbody></table>
    </div>
    <!-- History -->
    <div id="trade-tab-history" class="trade-bottom-content" style="display:none">
      <table style="width:100%"><thead><tr><th>时间</th><th>交易对</th><th>方向</th><th>价格</th><th>数量</th><th>状态</th></tr></thead>
      <tbody id="t-history"><tr><td colspan="6" class="m" style="text-align:center;padding:16px">暂无历史记录</td></tr></tbody></table>
    </div>
    <!-- Positions -->
    <div id="trade-tab-positions" class="trade-bottom-content" style="display:none">
      <div id="t-positions-detailed" style="display:flex;gap:8px;overflow-x:auto;padding:4px 0">
        <div class="m" style="text-align:center;padding:20px;width:100%">暂无持仓</div>
      </div>
    </div>
    <!-- Trade History -->
    <div id="trade-tab-trades" class="trade-bottom-content" style="display:none">
      <table style="width:100%"><thead><tr><th>时间</th><th>交易对</th><th>方向</th><th>价格</th><th>数量</th><th>盈亏</th></tr></thead>
      <tbody id="t-trades-body"><tr><td colspan="6" class="m" style="text-align:center;padding:16px">暂无成交记录</td></tr></tbody></table>
    </div>
    <div class="trade-bottom-bar">
      <span style="color:var(--muted)" id="t-order-count">0 个订单</span>
      <button class="btn btn-xs btn-r" onclick="cancelAllOrders()">撤销全部</button>
    </div>
  </div>
</div>

<!-- ═══════════════════════════════════════════ AI OPPORTUNITY ═══════════════════════════════════════════ -->
  <div class="page" id="pg-ai_oppty">
  <div style="display:flex;gap:12px;flex:1;overflow:hidden;padding:12px">
    <!-- LEFT: Model Management Panel -->
    <div class="ai3-model-panel" id="ai3-model-panel" style="margin:0;border-radius:8px">
      <div class="ai3-section-title">AI模型管理</div>
      <div id="ai3-model-list">加载中...</div>
      <div class="ai3-preset-bar">
        <button class="ai3-preset-btn" onclick="applyAI3Preset('all')">全部</button>
        <button class="ai3-preset-btn" onclick="applyAI3Preset('top3')">Top3</button>
        <button class="ai3-preset-btn" onclick="applyAI3Preset('chinese')">国产</button>
        <button class="ai3-preset-btn" onclick="applyAI3Preset('fast')">极速</button>
      </div>
      <button class="ai3-save-btn" onclick="saveAI3ModelConfig()">保存配置</button>
    </div>

    <!-- CENTER: Chart + Consensus -->
    <div style="flex:1;display:flex;flex-direction:column;gap:10px;min-width:0;overflow-y:auto">
      <!-- Symbol Selector Bar -->
      <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
        <span style="font-size:12px;font-weight:600;white-space:nowrap">AI市场分析</span>
        <select id="aio-symbol" onchange="refreshAIOSnapshot();loadAI3Chart()" style="width:130px">
          <option>BTCUSDT</option><option>ETHUSDT</option><option>BNBUSDT</option><option>SOLUSDT</option>
          <option>DOGEUSDT</option><option>XRPUSDT</option><option>ADAUSDT</option><option>AVAXUSDT</option>
        </select>
        <select id="aio-interval" onchange="refreshAIOSnapshot();loadAI3Chart()" style="width:90px">
          <option value="15m">15分钟</option><option value="1h" selected>1小时</option><option value="4h">4小时</option><option value="1d">日线</option>
        </select>
        <button class="btn btn-g" onclick="startAI3Analysis()" id="aio-analyze-btn" style="padding:6px 14px;font-size:12px">多模型分析</button>
        <button class="btn" onclick="runQuickScan()" id="aio-scan-btn" style="padding:6px 14px;font-size:12px">快速扫描</button>
        <span id="aio-status" style="font-size:10px;color:var(--muted)"></span>
      </div>

      <!-- K-line Chart -->
      <div class="ai3-chart-area" style="flex:1;min-height:300px;border-radius:8px">
        <div class="ai2-chart-header">
          <span style="font-weight:600;font-size:11px" id="ai3-chart-title">BTC/USDT <span id="ai3-chart-interval">1H</span></span>
          <span style="font-size:10px;color:var(--green)" id="ai3-chart-price">--</span>
          <span id="ai3-chart-demo-badge" style="display:none;font-size:10px;color:#d2991d;margin-left:8px">模拟数据</span>
        </div>
        <canvas id="ai3-canvas" style="width:100%;flex:1;min-height:0"></canvas>
      </div>

      <!-- Consensus summary bar -->
      <div class="ai3-consensus" id="ai3-consensus-bar" style="border-radius:8px">
        <span class="ai3-consensus-label">共识:</span>
        <span class="ai3-consensus-val neutral" id="ai3-consensus-val">--</span>
        <div class="ai3-vote-bar-wrap" id="ai3-vote-bar">
          <div class="ai3-vote-bar-bullish" style="width:0%"></div>
          <div class="ai3-vote-bar-bearish" style="width:0%"></div>
          <div class="ai3-vote-bar-neutral" style="width:100%"></div>
        </div>
        <span style="font-size:9px;color:var(--muted)" id="ai3-confidence-span">置信度 --</span>
        <span style="font-size:9px;color:var(--muted)" id="ai3-divergence-span">分歧 --</span>
      </div>

      <!-- Market Snapshot Cards -->
      <div id="aio-snapshot" style="display:grid;grid-template-columns:repeat(4,1fr);gap:8px">
        <div class="aio-card"><div class="aio-card-label">当前价格</div><div class="aio-card-val" id="aio-price">--</div></div>
        <div class="aio-card"><div class="aio-card-label">24h涨跌</div><div class="aio-card-val" id="aio-change">--</div></div>
        <div class="aio-card"><div class="aio-card-label">24h成交量</div><div class="aio-card-val" id="aio-volume">--</div></div>
        <div class="aio-card"><div class="aio-card-label">波动率(ATR)</div><div class="aio-card-val" id="aio-atr">--</div></div>
      </div>

      <!-- AI Analysis Result -->
      <div id="aio-result" style="background:var(--card);border:1px solid var(--border);border-radius:8px;padding:14px;overflow-y:auto;min-height:120px">
        <div style="color:var(--muted);text-align:center;padding:20px 0">
          <div style="font-size:32px;margin-bottom:8px">&#x1F52C;</div>
          <div>选择交易对，点击"多模型分析"获取AI交易机会分析</div>
          <div style="font-size:10px;margin-top:4px;color:var(--border)">分析内容包括：趋势判断、支撑阻力、入场时机、风险提示</div>
        </div>
      </div>

      <!-- Quick Actions -->
      <div style="display:flex;gap:6px;flex-wrap:wrap">
        <span style="font-size:10px;color:var(--muted);margin-right:4px">快捷提问:</span>
        <button class="aio-quick-btn" onclick="quickAsk('BTCUSDT当前趋势如何？适合入场吗？')">BTC趋势分析</button>
        <button class="aio-quick-btn" onclick="quickAsk('ETHUSDT支撑和阻力位在哪？')">ETH支撑阻力</button>
        <button class="aio-quick-btn" onclick="quickAsk('最近24小时有什么重要的市场异动？')">市场异动</button>
        <button class="aio-quick-btn" onclick="quickAsk('当前市场情绪和资金流向如何？')">情绪&资金</button>
        <button class="aio-quick-btn" onclick="quickAsk('推荐1个低风险的交易策略')">策略推荐</button>
      </div>
    </div>

    <!-- RIGHT: Discussion Room -->
    <div style="width:320px;flex-shrink:0;display:flex;flex-direction:column;background:var(--card);border:1px solid var(--border);border-radius:8px;overflow:hidden">
      <div style="display:flex;justify-content:space-between;align-items:center;padding:10px 14px;border-bottom:1px solid var(--border)">
        <span style="font-weight:600;font-size:13px">AI多模型讨论</span>
        <button class="btn btn-xs" onclick="clearAIOChat()" style="font-size:9px;padding:2px 8px">清空</button>
      </div>
      <div id="ai3-chat-messages" style="flex:1;overflow-y:auto;padding:8px">
        <div class="ai3-chat-empty">点击"多模型分析"开始<br>或输入问题与AI讨论</div>
      </div>
      <div id="aio-typing" style="display:none;padding:4px 16px;font-size:10px;color:var(--muted)">AI正在分析中<span class="typing-dots"></span></div>
      <!-- Auto-trade controls -->
      <div class="ai3-autotrade">
        <div class="ai3-at-row">
          <label style="font-weight:700;color:#1f2328">自动交易</label>
          <div class="toggle-switch" id="ai3-at-toggle" onclick="toggleAI3AutoTrade()" style="margin-left:auto"></div>
        </div>
        <div class="ai3-at-row">
          <label>置信度阈值</label>
          <input type="range" min="50" max="95" value="70" id="ai3-at-threshold" oninput="$('ai3-at-threshold-val').textContent=this.value+'%'">
          <span style="font-size:9px;color:var(--muted);width:32px" id="ai3-at-threshold-val">70%</span>
        </div>
        <div class="ai3-at-row">
          <label>分歧保护</label>
          <input type="range" min="10" max="60" value="30" id="ai3-at-divergence" oninput="$('ai3-at-divergence-val').textContent=this.value+'%'">
          <span style="font-size:9px;color:var(--muted);width:32px" id="ai3-at-divergence-val">30%</span>
        </div>
      </div>
      <div style="display:flex;gap:6px;padding:10px;border-top:1px solid var(--border)">
        <textarea id="aio-input" rows="2" placeholder="输入问题，如：BTCUSDT现在适合做多吗？" style="flex:1;background:var(--bg);border:1px solid var(--border);color:#1f2328;padding:8px;border-radius:6px;font-size:11px;resize:none;font-family:inherit" onkeydown="if(event.key==='Enter'&&!event.shiftKey){event.preventDefault();sendAI3Chat()}"></textarea>
        <button class="btn btn-g" onclick="sendAI3Chat()" id="aio-send-btn" style="padding:6px 14px;font-size:12px;align-self:flex-end">发送</button>
      </div>
    </div>
  </div>
</div><!-- #pg-ai_oppty -->
  <div class="page" id="pg-ai_generate" style="flex-direction:column;overflow:hidden">
    <div style="flex:1;display:flex;overflow:hidden;min-height:0">
      <div id="ai2-code-rail" style="display:none;width:36px;flex-shrink:0;background:var(--card);border-right:1px solid var(--border);cursor:pointer;flex-direction:column;align-items:center;padding-top:10px;gap:6px" onclick="toggleCodePanel()">
        <span style="font-size:16px">&#x1F4DD;</span>
        <span style="writing-mode:vertical-rl;font-size:9px;color:var(--muted)">Code</span>
      </div>
      <div id="ai2-code-drawer" style="width:360px;flex-shrink:0;display:flex;flex-direction:column;border-right:1px solid var(--border);overflow-y:auto">
        <div style="display:flex;justify-content:space-between;align-items:center;padding:8px 12px;border-bottom:1px solid var(--border)">
          <div style="display:flex;align-items:center;gap:8px">
            <span style="font-size:16px">&#x1F4DD;</span>
            <span style="font-weight:600;font-size:12px" data-i18n="agent.code_title">Strategy Code</span>
            <span id="ai2-modified-tag2" style="display:none;font-size:9px;background:rgba(210,153,34,.15);color:var(--yel);padding:2px 6px;border-radius:3px" data-i18n="agent.modified">Modified</span>
          </div>
          <div style="display:flex;gap:4px">
            <button class="btn btn-xs" onclick="newAI2Strategy()" style="font-size:10px;padding:2px 8px" data-i18n="agent.new">New</button>
            <button class="btn btn-xs btn-p" onclick="saveAI2Strategy()" style="font-size:10px;padding:2px 8px" data-i18n="agent.save">Save</button>
            <button class="btn btn-xs btn-r" onclick="deleteAI2Strategy()" style="font-size:10px;padding:2px 8px" data-i18n="agent.delete">Del</button>
            <button class="btn btn-xs btn-p" onclick="runAI2Backtest()" style="font-size:10px;padding:2px 8px" data-i18n="agent.run">Run</button>
            <button onclick="toggleCodePanel()" style="background:transparent;border:none;color:var(--muted);cursor:pointer;font-size:16px;padding:0 2px">&times;</button>
          </div>
        </div>
        <div id="ai2-code-editor" style="width:100%;flex:1;min-height:200px;border-bottom:1px solid var(--border)"></div>
        <div style="border-bottom:1px solid var(--border)">
          <div onclick="ai2ToggleGen()" style="display:flex;align-items:center;justify-content:space-between;padding:6px 12px;cursor:pointer;font-size:10px;color:var(--muted);background:var(--bg)">
            <span data-i18n="agent.ai_gen">AI Generate</span>
            <span id="ai2-gen-arrow">&#9654;</span>
          </div>
          <div id="ai2-gen-body" style="display:none;padding:8px 12px">
            <textarea id="ai-chat-inline" rows="3" data-i18n-placeholder="agent.ai_placeholder" placeholder="Describe your strategy..." style="width:100%;background:var(--bg);border:1px solid var(--border);color:#1f2328;padding:6px 8px;border-radius:4px;font-size:10px;resize:vertical;font-family:inherit"></textarea>
            <div style="display:flex;gap:4px;margin-top:6px">
              <button class="btn btn-p" onclick="sendAIInline()" style="flex:1;font-size:10px;padding:6px" data-i18n="agent.gen_code">Generate Code</button>
            </div>
            <div style="display:flex;flex-wrap:wrap;gap:4px;margin-top:6px">
              <span class="ai-chip" onclick="ai2Chip('Generate an EMA crossover strategy for BTC')" style="font-size:9px;padding:2px 6px">EMA Trend</span>
              <span class="ai-chip" onclick="ai2Chip('Generate a grid trading strategy range 64000-72000')" style="font-size:9px;padding:2px 6px">Grid</span>
              <span class="ai-chip" onclick="ai2Chip('Generate a low risk mean reversion strategy')" style="font-size:9px;padding:2px 6px">Reversal</span>
            </div>
          </div>
        </div>
        <div style="padding:6px 10px;border-bottom:1px solid var(--border);font-size:10px;display:flex;align-items:center;gap:6px">
          <span style="color:var(--muted)" data-i18n="agent.backtest">Backtest:</span>
          <input id="ai2-bt-capital" value="10000" type="number" style="width:60px;font-size:9px;padding:2px 4px;border:1px solid var(--border);border-radius:3px">
          <input id="ai2-bt-fee" value="0.1" step="0.01" type="number" style="width:45px;font-size:9px;padding:2px 4px;border:1px solid var(--border);border-radius:3px">
          <select id="ai2-bt-direction" style="font-size:9px;padding:2px 4px;border:1px solid var(--border);border-radius:3px"><option value="both">Both</option><option value="long">Long</option><option value="short">Short</option></select>
          <button class="btn btn-xs btn-g" onclick="runAI2Backtest()" style="margin-left:auto;font-size:9px;padding:2px 10px">Run</button>
        </div>
        <div style="display:grid;grid-template-columns:repeat(4,1fr);gap:2px;padding:4px 10px;font-size:9px;border-bottom:1px solid var(--border)">
          <div><span style="color:var(--muted)" data-i18n="agent.return">Return</span> <b id="ai2bt-totret" style="display:block">--</b></div>
          <div><span style="color:var(--muted)" data-i18n="agent.max_dd">Max DD</span> <b id="ai2bt-maxdd" style="display:block;color:var(--red)">--</b></div>
          <div><span style="color:var(--muted)" data-i18n="agent.sharpe">Sharpe</span> <b id="ai2bt-sharpe" style="display:block">--</b></div>
          <div><span style="color:var(--muted)" data-i18n="agent.trades">Trades</span> <b id="ai2bt-trades" style="display:block">--</b></div>
        </div>
        <div style="flex:1;overflow-y:auto;padding:4px 10px;font-size:9px">
          <div id="ai2-bt-history-list" style="color:var(--muted)"></div>
        </div>
        
      </div>
      <div style="flex:1;display:flex;flex-direction:column;min-width:0">
        <div style="display:flex;align-items:center;gap:4px;padding:5px 10px;border-bottom:1px solid var(--border);background:var(--bg);flex-shrink:0">
          <span style="flex:1"></span>
          <span style="font-weight:700;font-size:12px;cursor:pointer" id="ai2-symbol-badge" onclick="openSymbolSearch()">BTC/USDT</span>
          <select id="ai2-interval-select" onchange="setAI2Interval(this.value)" style="font-size:9px;padding:2px 4px;border:1px solid var(--border);border-radius:3px">
            <option value="1m">1m</option><option value="5m">5m</option><option value="15m">15m</option><option value="1h" selected>1h</option><option value="4h">4h</option><option value="1d">1d</option><option value="1w">1w</option>
          </select>
          <span style="width:1px;height:14px;background:var(--border);margin:0 4px"></span>
          <button class="chart-ind-btn" onclick="ai2ToggleIndicator('MA')" style="font-size:9px;padding:2px 6px">MA</button>
          <button class="chart-ind-btn" onclick="ai2ToggleIndicator('EMA')" style="font-size:9px;padding:2px 6px">EMA</button>
          <button class="chart-ind-btn" onclick="ai2ToggleIndicator('MACD')" style="font-size:9px;padding:2px 6px">MACD</button>
          <button class="chart-ind-btn" onclick="ai2ToggleIndicator('RSI')" style="font-size:9px;padding:2px 6px">RSI</button>
          <button class="chart-ind-btn" onclick="ai2ToggleIndicator('BOLL')" style="font-size:9px;padding:2px 6px">BOLL</button>
          <button class="chart-ind-btn" onclick="ai2ClearIndicators()" style="color:var(--red);font-size:9px;padding:2px 6px" data-i18n="agent.clear">Clear</button>
          <span style="width:1px;height:14px;background:var(--border);margin:0 4px"></span>
          <span id="ai2-modified-tag" style="display:none;font-size:9px;color:var(--yel)">Modified</span>
          <button class="btn btn-xs btn-p" onclick="runAI2Backtest()" style="font-size:10px;padding:2px 10px;font-weight:600">Run</button>
        </div>
        <div id="ai2-chart-container" style="width:100%;flex:1;min-height:0"></div>
      </div>
    </div>
  </div><!-- #pg-ai_generate -->

<!-- ═══════════════════════════════════════════ BACKTEST ═══════════════════════════════════════════ -->
<div class="page" id="pg-backtest">
<div class="bt-layout">
  <!-- Config Panel -->
  <div style="grid-row:1/3"><div class="pad">
    <div class="section-title" data-i18n="bt.config">Backtest Configuration</div>
    <div class="f-label" data-i18n="bt.strategy">Strategy</div><select id="bt-strategy" style="margin-bottom:8px"><option value="breakout">Breakout</option><option value="market_making">Market Making</option><option value="grid">Grid Trading</option></select>
    <div class="f-label" data-i18n="bt.symbol">Symbol</div><input id="bt-symbol" value="BTCUSDT" style="margin-bottom:8px">
    <div class="f-label" data-i18n="bt.capital">Initial Capital (USDT)</div><input id="bt-capital" value="100000" type="number" style="margin-bottom:8px">
    <div class="f-row"><div><div class="f-label" data-i18n="bt.fee">Fee Rate</div><input id="bt-fee" value="0.001" step="0.0001"></div><div><div class="f-label" data-i18n="bt.slippage">Slippage</div><input id="bt-slippage" value="0.0005" step="0.0001"></div></div>
    <div class="f-row"><div><div class="f-label" data-i18n="bt.bars">Bars</div><input id="bt-bars" value="500" type="number"></div><div><div class="f-label" data-i18n="bt.price">Base Price</div><input id="bt-price" value="68000" type="number"></div></div>
    <div class="f-label" data-i18n="bt.params">Strategy Parameters</div>
    <div id="bt-params"></div>
    <button class="btn btn-g" style="width:100%;padding:10px;font-size:13px;font-weight:600;margin-top:10px" onclick="runBacktest()" id="bt-run-btn" data-i18n="bt.run">Run Backtest</button>
    <div id="bt-status" style="font-size:10px;margin-top:8px;color:var(--yel)"></div>
    <div id="bt-last-result" style="font-size:10px;margin-top:8px"></div>
  </div></div>
  <!-- Results Chart -->
  <div><div class="pad"><div class="section-title" data-i18n="bt.equity">Equity Curve</div><div id="bt-chart" style="height:calc(100% - 28px)"></div></div></div>
  <!-- Results Metrics -->
  <div><div class="pad"><div class="section-title" data-i18n="bt.results">Performance Metrics</div><div id="bt-metrics" style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:8px"></div></div></div>
</div>
</div>

<!-- ═══════════════════════════════════════════ STRATEGIES ═══════════════════════════════════════════ -->
<div class="page" id="pg-strategy">
<div class="strategy-page">
  <!-- LEFT: Sidebar tabs -->
  <div class="strategy-sidebar">
    <div class="sidebar-label" data-i18n="sidebar.label">策略管理</div>
    <a class="sidebar-tab active" data-sub="spot" onclick="switchSubPage('spot')" data-i18n="nav.spot">现货策略</a>
    <a class="sidebar-tab" data-sub="contract" onclick="switchSubPage('contract')" data-i18n="nav.contract">合约策略</a>
        <a class="sidebar-tab" data-sub="native" onclick="switchSubPage('native')" data-i18n="nav.native">Python原生策略</a>
    <a class="sidebar-tab" data-sub="templates" onclick="switchSubPage('templates')" data-i18n="nav.templates">策略模板管理</a>
    <a class="sidebar-tab" data-sub="logs" onclick="switchSubPage('logs')" data-i18n="nav.logs">策略运行日志</a>
  </div>
  <!-- MIDDLE: Account data + Risk params (hidden from main layout, shown as floating widget) -->
  <div id="account-widget" class="account-float-widget">
    <div class="afw-header" onclick="toggleAccountWidget()">
      <span style="font-size:10px;font-weight:600">账户 & 风控</span>
      <span id="afw-toggle-icon">&#x25B2;</span>
    </div>
    <div class="afw-body" id="afw-body">
      <div class="kpi-card" style="margin-bottom:6px"><div class="klabel" data-i18n="kpi.margin">保证金可用余额</div><div class="kvalue" id="sk-margin">0.00</div><div class="ksub">USDT</div></div>
      <div class="kpi-card" style="margin-bottom:6px"><div class="klabel" data-i18n="kpi.holdings">持仓市值</div><div class="kvalue" id="sk-holdings">0.00</div><div class="ksub">USDT</div></div>
      <div class="kpi-card" style="margin-bottom:6px"><div class="klabel" data-i18n="kpi.protection">盈利保护开关</div><div style="margin-top:4px"><div class="toggle-switch" id="sk-protection-toggle" onclick="toggleSwitch(this);saveGlobalSettings()"></div><input type="hidden" id="sk-protection" class="toggle-input" value="0"></div></div>
      <div class="kpi-card"><div class="klabel" data-i18n="kpi.max_orders">最大并单数</div><div style="margin-top:4px"><input id="sk-max-orders" type="number" value="5" min="1" max="50" style="width:64px;padding:3px 6px;font-size:14px" onchange="saveGlobalSettings()"></div></div>
    </div>
  </div>
  <!-- RIGHT: Main content area -->
  <div class="strategy-main">
  <!-- SUB: Spot Strategies -->
  <div class="sub-page active" id="sub-spot">
    <div class="filter-bar">
      <input id="spot-search" data-i18n-placeholder="filter.search" placeholder="搜索币种..." oninput="loadSpotList()" style="width:130px">
      <select id="spot-type" onchange="loadSpotList()"><option value="" data-i18n="filter.type_all">全部类型</option><option value="martin">马丁趋势</option><option value="wall_street">华尔街</option><option value="radical">激进</option><option value="conservative">保守</option><option value="hft">高频</option></select>
      <span class="status-tabs">
        <a class="st-tab active" data-st="all" onclick="spotStatus='all';loadSpotList()" data-i18n="status.all">全部</a>
        <a class="st-tab" data-st="running" onclick="spotStatus='running';loadSpotList()" data-i18n="status.running">执行中</a>
        <a class="st-tab" data-st="stopped" onclick="spotStatus='stopped';loadSpotList()" data-i18n="status.stopped">未启动</a>
        <a class="st-tab" data-st="history" onclick="spotStatus='history';loadSpotList()" data-i18n="status.history">历史</a>
      </span>
    </div>
    <div class="stats-bar"><span><span data-i18n="stats.total">共</span> <b id="spot-total">0</b></span><span><span data-i18n="stats.running">执行中</span> <b id="spot-running" class="g">0</b></span><span><span data-i18n="stats.pnl">总盈亏</span> <b id="spot-pnl">0.00</b></span></div>
    <div class="table-container" style="flex:1;overflow:auto"><table>
      <thead><tr><th style="width:30px"><input type="checkbox" id="spot-check-all" onchange="toggleAllSpot()"></th><th data-i18n="table.id">策略ID</th><th data-i18n="table.coin">币种</th><th data-i18n="table.type">策略类型</th><th data-i18n="table.status">状态</th><th data-i18n="table.pnl">当前盈亏</th><th data-i18n="table.actions">操作</th></tr></thead>
      <tbody id="spot-tbody"></tbody>
    </table></div>
    <div id="spot-empty" class="empty-state" style="display:none"><div class="empty-icon">&#128200;</div><div class="empty-title" data-i18n="label.empty_spot">暂无现货策略</div><div class="empty-desc" data-i18n="label.empty_spot_desc">点击下方按钮创建你的第一个策略</div></div>
    <div class="toolbar">
      <span style="font-size:10px;color:var(--muted)"><span data-i18n="common.selected_text">已选</span> <b id="spot-selected">0</b> <span data-i18n="common.items">项</span></span>
      <div style="display:flex;gap:5px">
        <button class="btn btn-xs btn-g" id="spot-batch-start" onclick="batchSpot('start')" disabled data-i18n="btn.batch_start">批量启动</button>
        <button class="btn btn-xs btn-r" id="spot-batch-stop" onclick="batchSpot('stop')" disabled data-i18n="btn.batch_stop">批量停止</button>
        <button class="btn btn-xs" id="spot-batch-del" onclick="batchSpot('delete')" disabled data-i18n="btn.batch_delete">批量删除</button>
        <button class="btn btn-p" onclick="openStrategyForm('spot')" data-i18n="btn.create_spot">创建现货策略</button>
      </div>
    </div>
  </div>
  <!-- SUB: Contract Strategies -->
  <div class="sub-page" id="sub-contract">
    <div class="filter-bar">
      <input id="ct-search" data-i18n-placeholder="filter.search" placeholder="搜索币种..." oninput="loadContractList()" style="width:130px">
      <select id="ct-type" onchange="loadContractList()"><option value="" data-i18n="filter.type_all">全部类型</option><option value="trend_long">顺势多</option><option value="trend_short">顺势空</option><option value="counter_stable">逆势稳健</option><option value="counter_conservative">逆势保守</option><option value="hft">高频</option><option value="arbitrage">首尾套利</option></select>
      <span class="status-tabs">
        <a class="st-tab active" data-st="all" onclick="ctStatus='all';loadContractList()" data-i18n="status.all">全部</a>
        <a class="st-tab" data-st="running" onclick="ctStatus='running';loadContractList()" data-i18n="status.running">执行中</a>
        <a class="st-tab" data-st="stopped" onclick="ctStatus='stopped';loadContractList()" data-i18n="status.stopped">未启动</a>
        <a class="st-tab" data-st="monitoring" onclick="ctStatus='monitoring';loadContractList()" data-i18n="status.monitoring">检测中</a>
        <a class="st-tab" data-st="history" onclick="ctStatus='history';loadContractList()" data-i18n="status.history">历史</a>
      </span>
    </div>
    <div class="stats-bar"><span><span data-i18n="stats.short">开空</span> <b id="ct-short" class="r">0</b></span><span><span data-i18n="stats.long">开多</span> <b id="ct-long" class="g">0</b></span><span><span data-i18n="stats.online">在线单</span> <b id="ct-online">0</b></span></div>
    <div class="table-container" style="flex:1;overflow:auto"><table>
      <thead><tr><th style="width:30px"><input type="checkbox" id="ct-check-all" onchange="toggleAllCt()"></th><th data-i18n="table.id">策略ID</th><th data-i18n="table.coin">币种</th><th data-i18n="table.direction">方向</th><th data-i18n="table.leverage">杠杆</th><th data-i18n="table.status">状态</th><th data-i18n="table.pnl">当前盈亏</th><th data-i18n="table.actions">操作</th></tr></thead>
      <tbody id="ct-tbody"></tbody>
    </table></div>
    <div id="ct-empty" class="empty-state" style="display:none"><div class="empty-icon">&#128200;</div><div class="empty-title" data-i18n="label.empty_ct">暂无合约策略</div><div class="empty-desc" data-i18n="label.empty_ct_desc">点击下方按钮创建你的第一个策略</div></div>
    <div class="toolbar">
      <span style="font-size:10px;color:var(--muted)"><span data-i18n="common.selected_text">已选</span> <b id="ct-selected">0</b> <span data-i18n="common.items">项</span></span>
      <div style="display:flex;gap:5px">
        <button class="btn btn-xs btn-g" id="ct-batch-start" onclick="batchCt('start')" disabled data-i18n="btn.batch_start">批量启动</button>
        <button class="btn btn-xs btn-r" id="ct-batch-stop" onclick="batchCt('stop')" disabled data-i18n="btn.batch_stop">批量停止</button>
        <button class="btn btn-xs" id="ct-batch-del" onclick="batchCt('delete')" disabled data-i18n="btn.batch_delete">批量删除</button>
        <button class="btn btn-p" onclick="openStrategyForm('contract')" data-i18n="btn.create_contract">创建合约策略</button>
      </div>
    </div>
  </div>
  <!-- SUB: Templates -->
  <div class="sub-page" id="sub-templates">
    <div class="filter-bar">
      <span class="status-tabs">
        <a class="st-tab active" data-st="spot" onclick="tplCategory='spot';loadTemplates()" data-i18n="tpl.spot">现货模板</a>
        <a class="st-tab" data-st="contract" onclick="tplCategory='contract';loadTemplates()" data-i18n="tpl.contract">合约模板</a>
      </span>
    </div>
    <div id="tpl-list" style="flex:1;overflow:auto;padding:12px;display:flex;flex-wrap:wrap;gap:10px;align-content:flex-start"></div>
    <div id="tpl-empty" class="empty-state" style="display:none"><div class="empty-icon">&#128203;</div><div class="empty-title" data-i18n="label.empty_tpl">暂无策略模板</div><div class="empty-desc" data-i18n="label.empty_tpl_desc">创建策略时勾选「保存为默认」即可保存为模板</div></div>
    <div class="toolbar"><button class="btn btn-xs" onclick="showSaveTemplate()" data-i18n="tpl.save_as">保存当前为模板</button></div>
  </div>
  <!-- SUB: Run Logs -->
  <div class="sub-page" id="sub-logs">
    <div class="filter-bar">
      <select id="log-strategy" onchange="loadLogs()"><option value="" data-i18n="log.all_strategies">全部策略</option></select>
      <select id="log-level" onchange="loadLogs()"><option value="" data-i18n="filter.level_all">全部级别</option><option value="INFO">INFO</option><option value="WARN">WARN</option><option value="ERROR">ERROR</option></select>
      <input id="log-search" data-i18n-placeholder="common.search" placeholder="搜索..." oninput="loadLogs()" style="width:140px">
      <label style="font-size:10px;display:flex;align-items:center;gap:4px;cursor:pointer"><input type="checkbox" id="log-auto" checked onchange="toggleLogAuto()"><span data-i18n="log.auto_refresh">自动刷新</span></label>
      <button class="btn btn-xs btn-r" onclick="clearLogs()" data-i18n="btn.clear">清除日志</button>
    </div>
    <div class="table-container" style="flex:1;overflow:auto"><table>
      <thead><tr><th style="width:140px" data-i18n="log.col_time">时间</th><th style="width:90px" data-i18n="table.id">策略ID</th><th style="width:55px" data-i18n="log.col_level">级别</th><th data-i18n="log.col_msg">消息</th></tr></thead>
      <tbody id="log-tbody"></tbody>
    </table></div>
    <div id="log-empty" class="empty-state" style="display:none"><div class="empty-icon">&#128196;</div><div class="empty-title" data-i18n="label.empty_log">暂无运行日志</div><div class="empty-desc" data-i18n="label.empty_log_desc">启动策略后将在此显示运行日志</div></div>
  </div>
  <!-- SUB: AI Strategy Generator (Redesigned) -->
  <div class="sub-page" id="sub-native">
    <div style="display:flex;gap:12px;flex:1;overflow:hidden">
      <!-- Left: Config Panel -->
      <div style="width:340px;flex-shrink:0;background:var(--card);border:1px solid var(--border);border-radius:8px;padding:16px;overflow-y:auto">
        <div class="section-title" data-i18n="native.title">Python原生策略</div>
        <!-- Mode tabs -->
        <div class="native-mode-tabs">
          <button class="native-mode-tab active" data-mode="indicator" onclick="switchNativeMode('indicator')" data-i18n="native.indicator_mode">指标策略</button>
          <button class="native-mode-tab" data-mode="script" onclick="switchNativeMode('script')" data-i18n="native.script_mode">脚本策略</button>
        </div>
        <!-- Indicator Panel -->
        <div id="native-indicator-panel">
          <div class="f-label" data-i18n="trade.symbol">交易对</div>
          <select id="native-symbol" style="margin-bottom:8px">
            <option>BTCUSDT</option><option>ETHUSDT</option><option>BNBUSDT</option><option>SOLUSDT</option>
          </select>
          <div class="f-label" data-i18n="ai.interval">K线周期</div>
          <select id="native-interval" style="margin-bottom:10px">
            <option value="1m">1分钟</option><option value="5m">5分钟</option><option value="15m">15分钟</option>
            <option value="1h" selected>1小时</option><option value="4h">4小时</option><option value="1d">1天</option>
          </select>
          <div class="f-label" data-i18n="native.entry_indicator">开仓指标</div>
          <select id="native-entry-ind" onchange="updateNativeCode()" style="margin-bottom:4px">
            <option value="ma_cross">MA 均线金叉/死叉</option>
            <option value="rsi">RSI 超买超卖</option>
            <option value="macd">MACD 金叉/死叉</option>
            <option value="boll">布林带突破</option>
          </select>
          <div id="native-entry-params" style="margin-bottom:8px"></div>
          <div class="f-label" data-i18n="native.exit_indicator">平仓指标</div>
          <select id="native-exit-ind" onchange="updateNativeCode()" style="margin-bottom:4px">
            <option value="fixed_stop">固定止损止盈</option>
            <option value="trailing_stop">移动止损</option>
            <option value="reverse_signal">反向信号</option>
          </select>
          <div id="native-exit-params" style="margin-bottom:8px"></div>
          <div class="f-label" data-i18n="bt.capital">初始资金 (USDT)</div>
          <input id="native-capital" value="100000" type="number" style="margin-bottom:8px">
          <div class="f-label" data-i18n="bt.bars">K线数</div>
          <input id="native-bars" value="500" type="number" style="margin-bottom:10px">
          <button class="btn btn-g" style="width:100%;padding:10px;font-size:13px;font-weight:600" onclick="runNativeStrategy()" id="native-run-btn" data-i18n="native.run_backtest">运行回测</button>
          <div id="native-status" style="font-size:10px;margin-top:8px;color:var(--yel)"></div>
        </div>
        <!-- Script Panel -->
        <div id="native-script-panel" style="display:none">
          <div class="f-label" data-i18n="trade.symbol">交易对</div>
          <select id="native-sc-symbol" style="margin-bottom:8px">
            <option>BTCUSDT</option><option>ETHUSDT</option><option>BNBUSDT</option><option>SOLUSDT</option>
          </select>
          <div class="f-label" data-i18n="ai.interval">K线周期</div>
          <select id="native-sc-interval" style="margin-bottom:8px">
            <option value="1m">1分钟</option><option value="5m">5分钟</option><option value="15m">15分钟</option>
            <option value="1h" selected>1小时</option><option value="4h">4小时</option><option value="1d">1天</option>
          </select>
          <div style="display:flex;gap:4px;margin-bottom:8px">
            <button class="btn btn-xs" onclick="loadScriptTemplate('indicator')" data-i18n="native.load_indicator_tpl">加载指标模板</button>
            <button class="btn btn-xs" onclick="loadScriptTemplate('empty')" data-i18n="native.load_empty_tpl">清空模板</button>
          </div>
          <div class="f-label" data-i18n="native.script_code">策略代码</div>
          <textarea id="native-script-editor" rows="12" style="background:var(--bg);border:1px solid var(--border);color:#1f2328;padding:8px;border-radius:5px;font-size:10px;width:100%;resize:vertical;font-family:Cascadia Code,monospace;margin-bottom:4px;white-space:pre;tab-size:2" spellcheck="false"></textarea>
          <div style="display:flex;gap:4px;margin-bottom:8px">
            <button class="btn btn-xs" onclick="copyNativeCode()" data-i18n="ai.copy_code">复制</button>
          </div>
          <div class="f-label" data-i18n="bt.capital">初始资金 (USDT)</div>
          <input id="native-sc-capital" value="100000" type="number" style="margin-bottom:8px">
          <div class="f-label" data-i18n="bt.bars">K线数</div>
          <input id="native-sc-bars" value="500" type="number" style="margin-bottom:10px">
          <button class="btn btn-g" style="width:100%;padding:10px;font-size:13px;font-weight:600" onclick="runNativeScriptStrategy()" id="native-sc-run-btn" data-i18n="native.run_backtest">运行回测</button>
          <div id="native-sc-status" style="font-size:10px;margin-top:8px;color:var(--yel)"></div>
        </div>
      </div>
      <!-- Right: Code + Results -->
      <div style="flex:1;display:flex;flex-direction:column;gap:8px;min-width:0;overflow:hidden">
        <!-- Code display -->
        <div style="flex:1;background:var(--card);border:1px solid var(--border);border-radius:8px;overflow:hidden;display:flex;flex-direction:column;min-height:180px">
          <div style="display:flex;justify-content:space-between;padding:8px 12px;border-bottom:1px solid var(--border);align-items:center">
            <span class="section-title" style="margin:0" data-i18n="ai.code">生成的策略代码</span>
            <div style="display:flex;gap:4px">
              <button class="btn btn-xs" onclick="copyNativeGeneratedCode()" data-i18n="ai.copy_code">复制</button>
              <button class="btn btn-xs" onclick="downloadNativeCode()" data-i18n="ai.download_code">下载</button>
              <button class="btn btn-xs btn-p" onclick="deployNativeStrategy()" id="native-deploy-btn" disabled data-i18n="ai.deploy">保存为模板</button>
            </div>
          </div>
          <pre id="native-code-display" style="flex:1;overflow:auto;padding:12px;font-size:10px;font-family:Cascadia Code,monospace;color:var(--muted);margin:0;white-space:pre-wrap;word-break:break-all" data-i18n="native.code_placeholder">选择指标参数或编写脚本，然后点击"运行回测"生成策略...</pre>
        </div>
        <!-- Backtest Results -->
        <div id="native-results" style="display:none;flex:1;flex-direction:column;gap:8px;min-height:200px">
          <div style="background:var(--card);border:1px solid var(--border);border-radius:8px;padding:12px;flex:1;display:flex;flex-direction:column">
            <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
              <span class="section-title" style="margin:0" data-i18n="ai.results">回测结果</span>
              <button class="btn btn-xs" onclick="resetNativeStrategy()" data-i18n="ai.regenerate">重新生成</button>
            </div>
            <div id="native-metrics" style="display:grid;grid-template-columns:1fr 1fr 1fr 1fr;gap:6px;margin-bottom:8px"></div>
            <div id="native-chart" style="flex:1;min-height:180px"></div>
          </div>
        </div>
      </div>
    </div>
  </div><!-- #sub-native -->
  </div><!-- .strategy-main -->
</div><!-- .strategy-page -->
</div><!-- #pg-strategy -->

<!-- ═══════════════════════════════════════════ SETTINGS ═══════════════════════════════════════════ -->
<div class="page" id="pg-settings">
<div style="flex:1;overflow-y:auto;padding:20px 24px">
  <!-- Page Title -->
  <div class="settings-page-title" data-i18n="settings.page_title">系统设置</div>

  <!-- Settings Tab Bar -->
  <div class="stabs">
    <a class="stab-item active" data-stab="exchange" onclick="switchSettingsTab('exchange')" data-i18n="settings.exchange_title">交易所配置</a>
    <a class="stab-item" data-stab="ai" onclick="switchSettingsTab('ai')" data-i18n="settings.ai_module_title">AI大模型配置</a>
    <a class="stab-item" data-stab="agent" onclick="switchSettingsTab('agent')" data-i18n="settings.agent_title">Agent接入</a>
    <a class="stab-item" data-stab="ui" onclick="switchSettingsTab('ui')">界面设置</a>
  </div>

  <!-- Tab: Exchange Configuration -->
  <div class="stab-content active" id="stab-exchange">
  <div style="display:flex;align-items:center;gap:12px;margin-bottom:16px;padding:10px 14px;background:var(--card);border:1px solid var(--border);border-radius:8px">
    <span style="font-size:12px;color:var(--muted);white-space:nowrap" data-i18n="settings.default_exchange">默认交易所</span>
    <select id="exchange-default" onchange="saveDefaultExchange()" style="flex:1;max-width:260px;margin-bottom:0">
      <option value="" data-i18n="settings.auto_select">自动选择(第一个已配置)</option>
      <option value="binance">Binance</option>
      <option value="okx">OKX</option>
      <option value="kucoin">KuCoin</option>
      <option value="bybit">Bybit</option>
      <option value="gate">Gate.io</option>
      <option value="htx">HTX</option>
      <option value="coinbase">Coinbase</option>
      <option value="mexc">MEXC</option>
      <option value="zb">ZB</option>
      <option value="bitget">Bitget</option>
      <option value="phemex">Phemex</option>
      <option value="deribit">Deribit</option>
    </select>
  </div>
  <div class="settings-module">
    <div class="settings-module-title" data-i18n="settings.exchange_title">交易所配置</div>
    <div class="settings-card-grid">

      <!-- Binance -->
      <div class="scard" id="scard-binance">
        <div class="scard-header">
          <span class="scard-icon" style="background:#F0B90B;color:#000">B</span>
          <span class="scard-name">Binance</span>
          <span class="scard-status" id="s-bn-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-bn-key" autocomplete="off" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.secret_key">Secret Key</div>
          <div class="sc-pw-row"><input id="s-bn-secret" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-bn-secret',this)" title="Show/Hide">&#128065;</button></div>
          <div class="sc-row">
            <div style="flex:1"><div class="f-label" data-i18n="settings.trade_type">交易类型</div><select id="s-bn-tradetype"><option value="spot">现货 Spot</option><option value="futures">合约 Futures</option></select></div>
            <div><div class="f-label" data-i18n="settings.testnet">测试网</div><div class="toggle-switch" id="s-bn-testnet-toggle" onclick="toggleSwitch(this)"></div><input type="hidden" class="toggle-input" id="s-bn-testnet" value="0"></div>
          </div>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testExchange('binance')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveExchange('binance')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-bn-msg"></span>
          </div>
        </div>
      </div>

      <!-- OKX -->
      <div class="scard" id="scard-okx">
        <div class="scard-header">
          <span class="scard-icon" style="background:#fff;color:#000">O</span>
          <span class="scard-name">OKX</span>
          <span class="scard-status" id="s-ok-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-ok-key" autocomplete="off" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.secret_key">Secret Key</div>
          <div class="sc-pw-row"><input id="s-ok-secret" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-ok-secret',this)" title="Show/Hide">&#128065;</button></div>
          <div class="f-label" data-i18n="settings.passphrase">Passphrase</div>
          <div class="sc-pw-row"><input id="s-ok-pass" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-ok-pass',this)" title="Show/Hide">&#128065;</button></div>
          <div class="sc-row">
            <div style="flex:1"><div class="f-label" data-i18n="settings.trade_type">交易类型</div><select id="s-ok-tradetype"><option value="spot">现货 Spot</option><option value="futures">合约 Futures</option></select></div>
            <div><div class="f-label" data-i18n="settings.testnet">测试网</div><div class="toggle-switch" id="s-ok-testnet-toggle" onclick="toggleSwitch(this)"></div><input type="hidden" class="toggle-input" id="s-ok-testnet" value="0"></div>
          </div>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testExchange('okx')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveExchange('okx')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-ok-msg"></span>
          </div>
        </div>
      </div>

      <!-- Coinbase -->
      <div class="scard" id="scard-coinbase">
        <div class="scard-header">
          <span class="scard-icon" style="background:#0052FF;color:#1f2328">C</span>
          <span class="scard-name">Coinbase</span>
          <span class="scard-status" id="s-cb-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-cb-key" autocomplete="off" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.secret_key">Secret Key</div>
          <div class="sc-pw-row"><input id="s-cb-secret" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-cb-secret',this)" title="Show/Hide">&#128065;</button></div>
          <div class="f-label" data-i18n="settings.passphrase">Passphrase</div>
          <div class="sc-pw-row"><input id="s-cb-pass" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-cb-pass',this)" title="Show/Hide">&#128065;</button></div>
          <div class="sc-row">
            <div style="flex:1"><div class="f-label" data-i18n="settings.trade_type">交易类型</div><select id="s-cb-tradetype"><option value="spot">现货 Spot</option><option value="futures">合约 Futures</option></select></div>
            <div><div class="f-label" data-i18n="settings.testnet">测试网</div><div class="toggle-switch" id="s-cb-testnet-toggle" onclick="toggleSwitch(this)"></div><input type="hidden" class="toggle-input" id="s-cb-testnet" value="0"></div>
          </div>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testExchange('coinbase')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveExchange('coinbase')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-cb-msg"></span>
          </div>
        </div>
      </div>

      <!-- KuCoin -->
      <div class="scard" id="scard-kucoin">
        <div class="scard-header">
          <span class="scard-icon" style="background:#24C78B;color:#000">K</span>
          <span class="scard-name">KuCoin</span>
          <span class="scard-status" id="s-kc-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-kc-key" autocomplete="off" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.secret_key">Secret Key</div>
          <div class="sc-pw-row"><input id="s-kc-secret" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-kc-secret',this)" title="Show/Hide">&#128065;</button></div>
          <div class="f-label" data-i18n="settings.passphrase">Passphrase</div>
          <div class="sc-pw-row"><input id="s-kc-pass" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-kc-pass',this)" title="Show/Hide">&#128065;</button></div>
          <div class="sc-row">
            <div style="flex:1"><div class="f-label" data-i18n="settings.trade_type">交易类型</div><select id="s-kc-tradetype"><option value="spot">现货 Spot</option><option value="futures">合约 Futures</option></select></div>
            <div><div class="f-label" data-i18n="settings.testnet">测试网</div><div class="toggle-switch" id="s-kc-testnet-toggle" onclick="toggleSwitch(this)"></div><input type="hidden" class="toggle-input" id="s-kc-testnet" value="0"></div>
          </div>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testExchange('kucoin')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveExchange('kucoin')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-kc-msg"></span>
          </div>
        </div>
      </div>

      <!-- Bybit -->
      <div class="scard" id="scard-bybit">
        <div class="scard-header">
          <span class="scard-icon" style="background:#F7A600;color:#000">B</span>
          <span class="scard-name">Bybit</span>
          <span class="scard-status" id="s-by-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-by-key" autocomplete="off" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.secret_key">Secret Key</div>
          <div class="sc-pw-row"><input id="s-by-secret" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-by-secret',this)" title="Show/Hide">&#128065;</button></div>
          <div class="sc-row">
            <div style="flex:1"><div class="f-label" data-i18n="settings.trade_type">交易类型</div><select id="s-by-tradetype"><option value="spot">现货 Spot</option><option value="futures">合约 Futures</option></select></div>
            <div><div class="f-label" data-i18n="settings.testnet">测试网</div><div class="toggle-switch" id="s-by-testnet-toggle" onclick="toggleSwitch(this)"></div><input type="hidden" class="toggle-input" id="s-by-testnet" value="0"></div>
          </div>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testExchange('bybit')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveExchange('bybit')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-by-msg"></span>
          </div>
        </div>
      </div>

      <!-- Gate.io -->
      <div class="scard" id="scard-gate">
        <div class="scard-header">
          <span class="scard-icon" style="background:#17E6A1;color:#000">G</span>
          <span class="scard-name">Gate.io</span>
          <span class="scard-status" id="s-gt-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-gt-key" autocomplete="off" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.secret_key">Secret Key</div>
          <div class="sc-pw-row"><input id="s-gt-secret" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-gt-secret',this)" title="Show/Hide">&#128065;</button></div>
          <div class="sc-row">
            <div style="flex:1"><div class="f-label" data-i18n="settings.trade_type">交易类型</div><select id="s-gt-tradetype"><option value="spot">现货 Spot</option><option value="futures">合约 Futures</option></select></div>
            <div><div class="f-label" data-i18n="settings.testnet">测试网</div><div class="toggle-switch" id="s-gt-testnet-toggle" onclick="toggleSwitch(this)"></div><input type="hidden" class="toggle-input" id="s-gt-testnet" value="0"></div>
          </div>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testExchange('gate')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveExchange('gate')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-gt-msg"></span>
          </div>
        </div>
      </div>

      <!-- HTX (Huobi) -->
      <div class="scard" id="scard-htx">
        <div class="scard-header">
          <span class="scard-icon" style="background:#2CA6E0;color:#1f2328">H</span>
          <span class="scard-name">HTX (火币)</span>
          <span class="scard-status" id="s-ht-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-ht-key" autocomplete="off" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.secret_key">Secret Key</div>
          <div class="sc-pw-row"><input id="s-ht-secret" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-ht-secret',this)" title="Show/Hide">&#128065;</button></div>
          <div class="sc-row">
            <div style="flex:1"><div class="f-label" data-i18n="settings.trade_type">交易类型</div><select id="s-ht-tradetype"><option value="spot">现货 Spot</option><option value="futures">合约 Futures</option></select></div>
            <div><div class="f-label" data-i18n="settings.testnet">测试网</div><div class="toggle-switch" id="s-ht-testnet-toggle" onclick="toggleSwitch(this)"></div><input type="hidden" class="toggle-input" id="s-ht-testnet" value="0"></div>
          </div>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testExchange('htx')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveExchange('htx')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-ht-msg"></span>
          </div>
        </div>
      </div>

      <!-- MEXC -->
      <div class="scard" id="scard-mexc">
        <div class="scard-header">
          <span class="scard-icon" style="background:#1F8B7A;color:#1f2328">M</span>
          <span class="scard-name">MEXC (抹茶)</span>
          <span class="scard-status" id="s-me-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-me-key" autocomplete="off" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.secret_key">Secret Key</div>
          <div class="sc-pw-row"><input id="s-me-secret" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-me-secret',this)" title="Show/Hide">&#128065;</button></div>
          <div class="sc-row">
            <div style="flex:1"><div class="f-label" data-i18n="settings.trade_type">交易类型</div><select id="s-me-tradetype"><option value="spot">现货 Spot</option><option value="futures">合约 Futures</option></select></div>
            <div><div class="f-label" data-i18n="settings.testnet">测试网</div><div class="toggle-switch" id="s-me-testnet-toggle" onclick="toggleSwitch(this)"></div><input type="hidden" class="toggle-input" id="s-me-testnet" value="0"></div>
          </div>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testExchange('mexc')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveExchange('mexc')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-me-msg"></span>
          </div>
        </div>
      </div>

      <!-- ZB -->
      <div class="scard" id="scard-zb">
        <div class="scard-header">
          <span class="scard-icon" style="background:#0D6EFD;color:#1f2328">Z</span>
          <span class="scard-name">ZB (中币)</span>
          <span class="scard-status" id="s-zb-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-zb-key" autocomplete="off" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.secret_key">Secret Key</div>
          <div class="sc-pw-row"><input id="s-zb-secret" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-zb-secret',this)" title="Show/Hide">&#128065;</button></div>
          <div class="sc-row">
            <div style="flex:1"><div class="f-label" data-i18n="settings.trade_type">交易类型</div><select id="s-zb-tradetype"><option value="spot">现货 Spot</option></select></div>
            <div><div class="f-label" data-i18n="settings.testnet">测试网</div><div class="toggle-switch" id="s-zb-testnet-toggle" onclick="toggleSwitch(this)"></div><input type="hidden" class="toggle-input" id="s-zb-testnet" value="0"></div>
          </div>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testExchange('zb')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveExchange('zb')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-zb-msg"></span>
          </div>
        </div>
      </div>

      <!-- Bitget -->
      <div class="scard" id="scard-bitget">
        <div class="scard-header">
          <span class="scard-icon" style="background:#00B4C5;color:#1f2328">B</span>
          <span class="scard-name">Bitget</span>
          <span class="scard-status" id="s-bg-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-bg-key" autocomplete="off" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.secret_key">Secret Key</div>
          <div class="sc-pw-row"><input id="s-bg-secret" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-bg-secret',this)" title="Show/Hide">&#128065;</button></div>
          <div class="f-label" data-i18n="settings.passphrase">Passphrase</div>
          <div class="sc-pw-row"><input id="s-bg-pass" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-bg-pass',this)" title="Show/Hide">&#128065;</button></div>
          <div class="sc-row">
            <div style="flex:1"><div class="f-label" data-i18n="settings.trade_type">交易类型</div><select id="s-bg-tradetype"><option value="spot">现货 Spot</option><option value="futures">合约 Futures</option></select></div>
            <div><div class="f-label" data-i18n="settings.testnet">测试网</div><div class="toggle-switch" id="s-bg-testnet-toggle" onclick="toggleSwitch(this)"></div><input type="hidden" class="toggle-input" id="s-bg-testnet" value="0"></div>
          </div>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testExchange('bitget')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveExchange('bitget')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-bg-msg"></span>
          </div>
        </div>
      </div>

      <!-- Phemex -->
      <div class="scard" id="scard-phemex">
        <div class="scard-header">
          <span class="scard-icon" style="background:#3B82F6;color:#1f2328">P</span>
          <span class="scard-name">Phemex</span>
          <span class="scard-status" id="s-pm-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-pm-key" autocomplete="off" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.secret_key">Secret Key</div>
          <div class="sc-pw-row"><input id="s-pm-secret" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-pm-secret',this)" title="Show/Hide">&#128065;</button></div>
          <div class="sc-row">
            <div style="flex:1"><div class="f-label" data-i18n="settings.trade_type">交易类型</div><select id="s-pm-tradetype"><option value="futures">合约 Futures</option></select></div>
            <div><div class="f-label" data-i18n="settings.testnet">测试网</div><div class="toggle-switch" id="s-pm-testnet-toggle" onclick="toggleSwitch(this)"></div><input type="hidden" class="toggle-input" id="s-pm-testnet" value="0"></div>
          </div>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testExchange('phemex')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveExchange('phemex')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-pm-msg"></span>
          </div>
        </div>
      </div>

      <!-- Deribit -->
      <div class="scard" id="scard-deribit">
        <div class="scard-header">
          <span class="scard-icon" style="background:#00D492;color:#000">D</span>
          <span class="scard-name">Deribit</span>
          <span class="scard-status" id="s-dr-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-dr-key" autocomplete="off" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.secret_key">Secret Key</div>
          <div class="sc-pw-row"><input id="s-dr-secret" type="password" autocomplete="off"><button class="sc-eye" onclick="toggleSecret('s-dr-secret',this)" title="Show/Hide">&#128065;</button></div>
          <div class="sc-row">
            <div style="flex:1"><div class="f-label" data-i18n="settings.trade_type">交易类型</div><select id="s-dr-tradetype"><option value="futures">合约 Futures</option></select></div>
            <div><div class="f-label" data-i18n="settings.testnet">测试网</div><div class="toggle-switch" id="s-dr-testnet-toggle" onclick="toggleSwitch(this)"></div><input type="hidden" class="toggle-input" id="s-dr-testnet" value="0"></div>
          </div>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testExchange('deribit')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveExchange('deribit')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-dr-msg"></span>
          </div>
        </div>
      </div>

    </div>
  </div>
  </div><!-- #stab-exchange -->

  <!-- Tab: AI Model Configuration -->
  <div class="stab-content" id="stab-ai">
  <div style="display:flex;align-items:center;gap:12px;margin-bottom:16px;padding:10px 14px;background:var(--card);border:1px solid var(--border);border-radius:8px">
    <span style="font-size:12px;color:var(--muted);white-space:nowrap" data-i18n="settings.default_provider">默认模型</span>
    <select id="ai-default-provider" onchange="saveDefaultAI()" style="flex:1;max-width:260px;margin-bottom:0">
      <option value="" data-i18n="settings.auto_select">自动选择(第一个已配置)</option>
      <option value="openai">OpenAI</option><option value="qwen">通义千问 Qwen</option><option value="anthropic">Anthropic Claude</option>
      <option value="google">Google Gemini</option><option value="deepseek">DeepSeek</option><option value="doubao">豆包 Doubao</option>
      <option value="baidu">文心一言 ERNIE</option><option value="hunyuan">混元 Hunyuan</option>
      <option value="mistral">Mistral AI</option><option value="zhipu">智谱 GLM</option><option value="yi">零一万物 Yi</option><option value="local">本地模型 Local</option>
    </select>
  </div>
  <div class="settings-module">
    <div class="settings-module-title" data-i18n="settings.ai_module_title">AI大模型配置</div>
    <div class="settings-card-grid">

      <!-- OpenAI -->
      <div class="scard scard-ai" id="scard-ai-openai">
        <div class="scard-header">
          <span class="scard-icon" style="background:#10A37F;color:#1f2328">O</span>
          <span class="scard-name">OpenAI</span>
          <span class="scard-status" id="s-ai-openai-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-ai-openai-key" autocomplete="off" placeholder="sk-..." style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.base_url">Base URL</div>
          <input id="s-ai-openai-url" value="https://api.openai.com/v1" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.model">模型</div>
          <select id="s-ai-openai-model"><option value="gpt-4o">gpt-4o</option><option value="gpt-4-turbo">gpt-4-turbo</option><option value="gpt-4">gpt-4</option><option value="gpt-3.5-turbo">gpt-3.5-turbo</option></select>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testAI('openai')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveAI('openai')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-ai-openai-msg"></span>
          </div>
        </div>
      </div>

      <!-- Qwen (通义千问) -->
      <div class="scard scard-ai" id="scard-ai-qwen">
        <div class="scard-header">
          <span class="scard-icon" style="background:#FF6A00;color:#1f2328">Q</span>
          <span class="scard-name">通义千问 Qwen</span>
          <span class="scard-status" id="s-ai-qwen-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-ai-qwen-key" autocomplete="off" placeholder="sk-..." style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.base_url">Base URL</div>
          <input id="s-ai-qwen-url" value="https://dashscope.aliyuncs.com/compatible-mode/v1" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.model">模型</div>
          <select id="s-ai-qwen-model"><option value="qwen-max">qwen-max</option><option value="qwen-plus">qwen-plus</option><option value="qwen-turbo">qwen-turbo</option></select>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testAI('qwen')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveAI('qwen')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-ai-qwen-msg"></span>
          </div>
        </div>
      </div>

      <!-- Anthropic (Claude) -->
      <div class="scard scard-ai" id="scard-ai-anthropic">
        <div class="scard-header">
          <span class="scard-icon" style="background:#D97706;color:#1f2328">C</span>
          <span class="scard-name">Anthropic Claude</span>
          <span class="scard-status" id="s-ai-anthropic-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-ai-anthropic-key" autocomplete="off" placeholder="sk-ant-..." style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.base_url">Base URL</div>
          <input id="s-ai-anthropic-url" value="https://api.anthropic.com/v1" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.model">模型</div>
          <select id="s-ai-anthropic-model"><option value="claude-sonnet-4-6">claude-sonnet-4-6</option><option value="claude-opus-4-7">claude-opus-4-7</option><option value="claude-haiku-4-5">claude-haiku-4-5</option></select>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testAI('anthropic')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveAI('anthropic')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-ai-anthropic-msg"></span>
          </div>
        </div>
      </div>

      <!-- Google Gemini -->
      <div class="scard scard-ai" id="scard-ai-google">
        <div class="scard-header">
          <span class="scard-icon" style="background:#4285F4;color:#1f2328">G</span>
          <span class="scard-name">Google Gemini</span>
          <span class="scard-status" id="s-ai-google-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-ai-google-key" autocomplete="off" placeholder="AIza..." style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.base_url">Base URL</div>
          <input id="s-ai-google-url" value="https://generativelanguage.googleapis.com/v1" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.model">模型</div>
          <select id="s-ai-google-model"><option value="gemini-1.5-pro">gemini-1.5-pro</option><option value="gemini-1.5-flash">gemini-1.5-flash</option><option value="gemini-2.0-flash">gemini-2.0-flash</option></select>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testAI('google')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveAI('google')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-ai-google-msg"></span>
          </div>
        </div>
      </div>

      <!-- DeepSeek -->
      <div class="scard scard-ai" id="scard-ai-deepseek">
        <div class="scard-header">
          <span class="scard-icon" style="background:#4D6BFE;color:#1f2328">D</span>
          <span class="scard-name">DeepSeek</span>
          <span class="scard-status" id="s-ai-deepseek-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-ai-deepseek-key" autocomplete="off" placeholder="sk-..." style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.base_url">Base URL</div>
          <input id="s-ai-deepseek-url" value="https://api.deepseek.com/v1" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.model">模型</div>
          <select id="s-ai-deepseek-model"><option value="deepseek-chat">deepseek-chat</option><option value="deepseek-coder">deepseek-coder</option></select>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testAI('deepseek')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveAI('deepseek')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-ai-deepseek-msg"></span>
          </div>
        </div>
      </div>

      <!-- Doubao (ByteDance) -->
      <div class="scard scard-ai" id="scard-ai-doubao">
        <div class="scard-header">
          <span class="scard-icon" style="background:#00D4AA;color:#000">豆</span>
          <span class="scard-name">豆包 Doubao</span>
          <span class="scard-status" id="s-ai-doubao-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-ai-doubao-key" autocomplete="off" placeholder="sk-..." style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.base_url">Base URL</div>
          <input id="s-ai-doubao-url" value="https://ark.cn-beijing.volces.com/api/v3" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.model">模型</div>
          <select id="s-ai-doubao-model"><option value="doubao-pro">doubao-pro</option><option value="doubao-lite">doubao-lite</option></select>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testAI('doubao')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveAI('doubao')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-ai-doubao-msg"></span>
          </div>
        </div>
      </div>

      <!-- Baidu ERNIE -->
      <div class="scard scard-ai" id="scard-ai-baidu">
        <div class="scard-header">
          <span class="scard-icon" style="background:#2932E1;color:#1f2328">文</span>
          <span class="scard-name">文心一言 ERNIE</span>
          <span class="scard-status" id="s-ai-baidu-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-ai-baidu-key" autocomplete="off" placeholder="sk-..." style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.base_url">Base URL</div>
          <input id="s-ai-baidu-url" value="https://qianfan.baidubce.com/v2" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.model">模型</div>
          <select id="s-ai-baidu-model"><option value="ernie-4.0">ERNIE 4.0</option><option value="ernie-3.5">ERNIE 3.5</option><option value="ernie-speed">ERNIE Speed</option></select>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testAI('baidu')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveAI('baidu')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-ai-baidu-msg"></span>
          </div>
        </div>
      </div>

      <!-- Tencent Hunyuan -->
      <div class="scard scard-ai" id="scard-ai-hunyuan">
        <div class="scard-header">
          <span class="scard-icon" style="background:#00A4FF;color:#1f2328">混</span>
          <span class="scard-name">混元 Hunyuan</span>
          <span class="scard-status" id="s-ai-hunyuan-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-ai-hunyuan-key" autocomplete="off" placeholder="sk-..." style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.base_url">Base URL</div>
          <input id="s-ai-hunyuan-url" value="https://api.hunyuan.cloud.tencent.com/v1" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.model">模型</div>
          <select id="s-ai-hunyuan-model"><option value="hunyuan-pro">hunyuan-pro</option><option value="hunyuan-lite">hunyuan-lite</option></select>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testAI('hunyuan')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveAI('hunyuan')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-ai-hunyuan-msg"></span>
          </div>
        </div>
      </div>

      <!-- Mistral AI -->
      <div class="scard scard-ai" id="scard-ai-mistral">
        <div class="scard-header">
          <span class="scard-icon" style="background:#FF7A00;color:#000">M</span>
          <span class="scard-name">Mistral AI</span>
          <span class="scard-status" id="s-ai-mistral-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-ai-mistral-key" autocomplete="off" placeholder="sk-..." style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.base_url">Base URL</div>
          <input id="s-ai-mistral-url" value="https://api.mistral.ai/v1" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.model">模型</div>
          <select id="s-ai-mistral-model"><option value="mistral-large">mistral-large</option><option value="mistral-small">mistral-small</option><option value="open-mistral-8x7b">open-mistral-8x7b</option></select>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testAI('mistral')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveAI('mistral')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-ai-mistral-msg"></span>
          </div>
        </div>
      </div>

      <!-- Zhipu AI (GLM) -->
      <div class="scard scard-ai" id="scard-ai-zhipu">
        <div class="scard-header">
          <span class="scard-icon" style="background:#3B5998;color:#1f2328">智</span>
          <span class="scard-name">智谱AI GLM</span>
          <span class="scard-status" id="s-ai-zhipu-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-ai-zhipu-key" autocomplete="off" placeholder="sk-..." style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.base_url">Base URL</div>
          <input id="s-ai-zhipu-url" value="https://open.bigmodel.cn/api/paas/v4" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.model">模型</div>
          <select id="s-ai-zhipu-model"><option value="glm-4">GLM-4</option><option value="glm-4-flash">GLM-4 Flash</option><option value="glm-3-turbo">GLM-3 Turbo</option></select>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testAI('zhipu')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveAI('zhipu')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-ai-zhipu-msg"></span>
          </div>
        </div>
      </div>

      <!-- 01.AI Yi -->
      <div class="scard scard-ai" id="scard-ai-yi">
        <div class="scard-header">
          <span class="scard-icon" style="background:#6366F1;color:#1f2328">一</span>
          <span class="scard-name">零一万物 Yi</span>
          <span class="scard-status" id="s-ai-yi-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-ai-yi-key" autocomplete="off" placeholder="sk-..." style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.base_url">Base URL</div>
          <input id="s-ai-yi-url" value="https://api.lingyiwanwu.com/v1" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.model">模型</div>
          <select id="s-ai-yi-model"><option value="yi-large">yi-large</option><option value="yi-medium">yi-medium</option><option value="yi-spark">yi-spark</option></select>
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testAI('yi')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveAI('yi')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-ai-yi-msg"></span>
          </div>
        </div>
      </div>

      <!-- Local Model -->
      <div class="scard scard-ai" id="scard-ai-local" style="border-style:dashed">
        <div class="scard-header">
          <span class="scard-icon" style="background:#6B7280;color:#1f2328">本</span>
          <span class="scard-name" data-i18n="settings.ai_local">本地模型</span>
          <span class="scard-status" id="s-ai-local-status">-</span>
        </div>
        <div class="scard-body">
          <div class="f-label" data-i18n="settings.api_key">API Key</div>
          <input id="s-ai-local-key" autocomplete="off" placeholder="ollama / vllm / ..." style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.base_url">Base URL</div>
          <input id="s-ai-local-url" value="http://localhost:11434/v1" style="margin-bottom:8px">
          <div class="f-label" data-i18n="settings.model">模型</div>
          <input id="s-ai-local-model" value="llama3" placeholder="llama3 / qwen2 / ...">
          <div class="sc-actions">
            <button class="btn sc-btn-test" onclick="testAI('local')" data-i18n="settings.test_conn">测试连接</button>
            <button class="btn btn-p" onclick="saveAI('local')" data-i18n="settings.save">保存</button>
            <span class="sc-msg" id="s-ai-local-msg"></span>
          </div>
        </div>
      </div>

    </div>
  </div>
  </div><!-- #stab-ai -->

  <!-- Tab: Agent CC Switch + AI Tools -->
  <div class="stab-content" id="stab-agent">
  <div class="settings-module">
    <div class="settings-module-title">Agent 接入</div>

    <!-- Explanation -->
    <div style="display:flex;align-items:center;gap:12px;margin-bottom:14px;padding:10px 14px;background:var(--bg);border:1px solid var(--border);border-radius:8px;font-size:10px;color:var(--muted)">
      <span style="font-size:16px">&#9889;</span>
      <span><b style="color:#1f2328">CC Switch</b> 将 Anthropic 格式请求转成 OpenAI 格式，让 Cursor / Claude Code / Codex 可以使用 DeepSeek / Qwen 等模型。</span>
    </div>

    <div class="settings-card-grid">

      <!-- Card 1: CC Switch -->
      <div class="scard" style="padding:16px">
        <div class="scard-header" style="margin-bottom:10px">
          <span class="scard-icon" style="background:#7C3AED;color:#1f2328">C</span>
          <span class="scard-name">CC Switch 代理</span>
          <span id="ag-ccs-status" style="font-size:10px;margin-left:auto;color:var(--muted)">已停止</span>
        </div>
        <div class="f-label">后端模型</div>
        <select id="ag-ccs-model" onchange="onCCSwitchModelChange()" style="margin-bottom:8px">
          <option value="">加载中...</option>
        </select>
        <div class="f-label">API 地址</div>
        <input id="ag-ccs-url" value="https://api.deepseek.com/v1/chat/completions" style="margin-bottom:8px">
        <div class="f-label">API Key <span style="font-size:9px;color:#10B981">（自动同步）</span></div>
        <input id="ag-ccs-key" type="password" readonly style="margin-bottom:8px;background:var(--bg);color:var(--muted)" placeholder="选择模型后自动填充">
        <div class="f-label">监听端口</div>
        <input id="ag-ccs-port" value="12435" style="max-width:120px;margin-bottom:8px">
        <div style="display:flex;gap:8px;align-items:center;margin-bottom:8px">
          <span style="font-size:9px;color:var(--muted)">代理地址</span>
          <code id="ag-ccs-addr" style="font-size:10px;color:var(--accent)">http://127.0.0.1:12435</code>
        </div>
        <div style="display:flex;gap:8px">
          <button class="btn btn-p" id="ag-ccs-start-btn" onclick="ccSwitchStart()">启动</button>
          <button class="btn btn-d" id="ag-ccs-stop-btn" onclick="ccSwitchStop()" style="display:none">停止</button>
        </div>
      </div>

      <!-- Card 2: Cursor -->
      <div class="scard" style="padding:16px">
        <div class="scard-header" style="margin-bottom:10px">
          <span class="scard-icon" style="background:#007AFF;color:#1f2328">C</span>
          <span class="scard-name">Cursor</span>
          <div class="toggle-switch" id="ag-cursor-toggle" onclick="toggleAgentTool('cursor')" style="margin-left:auto"></div>
        </div>
        <div id="ag-cursor-config" style="display:none;font-size:10px;color:var(--muted);line-height:1.8">
          1. Cursor Settings → Models → 关闭所有 Anthropic 模型<br>
          2. 添加 OpenAI API Key：填 <b style="color:var(--accent)">CC Switch 代理所用的 Key</b><br>
          3. 添加 Base URL：<code id="ag-cursor-addr" style="color:var(--accent)">http://127.0.0.1:12435/v1</code><br>
          4. 选择对应模型即可对话
        </div>
      </div>

      <!-- Card 3: Claude Code -->
      <div class="scard" style="padding:16px">
        <div class="scard-header" style="margin-bottom:10px">
          <span class="scard-icon" style="background:#D97706;color:#1f2328">C</span>
          <span class="scard-name">Claude Code</span>
          <div class="toggle-switch" id="ag-claude-toggle" onclick="toggleAgentTool('claude')" style="margin-left:auto"></div>
        </div>
        <div id="ag-claude-config" style="display:none;font-size:10px;color:var(--muted);line-height:1.8">
          1. 确保本页面 CC Switch 代理已启动<br>
          2. 终端设环境变量：<br>
          &nbsp;&nbsp;<code>ANTHROPIC_BASE_URL=http://127.0.0.1:12435</code><br>
          &nbsp;&nbsp;<code>ANTHROPIC_API_KEY=any-value</code><br>
          3. 项目目录下执行 <code>claude</code> 即可
        </div>
      </div>

      <!-- Card 4: Codex -->
      <div class="scard" style="padding:16px">
        <div class="scard-header" style="margin-bottom:10px">
          <span class="scard-icon" style="background:#10A37F;color:#1f2328">C</span>
          <span class="scard-name">Codex</span>
          <div class="toggle-switch" id="ag-codex-toggle" onclick="toggleAgentTool('codex')" style="margin-left:auto"></div>
        </div>
        <div id="ag-codex-config" style="display:none;font-size:10px;color:var(--muted);line-height:1.8">
          1. Codex Settings → Providers → Add Custom<br>
          2. URL 填：<code id="ag-codex-addr" style="color:var(--accent)">http://127.0.0.1:12435/v1</code><br>
          3. API Key 填 CC Switch 代理所用的 Key<br>
          4. 选择对应模型即可使用
        </div>
      </div>

    </div>

    <!-- API Endpoints -->
    <div class="scard" style="padding:14px;margin-top:14px">
      <div class="scard-header" style="margin-bottom:8px">
        <span class="scard-icon" style="background:var(--accent);color:#1f2328">A</span>
        <span class="scard-name">可用 API 端点 (11)</span>
        <button class="btn btn-xs" onclick="toggleMCPTools()" style="margin-left:auto;font-size:9px;padding:2px 8px" id="ag-tools-toggle">展开</button>
      </div>
      <div id="ag-tools-list" style="display:none;font-size:10px">
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:4px 12px">
          <div style="padding:3px 6px;border-radius:3px;background:var(--bg)"><b style="color:#58a6ff">get_market_data</b><span style="color:var(--muted)"> — 实时行情/价格/深度</span></div>
          <div style="padding:3px 6px;border-radius:3px;background:var(--bg)"><b style="color:#58a6ff">run_backtest</b><span style="color:var(--muted)"> — 运行策略回测</span></div>
          <div style="padding:3px 6px;border-radius:3px;background:var(--bg)"><b style="color:#58a6ff">list_strategies</b><span style="color:var(--muted)"> — 列出策略模板</span></div>
          <div style="padding:3px 6px;border-radius:3px;background:var(--bg)"><b style="color:#58a6ff">deploy_strategy</b><span style="color:var(--muted)"> — 部署策略到策略库</span></div>
          <div style="padding:3px 6px;border-radius:3px;background:var(--bg)"><b style="color:#58a6ff">start_strategy</b><span style="color:var(--muted)"> — 启动策略</span></div>
          <div style="padding:3px 6px;border-radius:3px;background:var(--bg)"><b style="color:#58a6ff">stop_strategy</b><span style="color:var(--muted)"> — 停止策略</span></div>
          <div style="padding:3px 6px;border-radius:3px;background:var(--bg)"><b style="color:#58a6ff">delete_strategy</b><span style="color:var(--muted)"> — 删除策略</span></div>
          <div style="padding:3px 6px;border-radius:3px;background:var(--bg)"><b style="color:#58a6ff">place_paper_order</b><span style="color:var(--muted)"> — 模拟下单</span></div>
          <div style="padding:3px 6px;border-radius:3px;background:var(--bg)"><b style="color:#58a6ff">get_orders</b><span style="color:var(--muted)"> — 查询活跃订单</span></div>
          <div style="padding:3px 6px;border-radius:3px;background:var(--bg)"><b style="color:#58a6ff">cancel_order</b><span style="color:var(--muted)"> — 撤销订单</span></div>
          <div style="padding:3px 6px;border-radius:3px;background:var(--bg)"><b style="color:#58a6ff">get_stats</b><span style="color:var(--muted)"> — 账户/持仓统计</span></div>
        </div>
      </div>
    </div>

  </div>
  </div><!-- #stab-agent -->

	  <!-- Tab: UI Settings -->
	  <div class="stab-content" id="stab-ui">
	    <div class="settings-module">
	      <div class="settings-module-title">界面主题</div>
	      <div class="settings-card-grid">
	        <div class="scard" style="padding:16px">
	          <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
	            <div>
	              <div style="font-weight:600;font-size:13px;color:#1f2328">深色主题</div>
	              <div style="font-size:10px;color:var(--muted);margin-top:2px">GitHub Dark 风格配色</div>
	            </div>
	            <div class="toggle-switch active" id="ui-theme-dark-toggle" onclick="toggleSwitch(this);this.classList.contains('active')?document.body.style.filter='':document.body.style.filter='invert(.9) hue-rotate(180deg)'"></div>
	          </div>
	          <div style="display:flex;align-items:center;justify-content:space-between">
	            <div>
	              <div style="font-weight:600;font-size:13px;color:#1f2328">跟随系统</div>
	              <div style="font-size:10px;color:var(--muted);margin-top:2px">自动匹配系统亮色/暗色模式</div>
	            </div>
	            <div class="toggle-switch" id="ui-theme-system-toggle" onclick="toggleSwitch(this)"></div>
	          </div>
	        </div>
	      </div>
	    </div>

	    <div class="settings-module">
	      <div class="settings-module-title">字体与缩放</div>
	      <div class="settings-card-grid">
	        <div class="scard" style="padding:16px">
	          <div class="f-label">字体大小</div>
	          <div style="display:flex;align-items:center;gap:10px;margin-bottom:12px">
	            <span style="font-size:10px;color:var(--muted)">A</span>
	            <input type="range" id="ui-font-size" min="11" max="16" value="13" style="flex:1" oninput="document.body.style.fontSize=this.value+'px'">
	            <span style="font-size:16px;color:var(--muted)">A</span>
	          </div>
	          <div class="f-label">代码字体大小</div>
	          <div style="display:flex;align-items:center;gap:10px">
	            <span style="font-size:9px;color:var(--muted);font-family:monospace">1em</span>
	            <input type="range" id="ui-code-font-size" min="10" max="15" value="12" style="flex:1" oninput="document.querySelectorAll('pre,code,textarea').forEach(function(el){el.style.fontSize=this.value+'px'}.bind(this))">
	            <span style="font-size:15px;color:var(--muted);font-family:monospace">1em</span>
	          </div>
	        </div>
	      </div>
	    </div>

	    <div class="settings-module">
	      <div class="settings-module-title">K线图表</div>
	      <div class="settings-card-grid">
	        <div class="scard" style="padding:16px">
	          <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px">
	            <div>
	              <div style="font-weight:600;font-size:13px;color:#1f2328">涨色</div>
	              <div style="font-size:10px;color:var(--muted);margin-top:2px">阳线 / 买入</div>
	            </div>
	            <input type="color" id="ui-kline-up" value="#3fb950" style="width:36px;height:28px;border:none;border-radius:4px;cursor:pointer">
	          </div>
	          <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px">
	            <div>
	              <div style="font-weight:600;font-size:13px;color:#1f2328">跌色</div>
	              <div style="font-size:10px;color:var(--muted);margin-top:2px">阴线 / 卖出</div>
	            </div>
	            <input type="color" id="ui-kline-down" value="#f85149" style="width:36px;height:28px;border:none;border-radius:4px;cursor:pointer">
	          </div>
	          <div style="display:flex;align-items:center;justify-content:space-between">
	            <div>
	              <div style="font-weight:600;font-size:13px;color:#1f2328">成交量透明度</div>
	              <div style="font-size:10px;color:var(--muted);margin-top:2px">Volume bar 不透明度</div>
	            </div>
	            <input type="range" id="ui-vol-opacity" min="20" max="80" value="40" style="width:100px">
	          </div>
	        </div>
	      </div>
	    </div>

	    <div class="settings-module">
	      <div class="settings-module-title">悬浮球设置</div>
	      <div class="settings-card-grid">
	        <div class="scard" style="padding:16px">
	          <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px">
	            <div>
	              <div style="font-weight:600;font-size:13px;color:#1f2328">显示悬浮球</div>
	              <div style="font-size:10px;color:var(--muted);margin-top:2px">右下角 AI 助手入口</div>
	            </div>
	            <div class="toggle-switch active" id="ui-show-ball-toggle" onclick="toggleSwitch(this);var b=document.getElementById('agent-ball');if(b)b.style.display=this.classList.contains('active')?'':'none'"></div>
	          </div>
	          <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px">
	            <div>
	              <div style="font-weight:600;font-size:13px;color:#1f2328">悬浮球大小</div>
	              <div style="font-size:10px;color:var(--muted);margin-top:2px">默认 52px</div>
	            </div>
	            <select id="ui-ball-size" style="width:120px" onchange="var b=document.getElementById('agent-ball');if(b){var s=this.value+'px';b.style.width=s;b.style.height=s}">
	              <option value="44">小 (44px)</option>
	              <option value="52" selected>中 (52px)</option>
	              <option value="60">大 (60px)</option>
	            </select>
	          </div>
	          <div style="display:flex;align-items:center;justify-content:space-between">
	            <div>
	              <div style="font-weight:600;font-size:13px;color:#1f2328">重置位置</div>
	              <div style="font-size:10px;color:var(--muted);margin-top:2px">恢复悬浮球和面板默认位置</div>
	            </div>
	            <button class="btn" onclick="resetUIPositions()" style="font-size:10px;padding:4px 10px">重置</button>
	          </div>
	        </div>
	      </div>
	    </div>

	    <div class="settings-module">
	      <div class="settings-module-title">动画与性能</div>
	      <div class="settings-card-grid">
	        <div class="scard" style="padding:16px">
	          <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px">
	            <div>
	              <div style="font-weight:600;font-size:13px;color:#1f2328">减少动画</div>
	              <div style="font-size:10px;color:var(--muted);margin-top:2px">关闭过渡动画以提升性能</div>
	            </div>
	            <div class="toggle-switch" id="ui-reduce-motion-toggle" onclick="toggleSwitch(this);document.body.style.setProperty('--transition-speed',this.classList.contains('active')?'0s':'.15s')"></div>
	          </div>
	          <div style="display:flex;align-items:center;justify-content:space-between">
	            <div>
	              <div style="font-weight:600;font-size:13px;color:#1f2328">数据刷新间隔</div>
	              <div style="font-size:10px;color:var(--muted);margin-top:2px">Dashboard 自动刷新频率</div>
	            </div>
	            <select id="ui-refresh-interval" style="width:130px">
	              <option value="5000">5 秒 (快速)</option>
	              <option value="10000" selected>10 秒 (默认)</option>
	              <option value="30000">30 秒 (省电)</option>
	              <option value="60000">60 秒 (最低)</option>
	            </select>
	          </div>
	        </div>
	      </div>
	    </div>

	    <div style="display:flex;gap:10px;margin-top:16px">
	      <button class="btn btn-p" onclick="saveUISettings()">保存界面设置</button>
	      <button class="btn" onclick="resetUISettings()">恢复默认</button>
	      <span class="sc-msg" id="ui-settings-msg"></span>
	    </div>

	  </div><!-- #stab-ui -->

</div>
</div>

<div id="toast"></div>

<script>
// ═══════════════════════════════════════════════ STATE ═══════════════════════════════════════════════
var S={
  ws:null,wsOk:false,
  page:'dash',subPage:'spot',
  prices:{},bids:[],asks:[],
  tick:0,
  tradeSide:'BUY',
  lang:'zh',
  strategyForm:{mode:'create',id:null,data:{},conditions:[],tpTiers:[]},
  dashChart:null,dashSeries:null,
  tradeChart:null,tradeCandle:null,tradeVol:null,
  btChart:null,btSeries:null,
  aiStrategy:null,aiReport:null,aiChart:null,aiSeries:null,
  settingsTab:'exchange',
};

// ═══════════════════════════════════════════════ I18N ═══════════════════════════════════════════════
S.i18n={
zh:{
  'nav.spot':'现货策略','nav.contract':'合约策略','nav.templates':'策略模板管理','nav.logs':'策略运行日志',
  'kpi.margin':'保证金可用余额','kpi.holdings':'现货/合约持仓市值','kpi.protection':'盈利保护开关','kpi.max_orders':'最大并单数',
  'btn.create':'创建策略','btn.create_spot':'创建现货策略','btn.create_contract':'创建合约策略','btn.edit':'编辑','btn.delete':'删除',
  'btn.start':'启动','btn.stop':'停止','btn.save':'保存','btn.cancel':'取消','btn.apply':'应用','btn.clear':'清除',
  'btn.batch_start':'批量启动','btn.batch_stop':'批量停止','btn.batch_delete':'批量删除',
  'btn.conditions':'补仓条件','btn.tp_params':'止盈参数','btn.add_row':'+','btn.del_row':'x',
  'status.running':'执行中','status.stopped':'未启动','status.all':'全部','status.monitoring':'检测中','status.history':'执行历史',
  'filter.search':'搜索币种...','filter.type':'策略类型筛选','filter.level':'日志级别',
  'table.id':'策略ID','table.coin':'币种','table.type':'策略类型','table.status':'状态','table.pnl':'当前盈亏',
  'table.actions':'操作','table.direction':'方向','table.leverage':'杠杆倍数',
  'stats.total':'共','stats.running':'执行中','stats.pnl':'总盈亏','stats.short':'开空','stats.long':'开多','stats.online':'在线单',
  'form.basic':'基础信息','form.entry':'开仓设置','form.avg':'补仓设置','form.pnl':'盈亏设置','form.burn':'燃烧设置',
  'form.strategy_type':'策略类型','form.template':'我的策略模板','form.coin':'币种',
  'form.limit_price':'挂单价格 (0=市价)','form.first_amount':'首单额度 (USDT)','form.first_mult':'首单加倍 (倍)',
  'form.cycle_type':'循环类型','form.cycle_count':'循环次数','form.single':'单次','form.loop':'循环',
  'form.avg_enable':'开启补仓','form.avg_count':'补仓次数','form.avg_total':'补仓总金额 (USDT)',
  'form.anti_waterfall':'开启防瀑布','form.anti_waterfall_ratio':'防瀑布比例 (%)',
  'form.tp_mode':'止盈方式','form.tp_full':'全仓','form.tp_tail':'尾单','form.tp_first_tail':'首尾',
  'form.tp_type':'止盈设置','form.tp_static':'静态','form.tp_trailing':'移动',
  'form.entry_macd':'开仓MACD监测','form.entry_counter_ema':'逆势EMA监测','form.entry_trend_ema':'顺势EMA监测',
  'form.direction':'策略方向','form.dir_long':'多','form.dir_short':'空','form.dir_both':'双向',
  'form.entry_double':'开仓加倍','form.follow_trend':'顺势而为','form.leverage':'杠杆倍数',
  'form.avg_macd':'补仓MACD监测','form.avg_ema':'补仓EMA监测',
  'form.reverse_tp':'反向止盈','form.reverse_sl':'反向止损','form.sl_enable':'止损开关',
  'form.sl_type':'止损类型','form.sl_ratio':'止损比例 (%)','form.sl_price':'止损价格','form.sl_amount':'止损金额',
  'form.burn_global':'全局燃烧开关','form.burn_counter':'对向燃烧开关',
  'form.save_default':'保存为默认策略',
  'modal.avg_title_spot':'补仓条件设置 (7单)','modal.avg_title_contract':'补仓条件设置 (9单)',
  'modal.tp_title':'移动止盈设置 (4档)','modal.delete_title':'确认删除','modal.delete_msg':'确定要删除此策略吗？',
  'col.order':'序号','col.mult':'倍数','col.gap':'差价(跌幅%)','col.callback':'回调(涨幅%)','col.ema':'EMA监测',
  'col.tier':'档位','col.tp':'止盈比例%','col.trail':'止盈回撤%',
  'tpl.spot':'现货模板','tpl.contract':'合约模板','tpl.save_as':'保存当前为模板','tpl.name':'模板名称',
  'log.auto_refresh':'自动刷新','log.no_logs':'暂无日志记录',
  'toast.saved':'保存成功','toast.deleted':'删除成功','toast.started':'策略已启动','toast.stopped':'策略已停止',
  'toast.error':'操作失败','toast.tpl_saved':'模板已保存','toast.cancelled':'已取消','toast.failed':'操作失败',
  // Nav & common
  'nav.dash':'仪表盘','nav.trade':'交易','nav.ai_oppty':'AI分析','nav.backtest':'回测','nav.strategy':'策略','nav.settings':'设置',
  'kpi.equity':'总权益','kpi.balance':'可用余额','kpi.return':'总收益','kpi.max_dd':'最大回撤',
  'kpi.sharpe':'夏普比率','kpi.winrate':'胜率',
  'label.no_orders':'暂无订单','label.no_active_orders':'暂无活跃订单','label.no_positions':'暂无持仓','label.no_history':'暂无历史记录','label.connecting':'连接中','label.offline':'已断开','label.connected':'已连接',
  'label.annualized':'年化','label.current':'当前','label.empty_spot':'暂无现货策略','label.empty_spot_desc':'点击下方按钮创建你的第一个策略',
  'label.empty_ct':'暂无合约策略','label.empty_ct_desc':'点击下方按钮创建你的第一个策略',
  'label.empty_tpl':'暂无策略模板','label.empty_tpl_desc':'创建策略时勾选「保存为默认」即可保存为模板',
  'label.empty_log':'暂无运行日志','label.empty_log_desc':'启动策略后将在此显示运行日志',
  'log.all_strategies':'全部策略',
  'sidebar.label':'策略管理','middle.account':'账户数据','middle.risk':'风控参数',
  // Dashboard
  'dash.equity':'权益曲线','dash.orders':'最近订单','dash.risk':'风控监控',
  // Trading
  'trade.place_order':'下单','trade.symbol':'交易对','trade.side':'方向','trade.type':'类型',
  'trade.price':'价格','trade.qty':'数量','trade.submit':'提交订单',
  'trade.orderbook':'订单簿','trade.active_orders':'当前委托','trade.positions':'持仓','trade.history':'历史','trade.chart':'图表',
  'trade.available':'可用','trade.cancel_all':'撤销全部','trade.buy_btc':'买入 BTC','trade.sell_btc':'卖出 BTC','trade.order_history':'历史委托','trade.trade_history':'成交记录','toast.order_placed':'订单已提交','toast.cancelled':'已撤销','toast.failed':'操作失败',
  'val.select_symbol':'请选择交易对','val.invalid_qty':'请输入有效数量','val.qty_too_large':'数量过大','val.invalid_price':'请输入有效价格','val.invalid_side':'无效的交易方向','val.no_connection':'未连接到服务器',
  // Backtest
  'bt.config':'回测配置','bt.strategy':'策略','bt.symbol':'交易对',
  'bt.capital':'初始资金 (USDT)','bt.fee':'手续费率','bt.slippage':'滑点',
  'bt.bars':'K线数','bt.price':'基准价格','bt.params':'策略参数','bt.run':'运行回测',
  'bt.results':'回测结果','bt.equity':'权益曲线',
  // Settings
  'settings.page_title':'系统设置','settings.exchange_title':'交易所配置','settings.default_exchange':'默认交易所','settings.ai_module_title':'AI大模型配置',
  'settings.api_key':'API Key','settings.secret_key':'Secret Key','settings.passphrase':'Passphrase',
  'settings.testnet':'测试网','settings.trade_type':'交易类型','settings.save':'保存',
  'settings.base_url':'接口地址','settings.model':'模型',
  'settings.test_conn':'测试连接','settings.connected':'已连接','settings.not_connected':'未连接',
  'settings.configured':'已配置','settings.not_configured':'未配置','settings.testing':'检测中...',
  'settings.conn_ok':'连接成功','settings.conn_fail':'连接失败','settings.saving':'保存中...',
  'settings.ai_local':'本地模型','settings.default_provider':'默认模型','settings.auto_select':'自动选择（第一个已配置）',
  'settings.agent_title':'Agent接入',
  'settings.copy_mcp':'复制配置','settings.mcp_copied':'MCP配置已复制到剪贴板',
  'settings.agent_tools_title':'接入 AI 编码工具',
  'settings.agent_cursor_desc':'将下方配置保存为项目根目录的 <code>.cursor/mcp.json</code>，重启 Cursor 即可在 AI 面板中操作你的交易系统。',
  'settings.agent_claude_desc':'将下方配置保存为 <code>.claude/mcp.json</code> 或通过 <code>claude mcp add</code> 命令导入。重启 Claude Code 后生效。',
  'settings.agent_codex_desc':'将下方配置添加到 Codex 的 MCP 配置中。在 Codex 设置 → MCP Servers → 添加自定义服务器。',
  'settings.agent_mcp_desc':'通用 MCP 配置，适用于任何支持 MCP 协议的 AI 客户端。使用本地 MCP server <code>python -m xtquant.agent.mcp_server</code>。',
  'settings.download_config':'下载配置文件','settings.config_downloaded':'配置文件已下载',
  'settings.agent_tokens_title':'Agent Token 管理','settings.agent_tokens_desc':'签发 Agent Token 后，AI 编码工具即可通过 MCP 协议操作你的交易系统。Token 仅创建时显示一次。',
  'settings.agent_create_token':'创建 Agent Token','settings.agent_token_name':'Token 名称','settings.agent_scopes':'权限范围',
  'settings.agent_rate_limit':'速率限制 (次/分)','settings.agent_expires':'过期时间','settings.agent_never':'永不过期',
  'settings.agent_paper_only':'仅纸面交易 (paper_only)','settings.agent_paper_hint':'取消勾选后仍需服务端 AGENT_LIVE_TRADING_ENABLED=true 才可实盘',
  'settings.agent_create_btn':'创建','settings.agent_revoke':'吊销','settings.agent_revoke_confirm':'确认吊销此 Token？吊销后立即失效。',
  'settings.agent_token_created':'Token 创建成功！请立即复制保存，关闭后无法再次查看。','settings.agent_copy_token':'复制 Token','settings.agent_token_copied':'Token 已复制到剪贴板',
  'settings.agent_no_tokens':'暂无 Token，点击上方按钮创建',
  'settings.agent_ai_title':'Agent AI 模型配置','settings.agent_ai_desc':'为 Agent 配置大模型，让 MCP Server 具备 AI 推理能力。国内用户可选择 DeepSeek、通义千问等国内模型，或配置代理访问国外模型。',
  'settings.agent_ai_card_title':'Agent 模型','settings.agent_ai_provider':'AI 提供商','settings.agent_ai_select':'请选择模型...',
  'settings.agent_ai_api_key':'API Key','settings.agent_ai_base_url':'Base URL','settings.agent_ai_model':'模型名称',
  'settings.agent_ai_proxy':'代理 (Proxy)','settings.agent_ai_proxy_hint':'国内用户配置代理访问国外模型',
  'settings.agent_ai_http_proxy':'HTTP 代理','settings.agent_ai_https_proxy':'HTTPS 代理',
  // Wizard steps
  'settings.ag_step1':'1. 创建 Token','settings.ag_step2':'2. 选择工具','settings.ag_step3':'3. AI 模型','settings.ag_step4':'4. 完成',
  'settings.ag_create_your_token':'创建你的 Agent Token',
  'settings.ag_scope_read':'只读','settings.ag_scope_read_desc':'行情 + 持仓','settings.ag_scope_backtest':'回测','settings.ag_scope_backtest_desc':'行情 + 回测',
  'settings.ag_scope_trade':'模拟交易','settings.ag_scope_trade_desc':'行情 + 回测 + 下单','settings.ag_scope_full':'完整权限','settings.ag_scope_full_desc':'所有功能 (含策略管理)',
  'settings.ag_select_tool':'选择你的 AI 编码工具',
  'settings.ag_cursor_instructions':'配置将保存为项目根目录的 .cursor/mcp.json，重启 Cursor 后生效。',
  'settings.ag_claude_instructions':'配置将保存为 .claude/mcp.json 或通过 claude mcp add 命令导入。重启 Claude Code 后生效。',
  'settings.ag_codex_instructions':'将配置添加到 Codex 的 MCP 设置中：Settings → MCP Servers → 添加自定义服务器。',
  'settings.ag_mcp_instructions':'通用 MCP 配置，适用于任何支持 MCP 协议的 AI 客户端。命令: python -m xtquant.agent.mcp_server',
  'settings.ag_ready':'配置已就绪！','settings.ag_one_cmd_desc':'复制下方命令到终端执行，即可一键完成配置。',
  'settings.ag_terminal_cmd':'终端命令','settings.copy_cmd':'复制命令','settings.cmd_copied':'命令已复制，粘贴到终端执行即可',
  'settings.ag_view_json':'查看 JSON 配置',
  'settings.ag_config_preview':'MCP 配置预览','settings.ag_cli_alternative':'或通过命令行快速配置 (Claude Code):',
  'settings.ag_prev':'上一步','settings.ag_next':'下一步','settings.ag_skip':'跳过','settings.ag_done':'完成','settings.ag_next_create':'创建并继续',
  'settings.ag_need_token':'请先创建 Token','settings.ag_setup_complete':'配置完成！将配置添加到你的 AI 工具中即可开始使用。',
  'settings.agent_token_created_short':'Token 创建成功',
  'common.cancel':'取消',
  'filter.type_all':'全部类型','filter.level_all':'全部级别',
  'common.selected_text':'已选','common.items':'项','common.selected':'已选','common.all':'全部','common.search':'搜索...',
  'log.col_time':'时间','log.col_level':'级别','log.col_msg':'消息',
  'bt.running':'运行中...','bt.running_status':'正在运行回测...','bt.last_run':'上次运行','bt.trades':'笔交易',
  'nav.ai_gen':'Agent策略生成','nav.native':'Python原生策略','ai.form_title':'Agent策略生成器','ai.interval':'K线周期','ai.risk':'风险偏好',
  'ai.risk_low':'低风险','ai.risk_med':'中风险','ai.risk_high':'高风险',
  'ai.prompt':'策略描述','ai.generate':'生成策略','ai.code':'生成的策略代码',
  'ai.code_placeholder':'点击"生成策略"来创建Agent策略...','ai.results':'回测结果',
  'ai.deploy':'部署为模板','ai.regenerate':'重新生成',
  'ai.generating':'策略生成中...','ai.copy_code':'复制','ai.download_code':'下载',
  'ai.view_code':'查看完整代码','ai.run_backtest':'立即回测',
  'ai.template_label':'预设模板','ai.template_trend':'趋势跟踪型','ai.template_oscillation':'震荡套利型',
  'ai.template_breakout':'突破型','ai.template_custom':'-- 自定义输入 --',
  'ai.mode_quick':'快速生成','ai.mode_advanced':'高级定制','ai.mode_optimize':'策略优化','ai.mode_combo':'组合策略',
  'ai.win_rate':'预期胜率','ai.max_dd':'最大回撤','ai.annual_ret':'年化收益',
  'ai.inspiration':'策略灵感','ai.inspiration_hint':'点击上方标签快速填充策略描述',
  'ai.prompt_placeholder':'【开仓条件】：\n【平仓条件】：\n【止损止盈规则】：\n【特殊限制】（如最大持仓、交易时段）：',
  'native.title':'Python原生策略','native.indicator_mode':'指标策略','native.script_mode':'脚本策略',
  'native.entry_indicator':'开仓指标','native.exit_indicator':'平仓指标',
  'native.run_backtest':'运行回测','native.script_code':'策略代码',
  'native.load_indicator_tpl':'加载指标模板','native.load_empty_tpl':'清空模板',
    'agent.code_title':'策略代码','agent.modified':'已修改','agent.new':'新建','agent.save':'保存','agent.delete':'删除','agent.run':'运行',
  'agent.ai_gen':'AI 生成','agent.gen_code':'生成代码','agent.ai_placeholder':'用自然语言描述你的策略...',
  'agent.backtest':'回测','agent.return':'收益','agent.max_dd':'最大回撤','agent.sharpe':'夏普','agent.trades':'交易',
  'agent.clear':'清除','agent.code_rail':'代码',
'native.code_placeholder':'选择指标参数或编写脚本，然后点击"运行回测"生成策略...',
  'nav.ai_generate':'Agent策略生成',
  'agent.new':'新建','agent.save':'保存','agent.delete':'删除','agent.ai_chat':'AI对话',
  'agent.code_title':'策略代码','agent.copy':'复制','agent.fix':'修复','agent.backtest':'回测',
  'agent.config':'回测配置','agent.results':'回测结果','agent.history':'回测历史','agent.saved':'已保存',
  'agent.capital':'初始资金','agent.fee':'手续费%','agent.slip':'滑点%','agent.dir':'方向',
  'agent.both':'双向','agent.long':'做多','agent.short':'做空','agent.run':'运行回测',
  'agent.ai_smart':'AI智能创建','agent.grid':'网格策略','agent.trend':'趋势策略',
  'agent.arbitrage':'套利策略','agent.dca':'DCA定投','agent.martingale':'马丁格尔',
  'agent.modified':'已修改','agent.no_code':'暂无代码','agent.no_history':'暂无回测历史',
},
en:{
  'nav.spot':'Spot Strategies','nav.contract':'Contract Strategies','nav.templates':'Strategy Templates','nav.logs':'Run Logs',
  'kpi.margin':'Available Margin','kpi.holdings':'Holdings Value','kpi.protection':'Profit Protection','kpi.max_orders':'Max Concurrent Orders',
  'btn.create':'Create Strategy','btn.create_spot':'Create Spot Strategy','btn.create_contract':'Create Contract Strategy',
  'btn.edit':'Edit','btn.delete':'Delete','btn.start':'Start','btn.stop':'Stop','btn.save':'Save','btn.cancel':'Cancel',
  'btn.apply':'Apply','btn.clear':'Clear','btn.batch_start':'Batch Start','btn.batch_stop':'Batch Stop','btn.batch_delete':'Batch Delete',
  'btn.conditions':'Conditions','btn.tp_params':'TP Params','btn.add_row':'+','btn.del_row':'x',
  'status.running':'Running','status.stopped':'Stopped','status.all':'All','status.monitoring':'Monitoring','status.history':'History',
  'filter.search':'Search coin...','filter.type':'Filter by type','filter.level':'Log level',
  'table.id':'Strategy ID','table.coin':'Coin','table.type':'Type','table.status':'Status','table.pnl':'P&L',
  'table.actions':'Actions','table.direction':'Direction','table.leverage':'Leverage',
  'stats.total':'Total','stats.running':'Running','stats.pnl':'Total P&L','stats.short':'Short','stats.long':'Long','stats.online':'Online',
  'form.basic':'Basic Info','form.entry':'Entry Settings','form.avg':'Averaging Settings','form.pnl':'P&L Settings','form.burn':'Burn Settings',
  'form.strategy_type':'Strategy Type','form.template':'My Template','form.coin':'Coin',
  'form.limit_price':'Limit Price (0=Market)','form.first_amount':'First Order (USDT)','form.first_mult':'First Multiplier',
  'form.cycle_type':'Cycle Type','form.cycle_count':'Cycle Count','form.single':'Single','form.loop':'Loop',
  'form.avg_enable':'Enable Averaging','form.avg_count':'Averaging Count','form.avg_total':'Total Amount (USDT)',
  'form.anti_waterfall':'Anti-Waterfall','form.anti_waterfall_ratio':'Waterfall Ratio (%)',
  'form.tp_mode':'Take Profit Mode','form.tp_full':'Full','form.tp_tail':'Tail','form.tp_first_tail':'First-Tail',
  'form.tp_type':'TP Type','form.tp_static':'Static','form.tp_trailing':'Trailing',
  'form.entry_macd':'Entry MACD Monitor','form.entry_counter_ema':'Counter EMA Monitor','form.entry_trend_ema':'Trend EMA Monitor',
  'form.direction':'Direction','form.dir_long':'Long','form.dir_short':'Short','form.dir_both':'Both',
  'form.entry_double':'Entry Doubling','form.follow_trend':'Follow Trend','form.leverage':'Leverage',
  'form.avg_macd':'Avg MACD Monitor','form.avg_ema':'Avg EMA Monitor',
  'form.reverse_tp':'Reverse TP','form.reverse_sl':'Reverse SL','form.sl_enable':'Stop Loss',
  'form.sl_type':'SL Type','form.sl_ratio':'SL Ratio (%)','form.sl_price':'SL Price','form.sl_amount':'SL Amount',
  'form.burn_global':'Global Burn','form.burn_counter':'Counter Burn','form.save_default':'Save as Default',
  'modal.avg_title_spot':'Averaging Conditions (7 Orders)','modal.avg_title_contract':'Averaging Conditions (9 Orders)',
  'modal.tp_title':'Trailing TP Settings (4 Tiers)','modal.delete_title':'Confirm Delete','modal.delete_msg':'Are you sure to delete this strategy?',
  'col.order':'Order#','col.mult':'Multiplier','col.gap':'Gap%','col.callback':'Callback%','col.ema':'EMA',
  'col.tier':'Tier','col.tp':'TP%','col.trail':'Trail%',
  'tpl.spot':'Spot Templates','tpl.contract':'Contract Templates','tpl.save_as':'Save Current as Template','tpl.name':'Template Name',
  'log.auto_refresh':'Auto Refresh','log.no_logs':'No log entries',
  'toast.saved':'Saved successfully','toast.deleted':'Deleted','toast.started':'Strategy started','toast.stopped':'Strategy stopped',
  'toast.error':'Operation failed','toast.tpl_saved':'Template saved','toast.cancelled':'Cancelled','toast.failed':'Failed',
  // Nav & common
  'nav.dash':'Dashboard','nav.trade':'Trading','nav.backtest':'Backtest','nav.strategy':'Strategies','nav.settings':'Settings',
  'kpi.equity':'Total Equity','kpi.balance':'Available Balance','kpi.return':'Total Return','kpi.max_dd':'Max Drawdown',
  'kpi.sharpe':'Sharpe Ratio','kpi.winrate':'Win Rate',
  'label.no_orders':'No orders','label.no_active_orders':'No active orders','label.no_positions':'No positions','label.no_history':'No history','label.connecting':'Connecting','label.offline':'Offline','label.connected':'Connected',
  'label.annualized':'Annualized','label.current':'Current','label.empty_spot':'No spot strategies','label.empty_spot_desc':'Click the button below to create your first strategy',
  'label.empty_ct':'No contract strategies','label.empty_ct_desc':'Click the button below to create your first strategy',
  'label.empty_tpl':'No strategy templates','label.empty_tpl_desc':'Check "Save as Default" when creating a strategy to save as template',
  'label.empty_log':'No run logs','label.empty_log_desc':'Strategy run logs will appear here after starting',
  'log.all_strategies':'All Strategies',
  'sidebar.label':'Strategy Mgmt','middle.account':'Account Data','middle.risk':'Risk Params',
  // Dashboard
  'dash.equity':'Equity Curve','dash.orders':'Recent Orders','dash.risk':'Risk Monitor',
  // Trading
  'trade.place_order':'Place Order','trade.symbol':'Symbol','trade.side':'Side','trade.type':'Type',
  'trade.price':'Price','trade.qty':'Quantity','trade.submit':'Submit Order',
  'trade.orderbook':'Order Book','trade.active_orders':'Active Orders','trade.positions':'Positions','trade.history':'History','trade.chart':'Chart',
  'trade.available':'Available','trade.cancel_all':'Cancel All','trade.buy_btc':'Buy BTC','trade.sell_btc':'Sell BTC','trade.order_history':'Order History','trade.trade_history':'Trade History','toast.order_placed':'Order placed','toast.cancelled':'Cancelled','toast.failed':'Failed',
  'val.select_symbol':'Please select a symbol','val.invalid_qty':'Invalid quantity','val.qty_too_large':'Quantity too large','val.invalid_price':'Invalid price','val.invalid_side':'Invalid trade side','val.no_connection':'Not connected to server',
  // Backtest
  'bt.config':'Backtest Configuration','bt.strategy':'Strategy','bt.symbol':'Symbol',
  'bt.capital':'Initial Capital (USDT)','bt.fee':'Fee Rate','bt.slippage':'Slippage',
  'bt.bars':'Bars','bt.price':'Base Price','bt.params':'Strategy Parameters','bt.run':'Run Backtest',
  'bt.results':'Backtest Results','bt.equity':'Equity Curve',
  // Settings
  'settings.page_title':'System Settings','settings.exchange_title':'Exchange Configuration','settings.default_exchange':'Default Exchange','settings.ai_module_title':'AI Model Configuration',
  'settings.api_key':'API Key','settings.secret_key':'Secret Key','settings.passphrase':'Passphrase',
  'settings.testnet':'Testnet','settings.trade_type':'Trade Type','settings.save':'Save',
  'settings.base_url':'Base URL','settings.model':'Model',
  'settings.test_conn':'Test Connection','settings.connected':'Connected','settings.not_connected':'Not Connected',
  'settings.configured':'Configured','settings.not_configured':'Not Configured','settings.testing':'Testing...',
  'settings.conn_ok':'Connection OK','settings.conn_fail':'Connection Failed','settings.saving':'Saving...',
  'settings.ai_local':'Local Model','settings.default_provider':'Default Provider','settings.auto_select':'Auto-select (first configured)',
  'settings.agent_title':'Agent',
  'settings.copy_mcp':'Copy Config','settings.mcp_copied':'MCP config copied to clipboard',
  'settings.agent_tools_title':'Connect AI Coding Tools',
  'settings.agent_cursor_desc':'Save the config below as <code>.cursor/mcp.json</code> in your project root. Restart Cursor to operate your trading system from the AI panel.',
  'settings.agent_claude_desc':'Save the config below as <code>.claude/mcp.json</code> or import via <code>claude mcp add</code>. Restart Claude Code to take effect.',
  'settings.agent_codex_desc':'Add the config below to your Codex MCP settings. Go to Codex Settings → MCP Servers → Add custom server.',
  'settings.agent_mcp_desc':'Generic MCP config for any MCP-compatible AI client. Uses local MCP server via <code>python -m xtquant.agent.mcp_server</code>.',
  'settings.download_config':'Download Config','settings.config_downloaded':'Config file downloaded',
  'settings.agent_tokens_title':'Agent Token Management','settings.agent_tokens_desc':'Issue Agent Tokens to let AI coding tools operate your trading system via MCP. Tokens are shown only once upon creation.',
  'settings.agent_create_token':'Create Agent Token','settings.agent_token_name':'Token Name','settings.agent_scopes':'Scopes',
  'settings.agent_rate_limit':'Rate Limit (req/min)','settings.agent_expires':'Expires','settings.agent_never':'Never',
  'settings.agent_paper_only':'Paper Trading Only','settings.agent_paper_hint':'Server must also have AGENT_LIVE_TRADING_ENABLED=true for live trading',
  'settings.agent_create_btn':'Create','settings.agent_revoke':'Revoke','settings.agent_revoke_confirm':'Revoke this token? It will be invalidated immediately.',
  'settings.agent_token_created':'Token created! Copy it now — it will not be shown again.','settings.agent_copy_token':'Copy Token','settings.agent_token_copied':'Token copied to clipboard',
  'settings.agent_no_tokens':'No tokens yet. Click the button above to create one.',
  'settings.agent_ai_title':'Agent AI Model','settings.agent_ai_desc':'Configure an AI model for the Agent to enable reasoning capabilities. Users in China can use domestic models like DeepSeek or Qwen, or configure a proxy for foreign models.',
  'settings.agent_ai_card_title':'Agent Model','settings.agent_ai_provider':'AI Provider','settings.agent_ai_select':'Select model...',
  'settings.agent_ai_api_key':'API Key','settings.agent_ai_base_url':'Base URL','settings.agent_ai_model':'Model Name',
  'settings.agent_ai_proxy':'Proxy','settings.agent_ai_proxy_hint':'Configure proxy for foreign AI APIs',
  'settings.agent_ai_http_proxy':'HTTP Proxy','settings.agent_ai_https_proxy':'HTTPS Proxy',
  // Wizard steps
  'settings.ag_step1':'1. Create Token','settings.ag_step2':'2. Select Tool','settings.ag_step3':'3. AI Model','settings.ag_step4':'4. Done',
  'settings.ag_create_your_token':'Create your Agent Token',
  'settings.ag_scope_read':'Read Only','settings.ag_scope_read_desc':'Market + Positions','settings.ag_scope_backtest':'Backtest','settings.ag_scope_backtest_desc':'Market + Backtest',
  'settings.ag_scope_trade':'Paper Trade','settings.ag_scope_trade_desc':'Market + Backtest + Trade','settings.ag_scope_full':'Full Access','settings.ag_scope_full_desc':'All tools (incl. strategy mgmt)',
  'settings.ag_select_tool':'Select your AI Coding Tool',
  'settings.ag_cursor_instructions':'Save config as .cursor/mcp.json in your project root. Restart Cursor to activate.',
  'settings.ag_claude_instructions':'Save as .claude/mcp.json or import via claude mcp add. Restart Claude Code.',
  'settings.ag_codex_instructions':'Add to Codex MCP Settings → MCP Servers → Add custom server.',
  'settings.ag_mcp_instructions':'Generic MCP config for any MCP-compatible client. Command: python -m xtquant.agent.mcp_server',
  'settings.ag_ready':'Configuration Ready!','settings.ag_one_cmd_desc':'Copy the command below to your terminal to complete setup in one step.',
  'settings.ag_terminal_cmd':'Terminal Command','settings.copy_cmd':'Copy Command','settings.cmd_copied':'Command copied! Paste it in your terminal.',
  'settings.ag_view_json':'View JSON config',
  'settings.ag_config_preview':'MCP Config Preview','settings.ag_cli_alternative':'Or quickly configure via CLI (Claude Code):',
  'settings.ag_prev':'Previous','settings.ag_next':'Next','settings.ag_skip':'Skip','settings.ag_done':'Done','settings.ag_next_create':'Create &amp; Continue',
  'settings.ag_need_token':'Please create a token first','settings.ag_setup_complete':'Setup complete! Add the config to your AI tool to start.',
  'settings.agent_token_created_short':'Token Created',
  'common.cancel':'Cancel',
  'filter.type_all':'All Types','filter.level_all':'All Levels',
  'common.selected_text':'Selected','common.items':'items','common.selected':'selected','common.all':'All','common.search':'Search...',
  'log.col_time':'Time','log.col_level':'Level','log.col_msg':'Message',
  'bt.running':'Running...','bt.running_status':'Running backtest...','bt.last_run':'Last run','bt.trades':'trades',
  'nav.ai_gen':'Agent Generate','nav.native':'Python Native','ai.form_title':'Agent Strategy Generator','ai.interval':'Interval','ai.risk':'Risk Preference',
  'ai.risk_low':'Low Risk','ai.risk_med':'Medium Risk','ai.risk_high':'High Risk',
  'ai.prompt':'Strategy Description','ai.generate':'Generate Strategy','ai.code':'Generated Code',
  'ai.code_placeholder':'Click "Generate" to create an Agent strategy...','ai.results':'Backtest Results',
  'ai.deploy':'Deploy as Template','ai.regenerate':'Regenerate',
  'ai.generating':'Generating...','ai.copy_code':'Copy','ai.download_code':'Download',
  'ai.view_code':'View Code','ai.run_backtest':'Run Backtest',
  'ai.template_label':'Preset Template','ai.template_trend':'Trend Following','ai.template_oscillation':'Oscillation Arbitrage',
  'ai.template_breakout':'Breakout','ai.template_custom':'-- Custom Input --',
  'ai.mode_quick':'Quick Generate','ai.mode_advanced':'Advanced','ai.mode_optimize':'Optimize','ai.mode_combo':'Combo',
  'ai.win_rate':'Win Rate','ai.max_dd':'Max Drawdown','ai.annual_ret':'Annual Return',
  'ai.inspiration':'Strategy Inspiration','ai.inspiration_hint':'Click tags above to quickly fill in strategy description',
  'ai.prompt_placeholder':'[Entry Conditions]:\n[Exit Conditions]:\n[Stop Loss / Take Profit]:\n[Constraints] (max position, trading hours):',
  'native.title':'Python Native Strategy','native.indicator_mode':'Indicator Strategy','native.script_mode':'Script Strategy',
  'native.entry_indicator':'Entry Indicator','native.exit_indicator':'Exit Indicator',
  'native.run_backtest':'Run Backtest','native.script_code':'Strategy Code',
  'native.load_indicator_tpl':'Load Indicator Tpl','native.load_empty_tpl':'Clear',
  'nav.ai_generate':'Agent Strategy',
  'agent.new':'New','agent.save':'Save','agent.delete':'Delete','agent.ai_chat':'AI Chat',
  'agent.code_title':'Strategy Code','agent.copy':'Copy','agent.fix':'Fix','agent.backtest':'Backtest',
  'agent.config':'Config','agent.results':'Results','agent.history':'History','agent.saved':'Saved',
  'agent.capital':'Capital','agent.fee':'Fee%','agent.slip':'Slip%','agent.dir':'Direction',
  'agent.both':'Both','agent.long':'Long','agent.short':'Short','agent.run':'Run',
  'agent.ai_smart':'AI Smart','agent.grid':'Grid','agent.trend':'Trend',
  'agent.arbitrage':'Arbitrage','agent.dca':'DCA','agent.martingale':'Martingale',
  'agent.modified':'Modified','agent.no_code':'No code','agent.no_history':'No history',
    'agent.code_title':'Strategy Code','agent.modified':'Modified','agent.new':'New','agent.save':'Save','agent.delete':'Delete','agent.run':'Run',
  'agent.ai_gen':'AI Generate','agent.gen_code':'Generate Code','agent.ai_placeholder':'Describe your strategy in natural language...',
  'agent.backtest':'Backtest','agent.return':'Return','agent.max_dd':'Max DD','agent.sharpe':'Sharpe','agent.trades':'Trades',
  'agent.clear':'Clear','agent.code_rail':'Code',
'native.code_placeholder':'Select indicators or write script, then click "Run Backtest" to generate...',
}};
function T(key){return (S.i18n[S.lang]&&S.i18n[S.lang][key])||key}

// ── Data cache for text-only language re-render ──
S.cache={spotRows:[],ctRows:[],tplRows:[],logRows:[],dashStatus:null,dashOrders:[]};

function syncLangDOM(){
  document.querySelectorAll('[data-i18n]').forEach(function(el){
    var key=el.getAttribute('data-i18n');
    if(key)el.textContent=T(key);
  });
  document.querySelectorAll('[data-i18n-placeholder]').forEach(function(el){
    var key=el.getAttribute('data-i18n-placeholder');
    if(key)el.placeholder=T(key);
  });
  var navLabels={dash:['nav.dash'],trade:['nav.trade'],ai_oppty:['nav.ai_oppty'],ai_generate:['nav.ai_generate'],backtest:['nav.backtest'],strategy:['nav.strategy'],settings:['nav.settings']};
  document.querySelectorAll(".nav a").forEach(function(a){
    var n=navLabels[a.dataset.page];
    if(n)a.textContent=T(n[0]);
  });
  $('lang-toggle').textContent=S.lang==='zh'?'EN':'中';
  renderCurrentPageText();
}

function toggleLang(){
  S.lang=S.lang==='zh'?'en':'zh';
  syncLangDOM();
}

function renderCurrentPageText(){
  var p=S.page;
  if(p==='strategy'){
    updateSidebarLabels();
    if(S.subPage==='spot'&&S.cache.spotRows.length)renderSpotList(S.cache.spotRows);
    else if(S.subPage==='contract'&&S.cache.ctRows.length)renderContractList(S.cache.ctRows);
    else if(S.subPage==='templates'&&S.cache.tplRows.length)renderTplList(S.cache.tplRows);
    else if(S.subPage==='logs'&&S.cache.logRows.length)renderLogList(S.cache.logRows);
  }
  if(p==='dash'){
    updateDashLabels();
    if(S.cache.dashOrders.length)renderDashOrders(S.cache.dashOrders);
  }
  if(p==='trade')updateTradeLabels();
  if(p==='backtest')updateBacktestLabels();
  if(p==='settings')updateSettingsLabels();
}

function updateDashLabels(){
  var labels={'d-equity-lbl':'kpi.equity','d-balance-lbl':'kpi.balance','d-return-lbl':'kpi.return','d-dd-lbl':'kpi.max_dd','d-sharpe-lbl':'kpi.sharpe','d-winrate-lbl':'kpi.winrate'};
  for(var id in labels){var el=$(id);if(el)el.textContent=T(labels[id])}
}

function renderDashOrders(orders){
  var h='';
  for(var i=0;i<Math.min(orders.length,8);i++){
    var o=orders[i];
    h+='<tr><td style="font-size:8px">'+(o.order_id||'').substring(0,10)+'</td><td>'+o.symbol+'</td><td class="'+(o.side==='BUY'?'g':'r')+'">'+o.side+'</td><td>'+FUSD(o.price)+'</td><td>'+o.status+'</td></tr>';
  }
  $('d-orders').innerHTML=h||'<tr><td colspan="5" class="m" style="text-align:center;padding:16px">'+T('label.no_orders')+'</td></tr>';
}

function updateTradeLabels(){
  // Static labels handled by data-i18n walk; update dynamic text
  var noOrdText=T('label.no_active_orders');
  var tbody=$('t-orders');
  if(tbody&&tbody.children.length===1&&tbody.children[0].children.length===1){
    tbody.children[0].children[0].textContent=noOrdText;
  }
}
function updateBacktestLabels(){
  // Static labels handled by data-i18n; re-render dynamic params
  updateBtParams();
  // Update "Last run" text
  var lr=$('bt-last-result');
  if(lr&&lr.textContent)lr.innerHTML='<span class="g">'+T('bt.last_run')+': '+now()+'</span>';
}
function updateSettingsLabels(){
  // Static labels handled by data-i18n walk
  var msg=$('s-msg');
  if(msg&&msg.textContent==='Saving...')msg.textContent=T('settings.saving');
}

// ═══════════════════════════════════════════════ NAVIGATION ═══════════════════════════════════════════
  document.querySelectorAll(".nav a").forEach(function(a){
    a.addEventListener("click",function(){
      document.querySelectorAll(".nav a").forEach(function(x){x.classList.remove("active")});
      this.classList.add("active");
      S.page=this.dataset.page;
      document.querySelectorAll(".page").forEach(function(p){p.classList.remove("active")});
      document.getElementById("pg-"+S.page).classList.add("active");
      var gear=document.getElementById("settings-icon-btn");if(gear){gear.style.color=S.page==="settings"?"#fff":"var(--muted)";gear.style.borderColor=S.page==="settings"?"var(--blue)":"var(--border)";}
      if(S.page==="dash"){initDashChart();refreshDash()}
      if(S.page==="trade"){initTradeChart();refreshOrders();subscribeTradeWS()}
      if(S.page==="ai_oppty"){initAIOpportunity()}
      if(S.page==="backtest"){initBtChart();updateBtParams()}
      if(S.page==="strategy"){refreshStrategies()}
      if(S.page==="settings"){loadSettings()}
    });
  });

// ═══════════════════════════════════════════════ UTILS ═══════════════════════════════════════════════
function $(id){return document.getElementById(id)}
function navToPage(page){
  document.querySelectorAll('.nav a').forEach(function(a){a.classList.remove('active')});
  var target=document.querySelector('.nav a[data-page="'+page+'"]');
  if(target)target.classList.add('active');
  S.page=page;
  document.querySelectorAll('.page').forEach(function(p){p.classList.remove('active')});
  var pg=document.getElementById('pg-'+page);
  if(pg)pg.classList.add('active');
  syncLangDOM();
  var gear=document.getElementById('settings-icon-btn');
  if(gear){gear.style.color=page==='settings'?'#fff':'var(--muted)';gear.style.borderColor=page==='settings'?'var(--blue)':'var(--border)';}
  if(page==='dash'){initDashChart();refreshDash();subscribeDashWS()}
  else{unsubscribeDashWS()}
  if(page==='trade'){initTradeChart();refreshOrders();refreshTradePositionsTrades();subscribeTradeWS()}
  else{stopTradeRefresh();unsubscribeTradeWS()}
  if(page==='ai_oppty'){initAIOpportunity()}
  if(page==='ai_generate'){initAI2Chart();initMonacoEditor()}
  if(page==='backtest'){initBtChart();updateBtParams()}
  if(page==='strategy'){refreshStrategies()}
  if(page==='settings'){loadSettings()}
}
function resetUIPositions(){
  var ball=document.getElementById('agent-ball');
  if(ball){
    ball.style.left='24px'; ball.style.bottom='24px';
    ball.style.right='auto'; ball.style.top='auto';
  }
  var panel=document.getElementById('agent-panel');
  if(panel){
    panel.style.left=''; panel.style.top='';
    panel.style.right='24px'; panel.style.bottom='92px';
    panel._dragged=false;
  }
  showToast('位置已重置');
}
function saveUISettings(){
  var s={};
  var el;
  el=document.getElementById('ui-font-size');if(el)s.fontSize=el.value;
  el=document.getElementById('ui-code-font-size');if(el)s.codeFontSize=el.value;
  el=document.getElementById('ui-kline-up');if(el)s.klineUp=el.value;
  el=document.getElementById('ui-kline-down');if(el)s.klineDown=el.value;
  el=document.getElementById('ui-vol-opacity');if(el)s.volOpacity=el.value;
  el=document.getElementById('ui-ball-size');if(el)s.ballSize=el.value;
  el=document.getElementById('ui-reduce-motion-toggle');if(el)s.reduceMotion=el.classList.contains('active');
  el=document.getElementById('ui-refresh-interval');if(el)s.refreshInterval=el.value;
  localStorage.setItem('xt_ui_settings',JSON.stringify(s));
  var msg=document.getElementById('ui-settings-msg');if(msg){msg.textContent='设置已保存';msg.style.color='var(--green)'};
}
function resetUISettings(){
  localStorage.removeItem('xt_ui_settings');
  var ids=['ui-font-size','ui-code-font-size','ui-kline-up','ui-kline-down','ui-vol-opacity','ui-ball-size'];
  var defaults=['13','12','#3fb950','#f85149','40','52'];
  for(var i=0;i<ids.length;i++){var el=document.getElementById(ids[i]);if(el)el.value=defaults[i];}
  var t1=document.getElementById('ui-reduce-motion-toggle');if(t1&&t1.classList.contains('active'))t1.classList.remove('active');
  var t2=document.getElementById('ui-show-ball-toggle');if(t2&&!t2.classList.contains('active'))t2.classList.add('active');
  document.body.style.fontSize='13px';
  var msg=document.getElementById('ui-settings-msg');if(msg){msg.textContent='已恢复默认';msg.style.color='var(--yel)'};
}
function loadUISettings(){
  try{
    var s=JSON.parse(localStorage.getItem('xt_ui_settings'));
    if(!s)return;
    if(s.fontSize)document.body.style.fontSize=s.fontSize+'px';
    if(s.codeFontSize)document.querySelectorAll('pre,code,textarea').forEach(function(el){el.style.fontSize=s.codeFontSize+'px'});
    if(s.ballSize){var b=document.getElementById('agent-ball');if(b){b.style.width=s.ballSize+'px';b.style.height=s.ballSize+'px'};}
    if(s.klineUp)document.documentElement.style.setProperty('--green',s.klineUp);
    if(s.klineDown)document.documentElement.style.setProperty('--red',s.klineDown);
    if(s.reduceMotion)document.body.style.setProperty('--transition-speed','0s');
  }catch(e){}
}
function F(n,d){if(n==null)return'--';d=d||2;return Number(n).toFixed(d)}
function FUSD(n){if(n==null)return'--';return Number(n).toLocaleString(undefined,{minimumFractionDigits:2,maximumFractionDigits:2})}
function FP(n){if(n==null)return'--';return (n>=0?'+':'')+F(n,2)+'%'}
function FC(n,c){return'<span class="'+c+'">'+n+'</span>'}
function now(){return new Date().toLocaleString()}

function toast(m,t){t=t||'ok';var el=$('toast');el.textContent=m;el.className='toast show '+t;setTimeout(function(){el.classList.remove('show')},3500)}

setInterval(function(){$('hdr-time').textContent=now()},1000);

// ═══════════════════════════════════════════════ WEBSOCKET ═══════════════════════════════════════════
S._wsReconnectDelay=1000;
S._wsReconnectMax=30000;

function wsConnect(){
  var p=location.protocol==='https:'?'wss:':'ws:';
  S.ws=new WebSocket(p+'//'+location.host+'/ws');
  S.ws.onopen=function(){
    S.wsOk=true;$('ws-dot').className='dot g';$('ws-text').textContent=T('label.connected');
    S._wsReconnectDelay=1000;
    // Re-subscribe all active subscriptions after reconnect
    resubscribeAll();
  };
  S.ws.onclose=function(){
    S.wsOk=false;$('ws-dot').className='dot r';$('ws-text').textContent=T('label.offline');
    var delay=S._wsReconnectDelay;
    S._wsReconnectDelay=Math.min(delay*2,S._wsReconnectMax);
    setTimeout(wsConnect,delay);
  };
  S.ws.onerror=function(){
    S.wsOk=false;$('ws-dot').className='dot r';$('ws-text').textContent=T('label.offline');
  };
  S.ws.onmessage=function(e){
    var d=JSON.parse(e.data);
    if(d.type==='orderbook' && d.data && d.data.bids) console.log('[WS] recv orderbook, bids='+d.data.bids.length);
    if(d.type==='trades' && d.data) console.log('[WS] recv trades, count='+d.data.length);
    if(d.type==='status') updateFromStatus(d.data);
    if(d.type==='price') updatePrice(d.symbol,d.data);
    if(d.type==='orderbook') updateBook(d.data);
    if(d.type==='trades') renderRecentTrades(d.data);
  };
}

// ── WebSocket Subscription Ref Counting ──
S._wsSubs = {};
function wsSubscribe(channel, symbol) {
  var key = channel + ':' + symbol;
  if (S._wsSubs[key]) { S._wsSubs[key]++; return; }
  S._wsSubs[key] = 1;
  if (S.ws && S.ws.readyState === WebSocket.OPEN) {
    S.ws.send(JSON.stringify({type: 'subscribe', channel: channel, symbol: symbol}));
  }
}
function wsUnsubscribe(channel, symbol) {
  var key = channel + ':' + symbol;
  if (!S._wsSubs[key]) return;
  S._wsSubs[key]--;
  if (S._wsSubs[key] <= 0) {
    delete S._wsSubs[key];
    if (S.ws && S.ws.readyState === WebSocket.OPEN) {
      S.ws.send(JSON.stringify({type: 'unsubscribe', channel: channel, symbol: symbol}));
    }
  }
}
function resubscribeAll() {
  var keys = Object.keys(S._wsSubs);
  for (var i = 0; i < keys.length; i++) {
    var parts = keys[i].split(':');
    if (parts.length === 2 && S.ws && S.ws.readyState === WebSocket.OPEN) {
      S.ws.send(JSON.stringify({type: 'subscribe', channel: parts[0], symbol: parts[1]}));
    }
  }
}

// ═══════════════════════════════════════════════ BINANCE DIRECT WS ═══════════════════════════════════
// Bypasses Python backend for market data — connects directly to Binance public streams
// ── Binance Direct WebSocket (dynamic subscriptions) ──
var BinanceWs = {
  _ws: null,
  _symbol: null,
  _reconnectTimer: null,
  _reconnectDelay: 2000,
  _maxTrades: 30,
  _tradeBuf: [],
  _handlers: {},   // streamName → [{fn, id}]
  _nextId: 1,
  _pending: [],    // streams waiting for onopen

  connect: function(symbol) {
    if (!symbol) return;
    if (this._symbol === symbol && this._ws && this._ws.readyState === WebSocket.OPEN) return;
    // Preserve kline handlers across symbol changes
    var savedKline = {};
    var ks = Object.keys(this._handlers);
    for (var i = 0; i < ks.length; i++) {
      if (ks[i].indexOf('@kline_') > -1) {
        savedKline[ks[i]] = this._handlers[ks[i]];
      }
    }
    this.disconnect();
    this._handlers = savedKline;
    this._symbol = symbol;
    this._tradeBuf = [];
    var sym = symbol.toLowerCase();
    // Subscribe depth + trades for the current symbol
    this._pending = [sym + '@depth20@100ms', sym + '@trade'];
    var url = 'wss://stream.binance.com:9443/stream?streams=' + sym + '@depth20@100ms/' + sym + '@trade';
    this._ws = new WebSocket(url);
    var self = this;
    this._ws.onopen = function() {
      console.log('[Binance] Connected — ' + symbol);
      self._reconnectDelay = 2000;
      // Subscribe kline streams (depth+trade already in combined URL)
      var ks = Object.keys(self._handlers);
      if (ks.length) {
        self._ws.send(JSON.stringify({method: 'SUBSCRIBE', params: ks, id: self._nextId++}));
      }
    };
    this._ws.onmessage = function(e) {
      try {
        var msg = JSON.parse(e.data);
        // Handle subscription confirmations
        if (msg.result !== undefined) return;
        if (msg.id) return;
        var s = msg.stream;
        if (!s) return;
        // Kline streams
        if (s.indexOf('@kline_') > -1) {
          self._onKline(msg.data);
          return;
        }
        // Depth
        if (s.indexOf('@depth') > -1 && msg.data) {
          self._onDepth(msg.data);
          return;
        }
        // Trade
        if (s.indexOf('@trade') > -1 && msg.data) {
          self._onTrade(msg.data);
          return;
        }
      } catch(err) { console.log('[Binance] parse err:', err); }
    };
    this._ws.onclose = function(e) {
      console.log('[Binance] Closed — code=' + e.code);
      if (self._symbol) {
        self._reconnectTimer = setTimeout(function() { self.connect(self._symbol); }, self._reconnectDelay);
        self._reconnectDelay = Math.min(self._reconnectDelay * 2, 30000);
      }
    };
    this._ws.onerror = function() { console.log('[Binance] Error — check connectivity'); };
  },

  disconnect: function() {
    if (this._reconnectTimer) { clearTimeout(this._reconnectTimer); this._reconnectTimer = null; }
    if (this._ws) {
      var streams = Object.keys(this._handlers);
      if (streams.length) {
        try { this._ws.send(JSON.stringify({method: 'UNSUBSCRIBE', params: streams, id: this._nextId++})); } catch(e) {}
      }
      this._ws.onclose = null;
      this._ws.onmessage = null;
      this._ws.close();
      this._ws = null;
    }
    this._symbol = null;
    this._tradeBuf = [];
    this._handlers = {};
    this._pending = [];
  },

  switchSymbol: function(symbol) {
    if (this._symbol === symbol) return;
    // Re-subscribe depth+trade for new symbol; kline handlers persist (Datafeed manages)
    var ks = Object.keys(this._handlers).filter(function(k) { return k.indexOf('@kline_') > -1; });
    this._handlers = {};
    var self = this;
    ks.forEach(function(k) { self._handlers[k] = []; }); // rebuild below
    this.connect(symbol);
    // After reconnect, kline handlers need re-registration — but we lose them here.
    // Instead, just switch the base symbol and resubscribe depth+trade.
    // We'll re-subscribe everything in onopen.
  },

  // ── Stream management ──
  _subscribe: function(stream, handler) {
    if (!this._handlers[stream]) {
      this._handlers[stream] = [];
      if (this._ws && this._ws.readyState === WebSocket.OPEN) {
        this._ws.send(JSON.stringify({method: 'SUBSCRIBE', params: [stream], id: this._nextId++}));
      }
    }
    this._handlers[stream].push(handler);
  },

  _unsubscribe: function(stream, handler) {
    var arr = this._handlers[stream];
    if (!arr) return;
    var idx = arr.indexOf(handler);
    if (idx >= 0) arr.splice(idx, 1);
    if (arr.length === 0) {
      delete this._handlers[stream];
      if (this._ws && this._ws.readyState === WebSocket.OPEN) {
        this._ws.send(JSON.stringify({method: 'UNSUBSCRIBE', params: [stream], id: this._nextId++}));
      }
    }
  },

  // ── Kline subscribe/unsubscribe for Datafeed ──
  subscribeKlines: function(symbol, interval, callback) {
    var stream = symbol.toLowerCase() + '@kline_' + interval;
    console.log('[BinanceWs] subscribeKlines: ' + stream + ' wsReady=' + (this._ws ? this._ws.readyState : 'null'));
    this._subscribe(stream, callback);
  },

  unsubscribeKlines: function(symbol, interval, callback) {
    var stream = symbol.toLowerCase() + '@kline_' + interval;
    this._unsubscribe(stream, callback);
  },

  // ── Depth handler ──
  _onDepth: function(data) {
    var bids = [], asks = [], limit = 12;
    for (var i = 0; i < Math.min(data.bids.length, limit); i++) {
      bids.push([parseFloat(data.bids[i][0]), parseFloat(data.bids[i][1])]);
    }
    for (var i = 0; i < Math.min(data.asks.length, limit); i++) {
      asks.push([parseFloat(data.asks[i][0]), parseFloat(data.asks[i][1])]);
    }
    updateBook({ bids: bids, asks: asks });
    if (bids.length && asks.length) {
      var mid = (bids[0][0] + asks[0][0]) / 2;
      var sym = S._tradeSym || 'BTCUSDT';
      updatePrice(sym, { price: mid, change_pct: 0, high: 0, low: 0, volume: 0 });
    }
  },

  // ── Trade handler ──
  _onTrade: function(data) {
    var trade = {
      price: parseFloat(data.p),
      qty: parseFloat(data.q),
      time: Math.floor(data.T / 1000),
      side: data.m ? 'SELL' : 'BUY'
    };
    this._tradeBuf.unshift(trade);
    if (this._tradeBuf.length > this._maxTrades) this._tradeBuf.length = this._maxTrades;
    if (S.page === 'trade') renderRecentTrades(this._tradeBuf);
  },

  // ── Kline handler ──
  _onKline: function(data) {
    // Binance kline: {e:"kline", k:{t,o,h,l,c,v,x,...}}
    var k = data.k;
    var bar = {
      timestamp: k.t,
      open: parseFloat(k.o),
      high: parseFloat(k.h),
      low: parseFloat(k.l),
      close: parseFloat(k.c),
      volume: parseFloat(k.v)
    };
    var stream = data.s.toLowerCase() + '@kline_' + k.i;
    var handlers = this._handlers[stream];
    console.log('[BinanceWs] _onKline stream=' + stream + ' handlers=' + (handlers ? handlers.length : 0) + ' close=' + bar.close + ' final=' + k.x);
    if (handlers) {
      for (var i = 0; i < handlers.length; i++) {
        try { handlers[i](bar); } catch(e) { console.log('[BinanceWs] handler error:', e); }
      }
    }
  },

  getTrades: function() {
    return this._tradeBuf;
  }
};

// ── Quick-fill price from orderbook ──
function fillBestBid() {
  if (S.bids && S.bids.length) {
    $('tf-price').value = S.bids[0][0];
    updateOrderPreview();
  }
}
function fillBestAsk() {
  if (S.asks && S.asks.length) {
    $('tf-price').value = S.asks[0][0];
    updateOrderPreview();
  }
}

function updateFromStatus(d){
  $('hdr-equity').textContent=FUSD(d.portfolio?.total_equity);
  var r=d.risk||{};
  $('d-equity').textContent=FUSD(d.portfolio?.total_equity);
  $('d-balance').textContent=FUSD(d.portfolio?.available_balance);
  $('d-dd').textContent=(r.current_drawdown_pct||'0%');
  $('d-sharpe').textContent='--';
  $('d-winrate').textContent='--';
  updateRiskPanel(r);
  // Update equity curve chart from real data
  if (d.equity_curve && d.equity_curve.length > 0) {
    if (!S.dashChart) initDashChart();
    var eqData = [];
    for (var i = 0; i < d.equity_curve.length; i++) {
      var p = d.equity_curve[i];
      eqData.push({time: p.time || (Date.now()/1000 - (d.equity_curve.length-i)*60), value: p.equity});
    }
    S.dashSeries.setData(eqData);
    S.dashChart.timeScale().fitContent();
  }
  if(d.strategies) updateStList(d.strategies);
}

function updatePrice(sym,data){
  S.prices[sym]=data;
  if(S.page==='trade'&&sym===$('t-chart-sym').textContent){
    $('ob-sym').textContent=sym;
    // Ticker strip
    var price=data.last||data.price;
    var chg=data.change_pct||0;
    var priceEl=$('ticker-price');
    priceEl.textContent=FUSD(price);
    priceEl.style.color=chg>=0?'var(--green)':'var(--red)';
    var chgEl=$('ticker-change');
    chgEl.textContent=(chg>=0?'+':'')+F(chg,2)+'%';
    chgEl.style.color=chg>=0?'var(--green)':'var(--red)';
    chgEl.style.background=chg>=0?'rgba(63,185,80,.12)':'rgba(248,81,73,.12)';
    $('ticker-high').textContent=FUSD(data.high||price);
    $('ticker-low').textContent=FUSD(data.low||price);
    $('ticker-vol').textContent=F(data.volume||0,1);
  }
}

function updateBook(data){
  S.bids=data.bids||[];S.asks=data.asks||[];
  if(S.page!=='trade'){console.log('[OB] skip — not on trade page, page='+S.page);return;}
  if(!data.bids||!data.bids.length){console.log('[OB] skip — no bid data');return;}
  console.log('[OB] rendering '+S.bids.length+' bids, '+S.asks.length+' asks');
  // Calculate max for bar widths
  var mx=0;
  for(var i=0;i<S.asks.length;i++)mx=Math.max(mx,S.asks[i][1]);
  for(var i=0;i<S.bids.length;i++)mx=Math.max(mx,S.bids[i][1]);
  // Ask rows
  var aHtml='',cumA=0;
  for(var i=Math.min(S.asks.length,12)-1;i>=0;i--){
    var a=S.asks[i];cumA+=a[1];
    var pct=Math.round(a[1]/mx*100)||1;
    aHtml+='<div class="ob-row ask" onclick="setPriceFromBook('+a[0]+')"><div class="ob-bar" style="width:'+pct+'%"></div><span class="ob-price">'+FUSD(a[0])+'</span><span class="ob-amount">'+F(a[1],4)+'</span><span class="ob-cumulative">'+F(cumA,1)+'</span></div>';
  }
  $('ob-asks').innerHTML=aHtml;
  // Current price
  if(S.asks.length&&S.bids.length){
    var mid=S.bids[0][0];
    var sp=S.asks[0][0]-S.bids[0][0];
    $('ob-price-val').textContent=FUSD(mid);
    $('ob-price-val').style.color=S.bids[0][0]>=S.asks[0][0]?'var(--green)':'var(--red)';
    $('ob-spread-val').textContent='价差 '+FUSD(sp)+' ('+F(sp/S.bids[0][0]*100,3)+'%)';
  }
  // Bid rows
  var bHtml='',cumB=0;
  for(var i=0;i<Math.min(S.bids.length,12);i++){
    var b=S.bids[i];cumB+=b[1];
    var pct=Math.round(b[1]/mx*100)||1;
    bHtml+='<div class="ob-row bid" onclick="setPriceFromBook('+b[0]+')"><div class="ob-bar" style="width:'+pct+'%"></div><span class="ob-price">'+FUSD(b[0])+'</span><span class="ob-amount">'+F(b[1],4)+'</span><span class="ob-cumulative">'+F(cumB,1)+'</span></div>';
  }
  $('ob-bids').innerHTML=bHtml;
}

function updateRiskPanel(r){
  var h='';
  var items=[
    ['Circuit Breaker',r.circuit_open?'<span class="r">TRIPPED</span>':'<span class="g">Normal</span>'],
    ['Daily Orders',r.daily_orders||0],
    ['Consecutive Losses',r.consecutive_losses||0],
    ['Drawdown',r.current_drawdown_pct||'0%'],
    ['Margin Ratio',r.margin_ratio||'N/A'],
    ['Price Chg (1m)',r.price_change_1min_pct||'0%'],
    ['Volatility',r.volatility||'N/A'],
    ['Blacklist',(r.blacklist||[]).join(', ')||'empty'],
  ];
  for(var i=0;i<items.length;i++){
    h+='<div style="display:flex;justify-content:space-between;padding:4px 0;border-bottom:1px solid rgba(48,54,61,.3)"><span>'+items[i][0]+'</span><b>'+items[i][1]+'</b></div>';
  }
  $('d-risk').innerHTML=h;
}

// ═══════════════════════════════════════════════ DASHBOARD CHARTS ═══════════════════════════════════
// ======== DASHBOARD (ECharts) ========
var S_dashData=null,S_dashCalIdx=0;
function initDashCharts(){
  var eqEl=document.getElementById('dash-equity-echart');if(eqEl&&!S.dashEquityChart)S.dashEquityChart=echarts.init(eqEl);
  var pieEl=document.getElementById('dash-pie-echart');if(pieEl&&!S.dashPieChart)S.dashPieChart=echarts.init(pieEl);
  var ddEl=document.getElementById('dash-dd-echart');if(ddEl&&!S.dashDDChart)S.dashDDChart=echarts.init(ddEl);
  var hrEl=document.getElementById('dash-hourly-echart');if(hrEl&&!S.dashHourlyChart)S.dashHourlyChart=echarts.init(hrEl);
}
function refreshDash(){fetch('/api/dashboard/summary').then(function(r){return r.json()}).then(function(d){
  if(d.status!=='ok'||!d.data)return;S_dashData=d.data;initDashCharts();
  DKPI(d.data);DEq(d.data);DPie(d.data.strategy_pnl_pie||[]);DDD(d.data);DHour(d.data.hourly_distribution||[]);
  DCal(d.data.calendar_months||[]);DRank(d.data.strategy_stats||[]);
}).catch(function(){});}
function refreshDashSub(){if(S_dashData){var d=S_dashData;initDashCharts();DEq(d);DPie(d.strategy_pnl_pie||[]);DDD(d);DHour(d.hourly_distribution||[]);DCal(d.calendar_months||[]);}}
function DKPI(d){
  var E=function(id){return document.getElementById(id);};
  E('dk-equity').textContent='$'+FX(d.total_equity,0);
  E('dk-pnl').textContent=FXP(d.total_return_pct);
  E('dk-winrate').textContent=FX(d.win_rate_pct,1)+'%';
  var r=E('dk-ring-fg');if(r)r.setAttribute('stroke-dasharray',FX(d.win_rate_pct,1)+',100');
  E('dk-maxdd').textContent=FX(d.max_drawdown_pct,1)+'%';
  E('dk-dd-amt').textContent=FX(d.total_equity*d.max_drawdown_pct/100||0,0);
  E('dk-trades').textContent=d.total_trades||0;
  var run=0;(d.strategy_stats||[]).forEach(function(s){if(s.running)run++;});
  E('dk-running').textContent=run;E('dk-totalstrats').textContent=(d.strategy_stats||[]).length;
  E('dk-pf').textContent='--';E('dk-avgwin').textContent=FXC(d.best_day);
}
function DEq(d){
  if(!S.dashEquityChart)return;var dy=d.daily_pnl||[],dates=[],eqv=[],bars=[];
  var eq=d.total_equity||100000,aeq=[eq];
  for(var i=dy.length-1;i>=0;i--){eq-=dy[i].profit;aeq.unshift(eq);}
  for(var j=0;j<dy.length;j++){dates.push(dy[j].date);eqv.push(aeq[j+1]);bars.push({value:dy[j].profit,itemStyle:{color:dy[j].profit>=0?'#1a7f37':'#cf222e'}});}
  S.dashEquityChart.setOption({tooltip:{trigger:'axis'},legend:{data:['Equity','Daily PnL'],bottom:0},grid:{left:55,right:20,top:10,bottom:28},xAxis:{type:'category',data:dates,axisLabel:{fontSize:9}},yAxis:[{type:'value',axisLabel:{fontSize:9,formatter:function(v){return (v/1000).toFixed(0)+'k'}}},{type:'value',axisLabel:{fontSize:9}}],series:[{name:'Equity',type:'line',data:eqv,lineStyle:{color:'#58a6ff',width:2},areaStyle:{color:'rgba(88,166,255,.15)'},smooth:true},{name:'Daily PnL',type:'bar',yAxisIndex:1,data:bars}]});
}
function DPie(pd){
  if(!S.dashPieChart)return;var colors=['#58a6ff','#3fb950','#f85149','#d29922','#bc8cff','#79c0ff','#ffa657','#7ee787'];
  S.dashPieChart.setOption({tooltip:{trigger:'item'},series:[{type:'pie',radius:['40%','70%'],data:pd.map(function(p,i){return{name:p.name,value:p.value,itemStyle:{color:colors[i%8]}}}),label:{fontSize:9}}]});
}
function DDD(d){
  if(!S.dashDDChart)return;var dy=d.daily_pnl||[],eq=d.total_equity||100000,aeq=[eq],peak=0,dds=[],dts=[];
  for(var i=dy.length-1;i>=0;i--){eq-=dy[i].profit;aeq.unshift(eq);}
  for(var j=0;j<aeq.length;j++){peak=Math.max(peak,aeq[j]);dds.push(peak>0?(peak-aeq[j])/peak*100:0);if(j>0&&j<=dy.length)dts.push(dy[j-1].date);}
  S.dashDDChart.setOption({tooltip:{trigger:'axis'},grid:{left:50,right:10,top:10,bottom:28},xAxis:{type:'category',data:dts,axisLabel:{fontSize:9}},yAxis:{type:'value',axisLabel:{fontSize:9},inverse:true},series:[{type:'area',data:dds,areaStyle:{color:'rgba(207,34,46,.15)'},lineStyle:{color:'#cf222e',width:1.5}}]});
}
function DHour(hs){
  if(!S.dashHourlyChart)return;
  S.dashHourlyChart.setOption({tooltip:{trigger:'axis'},legend:{data:['Trades','PnL'],bottom:0},grid:{left:45,right:10,top:10,bottom:28},xAxis:{type:'category',data:hs.map(function(h){return h.hour+'h'}),axisLabel:{fontSize:8}},yAxis:[{type:'value',axisLabel:{fontSize:8}},{type:'value',axisLabel:{fontSize:8}}],series:[{name:'Trades',type:'bar',data:hs.map(function(h){return h.count}),itemStyle:{color:'#58a6ff'}},{name:'PnL',type:'line',yAxisIndex:1,data:hs.map(function(h){return h.profit}),lineStyle:{color:'#3fb950'}}]});
}
function FXC(n){return n!=null?'$'+FX(Math.abs(n),0):'--'}
function initDashChart(){if(S.dashChart)return;}

// ── Calendar ──
function dashPrevMonth(){if(S_dashCalIdx<(S_dashData.calendar_months||[]).length-1){S_dashCalIdx++;DCal(S_dashData.calendar_months||[]);}}
function dashNextMonth(){if(S_dashCalIdx>0){S_dashCalIdx--;DCal(S_dashData.calendar_months||[]);}}
function DCal(mos){
  var el=$('dash-calendar');if(!el||!mos.length){el.innerHTML='<div style=\"color:var(--muted);text-align:center;padding:30px\">No data</div>';return}
  var m=mos[S_dashCalIdx]||mos[0];
  $('dash-cal-label').textContent=m.year+'-'+String(m.month).padStart(2,'0')+' W'+m.win_days+' L'+m.lose_days+' $'+FX(m.total,0);
  var wk=['M','T','W','T','F','S','S'];
  var h='<div class=\"dash-cal-header\">';for(var i=0;i<7;i++)h+='<span>'+wk[i]+'</span>';h+='</div><div>';
  for(var j=0;j<m.first_weekday;j++)h+='<span class=\"dash-cal-day\"></span>';
  for(var d=1;d<=m.days_in_month;d++){var dk=String(d).padStart(2,'0'),pnl=m.days[dk],cls='zero';if(pnl>0)cls='profit';else if(pnl<0)cls='loss';h+='<span class=\"dash-cal-day '+cls+'\" title=\"'+m.month_key+'-'+dk+': $'+FX(pnl,0)+'\">'+d+'</span>';}
  h+='</div>';el.innerHTML=h;
}

// ── Strategy Ranking ──
function DRank(ss){
  var el=$('dash-ranking');if(!el)return;
  if(!ss||!ss.length){el.innerHTML='<div style=\"color:var(--muted);text-align:center;padding:20px\">No strategies</div>';return}
  ss.sort(function(a,b){return(b.pnl||0)-(a.pnl||0)});var h='';
  for(var i=0;i<Math.min(ss.length,6);i++){var s=ss[i],top=i<3,rc='rank-card'+(top?' rank-top':''),rcls='r'+(i<3?i+1:'n');
    h+='<div class=\"'+rc+'\"><div class=\"rank-badge '+rcls+'\">'+(i+1)+'</div><div class=\"rank-info\"><div class=\"rank-name\">'+s.name+'</div><div class=\"rank-stats\"><span>'+s.trade_count+' trades</span></div></div><div class=\"rank-pnl\" style=\"color:'+(s.pnl>=0?'var(--green)':'var(--red)')+'\">'+FXC(s.pnl)+'</div></div>';}
  el.innerHTML=h;
}

// ═══════════════════════════════════════════════ TRADING ═══════════════════════════════════════════
S.tradeSide='BUY';

function setTradeSide(s){
  S.tradeSide=s;
  $('tf-buy-btn').classList.toggle('active',s==='BUY');
  $('tf-sell-btn').classList.toggle('active',s==='SELL');
  var submit=$('tf-submit-btn');
  submit.classList.toggle('buy',s==='BUY');
  submit.classList.toggle('sell',s==='SELL');
  submit.innerHTML=s==='BUY'?'<span data-i18n="trade.buy_btc">买入 BTC</span>':'<span data-i18n="trade.sell_btc">卖出 BTC</span>';
  updateOrderPreview();
}

function setTradeType(t){
  S.tradeType=t;
  $('tf-type').value=t;
  document.querySelectorAll('[data-tt]').forEach(function(el){el.classList.toggle('active',el.dataset.tt===t)});
  $('tf-price').disabled=(t==='MARKET');
  if(t==='MARKET'){$('tf-price').value='';$('tf-price').placeholder='市价';}
  else{$('tf-price').placeholder='0.00';}
}

function setPriceFromBook(p){
  $('tf-price').value=p;
  updateOrderPreview();
}

// ── Percentage Slider Drag ──
var S_pctDragging=false;

function getPctFromEvent(e){
  var track=$('pct-track');
  if(!track)return 0;
  var rect=track.getBoundingClientRect();
  var x=e.clientX-rect.left;
  var pct=Math.round((x/rect.width)*100);
  return Math.max(0,Math.min(100,pct));
}

function startPctDrag(e){
  e.preventDefault();
  S_pctDragging=true;
  var pct=getPctFromEvent(e);
  setQtyPct(pct);
  document.addEventListener('mousemove',onPctDrag);
  document.addEventListener('mouseup',stopPctDrag);
}

function onPctDrag(e){
  if(!S_pctDragging)return;
  var pct=getPctFromEvent(e);
  setQtyPct(pct);
}

function stopPctDrag(e){
  S_pctDragging=false;
  document.removeEventListener('mousemove',onPctDrag);
  document.removeEventListener('mouseup',stopPctDrag);
}

function setQtyPct(pct){
  // Visual slider update (safe if elements don't exist)
  var fill=$('pct-fill');if(fill)fill.style.width=pct+'%';
  var thumb=$('pct-thumb');if(thumb)thumb.style.left=pct+'%';
  // Use available balance to calculate qty
  var available=100000; // default
  var price=parseFloat($('tf-price').value)||0;
  if(!price){
    var askEl=document.querySelector('#ob-asks .ob-price');
    price=askEl?parseFloat(askEl.textContent):0;
  }
  if(!price)return;
  var qty=(available*pct/100)/price;
  $('tf-qty').value=F(qty,4);
  updateOrderPreview();
}

function updateOrderPreview(){
  var price=parseFloat($('tf-price').value)||0;
  var qty=parseFloat($('tf-qty').value)||0;
  var total=price*qty;
  var fee=total*0.001;
  var preview=$('tf-preview');
  if(total>0){
    preview.style.display='block';
    $('tf-pv-total').textContent=FUSD(total)+' USDT';
    $('tf-pv-fee').textContent=FUSD(fee)+' USDT';
  }else{preview.style.display='none';}
}

function setTradeInterval(interval){
  // KLineChart Pro handles intervals via its built-in period selector
  if(!S.tradeChart)return;
  var map = { '1m':1, '5m':5, '15m':15, '30m':30, '1h':1, '4h':4, '1d':1, '1w':1 };
  var ts = { '1m':'minute', '5m':'minute', '15m':'minute', '30m':'minute', '1h':'hour', '4h':'hour', '1d':'day', '1w':'week' };
  S.tradeChart.setPeriod({ multiplier: map[interval]||1, timespan: ts[interval]||'hour', text: interval });
}

function switchTradeTab(tab){
  document.querySelectorAll('.trade-tab[data-tab]').forEach(function(t){t.classList.toggle('active',t.dataset.tab===tab)});
  ['orders','history','positions','trades'].forEach(function(id){
    $('trade-tab-'+id).style.display=id===tab?'block':'none';
  });
}

function switchOBTab(view){
  document.querySelectorAll('.ob-tab').forEach(function(t){t.classList.toggle('active',t.textContent.includes(view==='book'?'订单簿':'成交'))});
  $('ob-asks').style.display=view==='book'?'':'none';
  $('ob-bids').style.display=view==='book'?'':'none';
  var spread=document.querySelector('.ob-spread-bar');
  if(spread)spread.style.display=view==='book'?'':'none';
  // Hide orderbook header when showing trades
  var obh=$('ob-book-header');if(obh)obh.style.display=view==='book'?'':'none';
  $('ob-trades-view').style.display=view==='trades'?'':'none';
  if(view==='trades')renderRecentTrades(BinanceWs.getTrades());
}

function onTradeSymbolChange(){
  var sym=$('tf-symbol').value;
  // Unsubscribe old symbol, subscribe new
  unsubscribeTradeWS();
  $('t-chart-sym').textContent=sym;
  $('ob-sym').textContent=sym;
  subscribeTradeWS();
  setTradeSymbol(sym);
}

var _tradeSubSymbol = null;
function subscribeTradeWS(){
  var sym = $('tf-symbol').value;
  if (!sym || sym === _tradeSubSymbol) return;
  _tradeSubSymbol = sym;
  wsSubscribe('price', sym);
  wsSubscribe('orderbook', sym);
  wsSubscribe('trades', sym);
  // Direct Binance connection for real-time orderbook + trades
  BinanceWs.connect(sym);
}

function stopTradeRefresh(){}
function unsubscribeTradeWS(){
  if (!_tradeSubSymbol) return;
  wsUnsubscribe('price', _tradeSubSymbol);
  wsUnsubscribe('orderbook', _tradeSubSymbol);
  wsUnsubscribe('trades', _tradeSubSymbol);
  BinanceWs.disconnect();
  _tradeSubSymbol = null;
}

function subscribeDashWS(){
  wsSubscribe('price', 'BTCUSDT');
  wsSubscribe('price', 'ETHUSDT');
}

function unsubscribeDashWS(){
  wsUnsubscribe('price', 'BTCUSDT');
  wsUnsubscribe('price', 'ETHUSDT');
}

// ── Bottom Panel Resize ──
var _bottomResizing=false;
function startBottomResize(e){
  e.preventDefault();
  _bottomResizing=true;
  document.addEventListener('mousemove',onBottomResize);
  document.addEventListener('mouseup',stopBottomResize);
}
function onBottomResize(e){
  if(!_bottomResizing)return;
  var bottom=$('trade-bottom');
  if(!bottom)return;
  var app=$('app');
  var h=app.clientHeight-e.clientY+44;
  h=Math.max(100,Math.min(h,app.clientHeight-300));
  bottom.style.height=h+'px';
  // Chart needs time to resize
  if(S.tradeChart)S.tradeChart.resize();
}
function stopBottomResize(){
  _bottomResizing=false;
  document.removeEventListener('mousemove',onBottomResize);
  document.removeEventListener('mouseup',stopBottomResize);
}

async function cancelAllOrders(){
  try{
    var r=await fetch('/api/orders');var orders=await r.json();
    if(!orders.length){toast('No orders to cancel');return}
    for(var i=0;i<orders.length;i++){
      await fetch('/api/orders/'+orders[i].order_id+'/cancel',{method:'POST'});
    }
    toast(T('toast.cancelled'));
    refreshOrders();
  }catch(e){toast(T('toast.failed'),'err')}
}

// ── KLineChart Pro Datafeed ──
var _lastKlineTs = {};
var _fillingGap = {};

function createTradeDatafeed() {
  return {
    searchSymbols: async function(search) {
      try {
        var r = await fetch('/api/symbols/search?q=' + encodeURIComponent(search || ''));
        var symbols = await r.json();
        return (symbols || []).map(function(s) {
          return { ticker: s, name: s, shortName: s, exchange: 'BINANCE' };
        });
      } catch(e) { return []; }
    },
    getHistoryKLineData: async function(symbol, period, from, to) {
      try {
        var interval;
        var ts = period.timespan;
        var m = period.multiplier;
        if (ts === 'minute') interval = m + 'm';
        else if (ts === 'hour') interval = m + 'h';
        else if (ts === 'day') interval = m + 'd';
        else if (ts === 'week') interval = m + 'w';
        else interval = '1h';
        var url = '/api/klines/' + symbol.ticker + '?interval=' + interval + '&limit=1000';
        if (from && to && from > 0 && to > from) {
          url += '&from_val=' + from + '&to_val=' + to;
        }
        var r = await fetch(url);
        var raw = await r.json();
        if (!raw || !raw.length) return [];
        return raw.map(function(k) {
          return { timestamp: k[0], open: parseFloat(k[1]), high: parseFloat(k[2]), low: parseFloat(k[3]), close: parseFloat(k[4]), volume: parseFloat(k[5]) };
        }).sort(function(a, b) { return a.timestamp - b.timestamp; });
      } catch(e) { return []; }
    },
    subscribe: function(symbol, period, callback) {
      var interval;
      var ts = period.timespan;
      var m = period.multiplier;
      if (ts === 'minute') interval = m + 'm';
      else if (ts === 'hour') interval = m + 'h';
      else if (ts === 'day') interval = m + 'd';
      else if (ts === 'week') interval = m + 'w';
      else if (ts === 'month') interval = m + 'M';
      else interval = '1h';
      var key = symbol.ticker + ':' + interval;
      if (!this._polls) this._polls = {};
      if (this._polls[key]) clearInterval(this._polls[key]);
      this._polls[key] = setInterval(async function() {
        try {
          var r = await fetch('/api/klines/' + symbol.ticker + '?interval=' + interval + '&limit=2');
          var data = await r.json();
          if (data && data.length) {
            var b = data[data.length-1];
            callback(b);
            _lastKlineTs[key] = b.timestamp;
          }
        } catch(e) {}
      }, 3000);
    },
    unsubscribe: function(symbol, period) {
      var interval;
      var ts = period.timespan;
      var m = period.multiplier;
      if (ts === 'minute') interval = m + 'm';
      else if (ts === 'hour') interval = m + 'h';
      else if (ts === 'day') interval = m + 'd';
      else if (ts === 'week') interval = m + 'w';
      else if (ts === 'month') interval = m + 'M';
      else interval = '1h';
      var key = symbol.ticker + ':' + interval;
      if (this._polls && this._polls[key]) { clearInterval(this._polls[key]); delete this._polls[key]; }
    }
  };
}

var TRADE_PERIODS = [
  { multiplier: 1, timespan: 'minute', text: '1m' },
  { multiplier: 5, timespan: 'minute', text: '5m' },
  { multiplier: 15, timespan: 'minute', text: '15m' },
  { multiplier: 30, timespan: 'minute', text: '30m' },
  { multiplier: 1, timespan: 'hour', text: '1h' },
  { multiplier: 4, timespan: 'hour', text: '4h' },
  { multiplier: 1, timespan: 'day', text: '1d' },
  { multiplier: 1, timespan: 'week', text: '1w' }
];

function initTradeChart() {
  if (S.tradeChart) return;
  S.tradeChart = new klinechartspro.KLineChartPro({
    container: 'trade-chart',
    styles: {
      grid: { color: 'rgba(48,54,61,.3)' },
      candle: {
        type: 'candle_solid',
        bar: { upColor: '#3fb950', downColor: '#f85149', noChangeColor: '#888' }
      }
    },
    theme: 'light',
    locale: 'zh-CN',
    drawingBarVisible: true,
    symbol: { ticker: 'BTCUSDT', name: 'BTC/USDT', shortName: 'BTC' },
    period: { multiplier: 1, timespan: 'hour', text: '1h' },
    periods: TRADE_PERIODS,
    mainIndicators: ['MA'],
    subIndicators: [],
    datafeed: createTradeDatafeed()
  });
  // Track current symbol for the ticker strip
  S._tradeSym = 'BTCUSDT';
}

function setTradeSymbol(sym) {
  if (!S.tradeChart) { S._tradeSym = sym; return; }
  S.tradeChart.setSymbol({ ticker: sym, name: sym.replace('USDT', '/USDT'), shortName: sym.replace('USDT', '') });
  S._tradeSym = sym;
}

async function placeOrder(){
  var sym=$('tf-symbol').value,side=S.tradeSide,type=$('tf-type').value;
  var price=parseFloat($('tf-price').value)||0,qty=parseFloat($('tf-qty').value)||0;
  $('tf-msg').innerHTML='';

  // Client-side validation
  if (!sym || sym.trim()==='') {
    $('tf-msg').innerHTML='<span class="r">'+T('val.select_symbol')+'</span>';return;
  }
  if (qty <= 0 || isNaN(qty)) {
    $('tf-msg').innerHTML='<span class="r">'+T('val.invalid_qty')+'</span>';return;
  }
  if (qty > 1000) {
    $('tf-msg').innerHTML='<span class="r">'+T('val.qty_too_large')+'</span>';return;
  }
  if (type==='LIMIT' && (price <= 0 || isNaN(price))) {
    $('tf-msg').innerHTML='<span class="r">'+T('val.invalid_price')+'</span>';return;
  }
  if (side!=='BUY' && side!=='SELL') {
    $('tf-msg').innerHTML='<span class="r">'+T('val.invalid_side')+'</span>';return;
  }
  if (!S.wsOk) {
    $('tf-msg').innerHTML='<span class="r">'+T('val.no_connection')+'</span>';return;
  }

  try{
    var r=await fetch('/api/order',{
      method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({symbol:sym,side:side,order_type:type,price:price,quantity:qty,exchange:'BINANCE'})
    });
    var d=await r.json();
    if(r.ok){toast(T('toast.order_placed')+': '+d.order_id);refreshOrders();$('tf-msg').innerHTML='<span class="g">'+T('toast.order_placed')+'</span>'}
    else{$('tf-msg').innerHTML='<span class="r">'+d.detail+'</span>'}
  }catch(e){$('tf-msg').innerHTML='<span class="r">Error: '+e.message+'</span>'}
}

// ── Order management with Record pattern (O(1) operations) ──
S.orders = {};

function orderRowHTML(o){
  var timeStr=new Date((o.created_at||0)*1000).toLocaleTimeString().substring(0,5);
  var filled=o.filled||o.filled_qty||0;
  return '<tr id="ord-'+o.order_id+'"><td style="font-size:8px">'+timeStr+'</td><td>'+o.symbol+'</td><td class="'+(o.side==='BUY'?'g':'r')+'">'+o.side+'</td><td>'+FUSD(o.price)+'</td><td>'+o.quantity+'</td><td>'+filled+'</td><td class="ord-status">'+o.status+'</td><td><button class="btn btn-xs btn-r" onclick="cancelOrd(\''+o.order_id+'\')">X</button></td></tr>';
}

function addOrderRow(o){
  S.orders[o.order_id]=o;
  var empty=document.querySelector('#t-orders .m');
  if(empty)empty.parentElement.innerHTML='';
  $('t-orders').insertAdjacentHTML('beforeend',orderRowHTML(o));
  updateOrderCount();
}

function updateOrderCount(){
  var count=Object.keys(S.orders).length;
  var el=$('t-order-count');if(el)el.textContent=count+' 个订单';
  var tc=$('t-order-tab-count');if(tc)tc.textContent=count>0?count:'0';
}

function removeOrderRow(orderId){
  delete S.orders[orderId];
  var row=document.getElementById('ord-'+orderId);
  if(row)row.remove();
  updateOrderCount();
}

async function refreshOrders(){
  try{
    var r=await fetch('/api/orders');var orders=await r.json();
    S.orders={};var h='';
    for(var i=0;i<orders.length;i++){
      var o=orders[i];S.orders[o.order_id]=o;
      h+=orderRowHTML(o);
    }
    $('t-orders').innerHTML=h||'<tr><td colspan="8" class="m" style="text-align:center;padding:16px">暂无活跃订单</td></tr>';
    updateOrderCount();
    if(S.page==='dash'){S.cache.dashOrders=orders;renderDashOrders(orders);}
  }catch(e){}
}

async function cancelOrd(oid){
  try{await fetch('/api/orders/'+oid+'/cancel',{method:'POST'});toast('已撤销');removeOrderRow(oid)}catch(e){toast('取消失败','err')}
}

// KLineChart Pro handles indicators and drawing tools via its built-in UI

function toggleChartFullscreen(){
  var chartEl=$('trade-chart').parentElement;
  if(document.fullscreenElement){
    document.exitFullscreen();
  }else{
    chartEl.requestFullscreen().catch(function(){});
  }
}

function renderTradePositions(positions){
  var el=$('t-positions-detailed');if(!el)return;
  if(!positions||!positions.length){el.innerHTML='<div class=\"m\" style=\"text-align:center;padding:20px;width:100%\">暂无持仓</div>';return}
  var h='';
  for(var i=0;i<positions.length;i++){var p=positions[i];
    var pnl=(p.unrealized_pnl||0),entry=p.entry_price||0,size=p.size||p.quantity||0;
    var pnlPct=entry>0?pnl/(entry*size)*100:0,clr=pnl>=0?'var(--green)':'var(--red)';
    h+='<div class=\"pos-card\"><span class=\"pc-symbol\">'+p.symbol+'</span><span class=\"pc-side '+(pnl>=0?'long':'short')+'\">'+(pnl>=0?'LONG':'SHORT')+'</span>';
    h+='<div class=\"pc-row\"><span>入场</span><span>$'+FX(entry)+'</span></div>';
    h+='<div class=\"pc-row\"><span>现价</span><span>$'+FX(p.mark_price||p.current_price||0)+'</span></div>';
    h+='<div class=\"pc-row\"><span>数量</span><span>'+FX(size,4)+'</span></div>';
    h+='<div class=\"pc-pnl\" style=\"color:'+clr+'\">'+(pnl>=0?'+':'')+'$'+FX(pnl,2)+' ('+FXP(pnlPct)+')</div></div>';
  }
  el.innerHTML=h;
}
function renderTradeHistory(trades){
  var el=$('t-trades-body');if(!el)return;
  if(!trades||!trades.length){el.innerHTML='<tr><td colspan=\"6\" class=\"m\" style=\"text-align:center;padding:16px\">暂无成交记录</td></tr>';return}
  var h='';
  for(var i=0;i<Math.min(trades.length,50);i++){var t=trades[i];
    var ts=t.time||t.created_at;if(ts>1e10)ts=ts/1000;
    var timeStr=new Date(ts*1000).toISOString().slice(11,19);
    var pnl=t.pnl||0,pnlClr=pnl>0?'var(--green)':pnl<0?'var(--red)':'var(--muted)';
    h+='<tr><td style=\"font-size:9px\">'+timeStr+'</td><td>'+t.symbol+'</td><td style=\"color:'+(t.side==='buy'||t.side==='BUY'?'var(--green)':'var(--red)')+'\">'+(t.side||'--')+'</td><td>$'+FX(t.price||0)+'</td><td>'+FX(t.quantity||t.qty,4)+'</td><td style=\"color:'+pnlClr+'\">'+FXP(pnl)+'</td></tr>';
  }
  el.innerHTML=h;
}
function refreshTradePositionsTrades(){
  fetch('/api/dashboard/summary').then(function(r){return r.json()}).then(function(d){
    if(d.status!=='ok'||!d.data)return;
    renderTradePositions(d.data.positions||[]);
    renderTradeHistory(d.data.recent_trades||[]);
  }).catch(function(){});
}
function FX(n,d){d=d||2;return n!=null?Number(n).toFixed(d):'--'}
function FXP(n){if(n==null)return'--';return (n>=0?'+':'')+Number(n).toFixed(2)+'%'}
function renderRecentTrades(trades){
  if(!trades||!trades.length){$('recent-trades').innerHTML='<div class="m" style="text-align:center;padding:16px">等待数据...</div>';return}
  var h='';
  for(var i=0;i<Math.min(trades.length,20);i++){
    var t=trades[i];
    var sideClass=t.side==='BUY'?'g':'r';
    h+='<div class="ob-row" style="grid-template-columns:1fr 1fr 1fr"><span class="'+sideClass+'">'+FUSD(t.price)+'</span><span style="text-align:right">'+F(t.qty||t.quantity,4)+'</span><span style="text-align:right;font-size:9px;color:var(--muted)">'+new Date(t.time*1000).toLocaleTimeString().substring(0,5)+'</span></div>';
  }
  $('recent-trades').innerHTML=h;
}

// ═══════════════════════════════════════════════ BACKTEST ═══════════════════════════════════════════
function updateBtParams(){
  var st=$('bt-strategy').value;
  var html='';
  if(st==='breakout')html='<div class="f-label">Period</div><input id="bt-period" value="20" type="number"><div class="f-label" style="margin-top:6px">Qty</div><input id="bt-qty" value="0.001" step="0.001">';
  else if(st==='market_making')html='<div class="f-label">Base Spread</div><input id="bt-spread" value="0.002" step="0.0001"><div class="f-label" style="margin-top:6px">Qty</div><input id="bt-qty" value="0.001" step="0.001">';
  else if(st==='grid')html='<div class="f-label">Upper Price</div><input id="bt-upper" value="72000"><div class="f-label" style="margin-top:6px">Lower Price</div><input id="bt-lower" value="64000"><div class="f-label" style="margin-top:6px">Grids</div><input id="bt-grids" value="10" type="number"><div class="f-label" style="margin-top:6px">Qty</div><input id="bt-qty" value="0.001" step="0.001">';
  $('bt-params').innerHTML=html;
}
$('bt-strategy').addEventListener('change',updateBtParams);
updateBtParams();

function initBtChart(){
  if(S.btChart)return;
  S.btChart=LightweightCharts.createChart($('bt-chart'),{
    layout:{background:{color:'#1f2328fff'},textColor:'#1f2328'},
    grid:{vertLines:{color:'rgba(48,54,61,.3)'},horzLines:{color:'rgba(48,54,61,.3)'}},
    rightPriceScale:{borderColor:'#d0d7de'},timeScale:{borderColor:'#d0d7de',timeVisible:true},
  });
  S.btSeries=S.btChart.addAreaSeries({topColor:'rgba(88,166,255,.3)',bottomColor:'rgba(88,166,255,.01)',lineColor:'#58a6ff',lineWidth:2});
}

async function runBacktest(){
  var btn=$('bt-run-btn');btn.disabled=true;btn.textContent=T('bt.running');
  $('bt-status').textContent=T('bt.running_status');

  var st=$('bt-strategy').value;
  var params={qty:parseFloat($('bt-qty')?.value||0.001)};
  if(st==='breakout')params.period=parseInt($('bt-period')?.value||20);
  if(st==='market_making')params.base_spread=parseFloat($('bt-spread')?.value||0.002);
  if(st==='grid'){params.upper_price=parseFloat($('bt-upper')?.value||72000);params.lower_price=parseFloat($('bt-lower')?.value||64000);params.grid_num=parseInt($('bt-grids')?.value||10);}

  var config={
    strategy:st,params:params,
    symbol:$('bt-symbol').value,
    initial_balance:{USDT:parseFloat($('bt-capital').value)},
    fee_rate:parseFloat($('bt-fee').value),
    slippage:parseFloat($('bt-slippage').value),
    num_bars:parseInt($('bt-bars').value),
    base_price:parseFloat($('bt-price').value),
  };

  try{
    var r=await fetch('/api/backtest/run',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(config)});
    var d=await r.json();
    if(d.error){$('bt-status').textContent=d.error;btn.disabled=false;btn.textContent=T('bt.run');return}
    $('bt-status').textContent='';

    // Chart
    if(!S.btChart)initBtChart();
    var eqData=[];
    if(d.equity_curve)for(var i=0;i<d.equity_curve.length;i++){var p=d.equity_curve[i];eqData.push({time:Date.now()/1000-(d.equity_curve.length-i)*60,value:p.equity});}
    S.btSeries.setData(eqData);S.btChart.timeScale().fitContent();

    // Metrics
    var rep=d.report||{};
    var metrics=[
      ['Total Return',FP(rep.total_return_pct)],
      ['Annual Return',FP(rep.annual_return_pct)],
      ['Sharpe',F(rep.sharpe_ratio,3)],
      ['Sortino',F(rep.sortino_ratio,3)],
      ['Calmar',F(rep.calmar_ratio,3)],
      ['Max Drawdown',FP(rep.max_drawdown_pct)],
      ['Win Rate',FP(rep.win_rate_pct)],
      ['Profit Factor',F(rep.profit_factor,3)],
      ['Total Trades',rep.total_trades||0],
      ['Final Equity',FUSD(rep.final_equity)],
      ['Initial Equity',FUSD(rep.initial_equity)],
    ];
    var mh='';
    for(var i=0;i<metrics.length;i++){
      mh+='<div style="background:var(--bg);padding:10px;border-radius:6px"><div class="f-label">'+metrics[i][0]+'</div><div style="font-size:16px;font-weight:700">'+metrics[i][1]+'</div></div>';
    }
    $('bt-metrics').innerHTML=mh;
    $('bt-last-result').innerHTML='<span class="g">'+T('bt.last_run')+': '+now()+' | '+d.trades.length+' '+T('bt.trades')+'</span>';
    toast('Backtest complete');
  }catch(e){
    $('bt-status').textContent='Error: '+e.message;
  }
  btn.disabled=false;btn.textContent=T('bt.run');
}

// ═══════════════════════════════════════════════ STRATEGIES MODULE ═══════════════════════════════════════════
var spotStatus='all',ctStatus='all',tplCategory='spot',logTimer=null;S.subPage='spot';

function switchSubPage(sub){
  S.subPage=sub;
  document.querySelectorAll('.sidebar-tab').forEach(function(t){t.classList.toggle('active',t.dataset.sub===sub)});
  document.querySelectorAll('.sub-page').forEach(function(p){p.classList.toggle('active',p.id==='sub-'+sub)});
  if(sub==='spot')loadSpotList();
  if(sub==='contract')loadContractList();
  if(sub==='templates')loadTemplates();
  if(sub==='logs'){loadLogs();$('log-auto').checked=true;startLogAuto()}
  if(sub==='ai'){initAIChart();if(typeof loadAI2Chart==='function')loadAI2Chart()}
  updateSidebarLabels();
  updateEmptyStates();
}

function updateSidebarLabels(){
  var tabs=document.querySelectorAll('.sidebar-tab');
  if(tabs.length>=6){
    tabs[0].textContent=T('nav.spot');tabs[1].textContent=T('nav.contract');
    tabs[2].textContent=T('nav.ai_gen');tabs[3].textContent=T('nav.native');
    tabs[4].textContent=T('nav.templates');tabs[5].textContent=T('nav.logs');
  }
}

function updateEmptyStates(){
  var sub=S.subPage;
  if(sub==='spot'){$('spot-empty').style.display=$('spot-tbody').children.length===0?'flex':'none'}
  else if(sub==='contract'){$('ct-empty').style.display=$('ct-tbody').children.length===0?'flex':'none'}
  else if(sub==='templates'){$('tpl-empty').style.display=$('tpl-list').children.length===0?'flex':'none'}
  else if(sub==='logs'){$('log-empty').style.display=$('log-tbody').children.length===0?'flex':'none'}
}

function refreshStrategyPage(){
  updateSidebarLabels();
  if(S.subPage==='spot')loadSpotList();
  else if(S.subPage==='contract')loadContractList();
  else if(S.subPage==='templates')loadTemplates();
  else if(S.subPage==='logs')loadLogs();
  loadGlobalSettings();
}

// ── Global Settings ──
async function loadGlobalSettings(){
  try{
    var r=await fetch('/api/strategies/global');var d=await r.json();
    $('sk-protection-toggle').classList.toggle('active',d.profit_protection_enabled||false);$('sk-protection').value=(d.profit_protection_enabled||false)?'1':'0';
    $('sk-max-orders').value=d.max_concurrent_orders||5;
    var st=await (await fetch('/api/status')).json();
    var eq=st.portfolio?st.portfolio.total_equity:'0.00';
    $('sk-margin').textContent=FUSD(parseFloat(eq)||0);
    $('sk-holdings').textContent=FUSD(parseFloat(st.portfolio?st.portfolio.margin_used:'0')||0);
  }catch(e){}
}

async function saveGlobalSettings(){
  await fetch('/api/strategies/global',{method:'PUT',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({profit_protection_enabled:$('sk-protection-toggle').classList.contains('active'),max_concurrent_orders:parseInt($('sk-max-orders').value)||5})});
}

// ── Spot List ──
async function loadSpotList(){
  try{
    var p=[];if(spotStatus!=='all'&&spotStatus!=='history')p.push('status='+spotStatus);
    var type=$('spot-type').value;if(type)p.push('type='+type);
    var coin=$('spot-search').value.trim();if(coin)p.push('coin='+encodeURIComponent(coin));
    var q='category=spot&limit=200';if(p.length)q+='&'+p.join('&');
    var r=await fetch('/api/strategies/configs?'+q);var rows=await r.json();
    S.cache.spotRows=rows;renderSpotList(rows);
  }catch(e){}
}

function renderSpotList(rows){
  var h='',running=0,totalPnl=0;
  for(var i=0;i<rows.length;i++){
    var o=rows[i];
    if(o.status==='running')running++;
    totalPnl+=(o.pnl||0);
    var cfg={};try{cfg=typeof o.config_json==='string'?JSON.parse(o.config_json):(o.config_json||{})}catch(e){}
    var typeLabel={'martin':'马丁趋势','wall_street':'华尔街','radical':'激进','conservative':'保守','hft':'高频'}[o.strategy_type]||o.strategy_type;
    var stClass=o.status==='running'?'status-running':(o.status==='stopped'?'status-stopped':'status-history');
    h+='<tr>';
    h+='<td><input type="checkbox" class="spot-chk" value="'+o.id+'" onchange="updateSpotBatchButtons()"></td>';
    h+='<td style="font-size:9px;font-family:monospace">'+o.id+'</td>';
    h+='<td class="coin-cell" title="'+o.coin+'">'+o.coin+'</td><td>'+typeLabel+'</td>';
    h+='<td class="'+stClass+'">'+T('status.'+(o.status==='running'?'running':'stopped'))+'</td>';
    h+='<td class="'+(o.pnl>=0?'g':'r')+'">'+FUSD(o.pnl)+'</td>';
    h+='<td style="white-space:nowrap">';
    if(o.status==='running')h+='<button class="btn btn-xs btn-r" onclick="actSt(\''+o.id+'\',\'stop\')">'+T('btn.stop')+'</button>';
    else h+='<button class="btn btn-xs btn-g" onclick="actSt(\''+o.id+'\',\'start\')">'+T('btn.start')+'</button>';
    h+=' <button class="btn btn-xs" style="background:#1f6feb;border-color:#1f6feb;color:#1f2328" onclick="editStrategy(\''+o.id+'\')">'+T('btn.edit')+'</button>';
    h+=' <button class="btn btn-xs btn-r" onclick="confirmDelete(\''+o.id+'\')">'+T('btn.delete')+'</button>';
    h+='</td></tr>';
  }
  $('spot-tbody').innerHTML=h;
  $('spot-empty').style.display=h?'none':'flex';
  $('spot-total').textContent=rows.length;$('spot-running').textContent=running;$('spot-pnl').textContent=FUSD(totalPnl);
  $('spot-check-all').checked=false;
  updateSpotBatchButtons();
  document.querySelectorAll('#sub-spot .st-tab').forEach(function(t){t.classList.toggle('active',t.dataset.st===spotStatus)});
}

function updateSpotBatchButtons(){
  var n=document.querySelectorAll('.spot-chk:checked').length;
  $('spot-selected').textContent=n;
  $('spot-batch-start').disabled=n===0;
  $('spot-batch-stop').disabled=n===0;
  $('spot-batch-del').disabled=n===0;
}

function toggleAllSpot(){var c=$('spot-check-all').checked;document.querySelectorAll('.spot-chk').forEach(function(b){b.checked=c});updateSpotBatchButtons()}

async function batchSpot(action){
  var ids=[];document.querySelectorAll('.spot-chk:checked').forEach(function(b){ids.push(b.value)});
  if(!ids.length){toast('No selection','err');return}
  if(action==='delete'){
    for(var i=0;i<ids.length;i++)await fetch('/api/strategies/configs/'+ids[i],{method:'DELETE'});
    toast(T('toast.deleted'));
  }else{
    await fetch('/api/strategies/configs/batch-'+action,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({ids:ids})});
    toast(action==='start'?T('toast.started'):T('toast.stopped'));
  }
  loadSpotList();
}

async function actSt(id,action){
  await fetch('/api/strategies/configs/'+id+'/'+action,{method:'POST'});
  toast(action==='start'?T('toast.started'):T('toast.stopped'));
  loadSpotList();
}

// ── Contract List ──
async function loadContractList(){
  try{
    var p=[];if(ctStatus!=='all'&&ctStatus!=='history')p.push('status='+ctStatus);
    var type=$('ct-type').value;if(type)p.push('type='+type);
    var coin=$('ct-search').value.trim();if(coin)p.push('coin='+encodeURIComponent(coin));
    var q='category=contract&limit=200';if(p.length)q+='&'+p.join('&');
    var r=await fetch('/api/strategies/configs?'+q);var rows=await r.json();
    S.cache.ctRows=rows;renderContractList(rows);
  }catch(e){}
}

function renderContractList(rows){
  var h='',sh=0,lo=0,on=0;
  for(var i=0;i<rows.length;i++){
    var o=rows[i];
    if(o.direction==='short')sh++;else lo++;
    if(o.status==='running'||o.status==='monitoring')on++;
    var cfg={};try{cfg=typeof o.config_json==='string'?JSON.parse(o.config_json):(o.config_json||{})}catch(e){}
    var typeLabel={'trend_long':'顺势多','trend_short':'顺势空','counter_stable':'逆势稳健','counter_conservative':'逆势保守','hft':'高频','arbitrage':'首尾套利'}[o.strategy_type]||o.strategy_type;
    var dirLabel={'long':'多','short':'空','both':'双向'}[o.direction]||o.direction;
    var stClass=o.status==='running'?'status-running':(o.status==='stopped'?'status-stopped':(o.status==='monitoring'?'status-monitoring':'status-history'));
    h+='<tr><td><input type="checkbox" class="ct-chk" value="'+o.id+'" onchange="updateCtBatchButtons()"></td>';
    h+='<td style="font-size:9px;font-family:monospace">'+o.id+'</td>';
    h+='<td class="coin-cell" title="'+o.coin+'">'+o.coin+'</td>';
    h+='<td class="'+(o.direction==='short'?'r':'g')+'">'+dirLabel+'</td>';
    h+='<td>'+F(o.leverage,0)+'x</td>';
    h+='<td class="'+stClass+'">'+T('status.'+((o.status==='running'||o.status==='stopped'||o.status==='monitoring')?o.status:'stopped'))+'</td>';
    h+='<td class="'+(o.pnl>=0?'g':'r')+'">'+FUSD(o.pnl)+'</td>';
    h+='<td style="white-space:nowrap">';
    if(o.status==='running'||o.status==='monitoring')h+='<button class="btn btn-xs btn-r" onclick="actCt(\''+o.id+'\',\'stop\')">'+T('btn.stop')+'</button>';
    else h+='<button class="btn btn-xs btn-g" onclick="actCt(\''+o.id+'\',\'start\')">'+T('btn.start')+'</button>';
    h+=' <button class="btn btn-xs" style="background:#1f6feb;border-color:#1f6feb;color:#1f2328" onclick="editStrategy(\''+o.id+'\')">'+T('btn.edit')+'</button>';
    h+=' <button class="btn btn-xs btn-r" onclick="confirmDelete(\''+o.id+'\')">'+T('btn.delete')+'</button>';
    h+='</td></tr>';
  }
  $('ct-tbody').innerHTML=h;
  $('ct-empty').style.display=h?'none':'flex';
  $('ct-short').textContent=sh;$('ct-long').textContent=lo;$('ct-online').textContent=on;
  $('ct-check-all').checked=false;
  updateCtBatchButtons();
  document.querySelectorAll('#sub-contract .st-tab').forEach(function(t){t.classList.toggle('active',t.dataset.st===ctStatus)});
}

function toggleAllCt(){var c=$('ct-check-all').checked;document.querySelectorAll('.ct-chk').forEach(function(b){b.checked=c});updateCtBatchButtons()}
function updateCtBatchButtons(){
  var n=document.querySelectorAll('.ct-chk:checked').length;
  $('ct-selected').textContent=n;
  $('ct-batch-start').disabled=n===0;
  $('ct-batch-stop').disabled=n===0;
  $('ct-batch-del').disabled=n===0;
}

async function batchCt(action){
  var ids=[];document.querySelectorAll('.ct-chk:checked').forEach(function(b){ids.push(b.value)});
  if(!ids.length){toast('No selection','err');return}
  if(action==='delete'){
    for(var i=0;i<ids.length;i++)await fetch('/api/strategies/configs/'+ids[i],{method:'DELETE'});
    toast(T('toast.deleted'));
  }else{
    await fetch('/api/strategies/configs/batch-'+action,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({ids:ids})});
    toast(action==='start'?T('toast.started'):T('toast.stopped'));
  }
  loadContractList();
}

async function actCt(id,action){
  await fetch('/api/strategies/configs/'+id+'/'+action,{method:'POST'});
  toast(action==='start'?T('toast.started'):T('toast.stopped'));
  loadContractList();
}

// ── Strategy Form (shared for spot & contract) ──
function openStrategyForm(category){
  S.strategyForm={mode:'create',id:null,data:{},conditions:[],tpTiers:[],category:category};
  renderStrategyForm();
  $('sf-modal').classList.add('show');
  loadTemplatesToDropdown(category);
}

async function editStrategy(id){
  try{
    var r=await fetch('/api/strategies/configs/'+id);var o=await r.json();
    var cfg={};try{cfg=typeof o.config_json==='string'?JSON.parse(o.config_json):(o.config||{})}catch(e){}
    S.strategyForm={mode:'edit',id:id,data:cfg,conditions:cfg.avg_conditions||[],tpTiers:cfg.tp_tiers||[],category:o.category||'spot'};
    renderStrategyForm();
    $('sf-modal').classList.add('show');
    loadTemplatesToDropdown(o.category);
  }catch(e){toast('Failed to load','err')}
}

function renderStrategyForm(){
  var f=S.strategyForm,d=f.data,c=f.category,isSpot=c==='spot';
  var h='<h3>'+T(isSpot?'btn.create_spot':'btn.create_contract')+'</h3>';
  // Basic info
  h+='<div class="form-section"><div class="form-section-title">'+T('form.basic')+'</div><div class="form-grid">';
  h+='<div><div class="f-label">'+T('form.strategy_type')+'</div>';
  if(isSpot){
    h+='<div id="sf-type" style="display:flex;flex-wrap:wrap;gap:4px">';
    ['martin','wall_street','radical','conservative','hft'].forEach(function(t){
      var lb={'martin':'马丁趋势','wall_street':'华尔街','radical':'激进','conservative':'保守','hft':'高频'}[t];
      h+='<label class="radio-chip"><input type="radio" name="sf-type" value="'+t+'"'+((d.strategy_type||'martin')===t?' checked':'')+' onchange="onSpotTypeChange()"><span>'+lb+'</span></label>';
    });
    h+='</div>';
  }else{
    h+='<select id="sf-type">';
    ['trend_long','trend_short','counter_stable','counter_conservative','hft','arbitrage'].forEach(function(t){
      var lb={'trend_long':'顺势多','trend_short':'顺势空','counter_stable':'逆势稳健','counter_conservative':'逆势保守','hft':'高频','arbitrage':'首尾套利'}[t];
      h+='<option value="'+t+'"'+((d.strategy_type||'trend_long')===t?' selected':'')+'>'+lb+'</option>';
    });
    h+='</select>';
  }
  h+='</div>';
  h+='<div><div class="f-label">'+T('form.template')+'</div><select id="sf-template" onchange="loadTemplateToForm()"><option value="">-- '+T('form.template')+' --</option></select></div>';
  h+='<div><div class="f-label">'+T('form.coin')+'</div>';
  h+='<div style="position:relative"><input id="sf-coin-search" placeholder="'+T('filter.search')+'" oninput="filterCoinList()" style="margin-bottom:2px;font-size:10px;padding:5px 8px" value="'+(d.coin||'BTCUSDT')+'">';
  h+='<select id="sf-coin" onchange="syncCoinSearch()" style="font-size:10px">';
  ['BTCUSDT','ETHUSDT','SOLUSDT','XRPUSDT','BNBUSDT','ADAUSDT','DOGEUSDT','AVAXUSDT','DOTUSDT','LINKUSDT','MATICUSDT','UNIUSDT','ATOMUSDT','LTCUSDT','ETCUSDT','FILUSDT','APTUSDT','ARBUSDT','OPUSDT','NEARUSDT','SUIUSDT','PEPEUSDT','SHIBUSDT','WIFUSDT','BONKUSDT'].forEach(function(sym){
    h+='<option value="'+sym+'"'+((d.coin||'BTCUSDT')===sym?' selected':'')+'>'+sym+'</option>';
  });
  h+='</select></div></div>';
  if(!isSpot){h+='<div><div class="f-label">'+T('form.direction')+'</div><select id="sf-dir"><option value="long"'+(d.direction==='long'?' selected':'')+'>'+T('form.dir_long')+'</option><option value="short"'+(d.direction==='short'?' selected':'')+'>'+T('form.dir_short')+'</option><option value="both"'+(d.direction==='both'?' selected':'')+'>'+T('form.dir_both')+'</option></select></div>';}
  h+='</div></div>';

  // Entry
  h+='<div class="form-section"><div class="form-section-title">'+T('form.entry')+'</div><div class="form-grid">';
  if(!isSpot){
    h+='<div><div class="f-label">'+T('form.entry_macd')+'</div><select id="sf-emacd">'+intervalOptions(d.entry_macd)+'</select></div>';
    h+='<div><div class="f-label">'+T('form.entry_counter_ema')+'</div><select id="sf-cema">'+intervalOptions(d.counter_ema)+'</select></div>';
    h+='<div><div class="f-label">'+T('form.entry_trend_ema')+'</div><select id="sf-tema">'+intervalOptions(d.trend_ema)+'</select></div>';
  }
  h+='<div><div class="f-label">'+T('form.limit_price')+'</div><input id="sf-price" type="number" value="'+(d.limit_price||0)+'" step="0.01"></div>';
  h+='<div><div class="f-label">'+T('form.first_amount')+'</div><input id="sf-amt" type="number" value="'+(d.first_amount||(isSpot?10:5))+'" step="1"></div>';
  h+='<div><div class="f-label">'+T('form.first_mult')+'</div><input id="sf-mult" type="number" value="'+(d.first_mult||1)+'" step="0.1"></div>';
  h+='<div><div class="f-label">'+T('form.cycle_type')+'</div><select id="sf-cycle"><option value="single"'+(d.cycle_type==='single'?' selected':'')+'>'+T('form.single')+'</option><option value="loop"'+(d.cycle_type!=='single'?' selected':'')+'>'+T('form.loop')+'</option></select></div>';
  h+='<div><div class="f-label">'+T('form.cycle_count')+'</div><input id="sf-ccnt" type="number" value="'+(d.cycle_count||(isSpot?100:5000))+'" step="1"></div>';
  if(!isSpot){
    h+='<div><div class="f-label">'+T('form.leverage')+'</div><input id="sf-lev" type="number" value="'+(d.leverage||10)+'" step="1"></div>';
    h+='<div class="form-inline"><div class="toggle-switch'+(d.entry_doubling?' active':'')+'" onclick="toggleSwitch(this)" id="sf-edouble"></div><input type="hidden" class="toggle-input" value="'+(d.entry_doubling?'1':'0')+'"><span style="font-size:10px;">'+T('form.entry_double')+'</span></div>';
    h+='<div class="form-inline"><div class="toggle-switch'+(d.follow_trend?' active':'')+'" onclick="toggleSwitch(this)" id="sf-ftrend"></div><input type="hidden" class="toggle-input" value="'+(d.follow_trend?'1':'0')+'"><span style="font-size:10px;">'+T('form.follow_trend')+'</span></div>';
  }
  h+='</div></div>';

  // Averaging
  h+='<div class="form-section"><div class="form-section-title">'+T('form.avg')+'</div><div class="form-grid">';
  if(!isSpot){
    h+='<div><div class="f-label">'+T('form.avg_macd')+'</div><select id="sf-amacd">'+intervalOptions(d.avg_macd)+'</select></div>';
    h+='<div><div class="f-label">'+T('form.avg_ema')+'</div><select id="sf-aema">'+intervalOptions(d.avg_ema)+'</select></div>';
  }
  h+='<div class="form-inline"><div class="toggle-switch'+(d.avg_enabled!==false?' active':'')+'" onclick="toggleSwitch(this)" id="sf-aen"></div><input type="hidden" class="toggle-input" value="'+(d.avg_enabled!==false?'1':'0')+'"><span style="font-size:10px;">'+T('form.avg_enable')+'</span></div>';
  h+='<div><div class="f-label">'+T('form.avg_count')+'</div><input id="sf-acnt" type="number" value="'+(d.avg_count||(isSpot?7:9))+'" step="1"></div>';
  h+='<div><div class="f-label">'+T('form.avg_total')+'</div><input id="sf-atot" type="number" value="'+(d.avg_total||(isSpot?1280:0))+'" step="1" readonly style="opacity:0.6"></div>';
  h+='<div class="form-inline"><div class="toggle-switch'+(d.anti_waterfall?' active':'')+'" onclick="toggleSwitch(this)" id="sf-awf"></div><input type="hidden" class="toggle-input" value="'+(d.anti_waterfall?'1':'0')+'"><span style="font-size:10px;">'+T('form.anti_waterfall')+'</span></div>';
  h+='<div><div class="f-label">'+T('form.anti_waterfall_ratio')+'</div><input id="sf-awfr" type="number" value="'+(d.anti_waterfall_ratio||2)+'" step="0.01"></div>';
  h+='<div class="full"><button class="btn btn-xs" onclick="openConditionsModal()">'+T('btn.conditions')+' ('+(isSpot?7:9)+T('col.order')+')</button></div>';
  h+='</div></div>';

  // P&L
  h+='<div class="form-section"><div class="form-section-title">'+T('form.pnl')+'</div><div class="form-grid">';
  h+='<div><div class="f-label">'+T('form.tp_mode')+'</div><select id="sf-tpm"><option value="full"'+(d.tp_mode==='full'?' selected':'')+'>'+T('form.tp_full')+'</option><option value="tail"'+(d.tp_mode==='tail'?' selected':'')+'>'+T('form.tp_tail')+'</option><option value="first_tail"'+(d.tp_mode==='first_tail'?' selected':'')+'>'+T('form.tp_first_tail')+'</option></select></div>';
  h+='<div><div class="f-label">'+T('form.tp_type')+'</div><select id="sf-tpt"><option value="static"'+(d.tp_type==='static'?' selected':'')+'>'+T('form.tp_static')+'</option><option value="trailing"'+(d.tp_type!=='static'?' selected':'')+'>'+T('form.tp_trailing')+'</option></select></div>';
  h+='<div class="full"><button class="btn btn-xs" onclick="openTPSettingsModal()">'+T('btn.tp_params')+' (4'+T('col.tier')+')</button></div>';
  if(!isSpot){
    h+='<div><div class="f-label">'+T('form.reverse_tp')+'</div><select id="sf-rtp">'+intervalOptions(d.reverse_tp)+'</select></div>';
    h+='<div class="form-inline"><div class="toggle-switch'+(d.reverse_sl?' active':'')+'" onclick="toggleSwitch(this)" id="sf-rsl"></div><input type="hidden" class="toggle-input" value="'+(d.reverse_sl?'1':'0')+'"><span style="font-size:10px;">'+T('form.reverse_sl')+'</span></div>';
    h+='<div class="form-inline"><div class="toggle-switch'+(d.sl_enabled!==false?' active':'')+'" onclick="toggleSwitch(this)" id="sf-slen"></div><input type="hidden" class="toggle-input" value="'+(d.sl_enabled!==false?'1':'0')+'"><span style="font-size:10px;">'+T('form.sl_enable')+'</span></div>';
    h+='<div><div class="f-label">'+T('form.sl_type')+'</div><select id="sf-slt"><option value="ratio"'+(d.sl_type==='ratio'?' selected':'')+'>'+T('form.sl_ratio')+'</option><option value="amount"'+(d.sl_type==='amount'?' selected':'')+'>'+T('form.sl_amount')+'</option><option value="price"'+(d.sl_type==='price'?' selected':'')+'>'+T('form.sl_price')+'</option></select></div>';
    h+='<div><div class="f-label">'+T('form.sl_ratio')+'</div><input id="sf-slr" type="number" value="'+(d.sl_ratio||40)+'" step="0.1"></div>';
  }
  h+='</div></div>';

  // Burn (contract only)
  if(!isSpot){
    h+='<div class="form-section"><div class="form-section-title">'+T('form.burn')+'</div><div class="form-grid">';
    h+='<div class="form-inline"><div class="toggle-switch'+(d.global_burn?' active':'')+'" onclick="toggleSwitch(this)" id="sf-bg"></div><input type="hidden" class="toggle-input" value="'+(d.global_burn?'1':'0')+'"><span style="font-size:10px;">'+T('form.burn_global')+'</span></div>';
    h+='<div class="form-inline"><div class="toggle-switch'+(d.counter_burn?' active':'')+'" onclick="toggleSwitch(this)" id="sf-bc"></div><input type="hidden" class="toggle-input" value="'+(d.counter_burn?'1':'0')+'"><span style="font-size:10px;">'+T('form.burn_counter')+'</span></div>';
    h+='</div></div>';
  }

  // Actions
  h+='<div class="btn-row"><label style="font-size:10px;display:flex;align-items:center;gap:4px;margin-right:auto"><input type="checkbox" id="sf-save-tpl">'+T('form.save_default')+'</label>';
  h+='<button class="btn" onclick="closeStrategyForm()">'+T('btn.cancel')+'</button>';
  h+='<button class="btn btn-p" onclick="saveStrategyForm()">'+T('btn.save')+'</button></div>';

  $('sf-content').innerHTML=h;
}

function closeStrategyForm(){$('sf-modal').classList.remove('show')}

function filterCoinList(){
  var val=$('sf-coin-search').value.toUpperCase();
  var opts=$('sf-coin').options,found=null;
  for(var i=0;i<opts.length;i++){
    var show=opts[i].text.toUpperCase().indexOf(val)>=0;
    opts[i].style.display=show?'':'none';
    if(show&&!found)found=opts[i];
  }
  if(found&&!opts[$('sf-coin').selectedIndex].style.display)found.selected=true;
}
function syncCoinSearch(){$('sf-coin-search').value=$('sf-coin').value}
function onSpotTypeChange(){}

function intervalOptions(sel){
  var ivs=[['1m','1分钟'],['5m','5分钟'],['15m','15分钟'],['1h','1小时'],['4h','4小时'],['1d','1天']];
  var h='<option value="">关闭</option>';
  for(var i=0;i<ivs.length;i++)h+='<option value="'+ivs[i][0]+'"'+(sel===ivs[i][0]?' selected':'')+'>'+ivs[i][1]+'</option>';
  return h;
}

async function saveStrategyForm(){
  var c=S.strategyForm.category,isSpot=c==='spot',d={};
  d.strategy_type=isSpot?(document.querySelector('input[name="sf-type"]:checked')||{}).value||'martin':$('sf-type').value;d.coin=$('sf-coin').value.toUpperCase();
  d.limit_price=parseFloat($('sf-price').value)||0;
  d.first_amount=parseFloat($('sf-amt').value)||(isSpot?10:5);
  d.first_mult=parseFloat($('sf-mult').value)||1;
  d.cycle_type=$('sf-cycle').value;d.cycle_count=parseInt($('sf-ccnt').value)||1;
  d.avg_enabled=$('sf-aen').classList.contains('active');d.avg_count=parseInt($('sf-acnt').value)||(isSpot?7:9);
  d.avg_total=parseFloat($('sf-atot').value)||(isSpot?1280:0);
  d.anti_waterfall=$('sf-awf').classList.contains('active');d.anti_waterfall_ratio=parseFloat($('sf-awfr').value)||2;
  d.avg_conditions=S.strategyForm.conditions;
  d.tp_mode=$('sf-tpm').value;d.tp_type=$('sf-tpt').value;
  d.tp_tiers=S.strategyForm.tpTiers;
  if(!isSpot){
    d.direction=$('sf-dir').value;d.leverage=parseFloat($('sf-lev').value)||10;
    d.entry_macd=$('sf-emacd').value;d.counter_ema=$('sf-cema').value;d.trend_ema=$('sf-tema').value;
    d.entry_doubling=$('sf-edouble').classList.contains('active');d.follow_trend=$('sf-ftrend').classList.contains('active');
    d.avg_macd=$('sf-amacd').value;d.avg_ema=$('sf-aema').value;
    d.reverse_tp=$('sf-rtp').value;d.reverse_sl=$('sf-rsl').classList.contains('active');
    d.sl_enabled=$('sf-slen').classList.contains('active');d.sl_type=$('sf-slt').value;d.sl_ratio=parseFloat($('sf-slr').value)||40;
    d.global_burn=$('sf-bg').classList.contains('active');d.counter_burn=$('sf-bc').classList.contains('active');
  }
  var body={category:c,strategy_type:d.strategy_type,coin:d.coin,config:d};
  if(!isSpot){body.direction=d.direction;body.leverage=d.leverage}
  var method,url;
  if(S.strategyForm.mode==='edit'){method='PUT';url='/api/strategies/configs/'+S.strategyForm.id}
  else{method='POST';url='/api/strategies/configs'}
  if(S.strategyForm.mode==='create')body.name=d.strategy_type+'_'+d.coin;
  var r=await fetch(url,{method:method,headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
  var result=await r.json();
  if(result.status==='ok'){
    if($('sf-save-tpl').checked){await saveTemplateFromForm(d)}
    toast(T('toast.saved'));
    closeStrategyForm();
    if(c==='spot')loadSpotList();else loadContractList();
  }else{toast(T('toast.error'),'err')}
}

async function saveTemplateFromForm(d){
  var name=prompt(T('tpl.name'),d.strategy_type+'_template');
  if(!name)return;
  var c=S.strategyForm.category;
  await fetch('/api/strategies/templates',{method:'POST',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({name:name,category:c,strategy_type:d.strategy_type,coin:d.coin,config:d,direction:d.direction||'long',leverage:d.leverage||1})});
  toast(T('toast.tpl_saved'));
}

// ── Conditions Modal ──
function openConditionsModal(){
  var isSpot=S.strategyForm.category==='spot',n=isSpot?7:9;
  if(!S.strategyForm.conditions.length){
    var defs=isSpot?[
      [1,1,3.5,0.3],[2,2,5,0.5],[3,4,7,0.5],[4,8,9,0.5],[5,16,11,0.5],[6,32,13,0.5],[7,64,15,0.5]
    ]:[
      [1,1,3,0.3],[2,2,4,0.5],[3,4,5,0.5],[4,4,7,0.5],[5,8,9,0.5],[6,8,10,0.5],[7,16,11,0.5],[8,16,12,0.5],[9,32,15,0.5]
    ];
    for(var i=0;i<defs.length;i++)S.strategyForm.conditions.push({order:i+1,mult:defs[i][1],gap:defs[i][2],callback:defs[i][3],ema:!isSpot});
  }
  renderConditionsModal();$('cond-modal').classList.add('show');
}

function renderConditionsModal(){
  var c=S.strategyForm.conditions,isSpot=S.strategyForm.category==='spot';
  var h='<h3>'+T(isSpot?'modal.avg_title_spot':'modal.avg_title_contract')+'</h3><table><thead><tr>';
  h+='<th>'+T('col.order')+'</th><th>'+T('col.mult')+'</th><th>'+T('col.gap')+'</th><th>'+T('col.callback')+'</th>';
  if(!isSpot)h+='<th>'+T('col.ema')+'</th>';
  h+='<th></th></tr></thead><tbody>';
  for(var i=0;i<c.length;i++){
    var o=c[i];
    h+='<tr><td>'+(i+1)+'</td>';
    h+='<td><input type="number" class="cond-mult" value="'+o.mult+'" step="0.1" style="width:60px"></td>';
    h+='<td><input type="number" class="cond-gap" value="'+o.gap+'" step="0.1" style="width:60px">%</td>';
    h+='<td><input type="number" class="cond-cb" value="'+o.callback+'" step="0.1" style="width:60px">%</td>';
    if(!isSpot)h+='<td><select class="cond-ema" style="width:60px"><option value="1"'+(o.ema?' selected':'')+'>开启</option><option value="0"'+(o.ema?'':' selected')+'>关闭</option></select></td>';
    h+='<td><button class="btn btn-xs btn-r" onclick="delCondRow('+i+')">'+T('btn.del_row')+'</button></td></tr>';
  }
  h+='</tbody></table><div class="btn-row"><button class="btn btn-xs" onclick="addCondRow()">'+T('btn.add_row')+'</button>';
  h+='<button class="btn btn-p btn-xs" onclick="saveConditionsModal()">'+T('btn.save')+'</button></div>';
  $('cond-content').innerHTML=h;
}

function addCondRow(){var n=S.strategyForm.conditions.length+1;S.strategyForm.conditions.push({order:n,mult:1,gap:3,callback:0.3});renderConditionsModal()}
function delCondRow(i){S.strategyForm.conditions.splice(i,1);for(var j=0;j<S.strategyForm.conditions.length;j++)S.strategyForm.conditions[j].order=j+1;renderConditionsModal()}
function saveConditionsModal(){
  var rows=document.querySelectorAll('#cond-content tbody tr');
  for(var i=0;i<rows.length;i++){
    var inps=rows[i].querySelectorAll('input,select');
    S.strategyForm.conditions[i]={order:i+1,mult:parseFloat(inps[0].value)||1,gap:parseFloat(inps[1].value)||0,callback:parseFloat(inps[2].value)||0};
    if(inps.length>3)S.strategyForm.conditions[i].ema=inps[3].value==='1';
  }
  S.strategyForm.conditions.length=rows.length;
  $('cond-modal').classList.remove('show');
}

// ── TP Settings Modal ──
function openTPSettingsModal(){
  if(!S.strategyForm.tpTiers.length){
    var defs=S.strategyForm.category==='spot'?[[1,2,20],[2,3,20],[3,4,10],[4,5,10]]:[[1,1.1,15],[2,2,10],[3,3,10],[4,4,10]];
    for(var i=0;i<defs.length;i++)S.strategyForm.tpTiers.push({tier:i+1,tp:defs[i][1],trail:defs[i][2]});
  }
  renderTPModal();$('tp-modal').classList.add('show');
}

function renderTPModal(){
  var t=S.strategyForm.tpTiers,h='<h3>'+T('modal.tp_title')+'</h3><table><thead><tr><th>'+T('col.tier')+'</th><th>'+T('col.tp')+'</th><th>'+T('col.trail')+'</th></tr></thead><tbody>';
  for(var i=0;i<t.length;i++){
    h+='<tr><td>'+(i+1)+'</td>';
    h+='<td><input type="number" class="tp-val" value="'+t[i].tp+'" step="0.1" style="width:60px">%</td>';
    h+='<td><input type="number" class="tp-trail" value="'+t[i].trail+'" step="1" style="width:60px">%</td></tr>';
  }
  h+='</tbody></table><div class="btn-row"><button class="btn btn-p btn-xs" onclick="saveTPModal()">'+T('btn.save')+'</button></div>';
  $('tp-content').innerHTML=h;
}

function saveTPModal(){
  var rows=document.querySelectorAll('#tp-content tbody tr');
  for(var i=0;i<rows.length;i++){
    var inps=rows[i].querySelectorAll('input');
    S.strategyForm.tpTiers[i]={tier:i+1,tp:parseFloat(inps[0].value)||0,trail:parseFloat(inps[1].value)||0};
  }
  $('tp-modal').classList.remove('show');
}

// ── Delete Confirmation ──
var deleteId=null;
function confirmDelete(id){deleteId=id;$('del-msg').textContent=T('modal.delete_msg');$('del-modal').classList.add('show')}
async function doDelete(){
  if(!deleteId)return;
  await fetch('/api/strategies/configs/'+deleteId,{method:'DELETE'});
  $('del-modal').classList.remove('show');deleteId=null;
  toast(T('toast.deleted'));
  if(S.subPage==='spot')loadSpotList();else loadContractList();
}

// ── Templates ──
async function loadTemplatesToDropdown(category){
  try{
    var r=await fetch('/api/strategies/templates?category='+category);var rows=await r.json();
    var sel=$('sf-template');if(!sel)return;
    sel.innerHTML='<option value="">-- '+T('form.template')+' --</option>';
    for(var i=0;i<rows.length;i++)sel.innerHTML+='<option value="'+rows[i].id+'">'+rows[i].name+'</option>';
  }catch(e){}
}

async function loadTemplateToForm(){
  var id=$('sf-template').value;if(!id)return;
  try{
    var r=await fetch('/api/strategies/configs/'+id);var o=await r.json();
    var cfg={};try{cfg=typeof o.config_json==='string'?JSON.parse(o.config_json):(o.config||{})}catch(e){}
    cfg.strategy_type=o.strategy_type;cfg.coin=o.coin;cfg.direction=o.direction;cfg.leverage=o.leverage;
    S.strategyForm.data=cfg;S.strategyForm.conditions=cfg.avg_conditions||[];S.strategyForm.tpTiers=cfg.tp_tiers||[];
    renderStrategyForm();
  }catch(e){}
}

async function loadTemplates(){
  try{
    var r=await fetch('/api/strategies/templates?category='+tplCategory);var rows=await r.json();
    S.cache.tplRows=rows;renderTplList(rows);
    document.querySelectorAll('#sub-templates .st-tab').forEach(function(t){t.classList.toggle('active',t.dataset.st===tplCategory)});
  }catch(e){}
}

function renderTplList(rows){
  var h='';
  for(var i=0;i<rows.length;i++){
    var o=rows[i];
    h+='<div class="tpl-card">';
    h+='<div class="tpl-name">'+o.name+'</div>';
    h+='<div class="tpl-meta">'+o.strategy_type+' | '+o.coin+'</div>';
    h+='<div class="tpl-meta">'+new Date(o.created_at).toLocaleString()+'</div>';
    h+='<div class="tpl-actions">';
    h+='<button class="btn btn-xs btn-g" onclick="applyTemplate(\''+o.id+'\')">'+T('btn.apply')+'</button>';
    h+='<button class="btn btn-xs btn-r" onclick="delTemplate(\''+o.id+'\')">'+T('btn.delete')+'</button>';
    h+='</div></div>';
  }
  $('tpl-list').innerHTML=h;
  $('tpl-empty').style.display=h?'none':'flex';
}

async function applyTemplate(id){
  try{
    var r=await fetch('/api/strategies/configs/'+id);var o=await r.json();
    var cfg={};try{cfg=typeof o.config_json==='string'?JSON.parse(o.config_json):(o.config||{})}catch(e){}
    cfg.strategy_type=o.strategy_type;cfg.coin=o.coin;
    S.strategyForm={mode:'create',id:null,data:cfg,conditions:cfg.avg_conditions||[],tpTiers:cfg.tp_tiers||[],category:o.category};
    renderStrategyForm();
    $('sf-modal').classList.add('show');
    loadTemplatesToDropdown(o.category);
  }catch(e){}
}

async function delTemplate(id){await fetch('/api/strategies/templates/'+id,{method:'DELETE'});loadTemplates()}
function showSaveTemplate(){
  var d=S.strategyForm.data,name=prompt(T('tpl.name'),(d.strategy_type||'strategy')+'_template');
  if(!name)return;
  saveTemplateFromForm(d);
}

// ── Logs ──
async function loadLogs(){
  try{
    var p=['limit=200'];
    var sid=$('log-strategy').value;if(sid)p.push('strategy_id='+sid);
    var lv=$('log-level').value;if(lv)p.push('level='+lv);
    var r=await fetch('/api/strategies/logs?'+p.join('&'));var rows=await r.json();
    S.cache.logRows=rows;renderLogList(rows);
    // Update strategy dropdown
    var cfgR=await fetch('/api/strategies/configs?limit=200');var cfgs=await cfgR.json();
    var sel='<option value="">'+T('log.all_strategies')+'</option>';
    for(var j=0;j<cfgs.length;j++)sel+='<option value="'+cfgs[j].id+'">'+cfgs[j].id+' - '+cfgs[j].coin+'</option>';
    $('log-strategy').innerHTML=sel;
  }catch(e){}
}

function renderLogList(rows){
  var h='';
  for(var i=0;i<rows.length;i++){
    var o=rows[i],lvClass=o.level==='ERROR'?'r':(o.level==='WARN'?'y':'m');
    h+='<tr><td style="font-size:9px">'+new Date(o.timestamp).toLocaleString()+'</td>';
    h+='<td style="font-size:8px">'+(o.strategy_id||'')+'</td>';
    h+='<td class="'+lvClass+'">'+o.level+'</td>';
    h+='<td>'+o.message+'</td></tr>';
  }
  $('log-tbody').innerHTML=h;
  $('log-empty').style.display=h?'none':'flex';
}

function toggleLogAuto(){if($('log-auto').checked)startLogAuto();else stopLogAuto()}
function startLogAuto(){stopLogAuto();logTimer=setInterval(loadLogs,8000)}
function stopLogAuto(){if(logTimer){clearInterval(logTimer);logTimer=null}}
async function clearLogs(){
  var sid=$('log-strategy').value;
  await fetch('/api/strategies/logs'+(sid?'?strategy_id='+sid:''),{method:'DELETE'});
  loadLogs();toast('Cleared');
}

// ── Old strategy compat ──
function updateStList(strats){
  var h='';
  for(var name in strats){
    var s=strats[name];
    h+='<div style="display:flex;align-items:center;padding:8px 0;border-bottom:1px solid rgba(48,54,61,.3)">';
    h+='<span style="flex:1;font-weight:600">'+name+'</span>';
    h+='<span class="'+(s.running?'g':'m')+'" style="font-size:9px;margin-right:6px">'+(s.running?'RUNNING':'STOPPED')+'</span></div>';
  }
}

function showStDetail(name){}
function toggleSt(name,running){}
async function refreshStrategies(){
  loadGlobalSettings();
  if(S.subPage==='spot')loadSpotList();
  else if(S.subPage==='contract')loadContractList();
  else if(S.subPage==='templates')loadTemplates();
  else if(S.subPage==='logs')loadLogs();
}

// ═══════════════════════════════════════════════ SETTINGS ═══════════════════════════════════════════
var EXCH_IDS={binance:'bn',okx:'ok',coinbase:'cb',kucoin:'kc',bybit:'by',gate:'gt',htx:'ht',mexc:'me',zb:'zb',bitget:'bg',phemex:'pm',deribit:'dr'};
var AI_IDS={openai:'ai-openai',qwen:'ai-qwen',anthropic:'ai-anthropic',google:'ai-google',deepseek:'ai-deepseek',doubao:'ai-doubao',baidu:'ai-baidu',hunyuan:'ai-hunyuan',mistral:'ai-mistral',zhipu:'ai-zhipu',yi:'ai-yi',local:'ai-local'};

function toggleSecret(id,btn){
  var el=$(id);if(!el)return;
  if(el.type==='password'){el.type='text';btn.innerHTML='&#128064;';}
  else{el.type='password';btn.innerHTML='&#128065;';}
}

function setCardStatus(cardId,status){
  var el=$(cardId);if(!el)return;
  el.textContent=status.text;
  el.className='scard-status '+status.css;
}

function elVal(id,def){var e=$(id);return e?e.value:(def||'');}
function setEl(id,val,isSelect){var e=$(id);if(!e)return;if(isSelect){e.value=val}else{e.value=val}}
function escHtml(s){var d=document.createElement('div');d.textContent=s;return d.innerHTML;}

function switchSettingsTab(tab){
  S.settingsTab=tab;
  document.querySelectorAll('.stab-item').forEach(function(t){t.classList.toggle('active',t.dataset.stab===tab);});
  document.querySelectorAll('.stab-content').forEach(function(c){c.classList.toggle('active',c.id==='stab-'+tab);});
}
async function loadSettings(){
  try{
    var r=await fetch('/api/config');var c=await r.json();
    // Exchanges
    Object.keys(EXCH_IDS).forEach(function(name){
      var pf=EXCH_IDS[name];var ex=c[name]||(c.exchanges||{})[name]||{};
      if(!pf)return;
      setEl('s-'+pf+'-key',ex.api_key||'');
      setEl('s-'+pf+'-secret',ex.secret||'');
      setEl('s-'+pf+'-pass',ex.passphrase||'');
      setEl('s-'+pf+'-tradetype',ex.futures?'futures':'spot',true);
      var tnEl=$('s-'+pf+'-testnet');
      var tnTg=$('s-'+pf+'-testnet-toggle');
      if(tnEl)tnEl.value=ex.testnet!==false?'1':'0';
      if(tnTg)tnTg.classList.toggle('active',ex.testnet!==false);
      if(ex.api_key&&ex.secret)setCardStatus('s-'+pf+'-status',{text:T('settings.connected'),css:'ok'});
      else setCardStatus('s-'+pf+'-status',{text:T('settings.not_connected'),css:'err'});
    });
    // AI Providers
    var aiCfg=c.ai||{};
    Object.keys(AI_IDS).forEach(function(name){
      var pf=AI_IDS[name];var ai=aiCfg[name]||{};
      if(!pf)return;
      var keyEl=$('s-'+pf+'-key');
      var urlEl=$('s-'+pf+'-url');
      var modelEl=$('s-'+pf+'-model');
      if(keyEl)keyEl.value=ai.api_key||'';
      if(urlEl&&ai.base_url)urlEl.value=ai.base_url;
      if(modelEl&&ai.model)modelEl.value=ai.model;
      if(ai.api_key)setCardStatus('s-'+pf+'-status',{text:T('settings.configured'),css:'ok'});
      else setCardStatus('s-'+pf+'-status',{text:T('settings.not_configured'),css:'err'});
    });
    // Default provider
    var defProv=c.default_ai_provider||'';
    var defEl=$('ai-default-provider');
    if(defEl&&defProv)defEl.value=defProv;
    // Default exchange
    var defExch=c.default_exchange||'';
    var defExchEl=$('exchange-default');
    if(defExchEl&&defExch)defExchEl.value=defExch;
    // Agent tokens + AI config
    loadAgentAIConfig();
  }catch(e){console.error('loadSettings failed:',e);}
}

async function saveExchange(name){
  var pf=EXCH_IDS[name];if(!pf)return;
  var msgEl=$('s-'+pf+'-msg');msgEl.textContent=T('settings.saving');msgEl.className='sc-msg';
  var d={name:name,api_key:elVal('s-'+pf+'-key').trim(),secret:elVal('s-'+pf+'-secret').trim(),
    passphrase:elVal('s-'+pf+'-pass').trim(),
    testnet:elVal('s-'+pf+'-testnet')==='1',
    futures:elVal('s-'+pf+'-tradetype')==='futures'};
  try{
    var r=await fetch('/api/exchange/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(d)});
    if(r.ok){
      msgEl.textContent=T('toast.saved');msgEl.className='sc-msg ok';
      setCardStatus('s-'+pf+'-status',{text:T('settings.connected'),css:'ok'});
      if(name==='binance'||name==='okx'){var rstCfg={exchanges:{}};rstCfg.exchanges[name]={api_key:d.api_key,secret:d.secret,testnet:d.testnet,futures:d.futures,enabled:!!d.api_key};if(d.passphrase)rstCfg.exchanges[name].passphrase=d.passphrase;await fetch('/api/restart',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(rstCfg)});}
    }else{var e=await r.json();msgEl.textContent=e.detail||'Failed';msgEl.className='sc-msg err'}
  }catch(e){msgEl.textContent='Error';msgEl.className='sc-msg err'}
}

async function testExchange(name){
  var pf=EXCH_IDS[name];if(!pf)return;
  var msgEl=$('s-'+pf+'-msg');msgEl.textContent=T('settings.testing');msgEl.className='sc-msg';
  setCardStatus('s-'+pf+'-status',{text:T('settings.testing'),css:'testing'});
  var d={name:name,api_key:elVal('s-'+pf+'-key').trim(),secret:elVal('s-'+pf+'-secret').trim(),
    passphrase:elVal('s-'+pf+'-pass').trim(),
    testnet:elVal('s-'+pf+'-testnet')==='1',
    futures:elVal('s-'+pf+'-tradetype')==='futures'};
  try{
    var r=await fetch('/api/exchange/test',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(d)});
    var j=await r.json();
    if(r.ok&&j.status==='ok'){
      msgEl.textContent=T('settings.conn_ok');msgEl.className='sc-msg ok';
      setCardStatus('s-'+pf+'-status',{text:T('settings.connected'),css:'ok'});
    }else{
      msgEl.textContent=j.detail||T('settings.conn_fail');msgEl.className='sc-msg err';
      setCardStatus('s-'+pf+'-status',{text:T('settings.conn_fail'),css:'err'});
    }
  }catch(e){msgEl.textContent='Error';msgEl.className='sc-msg err'}
}

async function saveAI(name){
  var pf=AI_IDS[name];if(!pf)return;
  var msgEl=$('s-'+pf+'-msg');msgEl.textContent=T('settings.saving');msgEl.className='sc-msg';
  var d={provider:name,api_key:elVal('s-'+pf+'-key').trim(),base_url:elVal('s-'+pf+'-url').trim(),
    model:elVal('s-'+pf+'-model')};
  try{
    var r=await fetch('/api/ai/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(d)});
    if(r.ok){
      msgEl.textContent=T('toast.saved');msgEl.className='sc-msg ok';
      setCardStatus('s-'+pf+'-status',{text:T('settings.configured'),css:'ok'});
      // Auto-set as default provider if none set
      var defEl=$('ai-default-provider');
      if(defEl&&!defEl.value){defEl.value=name;saveDefaultAI();}
    }else{var e=await r.json();msgEl.textContent=e.detail||'Failed';msgEl.className='sc-msg err'}
  }catch(e){msgEl.textContent='Error';msgEl.className='sc-msg err'}
}

async function testAI(name){
  var pf=AI_IDS[name];if(!pf)return;
  var msgEl=$('s-'+pf+'-msg');msgEl.textContent=T('settings.testing');msgEl.className='sc-msg';
  setCardStatus('s-'+pf+'-status',{text:T('settings.testing'),css:'testing'});
  var d={provider:name,api_key:$('s-'+pf+'-key').value.trim(),base_url:$('s-'+pf+'-url').value.trim(),
    model:$('s-'+pf+'-model').value};
  try{
    var r=await fetch('/api/ai/test',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(d)});
    var j=await r.json();
    if(r.ok&&j.status==='ok'){
      msgEl.textContent=T('settings.conn_ok');msgEl.className='sc-msg ok';
      setCardStatus('s-'+pf+'-status',{text:T('settings.connected'),css:'ok'});
    }else{
      msgEl.textContent=j.detail||T('settings.conn_fail');msgEl.className='sc-msg err';
      setCardStatus('s-'+pf+'-status',{text:T('settings.conn_fail'),css:'err'});
    }
  }catch(e){msgEl.textContent='Error';msgEl.className='sc-msg err'}
}

async function saveDefaultAI(){
  var prov=$('ai-default-provider').value;
  try{
    await fetch('/api/ai/default',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({provider:prov})});
  }catch(e){}
}

async function saveDefaultExchange(){
  var exch=$('exchange-default').value;
  try{
    await fetch('/api/exchange/default',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({exchange:exch})});
  }catch(e){}
}

// ═══════════════════════════════════════════════ AGENT CC SWITCH ═══════════════════════════════════════
var _CCS_PROVIDERS={};

function onCCSwitchModelChange(){
  var prov=elVal('ag-ccs-model');
  var cfg=_CCS_PROVIDERS[prov];
  if(cfg){
    setEl('ag-ccs-url',cfg.base_url);
    setEl('ag-ccs-key',cfg.api_key||'');
  }else if(prov==='custom'){
    setEl('ag-ccs-url','');
    setEl('ag-ccs-key','');
  }
}

async function ccSwitchConfigure(){
  var modelSel=elVal('ag-ccs-model');
  var cfg=_CCS_PROVIDERS[modelSel];
  var targetModel=cfg?cfg.model:'';
  var r=await fetch('/api/agent/cc-switch/configure',{method:'POST',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({
      target_url:elVal('ag-ccs-url').trim(),
      target_model:targetModel,
      api_key:elVal('ag-ccs-key').trim(),
      port:parseInt(elVal('ag-ccs-port'))||12435
    })
  });
  return await r.json();
}

async function ccSwitchStart(){
  await ccSwitchConfigure();
  var r=await fetch('/api/agent/cc-switch/start',{method:'POST'});
  var j=await r.json();
  updateCCSwitchUI(j);
  if(j.status==='started'||j.status==='already_running')toast('CC Switch 已启动');
  else toast('启动失败: '+(j.error||''),'err');
}

async function ccSwitchStop(){
  var r=await fetch('/api/agent/cc-switch/stop',{method:'POST'});
  var j=await r.json();
  updateCCSwitchUI(j);
  toast('CC Switch 已停止');
}

function updateCCSwitchUI(s){
  var running=s.running===true;
  $('ag-ccs-status').textContent=running?'运行中':'已停止';
  $('ag-ccs-status').style.color=running?'var(--green)':'var(--muted)';
  $('ag-ccs-start-btn').style.display=running?'none':'';
  $('ag-ccs-stop-btn').style.display=running?'':'none';
  var addr='http://127.0.0.1:'+(s.port||12435);
  $('ag-ccs-addr').textContent=addr;
  var el;
  el=$('ag-cursor-addr');if(el)el.textContent=addr+'/v1';
  el=$('ag-codex-addr');if(el)el.textContent=addr+'/v1';
}

async function ccSwitchRefresh(){
  try{
    var r=await fetch('/api/agent/cc-switch');
    updateCCSwitchUI(await r.json());
  }catch(e){}
}

async function loadCCSProviders(){
  try{
    var r=await fetch('/api/config');
    var cfg=await r.json();
    var aiCfg=cfg.ai||{};
    _CCS_PROVIDERS={};
    var sel=$('ag-ccs-model');
    if(!sel)return;
    sel.innerHTML='';
    var hasAny=false;
    Object.keys(aiCfg).forEach(function(name){
      var p=aiCfg[name];
      if(p.api_key){
        _CCS_PROVIDERS[name]=p;
        var label=name+' ('+(p.model||'')+')';
        sel.innerHTML+='<option value="'+name+'">'+label+'</option>';
        hasAny=true;
      }
    });
    if(!hasAny)sel.innerHTML+='<option value="">无已配置的模型</option>';
    sel.innerHTML+='<option value="custom">自定义</option>';

    // Pre-select from saved agent config
    var r2=await fetch('/api/agent/ai-config');
    var agentCfg=await r2.json();
    if(agentCfg.provider&&_CCS_PROVIDERS[agentCfg.provider]){
      setEl('ag-ccs-model',agentCfg.provider,true);
      onCCSwitchModelChange();
    }else if(agentCfg.api_key){
      // Auto-fill key even if provider not in dropdown
      setEl('ag-ccs-key',agentCfg.api_key);
      if(agentCfg.base_url)setEl('ag-ccs-url',agentCfg.base_url);
    }
  }catch(e){console.error('loadCCSProviders:',e);}
}

function toggleAgentTool(tool){
  var el=$('ag-'+tool+'-config');
  var tgl=$('ag-'+tool+'-toggle');
  if(!el)return;
  var on=el.style.display==='none'||!el.style.display;
  el.style.display=on?'block':'none';
  tgl.classList.toggle('active',on);
}

function toggleMCPTools(){
  var el=$('ag-tools-list'),btn=$('ag-tools-toggle');
  if(el.style.display==='none'||!el.style.display){el.style.display='block';btn.textContent='收起';}
  else{el.style.display='none';btn.textContent='展开';}
}

// Init
(function(){
  ccSwitchRefresh();
  setInterval(ccSwitchRefresh,5000);
  loadCCSProviders();
})();

// ═══════════════════════════════════════════════ AGENT AI CONFIG ═══════════════════════════════════════
var AGENT_AI_DEFAULTS={
  openai:'https://api.openai.com/v1',
  qwen:'https://dashscope.aliyuncs.com/compatible-mode/v1',
  anthropic:'https://api.anthropic.com/v1',
  google:'https://generativelanguage.googleapis.com/v1beta',
  deepseek:'https://api.deepseek.com/v1',
  doubao:'https://ark.cn-beijing.volces.com/api/v3',
  baidu:'https://aip.baidubce.com/rpc/2.0/ai_custom/v1/wenxinworkshop/chat',
  hunyuan:'https://api.hunyuan.cloud.tencent.com/v1',
  mistral:'https://api.mistral.ai/v1',
  zhipu:'https://open.bigmodel.cn/api/paas/v4',
  yi:'https://api.lingyiwanwu.com/v1',
  local:'http://localhost:11434/v1'
};

function onAgentAIProviderChange(){
  var prov=$('s-agent-ai-provider').value;
  if(prov&&AGENT_AI_DEFAULTS[prov]){
    var urlEl=$('s-agent-ai-url');
    if(urlEl&&!urlEl.value)urlEl.value=AGENT_AI_DEFAULTS[prov];
  }
}

function toggleAgentProxy(){
  var tgl=$('s-agent-ai-proxy-toggle');
  var inp=$('s-agent-ai-proxy-enabled');
  var rows=$('ag-proxy-rows');
  var on=!tgl.classList.contains('active');
  tgl.classList.toggle('active',on);
  inp.value=on?'1':'0';
  if(rows)rows.style.display=on?'block':'none';
}

async function loadAgentAIConfig(){
  try{
    var r=await fetch('/api/agent/ai-config');
    var c=await r.json();
    setEl('s-agent-ai-provider',c.provider||'',true);
    setEl('s-agent-ai-key',c.api_key||'');
    setEl('s-agent-ai-url',c.base_url||'');
    setEl('s-agent-ai-model',c.model||'');
    var proxyOn=c.proxy_enabled===true;
    var tgl=$('s-agent-ai-proxy-toggle');
    var inp=$('s-agent-ai-proxy-enabled');
    var rows=$('ag-proxy-rows');
    if(tgl)tgl.classList.toggle('active',proxyOn);
    if(inp)inp.value=proxyOn?'1':'0';
    if(rows)rows.style.display=proxyOn?'block':'none';
    setEl('s-agent-ai-http-proxy',c.http_proxy||'');
    setEl('s-agent-ai-https-proxy',c.https_proxy||'');
  }catch(e){}
}

async function saveAgentAIConfig(){
  var msgEl=$('s-agent-ai-msg');msgEl.textContent=T('settings.saving');msgEl.className='sc-msg';
  var d={
    provider:elVal('s-agent-ai-provider'),
    api_key:elVal('s-agent-ai-key').trim(),
    base_url:elVal('s-agent-ai-url').trim(),
    model:elVal('s-agent-ai-model').trim(),
    proxy_enabled:elVal('s-agent-ai-proxy-enabled')==='1',
    http_proxy:elVal('s-agent-ai-http-proxy').trim(),
    https_proxy:elVal('s-agent-ai-https-proxy').trim()
  };
  try{
    var r=await fetch('/api/agent/ai-config',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(d)});
    if(r.ok){
      msgEl.textContent=T('toast.saved');msgEl.className='sc-msg ok';
    }else{var e=await r.json();msgEl.textContent=e.detail||'Failed';msgEl.className='sc-msg err';}
  }catch(e){msgEl.textContent='Error';msgEl.className='sc-msg err';}
}

async function testAgentAI(){
  var msgEl=$('s-agent-ai-msg');msgEl.textContent=T('settings.testing');msgEl.className='sc-msg';
  var d={
    provider:elVal('s-agent-ai-provider'),
    api_key:elVal('s-agent-ai-key').trim(),
    base_url:elVal('s-agent-ai-url').trim(),
    model:elVal('s-agent-ai-model').trim(),
    proxy_enabled:elVal('s-agent-ai-proxy-enabled')==='1',
    http_proxy:elVal('s-agent-ai-http-proxy').trim(),
    https_proxy:elVal('s-agent-ai-https-proxy').trim()
  };
  try{
    var r=await fetch('/api/agent/ai-test',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(d)});
    var j=await r.json();
    if(r.ok&&j.status==='ok'){
      msgEl.textContent=T('settings.conn_ok');msgEl.className='sc-msg ok';
    }else{
      msgEl.textContent=j.detail||T('settings.conn_fail');msgEl.className='sc-msg err';
    }
  }catch(e){msgEl.textContent='Error';msgEl.className='sc-msg err';}
}

// ═══════════════════════════════════════════════ AI STRATEGY GENERATOR ═══════════════════════════════
var S_aiMode='quick'; // quick | advanced | optimize | combo | multi

function selectRisk(risk){
  $('ai-risk-val').value=risk;
  document.querySelectorAll('.risk-card').forEach(function(c){c.classList.toggle('active',c.dataset.risk===risk)});
  updateRiskPreview(risk);
  updateAIWizard();
}

function updateRiskPreview(risk){
  var bars={
    low: {win:'55%',dd:'15%',ret:'30%',wl:'60-70%',dl:'3-5%',rl:'10-20%'},
    medium:{win:'40%',dd:'22%',ret:'45%',wl:'55-65%',dl:'10-15%',rl:'20-40%'},
    high:{win:'30%',dd:'30%',ret:'55%',wl:'45-55%',dl:'20-30%',rl:'40%+'}
  };
  var b=bars[risk]||bars.medium;
  var rows=$('ai-risk-preview').querySelectorAll('.risk-bar-row');
  if(rows.length>=3){
    rows[0].querySelector('.risk-bar-fill').style.width=b.win;
    rows[0].querySelector('.risk-bar-val').textContent=b.wl;
    rows[1].querySelector('.risk-bar-fill').style.width=b.dd;
    rows[1].querySelector('.risk-bar-val').textContent=b.dl;
    rows[2].querySelector('.risk-bar-fill').style.width=b.ret;
    rows[2].querySelector('.risk-bar-val').textContent=b.rl;
  }
}

function applyStrategyTemplate(){
  var tpl=$('ai-template').value;
  var prompts={
    trend:'【开仓条件】：当MA5上穿MA20且成交量放大2倍时开多\n【平仓条件】：下穿MA20平仓\n【止损止盈规则】：最大回撤控制在8%\n【特殊限制】：',
    oscillation:'【开仓条件】：当RSI低于25且布林带下轨时买入\n【平仓条件】：RSI高于75且上轨时卖出\n【止损止盈规则】：网格间距2%\n【特殊限制】：',
    breakout:'【开仓条件】：价格突破近20根K线高点+成交量放大1.5倍开多\n【平仓条件】：跌破前低止损\n【止损止盈规则】：止盈风险比1:2\n【特殊限制】：'
  };
  if(prompts[tpl]){$('ai-prompt').value=prompts[tpl];}
  else{$('ai-prompt').value='';}
  updateCharCount();
  updateAIWizard();
}

function setGenMode(mode){
  S_aiMode=mode;
  document.querySelectorAll('.ai-mode-btn').forEach(function(b){b.classList.toggle('active',b.dataset.mode===mode)});
  var mp=$('ai-multi-progress'), mi=$('ai-agent-insights');
  if(mode==='multi'){if(mp)mp.style.display='block';if(mi)mi.style.display='block';}
  else{if(mp)mp.style.display='none';if(mi)mi.style.display='none';}
}

function updateCharCount(){
  var len=$('ai-prompt').value.length;
  var el=$('ai-char-count');
  if(el)el.textContent='已输入 '+len+' 字'+(len<50?'，建议 50 字以上':'');
}

function updateAIWizard(){
  var symbol=$('ai-symbol').value;
  var risk=$('ai-risk-val').value;
  var promptLen=$('ai-prompt').value.length;
  var step=1;
  if(symbol)step=2;
  if(risk)step=3;
  if(promptLen>=50)step=4;
  for(var i=1;i<=4;i++){
    var ws=$('ai-ws-'+i);
    if(!ws)continue;
    ws.classList.remove('active','done');
    if(i<step)ws.classList.add('done');
    if(i===step)ws.classList.add('active');
  }
}

function copyCode(){
  var code=$('ai-code-display').textContent;
  if(!code||code.indexOf('点击')>=0){toast('没有可复制的代码','err');return}
  navigator.clipboard.writeText(code).then(function(){toast('代码已复制')},function(){toast('复制失败','err')});
}

function downloadCode(){
  var code=$('ai-code-display').textContent;
  if(!code||code.indexOf('点击')>=0){toast('没有可下载的代码','err');return}
  var name=(S.aiStrategy&&S.aiStrategy.strategy_name)||'strategy';
  var blob=new Blob([code],{type:'text/x-python'});
  var a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download=name+'.py';a.click();
}

function toggleCodeView(){
  var sec=$('ai-code-section');
  if(sec)sec.style.display=sec.style.display==='none'?'flex':'none';
}

var S_pendingBacktest=null;
function runBacktest(){
  if(S_pendingBacktest){doBacktest(S_pendingBacktest);S_pendingBacktest=null;}
}

async function doBacktest(strategyData){
  $('ai-gen-status').textContent='正在运行回测...';
  $('ai-results').style.display='none';
  try{
    var symbol=$('ai-symbol').value;
    var r2=await fetch('/api/ai/backtest',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({
      strategy_code:strategyData.strategy_code,
      config:{symbol:symbol,initial_balance:parseFloat($('ai-capital').value)||100000,num_bars:parseInt($('ai-bars').value)||500,base_price:symbol==='BTCUSDT'?68000:symbol==='ETHUSDT'?3500:symbol==='BNBUSDT'?600:150,params:{},strategy_name:strategyData.strategy_name}
    })});
    var d2=await r2.json();
    if(r2.status!==200){$('ai-gen-status').textContent='Backtest error: '+d2.detail;return}
    $('ai-gen-status').textContent='';
    renderBacktestResults(d2);
    toast('Agent策略生成完毕，回测完成');
  }catch(e){$('ai-gen-status').textContent='Error: '+e.message;}
}

function initAIChart(){
  if(S.aiChart)return;
  var el=$('ai-chart');
  if(!el||!el.clientHeight)return;
  S.aiChart=LightweightCharts.createChart(el,{
    layout:{background:{color:'#1f2328fff'},textColor:'#1f2328'},
    grid:{vertLines:{color:'rgba(48,54,61,.3)'},horzLines:{color:'rgba(48,54,61,.3)'}},
    rightPriceScale:{borderColor:'#d0d7de'},timeScale:{borderColor:'#d0d7de',timeVisible:true},
  });
  S.aiSeries=S.aiChart.addAreaSeries({topColor:'rgba(63,185,80,.3)',bottomColor:'rgba(63,185,80,.01)',lineColor:'#3fb950',lineWidth:2});
}

function renderBacktestResults(d2){
  S.aiReport=d2;
  $('ai-results').style.display='flex';
  initAIChart();
  if(S.aiSeries){
    var eqData=[];
    if(d2.equity_curve)for(var i=0;i<d2.equity_curve.length;i++){
      var p=d2.equity_curve[i];
      eqData.push({time:Date.now()/1000-(d2.equity_curve.length-i)*60,value:p.equity});
    }
    S.aiSeries.setData(eqData);S.aiChart.timeScale().fitContent();
  }
  var rep=d2.report||{};
  var metrics=[
    ['Total Return',FP(rep.total_return_pct)],['Annual Return',FP(rep.annual_return_pct)],
    ['Sharpe',F(rep.sharpe_ratio,3)],['Sortino',F(rep.sortino_ratio,3)],
    ['Max DD',FP(rep.max_drawdown_pct)],['Win Rate',FP(rep.win_rate_pct)],
    ['Profit Factor',F(rep.profit_factor,3)],['Total Trades',rep.total_trades||0]
  ];
  var mh='';
  for(var i=0;i<metrics.length;i++){
    mh+='<div style="background:var(--bg);padding:6px 8px;border-radius:5px"><div class="f-label">'+metrics[i][0]+'</div><div style="font-size:14px;font-weight:700">'+metrics[i][1]+'</div></div>';
  }
  $('ai-metrics').innerHTML=mh;
  $('ai-deploy-btn').disabled=false;$('ai-deploy-btn').textContent=T('ai.deploy');
}

async function generateAIStrategy(){
  var btn=$('ai-gen-btn');btn.disabled=true;
  btn.innerHTML='<span class="ai-spinner"></span>'+T('ai.generating');
  $('ai-gen-status').textContent=S_aiMode==='multi'?'正在启动7-Agent协作管道...':'正在调用AI生成策略...';
  $('ai-results').style.display='none';
  $('ai-preview-card').style.display='none';
  $('ai-code-section').style.display='flex';

  var prompt=$('ai-prompt').value.trim();
  if(!prompt){$('ai-gen-status').textContent='策略描述不清晰，请补充开仓/平仓条件';btn.disabled=false;btn.innerHTML=T('ai.generate');return}

  var risk=$('ai-risk-val').value||'medium';
  var symbol=$('ai-symbol').value;
  var interval=$('ai-interval').value;

  try{
    // Multi-agent mode: use /api/ai/multi-agent
    if(S_aiMode==='multi'){
      updateAgentChip('ac-tech','active');
      var r=await fetch('/api/ai/multi-agent',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({
        symbol:symbol,interval:interval,risk:risk,prompt:prompt,mode:S_aiMode
      })});
      var d=await r.json();
      if(r.status!==200){$('ai-gen-status').textContent='Error: '+d.detail;btn.disabled=false;btn.innerHTML=T('ai.generate');return}

      // Mark all agents done
      ['tech','onchain','sent','risk','bull','bear','trader','done'].forEach(function(id){
        updateAgentChip('ac-'+id,'done');
      });

      S.aiStrategy=d;
      $('ai-code-display').textContent=d.strategy_code;
      $('ai-code-name').textContent=d.strategy_name;

      // Render agent insights
      if(d.agents){
        renderAgentInsights(d.agents, d.debate_summary);
      }

      $('ai-gen-status').textContent=d.status==='ok'?'7-Agent协作完成，正在运行回测...':'Agent协作完成(有警告)，正在运行回测...';
    } else {
      // Original single-agent mode
      var defProv=$('ai-default-provider');var provider=defProv?defProv.value:'';
      var r=await fetch('/api/ai/generate',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({
        symbol:symbol,interval:interval,risk:risk,prompt:prompt,provider:provider,mode:S_aiMode
      })});
      var d=await r.json();
      if(r.status!==200){$('ai-gen-status').textContent='Error: '+d.detail;btn.disabled=false;btn.innerHTML=T('ai.generate');return}
      S.aiStrategy=d;
      $('ai-code-display').textContent=d.strategy_code;
      $('ai-code-name').textContent=d.strategy_name;
    }

    // Show preview card
    $('ai-preview-card').style.display='block';
    $('ai-preview-name').textContent=d.strategy_name;
    $('ai-preview-summary').textContent=d.description||(d.strategy_code||'').split('\n').filter(function(l){return l.trim()&&l.indexOf('def ')>=0})[0]||'';
    $('ai-code-section').style.display='flex';

    if(d.status==='warning'){$('ai-gen-status').textContent=d.message}
    else if(S_aiMode!=='multi'){$('ai-gen-status').textContent='策略代码已生成，正在运行回测...'}

    // Run backtest
    S_pendingBacktest=d;
    await doBacktest(d);
  }catch(e){
    $('ai-gen-status').textContent='Error: '+e.message;
  }
  btn.disabled=false;btn.innerHTML=T('ai.generate');
}

function updateAgentChip(id, state){
  var el=$(id);if(!el)return;
  el.classList.remove('active','done','error');
  if(state==='active')el.classList.add('active');
  else if(state==='done')el.classList.add('done');
  else if(state==='error')el.classList.add('error');
}

function renderAgentInsights(agents, debateSummary){
  var mi=$('ai-agent-insights');if(mi)mi.style.display='block';
  var content=$('ai-insights-content');if(!content)return;
  var html='';
  var labels={technical:'技术分析师',onchain:'链上分析师',sentiment:'情绪分析师',risk:'风险评估师',bull:'多头辩论',bear:'空头辩论'};
  Object.keys(labels).forEach(function(key){
    var text=agents[key]||'';
    if(text&&text.indexOf('[Error')!==0&&text.length>10){
      html+='<details style="margin-bottom:6px"><summary style="cursor:pointer;font-weight:600;font-size:11px;color:var(--accent)">'+labels[key]+'</summary>';
      html+='<div style="font-size:10px;color:var(--muted);padding:6px 12px;white-space:pre-wrap;line-height:1.5">'+escHtml(text)+'</div></details>';
    }
  });
  if(debateSummary&&debateSummary.length>20){
    html+='<details open style="margin-bottom:6px"><summary style="cursor:pointer;font-weight:600;font-size:11px;color:var(--orange)">辩论总结</summary>';
    html+='<div style="font-size:10px;color:var(--muted);padding:6px 12px;white-space:pre-wrap;line-height:1.5">'+escHtml(debateSummary)+'</div></details>';
  }
  content.innerHTML=html||'<div style="font-size:10px;color:var(--muted)">等待Agent分析结果...</div>';
}

function regenerateAI(){
  $('ai-prompt').value='';
  $('ai-gen-status').textContent='';
  $('ai-results').style.display='none';
  $('ai-preview-card').style.display='none';
  $('ai-code-section').style.display='flex';
  $('ai-code-display').textContent='';
  $('ai-code-name').textContent='';
  S.aiStrategy=null;S.aiReport=null;S_pendingBacktest=null;
  if(S.aiSeries)S.aiSeries.setData([]);
  $('ai-metrics').innerHTML='';
}

async function deployAIStrategy(){
  if(!S.aiStrategy){toast('没有可部署的策略','err');return}
  var ok=confirm('确认部署 "'+(S.aiStrategy.strategy_name||'Agent Strategy')+'" 到策略模板库吗？\n\n部署后可在"策略模板管理"页面使用此策略。');
  if(!ok)return;

  try{
    var r=await fetch('/api/ai/deploy',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({
      strategy_name:S.aiStrategy.strategy_name,
      strategy_code:S.aiStrategy.strategy_code,
      description:S.aiStrategy.description,
      symbol:$('ai-symbol').value,
      risk:$('ai-risk-val').value||'medium',
      backtest_report:S.aiReport||{}
    })});
    var d=await r.json();
    if(d.status==='ok'){
      toast('策略已部署到模板库! ID: '+d.id);
      $('ai-deploy-btn').disabled=true;$('ai-deploy-btn').textContent='已部署';
    }else{toast('部署失败','err')}
  }catch(e){toast('Error: '+e.message,'err')}
}

// ═══════════════════════════════════════════════ NATIVE PYTHON STRATEGY ═══════════════════════════════
var S_nativeMode='indicator';
var S_nativeStrategy=null;
var S_nativeReport=null;
var S_nativeChart=null;
var S_nativeSeries=null;

function switchNativeMode(mode){
  S_nativeMode=mode;
  document.querySelectorAll('.native-mode-tab').forEach(function(t){t.classList.toggle('active',t.dataset.mode===mode)});
  $('native-indicator-panel').style.display=mode==='indicator'?'block':'none';
  $('native-script-panel').style.display=mode==='script'?'block':'none';
  if(mode==='script')loadScriptTemplate('indicator');
  if(mode==='indicator')updateNativeCode();
}

function updateNativeEntryParams(){
  var ind=$('native-entry-ind').value;
  var html='';
  if(ind==='ma_cross'){
    html+='<div class="native-param-row"><label>快线</label><input id="nep-ma-fast" value="5" type="number"></div>';
    html+='<div class="native-param-row"><label>慢线</label><input id="nep-ma-slow" value="20" type="number"></div>';
  }else if(ind==='rsi'){
    html+='<div class="native-param-row"><label>周期</label><input id="nep-rsi-period" value="14" type="number"></div>';
    html+='<div class="native-param-row"><label>超卖</label><input id="nep-rsi-low" value="30" type="number"></div>';
    html+='<div class="native-param-row"><label>超买</label><input id="nep-rsi-high" value="70" type="number"></div>';
  }else if(ind==='macd'){
    html+='<div class="native-param-row"><label>快线</label><input id="nep-macd-fast" value="12" type="number"></div>';
    html+='<div class="native-param-row"><label>慢线</label><input id="nep-macd-slow" value="26" type="number"></div>';
    html+='<div class="native-param-row"><label>信号</label><input id="nep-macd-sig" value="9" type="number"></div>';
  }else if(ind==='boll'){
    html+='<div class="native-param-row"><label>周期</label><input id="nep-boll-period" value="20" type="number"></div>';
    html+='<div class="native-param-row"><label>标准差</label><input id="nep-boll-std" value="2.0" type="number" step="0.1"></div>';
  }
  $('native-entry-params').innerHTML=html;
}

function updateNativeExitParams(){
  var ind=$('native-exit-ind').value;
  var html='';
  if(ind==='fixed_stop'){
    html+='<div class="native-param-row"><label>止盈%</label><input id="nexp-tp" value="5.0" type="number" step="0.1"></div>';
    html+='<div class="native-param-row"><label>止损%</label><input id="nexp-sl" value="3.0" type="number" step="0.1"></div>';
  }else if(ind==='trailing_stop'){
    html+='<div class="native-param-row"><label>回撤%</label><input id="nexp-trail" value="2.0" type="number" step="0.1"></div>';
  }
  $('native-exit-params').innerHTML=html;
}

function updateNativeCode(){
  if(S_nativeMode!=='indicator')return;
  updateNativeEntryParams();
  updateNativeExitParams();
  var symbol=$('native-symbol').value;
  var interval=$('native-interval').value;
  var entryInd=$('native-entry-ind').value;
  var exitInd=$('native-exit-ind').value;

  var code=generateIndicatorStrategyCode(symbol,interval,entryInd,exitInd);
  $('native-code-display').textContent=code;
}

function generateIndicatorStrategyCode(symbol,interval,entryInd,exitInd){
  var code="'''IndicatorStrategy - Auto-generated'''\n";
  code+='from xtquant.strategy.base import BaseStrategy\n\n';
  code+='class IndicatorStrategy(BaseStrategy):\n';
  code+='    def __init__(self):\n';
  code+='        super().__init__("indicator_'+symbol.toLowerCase()+'", ["'+symbol+'"])\n';
  code+='        self.interval = "'+interval+'"\n\n';
  code+='    async def on_bar(self, bar):\n';
  code+='        bars = self.bar_cache["'+symbol+'"]\n';
  code+='        if not bars or len(bars) < 50:\n            return\n\n';

  // Entry logic
  if(entryInd==='ma_cross'){
    var fast=document.querySelector('#native-entry-params input')?document.querySelector('#native-entry-params input').value:'5';
    var slow=document.querySelector('#native-entry-params input:nth-child(3)')?document.querySelector('#native-entry-params input:nth-child(3)').value:'20';
    code+='        # MA Crossover\n';
    code+='        ma_fast = sum(b[-1] for b in bars[-'+fast+':]) / '+fast+'\n';
    code+='        ma_slow = sum(b[-1] for b in bars[-'+slow+':]) / '+slow+'\n';
    code+='        if ma_fast > ma_slow:\n';
    code+='            await self.buy("'+symbol+'", self.get_position("'+symbol+'").free * 0.95 / bar.close)\n';
    code+='        elif ma_fast < ma_slow:\n';
    code+='            await self.sell("'+symbol+'", self.get_position("'+symbol+'").amount)\n';
  }else if(entryInd==='rsi'){
    var period=14,low=30,high=70;
    code+='        # RSI Strategy\n';
    code+='        gains = sum(max(0, bars[i][-1]-bars[i-1][-1]) for i in range(-'+period+',0)) / '+period+'\n';
    code+='        losses = sum(max(0, bars[i-1][-1]-bars[i][-1]) for i in range(-'+period+',0)) / '+period+'\n';
    code+='        rsi = 100 - 100/(1 + gains/losses) if losses > 0 else 100\n';
    code+='        if rsi < '+low+':\n';
    code+='            await self.buy("'+symbol+'", self.get_position("'+symbol+'").free * 0.95 / bar.close)\n';
    code+='        elif rsi > '+high+':\n';
    code+='            await self.sell("'+symbol+'", self.get_position("'+symbol+'").amount)\n';
  }else if(entryInd==='macd'){
    code+='        # MACD Strategy\n';
    code+='        prices = [b[-1] for b in bars]\n';
    code+='        ema12 = sum(prices[-12:])/12\n ema26 = sum(prices[-26:])/26\n';
    code+='        dif = ema12 - ema26\n';
    code+='        if dif > 0:\n';
    code+='            await self.buy("'+symbol+'", self.get_position("'+symbol+'").free * 0.95 / bar.close)\n';
    code+='        elif dif < 0:\n';
    code+='            await self.sell("'+symbol+'", self.get_position("'+symbol+'").amount)\n';
  }else if(entryInd==='boll'){
    code+='        # Bollinger Bands\n';
    code+='        prices = [b[-1] for b in bars[-20:]]\n';
    code+='        mid = sum(prices)/20\n';
    code+='        std = (sum((p-mid)**2 for p in prices)/20)**0.5\n';
    code+='        if bar.close < mid - 2*std:\n';
    code+='            await self.buy("'+symbol+'", self.get_position("'+symbol+'").free * 0.95 / bar.close)\n';
    code+='        elif bar.close > mid + 2*std:\n';
    code+='            await self.sell("'+symbol+'", self.get_position("'+symbol+'").amount)\n';
  }

  // Exit logic
  if(exitInd==='fixed_stop'){
    code+='\n        # Fixed Stop Loss / Take Profit\n';
    code+='        pos = self.get_position("'+symbol+'")\n';
    code+='        if pos.amount > 0:\n';
    code+='            pnl_pct = (bar.close / pos.entry_price - 1) * 100\n';
    code+='            if pnl_pct >= 5.0 or pnl_pct <= -3.0:\n';
    code+='                await self.sell("'+symbol+'", pos.amount)\n';
  }else if(exitInd==='trailing_stop'){
    code+='\n        # Trailing Stop\n';
    code+='        pos = self.get_position("'+symbol+'")\n';
    code+='        if pos.amount > 0:\n';
    code+='            drawdown = (pos.high_price - bar.close) / pos.high_price * 100\n';
    code+='            if drawdown >= 2.0:\n';
    code+='                await self.sell("'+symbol+'", pos.amount)\n';
  }else if(exitInd==='reverse_signal'){
    code+='        # Exit on reverse signal handled in entry logic above\n';
  }

  code+='\n    async def run(self):\n';
  code+='        while self._running:\n';
  code+='            await asyncio.sleep(1)\n';
  return code;
}

function loadScriptTemplate(type){
  var code='';
  if(type==='indicator'){
    code="'''IndicatorStrategy Template'''\n";
    code+='from xtquant.strategy.base import BaseStrategy\n\n';
    code+='class MyIndicatorStrategy(BaseStrategy):\n';
    code+='    def __init__(self):\n';
    code+='        super().__init__("my_strategy", ["BTCUSDT"])\n\n';
    code+='    async def on_bar(self, bar):\n';
    code+='        bars = self.bar_cache.get(bar.symbol, [])\n';
    code+='        if not bars or len(bars) < 20:\n';
    code+='            return\n';
    code+='        # === Your indicator logic here ===\n';
    code+='        # ma5 = sum(b.close for b in bars[-5:]) / 5\n';
    code+='        # ma20 = sum(b.close for b in bars[-20:]) / 20\n';
    code+='        # if ma5 > ma20:\n';
    code+='        #     await self.buy(bar.symbol, 0.001)\n';
    code+='        pass\n\n';
    code+='    async def run(self):\n';
    code+='        while self._running:\n';
    code+='            await asyncio.sleep(1)\n';
  }else{
    code="'''ScriptStrategy Template'''\n";
    code+='from xtquant.strategy.base import BaseStrategy\n\n';
    code+='class MyScriptStrategy(BaseStrategy):\n';
    code+='    def __init__(self):\n';
    code+='        super().__init__("my_strategy", ["BTCUSDT"])\n\n';
    code+='    async def on_bar(self, bar):\n';
    code+='        pass\n\n';
    code+='    async def run(self):\n';
    code+='        while self._running:\n';
    code+='            await asyncio.sleep(1)\n';
  }
  $('native-script-editor').value=code;
}

async function runNativeStrategy(){
  var btn=$('native-run-btn');btn.disabled=true;
  btn.innerHTML='<span class="ai-spinner"></span>运行中...';
  $('native-status').textContent='正在生成策略代码...';
  $('native-results').style.display='none';

  var code=$('native-code-display').textContent;
  if(!code||code.indexOf('选择指标')>=0){$('native-status').textContent='请先选择指标参数';btn.disabled=false;btn.innerHTML='运行回测';return}

  var symbol=$('native-symbol').value;
  try{
    var r=await fetch('/api/native/backtest',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({
      strategy_code:code,
      config:{symbol:symbol,initial_balance:parseFloat($('native-capital').value)||100000,num_bars:parseInt($('native-bars').value)||500,base_price:symbol==='BTCUSDT'?68000:symbol==='ETHUSDT'?3500:150,params:{},strategy_name:'native_indicator'}
    })});
    var d=await r.json();
    if(r.status!==200){$('native-status').textContent='Error: '+d.detail;btn.disabled=false;btn.innerHTML='运行回测';return}
    $('native-status').textContent='';
    S_nativeStrategy={strategy_code:code,strategy_name:'native_indicator'};
    S_nativeReport=d;
    renderNativeBacktest(d);
    $('native-deploy-btn').disabled=false;
    toast('回测完成');
  }catch(e){$('native-status').textContent='Error: '+e.message;}
  btn.disabled=false;btn.innerHTML='运行回测';
}

async function runNativeScriptStrategy(){
  var btn=$('native-sc-run-btn');btn.disabled=true;
  btn.innerHTML='<span class="ai-spinner"></span>运行中...';
  $('native-sc-status').textContent='正在运行回测...';
  $('native-results').style.display='none';

  var code=$('native-script-editor').value.trim();
  if(!code){$('native-sc-status').textContent='请输入策略代码';btn.disabled=false;btn.innerHTML='运行回测';return}

  var symbol=$('native-sc-symbol').value;
  try{
    var r=await fetch('/api/native/backtest',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({
      strategy_code:code,
      config:{symbol:symbol,initial_balance:parseFloat($('native-sc-capital').value)||100000,num_bars:parseInt($('native-sc-bars').value)||500,base_price:symbol==='BTCUSDT'?68000:symbol==='ETHUSDT'?3500:150,params:{},strategy_name:'native_script'}
    })});
    var d=await r.json();
    if(r.status!==200){$('native-sc-status').textContent='Error: '+d.detail;btn.disabled=false;btn.innerHTML='运行回测';return}
    $('native-sc-status').textContent='';
    S_nativeStrategy={strategy_code:code,strategy_name:'native_script'};
    S_nativeReport=d;
    // Also show the code in the shared display
    $('native-code-display').textContent=code;
    renderNativeBacktest(d);
    $('native-deploy-btn').disabled=false;
    toast('回测完成');
  }catch(e){$('native-sc-status').textContent='Error: '+e.message;}
  btn.disabled=false;btn.innerHTML='运行回测';
}

function renderNativeBacktest(d2){
  $('native-results').style.display='flex';
  initNativeChart();
  if(S_nativeSeries){
    var eqData=[];
    if(d2.equity_curve)for(var i=0;i<d2.equity_curve.length;i++){
      var p=d2.equity_curve[i];
      eqData.push({time:Date.now()/1000-(d2.equity_curve.length-i)*60,value:p.equity});
    }
    S_nativeSeries.setData(eqData);S_nativeChart.timeScale().fitContent();
  }
  var rep=d2.report||{};
  var metrics=[
    ['Total Return',FP(rep.total_return_pct)],['Annual Return',FP(rep.annual_return_pct)],
    ['Sharpe',F(rep.sharpe_ratio,3)],['Sortino',F(rep.sortino_ratio,3)],
    ['Max DD',FP(rep.max_drawdown_pct)],['Win Rate',FP(rep.win_rate_pct)],
    ['Profit Factor',F(rep.profit_factor,3)],['Total Trades',rep.total_trades||0]
  ];
  var mh='';
  for(var i=0;i<metrics.length;i++){
    mh+='<div style="background:var(--bg);padding:6px 8px;border-radius:5px"><div class="f-label">'+metrics[i][0]+'</div><div style="font-size:14px;font-weight:700">'+metrics[i][1]+'</div></div>';
  }
  $('native-metrics').innerHTML=mh;
}

function initNativeChart(){
  if(S_nativeChart)return;
  var el=$('native-chart');
  if(!el||!el.clientHeight)return;
  S_nativeChart=LightweightCharts.createChart(el,{
    layout:{background:{color:'#1f2328fff'},textColor:'#1f2328'},
    grid:{vertLines:{color:'rgba(48,54,61,.3)'},horzLines:{color:'rgba(48,54,61,.3)'}},
    rightPriceScale:{borderColor:'#d0d7de'},timeScale:{borderColor:'#d0d7de',timeVisible:true},
  });
  S_nativeSeries=S_nativeChart.addAreaSeries({topColor:'rgba(63,185,80,.3)',bottomColor:'rgba(63,185,80,.01)',lineColor:'#3fb950',lineWidth:2});
}

function copyNativeCode(){
  var code=$('native-script-editor').value;
  if(!code){toast('没有可复制的代码','err');return}
  navigator.clipboard.writeText(code).then(function(){toast('代码已复制')},function(){toast('复制失败','err')});
}

function copyNativeGeneratedCode(){
  var code=$('native-code-display').textContent;
  if(!code||code.indexOf('选择指标')>=0){toast('没有可复制的代码','err');return}
  navigator.clipboard.writeText(code).then(function(){toast('代码已复制')},function(){toast('复制失败','err')});
}

function downloadNativeCode(){
  var code=$('native-code-display').textContent;
  if(!code||code.indexOf('选择指标')>=0){toast('没有可下载的代码','err');return}
  var name=(S_nativeStrategy&&S_nativeStrategy.strategy_name)||'native_strategy';
  var blob=new Blob([code],{type:'text/x-python'});
  var a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download=name+'.py';a.click();
}

async function deployNativeStrategy(){
  if(!S_nativeStrategy){toast('没有可部署的策略','err');return}
  var ok=confirm('确认部署 "'+(S_nativeStrategy.strategy_name||'Native Strategy')+'" 到策略模板库吗？');
  if(!ok)return;
  try{
    var r=await fetch('/api/ai/deploy',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({
      strategy_name:S_nativeStrategy.strategy_name,
      strategy_code:S_nativeStrategy.strategy_code,
      description:'Native Python strategy',
      symbol:$('native-symbol').value||$('native-sc-symbol').value,
      risk:'medium',
      backtest_report:S_nativeReport||{}
    })});
    var d=await r.json();
    if(d.status==='ok'){
      toast('策略已部署到模板库! ID: '+d.id);
      $('native-deploy-btn').disabled=true;$('native-deploy-btn').textContent='已部署';
    }else{toast('部署失败','err')}
  }catch(e){toast('Error: '+e.message,'err')}
}

function resetNativeStrategy(){
  S_nativeStrategy=null;S_nativeReport=null;
  $('native-results').style.display='none';
  $('native-metrics').innerHTML='';
  $('native-deploy-btn').disabled=true;
  if(S_nativeSeries)S_nativeSeries.setData([]);
}

// Init native mode on page load
updateNativeEntryParams();
updateNativeExitParams();
updateNativeCode();
loadScriptTemplate('indicator');

// ═══════════════════════════════════════════════ FLOATING AGENT CHAT ═══════════════════════════════
var S_agentOpen=false;
var S_agentBusy=false;

		function toggleAgentChatPanel(){
  S_agentOpen=!S_agentOpen;
  var panel=$('agent-panel');
  if(S_agentOpen){
    panel.classList.add('open');
    if(!panel._dragged){
      var pw=panel.offsetWidth, ph=panel.offsetHeight;
      panel.style.left=((window.innerWidth-pw)/2)+'px';
      panel.style.top=((window.innerHeight-ph)/2)+'px';
      panel.style.right='auto'; panel.style.bottom='auto';
    }
    $('agent-input').focus();
  }else{
    panel.classList.remove('open');
  }
}

function clearAgentChat(){
  $('agent-messages').innerHTML='<div class="agent-msg agent">对话已清空。有什么可以帮你的？</div>';
}

async function sendAgentMsg(){
  if(S_agentBusy)return;
  var input=$('agent-input');
  var text=input.value.trim();
  if(!text)return;
  input.value='';
  S_agentBusy=true;
  var btn=$('agent-send-btn');btn.disabled=true;

  var msgs=$('agent-messages');
  // Add user message
  var userDiv=document.createElement('div');
  userDiv.className='agent-msg user';userDiv.textContent=text;
  msgs.appendChild(userDiv);
  // Add typing indicator
  var typeDiv=document.createElement('div');
  typeDiv.className='agent-msg typing';
  typeDiv.innerHTML='<span class="agent-typing-dot"></span><span class="agent-typing-dot"></span><span class="agent-typing-dot"></span>';
  msgs.appendChild(typeDiv);
  msgs.scrollTop=msgs.scrollHeight;

  try{
    var r=await fetch('/api/agent/chat',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({message:text})});
    var d=await r.json();
    typeDiv.remove();
    var agentDiv=document.createElement('div');
    agentDiv.className='agent-msg agent';agentDiv.textContent=d.reply||'抱歉，我暂时无法回答。';
    msgs.appendChild(agentDiv);
  }catch(e){
    typeDiv.remove();
    var errDiv=document.createElement('div');
    errDiv.className='agent-msg agent';errDiv.textContent='网络错误，请稍后重试。';
    msgs.appendChild(errDiv);
  }
  msgs.scrollTop=msgs.scrollHeight;
  S_agentBusy=false;
  btn.disabled=false;
  input.focus();
}

// ═══════════════════════════════════════════════ AI OPPORTUNITY PAGE ═══════════════════════════════════
var S_aioBusy=false;

function initAIOpportunity(){
  loadAI3Models();
  loadAI3Chart();
  refreshAIOSnapshot();
  fetch('/api/auto-trade/config').then(function(r){return r.json()}).then(function(d){
    if(d.status==='ok'&&d.config){S_ai3.autoTrade=d.config;var toggle=$('ai3-at-toggle');if(toggle){if(d.config.enabled)toggle.classList.add('active');else toggle.classList.remove('active');}var th=$('ai3-at-threshold');if(th){th.value=d.config.threshold||70;$('ai3-at-threshold-val').textContent=(d.config.threshold||70)+'%';}var dv=$('ai3-at-divergence');if(dv){dv.value=d.config.divergence_protection||30;$('ai3-at-divergence-val').textContent=(d.config.divergence_protection||30)+'%';}}
  }).catch(function(e){});
}

async function refreshAIOSnapshot(){
  var symbol=$('aio-symbol').value||'BTCUSDT';
  var interval=$('aio-interval').value||'1h';
  try{
    var r=await fetch('/api/ai/snapshot?symbol='+symbol+'&interval='+interval);
    var d=await r.json();
    if(d.status==='ok'){$('aio-price').textContent=d.price||'--';var ch=d.change_24h||0;$('aio-change').textContent=(ch>=0?'+':'')+ch.toFixed(2)+'%';$('aio-change').className='aio-card-val'+(ch>=0?' up':' down');$('aio-volume').textContent=d.volume_24h||'--';$('aio-atr').textContent=d.atr||'--';}
    else{setAIODefaults();}
  }catch(e){setAIODefaults();}
}

function setAIODefaults(){['aio-price','aio-change','aio-volume','aio-atr'].forEach(function(id){$(id).textContent='--';});$('aio-change').className='aio-card-val';}

async function runQuickScan(){
  if(S_aioBusy)return;S_aioBusy=true;
  var btn=$('aio-scan-btn');if(btn)btn.disabled=true;
  $('aio-status').textContent='扫描中...';
  addAIOMsg('user','快速扫描 BTC/ETH/BNB/SOL');
  showAIOTyping();
  try{
    var r=await fetch('/api/agent/chat',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({message:'快速扫描BTCUSDT,ETHUSDT,BNBUSDT,SOLUSDT当前走势，给出简洁的多空判断和关键价位。'})});
    var d=await r.json();
    hideAIOTyping();
    if(d.reply){addAIOMsg('assistant',d.reply);$('aio-result').innerHTML=formatAnalysisResult(d.reply);}
  }catch(e){hideAIOTyping();}
  $('aio-status').textContent='';S_aioBusy=false;if(btn)btn.disabled=false;
}

function quickAsk(question){var input=$('aio-input');if(input){input.value=question;input.focus();}}

async function sendAIOMsg(){
  if(S_aioBusy)return;S_aioBusy=true;
  var input=$('aio-input');var msg=input.value.trim();if(!msg){S_aioBusy=false;return}
  addAIOMsg('user',msg);input.value='';showAIOTyping();
  try{
    var r=await fetch('/api/agent/chat',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({message:msg})});
    var d=await r.json();hideAIOTyping();
    if(d.reply){addAIOMsg('assistant',d.reply);$('aio-result').innerHTML=formatAnalysisResult(d.reply);}
  }catch(e){hideAIOTyping();}
  S_aioBusy=false;
}

function addAIOMsg(role,text){
  var container=$('ai3-chat-messages');if(!container)return;
  var div=document.createElement('div');div.className='aio-msg '+(role==='user'?'user':'assistant');
  var content=document.createElement('div');content.className='aio-msg-content';content.innerHTML=text.replace(/\\n/g,'<br>');
  div.appendChild(content);container.appendChild(div);container.scrollTop=container.scrollHeight;
}

function showAIOTyping(){$('aio-typing').style.display='block';}
function hideAIOTyping(){$('aio-typing').style.display='none';}
function clearAIOChat(){$('ai3-chat-messages').innerHTML='<div class=\"ai3-chat-empty\">点击"多模型分析"开始<br>或输入问题与AI讨论</div>';}

function formatAnalysisResult(text){
  var html=text.replace(/\\*\\*(.+?)\\*\\*/g,'<strong>$1</strong>');
  html=html.replace(/(?:看多|bullish)/gi,'<span class=\"signal-tag bullish\">$&</span>');
  html=html.replace(/(?:看空|bearish)/gi,'<span class=\"signal-tag bearish\">$&</span>');
  html=html.replace(/(?:震荡|neutral)/gi,'<span class=\"signal-tag neutral\">$&</span>');
  html=html.replace(/\\n/g,'<br>');return '<div class=\"analysis-section\">'+html+'</div>';
}

// ═══════════════════════════════ AI3 Multi-Model Analysis (for #pg-ai_oppty) ═══════════════════════════════
var S_ai3={
  symbol:'BTCUSDT',interval:'1h',klines:[],indicators:{},
  models:[],enabledModels:['deepseek','openai','qwen'],
  analysisTaskId:null,analysisResult:null,pollTimer:null,
  autoTrade:{enabled:false,threshold:70,divergence_protection:30},
  backtestResult:null
};

const AI3_PRESETS={
  all:['deepseek','openai','qwen','qwen_turbo','claude','gemini','moonshot','zhipu','baidu','minimax','stepfun','doubao'],
  top3:['deepseek','openai','claude'],
  chinese:['deepseek','qwen','qwen_turbo','moonshot','zhipu','baidu','minimax','stepfun','doubao'],
  fast:['qwen_turbo','gemini','moonshot']
};

// ═══ Model Management ═══
async function loadAI3Models(){
  try{
    var r=await fetch('/api/models/list');
    var d=await r.json();
    if(d.status==='ok'&&d.models){S_ai3.models=d.models;renderAI3Models();}
  }catch(e){}
}

function renderAI3Models(){
  var container=$('ai3-model-list');
  if(!container)return;
  var html='';
  S_ai3.models.forEach(function(m){
    var enabled=m.enabled&&m.has_api_key;
    html+='<div class="ai3-model-item'+(enabled?' enabled':' disabled')+'" onclick="toggleAI3Model(\''+m.id+'\')" id="ai3-model-'+m.id+'">';
    html+='<div class="ai3-model-color" style="background:'+m.color+'"></div>';
    html+='<div class="ai3-model-name">'+m.name+'</div>';
    html+='<span class="ai3-model-status '+(m.has_api_key?'ok':'warn')+'">'+(m.has_api_key?'已配置':'未配置')+'</span>';
    html+='</div>';
    html+='<div class="ai3-model-weight"><input type="range" min="0.1" max="2" step="0.1" value="'+m.weight+'" class="ai3-weight-slider" oninput="updateAI3ModelWeight(\''+m.id+'\',this.value)" onclick="event.stopPropagation()"></div>';
  });
  container.innerHTML=html;
}

function toggleAI3Model(id){
  var idx=S_ai3.enabledModels.indexOf(id);
  if(idx>=0){S_ai3.enabledModels.splice(idx,1);}
  else{var m=S_ai3.models.find(function(x){return x.id===id});if(m&&m.has_api_key)S_ai3.enabledModels.push(id);}
  renderAI3Models();
  S_ai3.enabledModels.forEach(function(mid){var el=$('ai3-model-'+mid);if(el){el.classList.add('enabled');el.classList.remove('disabled')}});
}

function updateAI3ModelWeight(id,weight){}
function applyAI3Preset(preset){
  var models=AI3_PRESETS[preset];if(!models)return;
  S_ai3.enabledModels=models.filter(function(mid){var m=S_ai3.models.find(function(x){return x.id===mid});return m&&m.has_api_key;});
  renderAI3Models();
  S_ai3.enabledModels.forEach(function(mid){var el=$('ai3-model-'+mid);if(el){el.classList.add('enabled');el.classList.remove('disabled')}});
  toast('已应用预设: '+preset);
}
function saveAI3ModelConfig(){toast('模型配置已保存');}

// ═══ K-Line Chart ═══
async function loadAI3Chart(){
  var symbol=$('aio-symbol').value||'BTCUSDT';
  var interval=$('aio-interval').value||'1h';
  S_ai3.symbol=symbol;S_ai3.interval=interval;
  var titleEl=$('ai3-chart-title');
  if(titleEl)titleEl.innerHTML=symbol.replace('USDT','/USDT')+' <span id="ai3-chart-interval">'+interval.toUpperCase()+'</span>';
  try{
    var r=await fetch('/api/ai/klines?symbol='+symbol+'&interval='+interval+'&limit=200');
    var d=await r.json();
    if(d.status==='ok'&&d.klines&&d.klines.length>0){
      S_ai3.klines=d.klines;
      $('ai3-chart-price').textContent=d.last_price||'--';
      $('ai3-chart-demo-badge').style.display='none';
      renderAI3Chart();return;
    }
  }catch(e){}
  $('ai3-chart-demo-badge').style.display='inline';
  generateDemoAI3Klines();renderAI3Chart();
}

function generateDemoAI3Klines(){
  var klines=[];var price=76000;var now=Date.now();
  var intervalMs={'15m':900000,'1h':3600000,'4h':14400000,'1d':86400000};
  var step=intervalMs[S_ai3.interval]||3600000;
  for(var i=199;i>=0;i--){
    var t=now-i*step;var change=(Math.random()-0.48)*800;
    var open=price;var close=price+change;
    var high=Math.max(open,close)+Math.random()*300;
    var low=Math.min(open,close)-Math.random()*300;
    var volume=500+Math.random()*2000;
    klines.push({time:t,open:open,high:high,low:low,close:close,volume:volume});
    price=close;
  }
  S_ai3.klines=klines;$('ai3-chart-price').textContent='$'+price.toFixed(0);
}

function renderAI3Chart(){
  var canvas=$('ai3-canvas');
  if(!canvas||!S_ai3.klines.length)return;
  var container=canvas.parentElement;
  var w=container.clientWidth,h=container.clientHeight;
  if(h<50){setTimeout(renderAI3Chart,100);return}
  canvas.width=w;canvas.height=h;
  var ctx=canvas.getContext('2d');
  var W=canvas.width,H=canvas.height;
  ctx.clearRect(0,0,W,H);
  var klines=S_ai3.klines;
  var chartH=H*0.7,volH=H*0.15,rsiH=H*0.13;

  var prices=klines.map(function(k){return [k.high,k.low]}).flat();
  var minP=Math.min.apply(null,prices),maxP=Math.max.apply(null,prices);
  var pad=(maxP-minP)*0.05;minP-=pad;maxP+=pad;
  var vols=klines.map(function(k){return k.volume});
  var maxV=Math.max.apply(null,vols);
  var n=klines.length,barW=(W-40)/n,candleW=Math.max(barW*0.7,0.5);
  function x(i){return 10+i*barW+barW/2}
  function y(p){return chartH-(p-minP)/(maxP-minP)*chartH}

  // Grid
  ctx.strokeStyle='#eaeef2';ctx.lineWidth=0.5;
  for(var i=0;i<5;i++){var gy=chartH*i/4;ctx.beginPath();ctx.moveTo(10,gy);ctx.lineTo(W-5,gy);ctx.stroke();var gp=minP+(maxP-minP)*(4-i)/4;ctx.fillStyle='#484f58';ctx.font='9px sans-serif';ctx.fillText('$'+gp.toFixed(0),W-55,gy-3);}

  // Signal markers
  if(S_ai3.analysisResult){drawAI3SignalMarkers(ctx,W,chartH,minP,maxP,x,y);}

  // Candles
  for(var i=0;i<n;i++){var k=klines[i];var cx=x(i),oy=y(k.open),cy=y(k.close),hy=y(k.high),ly=y(k.low);var isGreen=k.close>=k.open;ctx.strokeStyle=isGreen?'#3fb950':'#f85149';ctx.fillStyle=isGreen?'#3fb950':'#f85149';ctx.beginPath();ctx.moveTo(cx,hy);ctx.lineTo(cx,ly);ctx.stroke();var bh=Math.max(Math.abs(cy-oy),0.5);ctx.fillRect(cx-candleW/2,Math.min(oy,cy),candleW,bh);}

  // EMA
  var ema9=calcEMA(klines,9),ema21=calcEMA(klines,21);
  drawLine(ctx,ema9,'#58a6ff',x,y);drawLine(ctx,ema21,'#f0883e',x,y);
  ctx.fillStyle='#58a6ff';ctx.font='9px sans-serif';ctx.fillText('EMA 9',15,14);
  ctx.fillStyle='#f0883e';ctx.fillText('EMA 21',65,14);

  // Volume
  var volTop=chartH+5;
  for(var i=0;i<n;i++){var k=klines[i];var vh=(k.volume/maxV)*volH;var isGreen=k.close>=k.open;ctx.fillStyle=isGreen?'rgba(63,185,80,.4)':'rgba(248,81,73,.4)';ctx.fillRect(x(i)-candleW/2,volTop+volH-vh,candleW,vh);}
  ctx.strokeStyle='#d0d7de';ctx.beginPath();ctx.moveTo(10,volTop);ctx.lineTo(W-5,volTop);ctx.stroke();

  // RSI
  var rsi=calcRSI(klines,14),rsiTop=volTop+volH+8;
  ctx.strokeStyle='#d0d7de';ctx.beginPath();ctx.moveTo(10,rsiTop);ctx.lineTo(W-5,rsiTop);ctx.stroke();
  ctx.fillStyle='#484f58';ctx.font='8px sans-serif';ctx.fillText('RSI 14',15,rsiTop+10);
  var rsiY=function(v){return rsiTop+rsiH-(v/100)*rsiH};
  drawLine(ctx,rsi.map(function(v,i){return {i:i,v:rsiY(v)}}),'#d2a8ff',x,function(p){return p.v});

  var lp=klines[n-1].close;var lc=klines[n-1].close>=klines[n-1].open?'#3fb950':'#f85149';
  ctx.fillStyle=lc;ctx.fillText('$'+lp.toFixed(1),W-85,25);
}

function drawAI3SignalMarkers(ctx,W,chartH,minP,maxP,xFn,yFn){
  var signals=S_ai3.analysisResult.signals||[];
  signals.forEach(function(s){
    if(s.support&&s.support>minP&&s.support<maxP){var sy=yFn(s.support);ctx.strokeStyle=s.color||'#3fb950';ctx.lineWidth=1;ctx.setLineDash([4,3]);ctx.beginPath();ctx.moveTo(10,sy);ctx.lineTo(W-5,sy);ctx.stroke();ctx.setLineDash([]);ctx.fillStyle=s.color||'#3fb950';ctx.font='8px sans-serif';ctx.fillText('S:'+s.support.toFixed(0),W-60,sy-3);}
    if(s.resistance&&s.resistance>minP&&s.resistance<maxP){var ry=yFn(s.resistance);ctx.strokeStyle=s.color||'#f85149';ctx.lineWidth=1;ctx.setLineDash([4,3]);ctx.beginPath();ctx.moveTo(10,ry);ctx.lineTo(W-5,ry);ctx.stroke();ctx.setLineDash([]);ctx.fillStyle=s.color||'#f85149';ctx.font='8px sans-serif';ctx.fillText('R:'+s.resistance.toFixed(0),W-60,ry-3);}
  });
}

// ═══ Multi-Model Analysis ═══
async function startAI3Analysis(){
  if(S_ai3.analysisTaskId){toast('分析任务运行中');return;}
  var enabled=S_ai3.enabledModels;
  if(!enabled.length){enabled=S_ai3.models.filter(function(m){return m.has_api_key}).map(function(m){return m.id}).slice(0,3);S_ai3.enabledModels=enabled;}
  var btn=$('aio-analyze-btn');if(btn){btn.disabled=true;btn.textContent='分析中...';}
  $('aio-status').textContent='提交分析任务...';
  var symbol=$('aio-symbol').value||'BTCUSDT';
  var interval=$('aio-interval').value||'1h';
  try{
    var r=await fetch('/api/analysis/start',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({symbol:symbol,interval:interval,enabled_models:enabled,prompt:'分析当前行情'})});
    var d=await r.json();
    if(d.status==='ok'&&d.task_id){S_ai3.analysisTaskId=d.task_id;S_ai3.pollTimer=setInterval(pollAI3AnalysisResult,2000);$('aio-status').textContent='分析中 ('+enabled.length+'模型)...';}
    else{$('aio-status').textContent='提交失败';if(btn){btn.disabled=false;btn.textContent='多模型分析';}}
  }catch(e){$('aio-status').textContent='网络错误';if(btn){btn.disabled=false;btn.textContent='多模型分析';}}
}

async function pollAI3AnalysisResult(){
  if(!S_ai3.analysisTaskId)return;
  try{
    var r=await fetch('/api/analysis/result?task_id='+S_ai3.analysisTaskId);
    var d=await r.json();
    if(d.status==='running'){$('aio-status').textContent='分析中 ('+d.completed+'/'+d.total+')...';}
    else if(d.status==='completed'){
      clearInterval(S_ai3.pollTimer);S_ai3.pollTimer=null;S_ai3.analysisResult=d;
      var btn=$('aio-analyze-btn');if(btn){btn.disabled=false;btn.textContent='多模型分析';}
      $('aio-status').textContent='分析完成';
      renderAI3VoteResults(d.vote_summary);
      renderAI3ChatBubbles(d.results);
      renderAI3Chart();
      checkAI3AutoTradeCondition(d.vote_summary);
      setTimeout(function(){S_ai3.analysisTaskId=null;$('aio-status').textContent=''},3000);
    }
  }catch(e){clearInterval(S_ai3.pollTimer);S_ai3.pollTimer=null;$('aio-status').textContent='查询失败';var btn=$('aio-analyze-btn');if(btn){btn.disabled=false;btn.textContent='多模型分析';}}
}

function renderAI3VoteResults(vote){
  if(!vote)return;
  var el=$('ai3-consensus-val');if(el){el.textContent=vote.consensus==='bullish'?'看涨':(vote.consensus==='bearish'?'看跌':'中性');el.className='ai3-consensus-val '+vote.consensus;}
  var bar=$('ai3-vote-bar');if(bar){bar.innerHTML='<div class="ai3-vote-bar-bullish" style="width:'+vote.bullish_pct+'%"></div><div class="ai3-vote-bar-bearish" style="width:'+vote.bearish_pct+'%"></div><div class="ai3-vote-bar-neutral" style="width:'+vote.neutral_pct+'%"></div>';}
  $('ai3-confidence-span').textContent='置信度 '+vote.composite_confidence+'% ('+vote.consensus_strength+')';
  $('ai3-divergence-span').textContent='分歧 '+vote.divergence+'%';
}

function renderAI3ChatBubbles(results){
  var container=$('ai3-chat-messages');if(!container)return;
  var html='';
  for(var mid in results){var r=results[mid];var name=r.model_name||mid;var color=r.model_color||'#6B7280';var content=r.reasoning||r.reply||(r.error?('错误: '+r.error):'无回复');var direction=r.direction||'neutral';var sentimentLabel=direction==='bullish'?'看涨':(direction==='bearish'?'看跌':'中性');var confidence=r.confidence!==undefined?(' | 置信度:'+r.confidence+'%'):'';html+='<div class="ai3-chat-bubble" style="border-left-color:'+color+'"><div class="ai3-chat-bubble-header"><span class="ai3-chat-bubble-name" style="color:'+color+'">'+name+'</span><span class="ai3-chat-bubble-sentiment '+direction+'">'+sentimentLabel+confidence+'</span></div><div class="ai3-chat-bubble-body">'+content+'</div></div>';}
  container.innerHTML=html||'<div class="ai3-chat-empty">无分析结果</div>';
}

// ═══ Auto-Trade ═══
function toggleAI3AutoTrade(){
  var toggle=$('ai3-at-toggle');if(!toggle)return;
  toggle.classList.toggle('active');S_ai3.autoTrade.enabled=toggle.classList.contains('active');
  saveAI3AutoTradeConfig();
}
function saveAI3AutoTradeConfig(){
  var threshold=parseFloat($('ai3-at-threshold').value)||70;
  var divergence=parseFloat($('ai3-at-divergence').value)||30;
  S_ai3.autoTrade.threshold=threshold;S_ai3.autoTrade.divergence_protection=divergence;
  fetch('/api/auto-trade/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({enabled:S_ai3.autoTrade.enabled,threshold:threshold,divergence_protection:divergence,symbol:$('aio-symbol').value||'BTCUSDT'})}).catch(function(e){});
}
function checkAI3AutoTradeCondition(vote){
  if(!vote||!S_ai3.autoTrade.enabled)return;
  if(vote.composite_confidence>=S_ai3.autoTrade.threshold&&vote.divergence<S_ai3.autoTrade.divergence_protection){
    toast('自动交易信号触发! 共识:'+vote.consensus+' 置信度:'+vote.composite_confidence+'% 分歧:'+vote.divergence+'%');
  }
}

// ═══ Chat ═══
async function sendAI3Chat(){
  var input=$('aio-input');var message=input.value.trim();if(!message)return;
  var btn=$('aio-send-btn');if(btn){btn.disabled=true;btn.textContent='...';}
  addAIOMsg('user',message);
  input.value='';
  showAIOTyping();
  try{
    var r=await fetch('/api/chat/send',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({message:message,enabled_models:S_ai3.enabledModels})});
    var d=await r.json();
    hideAIOTyping();
    if(d.status==='ok'&&d.responses){renderAI3ChatBubbles(d.responses);}
  }catch(e){hideAIOTyping();}
  if(btn){btn.disabled=false;btn.textContent='发送';}
}


var S_ai2={
  symbol:'BTCUSDT',symbolName:'Bitcoin',exchange:'Binance',interval:'1h',code:'',klines:[],indicators:{},
  backtestResult:null,btChartData:null,overlayIds:[]
};

// ═══ Symbol Search Dropdown ═══
function openSymbolSearch(){
  closeExchangePicker();
  var badge=$('ai2-symbol-badge');
  if(!badge)return;
  var rect=badge.getBoundingClientRect();
  var overlay=document.createElement('div');
  overlay.className='sym-search-overlay';
  overlay.id='sym-search-overlay';
  overlay.onclick=function(e){if(e.target===overlay)closeSymbolSearch();};
  var dropdown=document.createElement('div');
  dropdown.className='sym-search-dropdown';
  dropdown.id='sym-search-dropdown';
  dropdown.style.left=Math.max(10,rect.left-20)+'px';
  dropdown.style.top=(rect.bottom+8)+'px';
  dropdown.innerHTML='<div class="sym-search-header"><div class="sym-search-input-wrap"><span class="sym-search-icon">&#x1F50D;</span><input class="sym-search-input" id="sym-search-input" placeholder="搜索币种..." autofocus oninput="filterSymbolList()" onkeydown="if(event.key===\'Escape\')closeSymbolSearch();if(event.key===\'Enter\'&&S_symResults&&S_symResults.length>0)selectSymbol(S_symResults[0])"></div></div><div class="sym-search-hot"><div class="sym-search-hot-label">热门币种</div><div class="sym-hot-chips" id="sym-hot-chips"><span class="sym-hot-chip" onclick="selectSymbol(\'BTCUSDT\')">BTC/USDT</span><span class="sym-hot-chip" onclick="selectSymbol(\'ETHUSDT\')">ETH/USDT</span><span class="sym-hot-chip" onclick="selectSymbol(\'SOLUSDT\')">SOL/USDT</span><span class="sym-hot-chip" onclick="selectSymbol(\'BNBUSDT\')">BNB/USDT</span><span class="sym-hot-chip" onclick="selectSymbol(\'DOGEUSDT\')">DOGE/USDT</span><span class="sym-hot-chip" onclick="selectSymbol(\'XRPUSDT\')">XRP/USDT</span></div></div><div class="sym-search-list" id="sym-search-list"><div class="sym-search-empty"><div class="sym-empty-icon">&#x1F50D;</div>输入关键词搜索币种</div></div>';
  overlay.appendChild(dropdown);
  document.body.appendChild(overlay);
  setTimeout(function(){
    var inp=$('sym-search-input');if(inp)inp.focus();
    loadHotSymbols();
  },50);
}
function closeSymbolSearch(){
  var overlay=$('sym-search-overlay');
  if(overlay)overlay.remove();
}
var S_symResults=[];
async function loadHotSymbols(){
  try{
    var r=await fetch('/api/symbols/search?q=');
    var data=await r.json();
    if(Array.isArray(data)&&data.length){
      S_symResults=data;
      renderSymList(data.slice(0,50));
    }
  }catch(e){}
}
function filterSymbolList(){
  var q=($('sym-search-input')||{}).value||'';
  if(!q.trim()){loadHotSymbols();return}
  var ql=q.toLowerCase();
  var filtered=S_symResults.filter(function(s){return s.toLowerCase().indexOf(ql)>=0;});
  renderSymList(filtered.slice(0,30));
}
function renderSymList(list){
  var container=$('sym-search-list');
  if(!container)return;
  if(!list.length){container.innerHTML='<div class="sym-search-empty"><div class="sym-empty-icon">&#x1F50D;</div>未找到匹配的币种</div>';return}
  var cur=S_ai2.symbol;
  container.innerHTML=list.map(function(s){
    var parts=s.replace('USDT','/USDT').replace('USDC','/USDC').split('/');
    var base=parts[0]||s;var quote=parts[1]||'';
    var active=s===cur?' active':'';
    return '<div class="sym-search-item'+active+'" onclick="selectSymbol(\''+s+'\')"><span class="sym-dot" style="background:var(--blue)"></span><span class="sym-pair">'+base+'<span class="sym-quote">/'+quote+'</span></span><span class="sym-coin-name">'+getCoinName(base)+'</span><span class="sym-exchange">'+S_ai2.exchange.toUpperCase()+'</span></div>';
  }).join('');
}
function getCoinName(base){
  var names={BTC:'Bitcoin',ETH:'Ethereum',SOL:'Solana',BNB:'BNB',DOGE:'Dogecoin',XRP:'Ripple',ADA:'Cardano',AVAX:'Avalanche',DOT:'Polkadot',LINK:'Chainlink',MATIC:'Polygon',UNI:'Uniswap',ATOM:'Cosmos',LTC:'Litecoin',ETC:'Ethereum Classic',FIL:'Filecoin',APT:'Aptos',ARB:'Arbitrum',OP:'Optimism',SUI:'Sui',SEI:'Sei',TIA:'Celestia',INJ:'Injective',RUNE:'THORChain',PEPE:'Pepe',WIF:'Dogwifhat',BONK:'Bonk',SHIB:'Shiba Inu',ORDI:'ORDI',SATS:'SATS',TRX:'TRON',TON:'Toncoin',NEAR:'NEAR',APT:'Aptos',HBAR:'Hedera',STX:'Stacks',FLOW:'Flow',GRT:'The Graph',AAVE:'Aave',MKR:'Maker',SNX:'Synthetix',COMP:'Compound',CRV:'Curve'};
  return names[base]||'';
}
function selectSymbol(sym){
  S_ai2.symbol=sym;
  var parts=sym.replace('USDT','/USDT').replace('USDC','/USDC').split('/');
  $('ai2-symbol-badge').innerHTML=parts[0]+'/<span style="color:var(--muted)">'+parts[1]+'</span> &#x25BE;';
  $('ai2-symbol-name').textContent=getCoinName(parts[0])||parts[0];
  closeSymbolSearch();
  loadAI2Chart();
  toast('已切换至 '+sym);
}

// ═══ Exchange Picker Dropdown ═══
function openExchangePicker(){
  closeSymbolSearch();
  var tag=$('ai2-exchange-tag');
  if(!tag)return;
  var rect=tag.getBoundingClientRect();
  var overlay=document.createElement('div');
  overlay.className='sym-search-overlay';
  overlay.id='ex-picker-overlay';
  overlay.onclick=function(e){if(e.target===overlay)closeExchangePicker();};
  var dropdown=document.createElement('div');
  dropdown.className='ex-dropdown';
  dropdown.id='ex-picker-dropdown';
  dropdown.style.left=Math.max(10,rect.left-20)+'px';
  dropdown.style.top=(rect.bottom+8)+'px';
  var cur=S_ai2.exchange;
  dropdown.innerHTML='<div class="ex-dropdown-header">选择交易所</div><div id="ex-picker-list">加载中...</div>';
  overlay.appendChild(dropdown);
  document.body.appendChild(overlay);
  loadExchangeList(cur);
}
function closeExchangePicker(){
  var overlay=$('ex-picker-overlay');
  if(overlay)overlay.remove();
}
async function loadExchangeList(current){
  var list=[{name:'Binance',connected:true},{name:'OKX',connected:true},{name:'Bybit',connected:false},{name:'Gate',connected:false},{name:'Bitget',connected:false},{name:'Huobi',connected:false}];
  try{
    var r=await fetch('/api/config');var d=await r.json();
    var exchanges=Object.keys(d).filter(function(k){return k!=='ai'&&k!=='default_ai_provider'&&k!=='default_exchange';});
    if(exchanges.length){
      list=exchanges.map(function(name){return {name:name,connected:d[name]&&d[name].api_key?true:false};});
    }
  }catch(e){}
  var container=$('ex-picker-list');
  if(!container)return;
  container.innerHTML=list.map(function(ex){
    var active=ex.name===current?' active':'';
    var dotClass=ex.connected?'connected':'disconnected';
    var badge=ex.connected?'<span class="ex-badge live">已配置</span>':'<span class="ex-badge nokeys">未配置</span>';
    return '<div class="ex-dropdown-item'+active+'" onclick="selectExchange(\''+ex.name+'\')"><span class="ex-dot '+dotClass+'"></span><span class="ex-name">'+ex.name+'</span>'+badge+'</div>';
  }).join('');
}
function selectExchange(name){
  S_ai2.exchange=name;
  $('ai2-exchange-tag').textContent=name+' ▾';
  closeExchangePicker();
  toast('已切换至 '+name);
}

// Close pickers on Esc
document.addEventListener('keydown',function(e){if(e.key==='Escape'){closeSymbolSearch();closeExchangePicker();}});

function switchAI2BtTab(tab){
  // Toggle saved panel visibility
  var saved=$('ai2-bt-saved-panel');
  if(saved) saved.style.display=(tab==='saved'&&saved.style.display==='none')?'block':'none';
}

function setAI2Interval(interval){
  S_ai2.interval=interval;
  // Update interval button active states
  document.querySelectorAll('#ai2-interval-bar .ai-int-btn').forEach(function(btn){
    btn.classList.toggle('active',btn.dataset.int===interval);
  });
  // Pro v0.1.1 setPeriod doesn't trigger data reload — destroy & recreate
  if(S_ai2Chart){
    try{S_ai2Chart.destroy();}catch(e){}
    S_ai2Chart=null;
  }
  initAI2Chart();
}

function quickBotCreate(type){
  var prompts={
    ai:'生成一个BTCUSDT的1小时趋势跟踪策略，使用EMA金叉死叉+RSI过滤，带2%止损4%止盈',
    grid:'生成一个BTCUSDT的网格交易策略，价格区间64000-72000，10格，每格0.001BTC',
    trend:'生成一个BTCUSDT的趋势跟踪策略，使用MA20/MA60均线系统，顺势加仓',
    arbitrage:'生成一个BTCUSDT-ETHUSDT的跨币种套利策略，监控价差偏离',
    dca:'生成一个BTCUSDT的DCA定投策略，每周定投100U，分批止盈',
    martingale:'生成一个BTCUSDT的马丁格尔策略，初始仓位0.001BTC，倍率1.5，最多5层'
  };
  S_ai2._prompt=prompts[type]||prompts.ai;
  generateAI2Strategy();
}
function quickTradeFromAI(){
  S.page='trade';
  document.querySelectorAll('.nav a').forEach(function(x){x.classList.remove('active')});
  document.querySelector('.nav a[data-page="trade"]').classList.add('active');
  document.querySelectorAll('.page').forEach(function(p){p.classList.remove('active')});
  $('pg-trade').classList.add('active');
  $('tf-symbol').value=S_ai2.symbol;
  initTradeChart();loadTradeChart();refreshOrders();
}

function ai2SetEditor(code){if(window._monacoEditor){window._monacoEditor.setValue(code||'')}else{var el=$('ai2-code-editor');if(el)el.value=code||'';S_ai2._pendingCode=code}}
function ai2GetEditor(){if(window._monacoEditor){return window._monacoEditor.getValue()}var el=$('ai2-code-editor');return el?el.value:''}
function copyAI2Code(){
  var code=ai2GetEditor()||S_ai2.code||'';
  if(!code){toast('No code to copy','err');return}
  navigator.clipboard.writeText(code).then(function(){toast('Copied')});
}
function toggleCodePanel(){
  var drawer=$('ai2-code-drawer');var rail=$('ai2-code-rail');
  if(drawer.style.display==='none'){drawer.style.display='';rail.style.display='none'}
  else{drawer.style.display='none';rail.style.display='flex'}
}
// Indicator toggle for Agent chart
S_ai2.activeIndicators=[];
function ai2ToggleIndicator(name){
  var idx=S_ai2.activeIndicators.indexOf(name);
  if(idx>=0){S_ai2.activeIndicators.splice(idx,1)}else{S_ai2.activeIndicators.push(name)}
  // Update button active state
  document.querySelectorAll('.chart-ind-btn').forEach(function(b){
    if(b.textContent===name)b.classList.toggle('active',idx<0);
  });
  if(S_ai2Chart&&S_ai2Chart.setIndicators){
    var mains=[],subs=[];
    S_ai2.activeIndicators.forEach(function(n){
      if(n==='MACD'||n==='RSI'||n==='KDJ'||n==='VOL')subs.push(n);
      else mains.push(n);
    });
    S_ai2Chart.setIndicators(mains,subs);
  }
}
function ai2ClearIndicators(){
  S_ai2.activeIndicators=[];
  document.querySelectorAll('.chart-ind-btn').forEach(function(b){b.classList.remove('active')});
  if(S_ai2Chart&&S_ai2Chart.setIndicators)S_ai2Chart.setIndicators([],[]);
}
function ai2ToggleGen(){var b=document.getElementById("ai2-gen-body");var a=document.getElementById("ai2-gen-arrow");if(b.style.display==="none"){b.style.display="";a.textContent="▼"}else{b.style.display="none";a.textContent="▶"}}
function ai2Chip(text){document.getElementById("ai-chat-inline").value=text;sendAIInline()}
function markCodeModified(){
  $('ai2-modified-tag').style.display='inline';$('ai2-modified-tag2').style.display='inline';
}
function newAI2Strategy(){ai2SetEditor('');S_ai2.code='';$('ai2-modified-tag').style.display='none';$('ai2-modified-tag2').style.display='none';toast('Cleared');}
function saveAI2Strategy(){
  var code=ai2GetEditor();if(!code){toast('No code to save','err');return}
  S_ai2.saved=S_ai2.saved||[];S_ai2.saved.push({name:'Strategy '+(S_ai2.saved.length+1),code:code,time:Date.now()});
  toast('Strategy saved');renderAI2SavedList();
}
function deleteAI2Strategy(){ai2SetEditor('');S_ai2.code='';S_ai2.saved=[];renderAI2SavedList();toast('Deleted');}
function renderAI2SavedList(){
  var list=S_ai2.saved||[];
  // Update sidebar
  var sb=$('ai2-strat-sidebar');if(sb){
    if(!list.length){sb.innerHTML='<div style=\"padding:8px;color:var(--muted);text-align:center\">暂无策略</div>';return}
    var h='';for(var i=0;i<list.length;i++){var s=list[i];h+='<div style=\"padding:6px 8px;cursor:pointer;border-bottom:1px solid var(--border);font-size:10px\" onclick=\"ai2SetEditor(S_ai2.saved['+i+'].code);S_ai2.code=S_ai2.saved['+i+'].code;loadAI2Chart()\" title=\"'+s.name+'\">'+s.name+'</div>';}
    sb.innerHTML=h;
  }
  // Also update bottom saved panel
  var el=$('ai2-strategy-list');if(!el)return;
  if(!list.length){el.innerHTML='暂无';return}
  var h2='';for(var i=0;i<list.length;i++){var s=list[i];h2+='<div style=\"padding:2px 0;cursor:pointer\" onclick=\"ai2SetEditor(S_ai2.saved['+i+'].code);S_ai2.code=S_ai2.saved['+i+'].code\"><span>'+s.name+'</span></div>';}
  el.innerHTML=h2;
}
// AI Chat
function openAIChat(){$('ai-chat-modal').style.display='flex';}
function closeAIChat(){$('ai-chat-modal').style.display='none';}
function aiChipClick(text){$('ai-chat-inline').value=text;sendAIInline();}
function sendAIInline(){
  var text=$('ai-chat-inline').value.trim();if(!text)return;
  $('ai-chat-inline').value='';S_ai2._prompt=text;generateAI2Strategy();
}
async function sendAIChat(){
  var input=$('ai-chat-input');var text=input.value.trim();if(!text)return;
  closeAIChat();S_ai2._prompt=text;generateAI2Strategy();
}

async function generateAI2Strategy(){
  var prompt=S_ai2._prompt||'';if(!prompt){toast('No prompt');return}
  $('ai2-gen-overlay').style.display='flex';
  $('ai2-modified-tag').style.display='none';$('ai2-modified-tag2').style.display='none';

  try{
    var r=await fetch('/api/ai/generate',{method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({
        symbol:S_ai2.symbol,interval:S_ai2.interval,prompt:prompt,
        risk:'medium',mode:'quick',capital:10000,bars:200
      })
    });
    var d=await r.json();
    if(d.status==='ok'&&(d.strategy_code||d.code)){
      S_ai2.code=d.strategy_code||d.code;
      ai2SetEditor(S_ai2.code);
      $('ai2-gen-overlay').style.display='none';
      setTimeout(function(){runAI2Backtest()},500);
    }else{
      $('ai2-gen-overlay').style.display='none';
      toast(d.detail||'Failed','err');
    }
  }catch(e){
    $('ai2-gen-status').innerHTML='<span style="color:#f85149">网络错误</span> <button class="btn btn-xs btn-r" onclick="fixAI2Code(\'网络错误\')" style="font-size:9px;padding:2px 8px">修复</button>';
  }
  btn.disabled=false;
}

function syntaxHighlightPython(code){
  if(!code)return '';
  var html=code
    .replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;')
    // Strings
    .replace(/(\"\"\"[\s\S]*?\"\"\")/g,'<span class="code-string">$1</span>')
    .replace(/("(?:[^"\\]|\\.)*")/g,'<span class="code-string">$1</span>')
    .replace(/('(?:[^'\\]|\\.)*')/g,'<span class="code-string">$1</span>')
    // Comments
    .replace(/(#.*$)/gm,'<span class="code-comment">$1</span>')
    // Decorators
    .replace(/^(@\w+)/gm,'<span class="code-decorator">$1</span>')
    // Keywords
    .replace(/\b(def|class|return|if|else|elif|for|while|import|from|as|try|except|finally|with|yield|lambda|and|or|not|in|is|None|True|False|pass|break|continue|raise|assert|global|nonlocal)\b/g,'<span class="code-keyword">$1</span>')
    // Numbers
    .replace(/\b(\d+\.?\d*)\b/g,'<span class="code-number">$1</span>')
    // Builtins
    .replace(/\b(print|len|range|int|float|str|list|dict|set|tuple|bool|enumerate|zip|map|filter|sorted|reversed|min|max|sum|abs|round|copy|get|params|df|pd|np|talib|ema_fast|ema_slow|rsi_period|atr_period|rsi_buy_threshold|rsi_sell_threshold|atr_multiplier|volume_multiplier)\b/g,'<span class="code-builtin">$1</span>')
    // Type hints
    .replace(/@param\s+(\w+)\s+(\w+)/g,'<span class="code-decorator">@param</span> <span class="code-keyword">$1</span> <span class="code-type">$2</span>')
    .replace(/@strategy\s+(\w+)\s+(\S+)/g,'<span class="code-decorator">@strategy</span> <span class="code-keyword">$1</span> <span class="code-number">$2</span>');
  return html;
}

// ═══ K-LINE CHART (Canvas) ═══
var S_chart={offset:0,scale:1,maData:[],rsiData:[]};

var S_ai2Chart=null; // KLineChartPro instance

var AI2_PERIODS=[
  {multiplier:1,timespan:'minute',text:'1m'},
  {multiplier:5,timespan:'minute',text:'5m'},
  {multiplier:15,timespan:'minute',text:'15m'},
  {multiplier:1,timespan:'hour',text:'1h'},
  {multiplier:4,timespan:'hour',text:'4h'},
  {multiplier:1,timespan:'day',text:'1d'},
  {multiplier:1,timespan:'week',text:'1w'}
];

function createAI2Datafeed(){
  return {
    searchSymbols: async function(search){
      try{
        var r=await fetch('/api/symbols/search?q='+encodeURIComponent(search||''));
        var symbols=await r.json();
        return (symbols||[]).map(function(s){
          return {ticker:s,name:s.replace('USDT','/USDT'),shortName:s.replace('USDT',''),exchange:'BINANCE'};
        });
      }catch(e){return[];}
    },
    getHistoryKLineData: async function(symbol,period,from,to){
      var interval;
      var ts=period.timespan,m=period.multiplier;
      if(ts==='minute')interval=m+'m';
      else if(ts==='hour')interval=m+'h';
      else if(ts==='day')interval=m+'d';
      else if(ts==='week')interval=m+'w';
      else interval='1h';
      S_ai2.interval=interval;
      try{
        var limit=800;
        if(from&&to&&from>0&&to>from){
          var fromMs=from>10000000000?from:from*1000;
          var toMs=to>10000000000?to:to*1000;
          var barMs=({minute:60000,hour:3600000,day:86400000,week:604800000})[ts]*m||3600000;
          limit=Math.max(limit,Math.min(Math.ceil((toMs-fromMs)/barMs)+50,1500));
        }
        var r=await fetch('/api/klines/'+symbol.ticker+'?interval='+interval+'&limit='+limit);
        var data=await r.json();
        if(!data||!data.length){S_ai2.klines=generateDemoKlines(interval);return S_ai2.klines;}
        var result=data.map(function(b){
          return{timestamp:b.time,open:b.open,high:b.high,low:b.low,close:b.close,volume:b.volume};
        });
        result.sort(function(a,b){return a.timestamp-b.timestamp;});
        S_ai2.klines=result;
        return result;
      }catch(e){S_ai2.klines=generateDemoKlines(interval);return S_ai2.klines;}
    },
    subscribe: function(symbol,period,callback){},
    unsubscribe: function(symbol,period){}
  };
}

var _monacoInited=false,_monacoLoading=false;
function initMonacoEditor(){
  if(_monacoInited||_monacoLoading)return;
  var container=$('ai2-code-editor');
  if(!container||container.clientHeight<1){setTimeout(initMonacoEditor,200);return}
  _monacoLoading=true;
  require.config({paths:{vs:'https://cdn.jsdelivr.net/npm/monaco-editor@0.45.0/min/vs'}});
  require(['vs/editor/editor.main'],function(){
    _monacoInited=true;_monacoLoading=false;
    window._monacoEditor=monaco.editor.create(container,{
      value:S_ai2.code||S_ai2._pendingCode||'',
      language:'python',
      theme:'vs',
      fontSize:12,
      fontFamily:'Cascadia Code, Consolas, monospace',
      minimap:{enabled:false},
      automaticLayout:true,
      scrollBeyondLastLine:false,
      tabSize:4,
      lineNumbers:'on',
      renderWhitespace:'selection',
    });
    window._monacoEditor.onDidChangeModelContent(function(){markCodeModified()});
    if(S_ai2._pendingCode){window._monacoEditor.setValue(S_ai2._pendingCode);S_ai2._pendingCode=null}
  });
}
function disposeMonacoEditor(){
  if(window._monacoEditor){window._monacoEditor.dispose();window._monacoEditor=null;_monacoInited=false}
}

function initAI2Chart(){
  if(S_ai2Chart)return;
  var container=$('ai2-chart-container');
  if(!container)return;
  if(container.clientHeight<50){
    setTimeout(function(){initAI2Chart();},300);
    return;
  }
  var ind=S_ai2.indicators||{};
  var mainInds=['EMA','MA'];
  var subInds=[];
  if(ind.type==='ema_cross'){mainInds=['EMA','MA'];}
  else if(ind.type==='rsi'){mainInds=['MA'];subInds=['RSI'];}
  else if(ind.type==='macd'){mainInds=['MA'];subInds=['MACD'];}
  else if(ind.type==='bollinger'){mainInds=['BOLL','MA'];}
  else if(ind.type==='atr_breakout'){mainInds=['MA'];subInds=['VOL'];}

  S_ai2Chart=new klinechartspro.KLineChartPro({
    container:'ai2-chart-container',
    styles:{
      grid:{color:'rgba(48,54,61,.3)'},
      candle:{type:'candle_solid',bar:{upColor:'#3fb950',downColor:'#f85149',noChangeColor:'#8b949e'}}
    },
    theme:'light',
    locale:'zh-CN',
    drawingBarVisible:true,
    symbol:{ticker:S_ai2.symbol,name:S_ai2.symbol.replace('USDT','/USDT'),shortName:S_ai2.symbol.replace('USDT','')},
    period:AI2_PERIODS.find(function(p){return p.text===S_ai2.interval;})||{multiplier:1,timespan:'hour',text:'1h'},
    periods:AI2_PERIODS,
    mainIndicators:mainInds,
    subIndicators:subInds,
    datafeed:createAI2Datafeed()
  });
  // Re-apply trade markers after chart recreation
  var bt=S_ai2.backtestResult;
  if(bt&&bt._trades&&bt._trades.length){
    setTimeout(function(){markTradesOnChart(bt._trades);},1200);
  }
}

function loadAI2Chart(){
  if(S_ai2Chart){
    try{S_ai2Chart.destroy();}catch(e){}
    S_ai2Chart=null;
  }
  initAI2Chart();
}

function generateDemoKlines(interval){
  var klines=[];
  var price=76000;var now=Date.now();
  var intervalMs={'1m':60000,'5m':300000,'15m':900000,'1h':3600000,'4h':14400000,'1d':86400000,'1w':604800000};
  var step=intervalMs[interval]||3600000;
  for(var i=199;i>=0;i--){
    var t=now-i*step;
    var change=(Math.random()-0.48)*800;
    var open=price;var close=price+change;
    var high=Math.max(open,close)+Math.random()*300;
    var low=Math.min(open,close)-Math.random()*300;
    var volume=500+Math.random()*2000;
    klines.push({open:open,close:close,high:high,low:low,volume:volume,timestamp:t});
    price=close;
  }
  return klines;
}

// ═══ TECHNICAL INDICATORS ═══
function btCalcEMA(closes,period){
  var ema=[],k=2/(period+1);
  for(var i=0;i<closes.length;i++){
    if(i===0)ema.push(closes[i]);
    else ema.push(closes[i]*k+ema[i-1]*(1-k));
  }
  return ema;
}

function btCalcRSI(closes,period){
  var rsi=[],gains=0,losses=0;
  for(var i=1;i<closes.length;i++){
    var diff=closes[i]-closes[i-1];
    if(i<period){
      if(diff>0)gains+=diff;else losses-=diff;
      rsi.push(null);
    }else if(i===period){
      if(diff>0)gains+=diff;else losses-=diff;
      gains/=period;losses/=period;
      var rs=losses===0?100:gains/losses;
      rsi.push(100-100/(1+rs));
    }else{
      gains=(gains*(period-1)+(diff>0?diff:0))/period;
      losses=(losses*(period-1)+(diff<0?-diff:0))/period;
      var rs2=losses===0?100:gains/losses;
      rsi.push(100-100/(1+rs2));
    }
  }
  return rsi;
}

function btCalcMACD(closes,fast,slow,signal){
  var ef=btCalcEMA(closes,fast),es=btCalcEMA(closes,slow);
  var macd=[],sig=[],hist=[];
  for(var i=0;i<closes.length;i++){
    macd.push(ef[i]-es[i]);
    if(i===0){sig.push(macd[i]);hist.push(0);}
    else{
      var k=2/(signal+1);
      sig.push(macd[i]*k+sig[i-1]*(1-k));
      hist.push(macd[i]-sig[i]);
    }
  }
  return{macd:macd,signal:sig,histogram:hist};
}

function btCalcBollinger(closes,period,stdDev){
  var mid=[],up=[],lo=[];
  for(var i=0;i<closes.length;i++){
    if(i<period-1){mid.push(null);up.push(null);lo.push(null);continue;}
    var slice=closes.slice(i-period+1,i+1);
    var avg=slice.reduce(function(a,b){return a+b;},0)/period;
    var v=slice.reduce(function(a,b){return a+(b-avg)*(b-avg);},0)/period;
    var std=Math.sqrt(v);
    mid.push(avg);up.push(avg+stdDev*std);lo.push(avg-stdDev*std);
  }
  return{middle:mid,upper:up,lower:lo};
}

function btCalcATR(klines,period){
  var tr=[],atr=[];
  for(var i=1;i<klines.length;i++){
    var h=klines[i].high,l=klines[i].low,pc=klines[i-1].close;
    tr.push(Math.max(h-l,Math.abs(h-pc),Math.abs(l-pc)));
  }
  for(var i=0;i<tr.length;i++){
    if(i===0)atr.push(tr[i]);
    else atr.push((atr[i-1]*(period-1)+tr[i])/period);
  }
  return atr;
}

// ═══ STRATEGY PARSER ═══
function parseStrategyParams(code){
  var params={};
  if(!code)return params;
  // Parse params.get('name', default) patterns
  var re=/params\.get\s*\(\s*['\"](\w+)['\"]\s*,\s*([0-9.]+)/g,m;
  while((m=re.exec(code))!==null){params[m[1]]=parseFloat(m[2]);}
  // Parse self.name = value patterns (only strategy params with underscore)
  var re2=/self\.(\w+)\s*=\s*params\.get\s*\(\s*['\"]\1['\"]\s*,\s*([0-9.]+)/g;
  while((m=re2.exec(code))!==null){params[m[1]]=parseFloat(m[2]);}
  // Parse bare self.param = number assignments
  var re3=/self\.(\w+)\s*=\s*([0-9.]+)/g;
  while((m=re3.exec(code))!==null){
    if(!(m[1] in params)&&m[1].indexOf('_')>=0)params[m[1]]=parseFloat(m[2]);
  }
  return params;
}

function detectStrategyType(code){
  if(!code)return'ema_cross';
  var c=code.toLowerCase();
  if(c.indexOf('macd')>=0)return'macd';
  if(c.indexOf('rsi')>=0&&c.indexOf('ema')<0&&c.indexOf('moving')<0)return'rsi';
  if(c.indexOf('bollinger')>=0||c.indexOf('boll')>=0)return'bollinger';
  if(c.indexOf('grid')>=0)return'grid';
  if(c.indexOf('atr')>=0&&c.indexOf('ema')<0)return'atr_breakout';
  if(c.indexOf('breakout')>=0)return'atr_breakout';
  return'ema_cross';
}

// ═══ STRATEGY BACKTEST ENGINE ═══
function runStrategyBacktest(type,params,klines,capital,fee,slippage){
  var closes=klines.map(function(k){return k.close;});
  var eq=capital,peak=capital,pos=null,maxDD=0,wins=0,total=0;
  var trades=[],curve=[],signals=[];
  var warmup=Math.max(30,Math.floor(closes.length*0.1));

  var tp=(params.tp_ratio||2.0)/100;
  var sl=(params.sl_ratio||1.0)/100;
  var posPct=0.3;

  // ── Signal arrays (computed lazily per strategy type) ──
  var emaFast,emaSlow,rsi,macd,bb,atr;

  if(type==='ema_cross'){
    emaFast=btCalcEMA(closes,params.ema_fast||9);
    emaSlow=btCalcEMA(closes,params.ema_slow||21);
  }else if(type==='rsi'){
    rsi=btCalcRSI(closes,params.rsi_period||14);
    var rsiBuy=params.rsi_buy_threshold||30;
    var rsiSell=params.rsi_sell_threshold||70;
  }else if(type==='macd'){
    macd=btCalcMACD(closes,params.macd_fast||12,params.macd_slow||26,params.macd_signal||9);
  }else if(type==='bollinger'){
    bb=btCalcBollinger(closes,params.bb_period||20,params.bb_std||2);
  }else if(type==='atr_breakout'){
    atr=btCalcATR(klines,params.atr_period||14);
    var atrMult=params.atr_multiplier||2;
  }else{
    emaFast=btCalcEMA(closes,9);emaSlow=btCalcEMA(closes,21);type='ema_cross';
  }

  for(var i=warmup;i<closes.length;i++){
    var price=closes[i];
    var ts=klines[i].timestamp||klines[i].time||(Date.now()-i*60000);
    var signal=0; // -1=sell, 0=none, +1=buy

    // ── Generate signals per strategy type ──
    if(type==='ema_cross'){
      if(emaFast[i]>emaSlow[i]&&emaFast[i-1]<=emaSlow[i-1])signal=1;
      else if(emaFast[i]<emaSlow[i]&&emaFast[i-1]>=emaSlow[i-1])signal=-1;
    }else if(type==='rsi'){
      if(rsi[i]!==null&&rsi[i]<rsiBuy&&rsi[i-1]>=rsiBuy)signal=1;
      else if(rsi[i]!==null&&rsi[i]>rsiSell&&rsi[i-1]<=rsiSell)signal=-1;
    }else if(type==='macd'){
      if(macd.macd[i]>macd.signal[i]&&macd.macd[i-1]<=macd.signal[i-1])signal=1;
      else if(macd.macd[i]<macd.signal[i]&&macd.macd[i-1]>=macd.signal[i-1])signal=-1;
    }else if(type==='bollinger'){
      if(bb.lower[i]!==null&&price<=bb.lower[i])signal=1;
      else if(bb.upper[i]!==null&&price>=bb.upper[i])signal=-1;
    }else if(type==='atr_breakout'){
      if(!atr[i])continue;
      var prevC=closes[i-1];
      if(price>prevC+atr[i]*atrMult)signal=1;
      else if(price<prevC-atr[i]*atrMult)signal=-1;
    }

    // ── Execute orders ──
    if(!pos&&signal!==0){
      pos={entry:price,side:signal===1?'long':'short',qty:eq*posPct/price,entryIdx:i,entryTime:ts};
      total++;signals.push({type:signal===1?'buy':'sell',price:price,idx:i,ts:ts});
    }

    // ── Exit logic ──
    if(pos){
      var pnlPct=(price-pos.entry)/pos.entry*(pos.side==='long'?1:-1);
      var exitSignal=(pos.side==='long'&&signal===-1)||(pos.side==='short'&&signal===1);
      // Also exit on TP/SL
      if(pnlPct<=-sl||pnlPct>=tp||exitSignal){
        var posVal=eq*posPct;
        eq+=posVal*pnlPct;eq-=posVal*fee;eq-=posVal*slippage;
        if(pnlPct>0)wins++;
        trades.push({entry:pos.entry,exit:price,side:pos.side,pnlPct:pnlPct*100,
          entryTime:pos.entryTime,exitTime:ts,entryIdx:pos.entryIdx,exitIdx:i});
        signals.push({type:pos.side==='long'?'sell':'buy',price:price,idx:i,ts:ts});
        pos=null;
      }
    }

    curve.push({i:i,v:eq+(pos?pos.qty*price*(pos.side==='long'?1:-1):0)});
    var cv=curve[curve.length-1].v;
    if(cv>peak)peak=cv;
    var dd=(peak-cv)/peak;if(dd>maxDD)maxDD=dd;
  }

  // Close open position at last price
  var lastPrice=closes[closes.length-1];
  var finalEq=eq+(pos?pos.qty*lastPrice*(pos.side==='long'?1:-1):0);

  return{
    equity:finalEq,trades:trades,signals:signals,equityCurve:curve,
    totalTrades:total,wins:wins,maxDD:maxDD,peak:peak
  };
}

// ═══ AI2 BACKTEST ═══
function updateAI2BtDates(){
  var range=$('ai2-bt-range').value;
  var end=new Date();
  var start=new Date();
  var months={M:1,Y:12};
  var n=parseInt(range);
  if(range.indexOf('M')>=0)start.setMonth(end.getMonth()-n);
  else start.setFullYear(end.getFullYear()-n);
  $('ai2-bt-start').value=start.toISOString().split('T')[0];
  $('ai2-bt-end').value=end.toISOString().split('T')[0];
}

async function runAI2Backtest(){
  var btn=$('ai2-bt-run-btn');btn.disabled=true;
  $('ai2-bt-status').textContent='回测运行中...';

  var capital=parseFloat($('ai2-bt-capital').value)||10000;
  var fee=parseFloat($('ai2-bt-fee').value)/100||0.0002;
  var slippage=parseFloat($('ai2-bt-slippage').value)/100||0.0002;
  var klines=S_ai2.klines;
  if(!klines||!klines.length){generateDemoKlines(S_ai2.interval);klines=S_ai2.klines;}

  // Parse strategy parameters and detect type from generated code
  var params=parseStrategyParams(S_ai2.code);
  var stratType=detectStrategyType(S_ai2.code);

  // Run strategy-driven backtest
  var bt=runStrategyBacktest(stratType,params,klines,capital,fee,slippage);

  var totRet=(bt.equity-capital)/capital*100;
  var sharpe=bt.maxDD>0?totRet/(bt.maxDD*100+0.01):totRet;
  var winRate=bt.totalTrades>0?(bt.wins/bt.totalTrades*100):0;

  S_ai2.backtestResult={totRet:totRet,annRet:totRet*2,maxDD:bt.maxDD*100,sharpe:sharpe,winRate:winRate,totalTrades:bt.totalTrades,equity:bt.equity,equityCurve:bt.equityCurve,stratType:stratType,params:params,_trades:bt.trades};
  $('ai2bt-totret').textContent=F(totRet,2)+'%';$('ai2bt-totret').className='bt-m-val '+(totRet>=0?'g':'r');
  $('ai2bt-annret').textContent=F(totRet*2,2)+'%';
  $('ai2bt-maxdd').textContent=F(bt.maxDD*100,2)+'%';$('ai2bt-maxdd').className='bt-m-val r';
  $('ai2bt-sharpe').textContent=F(sharpe,2);
  $('ai2bt-winrate').textContent=F(winRate,1)+'%';
  $('ai2bt-trades').textContent=bt.totalTrades;
  var avgWin=bt.trades.length>0?bt.trades.reduce(function(a,t){return a+t.pnlPct;},0)/bt.trades.length:0;
  $('ai2bt-plratio').textContent=F(avgWin,2)+'%';
  $('ai2bt-equity').textContent='$'+FUSD(bt.equity);

  var stratLabel={'ema_cross':'EMA交叉','rsi':'RSI','macd':'MACD','bollinger':'布林带','atr_breakout':'ATR突破','grid':'网格'}[stratType]||stratType;
  $('ai2-bt-status').innerHTML='回测完成 ('+stratLabel+') - '+bt.totalTrades+'笔交易'+(bt.totalTrades===0||totRet<-20?' <button class="btn btn-xs btn-r" onclick="fixAI2Code(\'回测异常: '+(bt.totalTrades===0?'0笔交易':'收益 '+F(totRet,1)+'%')+'\')" style="font-size:9px;padding:2px 8px">修复</button>':'');
  btn.disabled=false;
  switchAI2BtTab('results');
  // Save to history
  S_ai2.btHistory=S_ai2.btHistory||[];
  S_ai2.btHistory.unshift({time:Date.now(),ret:totRet,dd:maxDD,sharpe:sharpe,win:winRate,trades:numTrades});
  if(S_ai2.btHistory.length>20)S_ai2.btHistory.pop();
  renderAI2BtHistory();
  setTimeout(function(){renderAI2BtChart();},100);
}
function renderAI2BtHistory(){
  var el=$('ai2-bt-history-list');if(!el)return;
  var h=S_ai2.btHistory||[];if(!h.length){el.innerHTML='No backtest history';return}
  var out='';
  for(var i=0;i<h.length;i++){var r=h[i];var cl=r.ret>=0?'var(--green)':'var(--red)';
    out+='<div style=\"display:flex;justify-content:space-between;padding:3px 0;border-bottom:1px solid var(--border);font-size:10px\"><span>'+new Date(r.time).toLocaleString()+'</span><span style=\"color:'+cl+'">'+FX(r.ret,2)+'%</span><span>DD '+FX(r.dd,2)+'%</span><span>'+r.trades+' trades</span></div>';}
  el.innerHTML=out;
}

function markTradesOnChart(trades){
  var container=$('ai2-chart-container');
  if(!container)return;

  // Get or create signal overlay
  var overlay=$('ai2-signal-overlay');
  if(!overlay){
    overlay=document.createElement('div');
    overlay.id='ai2-signal-overlay';
    overlay.style.cssText='position:absolute;top:0;left:0;width:100%;height:100%;pointer-events:none;z-index:10;';
    if(window.getComputedStyle(container).position==='static')container.style.position='relative';
    container.appendChild(overlay);
  }
  // Clear old markers
  overlay.innerHTML='';

  if(!trades||!trades.length)return;
  var klines=S_ai2.klines;
  if(!klines||!klines.length)return;

  var W=container.clientWidth,H=container.clientHeight;
  var marginR=60,marginB=30,marginT=10,marginL=5;
  var cw=W-marginL-marginR,ch=H-marginT-marginB;

  // Visible price range from all loaded klines
  var allP=[];
  klines.forEach(function(k){allP.push(k.high);allP.push(k.low);});
  var maxP=Math.max.apply(null,allP),minP=Math.min.apply(null,allP);
  var range=maxP-minP||1;
  // Show last N bars (cover the full chart viewport)
  var visCnt=Math.min(klines.length,Math.max(100,Math.floor(cw/4.5)));
  var start=klines.length-visCnt;

  trades.forEach(function(t){
    var idx=t.entryIdx||t.exitIdx||0;
    if(idx<start)return;
    var rx=(idx-start)/visCnt;
    var x=marginL+rx*cw;
    var px=t.side==='long'?t.entry:t.exit;
    if(!px&&t.entry)px=t.entry;
    if(!px)return;
    var y=marginT+((maxP-px)/range)*ch;
    if(y<-10||y>H+10)return;

    // Draw a visible circle marker
    var mk=document.createElement('div');
    var isWin=t.pnlPct>0;
    var isBuy=t.side==='long';
    var color=isBuy?'#3fb950':'#f85149';
    mk.style.cssText='position:absolute;left:'+x+'px;top:'+y+'px;'+
      'width:10px;height:10px;border-radius:50%;background:'+color+';'+
      'border:2px solid #fff;box-shadow:0 0 6px '+color+';'+
      'transform:translate(-50%,-50%);';
    mk.title=(isBuy?'Buy @ ':'Sell @ ')+px.toFixed(1)+' | PnL: '+t.pnlPct.toFixed(2)+'%';
    overlay.appendChild(mk);
  });

  setTimeout(function(){markTradesOnBtChart(trades);},150);
}

function flagStrategyIndicators(stratType,params){
  S_ai2.indicators={type:stratType,params:params};
  // Recreate chart with appropriate indicators
  if(S_ai2Chart){
    try{S_ai2Chart.destroy();}catch(e){}
    S_ai2Chart=null;
  }
  // Clear existing HTML signal overlay
  var oldOv=$('ai2-signal-overlay');
  if(oldOv)oldOv.remove();
  initAI2Chart();
  // Re-apply trade markers after chart loads
  var bt=S_ai2.backtestResult;
  if(bt&&bt._trades&&bt._trades.length){
    setTimeout(function(){markTradesOnChart(bt._trades);},1200);
  }
}

function markTradesOnBtChart(trades){
  var canvas=$('ai2-bt-chart');
  if(!canvas||!trades.length)return;
  var curve=S_ai2.backtestResult?S_ai2.backtestResult.equityCurve:null;
  if(!curve||!curve.length)return;
  try {
    var container=canvas.parentElement;
    canvas.width=container.clientWidth;
    canvas.height=180;
    var ctx=canvas.getContext('2d');
    renderAI2BtChart_data(ctx, canvas.width, canvas.height, curve, trades);
  } catch(e) {}
}

function renderAI2BtChart_data(ctx,W,H,curve,trades){
  ctx.clearRect(0,0,W,H);
  ctx.fillStyle='#f6f8fa';ctx.fillRect(0,0,W,H);
  if(!curve||!curve.length)return;
  var vals=curve.map(function(p){return p.v});
  var minV=Math.min.apply(null,vals),maxV=Math.max.apply(null,vals);
  var pad=(maxV-minV)*0.05;minV-=pad;maxV+=pad;
  var n=curve.length;
  // Grid
  ctx.strokeStyle='#eaeef2';ctx.lineWidth=0.5;
  for(var i=0;i<4;i++){
    var gy=H*i/3;
    ctx.beginPath();ctx.moveTo(10,gy);ctx.lineTo(W-5,gy);ctx.stroke();
  }
  // Equity line
  ctx.strokeStyle='#3fb950';ctx.lineWidth=2;ctx.beginPath();
  var first=true;
  for(var i=0;i<n;i++){
    var px=10+(W-20)*i/n;
    var py=H-(curve[i].v-minV)/(maxV-minV)*H;
    if(first){ctx.moveTo(px,py);first=false}
    else ctx.lineTo(px,py);
  }
  ctx.stroke();
  // Fill area
  ctx.lineTo(10+(W-20)*(n-1)/n,H);ctx.lineTo(10,H);ctx.closePath();
  ctx.fillStyle='rgba(63,185,80,.08)';ctx.fill();
  // Trade markers on equity curve
  if(trades&&trades.length){
    trades.forEach(function(t){
      var idx=t.exitIdx||t.entryIdx||0;
      var ex=10+(W-20)*idx/n;
      var ev=H-(curve[Math.min(idx,n-1)].v-minV)/(maxV-minV)*H;
      var isWin=t.pnlPct>0;
      ctx.fillStyle=isWin?'#3fb950':'#f85149';
      ctx.beginPath();ctx.arc(ex,ev,3,0,Math.PI*2);ctx.fill();
      ctx.fillStyle=isWin?'#3fb950':'#f85149';
      ctx.font='bold 9px sans-serif';
      ctx.fillText(isWin?'▲':'▼',ex-5,ev-6);
    });
  }
  ctx.fillStyle='#484f58';ctx.font='9px sans-serif';
  ctx.fillText('$'+minV.toFixed(0),12,H-4);
  ctx.fillText('$'+maxV.toFixed(0),12,14);
}

function renderAI2BtChart(){
  var canvas=$('ai2-bt-chart');
  if(!canvas)return;
  var container=canvas.parentElement;
  canvas.width=container.clientWidth;
  canvas.height=180;
  var ctx=canvas.getContext('2d');
  var curve=S_ai2.backtestResult?S_ai2.backtestResult.equityCurve:null;
  renderAI2BtChart_data(ctx, canvas.width, canvas.height, curve, null);
}

async function runAI2Tuning(){
  var code=S_ai2.code||($('ai2-code-display')?$('ai2-code-display').textContent:'');
  if(!code||code.indexOf('BTC趋势动量策略')>=0&&!S_ai2.code){$('ai2-tuning-status').textContent='请先生成策略代码';return}
  $('ai2-tuning-status').textContent='Agent优化中...';
  var btn=$('ai2-tuning-btn');btn.disabled=true;

  var bt=S_ai2.backtestResult;
  var btSummary='';
  if(bt){
    btSummary='【回测数据】总收益:'+F(bt.totRet,2)+'% 年化:'+F(bt.annRet,2)+'% 最大回撤:'+F(bt.maxDD,2)+'% 夏普:'+F(bt.sharpe,2)+' 胜率:'+F(bt.winRate,1)+'% 交易数:'+bt.totalTrades;
  }

  try{
    var r=await fetch('/api/agent/chat',{method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({
        message:'请分析以下策略的回测结果，优化策略代码以提升收益、降低回撤。\n\n【交易对】'+S_ai2.symbol+' 【周期】'+S_ai2.interval+'\n'+btSummary+'\n\n【当前策略代码】\n```python\n'+code+'\n```\n\n请只返回优化后的完整Python策略代码（用```python```包裹），不要其他解释。'
      })
    });
    var d=await r.json();
    if(d.reply){
      var fixed=d.reply;
      var codeMatch=fixed.match(/```(?:python)?\s*([\s\S]*?)```/i);
      if(codeMatch){fixed=codeMatch[1].trim();}
      S_ai2.code=fixed;
      ai2SetEditor(S_ai2.code)=syntaxHighlightPython(fixed);
      var result='<div style="background:var(--bg);padding:10px;border-radius:6px;font-size:11px;line-height:1.6">策略代码已优化，以下为优化后代码</div>';
      $('ai2-tuning-result').innerHTML=result;
      $('ai2-tuning-status').textContent='优化完成，重新回测中...';
      setTimeout(function(){runAI2Backtest();},300);
    }else{
      $('ai2-tuning-result').innerHTML='<div style="color:#f85149;font-size:11px">优化失败，请重试</div>';
      $('ai2-tuning-status').textContent='优化失败';
    }
  }catch(e){
    $('ai2-tuning-status').textContent='优化失败';
  }
  btn.disabled=false;
}

// ═══ Saved Agent Strategies ═══
var S_ai2_strategies=[];

function saveAgentStrategy(){
  var code=S_ai2.code||($('ai2-code-display')?$('ai2-code-display').textContent:'');
  if(!code||code.indexOf('BTC趋势动量策略')>=0&&!S_ai2.code){toast('请先生成策略再保存','err');return}
  var name=prompt('策略名称：',S_ai2.symbol+' '+S_ai2.interval+' 策略 '+(S_ai2_strategies.length+1));
  if(!name)return;
  var strategy={
    id:'astrat_'+Date.now(),
    name:name,
    code:code,
    symbol:S_ai2.symbol,
    interval:S_ai2.interval,
    backtestResult:S_ai2.backtestResult||null,
    created:new Date().toISOString().split('T')[0]
  };
  S_ai2_strategies.unshift(strategy);
  try{localStorage.setItem('ai2_strategies',JSON.stringify(S_ai2_strategies));}catch(e){}
  renderAgentStrategies();
  toast('策略已保存：'+name);
}

function loadAgentStrategies(){
  try{
    var saved=localStorage.getItem('ai2_strategies');
    if(saved){S_ai2_strategies=JSON.parse(saved);}
  }catch(e){S_ai2_strategies=[];}
  renderAgentStrategies();
}

function renderAgentStrategies(){
  var container=$('ai2-strategy-list');
  if(!container)return;
  if(!S_ai2_strategies.length){
    container.innerHTML='<div class="ai2-strategy-empty">暂无保存的策略，生成策略后可点击"保存当前策略"保存到此处</div>';
    return;
  }
  var html='';
  S_ai2_strategies.forEach(function(s,i){
    var metrics='';
    if(s.backtestResult){
      var r=s.backtestResult;
      var cl=r.totRet>=0?'g':'r';
      metrics='<span class="ai2-strat-metric '+cl+'">收益 '+F(r.totRet,1)+'%</span>'+
              '<span class="ai2-strat-metric">胜率 '+F(r.winRate,1)+'%</span>'+
              '<span class="ai2-strat-metric r">回撤 '+F(r.maxDD,2)+'%</span>';
    }
    html+='<div class="ai2-strategy-item">'+
      '<div class="ai2-strat-info">'+
        '<span class="ai2-strat-name">'+s.name+'</span>'+
        '<span class="ai2-strat-meta">'+s.symbol+' | '+s.interval+' | '+s.created+'</span>'+
        '<div style="margin-top:2px">'+metrics+'</div>'+
      '</div>'+
      '<div class="ai2-strat-actions">'+
        '<button class="btn btn-xs btn-g" onclick="applyAgentStrategy('+i+')" style="font-size:9px;padding:2px 8px">加载</button>'+
        '<button class="btn btn-xs btn-r" onclick="deleteAgentStrategy('+i+')" style="font-size:9px;padding:2px 8px">删除</button>'+
      '</div>'+
    '</div>';
  });
  container.innerHTML=html;
}

function applyAgentStrategy(idx){
  var s=S_ai2_strategies[idx];
  if(!s)return;
  S_ai2.code=s.code;
  S_ai2.symbol=s.symbol;
  S_ai2.interval=s.interval;
  S_ai2.backtestResult=s.backtestResult;
  ai2SetEditor(S_ai2.code)=syntaxHighlightPython(s.code);
  $('ai2-symbol-badge').textContent=s.symbol.replace('USDT','/USDT');
  setAI2Interval(s.interval);
  if(s.backtestResult){
    $('ai2bt-totret').textContent=F(s.backtestResult.totRet,2)+'%';
    $('ai2bt-totret').className='bt-m-val '+(s.backtestResult.totRet>=0?'g':'r');
    $('ai2bt-annret').textContent=F(s.backtestResult.annRet,2)+'%';
    $('ai2bt-maxdd').textContent=F(s.backtestResult.maxDD,2)+'%';
    $('ai2bt-sharpe').textContent=F(s.backtestResult.sharpe,2);
    $('ai2bt-winrate').textContent=F(s.backtestResult.winRate,1)+'%';
    $('ai2bt-trades').textContent=s.backtestResult.totalTrades;
    $('ai2bt-equity').textContent='$'+FUSD(s.backtestResult.equity);
    switchAI2BtTab('results');
    setTimeout(function(){renderAI2BtChart();},100);
  }
  toast('已加载策略：'+s.name);
}

function deleteAgentStrategy(idx){
  var s=S_ai2_strategies[idx];
  if(!s||!confirm('确定删除策略 "'+s.name+'" 吗？'))return;
  S_ai2_strategies.splice(idx,1);
  try{localStorage.setItem('ai2_strategies',JSON.stringify(S_ai2_strategies));}catch(e){}
  renderAgentStrategies();
  toast('已删除：'+s.name);
}

// ═══ Auto-fix on Error ═══
var S_ai2_fixing=false;

async function fixAI2Code(errorMsg){
  if(S_ai2_fixing)return;
  var code=S_ai2.code||($('ai2-code-display')?$('ai2-code-display').textContent:'');
  if(!code){toast('没有可修复的代码','err');return}
  S_ai2_fixing=true;
  $('ai2-gen-status').textContent='AI修复中...';
  var btn=$('ai2-gen-btn');if(btn)btn.disabled=true;
  try{
    var r=await fetch('/api/agent/chat',{method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({
        message:'以下策略代码在回测时出现错误，请直接修复代码并返回完整的修复后代码。\n\n【错误信息】\n'+errorMsg+'\n\n【当前策略代码】\n```python\n'+code+'\n```\n\n请只返回修复后的完整Python策略代码（用```python```包裹），不要其他解释。'
      })
    });
    var d=await r.json();
    if(d.reply){
      var fixed=d.reply;
      var codeMatch=fixed.match(/```(?:python)?\s*([\s\S]*?)```/i);
      if(codeMatch){fixed=codeMatch[1].trim();}
      S_ai2.code=fixed;
      ai2SetEditor(S_ai2.code)=syntaxHighlightPython(fixed);
      $('ai2-gen-status').textContent='代码已修复，重新回测中...';
      setTimeout(function(){runAI2Backtest();},300);
    }else{
      $('ai2-gen-status').textContent='修复失败，请手动修改';
    }
  }catch(e){
    $('ai2-gen-status').textContent='修复请求失败';
  }
  S_ai2_fixing=false;
  if(btn)btn.disabled=false;
}

// ═══ Account Widget ═══
function toggleAccountWidget(){
  var w=$('account-widget');
  if(!w)return;
  w.classList.toggle('collapsed');
  var icon=$('afw-toggle-icon');
  if(icon)icon.innerHTML=w.classList.contains('collapsed')?'&#x25BC;':'&#x25B2;';
}

// ═══ Chart helpers ═══
function calcEMA(klines,period){var result=[];var k=2/(period+1);var ema=klines[0].close;for(var i=0;i<klines.length;i++){if(i===0)ema=klines[i].close;else ema=klines[i].close*k+ema*(1-k);result.push({i:i,v:ema});}return result;}
function calcRSI(klines,period){var result=[],gains=[],losses=[];for(var i=1;i<klines.length;i++){var diff=klines[i].close-klines[i-1].close;gains.push(diff>0?diff:0);losses.push(diff<0?-diff:0);}var avgGain=gains.slice(0,period).reduce(function(a,b){return a+b},0)/period;var avgLoss=losses.slice(0,period).reduce(function(a,b){return a+b},0)/period;for(var i=0;i<period;i++)result.push({i:i,v:50});for(var i=period;i<klines.length;i++){if(i-period<gains.length){avgGain=(avgGain*(period-1)+gains[i-1])/period;avgLoss=(avgLoss*(period-1)+losses[i-1])/period;}var rs=avgLoss===0?100:avgGain/avgLoss;result.push({i:i,v:100-100/(1+rs)});}return result;}
function drawLine(ctx,data,color,xFn,yFn){if(!data||data.length<2)return;ctx.strokeStyle=color;ctx.lineWidth=1.2;ctx.beginPath();var first=true;for(var i=0;i<data.length;i++){var px=xFn(data[i].i),py=yFn(data[i]);if(first){ctx.moveTo(px,py);first=false}else ctx.lineTo(px,py);}ctx.stroke();}

// ═══ Page initialization hooks ═══
var origSwitchSubPage=switchSubPage;
switchSubPage=function(sub){
  origSwitchSubPage(sub);
  if(sub==='ai'){
    updateAI2BtDates();
    if(typeof loadAgentStrategies==='function')loadAgentStrategies();
  }
};

// ═══════════════════════════════════════════════ TOGGLE SWITCH UTILITY ═══════════════════════════════
function toggleSwitch(el){
  if(!el)return;
  el.classList.toggle('active');
  var input=el.nextElementSibling;
  if(input&&input.classList&&input.classList.contains('toggle-input')){
    input.value=el.classList.contains('active')?'1':'0';
  }
}
// Event delegation fallback for dynamically created toggles without onclick
document.addEventListener('click',function(e){
  var el=e.target;
  while(el&&el!==document.body){
    if(el.classList&&el.classList.contains('toggle-switch'))break;
    el=el.parentElement;
  }
  if(!el||el===document.body)return;
  if(el.hasAttribute('onclick'))return; // let inline handler handle it
  e.preventDefault();
  toggleSwitch(el);
  if(el.id==='sk-protection-toggle')saveGlobalSettings();
});

// ═══════════════════════════════════════════════ INIT ═══════════════════════════════════════════════
syncLangDOM();
loadSettings();
	loadUISettings();
wsConnect();
refreshDash();
refreshOrders();
setInterval(refreshDash,10000);
setInterval(refreshOrders,8000);
</script>

<!-- ══════ Strategy Form Modal ══════ -->
<div id="sf-modal" class="modal-overlay">
<div class="modal wide">
<div id="sf-content"></div>
</div>
</div>

<!-- ══════ Conditions Modal ══════ -->
<div id="cond-modal" class="modal-overlay">
<div class="modal wide">
<div id="cond-content"></div>
</div>
</div>

<!-- ══════ TP Settings Modal ══════ -->
<div id="tp-modal" class="modal-overlay">
<div class="modal">
<div id="tp-content"></div>
</div>
</div>

<!-- ══════ Delete Confirmation Modal ══════ -->
<div id="del-modal" class="modal-overlay">
<div class="modal">
<h3 id="del-msg"></h3>
<div class="btn-row">
<button class="btn" onclick="$('del-modal').classList.remove('show');deleteId=null">Cancel</button>
<button class="btn btn-r" onclick="doDelete()">Delete</button>
</div>
</div>
</div>

<!-- ══════ Floating Agent Chat ══════ -->
<div id="agent-ball" title="AI Agent对话 — 拖拽移动 · 点击打开" >
  <div class="ball-pulse"></div>
  <svg viewBox="0 0 24 24"><path d="M20 2H4c-1.1 0-2 .9-2 2v18l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zm0 14H5.17L4 17.17V4h16v12zM7 9h10v2H7V9zm0 3h7v2H7v-2z"/></svg>
</div>

<div id="agent-panel">
  <div class="agent-panel-header">
    <span class="agent-title">&#x2728; AI Agent</span>
    <div class="agent-actions">
      <button onclick="clearAgentChat()" title="清空">&empty;</button>
      <button onclick="toggleAgentChatPanel()" title="关闭">&times;</button>
    </div>
  </div>
  <div id="agent-messages">
    <div class="agent-msg agent">你好！我是小天量化的AI助手。可以问我关于策略、交易指标、风控设置等问题。</div>
  </div>
  <div id="agent-input-row">
    <textarea id="agent-input" placeholder="输入消息..." rows="1" onkeydown="if(event.key==='Enter'&&!event.shiftKey){event.preventDefault();sendAgentMsg()}"></textarea>
    <button onclick="sendAgentMsg()" id="agent-send-btn">发送</button>
  </div>
</div>

<!-- ══════ Agent Ball & Panel Drag Script (after ball element) ══════ -->
<script>
(function(){
  var ball=document.getElementById('agent-ball');
  var panel=document.getElementById('agent-panel');
  if(!ball)return;

  // ── Ball drag state ──
  var D=false, sx=0, sy=0, ix=0, iy=0, ti=0;

  // ── Panel drag state ──
  var pD=false, psx=0, psy=0, pix=0, piy=0, pti=0;

  ball.addEventListener('mousedown',function(e){
    if(e.button!==0)return;
    e.preventDefault();
    D=true; ti=Date.now();
    sx=e.clientX; sy=e.clientY;
    var r=ball.getBoundingClientRect();
    ix=r.left; iy=r.top;
    ball.classList.add('dragging');
  });

  ball.addEventListener('touchstart',function(e){
    if(e.touches.length!==1)return;
    var t=e.touches[0];
    D=true; ti=Date.now();
    sx=t.clientX; sy=t.clientY;
    var r=ball.getBoundingClientRect();
    ix=r.left; iy=r.top;
    ball.classList.add('dragging');
  },{passive:false});

  // ── Panel drag: mousedown on header ──
  if(panel){
    var header=panel.querySelector('.agent-panel-header');
    if(header){
      header.addEventListener('mousedown',function(e){
        if(e.button!==0)return;
        if(e.target.tagName==='BUTTON')return;
        e.preventDefault();
        pD=true; pti=Date.now();
        panel._dragged=true;
        psx=e.clientX; psy=e.clientY;
        var r=panel.getBoundingClientRect();
        pix=r.left; piy=r.top;
        panel.style.right='auto'; panel.style.bottom='auto';
        panel.style.left=pix+'px'; panel.style.top=piy+'px';
        panel.style.transition='none';
      });
      header.addEventListener('touchstart',function(e){
        if(e.touches.length!==1)return;
        if(e.target.tagName==='BUTTON')return;
        var t=e.touches[0];
        pD=true; pti=Date.now();
        psx=t.clientX; psy=t.clientY;
        var r=panel.getBoundingClientRect();
        pix=r.left; piy=r.top;
        panel.style.right='auto'; panel.style.bottom='auto';
        panel.style.left=pix+'px'; panel.style.top=piy+'px';
        panel.style.transition='none';
      },{passive:false});
    }
  }

  document.addEventListener('mousemove',function(e){
    if(D){
      var nx=ix+(e.clientX-sx), ny=iy+(e.clientY-sy);
      nx=Math.max(0,Math.min(window.innerWidth-ball.offsetWidth,nx));
      ny=Math.max(0,Math.min(window.innerHeight-ball.offsetHeight,ny));
      ball.style.left=nx+'px'; ball.style.top=ny+'px';
      ball.style.right='auto'; ball.style.bottom='auto';
    }
    if(pD && panel){
      var nx=pix+(e.clientX-psx), ny=piy+(e.clientY-psy);
      nx=Math.max(0,Math.min(window.innerWidth-panel.offsetWidth,nx));
      ny=Math.max(0,Math.min(window.innerHeight-40,ny));
      panel.style.left=nx+'px'; panel.style.top=ny+'px';
    }
  });

  document.addEventListener('touchmove',function(e){
    if(D){
      var t=e.touches[0];
      var nx=ix+(t.clientX-sx), ny=iy+(t.clientY-sy);
      nx=Math.max(0,Math.min(window.innerWidth-ball.offsetWidth,nx));
      ny=Math.max(0,Math.min(window.innerHeight-ball.offsetHeight,ny));
      ball.style.left=nx+'px'; ball.style.top=ny+'px';
      ball.style.right='auto'; ball.style.bottom='auto';
    }
    if(pD && panel){
      var t=e.touches[0];
      var nx=pix+(t.clientX-psx), ny=piy+(t.clientY-psy);
      nx=Math.max(0,Math.min(window.innerWidth-panel.offsetWidth,nx));
      ny=Math.max(0,Math.min(window.innerHeight-40,ny));
      panel.style.left=nx+'px'; panel.style.top=ny+'px';
    }
  },{passive:false});

  document.addEventListener('mouseup',function(e){
    if(D){
      D=false; ball.classList.remove('dragging');
      var dt=Date.now()-ti;
      var r=ball.getBoundingClientRect();
      var dx=e.clientX-(r.left+r.width/2), dy=e.clientY-(r.top+r.height/2);
      if(dt<300 && Math.sqrt(dx*dx+dy*dy)<10){toggleAgentChatPanel()}
    }
    if(pD){
      pD=false;
      if(panel)panel.style.transition='';
    }
  });

  document.addEventListener('touchend',function(){
    if(D){
      D=false; ball.classList.remove('dragging');
      if(Date.now()-ti<300){toggleAgentChatPanel()}
    }
    if(pD){
      pD=false;
      if(panel)panel.style.transition='';
    }
  });

  window.addEventListener('resize',function(){
    var r=ball.getBoundingClientRect(),vw=window.innerWidth,vh=window.innerHeight,bw=ball.offsetWidth,bh=ball.offsetHeight;
    if(r.left>vw-bw){ball.style.left=(vw-bw-4)+'px';ball.style.right='auto'}
    if(r.top>vh-bh){ball.style.top=(vh-bh-4)+'px';ball.style.bottom='auto'}
    if(r.left<0){ball.style.left='4px'}
    if(r.top<0){ball.style.top='4px'}
    if(panel){
      var pr=panel.getBoundingClientRect(),pw=panel.offsetWidth,ph=panel.offsetHeight;
      if(pr.left>vw-pw){panel.style.left=(vw-pw-4)+'px'}
      if(pr.top>vh-40){panel.style.top=(vh-40)+'px'}
      if(pr.left<0){panel.style.left='4px'}
      if(pr.top<0){panel.style.top='4px'}
    }
  });
})();
</script>
</body>
</html>"""


class WebServer:
    """FastAPI Web Server — complete trading terminal"""

    def __init__(self, engine, host: str = "0.0.0.0", port: int = 8080,
                 config_path: str = "config.yaml"):
        if not HAS_FASTAPI:
            raise ImportError("fastapi + uvicorn required")
        self.engine = engine
        self.host = host
        self.port = port
        self.config_path = Path(config_path)
        self._running = False
        self._ws_clients: List[WebSocket] = []
        self._ws_subs: dict = {}  # client_id -> { "channel:symbol": ref_count }
        self._server_task: Optional[asyncio.Task] = None
        self._broadcast_task: Optional[asyncio.Task] = None

        self.app = FastAPI(title="XiaoTianQuant v2.0", docs_url=None, redoc_url=None)
        self.app.add_middleware(
            CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"]
        )
        self._setup_routes()
        if hasattr(self.engine, "db") and self.engine.db:
            register_agent_gateway(self.app, self.engine, self.engine.db)

    def _setup_routes(self):
        # ── Page ──
        @self.app.get("/", response_class=HTMLResponse)
        async def index():
            from fastapi.responses import Response
            return Response(content=INDEX_HTML, media_type="text/html",
                           headers={"Cache-Control": "no-cache, no-store, must-revalidate",
                                    "Pragma": "no-cache", "Expires": "0"})

        # ── Status ──
        @self.app.get("/api/status")
        async def api_status():
            return self.engine.get_stats()

        # ── Dashboard (delegates to shared routes module) ──
        from xtquant.web.routes.dashboard_routes import router as dashboard_router
        self.app.include_router(dashboard_router)

        # ── K-lines (real data preferred, minimal reference fallback) ──
        @self.app.get("/api/klines/{symbol}")
        async def api_klines(symbol: str, interval: str = "1m", limit: int = 200):
            bars = []
            for exch in self.engine.exchanges.values():
                if hasattr(exch, 'get_klines'):
                    try:
                        bars = await exch.get_klines(symbol, interval, limit)
                        if bars: break
                    except Exception: pass
            if not bars:
                for exch in self.engine.exchanges.values():
                    if hasattr(exch, 'get_bar_history'):
                        try:
                            hist = exch.get_bar_history(symbol)
                            if hist: bars = list(hist)[-limit:]; break
                        except Exception: pass
            # Fallback: direct Binance REST API
            if not bars:
                try:
                    import urllib.request, json
                    url = f"https://api.binance.com/api/v3/klines?symbol={symbol}&interval={interval}&limit={limit}"
                    req = urllib.request.Request(url, headers={"Accept":"application/json"})
                    resp = urllib.request.urlopen(req, timeout=10)
                    data = json.loads(resp.read().decode())
                    for k in data:
                        bars.append({"time":int(k[0]),"open":float(k[1]),"high":float(k[2]),"low":float(k[3]),"close":float(k[4]),"volume":float(k[5])})
                except Exception: pass
            result = []
            for b in bars:
                t = b.get("time", b.get("timestamp", 0))
                if hasattr(b, 'timestamp'): t = b.timestamp
                if t < 10000000000: t = t * 1000
                result.append({"time":t,"open":b.get("open",0)if isinstance(b,dict)else getattr(b,'open',0),"high":b.get("high",0)if isinstance(b,dict)else getattr(b,'high',0),"low":b.get("low",0)if isinstance(b,dict)else getattr(b,'low',0),"close":b.get("close",0)if isinstance(b,dict)else getattr(b,'close',0),"volume":b.get("volume",0)if isinstance(b,dict)else getattr(b,'volume',0)})
            return result

        # ── Orders ──
        @self.app.get("/api/orders")
        async def api_orders(symbol: str = None):
            if self.engine.oms:
                orders = self.engine.oms.get_active_orders(symbol=symbol)
                return [{"order_id": o.id, "symbol": o.symbol, "side": o.side,
                         "order_type": o.order_type, "price": o.price, "quantity": o.quantity,
                         "filled_qty": o.filled_qty, "status": o.status, "created_at": o.created_at}
                        for o in orders]
            return []

        @self.app.get("/api/orders/history")
        async def api_order_history(limit: int = 100):
            if self.engine.oms:
                return [{"order_id": o.id, "symbol": o.symbol, "side": o.side,
                         "order_type": o.order_type, "price": o.price, "quantity": o.quantity,
                         "filled_qty": o.filled_qty, "status": o.status, "created_at": o.created_at}
                        for o in self.engine.oms.get_order_history(limit=limit)]
            return []

        @self.app.post("/api/orders/{order_id}/cancel")
        async def api_cancel_order(order_id: str):
            if self.engine.oms:
                ok = await self.engine.oms.cancel(order_id)
                return {"status": "cancelled" if ok else "not found"}
            return {"status": "oms not available"}

        # ── Manual order placement ──
        @self.app.post("/api/order")
        async def api_place_order(request: Request):
            body = await request.json()
            exchange = body.get("exchange", "BINANCE")
            symbol = body.get("symbol", "BTCUSDT")
            side = body.get("side", "BUY")
            order_type = body.get("order_type", "LIMIT")
            price = float(body.get("price", 0))
            quantity = float(body.get("quantity", 0.001))

            order = await self.engine.place_order(
                exchange, symbol, side, order_type, price, quantity
            )
            if order:
                return {"status": "ok", "order_id": order.id,
                        "symbol": order.symbol, "side": order.side}
            return JSONResponse({"detail": "order rejected"}, status_code=400)

        # ── Positions ──
        @self.app.get("/api/positions")
        async def api_positions():
            if self.engine.portfolio:
                return [{"exchange": p.exchange, "symbol": p.symbol, "side": p.side,
                         "size": p.size, "entry_price": p.entry_price,
                         "mark_price": getattr(p, 'mark_price', p.entry_price),
                         "pnl": p.unrealized_pnl, "pnl_pct": getattr(p, 'unrealized_pnl_pct', 0)}
                        for p in self.engine.portfolio.get_all_positions()]
            return []

        # ── Strategies (v2.0 engine) ──
        @self.app.get("/api/strategies")
        async def api_strategies():
            return self.engine.get_strategy_info()

        @self.app.post("/api/strategies/{name}/start")
        async def api_start_strategy(name: str):
            return await self.engine.start_strategy(name)

        @self.app.post("/api/strategies/{name}/stop")
        async def api_stop_strategy(name: str):
            return await self.engine.stop_strategy(name)

        # ── Strategy Configs CRUD ──
        @self.app.get("/api/strategies/configs")
        async def api_strategy_configs(category: str = None, status: str = None,
                                        coin: str = None, type: str = None,
                                        limit: int = 100, offset: int = 0):
            repo = self.engine.strategy_config_repo
            if not repo:
                return []
            return await repo.list(category=category, status=status, coin=coin,
                                   strategy_type=type, is_template=False,
                                   limit=limit, offset=offset)

        @self.app.get("/api/strategies/configs/{sid}")
        async def api_get_strategy_config(sid: str):
            repo = self.engine.strategy_config_repo
            if not repo:
                return JSONResponse({"detail": "not available"}, status_code=500)
            row = await repo.get(sid)
            if not row:
                return JSONResponse({"detail": "not found"}, status_code=404)
            if row.get("config_json") and isinstance(row["config_json"], str):
                row["config"] = json.loads(row["config_json"])
            return row

        @self.app.post("/api/strategies/configs")
        async def api_create_strategy_config(request: Request):
            body = await request.json()
            repo = self.engine.strategy_config_repo
            if not repo:
                return JSONResponse({"detail": "not available"}, status_code=500)
            import uuid as _uuid
            sid = str(_uuid.uuid4())[:8]
            config = body.get("config", {})
            data = {
                "id": sid,
                "name": body.get("name", sid),
                "category": body.get("category", "spot"),
                "strategy_type": body.get("strategy_type", ""),
                "coin": body.get("coin", ""),
                "config_json": json.dumps(config) if isinstance(config, dict) else config,
                "direction": body.get("direction", "long"),
                "leverage": float(body.get("leverage", 1.0)),
                "status": "stopped",
                "is_template": 0,
                "pnl": 0.0,
                "created_at": int(time.time() * 1000),
                "updated_at": int(time.time() * 1000),
            }
            await repo.create(data)
            if repo and hasattr(self.engine, 'strategy_log_repo') and self.engine.strategy_log_repo:
                await self.engine.strategy_log_repo.append(sid, "INFO", f"策略 {data['name']} 已创建")
            return {"status": "ok", "id": sid}

        @self.app.put("/api/strategies/configs/{sid}")
        async def api_update_strategy_config(sid: str, request: Request):
            body = await request.json()
            repo = self.engine.strategy_config_repo
            if not repo:
                return JSONResponse({"detail": "not available"}, status_code=500)
            data = {"updated_at": int(time.time() * 1000)}
            for f in ["name", "coin", "strategy_type", "direction", "leverage", "category"]:
                if f in body:
                    data[f] = body[f]
            if "config" in body:
                data["config_json"] = json.dumps(body["config"]) if isinstance(body["config"], dict) else body["config"]
            await repo.update(sid, data)
            return {"status": "ok"}

        @self.app.delete("/api/strategies/configs/{sid}")
        async def api_delete_strategy_config(sid: str):
            repo = self.engine.strategy_config_repo
            if not repo:
                return JSONResponse({"detail": "not available"}, status_code=500)
            await repo.delete(sid)
            return {"status": "ok"}

        @self.app.post("/api/strategies/configs/{sid}/start")
        async def api_start_config(sid: str):
            repo = self.engine.strategy_config_repo
            if not repo:
                return JSONResponse({"detail": "not available"}, status_code=500)
            await repo.set_status(sid, "running")
            if self.engine.strategy_log_repo:
                await self.engine.strategy_log_repo.append(sid, "INFO", "策略已启动")
            return {"status": "ok"}

        @self.app.post("/api/strategies/configs/{sid}/stop")
        async def api_stop_config(sid: str):
            repo = self.engine.strategy_config_repo
            if not repo:
                return JSONResponse({"detail": "not available"}, status_code=500)
            await repo.set_status(sid, "stopped")
            if self.engine.strategy_log_repo:
                await self.engine.strategy_log_repo.append(sid, "INFO", "策略已停止")
            return {"status": "ok"}

        @self.app.post("/api/strategies/configs/batch-start")
        async def api_batch_start(request: Request):
            body = await request.json()
            repo = self.engine.strategy_config_repo
            if not repo:
                return JSONResponse({"detail": "not available"}, status_code=500)
            await repo.batch_set_status(body.get("ids", []), "running")
            return {"status": "ok"}

        @self.app.post("/api/strategies/configs/batch-stop")
        async def api_batch_stop(request: Request):
            body = await request.json()
            repo = self.engine.strategy_config_repo
            if not repo:
                return JSONResponse({"detail": "not available"}, status_code=500)
            await repo.batch_set_status(body.get("ids", []), "stopped")
            return {"status": "ok"}

        # ── Strategy Global Settings ──
        @self.app.get("/api/strategies/global")
        async def api_get_global():
            repo = self.engine.strategy_global_repo
            if not repo:
                return {"profit_protection_enabled": False, "max_concurrent_orders": 5}
            return await repo.get()

        @self.app.put("/api/strategies/global")
        async def api_update_global(request: Request):
            body = await request.json()
            repo = self.engine.strategy_global_repo
            if not repo:
                return JSONResponse({"detail": "not available"}, status_code=500)
            data = {}
            if "profit_protection_enabled" in body:
                data["profit_protection_enabled"] = 1 if body["profit_protection_enabled"] else 0
            if "max_concurrent_orders" in body:
                data["max_concurrent_orders"] = int(body["max_concurrent_orders"])
            if data:
                await repo.update(data)
            return {"status": "ok"}

        # ── Strategy Templates ──
        @self.app.get("/api/strategies/templates")
        async def api_templates(category: str = None):
            repo = self.engine.strategy_config_repo
            if not repo:
                return []
            rows = await repo.list(category=category, is_template=True, limit=200)
            for r in rows:
                if isinstance(r.get("config_json"), str):
                    r["config"] = json.loads(r["config_json"])
            return rows

        @self.app.post("/api/strategies/templates")
        async def api_save_template(request: Request):
            body = await request.json()
            repo = self.engine.strategy_config_repo
            if not repo:
                return JSONResponse({"detail": "not available"}, status_code=500)
            import uuid as _uuid
            tid = str(_uuid.uuid4())[:8]
            config = body.get("config", {})
            data = {
                "id": tid,
                "name": body.get("name", tid),
                "category": body.get("category", "spot"),
                "strategy_type": body.get("strategy_type", ""),
                "coin": body.get("coin", ""),
                "config_json": json.dumps(config) if isinstance(config, dict) else config,
                "direction": body.get("direction", "long"),
                "leverage": float(body.get("leverage", 1.0)),
                "status": "stopped",
                "is_template": 1,
                "template_name": body.get("name", tid),
                "created_at": int(time.time() * 1000),
                "updated_at": int(time.time() * 1000),
            }
            await repo.create(data)
            return {"status": "ok", "id": tid}

        @self.app.delete("/api/strategies/templates/{tid}")
        async def api_delete_template(tid: str):
            repo = self.engine.strategy_config_repo
            if not repo:
                return JSONResponse({"detail": "not available"}, status_code=500)
            await repo.delete(tid)
            return {"status": "ok"}

        # ── Strategy Logs ──
        @self.app.get("/api/strategies/logs")
        async def api_strategy_logs(strategy_id: str = None, level: str = None,
                                     limit: int = 100, offset: int = 0):
            repo = self.engine.strategy_log_repo
            if not repo:
                return []
            return await repo.list(strategy_id=strategy_id, level=level,
                                   limit=limit, offset=offset)

        @self.app.delete("/api/strategies/logs")
        async def api_clear_logs(strategy_id: str = None):
            repo = self.engine.strategy_log_repo
            if not repo:
                return JSONResponse({"detail": "not available"}, status_code=500)
            await repo.clear(strategy_id=strategy_id)
            return {"status": "ok"}

        # ── Strategy Trade Logs ──
        @self.app.get("/api/strategies/trade-logs")
        async def api_trade_logs(strategy_id: str = None, limit: int = 100):
            repo = self.engine.strategy_trade_repo
            if not repo:
                return []
            return await repo.list(strategy_id=strategy_id, limit=limit)

        # ── Risk ──
        @self.app.get("/api/risk")
        async def api_risk():
            if self.engine.risk_manager:
                return self.engine.risk_manager.get_stats()
            return {}

        # ── Backtest ──
        @self.app.post("/api/backtest/run")
        async def api_run_backtest(request: Request):
            body = await request.json()
            result = await self.engine.run_backtest_web(body)
            return result

        @self.app.get("/api/backtest/result")
        async def api_backtest_result():
            return self.engine.get_backtest_result()

        # ── AI Strategy Generation ──
        @self.app.post("/api/ai/generate")
        async def api_ai_generate(request: Request):
            body = await request.json()
            symbol = body.get("symbol", "BTCUSDT")
            interval = body.get("interval", "1m")
            risk = body.get("risk", "medium")
            prompt = body.get("prompt", "")
            mode = body.get("mode", "quick")

            if not prompt.strip():
                return JSONResponse({"detail": "prompt is required"}, status_code=400)

            # Build mode-specific prompt augmentation
            mode_guidance = {
                "quick": "Generate a simple, production-ready strategy with clear entry/exit rules. Keep it under 60 lines.",
                "advanced": "Generate an advanced strategy using multiple indicators, dynamic position sizing, and risk management rules.",
                "optimize": "Generate a strategy that includes parameter optimization logic and adaptive thresholds based on market regime detection.",
                "combo": "Generate a strategy that combines multiple sub-strategies with a signal fusion/ensemble approach and conflict resolution.",
            }
            mode_hint = mode_guidance.get(mode, mode_guidance["quick"])
            augmented_prompt = f"[Mode: {mode}] {mode_hint}\n\nUser request: {prompt}"

            # Read AI config from config.yaml, fall back to env vars
            import yaml
            ai_api_key = os.environ.get("OPENAI_API_KEY", "")
            ai_base_url = os.environ.get("OPENAI_BASE_URL", "")
            ai_model = os.environ.get("OPENAI_MODEL", "gpt-4")
            request_provider = body.get("provider", "")
            all_providers = ("openai","qwen","anthropic","google","deepseek","doubao","baidu","hunyuan","mistral","zhipu","yi","local")
            if self.config_path.exists():
                with open(self.config_path, encoding="utf-8") as f:
                    raw = yaml.safe_load(f) or {}
                ai_cfg = raw.get("ai", {})
                # If request specifies a provider, try that one first
                if request_provider and request_provider in all_providers:
                    pcfg = ai_cfg.get(request_provider, {})
                    if pcfg.get("api_key"):
                        ai_api_key = pcfg["api_key"]
                        ai_base_url = pcfg.get("base_url", "")
                        ai_model = pcfg.get("model", ai_model)
                # Fallback: pick first configured provider
                if not ai_api_key:
                    def_prov = raw.get("default_ai_provider", "")
                    if def_prov and def_prov in all_providers:
                        dpcfg = ai_cfg.get(def_prov, {})
                        if dpcfg.get("api_key"):
                            ai_api_key = dpcfg["api_key"]
                            ai_base_url = dpcfg.get("base_url", "")
                            ai_model = dpcfg.get("model", ai_model)
                if not ai_api_key:
                    for provider in all_providers:
                        pcfg = ai_cfg.get(provider, {})
                        if pcfg.get("api_key"):
                            ai_api_key = pcfg["api_key"]
                            ai_base_url = pcfg.get("base_url", ai_base_url)
                            ai_model = pcfg.get("model", ai_model)
                            break

            # Gather real-time market context for the AI
            market_context = ""
            try:
                from xtquant.ai.generator import gather_market_context
                for ex in self.engine.exchanges:
                    if hasattr(ex, 'get_klines'):
                        bars = await ex.get_klines(symbol, interval=interval, limit=100)
                        if bars:
                            market_context = gather_market_context(bars, symbol, interval)
                            logger.info(f"[AI] Market context gathered: {len(bars)} klines for {symbol} {interval}")
                        break
            except Exception as e:
                logger.warning(f"[AI] Failed to gather market context: {e}")

            generator = AIStrategyGenerator(api_key=ai_api_key, base_url=ai_base_url, model=ai_model)
            try:
                result = await generator.generate(symbol, interval, risk, augmented_prompt, market_context)
                valid, err = generator.validate_code(result["strategy_code"])
                if not valid:
                    result["status"] = "warning"
                    result["message"] = f"Code validation warning: {err}"
                else:
                    result["status"] = "ok"
                return result
            except ValueError as e:
                return JSONResponse({"detail": str(e)}, status_code=500)
            except Exception as e:
                logger.error(f"AI generate error: {e}")
                return JSONResponse({"detail": f"AI service error: {e}"}, status_code=500)

        @self.app.post("/api/ai/backtest")
        async def api_ai_backtest(request: Request):
            body = await request.json()
            strategy_code = body.get("strategy_code", "")
            config = body.get("config", {})

            generator = AIStrategyGenerator()
            valid, err = generator.validate_code(strategy_code)
            if not valid:
                return JSONResponse({"detail": f"Code rejected: {err}"}, status_code=400)

            try:
                strategy_class = generator.execute_sandbox(strategy_code)
            except Exception as e:
                logger.error(f"Sandbox execution error: {e}")
                return JSONResponse({"detail": f"Code execution error: {e}"}, status_code=500)

            full_config = {
                "symbol": config.get("symbol", "BTCUSDT"),
                "initial_balance": config.get("initial_balance", 100000),
                "fee_rate": config.get("fee_rate", 0.001),
                "slippage": config.get("slippage", 0.0005),
                "num_bars": config.get("num_bars", 500),
                "base_price": config.get("base_price", 68000),
                "volatility": config.get("volatility", 200),
                "params": config.get("params", {}),
                "strategy_name": config.get("strategy_name", "ai_strategy"),
            }

            try:
                result = await self.engine.run_backtest_ai(full_config, strategy_class)
                return {"status": "ok", **result}
            except Exception as e:
                logger.error(f"AI backtest error: {e}")
                return JSONResponse({"detail": f"Backtest error: {e}"}, status_code=500)

        @self.app.post("/api/native/backtest")
        async def api_native_backtest(request: Request):
            body = await request.json()
            strategy_code = body.get("strategy_code", "")
            config = body.get("config", {})

            generator = AIStrategyGenerator()
            valid, err = generator.validate_code(strategy_code)
            if not valid:
                return JSONResponse({"detail": f"Code rejected: {err}"}, status_code=400)

            try:
                strategy_class = generator.execute_sandbox(strategy_code)
            except Exception as e:
                logger.error(f"Native sandbox error: {e}")
                return JSONResponse({"detail": f"Code execution error: {e}"}, status_code=500)

            full_config = {
                "symbol": config.get("symbol", "BTCUSDT"),
                "initial_balance": config.get("initial_balance", 100000),
                "fee_rate": config.get("fee_rate", 0.001),
                "slippage": config.get("slippage", 0.0005),
                "num_bars": config.get("num_bars", 500),
                "base_price": config.get("base_price", 68000),
                "volatility": config.get("volatility", 200),
                "params": config.get("params", {}),
                "strategy_name": config.get("strategy_name", "native_strategy"),
            }

            try:
                result = await self.engine.run_backtest_ai(full_config, strategy_class)
                return {"status": "ok", **result}
            except Exception as e:
                logger.error(f"Native backtest error: {e}")
                return JSONResponse({"detail": f"Backtest error: {e}"}, status_code=500)

        @self.app.post("/api/ai/deploy")
        async def api_ai_deploy(request: Request):
            body = await request.json()
            repo = self.engine.strategy_config_repo
            if not repo:
                return JSONResponse({"detail": "not available"}, status_code=500)

            import uuid as _uuid
            tid = str(_uuid.uuid4())[:8]
            config_json = json.dumps({
                "strategy_code": body.get("strategy_code", ""),
                "description": body.get("description", ""),
                "risk": body.get("risk", "medium"),
                "backtest_report": body.get("backtest_report", {}),
            })
            data = {
                "id": tid,
                "name": body.get("strategy_name", "ai_strategy"),
                "category": "spot",
                "strategy_type": "ai_generated",
                "coin": body.get("symbol", "BTCUSDT"),
                "config_json": config_json,
                "direction": "long",
                "leverage": 1.0,
                "status": "stopped",
                "is_template": 1,
                "template_name": body.get("strategy_name", "ai_strategy"),
                "pnl": 0.0,
                "created_at": int(time.time() * 1000),
                "updated_at": int(time.time() * 1000),
            }
            await repo.create(data)
            if self.engine.strategy_log_repo:
                await self.engine.strategy_log_repo.append(tid, "INFO", f"Agent策略 {data['name']} 已部署到模板库")
            return {"status": "ok", "id": tid}

        # ── Multi-Agent Strategy Generation ──
        @self.app.post("/api/ai/multi-agent")
        async def api_ai_multi_agent(request: Request):
            body = await request.json()
            symbol = body.get("symbol", "BTCUSDT")
            interval = body.get("interval", "1h")
            risk = body.get("risk", "medium")
            prompt = body.get("prompt", "")
            mode = body.get("mode", "quick")

            if not prompt.strip():
                return JSONResponse({"detail": "prompt is required"}, status_code=400)

            mode_guidance = {
                "quick": "Generate a simple, production-ready strategy with clear entry/exit rules. Keep it under 60 lines.",
                "advanced": "Generate an advanced strategy using multiple indicators, dynamic position sizing, and risk management rules.",
                "optimize": "Generate a strategy that includes parameter optimization logic and adaptive thresholds.",
                "combo": "Generate a strategy that combines multiple sub-strategies with signal fusion.",
            }
            mode_hint = mode_guidance.get(mode, mode_guidance["quick"])
            augmented_prompt = f"[Mode: {mode}] {mode_hint}\n\nUser request: {prompt}"

            # Resolve AI config
            import yaml
            ai_api_key = os.environ.get("OPENAI_API_KEY", "")
            ai_base_url = os.environ.get("OPENAI_BASE_URL", "")
            ai_model = os.environ.get("OPENAI_MODEL", "gpt-4o")
            all_providers = ("openai","qwen","anthropic","google","deepseek","doubao","baidu","hunyuan","mistral","zhipu","yi","local")
            if self.config_path.exists():
                with open(self.config_path, encoding="utf-8") as f:
                    raw = yaml.safe_load(f) or {}
                ai_cfg = raw.get("ai", {})
                def_prov = raw.get("default_ai_provider", "")
                if def_prov and def_prov in all_providers:
                    dpcfg = ai_cfg.get(def_prov, {})
                    if dpcfg.get("api_key"):
                        ai_api_key = dpcfg["api_key"]
                        ai_base_url = dpcfg.get("base_url", "")
                        ai_model = dpcfg.get("model", ai_model)
                if not ai_api_key:
                    for provider in all_providers:
                        pcfg = ai_cfg.get(provider, {})
                        if pcfg.get("api_key"):
                            ai_api_key = pcfg["api_key"]
                            ai_base_url = pcfg.get("base_url", ai_base_url)
                            ai_model = pcfg.get("model", ai_model)
                            break

            if not ai_api_key:
                return JSONResponse({"detail": "No AI provider configured"}, status_code=400)

            # Gather market context
            market_context = ""
            try:
                from xtquant.ai.generator import gather_market_context
                for ex in self.engine.exchanges:
                    if hasattr(ex, 'get_klines'):
                        bars = await ex.get_klines(symbol, interval=interval, limit=100)
                        if bars:
                            market_context = gather_market_context(bars, symbol, interval)
                        break
            except Exception as e:
                logger.warning(f"[MultiAgent] Market context failed: {e}")

            # Run multi-agent pipeline
            from xtquant.ai.multi_agent import MultiAgentCoordinator
            coordinator = MultiAgentCoordinator(api_key=ai_api_key, base_url=ai_base_url, model=ai_model)
            try:
                result = await coordinator.generate(symbol, interval, risk, augmented_prompt, market_context)
                return {
                    "status": "ok" if result.success else "error",
                    "strategy_name": result.strategy_name,
                    "strategy_code": result.strategy_code,
                    "description": result.description,
                    "agents": {
                        "technical": result.technical_analysis,
                        "onchain": result.onchain_analysis,
                        "sentiment": result.sentiment_analysis,
                        "risk": result.risk_assessment,
                        "bull": result.bull_argument,
                        "bear": result.bear_argument,
                    },
                    "debate_summary": result.debate_summary,
                    "error": result.error,
                }
            except Exception as e:
                logger.error(f"[MultiAgent] Generation error: {e}")
                return JSONResponse({"detail": f"Multi-agent error: {e}"}, status_code=500)

        @self.app.post("/api/ai/optimize")
        async def api_ai_optimize(request: Request):
            body = await request.json()
            symbol = body.get("symbol", "BTCUSDT")
            interval = body.get("interval", "1h")
            risk = body.get("risk", "medium")
            prompt = body.get("prompt", "")
            max_iterations = min(int(body.get("max_iterations", 3)), 5)

            if not prompt.strip():
                return JSONResponse({"detail": "prompt is required"}, status_code=400)

            # Resolve AI config
            import yaml
            ai_api_key = os.environ.get("OPENAI_API_KEY", "")
            ai_base_url = os.environ.get("OPENAI_BASE_URL", "")
            ai_model = os.environ.get("OPENAI_MODEL", "gpt-4o")
            all_providers = ("openai","qwen","anthropic","google","deepseek","doubao","baidu","hunyuan","mistral","zhipu","yi","local")
            if self.config_path.exists():
                with open(self.config_path, encoding="utf-8") as f:
                    raw = yaml.safe_load(f) or {}
                ai_cfg = raw.get("ai", {})
                def_prov = raw.get("default_ai_provider", "")
                if def_prov and def_prov in all_providers:
                    dpcfg = ai_cfg.get(def_prov, {})
                    if dpcfg.get("api_key"):
                        ai_api_key = dpcfg["api_key"]
                        ai_base_url = dpcfg.get("base_url", "")
                        ai_model = dpcfg.get("model", ai_model)
                if not ai_api_key:
                    for provider in all_providers:
                        pcfg = ai_cfg.get(provider, {})
                        if pcfg.get("api_key"):
                            ai_api_key = pcfg["api_key"]
                            ai_base_url = pcfg.get("base_url", ai_base_url)
                            ai_model = pcfg.get("model", ai_model)
                            break

            if not ai_api_key:
                return JSONResponse({"detail": "No AI provider configured"}, status_code=400)

            # Gather market context
            market_context = ""
            try:
                from xtquant.ai.generator import gather_market_context
                for ex in self.engine.exchanges:
                    if hasattr(ex, 'get_klines'):
                        bars = await ex.get_klines(symbol, interval=interval, limit=100)
                        if bars:
                            market_context = gather_market_context(bars, symbol, interval)
                        break
            except Exception:
                pass

            # Build backtest function for closed-loop optimization
            async def backtest_fn(strategy_code, config):
                from xtquant.ai.generator import AIStrategyGenerator
                gen = AIStrategyGenerator()
                valid, err = gen.validate_code(strategy_code)
                if not valid:
                    return {"report": {"error": err}}
                try:
                    strategy_class = gen.execute_sandbox(strategy_code)
                except Exception as e:
                    return {"report": {"error": str(e)}}
                result = await self.engine.run_backtest_ai(config, strategy_class)
                return result

            from xtquant.ai.multi_agent import MultiAgentCoordinator
            coordinator = MultiAgentCoordinator(api_key=ai_api_key, base_url=ai_base_url, model=ai_model)
            try:
                result = await coordinator.optimize(
                    symbol, interval, risk, prompt, market_context,
                    backtest_fn=backtest_fn, max_iterations=max_iterations
                )
                resp = {
                    "status": "ok" if result.success else "error",
                    "strategy_name": result.strategy_name,
                    "strategy_code": result.strategy_code,
                    "description": result.description,
                    "agents": {
                        "technical": result.technical_analysis,
                        "onchain": result.onchain_analysis,
                        "sentiment": result.sentiment_analysis,
                        "risk": result.risk_assessment,
                        "bull": result.bull_argument,
                        "bear": result.bear_argument,
                    },
                    "error": result.error,
                }
                if hasattr(result, 'iteration_history'):
                    resp["iteration_history"] = result.iteration_history
                return resp
            except Exception as e:
                logger.error(f"[MultiAgent] Optimization error: {e}")
                return JSONResponse({"detail": f"Optimization error: {e}"}, status_code=500)

        # ── Config ──
        @self.app.get("/api/config")
        async def get_config():
            import yaml
            if not self.config_path.exists():
                return {}
            with open(self.config_path, encoding="utf-8") as f:
                raw = yaml.safe_load(f) or {}
            result = {}
            for name, cfg in raw.get("exchanges", {}).items():
                result[name] = {
                    "api_key": cfg.get("api_key", ""),
                    "secret": cfg.get("secret", ""),
                    "passphrase": cfg.get("passphrase", ""),
                    "testnet": cfg.get("testnet", True),
                    "futures": cfg.get("futures", False),
                    "enabled": cfg.get("enabled", True),
                }
            ai_raw = raw.get("ai", {})
            detected = False
            for prov in ("openai","qwen","anthropic","google","deepseek","doubao","baidu","hunyuan","mistral","zhipu","yi","local"):
                if prov in ai_raw:
                    detected = True
                    break
            if detected:
                # Return provider configs only, strip old flat-format keys
                result["ai"] = {k: v for k, v in ai_raw.items() if k not in ("api_key", "base_url", "model")}
            else:
                result["ai"] = {
                    "openai": {"api_key": ai_raw.get("api_key", ""), "base_url": "https://api.openai.com/v1", "model": "gpt-4o"},
                    "qwen": {"api_key": "", "base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1", "model": "qwen-max"},
                    "anthropic": {"api_key": "", "base_url": "https://api.anthropic.com/v1", "model": "claude-sonnet-4-6"},
                    "google": {"api_key": "", "base_url": "https://generativelanguage.googleapis.com/v1beta", "model": "gemini-pro"},
                    "deepseek": {"api_key": "", "base_url": "https://api.deepseek.com/v1", "model": "deepseek-chat"},
                    "doubao": {"api_key": "", "base_url": "https://ark.cn-beijing.volces.com/api/v3", "model": "doubao-pro"},
                    "baidu": {"api_key": "", "base_url": "https://aip.baidubce.com/rpc/2.0/ai_custom/v1/wenxinworkshop/chat", "model": "ernie-4.0"},
                    "hunyuan": {"api_key": "", "base_url": "https://api.hunyuan.cloud.tencent.com/v1", "model": "hunyuan-pro"},
                    "mistral": {"api_key": "", "base_url": "https://api.mistral.ai/v1", "model": "mistral-large"},
                    "zhipu": {"api_key": "", "base_url": "https://open.bigmodel.cn/api/paas/v4", "model": "glm-4"},
                    "yi": {"api_key": "", "base_url": "https://api.lingyiwanwu.com/v1", "model": "yi-large"},
                    "local": {"api_key": "", "base_url": "http://localhost:11434/v1", "model": "llama3"},
                }
            result["default_ai_provider"] = raw.get("default_ai_provider", "")
            result["default_exchange"] = raw.get("default_exchange", "")
            return result

        @self.app.post("/api/exchange/save")
        async def save_exchange(request: Request):
            import yaml
            body = await request.json()
            name = body.get("name", "")
            if not name:
                return JSONResponse({"detail": "name is required"}, status_code=400)
            if self.config_path.exists():
                with open(self.config_path, encoding="utf-8") as f:
                    raw = yaml.safe_load(f) or {}
            else:
                raw = {}
            if "exchanges" not in raw:
                raw["exchanges"] = {}
            if name not in raw["exchanges"]:
                raw["exchanges"][name] = {}
            raw["exchanges"][name]["api_key"] = body.get("api_key", "")
            raw["exchanges"][name]["secret"] = body.get("secret", "")
            raw["exchanges"][name]["testnet"] = body.get("testnet", True)
            raw["exchanges"][name]["futures"] = body.get("futures", False)
            raw["exchanges"][name]["enabled"] = body.get("api_key", "") != ""
            if body.get("passphrase"):
                raw["exchanges"][name]["passphrase"] = body["passphrase"]
            with open(self.config_path, "w", encoding="utf-8") as f:
                yaml.safe_dump(raw, f, default_flow_style=False, allow_unicode=True)
            logger.info("[Web] exchange saved: %s", name)
            return {"status": "ok"}

        @self.app.post("/api/exchange/test")
        async def test_exchange(request: Request):
            body = await request.json()
            name = body.get("name", "")
            api_key = body.get("api_key", "")
            secret = body.get("secret", "")
            futures = body.get("futures", False)
            if not api_key or not secret:
                return JSONResponse({"status": "fail", "detail": "API Key and Secret are required"}, status_code=400)
            try:
                if name == "binance":
                    import urllib.request, urllib.error, hmac, hashlib, time as _time
                    ts = int(_time.time() * 1000)
                    query = f"timestamp={ts}"
                    signature = hmac.new(secret.encode(), query.encode(), hashlib.sha256).hexdigest()
                    base = "https://fapi.binance.com" if futures else "https://api.binance.com"
                    url = f"{base}/api/v3/account?{query}&signature={signature}"
                    req = urllib.request.Request(url, headers={"X-MBX-APIKEY": api_key})
                    resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 10)
                    if resp.status == 200:
                        return {"status": "ok"}
                    text = resp.read().decode()
                    return JSONResponse({"status": "fail", "detail": text[:200]}, status_code=400)
                elif name in ("okx", "coinbase", "kucoin", "bybit", "gate", "htx", "mexc", "zb", "bitget", "phemex", "deribit"):
                    urls = {
                        "okx": "https://www.okx.com/api/v5/account/balance",
                        "coinbase": "https://api.coinbase.com/v2/accounts",
                        "kucoin": "https://api.kucoin.com/api/v1/accounts",
                        "bybit": "https://api.bybit.com/v5/account/info",
                        "gate": "https://api.gateio.ws/api/v4/wallet/total_balance",
                        "htx": "https://api.huobi.pro/v1/account/accounts",
                        "mexc": "https://api.mexc.com/api/v3/account",
                        "zb": "https://api.zb.com/data/v1/account",
                        "bitget": "https://api.bitget.com/api/v2/spot/account/assets",
                        "phemex": "https://api.phemex.com/accounts/accountPositions",
                        "deribit": "https://www.deribit.com/api/v2/private/get_account_summary",
                    }
                    url = urls.get(name, "")
                    import urllib.request, urllib.error
                    req = urllib.request.Request(url, headers={"Authorization": f"Bearer {api_key}"})
                    resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 10)
                    if resp.status in (200, 401):
                        return {"status": "ok"} if resp.status == 200 else JSONResponse({"status": "fail", "detail": "Invalid credentials"}, status_code=400)
                    return JSONResponse({"status": "fail", "detail": f"HTTP {resp.status}"}, status_code=400)
                else:
                    return JSONResponse({"status": "fail", "detail": "Unknown exchange"}, status_code=400)
                return {"status": "ok"}
            except Exception as e:
                logger.warning("[Web] exchange test failed: %s - %s", name, str(e)[:150])
                return JSONResponse({"status": "fail", "detail": str(e)[:200]}, status_code=400)

        @self.app.post("/api/ai/save")
        async def save_ai(request: Request):
            import yaml
            body = await request.json()
            provider = body.get("provider", "")
            if not provider:
                return JSONResponse({"detail": "provider is required"}, status_code=400)
            if self.config_path.exists():
                with open(self.config_path, encoding="utf-8") as f:
                    raw = yaml.safe_load(f) or {}
            else:
                raw = {}
            if "ai" not in raw:
                raw["ai"] = {}
            ai = raw["ai"]
            ai[provider] = {
                "api_key": body.get("api_key", ""),
                "base_url": body.get("base_url", ""),
                "model": body.get("model", ""),
            }
            # Clean old flat-format keys that would shadow provider configs
            for flat_key in ("api_key", "base_url", "model"):
                ai.pop(flat_key, None)
            # Auto-set default provider if none set yet and this provider has a key
            if body.get("api_key", "").strip() and not raw.get("default_ai_provider"):
                raw["default_ai_provider"] = provider
            with open(self.config_path, "w", encoding="utf-8") as f:
                yaml.safe_dump(raw, f, default_flow_style=False, allow_unicode=True)
            logger.info("[Web] AI provider saved: %s", provider)
            return {"status": "ok"}

        @self.app.post("/api/ai/test")
        async def test_ai(request: Request):
            body = await request.json()
            api_key = body.get("api_key", "")
            base_url = body.get("base_url", "")
            if not api_key:
                return JSONResponse({"status": "fail", "detail": "API Key is required"}, status_code=400)
            try:
                import urllib.request, urllib.error
                url = f"{base_url.rstrip('/')}/models"
                req = urllib.request.Request(url, headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"})
                resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 10)
                if resp.status == 200:
                    return {"status": "ok"}
                text = resp.read().decode()
                return JSONResponse({"status": "fail", "detail": text[:200]}, status_code=400)
            except Exception as e:
                return JSONResponse({"status": "fail", "detail": str(e)[:200]}, status_code=400)

        @self.app.post("/api/ai/default")
        async def set_default_ai(request: Request):
            import yaml
            body = await request.json()
            provider = body.get("provider", "")
            if self.config_path.exists():
                with open(self.config_path, encoding="utf-8") as f:
                    raw = yaml.safe_load(f) or {}
            else:
                raw = {}
            raw["default_ai_provider"] = provider
            with open(self.config_path, "w", encoding="utf-8") as f:
                yaml.safe_dump(raw, f, default_flow_style=False, allow_unicode=True)
            logger.info("[Web] default AI provider set to: %s", provider)
            return {"status": "ok"}

        @self.app.post("/api/exchange/default")
        async def set_default_exchange(request: Request):
            import yaml
            body = await request.json()
            exchange = body.get("exchange", "")
            if self.config_path.exists():
                with open(self.config_path, encoding="utf-8") as f:
                    raw = yaml.safe_load(f) or {}
            else:
                raw = {}
            raw["default_exchange"] = exchange
            with open(self.config_path, "w", encoding="utf-8") as f:
                yaml.safe_dump(raw, f, default_flow_style=False, allow_unicode=True)
            logger.info("[Web] default exchange set to: %s", exchange)
            return {"status": "ok"}

        # ── Agent Token Management ──
        @self.app.get("/api/agent/tokens")
        async def list_agent_tokens():
            if not hasattr(self.engine, "db") or not self.engine.db:
                return []
            tm = AgentTokenManager(self.engine.db)
            return await tm.list_tokens()

        @self.app.post("/api/agent/tokens")
        async def create_agent_token(request: Request):
            if not hasattr(self.engine, "db") or not self.engine.db:
                return JSONResponse({"detail": "Database not available"}, status_code=500)
            body = await request.json()
            tm = AgentTokenManager(self.engine.db)
            token = await tm.create_token(
                name=body.get("name", "Unnamed"),
                scopes=body.get("scopes", ["read_market", "run_backtest"]),
                paper_only=body.get("paper_only", True),
                rate_limit=body.get("rate_limit", 60),
                expires_at=body.get("expires_at", 0),
            )
            return {"status": "ok", "token": token}

        @self.app.delete("/api/agent/tokens/{token_id}")
        async def revoke_agent_token(token_id: str):
            if not hasattr(self.engine, "db") or not self.engine.db:
                return JSONResponse({"detail": "Database not available"}, status_code=500)
            tm = AgentTokenManager(self.engine.db)
            await tm.revoke_token(token_id)
            return {"status": "ok"}

        # ── Agent AI Model Config ──
        @self.app.get("/api/agent/ai-config")
        async def get_agent_ai_config():
            import yaml
            if self.config_path.exists():
                with open(self.config_path, encoding="utf-8") as f:
                    raw = yaml.safe_load(f) or {}
            else:
                raw = {}
            ai = raw.get("agent", {}).get("ai", {})
            provider = ai.get("provider", "")
            api_key = ai.get("api_key", "")
            base_url = ai.get("base_url", "")
            model = ai.get("model", "")
            # Fallback: if agent AI not configured, inherit from default AI provider
            if not api_key or api_key.startswith("sk-proxy-test") or api_key.startswith("sk-test"):
                def_prov = raw.get("default_ai_provider", "")
                if def_prov:
                    pcfg = (raw.get("ai", {}) or {}).get(def_prov, {})
                    if pcfg.get("api_key"):
                        provider = def_prov
                        api_key = pcfg.get("api_key", "")
                        base_url = pcfg.get("base_url", "")
                        model = pcfg.get("model", "")
            return {
                "provider": provider,
                "api_key": api_key,
                "base_url": base_url,
                "model": model,
                "proxy_enabled": ai.get("proxy_enabled", False),
                "http_proxy": ai.get("http_proxy", ""),
                "https_proxy": ai.get("https_proxy", ""),
            }

        @self.app.post("/api/agent/ai-config")
        async def save_agent_ai_config(request: Request):
            import yaml
            body = await request.json()
            if self.config_path.exists():
                with open(self.config_path, encoding="utf-8") as f:
                    raw = yaml.safe_load(f) or {}
            else:
                raw = {}
            if "agent" not in raw:
                raw["agent"] = {}
            raw["agent"]["ai"] = {
                "provider": body.get("provider", ""),
                "api_key": body.get("api_key", ""),
                "base_url": body.get("base_url", ""),
                "model": body.get("model", ""),
                "proxy_enabled": body.get("proxy_enabled", False),
                "http_proxy": body.get("http_proxy", ""),
                "https_proxy": body.get("https_proxy", ""),
            }
            with open(self.config_path, "w", encoding="utf-8") as f:
                yaml.safe_dump(raw, f, default_flow_style=False, allow_unicode=True)
            logger.info("[Web] Agent AI config saved: provider=%s", body.get("provider", ""))
            return {"status": "ok"}

        @self.app.post("/api/agent/ai-test")
        async def test_agent_ai(request: Request):
            body = await request.json()
            api_key = body.get("api_key", "")
            base_url = body.get("base_url", "")
            if not api_key:
                return JSONResponse({"status": "fail", "detail": "API Key is required"}, status_code=400)
            try:
                import urllib.request, urllib.error
                url = f"{base_url.rstrip('/')}/models"
                req = urllib.request.Request(url, headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"})
                resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 10)
                if resp.status == 200:
                    return {"status": "ok"}
                text = resp.read().decode()
                return JSONResponse({"status": "fail", "detail": text[:200]}, status_code=400)
            except Exception as e:
                return JSONResponse({"status": "fail", "detail": str(e)[:200]}, status_code=400)

        @self.app.post("/api/restart")
        async def restart_exchanges(request: Request):
            import yaml
            body = await request.json()
            if self.config_path.exists():
                with open(self.config_path, encoding="utf-8") as f:
                    raw = yaml.safe_load(f) or {}
            else:
                raw = {}
            if "exchanges" not in raw:
                raw["exchanges"] = {}
            # Merge any overrides from the request body
            for name, cfg in body.get("exchanges", {}).items():
                if name not in raw["exchanges"]:
                    raw["exchanges"][name] = {}
                for k, v in cfg.items():
                    if v:  # only override non-empty values
                        raw["exchanges"][name][k] = v
            result = await self.engine.reload_exchanges(raw.get("exchanges", {}))
            logger.info("[Web] exchanges reloaded: %s", result)
            return {"status": "ok", **result}

        # ── WebSocket ──
        @self.app.websocket("/ws")
        async def websocket_handler(ws: WebSocket):
            await ws.accept()
            self._ws_clients.append(ws)
            client_id = id(ws)
            self._ws_subs[client_id] = {}
            try:
                while True:
                    data = await ws.receive_text()
                    try:
                        msg = json.loads(data)
                        if msg.get("type") == "subscribe":
                            channel = msg.get("channel", "")
                            symbol = msg.get("symbol", "")
                            if channel and symbol:
                                key = f"{channel}:{symbol}"
                                self._ws_subs[client_id][key] = self._ws_subs[client_id].get(key, 0) + 1
                        elif msg.get("type") == "unsubscribe":
                            channel = msg.get("channel", "")
                            symbol = msg.get("symbol", "")
                            if channel and symbol:
                                key = f"{channel}:{symbol}"
                                c = self._ws_subs[client_id].get(key, 1) - 1
                                if c <= 0:
                                    self._ws_subs[client_id].pop(key, None)
                                else:
                                    self._ws_subs[client_id][key] = c
                    except (json.JSONDecodeError, KeyError):
                        pass
            except WebSocketDisconnect:
                pass
            except Exception:
                pass
            finally:
                if ws in self._ws_clients:
                    self._ws_clients.remove(ws)
                self._ws_subs.pop(client_id, None)

    async def _broadcast_loop(self):
        tick = 0
        while self._running:
            try:
                if self._ws_clients:
                    tick += 1
                    # Collect symbols from active strategies + configured strategies
                    active_symbols = set()
                    for st in self.engine.strategies.values():
                        if st._running and hasattr(st, 'symbols'):
                            active_symbols.update(st.symbols)
                    if not active_symbols:
                        if hasattr(self.engine, 'config') and self.engine.config:
                            strat_cfg = self.engine.config.get('strategies', {})
                            for name, cfg in strat_cfg.items():
                                if hasattr(cfg, 'symbols'):
                                    active_symbols.update(cfg.symbols)
                                elif isinstance(cfg, dict) and 'symbols' in cfg:
                                    active_symbols.update(cfg['symbols'])
                    if not active_symbols:
                        active_symbols = {'BTCUSDT'}

                    # Send to each client only what they subscribed to
                    for ws in self._ws_clients[:]:
                        try:
                            cid = id(ws)
                            subs = self._ws_subs.get(cid, {})

                            # Status: send every 10 ticks (unconditional, small message)
                            if tick % 10 == 0:
                                msg = json.dumps({"type": "status", "data": self.engine.get_stats()}, default=str)
                                await ws.send_text(msg)

                            # Price: only subscribed symbols
                            for sym in active_symbols:
                                price_key = f"price:{sym}"
                                if price_key in subs:
                                    price = self._get_symbol_price(sym)
                                    await ws.send_text(json.dumps({
                                        "type": "price", "symbol": sym,
                                        "data": {"price": round(price, 2)}
                                    }))

                            # Orderbook: only subscribed, every 10 ticks
                            if tick % 10 == 0:
                                for sym in active_symbols:
                                    ob_key = f"orderbook:{sym}"
                                    if ob_key in subs:
                                        book = self._build_orderbook({sym})
                                        if book:
                                            await ws.send_text(json.dumps({"type": "orderbook", "data": book, "symbol": sym}))

                            # Trades: only subscribed
                            for sym in active_symbols:
                                tr_key = f"trades:{sym}"
                                if tr_key in subs:
                                    trades = self._get_recent_trades(sym)
                                    if trades:
                                        await ws.send_text(json.dumps({"type": "trades", "symbol": sym, "data": trades}))
                        except Exception:
                            if ws in self._ws_clients:
                                self._ws_clients.remove(ws)
                            self._ws_subs.pop(cid, None)

                await asyncio.sleep(0.3)
            except asyncio.CancelledError:
                break

    def _get_symbol_price(self, symbol: str) -> float:
        """Get last price from exchange cache, falling back to orderbook mid price."""
        for exch in self.engine.exchanges.values():
            tick_cache = getattr(exch, '_tick_cache', {})
            if symbol in tick_cache:
                tick = tick_cache[symbol]
                return tick.price if hasattr(tick, 'price') else tick.get('price', 0)
            book_cache = getattr(exch, '_book_cache', {})
            if symbol in book_cache:
                book = book_cache[symbol]
                return book.mid_price()
        if self.engine.portfolio:
            for pos in self.engine.portfolio.get_all_positions():
                if pos.symbol == symbol:
                    return getattr(pos, 'mark_price', 0) or getattr(pos, 'entry_price', 0)
        return 0.0

    def _get_recent_trades(self, symbol: str) -> list:
        """Get recent trades for a symbol from exchange trade cache."""
        for exch in self.engine.exchanges.values():
            trade_cache = getattr(exch, '_trade_cache', {})
            if symbol in trade_cache:
                trades = trade_cache[symbol]
                if isinstance(trades, list):
                    recent = trades[-20:]
                    result = []
                    for t in recent:
                        if hasattr(t, 'price'):
                            result.append({
                                "price": t.price,
                                "qty": getattr(t, 'qty', getattr(t, 'quantity', 0)),
                                "side": getattr(t, 'side', 'BUY'),
                                "time": getattr(t, 'timestamp', 0)
                            })
                        elif isinstance(t, dict):
                            result.append(t)
                    return result
        # Generate simulated trades from price
        ref = self._get_symbol_price(symbol)
        if ref > 0:
            import random, time
            trades = []
            now = time.time()
            for i in range(20):
                px = round(ref * (1 + random.uniform(-0.002, 0.002)), 2)
                trades.append({
                    "price": px,
                    "qty": round(random.uniform(0.001, 0.5), 4),
                    "side": "BUY" if random.random() > 0.5 else "SELL",
                    "time": now - i * random.uniform(1, 30)
                })
            return trades
        return []

    def _build_orderbook(self, active_symbols: set) -> dict:
        """Build orderbook from exchange cache, with reference fallback."""
        for exch in self.engine.exchanges.values():
            for sym in active_symbols:
                cache = getattr(exch, '_book_cache', {})
                if sym in cache:
                    book = cache[sym]
                    bids = getattr(book, 'bids', None)
                    asks = getattr(book, 'asks', None)
                    if bids is not None and asks is not None:
                        return {"bids": bids[:10], "asks": asks[:10]}
        # Reference orderbook for first symbol when no exchange data
        if active_symbols:
            sym = next(iter(active_symbols))
            ref = self._get_symbol_price(sym)
            if ref > 0:
                bids = [[round(ref * (1 - 0.0001 * i), 2), round(1.5 - i * 0.1, 4)] for i in range(1, 11)]
                asks = [[round(ref * (1 + 0.0001 * i), 2), round(1.5 - i * 0.1, 4)] for i in range(1, 11)]
                return {"bids": bids, "asks": asks}
        return {}

    async def _broadcast_raw(self, msg: str):
        dead = []
        for ws in self._ws_clients:
            try:
                await ws.send_text(msg)
            except Exception:
                dead.append(ws)
        for ws in dead:
            self._ws_clients.remove(ws)

    async def start(self):
        self._running = True
        config = uvicorn.Config(self.app, host=self.host, port=self.port, log_level="warning")
        server = uvicorn.Server(config)
        self._server_task = asyncio.create_task(server.serve())
        self._broadcast_task = asyncio.create_task(self._broadcast_loop())
        logger.info(f"[Web] Trading terminal: http://{self.host}:{self.port}")

    async def stop(self):
        self._running = False
        if self._broadcast_task:
            self._broadcast_task.cancel()
        for ws in self._ws_clients:
            try: await ws.close()
            except Exception: pass
        self._ws_clients.clear()
        if self._server_task:
            self._server_task.cancel()
        logger.info("[Web] stopped")
