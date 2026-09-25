package order

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// 重启恢复路径需要真实 DB（xt_orders 扫描）：临时 sqlite，绝不触碰 ./runtime。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "order_test")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("DB_PATH", filepath.Join(dir, "gateway.db"))
	_ = os.Setenv("SECRET_KEY", "test-secret-key-not-for-production-use-only")
	if err := store.InitDB(); err != nil {
		panic(err)
	}
	code := m.Run()
	store.CloseDB()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// persistLMRecord 把一张 ltm 订单写进 xt_orders，模拟重启前 OMS 已持久化的状态。
// 用 Upsert 保证 -count=N 重跑时幂等。
func persistLMRecord(t *testing.T, rec *store.OrderRecord) {
	t.Helper()
	if err := store.GetOrderRepo().Upsert(rec); err != nil {
		t.Fatalf("persist order %s: %v", rec.ID, err)
	}
}

// finishLMRecord 把记录写成终态（真实环境 OMS 状态翻转会回写 xt_orders；
// fake 不落库，测试结束前提前清理，避免污染同库其他恢复扫描用例）。
func finishLMRecord(t *testing.T, rec *store.OrderRecord, status string) {
	t.Helper()
	cp := *rec
	cp.Status = status
	if err := store.GetOrderRepo().Upsert(&cp); err != nil {
		t.Fatalf("finish order %s: %v", rec.ID, err)
	}
}

func waitLMTerminal(t *testing.T, tr *LimitMarketTracker, parentID string) *LMState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := tr.Get(parentID)
		if s != nil && s.IsTerminal() {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	return tr.Get(parentID)
}

// 重启恢复：挂在 active 的 ltm 父单在重启后被重新跟踪，宽限期后照常
// 走 超时→撤限价剩余→市价补单 状态机直至 completed。
func TestLMRestoreResumesAfterRestart(t *testing.T) {
	// ── 第一次"进程"：下单但不启动 watcher（模拟挂在中间态就宕机）──
	placer1 := newFakeLMPlacer()
	tr1 := NewLimitMarketTracker(placer1)
	req := &Request{
		Symbol: "BTCUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 50000, Quantity: 1.0, Exchange: "paper", UserID: 7,
	}
	ord, state, err := tr1.PlaceLimitThenMarket(req, 10*time.Second)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if state.Status != "active" {
		t.Fatalf("应处于 active 中间态: %+v", state)
	}
	// OMS 在宕机前已把订单持久化（client_oid 带 ltm- 前缀）。
	persistLMRecord(t, &store.OrderRecord{
		ID: ord.ID, Symbol: ord.Symbol, Side: string(ord.Side), OrderType: string(ord.OrderType),
		Price: ord.Price, Quantity: ord.Quantity, Filled: 0, Status: "NEW",
		Exchange: ord.Exchange, UserID: 7, ClientOID: ord.ClientOID,
		CreatedAt: ord.CreatedAt, UpdatedAt: ord.UpdatedAt,
	})

	// ── 第二次"进程"：新 placer（内存全空）+ 新 tracker，从 DB 恢复 ──
	placer2 := newFakeLMPlacer()
	tr2 := NewLimitMarketTracker(placer2)
	if n := tr2.RestoreFromStore(200 * time.Millisecond); n != 1 {
		t.Fatalf("应恢复 1 个 ltm 订单, got %d", n)
	}
	restored := tr2.Get(ord.ID)
	if restored == nil || restored.Status != "active" {
		t.Fatalf("恢复后应重新进入 active: %+v", restored)
	}
	if restored.LimitOrderID != ord.ID || restored.Quantity != 1.0 {
		t.Fatalf("恢复状态字段错误: %+v", restored)
	}
	// OMS（placer）内存应被回填，否则恢复后撤单/查单全部 miss。
	if placer2.GetOrder(ord.ID) == nil {
		t.Fatal("恢复必须把订单回填 OMS 内存（HandleOrderUpdate）")
	}
	// 幂等：重复恢复不得重复跟踪。
	if n := tr2.RestoreFromStore(200 * time.Millisecond); n != 0 {
		t.Fatalf("已跟踪的订单不得重复恢复, got %d", n)
	}

	tr2.Start()
	defer tr2.Stop()
	s := waitLMTerminal(t, tr2, ord.ID)
	if s == nil || s.Status != "completed" {
		t.Fatalf("恢复后宽限期超时仍应走通状态机至 completed: %+v", s)
	}
	mkts := placer2.marketOrders()
	if len(mkts) != 1 || mkts[0].Quantity != 1.0 {
		t.Fatalf("恢复后应市价补单剩余 1.0: %+v", mkts)
	}
	if mkts[0].ClientOID != "ltm-mkt:"+ord.ID {
		t.Fatalf("补单 client_oid 应回链父单: %s", mkts[0].ClientOID)
	}
	// 真实环境 OMS 撤单后会回写 xt_orders（om.storeOrder）；fake 不落库，
	// 这里补写终态，模拟生产 DB 状态，避免污染后续恢复扫描。
	_ = store.GetOrderRepo().Update(&store.OrderRecord{
		ID: ord.ID, Symbol: ord.Symbol, Side: string(ord.Side), OrderType: string(ord.OrderType),
		Price: ord.Price, Quantity: ord.Quantity, Filled: 0, Status: "CANCELLED",
		Exchange: ord.Exchange, UserID: 7, ClientOID: ord.ClientOID,
		CreatedAt: ord.CreatedAt,
	})
}

