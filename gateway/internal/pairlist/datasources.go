package pairlist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/dataprovider"
	"github.com/xiaotian-quant/gateway/internal/market"
)

// 本文件是 pairlist 生成器的生产数据源接线层：
//   - MarketCapPairList.Source   ← CoinGecko（经 dataprovider 统一限流/熔断/TTL 缓存），
//     并与交易所 universe 求交（只产出本所真实挂牌的 (base,quote)，freqtrade 同此语义）
//   - PercentChangePairList.Universe / VolumePairList.InfoProvider 等 ← BinanceUniverse
//     （交易所公开 REST：/ticker/24hr 全量 + /exchangeInfo，TTL 缓存 + 失败回旧数据）
//   - PercentChangePairList.Candles ← market.KlineFeeder 的已闭合 K 线 REST 拉取
//
// 所有上游失败都返回明确错误（producer 自身再包一层降级文案），绝不静默放空名单。

// SourceDeps 是 producer/manager 的外部数据源注入集合（生产由 ProductionSourceDeps
// 构建，测试逐字段注入 fake）。
type SourceDeps struct {
	MarketCap MarketCapSource                                        // MarketCapPairList
	Universe  func(exchange, quoteAsset string) ([]*PairInfo, error) // PercentChangePairList 候选池
	Candles   CandleProvider                                         // PercentChangePairList lookback 模式
	AllPairs  func() ([]*PairInfo, error)                            // Volume/PerformancePairList.InfoProvider
	InfoMap   func(symbols []string) (map[string]*PairInfo, error)   // Manager.InfoProvider（过滤器取数）
}

// WireProducer 把外部数据源注入工厂构建的 producer（仅填补 nil 注入点，
// 测试已注入的桩不会被覆盖）。未知类型静默跳过（无需数据源）。
func WireProducer(p IProducer, deps SourceDeps) {
	switch prod := p.(type) {
	case *MarketCapPairList:
		if prod.Source == nil && deps.MarketCap != nil {
			prod.Source = deps.MarketCap
			prod.SourceName = "coingecko+exchange-universe"
		}
	case *PercentChangePairList:
		if prod.Universe == nil {
			prod.Universe = deps.Universe
		}
		if prod.Candles == nil {
			prod.Candles = deps.Candles
		}
	case *VolumePairList:
		if prod.InfoProvider == nil {
			prod.InfoProvider = deps.AllPairs
		}
	case *PerformancePairList:
		if prod.InfoProvider == nil {
			prod.InfoProvider = deps.AllPairs
		}
	}
}

// WireManager 给 Manager 接上过滤器取数通道（PairInfo 按 symbol 查询）。
func WireManager(m *Manager, deps SourceDeps) {
	if m != nil && deps.InfoMap != nil {
		m.SetInfoProvider(deps.InfoMap)
	}
}

// ProductionSourceDeps 装配生产数据源：CoinGecko 市值（dataprovider 统一韧性件）
// + Binance 公开 REST universe + KlineFeeder K 线。
func ProductionSourceDeps() SourceDeps {
	universe := NewBinanceUniverse("", nil, 0)
	// RecentClosedBars 只走 REST 拉取，不触碰 Bus，nil 总线安全。
	feeder := market.NewKlineFeeder(nil)
	return SourceDeps{
		MarketCap: CoinGeckoMarketCapSource(universe),
		Universe:  universe.Universe,
		Candles:   CandleProviderFromFeeder(feeder),
		AllPairs:  universe.AllPairs,
		InfoMap:   universe.InfoMap,
	}
}

// ── CoinGecko 市值源（dataprovider 适配）──────────────────────────

// CoinGeckoMarketCapSource 把 dataprovider 的 coingecko_markets 源适配成
// MarketCapSource。dataprovider.Default() 在闭包内惰性解析——main 的
// SetDefault 注册生产实例后才调用也不会抓到旧的回退实例。
func CoinGeckoMarketCapSource(universe *BinanceUniverse) MarketCapSource {
	return coinGeckoMarketCapSource(dataprovider.Default, universe)
}

