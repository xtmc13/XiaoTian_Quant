package pairlist

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// 本文件实现对标 freqtrade 的 pairlist 生成器/过滤器：
//   - MarketCapPairList    按市值排名生成名单（数据源可配置，缺失时明确降级错误）
//   - PercentChangePairList 按 N 周期涨跌幅排序选币（K线回溯或 ticker 24h 模式）
//   - RemotePairList         从可配置 URL 拉取远端名单（超时 + 本地缓存兜底）
//
// 与现有 13+ 种过滤器同一套 IProducer / IFilter 接口，可直接加入 Manager 链。

// ── MarketCapPairList ──────────────────────────────────────────

// MarketCapSource 返回全市场（或某 quote asset）的市值数据。
// 数据源可配置：交易所聚合、CoinGecko 网关、或测试桩。
type MarketCapSource func() ([]*PairInfo, error)

// MarketCapPairList 按市值排名生成交易对名单（对标 freqtrade MarketCapPairList）。
// 先按市值降序排名，取排名 <= MaxRank 的币种，再截取 NumberAssets 个。
// 未配置数据源时返回明确的降级错误，而不是静默返回空名单。
type MarketCapPairList struct {
	NumberAssets  int
	MaxRank       int
	RefreshPeriod time.Duration
	Source        MarketCapSource
	SourceName    string // 仅用于错误信息与日志

	mu       sync.Mutex
	cached   []string
	cachedAt time.Time
}

// NewMarketCapPairList 创建市值名单生成器。source 传 nil 时 Generate 返回降级错误。
func NewMarketCapPairList(numberAssets, maxRank int, refreshPeriod time.Duration, source MarketCapSource) *MarketCapPairList {
	if numberAssets <= 0 {
		numberAssets = 30
	}
	if maxRank <= 0 {
		maxRank = 30
	}
	if refreshPeriod <= 0 {
		refreshPeriod = 24 * time.Hour
	}
	return &MarketCapPairList{
		NumberAssets:  numberAssets,
		MaxRank:       maxRank,
		RefreshPeriod: refreshPeriod,
		Source:        source,
	}
}

func (p *MarketCapPairList) Name() string { return "MarketCapPairList" }

// Generate 生成按市值排名筛选后的名单。
func (p *MarketCapPairList) Generate(exchange, quoteAsset string) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.cached) > 0 && time.Since(p.cachedAt) < p.RefreshPeriod {
		result := make([]string, len(p.cached))
		copy(result, p.cached)
		return result, nil
	}

	if p.Source == nil {
		return nil, fmt.Errorf(
			"MarketCapPairList: 未配置市值数据源（通过 params.source/SetSource 注入，" +
				"交易所本身不提供市值数据，需外部数据源如 CoinGecko），已降级拒绝生成名单")
	}

	infos, err := p.Source()
	if err != nil {
		return nil, fmt.Errorf("MarketCapPairList: 市值数据源 %q 拉取失败: %w", p.SourceName, err)
	}

	// 过滤 quote asset 与交易状态，剔除无市值数据（市值为 0 视为缺失）
	eligible := make([]*PairInfo, 0, len(infos))
	for _, info := range infos {
		if info == nil || info.MarketCap <= 0 {
			continue
		}
		if quoteAsset != "" && info.QuoteAsset != "" && info.QuoteAsset != quoteAsset {
			continue
		}
		if info.Status != "" && info.Status != "TRADING" {
			continue
		}
		eligible = append(eligible, info)
	}

	// 按市值降序排名
	sort.SliceStable(eligible, func(i, j int) bool {
		return eligible[i].MarketCap > eligible[j].MarketCap
	})

	// 先按 max_rank 截断，再按 number_assets 截取
	n := p.MaxRank
	if n > len(eligible) {
		n = len(eligible)
	}
	if p.NumberAssets < n {
		n = p.NumberAssets
	}

	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = eligible[i].Symbol
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("MarketCapPairList: 数据源返回 %d 条记录，但无满足条件的交易对（quote=%s）", len(infos), quoteAsset)
	}

	p.cached = result
	p.cachedAt = time.Now()

	out := make([]string, len(result))
	copy(out, result)
	return out, nil
}

// ── PercentChangePairList ──────────────────────────────────────

