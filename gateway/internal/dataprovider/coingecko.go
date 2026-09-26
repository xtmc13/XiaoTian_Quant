package dataprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ══════════════════════════════════════════════════════════════
// 7. 市值：CoinGecko /coins/markets（免 key，免费档限流 10-30 req/min）
//    pairlist.MarketCapPairList 的生产数据源；复用本包统一的
//    限流（MinInterval）+ 熔断 + TTL 缓存 + 落库韧性件。
// ══════════════════════════════════════════════════════════════

// CoinGeckoSourceName 是 MarketCapPairList 装配时按名取数的源 id。
const CoinGeckoSourceName = "coingecko_markets"

type coinGeckoSource struct {
	base   string
	client *http.Client
	// minInterval 0 = 默认 6s（免费档下限 10 req/min）；测试可缩短以快速触发熔断。
	minInterval time.Duration
}

func newCoinGeckoSource(cfg Config, client *http.Client) *coinGeckoSource {
	return &coinGeckoSource{base: cfg.baseURL(CoinGeckoSourceName, "https://api.coingecko.com"), client: client}
}

func (s *coinGeckoSource) Name() string { return CoinGeckoSourceName }
func (s *coinGeckoSource) Description() string {
	return "CoinGecko 全市场市值排行（免 key，/coins/markets）"
}

// 市值是慢变量：TTL 30 分钟；免费档 10-30 req/min，MinInterval 取下限 10/min。
func (s *coinGeckoSource) TTL() time.Duration { return 30 * time.Minute }
func (s *coinGeckoSource) MinInterval() time.Duration {
	if s.minInterval > 0 {
		return s.minInterval
	}
	return 6 * time.Second
}
func (s *coinGeckoSource) RequiresKey() bool { return false }
func (s *coinGeckoSource) Configured() bool  { return true }

// CoinGeckoMarketsData 是 coingecko_markets 源的数据载荷（必须可 JSON 序列化）。
type CoinGeckoMarketsData struct {
	Coins     []CoinGeckoCoin `json:"coins"`
	UpdatedAt int64           `json:"updated_at"`
}

// CoinGeckoCoin 单个币种的市值视图（Symbol 已大写，如 "BTC"）。
type CoinGeckoCoin struct {
	Symbol            string  `json:"symbol"`
	Name              string  `json:"name"`
	MarketCapRank     int     `json:"market_cap_rank"`
	MarketCap         float64 `json:"market_cap"`
	CurrentPrice      float64 `json:"current_price"`
	TotalVolume       float64 `json:"total_volume"`
	PriceChangePct24h float64 `json:"price_change_pct_24h"`
}

// coinGeckoMarketsURL  coins/markets 查询串（独立成函数便于测试断言参数）。
func coinGeckoMarketsURL(base string, perPage int) string {
	q := url.Values{}
	q.Set("vs_currency", "usd")
	q.Set("order", "market_cap_desc")
	q.Set("per_page", strconv.Itoa(perPage))
	q.Set("page", "1")
	q.Set("sparkline", "false")
	q.Set("price_change_percentage", "24h")
	return strings.TrimRight(base, "/") + "/api/v3/coins/markets?" + q.Encode()
}

func (s *coinGeckoSource) Fetch(ctx context.Context) (any, error) {
	var raw []struct {
		Symbol            string   `json:"symbol"`
		Name              string   `json:"name"`
		CurrentPrice      *float64 `json:"current_price"`
		MarketCap         *float64 `json:"market_cap"`
		MarketCapRank     *int     `json:"market_cap_rank"`
		TotalVolume       *float64 `json:"total_volume"`
		PriceChangePct24h *float64 `json:"price_change_percentage_24h"`
	}
	// httpGetJSON 对非 200 只报状态码；429 限流单独给出可操作的错误文案。
	reqURL := coinGeckoMarketsURL(s.base, 250)
	if err := httpGetJSON(ctx, s.client, reqURL, nil, &raw); err != nil {
		if strings.Contains(err.Error(), "HTTP 429") {
			return nil, fmt.Errorf("coingecko: rate limited (HTTP 429)，免费档约 10-30 req/min，已靠 TTL 缓存+熔断 backoff")
		}
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("coingecko: empty markets payload")
	}
	coins := make([]CoinGeckoCoin, 0, len(raw))
	for _, c := range raw {
		sym := strings.ToUpper(strings.TrimSpace(c.Symbol))
		if sym == "" {
			continue
		}
		coin := CoinGeckoCoin{Symbol: sym, Name: strings.TrimSpace(c.Name)}
		if c.MarketCapRank != nil {
			coin.MarketCapRank = *c.MarketCapRank
		}
		if c.MarketCap != nil {
			coin.MarketCap = *c.MarketCap
		}
		if c.CurrentPrice != nil {
			coin.CurrentPrice = *c.CurrentPrice
		}
		if c.TotalVolume != nil {
			coin.TotalVolume = *c.TotalVolume
		}
		if c.PriceChangePct24h != nil {
			coin.PriceChangePct24h = *c.PriceChangePct24h
		}
		coins = append(coins, coin)
	}
	if len(coins) == 0 {
		return nil, fmt.Errorf("coingecko: no usable coin rows")
	}
	return &CoinGeckoMarketsData{Coins: coins, UpdatedAt: time.Now().Unix()}, nil
}
