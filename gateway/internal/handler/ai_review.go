package handler

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── AI 策略复盘报告（对标 QuantDinger strategy_review） ──
// POST /api/ai/review            基于真实成交同步生成 AI 复盘并落库
// GET  /api/ai/review/reports    当前用户的复盘历史（可按 scope 过滤）
// GET  /api/ai/review/reports/:id 单条详情（属主校验）
//
// 复盘可接受秒级等待，同步执行；AI 调用超时沿用 ai.Provider 自身的
// HTTP client / 请求 context 超时（与 ai.go 一致）。

// aiReviewGetProvider 是 getActiveAIProvider 的可替换入口，单测注入 fake provider。
var aiReviewGetProvider = getActiveAIProvider

const (
	aiReviewStatusPending = "pending"
	aiReviewStatusDone    = "done"
	aiReviewStatusFailed  = "failed"
)

type aiReviewRequest struct {
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
	Days      int    `json:"days"`
}

// AIReviewGenerate 拉取 scope 窗口内真实成交 → 汇总统计 → AI 生成复盘 → 落库。
// provider 未配置或生成失败时同样落库一条 status=failed 记录（error 记原因），
// 前端可直接在历史列表里看到失败原因。
func AIReviewGenerate(c *gin.Context) {
	var req aiReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json: " + err.Error()})
		return
	}
	req.ScopeType = strings.TrimSpace(strings.ToLower(req.ScopeType))
	req.ScopeID = strings.TrimSpace(req.ScopeID)
	if req.ScopeType != "strategy" && req.ScopeType != "bot" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "scope_type 必须是 strategy|bot"})
		return
	}
	if req.ScopeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "scope_id 不能为空"})
		return
	}
	if req.Days <= 0 {
		req.Days = 30
	}
	if req.Days > 365 {
		req.Days = 365
	}

	userID := getUserID(c)
	scopeName, ok := resolveAIReviewScope(c, req.ScopeType, req.ScopeID, userID)
	if !ok {
		return // 已写出 404/403
	}

	endMs := time.Now().UnixMilli()
	startMs := endMs - int64(req.Days)*24*3600*1000
	trades, ledger, err := store.LoadScopeTrades(req.ScopeType, req.ScopeID, userID, startMs, endMs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "加载成交失败: " + err.Error()})
		return
	}
	if trades == nil {
		trades = []store.ScopeTrade{}
	}
	stats := computeAIReviewStats(trades, ledger)

	rec := &store.AIReviewReportRecord{
		UserID:      userID,
		ScopeType:   req.ScopeType,
		ScopeID:     req.ScopeID,
		PeriodStart: startMs,
		PeriodEnd:   endMs,
		TradesCount: stats.TradesCount,
		TotalPnL:    store.RoundFloat(stats.TotalPnL, 4),
		WinRate:     store.RoundFloat(stats.WinRate, 2),
		MaxDrawdown: store.RoundFloat(stats.MaxDrawdown, 4),
		Status:      aiReviewStatusPending,
	}

	provider := aiReviewGetProvider()
	if provider == nil {
		rec.Status = aiReviewStatusFailed
		rec.Error = "No AI provider configured. Set an API key in Settings → AI."
		persistAIReviewReport(c, rec)
		return
	}
	rec.Model = provider.Name + "/" + provider.Model

	prompt := buildAIReviewPrompt(req, scopeName, startMs, endMs, trades, ledger, stats)
	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Messages: []ai.ChatMessage{
			{Role: ai.RoleSystem, Content: "You are a senior quantitative trading review coach. You write honest, data-driven post-trade reviews in Chinese, plain text only, no markdown."},
			{Role: ai.RoleUser, Content: prompt},
		},
		MaxTokens:   2048,
		Temperature: 0.5,
	})
	if err != nil {
		rec.Status = aiReviewStatusFailed
		rec.Error = trimAIReviewErr(err.Error())
		persistAIReviewReport(c, rec)
		return
	}
	if len(resp.Choices) == 0 {
		rec.Status = aiReviewStatusFailed
		rec.Error = "AI returned empty response"
		persistAIReviewReport(c, rec)
		return
	}

	rec.Status = aiReviewStatusDone
	rec.ReportText = strings.TrimSpace(resp.Choices[0].Message.Content)
	persistAIReviewReport(c, rec)
}

