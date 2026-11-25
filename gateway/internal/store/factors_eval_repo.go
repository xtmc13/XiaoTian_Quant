package store

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ── 因子评价 Repository ──
// 表结构见 migrations/sql/0012_factor_research.sql。

type FactorEvaluationRecord struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"user_id"`
	Kind          string `json:"kind"` // ic | layers
	FactorName    string `json:"factor_name"`
	FactorVersion int    `json:"factor_version"`
	Category      string `json:"category"`
	Symbol        string `json:"symbol"`
	TF            string `json:"tf"`
	ForwardBars   int    `json:"forward_bars"`
	ParamsJSON    string `json:"params_json"`
	Samples       int    `json:"samples"`
	ResultJSON    string `json:"result_json"`
	CreatedAt     int64  `json:"created_at"`
}

type FactorEvaluationRepo struct{ mu sync.RWMutex }

func NewFactorEvaluationRepo() *FactorEvaluationRepo { return &FactorEvaluationRepo{} }

func (r *FactorEvaluationRepo) Create(rec *FactorEvaluationRecord) error {
	if rec.Kind == "" {
		rec.Kind = "ic"
	}
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().UnixMilli()
	}
	err := db.QueryRow(
		`INSERT INTO factors_evaluations
			(user_id, kind, factor_name, factor_version, category, symbol, tf, forward_bars, params_json, samples, result_json, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`,
		rec.UserID, rec.Kind, rec.FactorName, rec.FactorVersion, rec.Category, rec.Symbol, rec.TF,
		rec.ForwardBars, rec.ParamsJSON, rec.Samples, rec.ResultJSON, rec.CreatedAt,
	).Scan(&rec.ID)
	if err != nil {
		return err
	}
	return nil
}

const factorEvalColumns = `id, user_id, kind, factor_name, factor_version, category, symbol, tf, forward_bars, params_json, samples, result_json, created_at`

func (r *FactorEvaluationRepo) scan(rows rowScanner) (*FactorEvaluationRecord, error) {
	var rec FactorEvaluationRecord
	err := rows.Scan(&rec.ID, &rec.UserID, &rec.Kind, &rec.FactorName, &rec.FactorVersion,
		&rec.Category, &rec.Symbol, &rec.TF, &rec.ForwardBars, &rec.ParamsJSON, &rec.Samples,
		&rec.ResultJSON, &rec.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

// ListByUser 返回该用户的最近评价（可按因子过滤），limit<=0 时默认 100。
func (r *FactorEvaluationRepo) ListByUser(userID int64, factorName string, limit int) ([]*FactorEvaluationRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT ` + factorEvalColumns + ` FROM factors_evaluations WHERE user_id = $1`
	args := []any{userID}
	next := 2
	if factorName != "" {
		query += fmt.Sprintf(` AND factor_name = $%d`, next)
		args = append(args, factorName)
		next++
	}
	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d::bigint`, next)
	args = append(args, limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*FactorEvaluationRecord
	for rows.Next() {
		rec, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// GetByID 取单条（属主校验由 handler 做）。
func (r *FactorEvaluationRepo) GetByID(id int64) (*FactorEvaluationRecord, error) {
	row := db.QueryRow(`SELECT `+factorEvalColumns+` FROM factors_evaluations WHERE id = $1`, id)
	return r.scan(row)
}

// ResultAsMap 解析 result_json 为 map（handler 响应用）。
func (rec *FactorEvaluationRecord) ResultAsMap() (map[string]any, error) {
	out := map[string]any{}
	if rec.ResultJSON == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(rec.ResultJSON), &out); err != nil {
		return nil, err
	}
	return out, nil
}
