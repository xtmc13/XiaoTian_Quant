package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/market"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
	"github.com/xiaotian-quant/gateway/internal/strategy/strategies"
)

// ── K线供给管 ──
// 币安 WS 不向事件总线推真实 K 线，吃 K 线（OnBar）的策略靠 KlineFeeder
// 轮询 REST 补齐：策略配置启动时为 symbol+timeframe 建轮询，停止时释放。
var (
	klineFeeder  *market.KlineFeeder
	klineFeedsMu sync.Mutex
	klineFeeds   = map[string][2]string{} // configID -> {symbol, interval}
)

// SetKlineFeeder 注入 K 线供给管（参考 SetGridService，main 启动时接线）。
func SetKlineFeeder(f *market.KlineFeeder) { klineFeeder = f }

// KlineFeederForEngine 返回已注入的 K 线供给管，供策略引擎接线
// （多周期 A7.1 / 动态 universe A7.3 的动态订阅与 handler 侧共用同一实例；
// feeder 内部引用计数，两侧独立记账互不干扰）。未注入时返回 nil。
func KlineFeederForEngine() *market.KlineFeeder { return klineFeeder }

// ensureKlineFeed 为配置 id 启动对应 symbol+timeframe 的 K 线轮询。
// symbol 用引擎实际订阅的 wrapped.Symbol()，保证事件 topic 与策略订阅一致。
// 返回供给管是否新起轮询（true=bus 侧即将回补历史 K 线；false=供给管已在跑，
// 需要调用方直接暖机策略，否则新策略要等下一根新闭合 K 线，最长一个周期）。
func ensureKlineFeed(id, symbol, timeframe string) bool {
	if klineFeeder == nil || symbol == "" {
		return false
	}
	interval := strings.TrimSpace(strings.ToLower(timeframe))
	if interval == "" {
		interval = "15m"
	}
	klineFeedsMu.Lock()
	klineFeeds[id] = [2]string{symbol, interval}
	klineFeedsMu.Unlock()
	return klineFeeder.EnsureSymbol(symbol, interval)
}

// releaseKlineFeed 停掉配置 id 的 K 线轮询（幂等）。
func releaseKlineFeed(id string) {
	klineFeedsMu.Lock()
	key, ok := klineFeeds[id]
	if ok {
		delete(klineFeeds, id)
	}
	klineFeedsMu.Unlock()
	if ok && klineFeeder != nil {
		klineFeeder.ReleaseSymbol(key[0], key[1])
	}
}

// persistStrategyConfigToDB 把策略配置写穿透到 SQLite，保证 DB 优先的
// 读取路径（GetStrategyConfigs 每次从 DB 重建）能立即看到 API 的变更。
// 失败仅记日志——JSON 文件仍是兜底。
func persistStrategyConfigToDB(item map[string]any) {
	rec := store.StrategyConfigRecordFromMap(item)
	if rec == nil || rec.ID == "" {
		return
	}
	if err := store.NewStrategyConfigRepo().UpsertAll([]*store.StrategyConfigRecord{rec}); err != nil {
		log.Printf("[strategy] persist to DB failed (id=%s): %v", rec.ID, err)
	}
}

func GetStrategyConfigs(c *gin.Context) {
	category := c.Query("category")
	status := c.Query("status")
	coin := c.Query("coin")
	stype := c.Query("type")
	kind := strings.ToLower(strings.TrimSpace(c.Query("kind")))
	if kind != "" && kind != "strategy" && kind != "bot" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "kind must be strategy or bot"})
		return
	}
	limit := 200
	offset := 0
	fmtScan(c.Query("limit"), &limit)
	fmtScan(c.Query("offset"), &offset)

	// GetStrategyConfigs returns a defensive copy, so no extra lock is needed here.
	configs := store.GetStrategyConfigs()
	items := make([]map[string]any, 0, len(configs))
	for _, v := range configs {
		items = append(items, v)
	}

	// 多用户越权防护：非 admin 只能看到本人 + 历史无属主(user_id=0)的配置。
	if uid, injected := ctxUserID(c); injected && !ctxIsAdmin(c) {
		kept := items[:0]
		for _, it := range items {
			if getInt64Of(it, "user_id") == 0 || getInt64Of(it, "user_id") == int64(uid) {
				kept = append(kept, it)
			}
		}
		items = kept
	}

	// bot 身份字段（bot_type/trading_config）不是 DB 列，DB 重建（ToMap）会
	// 丢弃；这里从 config_json 兜底恢复到顶层，保证 isBotItem 判别与机器人页
	// 数据在重启/重建后依然稳定。
	for _, it := range items {
		hydrateBotFields(it)
	}

	// 策略机器人（马丁/华尔街）与策略实验室共用存储。未指定 category 且未
	// 指定 kind 时默认排除机器人品类，避免两个页面数据互窜（向后兼容）。
	// 指定 kind 时由后端判别函数 isBotItem 精确分流，不再依赖 category 猜谜。
	if kind == "" {
		if category == "" {
			kept := items[:0]
			for _, it := range items {
				cat, _ := it["category"].(string)
				if cat == "martin" || cat == "wallstreet" {
					continue
				}
				kept = append(kept, it)
			}
			items = kept
		}
	}

	if category != "" {
		items = filterMap(items, "category", category)
	}
	if status != "" {
		items = filterMap(items, "status", status)
	}
	if coin != "" {
		items = filterMapContains(items, "coin", coin)
	}
	if stype != "" {
		items = filterMap(items, "strategy_type", stype)
	}

	// kind 分流：strategy=仅策略实验室（!isBotItem），bot=仅策略机器人。
	// market_type='futures' 的无 bot 标记合约策略归 strategy 侧——这是
	// 2026-09-17 用户反馈的回归点（前端 swap 启发式过滤漏 futures）。
	switch kind {
	case "strategy":
		kept := items[:0]
		for _, it := range items {
			if isBotItem(it) {
				continue
			}
			kept = append(kept, it)
		}
		items = kept
	case "bot":
		kept := items[:0]
		for _, it := range items {
			if !isBotItem(it) {
				continue
			}
			kept = append(kept, it)
		}
		items = kept
	}

	sort.Slice(items, func(i, j int) bool {
		a := getFloat(items[i], "updated_at", 0)
		b := getFloat(items[j], "updated_at", 0)
		return a > b
	})

	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	items = items[offset:end]

	for _, it := range items {
		if configJSON, ok := it["config_json"].(string); ok {
			var config map[string]any
			if json.Unmarshal([]byte(configJSON), &config) == nil {
				it["config"] = config
			}
		}
	}
	if items == nil {
		items = []map[string]any{}
	}
	// Normalize field names to match frontend StrategyItem interface
	normalized := make([]map[string]any, 0, len(items))
	for _, it := range items {
		normalized = append(normalized, normalizeStrategyConfig(it))
	}
	c.JSON(http.StatusOK, normalized)
}

func GetStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	item := store.GetStrategyConfig(id)
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	if !requireOwner(c, getInt64Of(item, "user_id")) {
		return
	}
	hydrateBotFields(item)
	result := copyMap(item)
	if configJSON, ok := item["config_json"].(string); ok {
		var config map[string]any
		if json.Unmarshal([]byte(configJSON), &config) == nil {
			result["config"] = config
		}
	}
	c.JSON(http.StatusOK, normalizeStrategyConfig(result))
}

