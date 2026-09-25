package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ── AI Gate Decision Repository ──
// 表结构见 migrations/sql/0027_ai_gate.sql。
// AI 交易决策门的审批时间线：每次入场审批（含绕过/跳过/fail-open）落一行，
// 放行后由 OMS 回写 order_id 与最终成交结果（MarkOutcome）。

type AIGateDecisionRecord struct {
	ID            string   `json:"id"`
	UserID        int64    `json:"user_id"`
	Source        string   `json:"source"` // manual | signal:<strategy> | dca:<id> | grid:<id> | lmartin:<id> | pystrat:<id>
	Symbol        string   `json:"symbol"`
	Side          string   `json:"side"`
	OrderType     string   `json:"order_type"`
	MarketType    string   `json:"market_type"`
	PositionSide  string   `json:"position_side"`
	Quantity      float64  `json:"quantity"`
	RefPrice      float64  `json:"ref_price"`
	Notional      float64  `json:"notional"`
	Decision      string   `json:"decision"` // approve|reject|abstain|bypassed_exit|skipped|fail_open
	Allowed       bool     `json:"allowed"`
	Confidence    float64  `json:"confidence"`
	Reasons       []string `json:"reasons"`
	Provider      string   `json:"provider"`
	Model         string   `json:"model"`
	LatencyMs     int64    `json:"latency_ms"`
	FailOpen      bool     `json:"fail_open"`
	DegradeReason string   `json:"degrade_reason"`
	RequestHash   string   `json:"request_hash"`
	ContextJSON   string   `json:"context_json"`
	OrderID       string   `json:"order_id"`
	Executed      bool     `json:"executed"`
	CreatedAt     int64    `json:"created_at"`
	UpdatedAt     int64    `json:"updated_at"`
}

type AIGateDecisionRepo struct{}

// NewAIGateDecisionRepo creates a repo instance.
func NewAIGateDecisionRepo() *AIGateDecisionRepo { return &AIGateDecisionRepo{} }

var (
	aiGateDecisionRepo     *AIGateDecisionRepo
	aiGateDecisionRepoOnce sync.Once
)

// GetAIGateDecisionRepo returns the global repo instance（与其他 repo 同一惰性单例模式）。
func GetAIGateDecisionRepo() *AIGateDecisionRepo {
	aiGateDecisionRepoOnce.Do(func() {
		aiGateDecisionRepo = NewAIGateDecisionRepo()
	})
	return aiGateDecisionRepo
}

const aiGateDecisionColumns = `id, user_id, source, symbol, side, order_type, market_type, position_side,
	quantity, ref_price, notional, decision, allowed, confidence, reasons_json, provider, model,
	latency_ms, fail_open, degrade_reason, request_hash, context_json, order_id, executed, created_at, updated_at`

