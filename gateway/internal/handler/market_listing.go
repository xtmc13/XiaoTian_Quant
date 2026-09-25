package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/marketplace"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 机器人/信号市场上架准入（对标 CryptoRobotics 创作者市场）──
// 作者侧：提交自有 AI 机器人实例考核 → 进度/驳回原因可见；
// 公开侧：listed 条目 + 标准化统计卡片（分页排序）；
// 管理侧：审核队列 / 通过 / 驳回(原因) / 强制下架 / 考核规则配置。

var marketSvc = marketplace.NewService()

// marketUserID 取登录用户 id；未登录写 401 并返回 false。
func marketUserID(c *gin.Context) (int, bool) {
	uid, injected := ctxUserID(c)
	if !injected || uid <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "unauthorized"})
		return 0, false
	}
	return uid, true
}

func marketError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"detail": message})
}

// marketMustGetListing 取条目并做属主/管理员校验（非公开访问路径用）。
// 不存在 404；存在但非作者且非 admin 403。
func marketMustGetListing(c *gin.Context) *store.MarketListingRecord {
	l := marketSvc.Repo().GetByID(c.Param("id"))
	if l == nil {
		marketError(c, http.StatusNotFound, "listing not found")
		return nil
	}
	if !requireOwner(c, l.AuthorUserID) {
		return nil
	}
	return l
}

// listingCard 条目 + 最新统计快照（市场卡片形态）。
func listingCard(l *store.MarketListingRecord) gin.H {
	card := gin.H{
		"id":              l.ID,
		"author_user_id":  l.AuthorUserID,
		"bot_instance_id": l.BotInstanceID,
		"kind":            l.Kind,
		"name":            l.Name,
		"description":     l.Description,
		"fee_model":       l.FeeModel,
		"fee_percent":     l.FeePercent,
		"monthly_fee":     l.MonthlyFee,
		"status":          l.Status,
		"listed_at":       l.ListedAt,
		// listed 即通过「考核期 + 人工审核」双重准入，卡片据此打标识。
		"probation_passed": l.Status == marketplace.StatusListed,
	}
	if stats := marketSvc.Repo().LatestStats(l.ID); stats != nil {
		card["stats"] = stats
	}
	return card
}

// ── 作者侧 ──

// MarketListingCreate 创建 draft 条目；body.submit=true 时直接提交考核。
// POST /api/market/listings
func MarketListingCreate(c *gin.Context) {
	uid, ok := marketUserID(c)
	if !ok {
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		marketError(c, http.StatusBadRequest, "invalid json")
		return
	}
	instanceID := getString(body, "bot_instance_id", "")
	if instanceID == "" {
		marketError(c, http.StatusBadRequest, "bot_instance_id required")
		return
	}
	rec, err := marketSvc.Create(int64(uid), instanceID,
		getString(body, "kind", "robot"),
		getString(body, "name", ""),
		getString(body, "description", ""),
		getString(body, "fee_model", "free"),
		getFloat(body, "fee_percent", 0),
		getFloat(body, "monthly_fee", 0))
	if err != nil {
		marketError(c, http.StatusBadRequest, err.Error())
		return
	}
	if submit, _ := body["submit"].(bool); submit {
		if err := marketSvc.Submit(rec, time.Now()); err != nil {
			marketError(c, http.StatusConflict, err.Error())
			return
		}
	}
	c.JSON(http.StatusOK, rec)
}

// MarketMyListings 作者条目列表（含最新统计与考核进度）。
// GET /api/market/my-listings
func MarketMyListings(c *gin.Context) {
	uid, ok := marketUserID(c)
	if !ok {
		return
	}
	out := []gin.H{}
	for _, l := range marketSvc.Repo().ListByAuthor(int64(uid)) {
		item := listingCard(l)
		item["reject_reason"] = l.RejectReason
		item["delist_reason"] = l.DelistReason
		item["probation_started_at"] = l.ProbationStartedAt
		if l.Status == marketplace.StatusProbation {
			if stats, err := marketSvc.Aggregate(l, time.Now()); err == nil {
				item["progress"] = marketplace.EvaluateProbation(l, stats)
			}
		}
		out = append(out, item)
	}
	c.JSON(http.StatusOK, gin.H{"listings": out})
}

// MarketListingSubmit 提交/重新提交考核：draft|rejected|delisted → probation。
// POST /api/market/listings/:id/submit
func MarketListingSubmit(c *gin.Context) {
	if _, ok := marketUserID(c); !ok {
		return
	}
	l := marketMustGetListing(c)
	if l == nil {
		return
	}
	if err := marketSvc.Submit(l, time.Now()); err != nil {
		marketError(c, http.StatusConflict, err.Error())
		return
	}
	c.JSON(http.StatusOK, l)
}

// MarketListingCancel 撤回考核：probation → draft。
// POST /api/market/listings/:id/cancel
func MarketListingCancel(c *gin.Context) {
	if _, ok := marketUserID(c); !ok {
		return
	}
	l := marketMustGetListing(c)
	if l == nil {
		return
	}
	if err := marketSvc.Cancel(l); err != nil {
		marketError(c, http.StatusConflict, err.Error())
		return
	}
	c.JSON(http.StatusOK, l)
}