// GetStrategyRuntime 返回运行中策略的实时状态（运行面板数据源）：
// status=引擎内策略实例的 RuntimeStatus（未运行/不支持时为 null）、
// price=币安 WS 最新价（取不到为 0）、config=该策略 config_json 解析结果，
// 另附 next_add_distance_pct（下一档补仓距离）与 take_profit_distance_pct
// （静态止盈距离，可计算时才返回）。
func GetStrategyRuntime(c *gin.Context) {
	id := c.Param("id")
	item := store.GetStrategyConfig(id)
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	if !requireOwner(c, getInt64Of(item, "user_id")) {
		return
	}

	var status map[string]any
	if eng := strategy.GetEngine(nil); eng != nil {
		if st, ok := eng.RuntimeStatus(id); ok {
			status = st
		}
	}

	price := 0.0
	if appCtx := app.Get(); appCtx != nil && appCtx.BinanceWS != nil {
		sym := strings.ToUpper(strings.TrimSpace(getString(item, "symbol", "")))
		if sym == "" {
			if coin := strings.TrimSpace(getString(item, "coin", "")); coin != "" {
				sym = strings.ToUpper(coin) + "USDT"
			}
		}
		if sym != "" {
			price = appCtx.BinanceWS.GetPrice(sym)
		}
	}

	config := map[string]any{}
	if cj, ok := item["config_json"].(string); ok && cj != "" {
		var parsed map[string]any
		if json.Unmarshal([]byte(cj), &parsed) == nil && parsed != nil {
			config = parsed
		}
	}

	resp := gin.H{
		"status": status,
		"price":  price,
		"config": config,
	}
	if d, ok := computeNextAddDistance(status, config, price); ok {
		resp["next_add_distance_pct"] = d
	}
	if d, ok := computeTakeProfitDistance(status, config, price); ok {
		resp["take_profit_distance_pct"] = d
	}
	c.JSON(http.StatusOK, resp)
}

// computeNextAddDistance 计算现价到下一档未触发补仓档位的距离百分比。
// 在仓、有阶梯、有参考价（均价/入场价）且现价>0 才有意义，否则不返回。
func computeNextAddDistance(status, config map[string]any, price float64) (float64, bool) {
	if price <= 0 || status == nil {
		return 0, false
	}
	inPos, _ := status["in_position"].(bool)
	if !inPos {
		return 0, false
	}
	ladder, ok := config["add_positions"].([]any)
	if !ok || len(ladder) == 0 {
		return 0, false
	}
	ref := getFloat(status, "avg_entry_price", 0)
	if ref <= 0 {
		ref = getFloat(status, "entry_price", 0)
	}
	if ref <= 0 {
		return 0, false
	}
	side, _ := status["direction"].(string)
	filled := int(getFloat(status, "filled_orders", 0))
	// 当前价下方（做多）/上方（做空）最近的未触发档位。
	best := 0.0
	found := false
	for _, it := range ladder {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		order := int(getFloat(m, "order", 0))
		if order <= filled {
			continue // 已触发
		}
		spread := getFloat(m, "spread", 0)
		if spread <= 0 {
			continue
		}
		var tierPrice float64
		var dist float64
		if side == "short" {
			tierPrice = ref * (1 + spread)
			if tierPrice <= price {
				continue
			}
			dist = (tierPrice - price) / price * 100
		} else {
			tierPrice = ref * (1 - spread)
			if tierPrice >= price {
				continue
			}
			dist = (price - tierPrice) / price * 100
		}
		if !found || dist < best {
			best, found = dist, true
		}
	}
	return best, found
}

// computeTakeProfitDistance 静态止盈（tp_mode=static）下，现价到止盈目标价的
// 距离百分比；移动止盈（moving）没有固定目标价，不返回。
func computeTakeProfitDistance(status, config map[string]any, price float64) (float64, bool) {
	if price <= 0 || status == nil {
		return 0, false
	}
	if strings.ToLower(getString(config, "tp_mode", "")) != "static" {
		return 0, false
	}
	tpRatio := getFloat(config, "take_profit_ratio", 0)
	if tpRatio <= 0 {
		return 0, false
	}
	inPos, _ := status["in_position"].(bool)
	if !inPos {
		return 0, false
	}
	ref := getFloat(status, "avg_entry_price", 0)
	if ref <= 0 {
		ref = getFloat(status, "entry_price", 0)
	}
	if ref <= 0 {
		return 0, false
	}
	side, _ := status["direction"].(string)
	if side == "short" {
		target := ref * (1 - tpRatio)
		if target >= price {
			return 0, false
		}
		return (price - target) / price * 100, true
	}
	target := ref * (1 + tpRatio)
	if target <= price {
		return 0, false
	}
	return (target - price) / price * 100, true
}