// coinGeckoMarketCapSource 可注入 service 提供器的内部实现（测试注入 fake service）。
//
// 与交易所 universe 求交：CoinGecko 市值榜上的币未必在本所挂牌，只对
// universe 中 TRADING 的 (base,quote) 产出 PairInfo；同一 ticker 多币种
// 重名时保留市值最高者（CoinGecko 返回已按市值降序）。
func coinGeckoMarketCapSource(svcFn func() *dataprovider.Service, universe *BinanceUniverse) MarketCapSource {
	return func() ([]*PairInfo, error) {
		svc := svcFn()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		res := svc.Get(ctx, dataprovider.CoinGeckoSourceName)
		if res.Status != "ok" && res.Status != "stale" {
			return nil, fmt.Errorf("CoinGecko 市值源不可用（status=%s error=%s）", res.Status, res.Error)
		}
		// Data 新鲜时是 *CoinGeckoMarketsData，DB 回灌时是 json.RawMessage，统一 JSON 归一。
		raw, err := json.Marshal(res.Data)
		if err != nil {
			return nil, fmt.Errorf("CoinGecko 载荷序列化失败: %w", err)
		}
		var data dataprovider.CoinGeckoMarketsData
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, fmt.Errorf("CoinGecko 载荷解析失败: %w", err)
		}
		if len(data.Coins) == 0 {
			return nil, fmt.Errorf("CoinGecko 载荷为空（status=%s）", res.Status)
		}

		pairs, err := universe.pairs()
		if err != nil {
			return nil, fmt.Errorf("市值名单需与交易所挂牌求交，universe 拉取失败: %w", err)
		}
		byBase := make(map[string][]*PairInfo, len(pairs))
		for _, p := range pairs {
			if p.BaseAsset == "" || (p.Status != "" && p.Status != "TRADING") {
				continue
			}
			byBase[p.BaseAsset] = append(byBase[p.BaseAsset], p)
		}

		out := make([]*PairInfo, 0, len(data.Coins))
		seen := make(map[string]bool, len(data.Coins))
		for _, c := range data.Coins {
			for _, up := range byBase[c.Symbol] {
				if seen[up.Symbol] {
					continue // ticker 重名：保留市值最高者
				}
				seen[up.Symbol] = true
				out = append(out, &PairInfo{
					Symbol:      up.Symbol,
					BaseAsset:   c.Symbol,
					QuoteAsset:  up.QuoteAsset,
					Price:       c.CurrentPrice,
					Volume24h:   c.TotalVolume,
					MarketCap:   c.MarketCap,
					PriceChange: c.PriceChangePct24h,
					Status:      "TRADING",
					Exchange:    up.Exchange,
				})
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("CoinGecko %d 个币种与交易所 universe 求交后为空", len(data.Coins))
		}
		return out, nil
	}
}

// ── 交易所 universe（Binance 公开 REST）───────────────────────────

// BinanceUniverse 交易所候选池：GET /ticker/24hr（全量）+ /exchangeInfo 合并
// 出 PairInfo（价格/24h 量/涨跌幅/价差/波动率/精度/交易状态）。
// TTL 缓存（ticker/24hr 全量是重调用）；刷新失败且有旧数据时回旧数据并记 WARN，
// 完全没有数据时返回明确错误。
//
// 目前仅支持 binance（其他交易所 adapter 无全市场列表接口；需要的交易所应在此
// 登记对应的 universe 实现，未登记的 exchange 一律明确报错而非错用币安数据）。
type BinanceUniverse struct {
	BaseURL string        // 空 = BINANCE_REST_URL env 或主网
	Client  *http.Client  // nil = 10s 超时
	TTL     time.Duration // <=0 = 60s

	mu       sync.Mutex
	cached   []*PairInfo
	cachedAt time.Time
}

// NewBinanceUniverse 创建 universe 提供器（参数为零值时取生产默认）。
func NewBinanceUniverse(baseURL string, client *http.Client, ttl time.Duration) *BinanceUniverse {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &BinanceUniverse{BaseURL: baseURL, Client: client, TTL: ttl}
}

