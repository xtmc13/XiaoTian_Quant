package store

import (
	"errors"
	"math"
	"testing"
)

func TestLedgerAppendIdempotent(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewLedgerRepo()
	e := &LedgerEntry{
		UserID:         7,
		Account:        "paper",
		Asset:          "USDT",
		Direction:      LedgerCredit,
		Amount:         100,
		BalanceAfter:  100,
		BizType:        LedgerBizDeposit,
		BizID:          "dep-1",
		IdempotencyKey: "dep-7-usdt-1",
	}
	id1, err := repo.Append(e)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if id1 == 0 {
		t.Fatal("expected non-zero entry id")
	}

	// 同一幂等键重复过账：不重复入账，返回重复错误与既有条目 ID。
	id2, err := repo.Append(e)
	if !errors.Is(err, ErrLedgerDuplicate) {
		t.Fatalf("duplicate append: got err %v, want ErrLedgerDuplicate", err)
	}
	if id2 != id1 {
		t.Fatalf("duplicate returned id %d, want existing %d", id2, id1)
	}

	bal, err := repo.Balance(7, "paper", "USDT")
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if bal != 100 {
		t.Fatalf("balance = %v, want 100 (no double entry)", bal)
	}
}

func TestLedgerBalanceDerivation(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewLedgerRepo()
	entries := []*LedgerEntry{
		{UserID: 7, Account: "paper", Asset: "USDT", Direction: LedgerCredit, Amount: 100, BizType: LedgerBizDeposit, IdempotencyKey: "k-1"},
		{UserID: 7, Account: "paper", Asset: "USDT", Direction: LedgerDebit, Amount: 30, BizType: LedgerBizFee, IdempotencyKey: "k-2"},
		{UserID: 7, Account: "paper", Asset: "BTC", Direction: LedgerCredit, Amount: 0.5, BizType: LedgerBizFill, IdempotencyKey: "k-3"},
	}
	for _, e := range entries {
		if _, err := repo.Append(e); err != nil {
			t.Fatalf("append %s: %v", e.IdempotencyKey, err)
		}
	}

	usdt, err := repo.Balance(7, "paper", "USDT")
	if err != nil {
		t.Fatalf("usdt balance: %v", err)
	}
	if usdt != 70 {
		t.Fatalf("USDT balance = %v, want 70", usdt)
	}
	btc, err := repo.Balance(7, "paper", "BTC")
	if err != nil {
		t.Fatalf("btc balance: %v", err)
	}
	if btc != 0.5 {
		t.Fatalf("BTC balance = %v, want 0.5", btc)
	}

	// 币种隔离：另一用户余额互不影响。
	other, err := repo.Balance(8, "paper", "USDT")
	if err != nil {
		t.Fatalf("other balance: %v", err)
	}
	if other != 0 {
		t.Fatalf("other user balance = %v, want 0", other)
	}

	balances, err := repo.BalancesByAsset(7, "paper")
	if err != nil {
		t.Fatalf("balances by asset: %v", err)
	}
	if balances["USDT"] != 70 || balances["BTC"] != 0.5 {
		t.Fatalf("balances = %v", balances)
	}
}

func TestLedgerListOrderedDesc(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewLedgerRepo()
	for i := 1; i <= 3; i++ {
		e := &LedgerEntry{
			UserID: 7, Account: "paper", Asset: "USDT",
			Direction: LedgerCredit, Amount: float64(i),
			BizType: LedgerBizDeposit, IdempotencyKey: "list-k-" + string(rune('0'+i)),
		}
		if _, err := repo.Append(e); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	rows, err := repo.List(7, "paper", "USDT", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	// 倒序：最新一条金额为 3。
	if rows[0].Amount != 3 {
		t.Fatalf("first row amount = %v, want 3 (desc)", rows[0].Amount)
	}
}

func TestLedgerReconcileBalances(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewLedgerRepo()
	// 账本派生：USDT 70（100 入账 - 30 扣费），BTC 0.5。
	entries := []*LedgerEntry{
		{UserID: 7, Account: "paper", Asset: "USDT", Direction: LedgerCredit, Amount: 100, IdempotencyKey: "r1"},
		{UserID: 7, Account: "paper", Asset: "USDT", Direction: LedgerDebit, Amount: 30, IdempotencyKey: "r2"},
		{UserID: 7, Account: "paper", Asset: "BTC", Direction: LedgerCredit, Amount: 0.5, IdempotencyKey: "r3"},
	}
	for _, e := range entries {
		if _, err := repo.Append(e); err != nil {
			t.Fatalf("append %s: %v", e.IdempotencyKey, err)
		}
	}

	// 声明余额：USDT 60（差 10），BTC 0.5（一致）。
	declared := map[string]float64{"USDT": 60, "BTC": 0.5}
	diffs, err := repo.ReconcileBalances(7, "paper", "binance", declared, 1e-6)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(diffs) != 1 {
		t.Fatalf("diffs = %d, want 1", len(diffs))
	}
	if diffs[0].Asset != "USDT" || diffs[0].Delta != 10 {
		t.Fatalf("diff = %+v", diffs[0])
	}

	// 差异已落 reconcile_diffs（open / balance / USDT / amount≈10）。
	rd := NewReconcileRepo()
	list, err := rd.ListDiffs(7, "open", "balance", "binance", 20, 0)
	if err != nil {
		t.Fatalf("list diffs: %v", err)
	}
	found := false
	for _, d := range list {
		if d.Asset == "USDT" && d.DiffType == "balance" {
			found = true
			if math.Abs(d.Amount-10) > 1e-6 {
				t.Fatalf("diff amount = %v, want 10", d.Amount)
			}
		}
	}
	if !found {
		t.Fatal("balance diff not persisted to reconcile_diffs")
	}

	// 声明对齐账本后：不再检出差异。
	aligned, err := repo.ReconcileBalances(7, "paper", "binance", map[string]float64{"USDT": 70, "BTC": 0.5}, 1e-6)
	if err != nil {
		t.Fatalf("reconcile aligned: %v", err)
	}
	if len(aligned) != 0 {
		t.Fatalf("aligned balances should have no diff: %v", aligned)
	}
}