func CreateStrategyConfig(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json"})
		return
	}

	// ── Validation ──
	name := getString(body, "name", "")
	if strings.TrimSpace(name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "策略名称不能为空"})
		return
	}
	symbol := getString(body, "symbol", "")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "交易对不能为空"})
		return
	}

	// ── 实盘安全闸（2026-09-17 用户要求加回实盘选项，但保留服务端总闸）──
	// execution_mode=live 仅在实盘开关开启时被接受；未开启一律 400。
	if strings.EqualFold(strings.TrimSpace(getString(body, "execution_mode", "")), "live") && !isLiveTradingEnabled() {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "实盘未开启：请先在服务端配置 trading.live_enabled: true（或 LIVE_TRADING_ENABLED=true / 管理员运行时解锁）后重启网关"})
		return
	}

	sid := shortUUID()
	nowTS := time.Now().UnixMilli()

	configJSON := "{}"
	if config, ok := body["config"].(map[string]any); ok && len(config) > 0 {
		mergeEngineSchedulingKeys(config, body)
		data, _ := json.Marshal(config)
		configJSON = string(data)
	} else if cj, ok := body["config_json"].(string); ok && cj != "" {
		configJSON = cj
	} else if flat := flattenCRAParams(body); len(flat) > 0 {
		// 部分入口（如指标 IDE）把 CRA 参数平铺在请求顶层且不带
		// config/config_json：收进 config_json，避免用户参数丢失。
		data, _ := json.Marshal(flat)
		configJSON = string(data)
	} else if flat := engineSchedulingKeys(body); len(flat) > 0 {
		// 只有 timeframes/schedule 等引擎级键的场景
		data, _ := json.Marshal(flat)
		configJSON = string(data)
	}

	item := map[string]any{
		"id":                sid,
		"user_id":           getUserID(c),
		"name":              strings.TrimSpace(name),
		"category":          getString(body, "category", ""),
		"strategy_type":     getString(body, "strategy_type", ""),
		"type":              getString(body, "strategy_type", ""),
		"strategy_name":     getString(body, "strategy_type", ""),
		"coin":              getString(body, "coin", ""),
		"config_json":       configJSON,
		"direction":         getString(body, "direction", ""),
		"trade_direction":   getString(body, "direction", ""),
		"leverage":          getFloat(body, "leverage", 1.0),
		"status":            "stopped",
		"pnl":               0.0,
		"total_pnl":         0.0,
		"total_pnl_percent": 0.0,
		"current_equity":    getFloat(body, "initial_capital", 0),
		"created_at":        float64(nowTS),
		"updated_at":        float64(nowTS),
		// ── Contract fields ──
		"market_type":     getString(body, "market_type", "spot"),
		"margin_mode":     getString(body, "margin_mode", "cross"),
		"symbol":          symbol,
		"timeframe":       getString(body, "timeframe", "15m"),
		"initial_capital": getFloat(body, "initial_capital", 0),
		"execution_mode":  getString(body, "execution_mode", ""),
		"mode":            getString(body, "execution_mode", ""),
		"strategy_mode":   getString(body, "execution_mode", ""),
		"group_id":        getString(body, "group_id", ""),
		"group_name":      getString(body, "group_name", ""),
		"indicator_name":  getString(body, "indicator_name", ""),
	}

	// "222 式"残废 payload 兜底：CRA 表单场景（及任何缺省入口）补全
	// strategy_type/category/execution_mode/direction，保证落库记录字段齐备。
	var payloadConfig map[string]any
	if cfg, ok := body["config"].(map[string]any); ok {
		payloadConfig = cfg
	}
	fillStrategyFieldDefaults(item, payloadConfig)

	// strategy_type 兜底后仍为空说明不是 CRA payload，维持原有拒绝行为。
	if getString(item, "strategy_type", "") == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "策略类型不能为空"})
		return
	}

	// ── 类型-参数防呆（P0-4）：显式类型与 CRA 特征键必须匹配 ──
	explicitType := strings.TrimSpace(getString(body, "strategy_type", "")) != ""
	if err := checkStrategyTypeConfigMatch(getString(item, "strategy_type", ""), mergedStrategyConfig(item, payloadConfig), explicitType); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	// ── 开仓指标选择器：indicator_params 合法化（宽松校验，非法 400）──
	if err := validateIndicatorParams(mergedStrategyConfig(item, payloadConfig)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	// ── 现货策略自定义键合法化 ──
	if err := validateSpotParams(mergedStrategyConfig(item, payloadConfig)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	// ── 策略级 protections 合法化（config_json["protections"]，非法拒绝写库）──
	if err := validateStrategyProtections(mergedStrategyConfig(item, payloadConfig)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	// ── 机器人身份持久化：strategy_mode/bot_type 不是 DB 列，必须写入
	// config_json 才能在 DB 重建后继续被 isBotItem 判别（根治互窜）。 ──
	if botType, isBot := botIdentityFromBody(body); isBot {
		if tc, ok := body["trading_config"]; ok && tc != nil {
			item["trading_config"] = tc
		}
		persistBotIdentity(item, botType)
	}

	store.SetStrategyConfig(sid, item)
	store.PersistStrategyConfigs()
	persistStrategyConfigToDB(item)
	resp := gin.H{"status": "ok", "id": sid}
	// forced_paper 仅在后端真实压回时返回（请求非 paper 且落库为 paper）；
	// 实盘总闸开启时 live 原样落库，不得误报。
	if req := strings.ToLower(strings.TrimSpace(getString(body, "execution_mode", ""))); req != "paper" && getString(item, "execution_mode", "") == "paper" {
		resp["forced_paper"] = true
	}
	c.JSON(http.StatusOK, resp)
}

func UpdateStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json"})
		return
	}
	item := store.GetStrategyConfig(id)
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	if !requireOwner(c, getInt64Of(item, "user_id")) {
		return
	}
	// ── 实盘安全闸：Update 显式改 live 同样受总闸约束 ──
	if strings.EqualFold(strings.TrimSpace(getString(body, "execution_mode", "")), "live") && !isLiveTradingEnabled() {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "实盘未开启：请先在服务端配置 trading.live_enabled: true（或 LIVE_TRADING_ENABLED=true / 管理员运行时解锁）后重启网关"})
		return
	}
	// 先在别名同步前捕获机器人身份（execution_mode 同步会把 strategy_mode
	// 覆写成 'paper'）；config_json 是 DB 重建后的唯一持久载体，需先水合。
	hydrateBotFields(item)
	wasBot := isBotItem(item)
	for _, f := range []string{"name", "coin", "strategy_type", "direction", "leverage", "category", "market_type", "margin_mode", "symbol", "timeframe", "execution_mode", "initial_capital", "group_id", "group_name", "indicator_name"} {
		if v, ok := body[f]; ok {
			item[f] = v
		}
	}
	// Keep alias fields in sync
	if v, ok := body["strategy_type"]; ok {
		item["type"] = v
		item["strategy_name"] = v
	}
	if v, ok := body["direction"]; ok {
		item["trade_direction"] = v
	}
	if v, ok := body["execution_mode"]; ok {
		item["mode"] = v
		item["strategy_mode"] = v
	}
	if config, ok := body["config"].(map[string]any); ok {
		mergeEngineSchedulingKeys(config, body)
		data, _ := json.Marshal(config)
		item["config_json"] = string(data)
	} else if cj, ok := body["config_json"].(string); ok {
		item["config_json"] = cj
	} else if flat := engineSchedulingKeys(body); len(flat) > 0 {
		// 仅带引擎级键（timeframes/schedule）的更新：合并进既有 config_json
		merged := map[string]any{}
		if cj := getString(item, "config_json", ""); cj != "" {
			_ = json.Unmarshal([]byte(cj), &merged)
		}
		mergeEngineSchedulingKeys(merged, body)
		data, _ := json.Marshal(merged)
		item["config_json"] = string(data)
	}
	// 编辑是修复历史残废记录（如 "222"）的入口：合并后同样做缺省补全。
	var payloadConfig map[string]any
	if cfg, ok := body["config"].(map[string]any); ok {
		payloadConfig = cfg
	}
	fillStrategyFieldDefaults(item, payloadConfig)

	// ── 类型-参数防呆（P0-4）：以合并后的类型/config 为准 ──
	explicitType := strings.TrimSpace(getString(body, "strategy_type", "")) != ""
	if err := checkStrategyTypeConfigMatch(getString(item, "strategy_type", ""), mergedStrategyConfig(item, payloadConfig), explicitType); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	// ── 开仓指标选择器：indicator_params 合法化 ──
	if err := validateIndicatorParams(mergedStrategyConfig(item, payloadConfig)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	// ── 现货策略自定义键合法化 ──
	if err := validateSpotParams(mergedStrategyConfig(item, payloadConfig)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	// ── 策略级 protections 合法化（config_json["protections"]，非法拒绝写库）──
	if err := validateStrategyProtections(mergedStrategyConfig(item, payloadConfig)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	// ── 机器人身份保留/刷新：执行别名字段同步会把 strategy_mode 覆写成
	// execution_mode('paper')，这里对机器人记录恢复 'bot' 标记。 ──
	botType, isBotInBody := botIdentityFromBody(body)
	if isBotInBody || wasBot {
		if tc, ok := body["trading_config"]; ok && tc != nil {
			item["trading_config"] = tc
		}
		if botType == "" {
			botType = getString(item, "bot_type", "")
		}
		persistBotIdentity(item, botType)
	}

	item["updated_at"] = float64(time.Now().UnixMilli())
	// 版本快照钩子：写库前自动打"更新前"快照（失败仅记日志，不影响更新）。
	// 必须放在 SetStrategyConfig 之前——SetStrategyConfig 在 db 可用时本身
	// 就直写 strategy_configs，晚于它快照拍到的就是新状态。
	autoSnapshotStrategyVersion(id, "自动快照（更新前）")
	store.SetStrategyConfig(id, item)
	store.PersistStrategyConfigs()
	persistStrategyConfigToDB(item)
	// 策略级 protections 热重载：运行中的策略按新 config_json 重建/清除
	refreshStrategyProtection(id, item)
	resp := gin.H{"status": "ok"}
	if em, ok := body["execution_mode"].(string); ok && strings.ToLower(strings.TrimSpace(em)) != "paper" && getString(item, "execution_mode", "") == "paper" {
		resp["forced_paper"] = true
	}
	c.JSON(http.StatusOK, resp)
}

// mergeEngineSchedulingKeys 把请求顶层的引擎级键（A7.1 多周期 timeframes、
// A7.2 调度 schedule、轮动候选池 symbols/universe 相关键）并入 config map，
// 让 buildStrategyParams 透传给策略。已存在的键不覆盖。
func mergeEngineSchedulingKeys(config, body map[string]any) {
	for k, v := range engineSchedulingKeys(body) {
		if _, exists := config[k]; !exists {
			config[k] = v
		}
	}
}

// engineSchedulingKeys 提取请求体里的引擎级调度/universe 键。
func engineSchedulingKeys(body map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"timeframes", "schedule", "watchlist", "universe_refresh_bars"} {
		if v, ok := body[k]; ok && v != nil {
			out[k] = v
		}
	}
	return out
}

// flattenCRAParams collects CRA parameter keys that some entry points flatten
// into the request body top level (instead of nesting them under config).
func flattenCRAParams(body map[string]any) map[string]any {
	flat := map[string]any{}
	for _, k := range craFormMarkers {
		if v, ok := body[k]; ok {
			flat[k] = v
		}
	}
	return flat
}

// craFormMarkers are config keys that identify a CRA parameter payload
// (web CRAParamForm). A config carrying any of these keys is treated as a CRA
// config even when strategy_type is missing ("222 式"残废 payload).
var craFormMarkers = []string{
	"first_order_amount", "first_order_multiplier", "add_positions",
	"tp_mode", "take_profit_method", "moving_take_profit_tiers", "enable_add_position",
}

// craCompatibleTypes 是接受 CRA 参数包的策略类型：cra_contract/cra_spot 及
// 后端启动时映射到 CRA 引擎的全部前端别名（见 mapCRAFactory）。参数防呆以
// 这个集合为准——集合外类型（macd/rsi/trend/ScriptStrategy 等纯指标或脚本
// 策略）携带 CRA 特征键即视为错配，防止保存"残废配置"（运行时按参数注册表
// 过滤，CRA 参数被静默丢弃）。
var craCompatibleTypes = map[string]bool{
	"cra_contract": true,
	"cra_spot":     true,
	// mapCRAFactory spotTypes
	"martin_trend":   true,
	"wallstreet":     true,
	"aggressive":     true,
	"conservative":   true,
	"high_frequency": true,
	// mapCRAFactory contractTypes
	"trend_long":          true,
	"trend_short":         true,
	"counter_stable":      true,
	"counter_safe":        true,
	"head_tail_arbitrage": true,
}

// checkStrategyTypeConfigMatch 类型-参数防呆：
//   - CRA 特征键 + 非 CRA 类型 → 400（残废配置拦截）；
//   - 显式 CRA 类型缺少 first_order_amount(≥1) 或 add_positions → 400。
//
// explicitType 表示请求体显式携带 strategy_type。反向（CRA 类型缺参）校验
// 只在显式声明类型时做：推断路径（"222 式"缺 strategy_type、按 CRA 特征键
// 兜底 cra_*）要保留默认补全行为，不能被 400 卡死。
func checkStrategyTypeConfigMatch(stype string, cfg map[string]any, explicitType bool) error {
	stype = strings.ToLower(strings.TrimSpace(stype))
	if stype == "" {
		return nil
	}
	if craCompatibleTypes[stype] {
		if !explicitType {
			return nil
		}
		if getFloat(cfg, "first_order_amount", 0) < 1 {
			return fmt.Errorf("参数与策略类型不匹配：%s 策略需要 first_order_amount（首单金额≥1）", stype)
		}
		addDisabled := false
		if v, ok := cfg["enable_add_position"].(bool); ok && !v {
			addDisabled = true
		}
		if s, ok := cfg["enable_add_position"].(string); ok && (s == "false" || s == "0") {
			addDisabled = true
		}
		if !addDisabled {
			if add, ok := cfg["add_positions"].([]any); !ok || len(add) == 0 {
				return fmt.Errorf("参数与策略类型不匹配：%s 策略需要 add_positions 补仓梯子（或显式关闭补仓）", stype)
			}
		}
		return nil
	}
	for _, k := range craFormMarkers {
		if _, ok := cfg[k]; ok {
			return fmt.Errorf("参数与策略类型不匹配：补仓/移动止盈参数仅适用于 cra_contract/cra_spot 及其模板类型，不适用于 %s", stype)
		}
	}
	return nil
}

// validateIndicatorParams 对 config 里的 indicator_params（开仓指标选择器）
// 做宽松合法化：不是 JSON 对象、已知数值字段非正数、custom 缺 code_id/name
// 一律 400。合法则透传落库（与 cra.ParseCRAParams 的校验口径一致）。
func validateIndicatorParams(cfg map[string]any) error {
	v, exists := cfg["indicator_params"]
	if !exists || v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("indicator_params 必须是 JSON 对象")
	}
	if err := cra.ValidateIndicatorParams(m); err != nil {
		return err
	}
	return nil
}

