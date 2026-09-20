package store

import (
	"fmt"
	"testing"
)

// makeEpoch 构造一条 epoch 测试记录。
func makeEpoch(id, jobID string, userID int64, loss float64, sharpe float64, trades int, dd float64) *HyperoptEpochRecord {
	return &HyperoptEpochRecord{
		ID:          id,
		UserID:      userID,
		JobID:       jobID,
		StrategyID:  "cfg-1",
		TrialID:     trades,
		ParamsJSON:  `{"lookback": 20}`,
		MetricsJSON: fmt.Sprintf(`{"sharpe_ratio": %v, "total_trades": %d, "max_drawdown_pct": %v}`, sharpe, trades, dd),
		Loss:        loss,
		LossName:    "sharpe",
		CreatedAt:   1700000000000 + int64(trades),
	}
}

func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }

func TestHyperoptEpochRepoCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewHyperoptEpochRepo()
	rec := makeEpoch("ep-1", "ho-1", 1, -1.5, 2.0, 10, 5.0)
	if err := repo.Create(rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetByID("ep-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected record")
	}
	if got.Loss != -1.5 || got.LossName != "sharpe" || got.TrialID != 10 {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
	if got.Applied {
		t.Error("fresh epoch must not be applied")
	}

	if err := repo.MarkApplied("ep-1", 0); err != nil {
		t.Fatalf("mark applied: %v", err)
	}
	got, _ = repo.GetByID("ep-1")
	if !got.Applied || got.AppliedAt == 0 {
		t.Errorf("expected applied with timestamp, got %+v", got)
	}

	// 不存在 → (nil, nil)
	miss, err := repo.GetByID("nope")
	if err != nil || miss != nil {
		t.Errorf("missing epoch should be (nil, nil), got %v, %v", miss, err)
	}
}