// 恢复扫描必须跳过：已终结的 ltm 单、非 ltm 单、孤儿市价补单（ltm-mkt: 前缀不是父单）。
func TestLMRestoreSkipsTerminalAndNonLTM(t *testing.T) {
	base := time.Now().UnixMilli()
	persistLMRecord(t, &store.OrderRecord{
		ID: "skip-filled", Symbol: "BTCUSDT", Side: "BUY", OrderType: "LIMIT",
		Price: 50000, Quantity: 1, Filled: 1, Status: "FILLED",
		Exchange: "paper", ClientOID: "ltm-skip-filled", CreatedAt: base,
	})
	persistLMRecord(t, &store.OrderRecord{
		ID: "skip-cancelled", Symbol: "BTCUSDT", Side: "BUY", OrderType: "LIMIT",
		Price: 50000, Quantity: 1, Status: "CANCELLED",
		Exchange: "paper", ClientOID: "ltm-skip-cancelled", CreatedAt: base,
	})
	persistLMRecord(t, &store.OrderRecord{
		ID: "skip-plain", Symbol: "BTCUSDT", Side: "BUY", OrderType: "LIMIT",
		Price: 50000, Quantity: 1, Status: "NEW",
		Exchange: "paper", ClientOID: "plain-order", CreatedAt: base,
	})
	persistLMRecord(t, &store.OrderRecord{
		ID: "skip-mktleg", Symbol: "BTCUSDT", Side: "BUY", OrderType: "MARKET",
		Quantity: 1, Status: "NEW",
		Exchange: "paper", ClientOID: "ltm-mkt:skip-mktleg", CreatedAt: base,
	})

	tr := NewLimitMarketTracker(newFakeLMPlacer())
	if n := tr.RestoreFromStore(time.Second); n != 0 {
		t.Fatalf("终结/非 ltm/补单腿都不得作为父单恢复, got %d", n)
	}
}

