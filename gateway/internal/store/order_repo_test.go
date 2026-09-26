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