func TestHyperoptEpochRepoListBase(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewHyperoptEpochRepo()
	for i, ep := range []*HyperoptEpochRecord{
		makeEpoch("e1", "job-a", 1, -2.0, 2.5, 30, 4.0),
		makeEpoch("e2", "job-a", 1, -1.0, 1.5, 10, 8.0),
		makeEpoch("e3", "job-b", 2, -3.0, 3.0, 50, 12.0),
	} {
		ep.TrialID = i + 1
		if err := repo.Create(ep); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// 无过滤：按 created_at 升序全量
	all, err := repo.List(HyperoptEpochFilter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("list all: %v, n=%d", err, len(all))
	}

	// job_id 过滤
	byJob, _ := repo.List(HyperoptEpochFilter{JobID: "job-a"})
	if len(byJob) != 2 {
		t.Errorf("job-a should have 2 epochs, got %d", len(byJob))
	}

	// user 过滤：本人 + 无属主（user_id=0）放行，他人被排除
	byUser, _ := repo.List(HyperoptEpochFilter{UserID: 1})
	if len(byUser) != 2 {
		t.Errorf("user 1 should see 2 epochs, got %d", len(byUser))
	}
	if err := repo.Create(makeEpoch("e0", "job-c", 0, -0.5, 1.0, 5, 3.0)); err != nil {
		t.Fatalf("seed ownerless: %v", err)
	}
	byUser, _ = repo.List(HyperoptEpochFilter{UserID: 1})
	if len(byUser) != 3 {
		t.Errorf("user 1 should see own + ownerless = 3, got %d", len(byUser))
	}

	// limit
	lim, _ := repo.List(HyperoptEpochFilter{Limit: 2})
	if len(lim) != 2 {
		t.Errorf("limit 2 should cap results, got %d", len(lim))
	}
}

// TestHyperoptEpochRepoListFilterCombos 对四个数值过滤做全组合断言。
// 数据集（job-a，全部 user_id=1）：
//
//	e1: loss=-2.0 sharpe=2.5 trades=30 dd=4.0
//	e2: loss=-1.0 sharpe=1.5 trades=10 dd=8.0
//	e3: loss= 1.0 sharpe=0.5 trades=60 dd=15.0
//	e4: loss= 0.5 sharpe=1.0 trades=20 dd=10.0
func TestHyperoptEpochRepoListFilterCombos(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewHyperoptEpochRepo()
	seed := []*HyperoptEpochRecord{
		makeEpoch("c1", "job-a", 1, -2.0, 2.5, 30, 4.0),
		makeEpoch("c2", "job-a", 1, -1.0, 1.5, 10, 8.0),
		makeEpoch("c3", "job-a", 1, 1.0, 0.5, 60, 15.0),
		makeEpoch("c4", "job-a", 1, 0.5, 1.0, 20, 10.0),
	}
	for i, ep := range seed {
		ep.TrialID = i + 1
		if err := repo.Create(ep); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	ids := func(recs []*HyperoptEpochRecord) map[string]bool {
		out := map[string]bool{}
		for _, r := range recs {
			out[r.ID] = true
		}
		return out
	}
	expect := func(f HyperoptEpochFilter, want ...string) {
		t.Helper()
		got, err := repo.List(f)
		if err != nil {
			t.Fatalf("list %+v: %v", f, err)
		}
		gotIDs := ids(got)
		if len(gotIDs) != len(want) {
			t.Errorf("filter %+v: want %v, got %v", f, want, gotIDs)
			return
		}
		for _, w := range want {
			if !gotIDs[w] {
				t.Errorf("filter %+v: missing %s (got %v)", f, w, gotIDs)
			}
		}
	}

	// 单条件
	expect(HyperoptEpochFilter{JobID: "job-a", LossMax: floatPtr(-1.0)}, "c1", "c2")
	expect(HyperoptEpochFilter{JobID: "job-a", SharpeMin: floatPtr(1.5)}, "c1", "c2")
	expect(HyperoptEpochFilter{JobID: "job-a", TradeCountMin: intPtr(20)}, "c1", "c3", "c4")
	expect(HyperoptEpochFilter{JobID: "job-a", MaxDrawdownMax: floatPtr(8.0)}, "c1", "c2")

	// 两两组合
	expect(HyperoptEpochFilter{JobID: "job-a", LossMax: floatPtr(0.0), SharpeMin: floatPtr(1.5)}, "c1", "c2")
	expect(HyperoptEpochFilter{JobID: "job-a", LossMax: floatPtr(1.0), TradeCountMin: intPtr(30)}, "c1", "c3")
	expect(HyperoptEpochFilter{JobID: "job-a", LossMax: floatPtr(0.0), MaxDrawdownMax: floatPtr(10.0)}, "c1", "c2")
	expect(HyperoptEpochFilter{JobID: "job-a", SharpeMin: floatPtr(1.0), TradeCountMin: intPtr(15)}, "c1", "c4")
	expect(HyperoptEpochFilter{JobID: "job-a", SharpeMin: floatPtr(1.0), MaxDrawdownMax: floatPtr(8.0)}, "c1", "c2")
	expect(HyperoptEpochFilter{JobID: "job-a", TradeCountMin: intPtr(10), MaxDrawdownMax: floatPtr(10.0)}, "c1", "c2", "c4")

	// 三组合
	expect(HyperoptEpochFilter{
		JobID: "job-a", LossMax: floatPtr(0.0), SharpeMin: floatPtr(1.0), TradeCountMin: intPtr(10),
	}, "c1", "c2")
	expect(HyperoptEpochFilter{
		JobID: "job-a", LossMax: floatPtr(1.0), SharpeMin: floatPtr(0.5), MaxDrawdownMax: floatPtr(15.0),
	}, "c1", "c2", "c3", "c4")
	expect(HyperoptEpochFilter{
		JobID: "job-a", SharpeMin: floatPtr(1.0), TradeCountMin: intPtr(10), MaxDrawdownMax: floatPtr(10.0),
	}, "c1", "c2", "c4")

	// 全组合
	expect(HyperoptEpochFilter{
		JobID: "job-a", LossMax: floatPtr(0.0), SharpeMin: floatPtr(1.0),
		TradeCountMin: intPtr(10), MaxDrawdownMax: floatPtr(10.0),
	}, "c1", "c2")

	// 全组合（空结果）
	expect(HyperoptEpochFilter{
		JobID: "job-a", LossMax: floatPtr(-3.0), SharpeMin: floatPtr(1.0),
		TradeCountMin: intPtr(10), MaxDrawdownMax: floatPtr(10.0),
	})

	// job_id + 数值 + user 三维组合
	expect(HyperoptEpochFilter{
		JobID: "job-a", UserID: 1, SharpeMin: floatPtr(2.0),
	}, "c1")
	expect(HyperoptEpochFilter{
		JobID: "job-a", UserID: 2, SharpeMin: floatPtr(2.0),
	})

	// limit 在过滤之后生效
	lim, err := repo.List(HyperoptEpochFilter{JobID: "job-a", TradeCountMin: intPtr(10), Limit: 2})
	if err != nil || len(lim) != 2 {
		t.Errorf("post-filter limit: n=%d err=%v", len(lim), err)
	}
}
