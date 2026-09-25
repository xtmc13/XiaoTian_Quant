package adapter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 交易所体检（对标 freqtrade check_exchange 的一键健康检查）──
//
// 分级语义（每级独立计时、独立成败，单项未实现记 skip 而非 fail）：
//   L1 公共连通：ping（如适配器实现）、ticker、OHLCV、orderbook——无需凭证。
//   L2 凭证有效：余额 / 持仓 / 未成交订单。未配置凭证时整级 skip。
//   L3 交易能力元数据：手续费、精度/最小下单量、杠杆档位、合约权限探测——
//      仅通过查询类接口探测，禁止真实下单。适配器未实现对应接口即 skip。
//   L4 WebSocket：短连接握手 + 首条消息超时测试（实现了 StartMarketStream 的所）。
//
// overall 汇总规则：
//   unhealthy      L1 失败（公共连通都没有），或 L2 失败（凭证无效/权限不足）
//   not_configured 未配置凭证（L2/L3 自动 skip），但 L1 正常
//   degraded       L1/L2 正常，但 L3 或 L4 存在失败项（部分能力受损）
//   healthy        所有非 skip 项全部通过

const (
	CheckPass = "pass"
	CheckFail = "fail"
	CheckSkip = "skip"

	OverallHealthy       = "healthy"
	OverallDegraded      = "degraded"
	OverallUnhealthy     = "unhealthy"
	OverallNotConfigured = "not_configured"
)

// HealthCheckItem 单项检查结果。
type HealthCheckItem struct {
	Name       string `json:"name"`
	Status     string `json:"status"` // pass / fail / skip
	DurationMs int64  `json:"duration_ms"`
	Detail     string `json:"detail,omitempty"`
	Error      string `json:"error,omitempty"`
}

// HealthCheckLevel 一级检查的汇总。
type HealthCheckLevel struct {
	Level      int               `json:"level"` // 1..4
	Name       string            `json:"name"`
	Status     string            `json:"status"` // 分项中最差状态；全 skip 记 skip
	DurationMs int64             `json:"duration_ms"`
	Items      []HealthCheckItem `json:"items"`
}

// ExchangeHealthReport 一家交易所的一次完整体检报告。
type ExchangeHealthReport struct {
	Exchange   string             `json:"exchange"`
	Configured bool               `json:"configured"`
	Overall    string             `json:"overall"`
	Levels     []HealthCheckLevel `json:"levels"`
	DurationMs int64              `json:"duration_ms"`
	CheckedAt  int64              `json:"checked_at"`
}

// HealthCheckTarget 是体检所需的最小适配器接口（10 家适配器全部满足）。
type HealthCheckTarget interface {
	Name() string
	GetTicker(symbol string) (map[string]any, error)
	GetKlines(symbol, interval string, limit int) ([][]any, error)
	GetBalance() ([]map[string]any, error)
	GetPositions() ([]map[string]any, error)
	GetOpenOrders(symbol string) ([]map[string]any, error)
}

// 可选能力接口：适配器实现了才检查，未实现记 skip。
type healthPinger interface {
	Ping() error
}
type orderBookFetcher interface {
	GetOrderBook(symbol string, limit int) (map[string]any, error)
}
type tradeFeeFetcher interface {
	GetTradeFee(symbol string) (map[string]any, error)
}
type exchangeInfoFetcher interface {
	GetExchangeInfo(symbol string) (map[string]any, error)
}
type leverageBracketFetcher interface {
	GetLeverageBrackets(symbol string) (map[string]any, error)
}
type futuresAccountFetcher interface {
	GetFuturesAccount() (map[string]any, error)
}
type marketStreamer interface {
	StartMarketStream(symbols []string) error
}
type tickerSubscriber interface {
	OnTicker(fn func(tick model.Tick))
}
type stopper interface {
	Stop() error
}

// healthCheckExchanges 是支持体检的交易所（规范名，与 adapter 构造器一一对应）。
var healthCheckExchanges = []string{
	"binance", "okx", "bybit", "kraken", "coinbase",
	"gate", "mexc", "bitget", "alpaca", "ibkr",
}