// persistAIReviewReport 落库并统一响应（成功 / 失败都返回报告记录）。
func persistAIReviewReport(c *gin.Context, rec *store.AIReviewReportRecord) {
	if err := store.NewAIReviewReportRepo().Create(rec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存复盘报告失败: " + err.Error()})
		return
	}
	body := gin.H{
		"status": rec.Status,
		"report": rec,
	}
	if rec.Status == aiReviewStatusFailed {
		body["msg"] = rec.Error
	}
	c.JSON(http.StatusOK, body)
}

// resolveAIReviewScope 校验 scope 存在且属于当前用户，返回展示名。
// 未通过时已写出 404（不存在）/ 403（属主不符）响应。
func resolveAIReviewScope(c *gin.Context, scopeType, scopeID string, userID int64) (string, bool) {
	if scopeType == "strategy" {
		rec, err := store.NewStrategyConfigRepo().GetByID(scopeID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"detail": "scope 不存在"})
			return "", false
		}
		if !requireOwner(c, rec.UserID) {
			return "", false
		}
		return rec.Name, true
	}
	// bot：AI 机器人实例 → DCA → 网格 → 分层马丁。
	if inst := store.GetAIBotInstanceByID(scopeID, int(userID)); inst != nil {
		return getString(inst, "name", scopeID), true
	}
	if rec, err := store.NewDCARepo().GetByID(scopeID); err == nil && rec != nil {
		if !requireOwner(c, rec.UserID) {
			return "", false
		}
		return rec.Name, true
	}
	if rec, err := store.NewGridRepo().GetByID(scopeID); err == nil && rec != nil {
		if !requireOwner(c, rec.UserID) {
			return "", false
		}
		return rec.Name, true
	}
	if rec, err := store.NewLayeredMartinRepo().GetByID(scopeID); err == nil && rec != nil {
		if !requireOwner(c, rec.UserID) {
			return "", false
		}
		return rec.Name, true
	}
	c.JSON(http.StatusNotFound, gin.H{"detail": "scope 不存在"})
	return "", false
}

// AIReviewReportsList 返回当前用户的复盘报告历史。
func AIReviewReportsList(c *gin.Context) {
	userID := getUserID(c)
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = min(n, 200)
		}
	}
	recs, err := store.NewAIReviewReportRepo().ListByUser(userID, c.Query("scope_type"), c.Query("scope_id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to list ai review reports"})
		return
	}
	if recs == nil {
		recs = []*store.AIReviewReportRecord{}
	}
	c.JSON(http.StatusOK, gin.H{"reports": recs})
}

// AIReviewReportGet 返回单条复盘详情（属主校验，越权 403）。
func AIReviewReportGet(c *gin.Context) {
	rec, err := store.NewAIReviewReportRepo().GetByID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	if !requireOwner(c, rec.UserID) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"report": rec})
}

// ── 统计聚合 ──

type aiReviewStats struct {
	TradesCount  int     // 成交笔数（台账模式=平仓次数）
	RoundTrips   int     // 完整往返次数（FIFO 平仓次数 / 台账行数）
	Wins         int     // 盈利往返次数
	TotalPnL     float64 // 已实现盈亏合计（未平仓部分不计入）
	WinRate      float64 // 0-100
	MaxDrawdown  float64 // 按累计盈亏曲线算的最大回撤（绝对额）
	AvgPnL       float64
	BestPnL      float64
	WorstPnL     float64
	BuyCount     int
	SellCount    int
	UnpairedSell int // 无对应买入的卖出（流水模式）
	Symbols      map[string]int
}

