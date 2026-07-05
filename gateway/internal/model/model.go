// Package model defines shared domain models used across the Go gateway.
// Types are organized by domain in separate files; this file re-exports them
// for backward compatibility with existing imports.
package model

// Re-exports are intentionally empty: Go packages expose all exported symbols
// from all files automatically. This file exists only to document the package
// boundary and preserve the import path `github.com/xiaotian-quant/gateway/internal/model`.
//
// Domain files:
//   - market.go    : Tick, OrderBookData, Bar, TradeData
//   - orders.go    : OrderSide, PositionSide, MarketType, MarginMode, OrderType,
//                    OrderStatus, OrderData, StatusTransitions
//   - portfolio.go : Balance, PositionData, AccountData, PortfolioSnapshot
//   - strategy.go  : Signal
//   - risk.go      : RiskAlert
