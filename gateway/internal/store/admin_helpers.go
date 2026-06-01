package store

import (
	"fmt"
	"time"
)

// ── Order Counts ──

func CountOrders() int {
	db := GetDB()
	if db == nil {
		return 0
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM xt_orders").Scan(&count)
	return count
}

func CountPendingOrders() int {
	db := GetDB()
	if db == nil {
		return 0
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM xt_orders WHERE status IN ('NEW','OPEN','PARTIALLY_FILLED','PENDING')").Scan(&count)
	return count
}

// ── Trade Counts ──

func CountTrades() int {
	db := GetDB()
	if db == nil {
		return 0
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM trades").Scan(&count)
	return count
}

func GetRecentTrades(limit int) []map[string]any {
	db := GetDB()
	if db == nil {
		return nil
	}
	rows, err := db.Query("SELECT symbol, side, price, quantity, time FROM trades ORDER BY time DESC LIMIT ?", limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []map[string]any
	for rows.Next() {
		var symbol, side string
		var price, qty float64
		var t int64
		rows.Scan(&symbol, &side, &price, &qty, &t)
		result = append(result, map[string]any{
			"symbol": symbol, "side": side, "price": price, "quantity": qty, "time": t,
		})
	}
	return result
}

// ── Strategy Counts ──

func CountStrategies() int {
	db := GetDB()
	if db == nil {
		return 0
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM strategy_configs").Scan(&count)
	return count
}

func CountActiveStrategies() int {
	db := GetDB()
	if db == nil {
		return 0
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM strategy_configs WHERE is_running = 1").Scan(&count)
	return count
}

// ── Risk Events ──

func GetRecentRiskEvents(limit int) []map[string]any {
	db := GetDB()
	if db == nil {
		return nil
	}
	rows, err := db.Query("SELECT level, message, timestamp FROM risk_events ORDER BY timestamp DESC LIMIT ?", limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []map[string]any
	for rows.Next() {
		var level, message string
		var ts int64
		rows.Scan(&level, &message, &ts)
		result = append(result, map[string]any{
			"level": level, "message": message, "timestamp": ts,
		})
	}
	return result
}

// ── Audit Log ──

func AddAuditLog(actor, action, detail string) {
	db := GetDB()
	if db == nil {
		return
	}
	db.Exec(
		`INSERT INTO agent_audit_log (actor, action, detail, created_at) VALUES (?, ?, ?, ?)`,
		actor, action, detail, time.Now().Unix(),
	)
}

func GetAuditLog(limit, offset int) []map[string]any {
	db := GetDB()
	if db == nil {
		return nil
	}
	rows, err := db.Query(
		`SELECT id, actor, action, detail, created_at FROM agent_audit_log ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []map[string]any
	for rows.Next() {
		var id int
		var actor, action, detail string
		var createdAt int64
		rows.Scan(&id, &actor, &action, &detail, &createdAt)
		result = append(result, map[string]any{
			"id": id, "actor": actor, "action": action, "detail": detail, "created_at": createdAt,
		})
	}
	return result
}

func CountAuditLog() int {
	db := GetDB()
	if db == nil {
		return 0
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM agent_audit_log").Scan(&count)
	return count
}

// ── User Management ──

func SetUserActive(userID string, active bool) error {
	db := GetDB()
	if db == nil {
		return fmt.Errorf("database not available")
	}
	isActive := 0
	if active {
		isActive = 1
	}
	_, err := db.Exec("UPDATE xt_users SET is_active = ? WHERE id = ?", isActive, userID)
	return err
}