// computeAIReviewStats 汇总复盘统计。
// ledger=true：trades 每行是一次完整往返（AI 机器人台账），PnL 直接采用；
// ledger=false：原始成交流水，按交易对 FIFO 配对买卖推算往返盈亏。
func computeAIReviewStats(trades []store.ScopeTrade, ledger bool) aiReviewStats {
	st := aiReviewStats{Symbols: map[string]int{}}
	if len(trades) == 0 {
		return st
	}
	var roundTrips []store.ScopeTrade
	if ledger {
		roundTrips = trades
		for _, t := range trades {
			st.Symbols[t.Symbol]++
			if strings.EqualFold(t.Side, "BUY") || strings.EqualFold(t.Side, "LONG") {
				st.BuyCount++
			} else {
				st.SellCount++
			}
		}
	} else {
		st.BuyCount, st.SellCount, st.UnpairedSell, roundTrips = pairFIFORoundTrips(trades)
		for _, t := range trades {
			st.Symbols[t.Symbol]++
		}
	}
	st.TradesCount = len(trades)
	st.RoundTrips = len(roundTrips)
	for _, rt := range roundTrips {
		st.TotalPnL += rt.PnL
		if rt.PnL > 0 {
			st.Wins++
		}
		if rt.PnL > st.BestPnL {
			st.BestPnL = rt.PnL
		}
		if rt.PnL < st.WorstPnL {
			st.WorstPnL = rt.PnL
		}
	}
	if st.RoundTrips > 0 {
		st.WinRate = float64(st.Wins) / float64(st.RoundTrips) * 100
		st.AvgPnL = st.TotalPnL / float64(st.RoundTrips)
	}
	st.MaxDrawdown = maxDrawdownOfPnL(roundTrips)
	return st
}

type fifoLot struct {
	qty      float64
	unitCost float64 // 含买入手续费的人均成本
}

// pairFIFORoundTrips 把成交流水按交易对 FIFO 配对为往返盈亏：
// 买入先入 lot（成本含买入费），卖出按时间优先匹配最早 lot，
// 往返盈亏 = 卖出金额 − 卖出手续费 − 匹配 lot 成本；无 lot 可配的卖出
// 记为 unpaired（窗口前已持仓，无法归因成本，不计盈亏）。
// 返回 (买入笔数, 卖出笔数, 无法归因卖出笔数, 往返列表)。
func pairFIFORoundTrips(trades []store.ScopeTrade) (buys, sells, unpaired int, roundTrips []store.ScopeTrade) {
	lots := map[string][]fifoLot{}
	for _, t := range trades {
		side := strings.ToUpper(t.Side)
		if side == "BUY" || side == "LONG" {
			buys++
			qty := t.Quantity
			if qty <= 0 {
				continue
			}
			cost := t.Price*qty + t.Fee
			lots[t.Symbol] = append(lots[t.Symbol], fifoLot{qty: qty, unitCost: cost / qty})
			continue
		}
		if side != "SELL" && side != "SHORT" {
			continue
		}
		sells++
		q := t.Quantity
		if q <= 0 {
			continue
		}
		queue := lots[t.Symbol]
		if len(queue) == 0 {
			unpaired++
			continue
		}
		pnl := 0.0
		for q > 1e-12 && len(queue) > 0 {
			lot := &queue[0]
			m := lot.qty
			if m > q {
				m = q
			}
			pnl += t.Price * m
			pnl -= lot.unitCost * m
			lot.qty -= m
			q -= m
			if lot.qty <= 1e-12 {
				queue = queue[1:]
			}
		}
		pnl -= t.Fee // 卖出手续费（整笔卖出计一次，不按匹配量分摊）
		lots[t.Symbol] = queue
		roundTrips = append(roundTrips, store.ScopeTrade{
			ID: t.ID, Symbol: t.Symbol, Side: t.Side, Price: t.Price,
			Quantity: t.Quantity, Fee: t.Fee, ClosedAt: t.ClosedAt, PnL: pnl,
		})
	}
	return buys, sells, unpaired, roundTrips
}

// maxDrawdownOfPnL 按平仓时间顺序对累计盈亏曲线求最大回撤（绝对额）。
func maxDrawdownOfPnL(roundTrips []store.ScopeTrade) float64 {
	sorted := make([]store.ScopeTrade, len(roundTrips))
	copy(sorted, roundTrips)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ClosedAt < sorted[j].ClosedAt })
	peak, maxDD := 0.0, 0.0
	cum := 0.0
	for _, rt := range sorted {
		cum += rt.PnL
		if cum > peak {
			peak = cum
		}
		if dd := peak - cum; dd > maxDD {
			maxDD = dd
		}
	}
	return maxDD
}

