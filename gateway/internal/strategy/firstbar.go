package strategy

import (
	"log"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// firstBarLogged tracks strategies that have already logged their first bar.
var firstBarLogged sync.Map

// LogFirstBar logs exactly once per strategy name the receipt of its first
// bar, providing end-to-end observability for kline delivery
// (feeder → bus → engine → strategy).
func LogFirstBar(name string, bar model.Bar) {
	if _, logged := firstBarLogged.LoadOrStore(name, true); !logged {
		log.Printf("[StrategyEngine] %s received first bar (%s %s close=%.2f)",
			name, bar.Symbol, bar.Interval, bar.Close)
	}
}