// healthCheckProbeSymbol 默认探测用 BTCUSDT；股票/券商类用 AAPL。
var healthCheckProbeSymbol = map[string]string{
	"alpaca": "AAPL",
	"ibkr":   "AAPL",
}

// SupportedHealthCheckExchanges 返回支持体检的交易所规范名列表。
func SupportedHealthCheckExchanges() []string {
	out := make([]string, len(healthCheckExchanges))
	copy(out, healthCheckExchanges)
	return out
}

// IsHealthCheckExchange 报告 name 是否支持体检（按规范名判断）。
func IsHealthCheckExchange(name string) bool {
	canonical := normalizeExchangeName(name)
	for _, ex := range healthCheckExchanges {
		if ex == canonical {
			return true
		}
	}
	return false
}

// HealthCheckConfigured 报告该交易所是否已配置可用凭证。
// IBKR 走本地 Client Portal 网关会话认证，无 API key，用 enabled 配置判断。
func HealthCheckConfigured(name string) bool {
	if normalizeExchangeName(name) == "ibkr" {
		return IBKRConfigured()
	}
	return HasCredential(name)
}

// BuildHealthCheckTarget 用 credential vault 中的凭证构造体检目标。
// 凭证只注入适配器内部，绝不出现在任何返回结果里。
func BuildHealthCheckTarget(name string) (HealthCheckTarget, error) {
	canonical := normalizeExchangeName(name)
	apiKey, secret, passphrase := GetCredential(canonical)
	switch canonical {
	case "binance":
		return NewBinanceAdapter(apiKey, secret, false), nil
	case "okx":
		return NewOKXAdapter(apiKey, secret, passphrase, false), nil
	case "bybit":
		return NewBybitAdapter(apiKey, secret, false), nil
	case "kraken":
		return NewKrakenAdapter(apiKey, secret), nil
	case "coinbase":
		return NewCoinbaseAdapter(apiKey, secret), nil
	case "gate":
		return NewGateIOAdapter(apiKey, secret), nil
	case "mexc":
		return NewMEXCAdapter(apiKey, secret), nil
	case "bitget":
		return NewBitgetAdapter(apiKey, secret, passphrase), nil
	case "alpaca":
		return NewAlpacaAdapter(apiKey, secret, false), nil
	case "ibkr":
		return NewIBKRAdapter(LoadIBKRConfig()), nil
	default:
		return nil, fmt.Errorf("unsupported exchange for health check: %s", name)
	}
}

// HealthChecker 执行分级体检编排。零值可用（默认超时也生效）。
type HealthChecker struct {
	// PerCheckTimeout 单项检查看门狗超时（默认 12s）。
	// 超时的检查记 fail；底层调用受适配器 http.Client 自身超时约束随后退出。
	PerCheckTimeout time.Duration
	// WSMessageTimeout L4 等待首条 WS 消息的超时（默认 8s）。
	WSMessageTimeout time.Duration
	// Symbol 覆盖探测交易对（默认按 healthCheckProbeSymbol）。
	Symbol string
}

func (hc *HealthChecker) perCheckTimeout() time.Duration {
	if hc.PerCheckTimeout > 0 {
		return hc.PerCheckTimeout
	}
	return 12 * time.Second
}

func (hc *HealthChecker) wsMessageTimeout() time.Duration {
	if hc.WSMessageTimeout > 0 {
		return hc.WSMessageTimeout
	}
	return 8 * time.Second
}

func (hc *HealthChecker) probeSymbol(exchange string) string {
	if hc.Symbol != "" {
		return hc.Symbol
	}
	if sym, ok := healthCheckProbeSymbol[exchange]; ok {
		return sym
	}
	return "BTCUSDT"
}