// ── 公开侧（需登录，与平台其余市场接口一致） ──

// MarketListingList 公开市场：listed 条目 + 标准化统计卡片。
// GET /api/market/listings?sort=return|drawdown|followers&order=desc|asc&page=&page_size=
func MarketListingList(c *gin.Context) {
	sort := c.DefaultQuery("sort", "")
	order := c.DefaultQuery("order", "desc")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	list := marketSvc.Repo().ListListed(sort, order == "asc", pageSize, (page-1)*pageSize)
	items := []gin.H{}
	for _, l := range list {
		items = append(items, listingCard(l))
	}
	c.JSON(http.StatusOK, gin.H{
		"listings":  items,
		"total":     marketSvc.Repo().CountListed(),
		"page":      page,
		"page_size": pageSize,
	})
}

// MarketListingStats 快照序列：listed 对所有人可见；其他状态仅作者/admin。
// GET /api/market/listings/:id/stats?limit=
func MarketListingStats(c *gin.Context) {
	l := marketSvc.Repo().GetByID(c.Param("id"))
	if l == nil {
		marketError(c, http.StatusNotFound, "listing not found")
		return
	}
	if l.Status != marketplace.StatusListed {
		if !requireOwner(c, l.AuthorUserID) {
			return
		}
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "90"))
	series := marketSvc.Repo().StatsSeries(l.ID, limit)
	c.JSON(http.StatusOK, gin.H{
		"listing": listingCard(l),
		"series":  series,
	})
}

// MarketRulesGet 当前考核规则（前端展示用，登录即可读）。
// GET /api/market/rules
func MarketRulesGet(c *gin.Context) {
	c.JSON(http.StatusOK, marketplace.LoadRules())
}

// ── 管理侧（路由层挂 middleware.AdminRequired()） ──

// AdminMarketListingList 审核队列/全量列表。
// GET /api/admin/market/listings?status=pending_review
func AdminMarketListingList(c *gin.Context) {
	status := c.DefaultQuery("status", marketplace.StatusPendingReview)
	list := marketSvc.Repo().ListByStatus(status)
	items := []gin.H{}
	for _, l := range list {
		item := listingCard(l)
		item["reject_reason"] = l.RejectReason
		item["probation_started_at"] = l.ProbationStartedAt
		item["rule_min_days"] = l.RuleMinDays
		item["rule_min_trades"] = l.RuleMinTrades
		item["rule_max_drawdown_pct"] = l.RuleMaxDrawdownPct
		if stats, err := marketSvc.Aggregate(l, time.Now()); err == nil {
			item["progress"] = marketplace.EvaluateProbation(l, stats)
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"listings": items})
}

// AdminMarketListingApprove 通过审核：pending_review → listed。
// POST /api/admin/market/listings/:id/approve
func AdminMarketListingApprove(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	l := marketSvc.Repo().GetByID(c.Param("id"))
	if l == nil {
		marketError(c, http.StatusNotFound, "listing not found")
		return
	}
	uid, _ := ctxUserID(c)
	if err := marketSvc.Approve(l, int64(uid), time.Now()); err != nil {
		marketError(c, http.StatusConflict, err.Error())
		return
	}
	c.JSON(http.StatusOK, l)
}

// AdminMarketListingReject 驳回：pending_review → rejected（原因必填）。
// POST /api/admin/market/listings/:id/reject
func AdminMarketListingReject(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	l := marketSvc.Repo().GetByID(c.Param("id"))
	if l == nil {
		marketError(c, http.StatusNotFound, "listing not found")
		return
	}
	var body map[string]any
	_ = c.ShouldBindJSON(&body)
	reason := getString(body, "reason", "")
	if reason == "" {
		marketError(c, http.StatusBadRequest, "reason required")
		return
	}
	uid, _ := ctxUserID(c)
	if err := marketSvc.Reject(l, int64(uid), reason, time.Now()); err != nil {
		marketError(c, http.StatusConflict, err.Error())
		return
	}
	c.JSON(http.StatusOK, l)
}

// AdminMarketListingDelist 强制下架：listed → delisted（原因必填）。
// POST /api/admin/market/listings/:id/delist
func AdminMarketListingDelist(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	l := marketSvc.Repo().GetByID(c.Param("id"))
	if l == nil {
		marketError(c, http.StatusNotFound, "listing not found")
		return
	}
	var body map[string]any
	_ = c.ShouldBindJSON(&body)
	reason := getString(body, "reason", "")
	if reason == "" {
		marketError(c, http.StatusBadRequest, "reason required")
		return
	}
	uid, _ := ctxUserID(c)
	if err := marketSvc.Delist(l, int64(uid), reason, time.Now()); err != nil {
		marketError(c, http.StatusConflict, err.Error())
		return
	}
	c.JSON(http.StatusOK, l)
}

// AdminMarketRulesPut 更新考核规则（min_days/min_trades/max_drawdown_pct）。
// PUT /api/admin/market/rules
func AdminMarketRulesPut(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var body marketplace.Rules
	if err := c.ShouldBindJSON(&body); err != nil {
		marketError(c, http.StatusBadRequest, "invalid json")
		return
	}
	if err := marketplace.SaveRules(body); err != nil {
		marketError(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, marketplace.LoadRules())
}