// ── Prompt 构造 ──

func buildAIReviewPrompt(req aiReviewRequest, scopeName string, startMs, endMs int64, trades []store.ScopeTrade, ledger bool, st aiReviewStats) string {
	scopeLabel := "策略"
	if req.ScopeType == "bot" {
		scopeLabel = "机器人"
	}
	start := time.UnixMilli(startMs).Format("2006-01-02")
	end := time.UnixMilli(endMs).Format("2006-01-02")

	var b strings.Builder
	fmt.Fprintf(&b, "你是一位资深量化交易复盘教练。请基于以下%s「%s」过去 %d 天（%s ~ %s）的真实成交数据写一份交易复盘报告。\n\n", scopeLabel, scopeName, req.Days, start, end)
	b.WriteString("【汇总统计】\n")
	fmt.Fprintf(&b, "- 成交笔数: %d\n", st.TradesCount)
	if ledger {
		fmt.Fprintf(&b, "- 完整开平仓次数: %d\n", st.RoundTrips)
	} else {
		fmt.Fprintf(&b, "- 买入笔数: %d，卖出笔数: %d（其中 %d 笔卖出因窗口前已持仓无法归因成本）\n", st.BuyCount, st.SellCount, st.UnpairedSell)
		fmt.Fprintf(&b, "- 按 FIFO 配对出的完整往返: %d 次（窗口内未平仓部分的浮动盈亏不计入）\n", st.RoundTrips)
	}
	fmt.Fprintf(&b, "- 已实现盈亏合计: %.2f USDT\n", st.TotalPnL)
	if st.RoundTrips > 0 {
		fmt.Fprintf(&b, "- 胜率: %.1f%%（%d/%d）\n", st.WinRate, st.Wins, st.RoundTrips)
		fmt.Fprintf(&b, "- 平均每笔盈亏: %.2f，最佳: %.2f，最差: %.2f\n", st.AvgPnL, st.BestPnL, st.WorstPnL)
	}
	fmt.Fprintf(&b, "- 最大回撤（按累计盈亏曲线）: %.2f USDT\n", st.MaxDrawdown)
	if len(st.Symbols) > 0 {
		names := make([]string, 0, len(st.Symbols))
		for s := range st.Symbols {
			names = append(names, s)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, s := range names {
			parts = append(parts, fmt.Sprintf("%s×%d", s, st.Symbols[s]))
		}
		fmt.Fprintf(&b, "- 成交分布: %s\n", strings.Join(parts, ", "))
	}
	b.WriteString("\n【成交流水】（按时间升序，最多 80 条；盈亏列为整笔往返盈亏，原始流水为空）\n")
	show := trades
	if len(show) > 80 {
		show = show[len(show)-80:]
	}
	for _, t := range show {
		ts := time.UnixMilli(t.ClosedAt).Format("01-02 15:04")
		if ledger {
			fmt.Fprintf(&b, "%s | %s | %s | 平仓价 %.4f | 数量 %.4f | 盈亏 %.2f | %s\n",
				ts, t.Symbol, strings.ToUpper(t.Side), t.Price, t.Quantity, t.PnL, t.CloseReason)
		} else {
			fmt.Fprintf(&b, "%s | %s | %s | 价格 %.4f | 数量 %.4f\n",
				ts, t.Symbol, strings.ToUpper(t.Side), t.Price, t.Quantity)
		}
	}
	b.WriteString(`
请严格按以下四段输出，纯文本，不要使用任何 Markdown 符号（不要 #、*、- 开头）：
一、总体评价：3-5 句话概括这段时间的盈亏表现、交易风格与稳定性。
二、做得好的点：结合具体数据指出值得保持的行为（如纪律性止盈、仓位控制等）。
三、问题与坏习惯：结合数据指出问题（如追高杀跌、频繁交易、止损不及时、盈亏比失衡、过度集中等），不要空泛。
四、可执行的改进建议：3-6 条具体可落地的改进措施（含可量化的目标，如单笔风险不超过本金 X%、止盈止损比例等）。`)
	return b.String()
}

func trimAIReviewErr(s string) string {
	const maxErrLen = 500
	if len(s) > maxErrLen {
		return s[:maxErrLen]
	}
	return s
}