// Run 对指定交易所执行完整分级体检。exchange 为规范名或别名（如 gateio）。
func (hc *HealthChecker) Run(ctx context.Context, exchange string) (*ExchangeHealthReport, error) {
	canonical := normalizeExchangeName(exchange)
	target, err := BuildHealthCheckTarget(canonical)
	if err != nil {
		return nil, err
	}
	secrets := healthCheckSecrets(canonical)
	return hc.runAgainst(ctx, canonical, target, HealthCheckConfigured(canonical), secrets), nil
}

// runAgainst 对给定目标执行体检（单测用 mock target 驱动）。
func (hc *HealthChecker) runAgainst(ctx context.Context, exchange string, target HealthCheckTarget, configured bool, secrets []string) *ExchangeHealthReport {
	started := time.Now()
	report := &ExchangeHealthReport{
		Exchange:   exchange,
		Configured: configured,
		CheckedAt:  started.UnixMilli(),
	}
	symbol := hc.probeSymbol(exchange)

	report.Levels = append(report.Levels,
		hc.checkL1Public(ctx, target, symbol, secrets),
		hc.checkL2Credentials(ctx, target, symbol, configured, secrets),
		hc.checkL3TradingMeta(ctx, target, symbol, configured, secrets),
		hc.checkL4WebSocket(ctx, target, symbol, secrets),
	)

	report.Overall = summarizeOverall(configured, report.Levels)
	report.DurationMs = time.Since(started).Milliseconds()

	// 体检专用实例自带 WS 流，用完即关，避免句柄泄漏。
	if s, ok := target.(stopper); ok {
		_ = s.Stop()
	}
	return report
}