// 重启前已发出市价补单的：恢复时认领补单腿，只确认结果，不重复撤限价单。
func TestLMRestorePicksUpMarketLeg(t *testing.T) {
	base := time.Now().UnixMilli()
	parentID := "restore-mkt-parent"
	parentRec := &store.OrderRecord{
		ID: parentID, Symbol: "ETHUSDT", Side: "SELL", OrderType: "LIMIT",
		Price: 3000, Quantity: 1.0, Filled: 0.4, Status: "PARTIALLY_FILLED",
		Exchange: "live", UserID: 3, ClientOID: "ltm-restore-mkt", AvgFillPrice: 3000,
		CreatedAt: base,
	}
	persistLMRecord(t, parentRec)
	mktID := "restore-mkt-leg"
	mktRec := &store.OrderRecord{
		ID: mktID, Symbol: "ETHUSDT", Side: "SELL", OrderType: "MARKET",
		Quantity: 0.6, Filled: 0, Status: "NEW",
		Exchange: "live", UserID: 3, ClientOID: "ltm-mkt:" + parentID,
		CreatedAt: base + 1,
	}
	persistLMRecord(t, mktRec)
	// 真实环境状态翻转会回写 DB；fake 不落库，测试结束前补终态避免污染后续扫描。
	t.Cleanup(func() {
		finishLMRecord(t, parentRec, "CANCELLED")
		finishLMRecord(t, mktRec, "FILLED")
	})

	// 交易所侧事实：限价单已撤、市价补单已全成交（宕机前来不及回写 DB）。
	placer := newFakeLMPlacer()
	placer.orders[parentID] = &model.OrderData{
		ID: parentID, Symbol: "ETHUSDT", Side: model.SideSell, OrderType: model.TypeLimit,
		Price: 3000, Quantity: 1.0, Filled: 0.4, AvgFillPrice: 3000,
		Status: model.StatusCancelled, Exchange: "live",
	}
	placer.orders[mktID] = &model.OrderData{
		ID: mktID, Symbol: "ETHUSDT", Side: model.SideSell, OrderType: model.TypeMarket,
		Quantity: 0.6, Filled: 0.6, AvgFillPrice: 3001,
		Status: model.StatusFilled, Exchange: "live",
	}

	tr := NewLimitMarketTracker(placer)
	if n := tr.RestoreFromStore(200 * time.Millisecond); n != 1 {
		t.Fatalf("应恢复 1 个 ltm 订单, got %d", n)
	}
	s := tr.Get(parentID)
	if s == nil || s.MarketOrderID != mktID {
		t.Fatalf("恢复必须认领已发出的市价补单腿: %+v", s)
	}
	tr.Start()
	defer tr.Stop()
	s = waitLMTerminal(t, tr, parentID)
	if s == nil || s.Status != "completed" {
		t.Fatalf("补单已成交应确认 completed: %+v", s)
	}
	if s.MarketFilled != 0.6 {
		t.Fatalf("补单成交量应以交易所事实为准: %+v", s)
	}
}

// OMS 内存全空时（真重启），父单与市价补单腿都必须从 DB 回填，
// 否则 confirmMarket 查单 miss 会误判 market_failed。
func TestLMRestoreRehydratesParentAndMarketLeg(t *testing.T) {
	base := time.Now().UnixMilli()
	parentID := "rehyd-parent"
	parentRec := &store.OrderRecord{
		ID: parentID, Symbol: "SOLUSDT", Side: "BUY", OrderType: "LIMIT",
		Price: 100, Quantity: 2.0, Filled: 1.0, Status: "PARTIALLY_FILLED",
		Exchange: "live", UserID: 5, ClientOID: "ltm-rehyd", AvgFillPrice: 100,
		CreatedAt: base,
	}
	persistLMRecord(t, parentRec)
	mktID := "rehyd-mkt-leg"
	mktRec := &store.OrderRecord{
		ID: mktID, Symbol: "SOLUSDT", Side: "BUY", OrderType: "MARKET",
		Quantity: 1.0, Status: "NEW",
		Exchange: "live", UserID: 5, ClientOID: "ltm-mkt:" + parentID,
		CreatedAt: base + 1,
	}
	persistLMRecord(t, mktRec)
	t.Cleanup(func() {
		finishLMRecord(t, parentRec, "CANCELLED")
		finishLMRecord(t, mktRec, "FILLED")
	})

	placer := newFakeLMPlacer() // 内存全空，模拟真重启
	tr := NewLimitMarketTracker(placer)
	if n := tr.RestoreFromStore(time.Second); n != 1 {
		t.Fatalf("应恢复 1 个 ltm 订单, got %d", n)
	}
	if placer.GetOrder(parentID) == nil {
		t.Fatal("父单必须回填 OMS 内存")
	}
	if placer.GetOrder(mktID) == nil {
		t.Fatal("市价补单腿必须回填 OMS 内存")
	}
	// 回填的市价腿已被交易所侧成交（fake 模拟 WS 回报），watcher 应确认 completed。
	placer.orders[mktID].Status = model.StatusFilled
	placer.orders[mktID].Filled = 1.0
	tr.Start()
	defer tr.Stop()
	if s := waitLMTerminal(t, tr, parentID); s == nil || s.Status != "completed" {
		t.Fatalf("回填后应确认 completed: %+v", s)
	}
}
