package store

import (
	"testing"
	"time"
)

// OrderRepo.GetByID 对 xt_orders.created_at/updated_at（历史 REAL 列）的扫描
// 必须与 List 同口径：先收 float64 再显式转 int64。回归：单查大毫秒时间戳
// 不再报 "unsupported Scan" 或错值（科学计数法字符串解析失败）。
func TestOrderRepoGetByIDRealTimestampColumns(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewOrderRepo()
	nowMs := time.Now().UnixMilli()
	rec := &OrderRecord{
		ID:           "ord-real-ts-1",
		Symbol:       "BTCUSDT",
		Side:         "BUY",
		OrderType:    "LIMIT",
		Price:        50000.5,
		Quantity:     0.25,
		Filled:       0.1,
		Status:       "PARTIALLY_FILLED",
		Exchange:     "paper",
		UserID:       42,
		ClientOID:    "webhook:tv",
		AvgFillPrice: 50000.25,
		CreatedAt:    nowMs - 3600_000,
		UpdatedAt:    nowMs,
	}
	if err := repo.Create(rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	// 确认列确为 REAL 亲和（REAL 列里 int64 以浮点存储，驱动读出 float64）。
	var colType string
	if err := db.QueryRow(`SELECT type FROM pragma_table_info('xt_orders') WHERE name='created_at'`).Scan(&colType); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if colType != "REAL" {
		t.Fatalf("created_at should be REAL affinity, got %q", colType)
	}

	got, err := repo.GetByID("ord-real-ts-1")
	if err != nil {
		t.Fatalf("GetByID must not fail on REAL timestamp columns: %v", err)
	}
	if got == nil {
		t.Fatal("expected record")
	}
	if got.CreatedAt != rec.CreatedAt || got.UpdatedAt != rec.UpdatedAt {
		t.Fatalf("timestamp round-trip mismatch: got (%d,%d), want (%d,%d)",
			got.CreatedAt, got.UpdatedAt, rec.CreatedAt, rec.UpdatedAt)
	}
	if got.Price != rec.Price || got.Filled != rec.Filled || got.AvgFillPrice != rec.AvgFillPrice ||
		got.UserID != rec.UserID || got.ClientOID != rec.ClientOID || got.Status != rec.Status {
		t.Fatalf("field round-trip mismatch: %+v", got)
	}

	// 与 List 同口径交叉验证：同一行两种读法结果一致。
	list, err := repo.List(map[string]any{"id": "ord-real-ts-1"}, 1)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}
	if list[0].CreatedAt != got.CreatedAt || list[0].UpdatedAt != got.UpdatedAt {
		t.Fatalf("GetByID/List timestamp mismatch: %+v vs %+v", got, list[0])
	}

	// 直写 SQL 的浮点时间戳（老库真实形态）也必须能读。
	if _, err := db.Exec(
		`INSERT INTO xt_orders (id, symbol, side, order_type, price, quantity, status, exchange, created_at, updated_at)
		 VALUES ('ord-real-ts-2','ETHUSDT','SELL','MARKET',3000.0,1.0,'FILLED','paper',?,?)`,
		float64(nowMs-7200_000)+0.5, float64(nowMs)+0.5,
	); err != nil {
		t.Fatalf("raw insert float ts: %v", err)
	}
	got2, err := repo.GetByID("ord-real-ts-2")
	if err != nil || got2 == nil {
		t.Fatalf("GetByID float ts row: %v", err)
	}
	if got2.CreatedAt != nowMs-7200_000 || got2.UpdatedAt != nowMs {
		t.Fatalf("float ts truncation wrong: got (%d,%d)", got2.CreatedAt, got2.UpdatedAt)
	}

	missing, err := repo.GetByID("ord-nope")
	if err == nil {
		t.Fatalf("missing id should return error (sql.ErrNoRows), got %v", missing)
	}
}

