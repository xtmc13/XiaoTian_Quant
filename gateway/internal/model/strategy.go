package model

// ── Signal ──

// SignalTagManual G1 手动操控信号标记（cra ManualAction 产出）：下单链路据此
// 给订单 client_oid 打 ":manual:" 中缀——策略 OnOrderUpdate 据此把手动补仓
// 成交与自动补仓精确区分（入档不推自动阶梯），成交账本也凭该标记在重启
// 重建时恢复 Manual 档。
const SignalTagManual = "manual"

type Signal struct {
	Symbol    string  `json:"symbol"`
	Direction string  `json:"direction"` // LONG, SHORT, CLOSE
	Strength  float64 `json:"strength"`  // 0.0 - 1.0
	Strategy  string  `json:"strategy"`
	Reason    string  `json:"reason"`
	Timestamp int64   `json:"timestamp"`
	Qty       float64 `json:"qty,omitempty"`
	// Tag 信号来源标记（""=自动信号；SignalTagManual=运行时手动操控）。
	Tag string `json:"tag,omitempty"`
}
