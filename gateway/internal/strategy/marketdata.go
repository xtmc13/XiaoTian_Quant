package strategy

import (
	"strings"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 多周期 K 线访问（A7.1） ──
// MarketData 在事件总线上订阅全部 K 线（空 symbol topic = 通配），按
// symbol+timeframe 分桶保存最近 maxMarketDataBars 根，供策略在多周期
// 场景下用 GetBar/GetSeries 读取非主周期数据。全部方法线程安全。

const maxMarketDataBars = 500

// BarProvider 是引擎提供给策略的多周期数据访问接口。
type BarProvider interface {
	// GetBar 取 symbol+tf 的最新一根 K 线。
	GetBar(symbol, tf string) (model.Bar, bool)
	// GetSeries 取 symbol+tf 的最近 K 线序列（旧→新，拷贝）。
	GetSeries(symbol, tf string) []model.Bar
}

// BarProviderSetter 可选接口：引擎在策略 Start 后注入 BarProvider。
type BarProviderSetter interface {
	SetBarProvider(p BarProvider)
}

// MarketData 是 BarProvider 的总线驱动实现。
type MarketData struct {
	mu   sync.RWMutex
	data map[string][]model.Bar // key: SYMBOL|tf
	sub  event.SubscriptionID
	bus  *event.EventBus
}

func marketDataKey(symbol, tf string) string {
	return strings.ToUpper(strings.TrimSpace(symbol)) + "|" + strings.ToLower(strings.TrimSpace(tf))
}

// NewMarketData 创建并订阅总线（symbol="" 通配全部 TypeBar）。
func NewMarketData(bus *event.EventBus) *MarketData {
	m := &MarketData{data: make(map[string][]model.Bar), bus: bus}
	if bus != nil {
		m.sub = bus.Subscribe("", event.PrioLow, func(evt event.Event) {
			if evt.Type != event.TypeBar {
				return
			}
			if bar, ok := evt.Data.(model.Bar); ok {
				m.onBar(bar)
			}
		}, event.TypeBar)
	}
	return m
}

// Close 退订总线。
func (m *MarketData) Close() {
	if m.bus != nil && m.sub != 0 {
		m.bus.Unsubscribe(m.sub)
	}
}

func (m *MarketData) onBar(bar model.Bar) {
	key := marketDataKey(bar.Symbol, bar.Interval)
	m.mu.Lock()
	defer m.mu.Unlock()
	bucket := m.data[key]
	if n := len(bucket); n > 0 && bucket[n-1].Time >= bar.Time {
		// 回补/重复推送：只刷新同时间戳的最后一根
		if bucket[n-1].Time == bar.Time {
			bucket[n-1] = bar
			m.data[key] = bucket
		}
		return
	}
	bucket = append(bucket, bar)
	if len(bucket) > maxMarketDataBars {
		bucket = bucket[len(bucket)-maxMarketDataBars:]
	}
	m.data[key] = bucket
}

// GetBar 返回 symbol+tf 的最新 K 线。
func (m *MarketData) GetBar(symbol, tf string) (model.Bar, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bucket := m.data[marketDataKey(symbol, tf)]
	if len(bucket) == 0 {
		return model.Bar{}, false
	}
	return bucket[len(bucket)-1], true
}

// GetSeries 返回 symbol+tf 的最近 K 线（旧→新，拷贝，调用方只读）。
func (m *MarketData) GetSeries(symbol, tf string) []model.Bar {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bucket := m.data[marketDataKey(symbol, tf)]
	out := make([]model.Bar, len(bucket))
	copy(out, bucket)
	return out
}

// LastBar 返回该 symbol 任意周期的最新 K 线（主周期回退用）。
func (m *MarketData) LastBar(symbol string) (model.Bar, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	prefix := strings.ToUpper(strings.TrimSpace(symbol)) + "|"
	var latest model.Bar
	found := false
	for key, bucket := range m.data {
		if !strings.HasPrefix(key, prefix) || len(bucket) == 0 {
			continue
		}
		if !found || bucket[len(bucket)-1].Time > latest.Time {
			latest = bucket[len(bucket)-1]
			found = true
		}
	}
	return latest, found
}
