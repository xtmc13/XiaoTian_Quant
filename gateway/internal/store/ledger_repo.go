package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

// ── 复式账本流水（D1）────────────────────────────────────────
//
// ledger_entries 是 append-only 的资金流水：每一笔冻结/释放/成交扣费/划转/
// 入账都过一条，幂等键去重。账户+币种余额由流水重算派生（见 Balance /
// BalancesByAsset），balance_after 只是过账时点的快照，用于对账校验，不作为
// 余额真值来源。

const (
	LedgerDebit  = "debit"  // 冻结/支出/扣费/划出（余额减少）
	LedgerCredit = "credit" // 释放/收入/入账/划入（余额增加）
)

// 常见业务类型（biz_type），便于对账与查询归类。
const (
	LedgerBizFreeze    = "order_freeze"    // 下单冻结保证金/资金
	LedgerBizRelease   = "order_release"   // 撤单/成交后释放冻结
	LedgerBizFill      = "trade_fill"       // 成交计入
	LedgerBizFee       = "fee"             // 手续费扣费
	LedgerBizFunding   = "funding"         // 资金费
	LedgerBizTransfer  = "transfer"        // 钱包划转
	LedgerBizDeposit    = "deposit"         // 入账/充值
	LedgerBizWithdraw  = "withdraw"        // 出金
	LedgerBizAdjust    = "adjust"          // 手工/系统调整
)

// ErrLedgerDuplicate 幂等键冲突：该业务事件已过账，重复调用不再入账。
var ErrLedgerDuplicate = errors.New("ledger entry already exists")

// LedgerEntry 一条账本流水。
type LedgerEntry struct {
	ID             int64   `json:"id"`
	UserID         int64   `json:"user_id"`
	Account        string  `json:"account"` // 账户/钱包标识：spot|futures|funding|paper|default
	Asset          string  `json:"asset"`
	Direction      string  `json:"direction"` // LedgerDebit | LedgerCredit
	Amount         float64 `json:"amount"`    // 非负绝对值
	BalanceAfter   float64 `json:"balance_after"`
	BizType        string  `json:"biz_type"`
	BizID          string  `json:"biz_id"`
	IdempotencyKey string  `json:"idempotency_key"`
	Detail         string  `json:"detail"`
	CreatedAt      int64   `json:"created_at"`
}

// LedgerRepo 账本流水读写入口。
type LedgerRepo struct{}

// NewLedgerRepo 创建账本仓储。
func NewLedgerRepo() *LedgerRepo { return &LedgerRepo{} }