// Create inserts a decision record. reasons 在写入前序列化。
func (r *AIGateDecisionRepo) Create(rec *AIGateDecisionRecord) error {
	if db == nil {
		return fmt.Errorf("db not initialized")
	}
	now := time.Now().UnixMilli()
	if rec.CreatedAt == 0 {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt == 0 {
		rec.UpdatedAt = rec.CreatedAt
	}
	reasonsJSON := "[]"
	if len(rec.Reasons) > 0 {
		if data, err := json.Marshal(rec.Reasons); err == nil {
			reasonsJSON = string(data)
		}
	}
	_, err := db.Exec(
		`INSERT INTO xt_ai_gate_decisions
			(`+aiGateDecisionColumns+`)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.UserID, rec.Source, rec.Symbol, rec.Side, rec.OrderType, rec.MarketType, rec.PositionSide,
		rec.Quantity, rec.RefPrice, rec.Notional, rec.Decision, boolToInt(rec.Allowed), rec.Confidence,
		reasonsJSON, rec.Provider, rec.Model, rec.LatencyMs, boolToInt(rec.FailOpen), rec.DegradeReason,
		rec.RequestHash, rec.ContextJSON, rec.OrderID, boolToInt(rec.Executed), rec.CreatedAt, rec.UpdatedAt,
	)
	return err
}

// MarkOutcome 回写放行订单的终态：order_id + 是否成交。幂等，记录不存在时静默忽略。
func (r *AIGateDecisionRepo) MarkOutcome(id, orderID string, executed bool) error {
	if db == nil {
		return fmt.Errorf("db not initialized")
	}
	_, err := db.Exec(
		`UPDATE xt_ai_gate_decisions SET order_id = ?, executed = ?, updated_at = ? WHERE id = ?`,
		orderID, boolToInt(executed), time.Now().UnixMilli(), id,
	)
	return err
}

// GetByID returns a single decision by ID（属主校验由 handler 做）；不存在返回 nil, nil。
func (r *AIGateDecisionRepo) GetByID(id string) (*AIGateDecisionRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("db not initialized")
	}
	row := db.QueryRow(`SELECT `+aiGateDecisionColumns+` FROM xt_ai_gate_decisions WHERE id = ?`, id)
	return r.scan(row)
}

// AIGateDecisionFilter 是 List/Count 的过滤条件；零值字段不参与过滤。
type AIGateDecisionFilter struct {
	UserID   int64 // >0 时按属主过滤（含历史无属主 0）
	Decision string
	Symbol   string
	Source   string
	FailOpen *bool
	SinceMs  int64
	Limit    int
	Offset   int
}

func (f AIGateDecisionFilter) where() (string, []any) {
	clause := " WHERE 1=1"
	var args []any
	if f.UserID > 0 {
		clause += " AND (user_id = ? OR user_id = 0)"
		args = append(args, f.UserID)
	}
	if f.Decision != "" {
		clause += " AND decision = ?"
		args = append(args, f.Decision)
	}
	if f.Symbol != "" {
		clause += " AND symbol = ?"
		args = append(args, f.Symbol)
	}
	if f.Source != "" {
		clause += " AND source = ?"
		args = append(args, f.Source)
	}
	if f.FailOpen != nil {
		clause += " AND fail_open = ?"
		args = append(args, boolToInt(*f.FailOpen))
	}
	if f.SinceMs > 0 {
		clause += " AND created_at >= ?"
		args = append(args, f.SinceMs)
	}
	return clause, args
}

// List 按创建时间倒序列出决策（分页）。
func (r *AIGateDecisionRepo) List(f AIGateDecisionFilter) ([]*AIGateDecisionRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("db not initialized")
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 500 {
		f.Limit = 500
	}
	where, args := f.where()
	query := `SELECT ` + aiGateDecisionColumns + ` FROM xt_ai_gate_decisions` + where +
		` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, f.Limit, f.Offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*AIGateDecisionRecord, 0, f.Limit)
	for rows.Next() {
		rec, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Count 返回过滤条件下的总条数（分页用）。
func (r *AIGateDecisionRepo) Count(f AIGateDecisionFilter) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("db not initialized")
	}
	where, args := f.where()
	var n int
	err := db.QueryRow(`SELECT COUNT(1) FROM xt_ai_gate_decisions`+where, args...).Scan(&n)
	return n, err
}

// AIGateStats 是决策门聚合统计（拦截率 / fail-open 率）。
type AIGateStats struct {
	Total        int `json:"total"`         // 全部记录
	Evaluated    int `json:"evaluated"`     // 真正经 LLM 评估的（approve+reject+abstain）
	Approved     int `json:"approved"`      // approve 且放行
	Blocked      int `json:"blocked"`       // 被门拦截（allowed=0）
	Abstained    int `json:"abstained"`     // abstain（含低置信度降级）
	FailOpen     int `json:"fail_open"`     // 故障放行次数
	BypassedExit int `json:"bypassed_exit"` // 出场/止损绕过次数
	Skipped      int `json:"skipped"`       // 配置跳过（paper_only/来源排除等）
}

// Stats 聚合过滤窗口内的决策统计。
func (r *AIGateDecisionRepo) Stats(f AIGateDecisionFilter) (*AIGateStats, error) {
	if db == nil {
		return nil, fmt.Errorf("db not initialized")
	}
	where, args := f.where()
	rows, err := db.Query(`SELECT decision, allowed, fail_open, COUNT(1) FROM xt_ai_gate_decisions`+where+` GROUP BY decision, allowed, fail_open`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := &AIGateStats{}
	for rows.Next() {
		var decision string
		var allowed, failOpen, n int
		if err := rows.Scan(&decision, &allowed, &failOpen, &n); err != nil {
			return nil, err
		}
		stats.Total += n
		switch decision {
		case "approve", "reject", "abstain":
			stats.Evaluated += n
		}
		if allowed == 0 {
			stats.Blocked += n
		}
		if failOpen == 1 {
			stats.FailOpen += n
		}
		switch decision {
		case "approve":
			stats.Approved += n
		case "abstain":
			stats.Abstained += n
		case "bypassed_exit":
			stats.BypassedExit += n
		case "skipped":
			stats.Skipped += n
		}
	}
	return stats, rows.Err()
}

// CountOrdersByClientOIDPrefix 统计 xt_orders 里 client_oid 带指定前缀（如
// "dca:42"、"pystrat:7"）且 created_at >= sinceMs 的订单数，供决策门组装
// "近期该策略表现" 上下文。db 未初始化时返回错误（调用方降级处理）。
func CountOrdersByClientOIDPrefix(prefix string, sinceMs int64) (total, filled, rejected int, err error) {
	if db == nil {
		return 0, 0, 0, fmt.Errorf("db not initialized")
	}
	rows, err := db.Query(
		`SELECT status, COUNT(1) FROM xt_orders WHERE client_oid LIKE ? AND created_at >= ? GROUP BY status`,
		prefix+"%", sinceMs,
	)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return 0, 0, 0, err
		}
		total += n
		switch status {
		case "FILLED":
			filled += n
		case "REJECTED":
			rejected += n
		}
	}
	return total, filled, rejected, rows.Err()
}

func (r *AIGateDecisionRepo) scan(s rowScanner) (*AIGateDecisionRecord, error) {
	var rec AIGateDecisionRecord
	var allowed, failOpen, executed int
	var reasonsJSON string
	err := s.Scan(&rec.ID, &rec.UserID, &rec.Source, &rec.Symbol, &rec.Side, &rec.OrderType,
		&rec.MarketType, &rec.PositionSide, &rec.Quantity, &rec.RefPrice, &rec.Notional,
		&rec.Decision, &allowed, &rec.Confidence, &reasonsJSON, &rec.Provider, &rec.Model,
		&rec.LatencyMs, &failOpen, &rec.DegradeReason, &rec.RequestHash, &rec.ContextJSON,
		&rec.OrderID, &executed, &rec.CreatedAt, &rec.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.Allowed = allowed != 0
	rec.FailOpen = failOpen != 0
	rec.Executed = executed != 0
	rec.Reasons = []string{}
	_ = json.Unmarshal([]byte(reasonsJSON), &rec.Reasons)
	return &rec, nil
}