func (u *BinanceUniverse) baseURL() string {
	if u.BaseURL != "" {
		return strings.TrimRight(u.BaseURL, "/")
	}
	if env := os.Getenv("BINANCE_REST_URL"); env != "" {
		return strings.TrimRight(env, "/")
	}
	return "https://api.binance.com/api/v3"
}

// Universe 适配 PercentChangePairList.Universe：仅 binance 支持，其余明确报错。
func (u *BinanceUniverse) Universe(exchange, quoteAsset string) ([]*PairInfo, error) {
	if !strings.EqualFold(strings.TrimSpace(exchange), "binance") {
		return nil, fmt.Errorf("exchange universe 数据源未接线：仅支持 binance（got %q），请用 Static/RemotePairList 或先接线该所 markets 接口", exchange)
	}
	all, err := u.pairs()
	if err != nil {
		return nil, err
	}
	if quoteAsset == "" {
		return all, nil
	}
	out := make([]*PairInfo, 0, len(all))
	for _, p := range all {
		if p.QuoteAsset == quoteAsset {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("binance universe 中无 quote=%s 的交易对", quoteAsset)
	}
	return out, nil
}

// AllPairs 适配 Volume/PerformancePairList.InfoProvider（无参形式）。
func (u *BinanceUniverse) AllPairs() ([]*PairInfo, error) {
	return u.pairs()
}

// InfoMap 适配 Manager.SetInfoProvider（按 symbol 取 PairInfo）。
func (u *BinanceUniverse) InfoMap(symbols []string) (map[string]*PairInfo, error) {
	all, err := u.pairs()
	if err != nil {
		return nil, err
	}
	idx := make(map[string]*PairInfo, len(all))
	for _, p := range all {
		idx[p.Symbol] = p
	}
	out := make(map[string]*PairInfo, len(symbols))
	for _, s := range symbols {
		if p, ok := idx[s]; ok {
			out[s] = p
		}
	}
	return out, nil
}

// pairs TTL 缓存入口；刷新失败回旧缓存（无缓存才报错）。
func (u *BinanceUniverse) pairs() ([]*PairInfo, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.cached != nil && time.Since(u.cachedAt) < u.TTL {
		return u.cached, nil
	}
	fresh, err := u.fetchAll()
	if err != nil {
		if u.cached != nil {
			log.Printf("[pairlist] WARN universe 刷新失败，回退旧缓存（%d 交易对）: %v", len(u.cached), err)
			return u.cached, nil
		}
		return nil, err
	}
	u.cached = fresh
	u.cachedAt = time.Now()
	return fresh, nil
}

type binanceTickerRow struct {
	Symbol             string `json:"symbol"`
	LastPrice          string `json:"lastPrice"`
	PriceChangePercent string `json:"priceChangePercent"`
	QuoteVolume        string `json:"quoteVolume"`
	HighPrice          string `json:"highPrice"`
	LowPrice           string `json:"lowPrice"`
	BidPrice           string `json:"bidPrice"`
	AskPrice           string `json:"askPrice"`
}

// fetchAll 拉取并合并 ticker/24hr + exchangeInfo。
func (u *BinanceUniverse) fetchAll() ([]*PairInfo, error) {
	tickers, err := u.fetchTickers()
	if err != nil {
		return nil, err
	}
	meta, err := u.fetchExchangeInfo()
	if err != nil {
		return nil, err
	}

	out := make([]*PairInfo, 0, len(tickers))
	for _, t := range tickers {
		if t.Symbol == "" {
			continue
		}
		m := meta[t.Symbol]
		p := &PairInfo{
			Symbol:   t.Symbol,
			Exchange: "binance",
			Status:   "TRADING", // ticker 列表内的默认可交易；exchangeInfo 有明示状态则覆盖
		}
		p.Price = parseFloatStr(t.LastPrice)
		p.Volume24h = parseFloatStr(t.QuoteVolume)
		p.PriceChange = parseFloatStr(t.PriceChangePercent)
		if bid, ask := parseFloatStr(t.BidPrice), parseFloatStr(t.AskPrice); bid > 0 && ask > 0 {
			p.Spread = (ask - bid) / bid * 100
		}
		if hi, lo := parseFloatStr(t.HighPrice), parseFloatStr(t.LowPrice); lo > 0 && hi > 0 {
			p.Volatility = (hi - lo) / lo * 100
		}
		if m != nil {
			p.BaseAsset = m.base
			p.QuoteAsset = m.quote
			p.Status = m.status
			p.PricePrecision = m.pricePrecision
			p.QtyPrecision = m.qtyPrecision
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("binance universe: 合并后无交易对（ticker %d 条）", len(tickers))
	}
	return out, nil
}

func (u *BinanceUniverse) fetchTickers() ([]binanceTickerRow, error) {
	var rows []binanceTickerRow
	if err := u.getJSON("/ticker/24hr", &rows); err != nil {
		return nil, fmt.Errorf("binance ticker/24hr: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("binance ticker/24hr: empty payload")
	}
	return rows, nil
}

type binanceSymbolMeta struct {
	base, quote, status          string
	pricePrecision, qtyPrecision int
}

func (u *BinanceUniverse) fetchExchangeInfo() (map[string]*binanceSymbolMeta, error) {
	var raw struct {
		Symbols []struct {
			Symbol     string `json:"symbol"`
			BaseAsset  string `json:"baseAsset"`
			QuoteAsset string `json:"quoteAsset"`
			Status     string `json:"status"`
			Filters    []struct {
				FilterType string `json:"filterType"`
				TickSize   string `json:"tickSize"`
				StepSize   string `json:"stepSize"`
			} `json:"filters"`
		} `json:"symbols"`
	}
	if err := u.getJSON("/exchangeInfo", &raw); err != nil {
		return nil, fmt.Errorf("binance exchangeInfo: %w", err)
	}
	out := make(map[string]*binanceSymbolMeta, len(raw.Symbols))
	for _, s := range raw.Symbols {
		if s.Symbol == "" {
			continue
		}
		m := &binanceSymbolMeta{base: s.BaseAsset, quote: s.QuoteAsset, status: s.Status}
		for _, f := range s.Filters {
			switch f.FilterType {
			case "PRICE_FILTER":
				m.pricePrecision = decimalPlaces(f.TickSize)
			case "LOT_SIZE":
				m.qtyPrecision = decimalPlaces(f.StepSize)
			}
		}
		out[s.Symbol] = m
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("binance exchangeInfo: no symbols in payload")
	}
	return out, nil
}

func (u *BinanceUniverse) getJSON(path string, out any) error {
	reqURL := u.baseURL() + path
	resp, err := u.Client.Get(reqURL) // #nosec G107 -- URL 来自配置/env，非用户输入
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateBody(string(body), 200))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}
	return nil
}

// ── K 线源（market.KlineFeeder 适配）──────────────────────────────

// CandleProviderFromFeeder 把 KlineFeeder 的已闭合 K 线 REST 拉取适配成
// CandleProvider（升序、剔除未闭合的最新一根，与 freqtrade 用闭合 K 线一致）。
func CandleProviderFromFeeder(f *market.KlineFeeder) CandleProvider {
	return func(symbol, timeframe string, limit int) ([]Candle, error) {
		if f == nil {
			return nil, fmt.Errorf("K 线供给器未配置")
		}
		if limit <= 0 {
			limit = 2
		}
		bars, err := f.RecentClosedBars(symbol, timeframe, limit)
		if err != nil {
			return nil, err
		}
		candles := make([]Candle, 0, len(bars))
		for _, b := range bars {
			candles = append(candles, Candle{Time: b.Time, Close: b.Close})
		}
		return candles, nil
	}
}

// ── 小工具 ──

func parseFloatStr(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

// decimalPlaces 由 step/tick 串（"0.00001000"）推小数位数。
func decimalPlaces(step string) int {
	step = strings.TrimSpace(step)
	if step == "" {
		return 0
	}
	if !strings.Contains(step, ".") {
		return 0
	}
	step = strings.TrimRight(step, "0")
	return len(step) - strings.Index(step, ".") - 1
}

func truncateBody(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