// Append 过账一条流水。幂等键冲突返回 ErrLedgerDuplicate（不重复入账），
// 同时返回已存在条目 ID。balance_after 由调用方按过账后余额填入（快照）。
func (r *LedgerRepo) Append(e *LedgerEntry) (int64, error) {
	if db == nil {
		return 0, nil
	}
	if e.Asset == "" {
		return 0, fmt.Errorf("ledger: asset required")
	}
	if e.Direction != LedgerDebit && e.Direction != LedgerCredit {
		return 0, fmt.Errorf("ledger: invalid direction %q", e.Direction)
	}
	if e.IdempotencyKey == "" {
		return 0, fmt.Errorf("ledger: idempotency_key required")
	}
	if e.Account == "" {
		e.Account = "default"
	}
	if e.CreatedAt == 0 {
		e.CreatedAt = time.Now().UnixMilli()
	}

	var id int64
	err := db.QueryRow(`INSERT INTO ledger_entries
		(user_id, account, asset, direction, amount, balance_after, biz_type, biz_id, idempotency_key, detail, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id`,
		e.UserID, e.Account, e.Asset, e.Direction, e.Amount, e.BalanceAfter,
		e.BizType, e.BizID, e.IdempotencyKey, e.Detail, e.CreatedAt,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		var existing int64
		if qerr := db.QueryRow(
			`SELECT id FROM ledger_entries WHERE idempotency_key=$1`, e.IdempotencyKey,
		).Scan(&existing); qerr == nil {
			return existing, ErrLedgerDuplicate
		}
		return 0, ErrLedgerDuplicate
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

// Balance 由流水重算某账户+币种余额：credit 合计 - debit 合计。
func (r *LedgerRepo) Balance(userID int64, account, asset string) (float64, error) {
	if db == nil {
		return 0, nil
	}
	if account == "" {
		account = "default"
	}
	var credit, debit float64
	err := db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN direction='credit' THEN amount ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN direction='debit'  THEN amount ELSE 0 END),0)
		FROM ledger_entries
		WHERE user_id=$1 AND account=$2 AND asset=$3`,
		userID, account, asset,
	).Scan(&credit, &debit)
	if err != nil {
		return 0, err
	}
	return credit - debit, nil
}

// BalancesByAsset 返回账户下各币种派生余额（credit-debit），键为币种。
func (r *LedgerRepo) BalancesByAsset(userID int64, account string) (map[string]float64, error) {
	out := map[string]float64{}
	if db == nil {
		return out, nil
	}
	if account == "" {
		account = "default"
	}
	rows, err := db.Query(`SELECT asset,
		COALESCE(SUM(CASE WHEN direction='credit' THEN amount ELSE 0 END),0)
		- COALESCE(SUM(CASE WHEN direction='debit'  THEN amount ELSE 0 END))
		FROM ledger_entries
		WHERE user_id=$1 AND account=$2
		GROUP BY asset`,
		userID, account)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var asset string
		var bal float64
		if err := rows.Scan(&asset, &bal); err != nil {
			return out, err
		}
		out[asset] = bal
	}
	return out, rows.Err()
}

// List 返回账户+币种（asset 为空则不限）的流水，按时间倒序、limit 条。
func (r *LedgerRepo) List(userID int64, account, asset string, limit int) ([]LedgerEntry, error) {
	out := []LedgerEntry{}
	if db == nil {
		return out, nil
	}
	if account == "" {
		account = "default"
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := db.Query(`SELECT id, user_id, account, asset, direction, amount,
		balance_after, biz_type, biz_id, idempotency_key, detail, created_at
		FROM ledger_entries
		WHERE user_id=$1 AND account=$2 AND ($3='' OR asset=$3)
		ORDER BY created_at DESC, id DESC
		LIMIT $4`,
		userID, account, asset, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.UserID, &e.Account, &e.Asset, &e.Direction, &e.Amount,
			&e.BalanceAfter, &e.BizType, &e.BizID, &e.IdempotencyKey, &e.Detail, &e.CreatedAt); err != nil {
			return out, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ── 包级默认实例与便捷函数 ───────────────────────────────────

var ledgerRepo = NewLedgerRepo()

// GetLedgerRepo 返回默认账本仓储实例。
func GetLedgerRepo() *LedgerRepo { return ledgerRepo }

// AppendLedgerEntry 包级便捷过账入口。
func AppendLedgerEntry(e *LedgerEntry) (int64, error) { return ledgerRepo.Append(e) }

// LedgerBalance 包级便捷派生余额查询。
func LedgerBalance(userID int64, account, asset string) (float64, error) {
	return ledgerRepo.Balance(userID, account, asset)
}

// BalanceDiff 账本派生余额与声明余额（快照/交易所）的差异。
type BalanceDiff struct {
	Asset           string  `json:"asset"`
	LedgerBalance  float64 `json:"ledger_balance"`
	DeclaredBalance float64 `json:"declared_balance"`
	Delta          float64 `json:"delta"` // ledger - declared
}

// ReconcileBalances 以账本流水派生余额为准，与 declared（本地快照/交易所声明
// 余额）逐币种比对。tolerance 内视为一致；不一致项返回并落 reconcile_diffs
// （diff_type='balance'，asset=币种，amount=|delta|，symbol=账户/钱包，
// exchange=交易所标识），供对账面板与告警读取。
func (r *LedgerRepo) ReconcileBalances(userID int64, account, exchange string, declared map[string]float64, tolerance float64) ([]BalanceDiff, error) {
	ledger, err := r.BalancesByAsset(userID, account)
	if err != nil {
		return nil, err
	}
	if tolerance <= 0 {
		tolerance = 1e-6
	}
	if account == "" {
		account = "default"
	}

	assets := map[string]struct{}{}
	for a := range ledger {
		assets[a] = struct{}{}
	}
	for a := range declared {
		assets[a] = struct{}{}
	}

	diffs := make([]BalanceDiff, 0)
	reconcileRepo := NewReconcileRepo()
	for a := range assets {
		lb := ledger[a]
		db := declared[a]
		delta := lb - db
		if math.Abs(delta) <= tolerance {
			continue
		}
		diffs = append(diffs, BalanceDiff{
			Asset:            a,
			LedgerBalance:    lb,
			DeclaredBalance:  db,
			Delta:            delta,
		})
		_, _, _ = reconcileRepo.CreateDiff(&ReconcileDiff{
			UserID:    userID,
			Exchange:  exchange,
			Symbol:    account,
			DiffType:  "balance",
			Asset:     a,
			LocalQty:  lb,
			ExchangeQty: db,
			Amount:    math.Abs(delta),
			Detail:    fmt.Sprintf("ledger=%.8f declared=%.8f delta=%.8f", lb, db, delta),
		})
	}
	return diffs, nil
}