// Candle 是一根 K 线的最小视图（涨跌幅计算只需要收盘价）。
type Candle struct {
	Time  int64   `json:"time"` // unix 毫秒
	Close float64 `json:"close"`
}

// CandleProvider 按交易对拉取最近 limit 根 K 线（升序）。
type CandleProvider func(symbol, timeframe string, limit int) ([]Candle, error)

// PercentChangePairList 按 N 周期涨跌幅排序选币（对标 freqtrade PercentChangePairList）。
//
// 两种模式：
//   - LookbackPeriod > 0：用 CandleProvider 拉取 K 线，涨跌幅 =
//     (最新收盘价 - lookback 根前收盘价) / lookback 根前收盘价 * 100
//   - LookbackPeriod == 0（ticker 模式）：使用 Universe 提供的 24h PriceChange
//
// 必需数据源缺失时返回明确降级错误。
type PercentChangePairList struct {
	NumberAssets      int
	MinValue          *float64 // 可选：涨跌幅下限（%）
	MaxValue          *float64 // 可选：涨跌幅上限（%）
	SortDirection     string   // "asc" | "desc"（默认 desc）
	LookbackPeriod    int      // 回溯 K 线数；0 = ticker 24h 模式
	LookbackTimeframe string   // K 线周期（默认 1h）
	RefreshPeriod     time.Duration

	Universe func(exchange, quoteAsset string) ([]*PairInfo, error) // 候选池
	Candles  CandleProvider // LookbackPeriod > 0 时必需

	mu       sync.Mutex
	cached   []string
	cachedAt time.Time
}

// NewPercentChangePairList 创建涨跌幅名单生成器。
func NewPercentChangePairList(numberAssets int, sortDirection string, lookbackPeriod int, lookbackTimeframe string) *PercentChangePairList {
	if numberAssets <= 0 {
		numberAssets = 30
	}
	if sortDirection != "asc" && sortDirection != "desc" {
		sortDirection = "desc"
	}
	if lookbackTimeframe == "" {
		lookbackTimeframe = "1h"
	}
	return &PercentChangePairList{
		NumberAssets:      numberAssets,
		SortDirection:     sortDirection,
		LookbackPeriod:    lookbackPeriod,
		LookbackTimeframe: lookbackTimeframe,
		RefreshPeriod:     30 * time.Minute,
	}
}

func (p *PercentChangePairList) Name() string { return "PercentChangePairList" }

type symbolChange struct {
	symbol string
	pct    float64
}

// Generate 生成按涨跌幅排序的名单。
func (p *PercentChangePairList) Generate(exchange, quoteAsset string) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.cached) > 0 && time.Since(p.cachedAt) < p.RefreshPeriod {
		result := make([]string, len(p.cached))
		copy(result, p.cached)
		return result, nil
	}

	if p.Universe == nil {
		return nil, fmt.Errorf(
			"PercentChangePairList: 未配置候选池数据源（params.universe/SetUniverse），已降级拒绝生成名单")
	}
	if p.LookbackPeriod > 0 && p.Candles == nil {
		return nil, fmt.Errorf(
			"PercentChangePairList: lookback_period=%d 需要 K 线数据源（params.candles/SetCandles），"+
				"或将 lookback_period 置 0 使用 ticker 24h 模式，已降级拒绝生成名单", p.LookbackPeriod)
	}

	infos, err := p.Universe(exchange, quoteAsset)
	if err != nil {
		return nil, fmt.Errorf("PercentChangePairList: 候选池拉取失败: %w", err)
	}

	changes := make([]symbolChange, 0, len(infos))
	if p.LookbackPeriod > 0 {
		for _, info := range infos {
			if info == nil || info.Symbol == "" {
				continue
			}
			if info.Status != "" && info.Status != "TRADING" {
				continue
			}
			pct := 0.0
			candles, err := p.Candles(info.Symbol, p.LookbackTimeframe, p.LookbackPeriod+1)
			if err == nil && len(candles) > p.LookbackPeriod {
				prev := candles[len(candles)-1-p.LookbackPeriod].Close
				cur := candles[len(candles)-1].Close
				if prev > 0 {
					pct = (cur - prev) / prev * 100
				}
			}
			// K 线不足按 freqtrade 语义记 0
			changes = append(changes, symbolChange{symbol: info.Symbol, pct: pct})
		}
	} else {
		for _, info := range infos {
			if info == nil || info.Symbol == "" {
				continue
			}
			if info.Status != "" && info.Status != "TRADING" {
				continue
			}
			changes = append(changes, symbolChange{symbol: info.Symbol, pct: info.PriceChange})
		}
	}

	// min/max 过滤
	if p.MinValue != nil || p.MaxValue != nil {
		filtered := changes[:0]
		for _, c := range changes {
			if p.MinValue != nil && c.pct <= *p.MinValue {
				continue
			}
			if p.MaxValue != nil && c.pct >= *p.MaxValue {
				continue
			}
			filtered = append(filtered, c)
		}
		changes = filtered
	}

	// 排序
	sort.SliceStable(changes, func(i, j int) bool {
		if p.SortDirection == "asc" {
			return changes[i].pct < changes[j].pct
		}
		return changes[i].pct > changes[j].pct
	})

	n := p.NumberAssets
	if n > len(changes) {
		n = len(changes)
	}
	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = changes[i].symbol
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("PercentChangePairList: 候选池 %d 个交易对经 min/max 过滤后为空", len(infos))
	}

	p.cached = result
	p.cachedAt = time.Now()

	out := make([]string, len(result))
	copy(out, result)
	return out, nil
}

