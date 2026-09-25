package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 收益分享卡（已平仓交易 / 回测报告一键生成分享图的数据源）────────────
//
// 安全口径：
//   - 仅属主/admin 可取卡片数据（非本人 403，requireOwner 统一语义）；
//   - 昵称默认脱敏（首字 + ***），?reveal_name=1 时属主可取回真实昵称；
//   - 金额同时下发绝对值与百分比，由前端切换渲染（分享图默认百分比）。
//
// 前端用返回的结构化数据在 canvas 客户端渲染成图（1080x1350），后端不出图。

// maskShareNickname 昵称脱敏：保留首字符，其余打码；空昵称回退"匿名用户"。
func maskShareNickname(n string) string {
	n = strings.TrimSpace(n)
	if n == "" {
		return "匿名用户"
	}
	r, _ := utf8.DecodeRuneInString(n)
	return string(r) + "***"
}

// shareNicknameOf 取属主昵称（默认脱敏；reveal_name=1 时返回原文）。
func shareNicknameOf(c *gin.Context, userID int64) string {
	name := ""
	if u := store.FindUserByID(int(userID)); u != nil {
		name, _ = u["nickname"].(string)
		if name == "" {
			name, _ = u["username"].(string)
		}
	}
	if c.Query("reveal_name") == "1" {
		if name == "" {
			return "匿名用户"
		}
		return name
	}
	return maskShareNickname(name)
}

// shareAmountMode 金额展示模式：pct（默认，百分比）| abs（绝对值）。
func shareAmountMode(c *gin.Context) string {
	if strings.EqualFold(c.Query("amount"), "abs") {
		return "abs"
	}
	return "pct"
}

// ShareTradeCard GET /api/share/trade/:id/card
// 已平仓持仓（positions.status=CLOSED）的结构化分享卡片。
func ShareTradeCard(c *gin.Context) {
	p, err := store.NewPositionRepo().GetByID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "position not found"})
		return
	}
	if !requireOwner(c, p.UserID) {
		return
	}
	if !strings.EqualFold(p.Status, "CLOSED") {
		c.JSON(http.StatusConflict, gin.H{"detail": "仅已平仓持仓可生成分享卡"})
		return
	}

	var pnlPct float64
	if p.CostBasis > 0 {
		pnlPct = p.RealizedPnL / p.CostBasis * 100
	}
	holdMs := int64(0)
	if p.ClosedAt > 0 && p.OpenedAt > 0 && p.ClosedAt >= p.OpenedAt {
		holdMs = p.ClosedAt - p.OpenedAt
	}
	c.JSON(http.StatusOK, gin.H{
		"kind":         "trade",
		"id":           p.ID,
		"symbol":       p.Symbol,
		"side":         p.Side,
		"exchange":     p.Exchange,
		"entry_price":  p.AvgEntryPrice,
		"exit_price":   p.CurrentPrice,
		"quantity":     p.Quantity,
		"cost_basis":   p.CostBasis,
		"pnl":          p.RealizedPnL,
		"pnl_pct":      pnlPct,
		"opened_at":    p.OpenedAt,
		"closed_at":    p.ClosedAt,
		"hold_ms":      holdMs,
		"nickname":     shareNicknameOf(c, p.UserID),
		"amount_mode":  shareAmountMode(c),
		"share_url":    "/share/trade/" + p.ID,
		"generated_at": time.Now().UnixMilli(),
	})
}

// ShareBacktestCard GET /api/share/backtest/:id/card
// 回测报告分享卡：优先组合回测（xt_portfolio_backtests，用户可见的持久化报告），
// 未命中回退 xt_backtests（report_json 提取同名指标）。
func ShareBacktestCard(c *gin.Context) {
	id := c.Param("id")
	if rec, err := store.NewPortfolioBacktestRepo().GetByID(id); err == nil {
		if !requireOwner(c, rec.UserID) {
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"kind":             "backtest",
			"id":               rec.ID,
			"name":             rec.Name,
			"strategy":         "portfolio",
			"timeframe":        rec.Timeframe,
			"total_return_pct": rec.TotalReturnPct,
			"max_drawdown_pct": rec.MaxDrawdownPct,
			"sharpe_ratio":     rec.SharpeRatio,
			"sortino_ratio":    rec.SortinoRatio,
			"win_rate":         rec.WinRate,
			"profit_factor":    rec.ProfitFactor,
			"total_trades":     rec.TotalTrades,
			"initial_capital":  rec.InitialCapital,
			"final_equity":     rec.FinalEquity,
			"start_time":       rec.StartTime,
			"end_time":         rec.EndTime,
			"created_at":       rec.CreatedAt,
			"nickname":         shareNicknameOf(c, rec.UserID),
			"amount_mode":      shareAmountMode(c),
			"share_url":        "/share/backtest/" + rec.ID,
			"generated_at":     time.Now().UnixMilli(),
		})
		return
	}

	rec, err := store.NewBacktestRepo().GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "backtest not found"})
		return
	}
	if !requireOwner(c, rec.UserID) {
		return
	}
	// report_json 提取指标（键名与 backtest.PerformanceReport 对齐）。
	report := map[string]any{}
	_ = json.Unmarshal([]byte(rec.ReportJSON), &report)
	num := func(key string) float64 {
		v, _ := report[key].(float64)
		return v
	}
	c.JSON(http.StatusOK, gin.H{
		"kind":             "backtest",
		"id":               rec.ID,
		"name":             rec.Name,
		"strategy":         rec.Strategy,
		"symbol":           rec.Symbol,
		"timeframe":        "",
		"total_return_pct": num("total_return_pct"),
		"max_drawdown_pct": num("max_drawdown_pct"),
		"sharpe_ratio":     num("sharpe_ratio"),
		"sortino_ratio":    num("sortino_ratio"),
		"win_rate":         num("win_rate"),
		"profit_factor":    num("profit_factor"),
		"total_trades":     num("total_trades"),
		"initial_capital":  num("initial_balance"),
		"final_equity":     num("final_equity"),
		"start_time":       rec.StartTime,
		"end_time":         rec.EndTime,
		"created_at":       rec.CreatedAt,
		"nickname":         shareNicknameOf(c, rec.UserID),
		"amount_mode":      shareAmountMode(c),
		"share_url":        "/share/backtest/" + rec.ID,
		"generated_at":     time.Now().UnixMilli(),
	})
}
