package alerts

import (
	"fmt"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/service"
)

// MarketKlineSource 生产用 K 线源：复用 MarketDataService（Binance REST + 30s
// 进程内缓存），同 symbol+interval 的多个告警任务在一轮扫描里自然共享一次拉取。
type MarketKlineSource struct{}

// RecentBars 拉取近期 limit 根 K 线并转为 model.Bar（时间升序，最后一根最新）。
func (MarketKlineSource) RecentBars(symbol, interval string, limit int) ([]model.Bar, error) {
	symbol = normalizeSymbol(symbol)
	rows, err := service.GetMarketService().FetchKlines(symbol, interval, limit)
	if err != nil {
		return nil, err
	}
	bars := make([]model.Bar, 0, len(rows))
	for _, r := range rows {
		bar := model.Bar{
			Symbol:   symbol,
			Interval: interval,
			Open:     klineFloat(r["open"]),
			High:     klineFloat(r["high"]),
			Low:      klineFloat(r["low"]),
			Close:    klineFloat(r["close"]),
			Volume:   klineFloat(r["volume"]),
			Time:     int64(klineFloat(r["time"])),
		}
		bars = append(bars, bar)
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("empty klines for %s %s", symbol, interval)
	}
	return bars, nil
}

// normalizeSymbol 规范交易对到 Binance 形式（大写、去分隔符）。
func normalizeSymbol(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

// klineFloat 从 MarketDataService 的 map 结构里安全取 float64。
func klineFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}