// ── RemotePairList ─────────────────────────────────────────────

// RemotePairList 从可配置 URL 拉取远端名单（对标 freqtrade RemotePairList）。
// 同时实现 IProducer 与 IFilter：
//   - 作为 producer：Generate 直接返回远端名单（whitelist 模式）
//   - 作为 filter：whitelist+filter=取交集，whitelist+append=并集追加，blacklist=剔除
//
// 远端返回 JSON：{"pairs": ["BTC/USDT", ...], "refresh_period": 1800}。
// 拉取失败时若 KeepOnFailure 且有历史缓存，用本地缓存兜底；否则返回明确错误。
type RemotePairList struct {
	URL            string // http(s):// 或 file:// 路径
	Mode           string // "whitelist"（默认）| "blacklist"
	ProcessingMode string // "filter"（默认）| "append"
	NumberAssets   int    // 0 = 不限制
	RefreshPeriod  time.Duration
	ReadTimeout    time.Duration
	BearerToken    string
	KeepOnFailure  bool
	HTTPClient     *http.Client // 可注入（测试用），nil 时按 ReadTimeout 构造

	mu       sync.Mutex
	cached   []string
	cachedAt time.Time
	lastGood []string
}

// NewRemotePairList 创建远端名单。url 必填。
func NewRemotePairList(url string) (*RemotePairList, error) {
	if url == "" {
		return nil, fmt.Errorf("RemotePairList: pairlist_url 不能为空")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "file://") {
		return nil, fmt.Errorf("RemotePairList: pairlist_url 仅支持 http(s):// 或 file://，got %q", url)
	}
	return &RemotePairList{
		URL:            url,
		Mode:           "whitelist",
		ProcessingMode: "filter",
		RefreshPeriod:  30 * time.Minute,
		ReadTimeout:    10 * time.Second,
		KeepOnFailure:  true,
	}, nil
}

func (p *RemotePairList) Name() string { return "RemotePairList" }

type remotePairlistPayload struct {
	Pairs         []string `json:"pairs"`
	RefreshPeriod int      `json:"refresh_period"` // 秒，可选：远端要求的最小刷新间隔
}

