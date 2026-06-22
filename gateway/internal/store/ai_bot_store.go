package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

// ── Catalog ──

func GetAIBotCatalog() []map[string]any {
	rows, err := db.Query(`
		SELECT id, name, description, strategy_type, market_type, risk_level,
		       fee_model, fee_percent, monthly_fee, performance_json, config_json,
		       is_builtin, is_active, created_at, updated_at
		FROM ai_bot_catalog WHERE is_active=1 ORDER BY created_at DESC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanAIBotCatalogRows(rows)
}

func GetAIBotCatalogByID(id string) map[string]any {
	row := db.QueryRow(`
		SELECT id, name, description, strategy_type, market_type, risk_level,
		       fee_model, fee_percent, monthly_fee, performance_json, config_json,
		       is_builtin, is_active, created_at, updated_at
		FROM ai_bot_catalog WHERE id=?`, id)
	return scanAIBotCatalogRow(row)
}

// ── Instances ──

func GetAIBotInstances(userID int) []map[string]any {
	var rows *sql.Rows
	var err error
	if userID > 0 {
		rows, err = db.Query(`
			SELECT id, user_id, catalog_id, name, strategy_type, symbol, market_type,
			       status, execution_mode, config_json, exchange_id,
			       unrealized_pnl, realized_pnl, total_return_pct, max_drawdown_pct,
			       sharpe_ratio, win_rate, total_trades, initial_balance, error_message,
			       created_at, updated_at, started_at, stopped_at
			FROM ai_bot_instances WHERE user_id=? ORDER BY updated_at DESC
		`, userID)
	} else {
		rows, err = db.Query(`
			SELECT id, user_id, catalog_id, name, strategy_type, symbol, market_type,
			       status, execution_mode, config_json, exchange_id,
			       unrealized_pnl, realized_pnl, total_return_pct, max_drawdown_pct,
			       sharpe_ratio, win_rate, total_trades, initial_balance, error_message,
			       created_at, updated_at, started_at, stopped_at
			FROM ai_bot_instances ORDER BY updated_at DESC
		`)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanAIBotInstanceRows(rows)
}

func GetAIBotInstanceByID(id string, userID int) map[string]any {
	row := db.QueryRow(`
		SELECT id, user_id, catalog_id, name, strategy_type, symbol, market_type,
		       status, execution_mode, config_json, exchange_id,
		       unrealized_pnl, realized_pnl, total_return_pct, max_drawdown_pct,
		       sharpe_ratio, win_rate, total_trades, initial_balance, error_message,
		       created_at, updated_at, started_at, stopped_at
		FROM ai_bot_instances WHERE id=? AND user_id=?`, id, userID)
	return scanAIBotInstanceRow(row)
}

func SaveAIBotInstance(item map[string]any) {
	id := getString(item, "id", "")
	if id == "" {
		return
	}
	configJSON := "{}"
	if c, ok := item["config_json"].(map[string]any); ok {
		b, _ := json.Marshal(c)
		configJSON = string(b)
	} else if s, ok := item["config_json"].(string); ok {
		configJSON = s
	}
	_, err := db.Exec(`
		INSERT OR REPLACE INTO ai_bot_instances
		(id, user_id, catalog_id, name, strategy_type, symbol, market_type,
		 status, execution_mode, config_json, exchange_id,
		 unrealized_pnl, realized_pnl, total_return_pct, max_drawdown_pct,
		 sharpe_ratio, win_rate, total_trades, initial_balance, error_message,
		 created_at, updated_at, started_at, stopped_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, getInt(item, "user_id", 0), getString(item, "catalog_id", ""),
		getString(item, "name", ""), getString(item, "strategy_type", ""),
		getString(item, "symbol", ""), getString(item, "market_type", "spot"),
		getString(item, "status", "stopped"), getString(item, "execution_mode", "paper"),
		configJSON, getString(item, "exchange_id", ""),
		getFloat(item, "unrealized_pnl", 0), getFloat(item, "realized_pnl", 0),
		getFloat(item, "total_return_pct", 0), getFloat(item, "max_drawdown_pct", 0),
		getFloat(item, "sharpe_ratio", 0), getFloat(item, "win_rate", 0),
		getInt(item, "total_trades", 0), getFloat(item, "initial_balance", 10000), getString(item, "error_message", ""),
		getInt64(item, "created_at", 0), getInt64(item, "updated_at", 0),
		getInt64(item, "started_at", 0), getInt64(item, "stopped_at", 0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[AIBotStore] save error: %v\n", err)
	}
}

func DeleteAIBotInstance(id string, userID int) {
	db.Exec("DELETE FROM ai_bot_instances WHERE id=? AND user_id=?", id, userID)
	db.Exec("DELETE FROM ai_bot_snapshots WHERE bot_instance_id=?", id)
}

// ── Snapshots ──

func SaveAIBotSnapshot(botInstanceID string, equity, unrealized, realized, totalReturn float64) {
	_, err := db.Exec(`
		INSERT INTO ai_bot_snapshots
		(bot_instance_id, total_equity, unrealized_pnl, realized_pnl, total_return_pct, timestamp)
		VALUES (?, ?, ?, ?, ?, ?)
	`, botInstanceID, equity, unrealized, realized, totalReturn, time.Now().Unix())
	if err != nil {
		fmt.Fprintf(os.Stderr, "[AIBotStore] snapshot error: %v\n", err)
	}
}

func GetAIBotSnapshots(botInstanceID string, limit int) []map[string]any {
	if limit <= 0 {
		limit = 30
	}
	rows, err := db.Query(`
		SELECT id, bot_instance_id, total_equity, unrealized_pnl, realized_pnl,
		       total_return_pct, timestamp
		FROM ai_bot_snapshots WHERE bot_instance_id=? ORDER BY timestamp DESC LIMIT ?
	`, botInstanceID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		var id int
		var botID string
		var totalEquity, unrealizedPnl, realizedPnl, totalReturnPct float64
		var timestamp int64
		if err := rows.Scan(&id, &botID, &totalEquity, &unrealizedPnl, &realizedPnl, &totalReturnPct, &timestamp); err != nil {
			continue
		}
		result = append(result, map[string]any{
			"id": id, "bot_instance_id": botID,
			"total_equity": totalEquity, "unrealized_pnl": unrealizedPnl,
			"realized_pnl": realizedPnl, "total_return_pct": totalReturnPct,
			"timestamp": timestamp,
		})
	}
	return result
}

func DeleteAIBotSnapshots(botInstanceID string) {
	db.Exec("DELETE FROM ai_bot_snapshots WHERE bot_instance_id=?", botInstanceID)
}

// ── Trades ──

func SaveAIBotTrade(item map[string]any) int {
	res, err := db.Exec(`
		INSERT INTO ai_bot_trades
		(bot_instance_id, symbol, side, entry_price, exit_price, quantity, pnl, pnl_pct, tp_price, sl_price, close_reason, opened_at, closed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, getString(item, "bot_instance_id", ""), getString(item, "symbol", ""), getString(item, "side", ""),
		getFloat(item, "entry_price", 0), getFloat(item, "exit_price", 0), getFloat(item, "quantity", 0),
		getFloat(item, "pnl", 0), getFloat(item, "pnl_pct", 0), getFloat(item, "tp_price", 0),
		getFloat(item, "sl_price", 0), getString(item, "close_reason", ""),
		getInt64(item, "opened_at", 0), getInt64(item, "closed_at", 0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[AIBotStore] trade save error: %v\n", err)
		return 0
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func GetAIBotTrades(botInstanceID string, limit int) []map[string]any {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`
		SELECT id, bot_instance_id, symbol, side, entry_price, exit_price, quantity,
		       pnl, pnl_pct, tp_price, sl_price, close_reason, opened_at, closed_at
		FROM ai_bot_trades WHERE bot_instance_id=? ORDER BY closed_at DESC, opened_at DESC LIMIT ?
	`, botInstanceID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		var id int
		var botID, symbol, side, closeReason string
		var entryPrice, exitPrice, qty, pnl, pnlPct, tpPrice, slPrice float64
		var openedAt, closedAt int64
		if err := rows.Scan(&id, &botID, &symbol, &side, &entryPrice, &exitPrice, &qty,
			&pnl, &pnlPct, &tpPrice, &slPrice, &closeReason, &openedAt, &closedAt); err != nil {
			continue
		}
		result = append(result, map[string]any{
			"id": id, "bot_instance_id": botID, "symbol": symbol, "side": side,
			"entry_price": entryPrice, "exit_price": exitPrice, "quantity": qty,
			"pnl": pnl, "pnl_pct": pnlPct, "tp_price": tpPrice, "sl_price": slPrice,
			"close_reason": closeReason, "opened_at": openedAt, "closed_at": closedAt,
		})
	}
	return result
}

func UpdateAIBotInstanceMetrics(id string, unrealized, realized, totalReturn, maxDrawdown, sharpe, winRate float64, totalTrades int) {
	db.Exec(`
		UPDATE ai_bot_instances
		SET unrealized_pnl=?, realized_pnl=?, total_return_pct=?, max_drawdown_pct=?,
		    sharpe_ratio=?, win_rate=?, total_trades=?, updated_at=?
		WHERE id=?
	`, unrealized, realized, totalReturn, maxDrawdown, sharpe, winRate, totalTrades, time.Now().Unix(), id)
}

// ── Subscriptions ──

func GetAIBotSubscriptions(userID int) []map[string]any {
	rows, err := db.Query(`
		SELECT id, user_id, bot_instance_id, fee_type, fee_percent, monthly_fee,
		       next_billing_at, status, created_at
		FROM ai_bot_subscriptions WHERE user_id=? ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		var id int
		var uid int
		var botID, feeType, status string
		var feePercent, monthlyFee float64
		var nextBilling, createdAt int64
		if err := rows.Scan(&id, &uid, &botID, &feeType, &feePercent, &monthlyFee, &nextBilling, &status, &createdAt); err != nil {
			continue
		}
		result = append(result, map[string]any{
			"id": id, "user_id": uid, "bot_instance_id": botID,
			"fee_type": feeType, "fee_percent": feePercent,
			"monthly_fee": monthlyFee, "next_billing_at": nextBilling,
			"status": status, "created_at": createdAt,
		})
	}
	return result
}

func GetAIBotSubscriptionByInstance(userID int, botInstanceID string) map[string]any {
	row := db.QueryRow(`
		SELECT id, user_id, bot_instance_id, fee_type, fee_percent, monthly_fee,
		       next_billing_at, status, created_at
		FROM ai_bot_subscriptions WHERE user_id=? AND bot_instance_id=? LIMIT 1
	`, userID, botInstanceID)
	var id int
	var uid int
	var botID, feeType, status string
	var feePercent, monthlyFee float64
	var nextBilling, createdAt int64
	if err := row.Scan(&id, &uid, &botID, &feeType, &feePercent, &monthlyFee, &nextBilling, &status, &createdAt); err != nil {
		return nil
	}
	return map[string]any{
		"id": id, "user_id": uid, "bot_instance_id": botID,
		"fee_type": feeType, "fee_percent": feePercent,
		"monthly_fee": monthlyFee, "next_billing_at": nextBilling,
		"status": status, "created_at": createdAt,
	}
}

func CreateAIBotSubscription(userID int, body map[string]any) int {
	res, err := db.Exec(`
		INSERT INTO ai_bot_subscriptions
		(user_id, bot_instance_id, fee_type, fee_percent, monthly_fee, next_billing_at, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, getString(body, "bot_instance_id", ""),
		getString(body, "fee_type", "profit_share"),
		getFloat(body, "fee_percent", 0), getFloat(body, "monthly_fee", 0),
		time.Now().AddDate(0, 1, 0).Unix(), "active", time.Now().Unix())
	if err != nil {
		fmt.Fprintf(os.Stderr, "[AIBotStore] subscription error: %v\n", err)
		return 0
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func CancelAIBotSubscription(id int, userID int) {
	db.Exec("UPDATE ai_bot_subscriptions SET status='cancelled' WHERE id=? AND user_id=?", id, userID)
}

// ── Seed data ──

func SeedAIBotCatalog() {
	count := 0
	if err := db.QueryRow("SELECT COUNT(*) FROM ai_bot_catalog").Scan(&count); err != nil {
		fmt.Fprintf(os.Stderr, "[AIBotStore] seed count error: %v\n", err)
		return
	}
	if count > 0 {
		return
	}
	now := time.Now().Unix()
	bots := []map[string]any{
		{
			"id": "optimus", "name": "Optimus", "description": "稳定型现货机器人，适合非剧烈波动市场，ALT/BTC 或 ALT/USDT 交易对",
			"strategy_type": "optimus", "market_type": "spot", "risk_level": "low",
			"fee_model": "profit_share", "fee_percent": 10, "monthly_fee": 0,
			"performance_json": `{"avg_monthly_profit":8.5,"win_rate":0.62,"sharpe":1.4,"max_drawdown":5.2}`,
			"config_json": `{"timeframe":"1h","first_order_amount":100,"order_count":7,"add_position_spread":3,"take_profit_ratio":1.3}`,
		},
		{
			"id": "cyberbot", "name": "CyberBot", "description": "熊市防御型现货机器人，专为下跌行情设计",
			"strategy_type": "cyberbot", "market_type": "spot", "risk_level": "medium",
			"fee_model": "profit_share", "fee_percent": 12, "monthly_fee": 0,
			"performance_json": `{"avg_monthly_profit":6.2,"win_rate":0.58,"sharpe":1.1,"max_drawdown":7.1}`,
			"config_json": `{"timeframe":"1h","first_order_amount":100,"order_count":5,"add_position_spread":4,"take_profit_ratio":1.5}`,
		},
		{
			"id": "mono-optimus", "name": "Mono Optimus", "description": "Optimus 单币对优化版，简化配置",
			"strategy_type": "mono_optimus", "market_type": "spot", "risk_level": "low",
			"fee_model": "free", "fee_percent": 0, "monthly_fee": 0,
			"performance_json": `{"avg_monthly_profit":5.8,"win_rate":0.65,"sharpe":1.5,"max_drawdown":4.1}`,
			"config_json": `{"timeframe":"1h","first_order_amount":150,"order_count":5,"add_position_spread":2.5,"take_profit_ratio":1.2}`,
		},
		{
			"id": "mono-cyberbot", "name": "Mono CyberBot", "description": "CyberBot 单币对优化版",
			"strategy_type": "mono_cyberbot", "market_type": "spot", "risk_level": "medium",
			"fee_model": "free", "fee_percent": 0, "monthly_fee": 0,
			"performance_json": `{"avg_monthly_profit":4.9,"win_rate":0.56,"sharpe":1.0,"max_drawdown":6.8}`,
			"config_json": `{"timeframe":"1h","first_order_amount":150,"order_count":5,"add_position_spread":3.5,"take_profit_ratio":1.4}`,
		},
		{
			"id": "crypto-future", "name": "Crypto Future", "description": "合约自适应机器人，支持保守/稳健/激进三种风险偏好",
			"strategy_type": "crypto_future", "market_type": "futures", "risk_level": "high",
			"fee_model": "profit_share", "fee_percent": 15, "monthly_fee": 0,
			"performance_json": `{"avg_monthly_profit":18.3,"win_rate":0.51,"sharpe":0.9,"max_drawdown":15.4}`,
			"config_json": `{"timeframe":"15m","leverage":3,"risk_profile":"moderate","tp_sl_ratio":2.0}`,
		},
		{
			"id": "ai-alpha", "name": "AI Alpha", "description": "AI/机器学习驱动的现货+合约机器人，基于 AI 分析做趋势判断",
			"strategy_type": "ai_alpha", "market_type": "spot", "risk_level": "medium",
			"fee_model": "profit_share", "fee_percent": 28, "monthly_fee": 0,
			"performance_json": `{"avg_monthly_profit":58.1,"win_rate":0.55,"sharpe":1.2,"max_drawdown":12.3}`,
			"config_json": `{"timeframe":"1h","model":"ai_alpha_v1","confidence_threshold":0.72}`,
		},
		{
			"id": "ai-alpha-futures", "name": "AI Alpha Futures", "description": "AI Alpha 合约版，基于机器学习进行多空判断",
			"strategy_type": "ai_alpha_futures", "market_type": "futures", "risk_level": "high",
			"fee_model": "profit_share", "fee_percent": 28, "monthly_fee": 0,
			"performance_json": `{"avg_monthly_profit":42.7,"win_rate":0.53,"sharpe":1.0,"max_drawdown":18.6}`,
			"config_json": `{"timeframe":"15m","leverage":2,"model":"ai_alpha_v1","confidence_threshold":0.75}`,
		},
		{
			"id": "terminator-volatility", "name": "Terminator Volatility", "description": "波动率/反转策略机器人，捕捉短期市场机会",
			"strategy_type": "terminator_volatility", "market_type": "spot", "risk_level": "high",
			"fee_model": "profit_share", "fee_percent": 18, "monthly_fee": 0,
			"performance_json": `{"avg_monthly_profit":12.8,"win_rate":0.43,"sharpe":0.8,"max_drawdown":14.2}`,
			"config_json": `{"timeframe":"5m","volatility_threshold":2.5,"holding_period":12}`,
		},
		{
			"id": "alt-volatility", "name": "ALT+ Volatility Bot", "description": "Altcoin 波动率套利机器人",
			"strategy_type": "alt_volatility", "market_type": "spot", "risk_level": "high",
			"fee_model": "profit_share", "fee_percent": 20, "monthly_fee": 0,
			"performance_json": `{"avg_monthly_profit":22.4,"win_rate":0.48,"sharpe":0.85,"max_drawdown":16.7}`,
			"config_json": `{"timeframe":"5m","altcoin_count":10,"volatility_threshold":3.0}`,
		},
		{
			"id": "trade-holder", "name": "Trade Holder", "description": "长期囤币/建仓机器人，适合长期 portfolio building",
			"strategy_type": "trade_holder", "market_type": "spot", "risk_level": "low",
			"fee_model": "monthly", "fee_percent": 0, "monthly_fee": 15,
			"performance_json": `{"avg_monthly_profit":3.5,"win_rate":0.70,"sharpe":1.6,"max_drawdown":8.0}`,
			"config_json": `{"timeframe":"1d","dca_amount":100,"dca_interval":"1w","take_profit_ratio":10.0}`,
		},
		{
			"id": "noah", "name": "Noah", "description": "高流动性币对机器人，针对 BTC/USDT、ETH/USDT 等主流交易对",
			"strategy_type": "noah", "market_type": "spot", "risk_level": "low",
			"fee_model": "monthly", "fee_percent": 0, "monthly_fee": 25,
			"performance_json": `{"avg_monthly_profit":4.2,"win_rate":0.68,"sharpe":1.7,"max_drawdown":3.5}`,
			"config_json": `{"timeframe":"30m","first_order_amount":200,"order_count":4,"take_profit_ratio":0.8}`,
		},
	}
	for _, bot := range bots {
		_, err := db.Exec(`
			INSERT INTO ai_bot_catalog
			(id, name, description, strategy_type, market_type, risk_level,
			 fee_model, fee_percent, monthly_fee, performance_json, config_json,
			 is_builtin, is_active, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, bot["id"], bot["name"], bot["description"], bot["strategy_type"], bot["market_type"], bot["risk_level"],
			bot["fee_model"], bot["fee_percent"], bot["monthly_fee"], bot["performance_json"], bot["config_json"],
			1, 1, now, now)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[AIBotStore] seed error %s: %v\n", bot["id"], err)
		}
	}
	fmt.Fprintln(os.Stderr, "[AIBotStore] seeded default catalog")
}

// SeedAIBotSignalProviders inserts default signal provider catalog rows.
func SeedAIBotSignalProviders() {
	count := 0
	if err := db.QueryRow("SELECT COUNT(*) FROM ai_bot_catalog WHERE strategy_type='signal_provider'").Scan(&count); err != nil {
		return
	}
	if count > 0 {
		return
	}
	now := time.Now().Unix()
	providers := []map[string]any{
		{"id": "kuresofa", "name": "Kuresofa", "description": "80.98% 累计收益，订阅制信号源", "risk_level": "medium", "monthly_fee": 30, "performance_json": `{"total_profit":80.98,"win_rate":0.61,"sharpe":1.3,"max_drawdown":9.2}`, "config_json": `{"exchanges":["binance","bitget","mexc","xt"]}`},
		{"id": "crypto-crescente", "name": "Crypto Crescente", "description": "稳健型信号源，累计收益 50.73%", "risk_level": "medium", "monthly_fee": 15, "performance_json": `{"total_profit":50.73,"win_rate":0.58,"sharpe":1.1,"max_drawdown":8.5}`, "config_json": `{"exchanges":["binance","bybit","kucoin","mexc","okx","xt"]}`},
		{"id": "cryptoleks", "name": "Cryptoleks", "description": "优质 altcoin 信号源", "risk_level": "high", "monthly_fee": 25, "performance_json": `{"total_profit":29.09,"win_rate":0.54,"sharpe":0.9,"max_drawdown":14.1}`, "config_json": `{"exchanges":["binance","binance_futures","bitget","bybit"]}`},
		{"id": "ai-alpha-signals", "name": "AI Alpha Signals", "description": "AI Alpha 信号频道，月均收益约 58.1%", "risk_level": "high", "fee_percent": 28, "performance_json": `{"avg_monthly_profit":58.1,"win_rate":0.55,"sharpe":1.2,"max_drawdown":12.3}`, "config_json": `{"exchanges":["binance","binance_futures"]}`},
		{"id": "jumper-stars", "name": "Jumper Stars", "description": "高风险高收益信号源", "risk_level": "high", "fee_percent": 23, "performance_json": `{"avg_monthly_profit":261.0,"win_rate":0.48,"sharpe":0.7,"max_drawdown":25.3}`, "config_json": `{"exchanges":["binance","bybit"]}`},
		{"id": "flash-signals", "name": "Flash Signals", "description": "短线快闪信号", "risk_level": "high", "fee_percent": 18, "performance_json": `{"avg_monthly_profit":2.32,"win_rate":0.52,"sharpe":0.95,"max_drawdown":6.4}`, "config_json": `{"exchanges":["binance","okx"]}`},
	}
	for _, p := range providers {
		feeModel := "monthly"
		feePercent := getFloat(p, "fee_percent", 0)
		if feePercent > 0 {
			feeModel = "profit_share"
		}
		_, err := db.Exec(`
			INSERT INTO ai_bot_catalog
			(id, name, description, strategy_type, market_type, risk_level,
			 fee_model, fee_percent, monthly_fee, performance_json, config_json,
			 is_builtin, is_active, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, p["id"], p["name"], p["description"], "signal_provider", "spot", p["risk_level"],
			feeModel, feePercent, p["monthly_fee"], p["performance_json"], p["config_json"],
			1, 1, now, now)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[AIBotStore] provider seed error %s: %v\n", p["id"], err)
		}
	}
	fmt.Fprintln(os.Stderr, "[AIBotStore] seeded default signal providers")
}

// ── Scan helpers ──

func scanAIBotCatalogRows(rows *sql.Rows) []map[string]any {
	var result []map[string]any
	for rows.Next() {
		var id, name, description, strategyType, marketType, riskLevel, feeModel string
		var feePercent, monthlyFee float64
		var performanceJSON, configJSON string
		var isBuiltin, isActive int
		var createdAt, updatedAt int64
		if err := rows.Scan(&id, &name, &description, &strategyType, &marketType, &riskLevel,
			&feeModel, &feePercent, &monthlyFee, &performanceJSON, &configJSON,
			&isBuiltin, &isActive, &createdAt, &updatedAt); err != nil {
			continue
		}
		result = append(result, map[string]any{
			"id": id, "name": name, "description": description,
			"strategy_type": strategyType, "market_type": marketType,
			"risk_level": riskLevel, "fee_model": feeModel,
			"fee_percent": feePercent, "monthly_fee": monthlyFee,
			"performance_json": performanceJSON, "config_json": configJSON,
			"is_builtin": isBuiltin, "is_active": isActive,
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
	return result
}

func scanAIBotCatalogRow(row *sql.Row) map[string]any {
	var id, name, description, strategyType, marketType, riskLevel, feeModel string
	var feePercent, monthlyFee float64
	var performanceJSON, configJSON string
	var isBuiltin, isActive int
	var createdAt, updatedAt int64
	err := row.Scan(&id, &name, &description, &strategyType, &marketType, &riskLevel,
		&feeModel, &feePercent, &monthlyFee, &performanceJSON, &configJSON,
		&isBuiltin, &isActive, &createdAt, &updatedAt)
	if err != nil {
		return nil
	}
	return map[string]any{
		"id": id, "name": name, "description": description,
		"strategy_type": strategyType, "market_type": marketType,
		"risk_level": riskLevel, "fee_model": feeModel,
		"fee_percent": feePercent, "monthly_fee": monthlyFee,
		"performance_json": performanceJSON, "config_json": configJSON,
		"is_builtin": isBuiltin, "is_active": isActive,
		"created_at": createdAt, "updated_at": updatedAt,
	}
}

func scanAIBotInstanceRows(rows *sql.Rows) []map[string]any {
	var result []map[string]any
	for rows.Next() {
		var id, catalogID, name, strategyType, symbol, marketType, status, executionMode, configJSON, exchangeID, errorMessage string
		var userID int
		var unrealizedPnl, realizedPnl, totalReturnPct, maxDrawdownPct, sharpeRatio, winRate, initialBalance float64
		var totalTrades int
		var createdAt, updatedAt, startedAt, stoppedAt int64
		if err := rows.Scan(&id, &userID, &catalogID, &name, &strategyType, &symbol, &marketType,
			&status, &executionMode, &configJSON, &exchangeID,
			&unrealizedPnl, &realizedPnl, &totalReturnPct, &maxDrawdownPct,
			&sharpeRatio, &winRate, &totalTrades, &initialBalance, &errorMessage,
			&createdAt, &updatedAt, &startedAt, &stoppedAt); err != nil {
			continue
		}
		result = append(result, map[string]any{
			"id": id, "user_id": userID, "catalog_id": catalogID,
			"name": name, "strategy_type": strategyType, "symbol": symbol,
			"market_type": marketType, "status": status,
			"execution_mode": executionMode, "config_json": configJSON,
			"exchange_id": exchangeID,
			"unrealized_pnl": unrealizedPnl, "realized_pnl": realizedPnl,
			"total_return_pct": totalReturnPct, "max_drawdown_pct": maxDrawdownPct,
			"sharpe_ratio": sharpeRatio, "win_rate": winRate,
			"total_trades": totalTrades, "initial_balance": initialBalance, "error_message": errorMessage,
			"created_at": createdAt, "updated_at": updatedAt,
			"started_at": startedAt, "stopped_at": stoppedAt,
		})
	}
	return result
}

func scanAIBotInstanceRow(row *sql.Row) map[string]any {
	var id, catalogID, name, strategyType, symbol, marketType, status, executionMode, configJSON, exchangeID, errorMessage string
	var userID int
	var unrealizedPnl, realizedPnl, totalReturnPct, maxDrawdownPct, sharpeRatio, winRate, initialBalance float64
	var totalTrades int
	var createdAt, updatedAt, startedAt, stoppedAt int64
	err := row.Scan(&id, &userID, &catalogID, &name, &strategyType, &symbol, &marketType,
		&status, &executionMode, &configJSON, &exchangeID,
		&unrealizedPnl, &realizedPnl, &totalReturnPct, &maxDrawdownPct,
		&sharpeRatio, &winRate, &totalTrades, &initialBalance, &errorMessage,
		&createdAt, &updatedAt, &startedAt, &stoppedAt)
	if err != nil {
		return nil
	}
	return map[string]any{
		"id": id, "user_id": userID, "catalog_id": catalogID,
		"name": name, "strategy_type": strategyType, "symbol": symbol,
		"market_type": marketType, "status": status,
		"execution_mode": executionMode, "config_json": configJSON,
		"exchange_id": exchangeID,
		"unrealized_pnl": unrealizedPnl, "realized_pnl": realizedPnl,
		"total_return_pct": totalReturnPct, "max_drawdown_pct": maxDrawdownPct,
		"sharpe_ratio": sharpeRatio, "win_rate": winRate,
		"total_trades": totalTrades, "initial_balance": initialBalance, "error_message": errorMessage,
		"created_at": createdAt, "updated_at": updatedAt,
		"started_at": startedAt, "stopped_at": stoppedAt,
	}
}

// getInt returns an int from a map with a default.
func getInt(m map[string]any, key string, def int) int {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case int64:
			return int(val)
		case float64:
			return int(val)
		case string:
			if i, err := strconv.Atoi(val); err == nil {
				return i
			}
		}
	}
	return def
}

func getInt64(m map[string]any, key string, def int64) int64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int64:
			return val
		case int:
			return int64(val)
		case float64:
			return int64(val)
		case string:
			if i, err := strconv.ParseInt(val, 10, 64); err == nil {
				return i
			}
		}
	}
	return def
}
