package analysis

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// FetchBinanceKlines 拉取 Binance 公共 K 线（已闭合 bar，时间升序）。
// 分析页与 smart_money 策略的取数口（与 KlineFeeder 同数据源，但按需全量拉）。
func FetchBinanceKlines(symbol, interval string, limit int) ([]model.Bar, error) {
	if limit < 2 {
		limit = 300
	}
	if limit > 1000 {
		limit = 1000
	}
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", interval)
	params.Set("limit", strconv.Itoa(limit))
	u, _ := url.Parse("https://api.binance.com/api/v3/klines")
	u.RawQuery = params.Encode()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance klines %d: %s", resp.StatusCode, truncate(string(body), 160))
	}
	var raw [][]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if len(raw) < 2 {
		return nil, fmt.Errorf("binance klines: only %d rows", len(raw))
	}
	// 最后一根仍在形成，剔除。
	bars := make([]model.Bar, 0, len(raw)-1)
	for _, k := range raw[:len(raw)-1] {
		num := func(j int) float64 {
			switch v := k[j].(type) {
			case string:
				f, _ := strconv.ParseFloat(v, 64)
				return f
			case float64:
				return v
			}
			return 0
		}
		ot, _ := k[0].(float64)
		bars = append(bars, model.Bar{
			Symbol:   symbol,
			Interval: interval,
			Time:     int64(ot),
			Open:     num(1), High: num(2), Low: num(3), Close: num(4), Volume: num(5),
		})
	}
	return bars, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