// NetFilledByStrategy：sig:<策略id>: 前缀归因的净买量与买入 VWAP——
// 重启仓位重建的数据源（2026-10-01 666 三次重启三次重复买入的根治）。
func TestNetFilledByStrategy(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewOrderRepo()
	nowMs := time.Now().UnixMilli()
	mk := func(id, side, oid string, qty, avg float64) *OrderRecord {
		return &OrderRecord{
			ID: id, Symbol: "BTCUSDT", Side: side, OrderType: "MARKET",
			Quantity: qty, Filled: qty, Status: "FILLED", Exchange: "binance",
			ClientOID: oid, AvgFillPrice: avg, CreatedAt: nowMs, UpdatedAt: nowMs,
		}
	}
	// 该策略两笔买单 0.1@50000 + 0.2@51000 → 净 0.3，VWAP=50666.67
	if err := repo.Create(mk("ord-n1", "BUY", "sig:cfg9:1", 0.1, 50000)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(mk("ord-n2", "BUY", "sig:cfg9:2", 0.2, 51000)); err != nil {
		t.Fatal(err)
	}
	// 卖出一半 + 其他策略的同 symbol 单（不得混入）
	if err := repo.Create(mk("ord-n3", "SELL", "sig:cfg9:3", 0.1, 52000)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(mk("ord-n4", "BUY", "sig:other:1", 9.9, 1)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(mk("ord-n5", "BUY", "", 9.9, 1)); err != nil {
		t.Fatal(err)
	}

	net, vwap, err := NetFilledByStrategy("cfg9", "BTCUSDT")
	if err != nil {
		t.Fatalf("net: %v", err)
	}
	if net < 0.1999 || net > 0.2001 {
		t.Fatalf("net = %v, want 0.2", net)
	}
	wantVWAP := (0.1*50000 + 0.2*51000) / 0.3
	if vwap < wantVWAP-0.01 || vwap > wantVWAP+0.01 {
		t.Fatalf("vwap = %v, want %v", vwap, wantVWAP)
	}

	// 无记录策略 → 0
	if net, _, err := NetFilledByStrategy("nope", "BTCUSDT"); err != nil || net != 0 {
		t.Fatalf("empty strategy: net=%v err=%v", net, err)
	}
}

// TestStrategyPaperPnL 平均成本法绩效：已实现 + 浮动，超卖保护，账本归属隔离。
func TestStrategyPaperPnL(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewOrderRepo()
	nowMs := time.Now().UnixMilli()
	mk := func(id, side, oid string, qty, avg float64) *OrderRecord {
		return &OrderRecord{
			ID: id, Symbol: "BTCUSDT", Side: side, OrderType: "MARKET",
			Quantity: qty, Filled: qty, Status: "FILLED", Exchange: "paper",
			ClientOID: oid, AvgFillPrice: avg, CreatedAt: nowMs, UpdatedAt: nowMs,
		}
	}
	// 买 0.1@50000 + 买 0.2@51000 → 成本 50666.67；卖 0.1@52000 已实现 +133.33；
	// 余 0.2，现价 53000 → 浮动 +466.67；合计 +600。
	for _, r := range []*OrderRecord{
		mk("ord-p1", "BUY", "sig:cfgP:1", 0.1, 50000),
		mk("ord-p2", "BUY", "sig:cfgP:2", 0.2, 51000),
		mk("ord-p3", "SELL", "sig:cfgP:3", 0.1, 52000),
		mk("ord-p4", "BUY", "sig:other:1", 9.9, 1), // 其他策略不混入
		mk("ord-p5", "SELL", "sig:cfgQ:1", 9.9, 1), // 无持仓对账卖单（超卖保护）
	} {
		if err := repo.Create(r); err != nil {
			t.Fatal(err)
		}
	}

	net, avg, realized, unrealized, total, ok := StrategyPaperPnL("cfgP", "BTCUSDT", 53000, 0.001, false)
	if !ok {
		t.Fatal("expected trades")
	}
	if net < 0.1999 || net > 0.2001 {
		t.Fatalf("net = %v, want 0.2", net)
	}
	wantAvg := (0.1*50000 + 0.2*51000) / 0.3
	if avg < wantAvg-0.01 || avg > wantAvg+0.01 {
		t.Fatalf("avgCost = %v, want %v", avg, wantAvg)
	}
	if realized < 133.33-0.01 || realized > 133.33+0.01 {
		t.Fatalf("realized = %v, want 133.33", realized)
	}
	if unrealized < 466.67-0.01 || unrealized > 466.67+0.01 {
		t.Fatalf("unrealized = %v, want 466.67", unrealized)
	}
	if total < 579.59 || total > 579.61 {
		t.Fatalf("total = %v, want 579.6 (gross 600 − fee 20.4)", total)
	}

	// 无持仓时的对账/重建卖单：不得卖穿、不得虚增已实现盈亏。
	netQ, _, realizedQ, _, totalQ, okQ := StrategyPaperPnL("cfgQ", "BTCUSDT", 53000, 0.001, false)
	if !okQ || netQ != 0 || realizedQ != 0 || totalQ != 0 {
		t.Fatalf("oversell guard: net=%v realized=%v total=%v", netQ, realizedQ, totalQ)
	}

	// 双向合约（cfgS）：SELL 开空 0.5@50000 → BUY 平空 0.4@49000。
	// realized=(50000−49000)×0.4=400；fee=(25000+19600)×0.001=44.6；
	// mark=53000 → 余空 0.1 的浮亏=(50000−53000)×0.1=−300 → total=55.4。
	if err := repo.Create(mk("ord-s1", "SELL", "sig:cfgS:1", 0.5, 50000)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(mk("ord-s2", "BUY", "sig:cfgS:2", 0.4, 49000)); err != nil {
		t.Fatal(err)
	}
	netS, _, realizedS, unrealizedS, totalS, okS := StrategyPaperPnL("cfgS", "BTCUSDT", 53000, 0.001, true)
	if !okS {
		t.Fatal("short: expected trades")
	}
	if netS > -0.0999 || netS < -0.1001 {
		t.Fatalf("short net = %v, want -0.1", netS)
	}
	if realizedS < 399.99 || realizedS > 400.01 {
		t.Fatalf("short realized = %v, want 400", realizedS)
	}
	if unrealizedS < -300.01 || unrealizedS > -299.99 {
		t.Fatalf("short unrealized = %v, want -300", unrealizedS)
	}
	if totalS < 55.39 || totalS > 55.41 {
		t.Fatalf("short total = %v, want 55.4", totalS)
	}

	// 无成交记录 → ok=false
	if _, _, _, _, _, has := StrategyPaperPnL("nope", "BTCUSDT", 53000, 0.001, false); has {
		t.Fatal("no-record strategy must return ok=false")
	}
}

// FilledOrdersByStrategy：重启分档重建的逐笔数据源——按成交时间升序、
// sig:<id>: 前缀归因、只收 FILLED 且 filled/avg_fill_price>0，其他策略与
// 未打标单不混入（口径与 NetFilledByStrategy 一致）。
func TestFilledOrdersByStrategy(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewOrderRepo()
	nowMs := time.Now().UnixMilli()
	mk := func(id, side, oid, status string, qty, avg float64, ts int64) *OrderRecord {
		return &OrderRecord{
			ID: id, Symbol: "BTCUSDT", Side: side, OrderType: "MARKET",
			Quantity: qty, Filled: qty, Status: status, Exchange: "paper",
			ClientOID: oid, AvgFillPrice: avg, CreatedAt: ts, UpdatedAt: ts,
		}
	}
	for _, r := range []*OrderRecord{
		mk("ord-f3", "BUY", "sig:cfgF:3", "FILLED", 4, 80, nowMs+2),
		mk("ord-f1", "BUY", "sig:cfgF:1", "FILLED", 1, 100, nowMs),
		mk("ord-f2", "BUY", "sig:cfgF:2", "FILLED", 2, 90, nowMs+1),
		mk("ord-f4", "SELL", "sig:cfgF:4", "FILLED", 4, 96, nowMs+3),
		mk("ord-f5", "BUY", "sig:other:1", "FILLED", 9.9, 1, nowMs+4), // 其他策略
		mk("ord-f6", "BUY", "", "FILLED", 9.9, 1, nowMs+5),            // 未打标
		mk("ord-f7", "BUY", "sig:cfgF:7", "NEW", 1, 1, nowMs+6),       // 未成交
	} {
		if err := repo.Create(r); err != nil {
			t.Fatal(err)
		}
	}

	fills, err := FilledOrdersByStrategy("cfgF", "BTCUSDT")
	if err != nil {
		t.Fatalf("fills: %v", err)
	}
	if len(fills) != 4 {
		t.Fatalf("fills len = %d, want 4: %+v", len(fills), fills)
	}
	// 升序：100@1 → 90@2 → 80@4 → SELL 4@96。ClientOID 随查询透出
	// （G1 手动档重建靠 ":manual:" 中缀识别）。
	want := []OrderFill{
		{Side: "BUY", Filled: 1, AvgFillPrice: 100, ClientOID: "sig:cfgF:1"},
		{Side: "BUY", Filled: 2, AvgFillPrice: 90, ClientOID: "sig:cfgF:2"},
		{Side: "BUY", Filled: 4, AvgFillPrice: 80, ClientOID: "sig:cfgF:3"},
		{Side: "SELL", Filled: 4, AvgFillPrice: 96, ClientOID: "sig:cfgF:4"},
	}
	for i, w := range want {
		if fills[i] != w {
			t.Fatalf("fills[%d] = %+v, want %+v", i, fills[i], w)
		}
	}

	// 无记录策略 → 空。
	if fills, err := FilledOrdersByStrategy("nope", "BTCUSDT"); err != nil || len(fills) != 0 {
		t.Fatalf("empty strategy: fills=%v err=%v", fills, err)
	}
}
