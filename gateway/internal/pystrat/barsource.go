package pystrat

import (
	"fmt"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// EventBarSource 生产用 BarSource：KlineFeeder（引用计数轮询 symbol+interval）
// 把闭合 K 线发布到事件总线，这里按 symbol+interval 过滤后转成 channel。
//
// 暖机语义与 handler.ensureKlineFeed 对齐：供给管新起时会经总线回补最近
// 最多 100 根历史 K 线（总线直达订阅，无需另行拉取）；供给管已在跑时
// （started=false）总线不会重放，由 RecentBars 直接拉近期闭合 K 线补齐。
type EventBarSource struct {
	Bus     *event.EventBus
	Ensure  func(symbol, interval string) (started bool, err error) // KlineFeeder.EnsureSymbol
	Release func(symbol, interval string)                           // KlineFeeder.ReleaseSymbol
	// RecentBars 在供给管已运行（无回补）时提供历史闭合 K 线暖机；nil 则跳过暖机。
	RecentBars func(symbol, interval string, limit int) ([]model.Bar, error)
}

// compile-time check: EventBarSource 实现 BarSource。
var _ BarSource = (*EventBarSource)(nil)

// Subscribe 见 BarSource 接口。
func (s *EventBarSource) Subscribe(symbol, interval string) (<-chan model.Bar, func(), error) {
	if s.Bus == nil || s.Ensure == nil || s.Release == nil {
		return nil, nil, fmt.Errorf("pystrat: EventBarSource not fully configured")
	}
	symbol = normalizeSymbol(symbol)
	interval = strings.ToLower(strings.TrimSpace(interval))
	if interval == "" {
		interval = "15m"
	}

	started, err := s.Ensure(symbol, interval)
	if err != nil {
		return nil, nil, fmt.Errorf("pystrat: ensure kline feed %s %s: %w", symbol, interval, err)
	}

	ch := make(chan model.Bar, 256)
	subID := s.Bus.Subscribe(symbol, event.PrioNormal, func(evt event.Event) {
		bar, ok := evt.Data.(model.Bar)
		if !ok {
			return
		}
		// 同 symbol 可能有多个 interval 的供给管，必须按 interval 过滤。
		if strings.ToLower(strings.TrimSpace(bar.Interval)) != interval {
			return
		}
		select {
		case ch <- bar:
		default: // 策略处理慢于供给时丢最旧（ring channel 满则丢新），不阻塞总线 worker
		}
	}, event.TypeBar)

	if !started && s.RecentBars != nil {
		go func() {
			bars, err := s.RecentBars(symbol, interval, 100)
			if err != nil {
				return
			}
			for _, b := range bars {
				select {
				case ch <- b:
				default:
				}
			}
		}()
	}

	unsubscribe := func() {
		s.Bus.Unsubscribe(subID)
		s.Release(symbol, interval)
	}
	return ch, unsubscribe, nil
}