// healthCheckSecrets 收集需要脱敏的密钥材料（空串忽略）。
func healthCheckSecrets(exchange string) []string {
	apiKey, secret, passphrase := GetCredential(exchange)
	var out []string
	for _, s := range []string{apiKey, secret, passphrase} {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// sanitizeError 把错误文本中出现的密钥替换为 "***"。
func sanitizeError(msg string, secrets []string) string {
	for _, s := range secrets {
		if s != "" {
			msg = strings.ReplaceAll(msg, s, "***")
		}
	}
	return msg
}

// runItem 执行单项检查：独立计时 + 看门狗超时 + 错误脱敏。
func (hc *HealthChecker) runItem(ctx context.Context, name string, secrets []string, fn func() (string, error)) HealthCheckItem {
	started := time.Now()
	item := HealthCheckItem{Name: name}

	type outcome struct {
		detail string
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		detail, err := fn()
		done <- outcome{detail, err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			item.Status = CheckFail
			item.Error = sanitizeError(res.err.Error(), secrets)
		} else {
			item.Status = CheckPass
			item.Detail = sanitizeError(res.detail, secrets)
		}
	case <-time.After(hc.perCheckTimeout()):
		item.Status = CheckFail
		item.Error = fmt.Sprintf("检查超时（>%s）", hc.perCheckTimeout())
	case <-ctx.Done():
		item.Status = CheckFail
		item.Error = "体检任务被取消"
	}
	item.DurationMs = time.Since(started).Milliseconds()
	return item
}

func skipItem(name, reason string) HealthCheckItem {
	return HealthCheckItem{Name: name, Status: CheckSkip, Detail: reason}
}

// levelStatus 汇总一级状态：任一 fail → fail；否则任一 pass → pass；全 skip → skip。
func levelStatus(items []HealthCheckItem) string {
	status := CheckSkip
	for _, it := range items {
		if it.Status == CheckFail {
			return CheckFail
		}
		if it.Status == CheckPass {
			status = CheckPass
		}
	}
	return status
}

func finishLevel(level int, name string, started time.Time, items []HealthCheckItem) HealthCheckLevel {
	return HealthCheckLevel{
		Level:      level,
		Name:       name,
		Status:     levelStatus(items),
		DurationMs: time.Since(started).Milliseconds(),
		Items:      items,
	}
}

// summarizeOverall 按文件头注释的规则汇总 overall。
func summarizeOverall(configured bool, levels []HealthCheckLevel) string {
	statusOf := func(level int) string {
		for _, l := range levels {
			if l.Level == level {
				return l.Status
			}
		}
		return CheckSkip
	}
	if statusOf(1) == CheckFail {
		return OverallUnhealthy
	}
	if !configured {
		return OverallNotConfigured
	}
	if statusOf(2) == CheckFail {
		return OverallUnhealthy
	}
	if statusOf(3) == CheckFail || statusOf(4) == CheckFail {
		return OverallDegraded
	}
	return OverallHealthy
}

// ── L1 公共连通 ──

func (hc *HealthChecker) checkL1Public(ctx context.Context, target HealthCheckTarget, symbol string, secrets []string) HealthCheckLevel {
	started := time.Now()
	var items []HealthCheckItem

	if p, ok := target.(healthPinger); ok {
		items = append(items, hc.runItem(ctx, "ping", secrets, func() (string, error) {
			if err := p.Ping(); err != nil {
				return "", err
			}
			return "服务器可达", nil
		}))
	} else {
		items = append(items, skipItem("ping", "适配器未实现 Ping"))
	}

	items = append(items, hc.runItem(ctx, "ticker", secrets, func() (string, error) {
		tick, err := target.GetTicker(symbol)
		if err != nil {
			return "", err
		}
		if len(tick) == 0 {
			return "", fmt.Errorf("ticker 响应为空")
		}
		return fmt.Sprintf("symbol=%s", symbol), nil
	}))

	items = append(items, hc.runItem(ctx, "ohlcv", secrets, func() (string, error) {
		klines, err := target.GetKlines(symbol, "1h", 5)
		if err != nil {
			return "", err
		}
		if len(klines) == 0 {
			return "", fmt.Errorf("K线响应为空")
		}
		return fmt.Sprintf("symbol=%s bars=%d", symbol, len(klines)), nil
	}))

	if ob, ok := target.(orderBookFetcher); ok {
		items = append(items, hc.runItem(ctx, "orderbook", secrets, func() (string, error) {
			book, err := ob.GetOrderBook(symbol, 5)
			if err != nil {
				return "", err
			}
			if len(book) == 0 {
				return "", fmt.Errorf("orderbook 响应为空")
			}
			return fmt.Sprintf("symbol=%s", symbol), nil
		}))
	} else {
		items = append(items, skipItem("orderbook", "适配器未实现 REST orderbook"))
	}

	return finishLevel(1, "公共连通", started, items)
}

// ── L2 凭证有效 ──

func (hc *HealthChecker) checkL2Credentials(ctx context.Context, target HealthCheckTarget, symbol string, configured bool, secrets []string) HealthCheckLevel {
	started := time.Now()
	if !configured {
		return finishLevel(2, "凭证有效", started, []HealthCheckItem{
			skipItem("balance", "未配置 API 凭证"),
			skipItem("positions", "未配置 API 凭证"),
			skipItem("open_orders", "未配置 API 凭证"),
		})
	}
	var items []HealthCheckItem

	items = append(items, hc.runItem(ctx, "balance", secrets, func() (string, error) {
		balances, err := target.GetBalance()
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("资产条目=%d", len(balances)), nil
	}))

	items = append(items, hc.runItem(ctx, "positions", secrets, func() (string, error) {
		positions, err := target.GetPositions()
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("持仓数=%d", len(positions)), nil
	}))

	items = append(items, hc.runItem(ctx, "open_orders", secrets, func() (string, error) {
		orders, err := target.GetOpenOrders(symbol)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("未成交订单=%d", len(orders)), nil
	}))

	return finishLevel(2, "凭证有效", started, items)
}

// ── L3 交易能力元数据（只读探测，禁止真实下单）──

func (hc *HealthChecker) checkL3TradingMeta(ctx context.Context, target HealthCheckTarget, symbol string, configured bool, secrets []string) HealthCheckLevel {
	started := time.Now()
	if !configured {
		return finishLevel(3, "交易能力", started, []HealthCheckItem{
			skipItem("trade_fee", "未配置 API 凭证"),
			skipItem("symbol_precision", "未配置 API 凭证"),
			skipItem("leverage_brackets", "未配置 API 凭证"),
			skipItem("futures_permission", "未配置 API 凭证"),
		})
	}
	var items []HealthCheckItem

	if f, ok := target.(tradeFeeFetcher); ok {
		items = append(items, hc.runItem(ctx, "trade_fee", secrets, func() (string, error) {
			fee, err := f.GetTradeFee(symbol)
			if err != nil {
				return "", err
			}
			if len(fee) == 0 {
				return "", fmt.Errorf("手续费响应为空")
			}
			return fmt.Sprintf("symbol=%s", symbol), nil
		}))
	} else {
		items = append(items, skipItem("trade_fee", "适配器未实现手续费查询"))
	}

	if e, ok := target.(exchangeInfoFetcher); ok {
		items = append(items, hc.runItem(ctx, "symbol_precision", secrets, func() (string, error) {
			info, err := e.GetExchangeInfo(symbol)
			if err != nil {
				return "", err
			}
			if len(info) == 0 {
				return "", fmt.Errorf("交易对元数据响应为空")
			}
			return fmt.Sprintf("symbol=%s", symbol), nil
		}))
	} else {
		items = append(items, skipItem("symbol_precision", "适配器未实现交易对元数据查询"))
	}

	if l, ok := target.(leverageBracketFetcher); ok {
		items = append(items, hc.runItem(ctx, "leverage_brackets", secrets, func() (string, error) {
			brackets, err := l.GetLeverageBrackets(symbol)
			if err != nil {
				return "", err
			}
			if len(brackets) == 0 {
				return "", fmt.Errorf("杠杆档位响应为空")
			}
			return fmt.Sprintf("symbol=%s", symbol), nil
		}))
	} else {
		items = append(items, skipItem("leverage_brackets", "适配器未实现杠杆档位查询"))
	}

	if f, ok := target.(futuresAccountFetcher); ok {
		items = append(items, hc.runItem(ctx, "futures_permission", secrets, func() (string, error) {
			acct, err := f.GetFuturesAccount()
			if err != nil {
				return "", err
			}
			if len(acct) == 0 {
				return "", fmt.Errorf("合约账户响应为空")
			}
			return "合约账户可查询（具备合约权限）", nil
		}))
	} else {
		items = append(items, skipItem("futures_permission", "适配器未实现合约账户查询"))
	}

	return finishLevel(3, "交易能力", started, items)
}

// ── L4 WebSocket ──

func (hc *HealthChecker) checkL4WebSocket(ctx context.Context, target HealthCheckTarget, symbol string, secrets []string) HealthCheckLevel {
	started := time.Now()
	streamer, ok := target.(marketStreamer)
	if !ok {
		return finishLevel(4, "WebSocket", started, []HealthCheckItem{
			skipItem("ws_handshake", "适配器未实现 StartMarketStream"),
		})
	}

	items := []HealthCheckItem{hc.runItem(ctx, "ws_handshake", secrets, func() (string, error) {
		// 首条消息信号：适配器支持 OnTicker 回调时挂探针。
		var firstMsg chan struct{}
		if sub, ok := target.(tickerSubscriber); ok {
			firstMsg = make(chan struct{}, 1)
			ch := firstMsg
			sub.OnTicker(func(model.Tick) {
				select {
				case ch <- struct{}{}:
				default:
				}
			})
		}

		if err := streamer.StartMarketStream([]string{symbol}); err != nil {
			return "", fmt.Errorf("WS 握手失败: %w", err)
		}
		if firstMsg == nil {
			return "握手成功（适配器无行情回调，跳过首条消息验证）", nil
		}

		select {
		case <-firstMsg:
			return "握手成功，已收到首条行情消息", nil
		case <-time.After(hc.wsMessageTimeout()):
			return "", fmt.Errorf("首条消息等待超时（>%s）", hc.wsMessageTimeout())
		case <-ctx.Done():
			return "", fmt.Errorf("体检任务被取消")
		}
	})}

	return finishLevel(4, "WebSocket", started, items)
}