// fetch 从远端（或本地文件）拉取名单。带超时与 refresh_period 协商。
func (p *RemotePairList) fetch() ([]string, error) {
	var body []byte
	if strings.HasPrefix(p.URL, "file://") {
		path := strings.TrimPrefix(p.URL, "file://")
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取本地名单文件失败: %w", err)
		}
		body = data
	} else {
		client := p.HTTPClient
		if client == nil {
			client = &http.Client{Timeout: p.ReadTimeout}
		}
		req, err := http.NewRequest(http.MethodGet, p.URL, nil)
		if err != nil {
			return nil, fmt.Errorf("构造请求失败: %w", err)
		}
		req.Header.Set("User-Agent", "XiaoTian-Quant RemotePairList")
		if p.BearerToken != "" {
			req.Header.Set("Authorization", "Bearer "+p.BearerToken)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("拉取远端名单失败（超时 %s）: %w", p.ReadTimeout, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("远端名单返回 HTTP %d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return nil, fmt.Errorf("读取远端名单响应失败: %w", err)
		}
		body = data
	}

	var payload remotePairlistPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析远端名单 JSON 失败（期望 {\"pairs\":[...]}）: %w", err)
	}
	// 远端可上调 refresh_period（freqtrade 语义：取两者较大值）
	if payload.RefreshPeriod > 0 && time.Duration(payload.RefreshPeriod)*time.Second > p.RefreshPeriod {
		p.RefreshPeriod = time.Duration(payload.RefreshPeriod) * time.Second
	}
	return payload.Pairs, nil
}

// remotePairs 返回远端名单（带 TTL 缓存与失败兜底）。
func (p *RemotePairList) remotePairs() ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cached != nil && time.Since(p.cachedAt) < p.RefreshPeriod {
		result := make([]string, len(p.cached))
		copy(result, p.cached)
		return result, nil
	}

	pairs, err := p.fetch()
	if err != nil {
		if p.KeepOnFailure && p.lastGood != nil {
			// 本地缓存兜底
			result := make([]string, len(p.lastGood))
			copy(result, p.lastGood)
			return result, nil
		}
		return nil, fmt.Errorf("RemotePairList: %w（无本地缓存可兜底）", err)
	}

	// 去重并保持顺序
	seen := make(map[string]bool, len(pairs))
	deduped := make([]string, 0, len(pairs))
	for _, s := range pairs {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		deduped = append(deduped, s)
	}

	p.cached = deduped
	p.cachedAt = time.Now()
	p.lastGood = deduped

	result := make([]string, len(deduped))
	copy(result, deduped)
	return result, nil
}

// Generate 作为 producer 使用：直接返回远端名单（blacklist 模式不能作为首位置 producer）。
func (p *RemotePairList) Generate(exchange, quoteAsset string) ([]string, error) {
	if p.Mode == "blacklist" {
		return nil, fmt.Errorf("RemotePairList: blacklist 模式不能作为链首 producer（freqtrade 同此约束）")
	}
	pairs, err := p.remotePairs()
	if err != nil {
		return nil, err
	}
	if p.NumberAssets > 0 && len(pairs) > p.NumberAssets {
		pairs = pairs[:p.NumberAssets]
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("RemotePairList: 远端名单为空（%s）", p.URL)
	}
	return pairs, nil
}

// Filter 作为过滤器使用：按 mode + processing_mode 合并远端名单。
func (p *RemotePairList) Filter(pairs []string, _ map[string]*PairInfo) ([]string, error) {
	remote, err := p.remotePairs()
	if err != nil {
		return nil, err
	}
	remoteSet := make(map[string]bool, len(remote))
	for _, s := range remote {
		remoteSet[s] = true
	}

	var result []string
	switch {
	case p.Mode == "blacklist":
		// 黑名单：剔除出现在远端名单中的交易对
		result = make([]string, 0, len(pairs))
		for _, s := range pairs {
			if !remoteSet[s] {
				result = append(result, s)
			}
		}
	case p.ProcessingMode == "append":
		// 白名单+追加：保留原名单，再按序追加远端新增项
		result = make([]string, 0, len(pairs)+len(remote))
		seen := make(map[string]bool, len(pairs))
		result = append(result, pairs...)
		for _, s := range pairs {
			seen[s] = true
		}
		for _, s := range remote {
			if !seen[s] {
				result = append(result, s)
				seen[s] = true
			}
		}
	default:
		// 白名单+过滤：取交集，保持原名单顺序
		result = make([]string, 0, len(pairs))
		for _, s := range pairs {
			if remoteSet[s] {
				result = append(result, s)
			}
		}
	}

	if p.NumberAssets > 0 && p.Mode == "whitelist" && len(result) > p.NumberAssets {
		result = result[:p.NumberAssets]
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("RemotePairList: 合并后名单为空（mode=%s processing=%s）", p.Mode, p.ProcessingMode)
	}
	return result, nil
}