// validateSpotParams 现货策略自定义参数键宽松校验（price_lower 存在即视为
// 现货参数包）：区间/格数/每格金额/手续费率正数与边界，区间 lower<upper。
// 现货表单同时写引擎认识的 CRA 键（first_order_amount=每格金额、
// order_count=格数、add_positions 等差 ladder），本校验只拦明显非法值。
func validateSpotParams(cfg map[string]any) error {
	if _, exists := cfg["price_lower"]; !exists {
		return nil
	}
	num := func(key string) (float64, bool) {
		v, ok := cfg[key]
		if !ok || v == nil {
			return 0, false
		}
		f, ok := v.(float64)
		return f, ok
	}
	if lower, ok := num("price_lower"); ok && lower <= 0 {
		return fmt.Errorf("现货价格区间下限必须为正数")
	}
	if lower, ok1 := num("price_lower"); ok1 {
		if upper, ok2 := num("price_upper"); ok2 && upper <= lower {
			return fmt.Errorf("现货价格区间上限必须大于下限")
		}
	}
	if n, ok := num("grid_count"); ok && (n < 2 || n > 200) {
		return fmt.Errorf("现货格数必须在 2-200 之间")
	}
	if n, ok := num("per_grid_amount"); ok && n < 1 {
		return fmt.Errorf("现货每格金额必须≥1")
	}
	if n, ok := num("fee_rate"); ok && n < 0 {
		return fmt.Errorf("现货手续费率不能为负数")
	}
	if lm, ok := cfg["loop_mode"].(string); ok && lm != "" && lm != "single" && lm != "cycle" {
		return fmt.Errorf("现货循环模式必须是 single 或 cycle")
	}
	return nil
}

// mergedStrategyConfig 解析 item.config_json 并用 payloadConfig（请求体携带的
// config）覆盖，得到"保存后将生效"的参数视图。
func mergedStrategyConfig(item, payloadConfig map[string]any) map[string]any {
	cfg := map[string]any{}
	if cj, ok := item["config_json"].(string); ok && cj != "" {
		var parsed map[string]any
		if json.Unmarshal([]byte(cj), &parsed) == nil {
			for k, v := range parsed {
				cfg[k] = v
			}
		}
	}
	for k, v := range payloadConfig {
		cfg[k] = v
	}
	return cfg
}

// craContractMarketTypes are market_type values denoting a contract strategy.
var craContractMarketTypes = map[string]bool{"swap": true, "futures": true, "margin": true}

// fillStrategyFieldDefaults backfills the identity fields every persisted
// strategy record must carry. Only empty fields are filled; explicit values
// pass through untouched.
//
// Defaults for the CRA form scenario: strategy_type cra_contract/cra_spot,
// category futures/spot, direction long. execution_mode always defaults to
// paper — 安全红线：空 execution_mode 会让信号单直连真实交易所
// （见 app/context.go 的 signal → order 管线）。
func fillStrategyFieldDefaults(item map[string]any, payloadConfig map[string]any) {
	cfg := map[string]any{}
	for k, v := range payloadConfig {
		cfg[k] = v
	}
	if cj, ok := item["config_json"].(string); ok && cj != "" {
		var parsed map[string]any
		if json.Unmarshal([]byte(cj), &parsed) == nil {
			for k, v := range parsed {
				if _, exists := cfg[k]; !exists {
					cfg[k] = v
				}
			}
		}
	}

	isContract := craContractMarketTypes[strings.ToLower(getString(item, "market_type", ""))] ||
		craContractMarketTypes[strings.ToLower(getString(cfg, "market_type", ""))]
	if cat := strings.ToLower(getString(item, "category", "")); cat == "contract" || cat == "futures" {
		isContract = true
	}
	if getFloat(item, "leverage", 0) > 1 || getFloat(cfg, "leverage", 0) > 1 {
		isContract = true
	}

	isCRA := false
	for _, k := range craFormMarkers {
		if _, ok := cfg[k]; ok {
			isCRA = true
			break
		}
	}

	if getString(item, "strategy_type", "") == "" && isCRA {
		if isContract {
			item["strategy_type"] = "cra_contract"
		} else {
			item["strategy_type"] = "cra_spot"
		}
	}
	if getString(item, "category", "") == "" {
		if isContract {
			item["category"] = "futures"
		} else {
			item["category"] = "spot"
		}
	}
	// 安全红线：一切下单默认 paper。execution_mode=live 仅在实盘总闸开启时
	// 放行（用户明确要求加回实盘选项，2026-09-17）；signal 等直连值与未开启
	// 总闸的 live 一律压回 paper 并留痕。将来收紧/放宽实盘时应只改总闸。
	em := strings.ToLower(getString(item, "execution_mode", ""))
	switch {
	case em == "":
		item["execution_mode"] = "paper"
	case em == "live" && isLiveTradingEnabled():
		// 实盘总闸已开启：接受 live。
	case em != "paper":
		log.Printf("[strategy] execution_mode=%s 已压回 paper（安全红线，实盘未开启）: id=%v name=%v type=%v",
			em, item["id"], item["name"], item["strategy_type"])
		item["execution_mode"] = "paper"
	}
	if getString(item, "direction", "") == "" {
		switch d := strings.ToLower(getString(cfg, "direction", "")); d {
		case "long", "short", "dual":
			item["direction"] = d
		default:
			item["direction"] = "long"
		}
	}
	// Keep alias fields in sync with the resolved values.
	if v := getString(item, "strategy_type", ""); v != "" {
		item["type"] = v
		item["strategy_name"] = v
	}
	if v := getString(item, "execution_mode", ""); v != "" {
		item["mode"] = v
		item["strategy_mode"] = v
	}
	if v := getString(item, "direction", ""); v != "" {
		item["trade_direction"] = v
	}
}

func DeleteStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	item := store.GetStrategyConfig(id)
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	if !requireOwner(c, getInt64Of(item, "user_id")) {
		return
	}
	if !store.DeleteStrategyConfig(id) {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	store.PersistStrategyConfigs()
	if err := store.NewStrategyConfigRepo().Delete(id); err != nil {
		log.Printf("[strategy] delete from DB failed (id=%s): %v", id, err)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// batchItemResult 是批量操作的单条结果（P1-8 汇总 toast 数据源）。
type batchItemResult struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func BatchStartConfigs(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	ids := getStringSlice(body, "ids")
	nowTS := float64(time.Now().UnixMilli())
	results := make([]batchItemResult, 0, len(ids))
	started, failed := 0, 0
	for _, sid := range ids {
		item := store.GetStrategyConfig(sid)
		if item == nil {
			failed++
			results = append(results, batchItemResult{ID: sid, OK: false, Error: "策略不存在"})
			continue
		}
		if !ownsResource(c, getInt64Of(item, "user_id")) {
			failed++
			results = append(results, batchItemResult{ID: sid, OK: false, Error: "无权操作该策略"})
			continue
		}
		if err := startStrategyInEngine(sid, item); err != nil {
			failed++
			results = append(results, batchItemResult{ID: sid, Name: getString(item, "name", ""), OK: false, Error: err.Error()})
			continue
		}
		started++
		item["status"] = "running"
		item["updated_at"] = nowTS
		store.SetStrategyConfig(sid, item)
		results = append(results, batchItemResult{ID: sid, Name: getString(item, "name", ""), OK: true})
	}
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "results": results, "started": started, "failed": failed})
}

func BatchStopConfigs(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	ids := getStringSlice(body, "ids")
	nowTS := float64(time.Now().UnixMilli())
	results := make([]batchItemResult, 0, len(ids))
	stopped, failed := 0, 0
	for _, sid := range ids {
		item := store.GetStrategyConfig(sid)
		if item == nil {
			failed++
			results = append(results, batchItemResult{ID: sid, OK: false, Error: "策略不存在"})
			continue
		}
		if !ownsResource(c, getInt64Of(item, "user_id")) {
			failed++
			results = append(results, batchItemResult{ID: sid, OK: false, Error: "无权操作该策略"})
			continue
		}
		stopStrategyInEngine(sid)
		item["status"] = "stopped"
		item["updated_at"] = nowTS
		store.SetStrategyConfig(sid, item)
		stopped++
		results = append(results, batchItemResult{ID: sid, Name: getString(item, "name", ""), OK: true})
	}
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "results": results, "stopped": stopped, "failed": failed})
}

func BatchCloseConfigs(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	ids := getStringSlice(body, "ids")
	nowTS := float64(time.Now().UnixMilli())
	closed := 0
	for _, sid := range ids {
		item := store.GetStrategyConfig(sid)
		if item == nil {
			continue
		}
		if !ownsResource(c, getInt64Of(item, "user_id")) {
			continue
		}
		stopStrategyInEngine(sid)
		item["status"] = "stopped"
		item["closed_at"] = nowTS
		item["updated_at"] = nowTS
		store.SetStrategyConfig(sid, item)
		closed++
	}
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "closed": closed})
}

func BatchDeleteConfigs(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	ids := getStringSlice(body, "ids")
	deleted := 0
	for _, sid := range ids {
		item := store.GetStrategyConfig(sid)
		if item == nil {
			continue
		}
		if !ownsResource(c, getInt64Of(item, "user_id")) {
			continue
		}
		stopStrategyInEngine(sid)
		store.DeleteStrategyConfig(sid)
		deleted++
	}
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "deleted": deleted})
}

// ResumeRunningStrategiesLoop 进程启动后恢复所有 status=running 的策略（断点续跑），
// 之后每 60s 复查一次（防御启动时行情/组件未就绪导致的恢复失败）。
// 此前服务器每次重启后策略都处于"DB 说 running、引擎是空"的假死状态。
func ResumeRunningStrategiesLoop() {
	resume := func() {
		for _, item := range store.GetStrategyConfigs() {
			if getString(item, "status", "") != "running" {
				continue
			}
			id := getString(item, "id", "")
			if id == "" {
				continue
			}
			// 已在引擎中运行的策略不是"假死"，直接跳过。
			// startStrategyInEngine 会先停再启（供用户手动重启用），
			// 若对它每 60s 调用一次，等于每分钟重启全部策略——每次重启
			// 都触发即时开单信号，把风控每日单量限额打爆并跳熔断。
			if eng := strategy.GetEngine(nil); eng != nil && eng.Get(id) != nil {
				continue
			}
			if err := startStrategyInEngine(id, item); err != nil {
				if !strings.Contains(err.Error(), "already registered") {
					log.Printf("[strategy] resume %s (%s) failed: %v", id, getString(item, "name", ""), err)
				}
				continue
			}
			log.Printf("[strategy] resumed %s (%s)", id, getString(item, "name", ""))
		}
	}
	resume()
	ticker := time.NewTicker(60 * time.Second)
	for range ticker.C {
		resume()
	}
}

func StartStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	item := store.GetStrategyConfig(id)
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	if !requireOwner(c, getInt64Of(item, "user_id")) {
		return
	}
	if err := startStrategyInEngine(id, item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	item["status"] = "running"
	item["updated_at"] = float64(time.Now().UnixMilli())
	store.SetStrategyConfig(id, item)
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func StopStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	item := store.GetStrategyConfig(id)
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	if !requireOwner(c, getInt64Of(item, "user_id")) {
		return
	}
	stopStrategyInEngine(id)
	item["status"] = "stopped"
	item["updated_at"] = float64(time.Now().UnixMilli())
	store.SetStrategyConfig(id, item)
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// startStrategyInEngine registers and starts a strategy instance in the engine.
func startStrategyInEngine(id string, item map[string]any) error {
	strategyType, _ := item["strategy_type"].(string)
	if strategyType == "" {
		// Fallback: try bot_type for frontend-created bot strategies
		strategyType, _ = item["bot_type"].(string)
	}
	if strategyType == "" {
		return fmt.Errorf("strategy_type not set")
	}

	eng := strategy.GetEngine(nil)
	if eng == nil {
		return fmt.Errorf("strategy engine not initialized")
	}

	// If already registered with this id, stop and unregister first
	releaseKlineFeed(id)
	if existing := eng.Get(id); existing != nil {
		_ = eng.Stop(id)
		_ = eng.Unregister(id)
	}

	// Map CRA frontend strategy types to unified backend factories.
	factoryName := strategyType
	if mapped, ok := mapCRAFactory(strategyType, item); ok {
		factoryName = mapped
	}

	// Create strategy instance from factory
	s := strategy.StrategyFactory(factoryName)
	if s == nil {
		return fmt.Errorf("unknown strategy type: %s", strategyType)
	}

	// Wrap with config id as unique name
	wrapped := strategy.WrapStrategy(id, s)

	// Build params from config
	params := buildStrategyParams(item)

	// Normalize the Binance symbol to its canonical uppercase form so the
	// engine subscription topic, the strategy symbol, and the K线供给管
	// publish topic all agree.
	if sym, ok := params["symbol"].(string); ok {
		params["symbol"] = strings.ToUpper(strings.TrimSpace(sym))
	}

	// Filter params to only those accepted by the strategy's parameter registry.
	// CRA-style configs carry many frontend fields that indicator strategies do not declare.
	// 引擎级键（symbol/timeframe/timeframes/schedule）始终保留：策略要靠它们
	// 识别交易对、声明多周期供给（A7.1）与计划调度（A7.2）。
	if registry := s.GetParameters(); registry != nil {
		engineKeys := map[string]bool{"symbol": true, "timeframe": true, "timeframes": true, "schedule": true}
		filtered := make(map[string]any)
		for _, p := range registry.All() {
			if v, ok := params[p.Name]; ok {
				filtered[p.Name] = v
			}
		}
		for k, v := range params {
			if engineKeys[strings.ToLower(k)] {
				filtered[k] = v
			}
		}
		params = filtered
	}

	// 策略级 protections（config_json["protections"]，hyperopt epoch 回写）：
	// 配置非法拒绝启动——静默裸奔比明确报错更危险。
	protMgr, err := strategyProtectionManagerFromConfig(item)
	if err != nil {
		return err
	}

	// Register and start
	if err := eng.Register(wrapped); err != nil {
		return fmt.Errorf("register strategy: %w", err)
	}
	if protMgr != nil {
		eng.SetStrategyProtectionManager(id, protMgr)
	}
	if err := eng.Start(id, params); err != nil {
		_ = eng.Unregister(id) // Unregister 一并释放策略级 protection manager
		return fmt.Errorf("start strategy: %w", err)
	}
	// 引擎订阅的 topic 是 wrapped.Symbol()（Start 应用参数后的值），
	// K 线供给管必须按同一 symbol 发布，策略的 OnBar 才收得到。
	timeframe, _ := item["timeframe"].(string)
	if fresh := ensureKlineFeed(id, wrapped.Symbol(), timeframe); !fresh {
		// 供给管已在运行（其他策略共用），不会有历史回补推给本策略。
		// 直接喂最近 100 根已闭合 K 线暖机：只吃 K 线养指标状态，信号丢弃，
		// 实盘信号等下一根真实 K 线（否则新策略最长要干等一个完整周期）。
		if klineFeeder != nil {
			if bars, err := klineFeeder.RecentClosedBars(wrapped.Symbol(), timeframe, 100); err == nil && len(bars) > 0 {
				for _, b := range bars {
					_, _ = wrapped.OnBar(b, nil)
				}
				log.Printf("[KlineFeed] direct warmup %d bars for strategy %s (%s %s)",
					len(bars), id, wrapped.Symbol(), timeframe)
			}
		}
	}
	return nil
}

// mapCRAFactory maps frontend CRA strategy types to the unified backend factories.
func mapCRAFactory(strategyType string, item map[string]any) (string, bool) {
	spotTypes := map[string]bool{
		"martin_trend":   true,
		"wallstreet":     true,
		"aggressive":     true,
		"conservative":   true,
		"high_frequency": true,
	}
	contractTypes := map[string]bool{
		"trend_long":          true,
		"trend_short":         true,
		"counter_stable":      true,
		"counter_safe":        true,
		"high_frequency":      true,
		"head_tail_arbitrage": true,
	}
	if spotTypes[strategyType] {
		// high_frequency is ambiguous: use category/market_type if available.
		if strategyType == "high_frequency" {
			if isContractCategory(item) {
				return "cra_contract", true
			}
		}
		return "cra_spot", true
	}
	if contractTypes[strategyType] {
		return "cra_contract", true
	}
	return "", false
}

func isContractCategory(item map[string]any) bool {
	if v, ok := item["category"].(string); ok {
		return v == "contract"
	}
	if v, ok := item["market_type"].(string); ok {
		return v == "swap" || v == "futures" || v == "margin"
	}
	return false
}

// isBotItem 判定一条配置是否属于"策略机器人"（vs 策略实验室）。这是后端
// 唯一的归属判别口径，前端不再用 market_type 启发式猜谜：
//   - strategy_mode/mode == 'bot'（含 DB 往返后的别名）；
//   - 存在 bot_type（顶层，或 trading_config.bot_type —— map 或 JSON 串均可，
//     或 config_json 解析结果里的 bot_type）。
//   - category ∈ {martin, wallstreet}（历史机器人品类）。
func isBotItem(it map[string]any) bool {
	// CRA 壳策略（现货/合约策略管理创建）永远归策略侧——即使带 bot 标记
	// （历史暗道保存的 顺势多444/333 等）。用户明确：机器人页只留真机器人，
	// 策略实例全部在策略管理页。须放在 bot 标记判定之前。
	switch strings.ToLower(getString(it, "strategy_type", getString(it, "type", ""))) {
	case "cra_contract", "cra_spot":
		return false
	}
	if strings.EqualFold(getString(it, "strategy_mode", ""), "bot") {
		return true
	}
	if strings.EqualFold(getString(it, "mode", ""), "bot") {
		return true
	}
	if getString(it, "bot_type", "") != "" {
		return true
	}
	if cfg, ok := it["config"].(map[string]any); ok && getString(cfg, "bot_type", "") != "" {
		return true
	}
	if tc := getString(it, "trading_config", ""); tc != "" {
		var m map[string]any
		if json.Unmarshal([]byte(tc), &m) == nil && getString(m, "bot_type", "") != "" {
			return true
		}
	}
	if tc, ok := it["trading_config"].(map[string]any); ok && getString(tc, "bot_type", "") != "" {
		return true
	}
	switch strings.ToLower(getString(it, "category", "")) {
	case "martin", "wallstreet":
		return true
	}
	return false
}

// hydrateBotFields 从 config_json 解析结果把 bot_type/trading_config 兜底恢
// 复到顶层。bot 身份字段不是 DB 列：SetStrategyConfig 落库走
// StrategyConfigRecordFromMap（白名单列），DB 重建（ToMap）后顶层丢失，
// 但 config_json 是持久载体，创建时已把 bot 标记写入其中。
func hydrateBotFields(it map[string]any) {
	if getString(it, "bot_type", "") != "" && it["trading_config"] != nil {
		return
	}
	cj, ok := it["config_json"].(string)
	if !ok || cj == "" {
		return
	}
	var cfg map[string]any
	if json.Unmarshal([]byte(cj), &cfg) != nil || cfg == nil {
		return
	}
	if getString(it, "bot_type", "") == "" {
		if bt, ok := cfg["bot_type"].(string); ok && bt != "" {
			it["bot_type"] = bt
		}
	}
	if it["trading_config"] == nil {
		if tc, ok := cfg["trading_config"]; ok && tc != nil {
			it["trading_config"] = tc
		}
	}
}

// botIdentityFromBody 从创建/更新请求体提取 bot 身份：
// strategy_mode=='bot'、顶层 bot_type、或 trading_config.bot_type 任一命中
// 即视为机器人配置，返回 (botType, true)。
func botIdentityFromBody(body map[string]any) (string, bool) {
	botType := getString(body, "bot_type", "")
	if botType == "" {
		if tc, ok := body["trading_config"].(map[string]any); ok {
			botType = getString(tc, "bot_type", "")
		}
	}
	isBot := strings.EqualFold(getString(body, "strategy_mode", ""), "bot") || botType != ""
	return botType, isBot
}

// persistBotIdentity 把 bot 身份写进 item：顶层 bot_type + strategy_mode/mode
// 置 'bot'（execution_mode 仍按安全红线保存为 paper），并合入 config_json —
// config_json 是 DB 重建后唯一可靠的身份载体。
func persistBotIdentity(item map[string]any, botType string) {
	if botType != "" {
		item["bot_type"] = botType
	}
	item["strategy_mode"] = "bot"
	item["mode"] = "bot"
	cj := getString(item, "config_json", "")
	cfg := map[string]any{}
	if cj != "" {
		var parsed map[string]any
		if json.Unmarshal([]byte(cj), &parsed) == nil && parsed != nil {
			cfg = parsed
		}
	}
	if botType != "" {
		if _, exists := cfg["bot_type"]; !exists {
			cfg["bot_type"] = botType
		}
	}
	if _, exists := cfg["trading_config"]; !exists {
		if tc, ok := item["trading_config"]; ok && tc != nil {
			cfg["trading_config"] = tc
		}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	item["config_json"] = string(data)
}

// stopStrategyInEngine stops and unregisters a strategy from the engine.
func stopStrategyInEngine(id string) {
	releaseKlineFeed(id)
	eng := strategy.GetEngine(nil)
	if eng == nil {
		return
	}
	_ = eng.Stop(id)
	_ = eng.Unregister(id)
}

// buildStrategyParams extracts strategy parameters from a config item.
func buildStrategyParams(item map[string]any) map[string]any {
	params := make(map[string]any)

	// Copy basic fields
	for _, key := range []string{"symbol", "coin", "direction", "leverage", "category", "market_type", "margin_mode", "timeframe", "execution_mode", "initial_capital"} {
		if v, ok := item[key]; ok {
			params[key] = v
		}
	}

	// Parse config_json
	if cj, ok := item["config_json"].(string); ok && cj != "" {
		var parsed map[string]any
		if json.Unmarshal([]byte(cj), &parsed) == nil {
			for k, v := range parsed {
				params[k] = v
			}
		}
	}

	// Ensure symbol is set
	if params["symbol"] == nil || params["symbol"] == "" {
		if coin, ok := params["coin"].(string); ok && coin != "" {
			params["symbol"] = coin + "USDT"
		} else {
			params["symbol"] = "BTCUSDT"
		}
	}

	// ── Contract fields from config_json ──
	if params["market_type"] == nil || params["market_type"] == "" {
		params["market_type"] = "spot"
	}
	if params["margin_mode"] == nil || params["margin_mode"] == "" {
		params["margin_mode"] = "cross"
	}
	// Extract position_side from direction
	if direction, ok := params["direction"].(string); ok {
		switch direction {
		case "long":
			params["position_side"] = "LONG"
		case "short":
			params["position_side"] = "SHORT"
		case "dual", "both":
			params["position_side"] = "BOTH"
		}
	}

	return params
}

func GetStrategyLogs(c *gin.Context) {
	sid := c.Query("strategy_id")
	limit := 200
	fmtScan(c.Query("limit"), &limit)
	logs := *store.GetLogsStore()
	if sid != "" {
		// 越权防护：已登录用户只能读本人(或无属主)策略的日志
		if _, injected := ctxUserID(c); injected {
			if item := store.GetStrategyConfig(sid); item != nil {
				if !requireOwner(c, getInt64Of(item, "user_id")) {
					return
				}
			}
		}
		logs = filterMap(logs, "strategy_id", sid)
	} else if _, injected := ctxUserID(c); injected && !ctxIsAdmin(c) {
		// 全量日志：过滤掉属他人策略的日志（策略已删除的宽松放行）
		filtered := make([]map[string]any, 0, len(logs))
		for _, l := range logs {
			item := store.GetStrategyConfig(getString(l, "strategy_id", ""))
			if item != nil && !ownsResource(c, getInt64Of(item, "user_id")) {
				continue
			}
			filtered = append(filtered, l)
		}
		logs = filtered
	}
	if len(logs) > limit {
		logs = logs[len(logs)-limit:]
	}
	if logs == nil {
		logs = []map[string]any{}
	}
	c.JSON(http.StatusOK, logs)
}

func ClearStrategyLogs(c *gin.Context) {
	sid := c.Query("strategy_id")
	if _, injected := ctxUserID(c); injected && sid != "" {
		// 越权防护：不能清他人策略的日志
		if item := store.GetStrategyConfig(sid); item != nil {
			if !requireOwner(c, getInt64Of(item, "user_id")) {
				return
			}
		}
	}
	logs := store.GetLogsStore()
	if sid != "" {
		var filtered []map[string]any
		for _, l := range *logs {
			if getString(l, "strategy_id", "") != sid {
				filtered = append(filtered, l)
			}
		}
		*logs = filtered
	} else if _, injected := ctxUserID(c); injected && !ctxIsAdmin(c) {
		// 全量清空：只清本人(或无属主)策略的日志，保留他人日志
		var filtered []map[string]any
		for _, l := range *logs {
			item := store.GetStrategyConfig(getString(l, "strategy_id", ""))
			if item != nil && !ownsResource(c, getInt64Of(item, "user_id")) {
				filtered = append(filtered, l)
			}
		}
		*logs = filtered
	} else {
		*logs = nil
	}
	store.PersistStrategyLogs()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func GetTemplates(c *gin.Context) {
	category := c.DefaultQuery("category", "spot")
	limit := 200
	fmtScan(c.Query("limit"), &limit)

	userID := getUserID(c)
	items, err := store.GetStrategyTemplateRepo().List(userID, category, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to list templates"})
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, rec := range items {
		result = append(result, rec.ToMap())
	}
	if result == nil {
		result = []map[string]any{}
	}
	c.JSON(http.StatusOK, result)
}

func CreateTemplate(c *gin.Context) {
	var data map[string]any
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json"})
		return
	}
	userID := getUserID(c)
	name := getString(data, "strategy_name", getString(data, "name", ""))
	if strings.TrimSpace(name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "template name required"})
		return
	}
	defaultConfig := "{}"
	if dc, ok := data["default_config"]; ok {
		switch v := dc.(type) {
		case string:
			defaultConfig = v
		default:
			b, _ := json.Marshal(v)
			defaultConfig = string(b)
		}
	}
	rec := &store.StrategyTemplateRecord{
		UserID:            userID,
		Name:              strings.TrimSpace(name),
		Category:          getString(data, "category", "spot"),
		StrategyType:      getString(data, "strategy_type", ""),
		Description:       getString(data, "description", ""),
		DefaultConfigJSON: defaultConfig,
	}
	if err := store.GetStrategyTemplateRepo().Create(rec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to create template"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "id": rec.ID})
}

func DeleteTemplate(c *gin.Context) {
	id := c.Param("id")
	userID := getUserID(c)
	deleted, err := store.GetStrategyTemplateRepo().Delete(id, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to delete template"})
		return
	}
	if !deleted {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Template not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// getUserID extracts the authenticated user ID from the Gin context.
func getUserID(c *gin.Context) int64 {
	if v, ok := c.Get("user_id"); ok {
		switch val := v.(type) {
		case int:
			return int64(val)
		case int64:
			return val
		case float64:
			return int64(val)
		}
	}
	return 0
}

// ── Helpers ──

func filterMap(items []map[string]any, key, val string) []map[string]any {
	var result []map[string]any
	for _, item := range items {
		if v, ok := item[key].(string); ok && v == val {
			result = append(result, item)
		}
	}
	return result
}

func filterMapContains(items []map[string]any, key, val string) []map[string]any {
	var result []map[string]any
	for _, item := range items {
		if v, ok := item[key].(string); ok && strings.Contains(strings.ToLower(v), strings.ToLower(val)) {
			result = append(result, item)
		}
	}
	return result
}

func copyMap(src map[string]any) map[string]any {
	dst := make(map[string]any)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func getStringSlice(m map[string]any, key string) []string {
	if arr, ok := m[key].([]any); ok {
		var result []string
		for _, v := range arr {
			if s, ok := v.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

func shortUUID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// GetStrategyParamDefs returns parameter definitions for a strategy type.
// Used by the frontend to render dynamic configuration forms.
func GetStrategyParamDefs(c *gin.Context) {
	strategyType := c.Query("type")
	if strategyType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "type query param required"})
		return
	}

	// CRA 策略的前端表单由 CRAParamForm 统一渲染，不需要动态参数定义。
	craTypes := map[string]bool{
		"cra_contract":        true,
		"cra_spot":            true,
		"martin_trend":        true,
		"wallstreet":          true,
		"aggressive":          true,
		"conservative":        true,
		"high_frequency":      true,
		"high_flat":           true,
		"counter_stable":      true,
		"counter_safe":        true,
		"head_tail_arbitrage": true,
		"dual_burn":           true,
		"global_burn":         true,
		"trend_long":          true,
		"trend_short":         true,
	}
	if craTypes[strategyType] {
		c.JSON(http.StatusOK, gin.H{"type": strategyType, "params": []map[string]any{}})
		return
	}

	var defs []map[string]any

	switch strategyType {
	case "breakout", "trend", "custom":
		s := strategies.NewBreakoutStrategy()
		defs = s.ParamDefs()
	case "ema_cross", "ema_follow", "ema_counter", "ema_follow_trend", "ema_counter_trend", "ema_spot":
		s := strategies.NewEMACrossStrategy()
		defs = s.ParamDefs()
	case "macd", "macd_golden", "macd_death", "macd_spot_long":
		s := strategies.NewMACDStrategy()
		defs = s.ParamDefs()
	case "rsi":
		s := strategies.NewRSIStrategy()
		defs = s.ParamDefs()
	case "bollinger_bands":
		s := strategies.NewBollingerBandsStrategy()
		defs = s.ParamDefs()
	case "atr_trailing_stop":
		s := strategies.NewATRTrailingStopStrategy()
		defs = s.ParamDefs()
	case "dual_thrust":
		s := strategies.NewDualThrustStrategy()
		defs = s.ParamDefs()
	case "renko":
		s := strategies.NewRenkoStrategy()
		defs = s.ParamDefs()
	case "grid_trading", "grid":
		s := strategies.NewGridTradingStrategy()
		defs = s.ParamDefs()
	case "arbitrage", "head_tail_arb":
		s := strategies.NewArbitrageStrategy()
		defs = s.ParamDefs()
	case "market_making":
		s := strategies.NewMarketMakingStrategy()
		defs = s.ParamDefs()
	case "martingale", "dca":
		s := strategies.NewMartingaleStrategy()
		defs = s.ParamDefs()
	case "universe_rotation":
		defs = strategies.NewUniverseRotationStrategy().ParamDefs()
	case "trend_long_mt":
		defs = strategies.NewTrendLongStrategy().ParamDefs()
	case "trend_short_mt":
		defs = strategies.NewTrendShortStrategy().ParamDefs()
	// AI Bot marketplace aliases (registered in cmd/server/main.go)
	case "optimus", "mono_optimus", "noah":
		s := strategies.NewGridTradingStrategy()
		defs = s.ParamDefs()
	case "cyberbot", "mono_cyberbot":
		s := strategies.NewRSIStrategy()
		defs = s.ParamDefs()
	case "crypto_future", "ai_alpha_futures", "alt_volatility":
		s := strategies.NewDualThrustStrategy()
		defs = s.ParamDefs()
	case "ai_alpha":
		s := strategies.NewEMACrossStrategy()
		defs = s.ParamDefs()
	case "terminator_volatility":
		s := strategies.NewATRTrailingStopStrategy()
		defs = s.ParamDefs()
	case "trade_holder":
		s := strategies.NewMartingaleStrategy()
		defs = s.ParamDefs()
	case "ml":
		defs = []map[string]any{
			{"name": "model_id", "type": "string", "required": true, "description": "已训练的ML模型ID"},
			{"name": "symbol", "type": "symbol", "required": true, "description": "交易对"},
			{"name": "interval", "type": "interval", "required": true, "description": "K线周期"},
			{"name": "predict_threshold", "type": "float", "default": 0.001, "description": "预测阈值，预测值超过此绝对值时开仓"},
			{"name": "position_size_pct", "type": "float", "default": 0.02, "min": 0.005, "max": 0.5, "description": "单次仓位比例"},
			{"name": "max_holding_bars", "type": "int", "default": 48, "min": 1, "max": 500, "description": "最大持仓K线数"},
			{"name": "stop_loss_pct", "type": "float", "default": 0.05, "min": 0.005, "max": 0.5, "description": "止损百分比"},
			{"name": "take_profit_pct", "type": "float", "default": 0.10, "min": 0.005, "max": 1.0, "description": "止盈百分比"},
			{"name": "direction", "type": "choice", "choices": []string{"long", "short", "both"}, "default": "both", "description": "交易方向"},
			{"name": "use_ensemble", "type": "boolean", "default": false, "description": "是否使用多模型集成预测"},
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"detail": "unknown strategy type: " + strategyType})
		return
	}

	if defs == nil {
		defs = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{"type": strategyType, "params": defs})
}

// normalizeStrategyConfig converts a raw store strategy config map into the
// frontend-expected StrategyItem format. It maps backend field names to frontend
// field names and converts Unix timestamps to ISO-8601 strings.
func normalizeStrategyConfig(it map[string]any) map[string]any {
	result := make(map[string]any)

	// Copy basic fields
	for _, k := range []string{"id", "name", "symbol", "status", "leverage", "timeframe", "initial_capital", "current_equity", "total_pnl", "total_pnl_percent", "group_id", "group_name", "indicator_name", "market_type", "margin_mode", "config", "config_json", "bot_type", "trading_config"} {
		if v, ok := it[k]; ok {
			result[k] = v
		}
	}

	// Field name mappings
	if v, ok := it["strategy_type"].(string); ok && v != "" {
		result["type"] = v
		result["strategy_name"] = v
	} else if v, ok := it["type"].(string); ok {
		result["type"] = v
		result["strategy_name"] = v
	}

	if v, ok := it["execution_mode"].(string); ok && v != "" {
		result["mode"] = v
		result["strategy_mode"] = v
	} else if v, ok := it["mode"].(string); ok {
		result["mode"] = v
		result["strategy_mode"] = v
	}

	if v, ok := it["direction"].(string); ok && v != "" {
		result["trade_direction"] = v
	} else if v, ok := it["trade_direction"].(string); ok {
		result["trade_direction"] = v
	}

	if v, ok := it["category"].(string); ok && v != "" {
		result["market_category"] = v
	}

	if v, ok := it["pnl"].(float64); ok {
		if _, hasTotalPnl := result["total_pnl"]; !hasTotalPnl {
			result["total_pnl"] = v
		}
	}

	// 策略级 protections（config_json["protections"]，hyperopt 回写）提升为顶层
	// 字段，前端策略列表/详情直接展示，不必各自再解析 config。
	if prot := strategyProtectionsView(it); prot != nil {
		result["protections"] = prot
	}

	// Time conversion: backend stores float64 Unix milliseconds, frontend expects ISO string
	if v, ok := it["created_at"].(float64); ok && v > 0 {
		result["created_at"] = time.UnixMilli(int64(v)).Format(time.RFC3339)
	} else if v, ok := it["created_at"].(string); ok {
		result["created_at"] = v
	}
	if v, ok := it["updated_at"].(float64); ok && v > 0 {
		result["updated_at"] = time.UnixMilli(int64(v)).Format(time.RFC3339)
	} else if v, ok := it["updated_at"].(string); ok {
		result["updated_at"] = v
	}

	return result
}
