package handler

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 统计聚合正确性 ──

func TestComputeExecutorStats(t *testing.T) {
	now := time.Now()
	day := func(offset int) int64 {
		return now.AddDate(0, 0, -offset).UnixMilli()
	}
	signals := []*store.SignalRecord{
		{Symbol: "BTCUSDT", SourceID: "s1", CreatedAt: day(0), Status: "CLOSED"},
		{Symbol: "BTCUSDT", SourceID: "s1", CreatedAt: day(0), Status: "CLOSED"},
		{Symbol: "BTCUSDT", SourceID: "s1", CreatedAt: day(5), Status: "CLOSED"},
		{Symbol: "ETHUSDT", SourceID: "s2", CreatedAt: day(1), Status: "CLOSED"},
		{Symbol: "ETHUSDT", SourceID: "s2", CreatedAt: day(10), Status: "PENDING"},
	}
	execs := []*store.SignalExecution{
		// BTC 两单：一盈（打满 T1/T2/T3）一亏（SL）
		{Symbol: "BTCUSDT", SourceID: "s1", Status: "closed", RealizedPnL: 10, TP1Filled: true, TP2Filled: true, TP3Filled: true, ClosedAt: day(0)},
		{Symbol: "BTCUSDT", SourceID: "s1", Status: "closed", RealizedPnL: -5, SLTriggered: true, ClosedAt: day(0)},
		// ETH 一单一盈（仅 T1）
		{Symbol: "ETHUSDT", SourceID: "s2", Status: "closed", RealizedPnL: 3, TP1Filled: true, ClosedAt: day(1)},
		// 一单仍活跃（不计入成功率分母）
		{Symbol: "SOLUSDT", SourceID: "s1", Status: "active", RealizedPnL: 0},
	}
	names := map[string]string{"s1": "源一", "s2": "源二"}
	st := computeExecutorStats(signals, execs, names)

	if st.TotalSignals != 5 {
		t.Fatalf("total_signals=%d want 5", st.TotalSignals)
	}
	if st.TodaySignals != 2 {
		t.Fatalf("today_signals=%d want 2", st.TodaySignals)
	}
	if st.Executed != 1 || st.Closed != 3 {
		t.Fatalf("executed=%d closed=%d want 1/3", st.Executed, st.Closed)
	}
	// 成功率：3 单平仓 2 盈
	if st.SuccessRate < 66.6 || st.SuccessRate > 66.7 {
		t.Fatalf("success_rate=%.2f want 66.67", st.SuccessRate)
	}
	// 达成率：T1=2/3, T2=1/3, T3=1/3, SL=1/3
	if st.Tp1Rate < 66.6 || st.Tp1Rate > 66.7 {
		t.Fatalf("tp1_rate=%.2f want 66.67", st.Tp1Rate)
	}
	if st.Tp2Rate < 33.3 || st.Tp2Rate > 33.4 {
		t.Fatalf("tp2_rate=%.2f want 33.33", st.Tp2Rate)
	}
	if st.Tp3Rate < 33.3 || st.Tp3Rate > 33.4 {
		t.Fatalf("tp3_rate=%.2f want 33.33", st.Tp3Rate)
	}
	if st.SlRate < 33.3 || st.SlRate > 33.4 {
		t.Fatalf("sl_rate=%.2f want 33.33", st.SlRate)
	}
	if st.CumPnL != 8 {
		t.Fatalf("cum_pnl=%.2f want 8", st.CumPnL)
	}
	if st.AvgSignalsPerDay <= 0 {
		t.Fatalf("avg_signals_per_day=%.2f want >0", st.AvgSignalsPerDay)
	}
	// 近 30 日曲线固定 30 个点
	if len(st.Daily) != 30 {
		t.Fatalf("daily points=%d want 30", len(st.Daily))
	}
	todayPnL := 0.0
	for _, d := range st.Daily {
		if d.Date == now.Format("2006-01-02") {
			todayPnL = d.PnL
		}
	}
	if todayPnL != 5 { // 10-5
		t.Fatalf("today pnl=%.2f want 5", todayPnL)
	}
	// 来源分组
	if len(st.BySource) != 2 {
		t.Fatalf("by_source=%d want 2", len(st.BySource))
	}
	if st.BySource[0].SourceID != "s1" || st.BySource[0].Signals != 3 {
		t.Fatalf("by_source[0]=%+v want s1/3", st.BySource[0])
	}
	if st.BySource[0].CumPnL != 5 {
		t.Fatalf("s1 cum_pnl=%.2f want 5", st.BySource[0].CumPnL)
	}
	// symbol 分组（SOL 只有执行记录没有信号，不进分组）
	if len(st.BySymbol) != 2 {
		t.Fatalf("by_symbol=%d want 2", len(st.BySymbol))
	}
}

// ── 保证金模式校验 ──

func TestNormalizeMarginMode(t *testing.T) {
	if m, err := normalizeMarginMode("", "swap"); err != nil || m != "cross" {
		t.Fatalf("empty default: %s %v", m, err)
	}
	if m, err := normalizeMarginMode("hedge", "swap"); err != nil || m != "hedge" {
		t.Fatalf("hedge: %s %v", m, err)
	}
	if _, err := normalizeMarginMode("hedge", "spot"); err == nil {
		t.Fatal("hedge on spot must fail")
	}
	if _, err := normalizeMarginMode("isolated", "spot"); err == nil {
		t.Fatal("isolated on spot must fail")
	}
	if _, err := normalizeMarginMode("nonsense", "swap"); err == nil {
		t.Fatal("unknown mode must fail")
	}
}

// ── 阶梯状态机（纯评估 + 全链路推进）──

func newTestExecution(sigID int, tpPct, trailing float64) *store.SignalExecution {
	return &store.SignalExecution{
		ID: "sigexec_test_" + time.Now().Format("150405.000000000"), SignalID: sigID, SourceID: "default",
		Symbol: "TESTUSDT", Direction: "LONG", MarginMode: "hedge", PositionSide: "LONG",
		PositionID: "TESTUSDT-LONG", Status: "active",
		EntryPrice: 100, EntryQty: 1, EntryTime: time.Now().UnixMilli(),
		TP1Price: 110, TP1Qty: 0.4, TP2Price: 115, TP2Qty: 0.3, TP3Price: 120, TP3Qty: 0.3,
		SLPrice: 95, CurrentSL: 95, MoveSLTo: 100,
		TrailingPct: tpPct, RemainingQty: 1, CreatedAt: time.Now().UnixMilli(),
	}
}

func TestEvalTPSLLevels(t *testing.T) {
	e := newTestExecution(1, 0, 0)
	// 未触及任何档位
	if acts := evalTPSL(e, 105); len(acts) != 0 {
		t.Fatalf("105 must not trigger, got %v", acts)
	}
	// 直接跳价越过 TP1：只触发 TP1（逐档推进）
	acts := evalTPSL(e, 111)
	if len(acts) != 1 || acts[0].Kind != tpslActionTP1 {
		t.Fatalf("111 must trigger tp1 only, got %v", acts)
	}
	// SHORT 方向反向触发
	s := newTestExecution(1, 0, 0)
	s.Direction, s.PositionSide, s.PositionID = "SHORT", "SHORT", "TESTUSDT-SHORT"
	s.TP1Price, s.TP2Price, s.TP3Price = 90, 85, 80
	s.SLPrice, s.CurrentSL = 105, 105
	if acts := evalTPSL(s, 89); len(acts) != 1 || acts[0].Kind != tpslActionTP1 {
		t.Fatalf("short 89 must trigger tp1, got %v", acts)
	}
	if acts := evalTPSL(s, 106); len(acts) != 1 || acts[0].Kind != tpslActionSL {
		t.Fatalf("short 106 must trigger sl, got %v", acts)
	}
}

// TestLadderFullPathTP：T1→T2→T3 全达成后平仓，盈亏按各档分批计算。
func TestLadderFullPathTP(t *testing.T) {
	sig := &store.SignalRecord{Symbol: "TESTUSDT", Direction: "LONG", Strategy: "test", Status: "EXECUTED"}
	if err := store.NewSignalRepo().Create(sig); err != nil {
		t.Fatal(err)
	}
	repo := store.NewSignalExecutionRepo()
	e := newTestExecution(sig.ID, 0, 0)
	if err := repo.Create(e); err != nil {
		t.Fatal(err)
	}
	sigTPSLManager.attach(e)

	// 先建组合持仓（TP 减仓依赖 fillOrderAndUpdatePortfolio 的持仓键）
	fillOrderAndUpdatePortfolio(map[string]any{
		"symbol": "TESTUSDT", "side": "BUY", "type": "MARKET", "price": 100.0, "quantity": 1.0,
		"market_type": "swap", "position_side": "LONG", "margin_mode": "cross", "leverage": 1.0,
	})

	TickExecutorTPSL("TESTUSDT", 111) // TP1
	reload, _ := repo.GetByID(e.ID)
	if !reload.TP1Filled || reload.RemainingQty != 0.6 {
		t.Fatalf("after tp1: %+v", reload)
	}
	// TP1 触发后移动止损到保本位 + 无追踪配置
	if reload.CurrentSL != 100 {
		t.Fatalf("move sl to breakeven: current_sl=%.2f want 100", reload.CurrentSL)
	}
	if reload.CurrentTP != 115 {
		t.Fatalf("current_tp=%.2f want 115", reload.CurrentTP)
	}

	TickExecutorTPSL("TESTUSDT", 116) // TP2
	reload, _ = repo.GetByID(e.ID)
	if !reload.TP2Filled || reload.RemainingQty < 0.29 || reload.RemainingQty > 0.31 {
		t.Fatalf("after tp2: %+v", reload)
	}

	TickExecutorTPSL("TESTUSDT", 121) // TP3 → 全平
	reload, _ = repo.GetByID(e.ID)
	if reload.Status != "closed" || reload.CloseReason != "tp3" {
		t.Fatalf("after tp3: status=%s reason=%s", reload.Status, reload.CloseReason)
	}
	// 盈亏 = (110-100)*0.4 + (115-100)*0.3 + (120-100)*0.3 = 4+4.5+6 = 14.5
	if reload.RealizedPnL < 14.4 || reload.RealizedPnL > 14.6 {
		t.Fatalf("realized pnl=%.4f want ~14.5", reload.RealizedPnL)
	}
	// 信号记录已回填终态
	after, _ := store.NewSignalRepo().GetByID(strconv.Itoa(sig.ID))
	if after.Status != "CLOSED" || after.RealizedPnL != reload.RealizedPnL || after.ClosedAt == 0 {
		t.Fatalf("signal not finalized: %+v", after)
	}
	sigTPSLManager.detach(e.ID)
}

// TestLadderSLPath：价格跌破止损，剩余仓位全平，reason=sl。
func TestLadderSLPath(t *testing.T) {
	sig := &store.SignalRecord{Symbol: "TESTUSDT", Direction: "LONG", Strategy: "test", Status: "EXECUTED"}
	if err := store.NewSignalRepo().Create(sig); err != nil {
		t.Fatal(err)
	}
	repo := store.NewSignalExecutionRepo()
	e := newTestExecution(sig.ID, 0, 0)
	if err := repo.Create(e); err != nil {
		t.Fatal(err)
	}
	sigTPSLManager.attach(e)
	fillOrderAndUpdatePortfolio(map[string]any{
		"symbol": "TESTUSDT", "side": "BUY", "type": "MARKET", "price": 100.0, "quantity": 1.0,
		"market_type": "swap", "position_side": "LONG", "margin_mode": "cross", "leverage": 1.0,
	})

	TickExecutorTPSL("TESTUSDT", 94) // 跌破 SL=95
	reload, _ := repo.GetByID(e.ID)
	if reload.Status != "closed" || reload.CloseReason != "sl" || !reload.SLTriggered {
		t.Fatalf("sl path: %+v", reload)
	}
	// 盈亏 = (95-100)*1 = -5
	if reload.RealizedPnL < -5.01 || reload.RealizedPnL > -4.99 {
		t.Fatalf("sl pnl=%.4f want ~-5", reload.RealizedPnL)
	}
	sigTPSLManager.detach(e.ID)
}

// TestLadderTrailingPath：TP1 后追踪止盈，峰值回撤超比例全平。
func TestLadderTrailingPath(t *testing.T) {
	sig := &store.SignalRecord{Symbol: "TESTUSDT", Direction: "LONG", Strategy: "test", Status: "EXECUTED"}
	if err := store.NewSignalRepo().Create(sig); err != nil {
		t.Fatal(err)
	}
	repo := store.NewSignalExecutionRepo()
	e := newTestExecution(sig.ID, 0.05, 0) // 追踪回撤 5%
	e.TP2Price, e.TP2Qty = 0, 0            // 只留 TP1，避免后续跳价穿过 T2/T3
	e.TP3Price, e.TP3Qty = 0, 0
	if err := repo.Create(e); err != nil {
		t.Fatal(err)
	}
	sigTPSLManager.attach(e)
	fillOrderAndUpdatePortfolio(map[string]any{
		"symbol": "TESTUSDT", "side": "BUY", "type": "MARKET", "price": 100.0, "quantity": 1.0,
		"market_type": "swap", "position_side": "LONG", "margin_mode": "cross", "leverage": 1.0,
	})

	TickExecutorTPSL("TESTUSDT", 111) // TP1 → 激活追踪，peak=110
	reload, _ := repo.GetByID(e.ID)
	if !reload.TrailingActive {
		t.Fatalf("trailing must be active after tp1: %+v", reload)
	}

	TickExecutorTPSL("TESTUSDT", 130) // 内存峰值更新到 130（未触发动作不落库，属预期）
	TickExecutorTPSL("TESTUSDT", 123.4) // 回撤 (130-123.4)/130=5.08% ≥ 5% → trailing 全平
	reload, _ = repo.GetByID(e.ID)
	if reload.Status != "closed" || reload.CloseReason != "trailing" {
		t.Fatalf("trailing close: %+v", reload)
	}
	// 盈亏 = (110-100)*0.4 + (123.4-100)*0.6 = 4 + 14.04 = 18.04
	if reload.RealizedPnL < 18.0 || reload.RealizedPnL > 18.1 {
		t.Fatalf("trailing pnl=%.4f want ~18.04", reload.RealizedPnL)
	}
	sigTPSLManager.detach(e.ID)
}

// ── hedge 双腿持仓不互相覆盖 ──

func TestHedgeDualLegPositions(t *testing.T) {
	repo := store.NewSignalRepo()
	longSig := &store.SignalRecord{Symbol: "HEDGEUSDT", Direction: "LONG", Strategy: "hedge-test", Status: "PENDING", MarginMode: "hedge", EntryPrice: 100, PositionSize: 1}
	shortSig := &store.SignalRecord{Symbol: "HEDGEUSDT", Direction: "SHORT", Strategy: "hedge-test", Status: "PENDING", MarginMode: "hedge", EntryPrice: 100, PositionSize: 2}
	if err := repo.Create(longSig); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(shortSig); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"market_type": "swap", "leverage": 1.0}
	if _, err := executeSignalRecord(longSig, body); err != nil {
		t.Fatalf("long leg: %v", err)
	}
	if _, err := executeSignalRecord(shortSig, body); err != nil {
		t.Fatalf("short leg: %v", err)
	}

	mgr := portfolio.GetManager()
	longPos := mgr.GetPositions()
	var longLeg, shortLeg bool
	for _, p := range longPos {
		if p.Symbol != "HEDGEUSDT" {
			continue
		}
		if p.PositionSide == "LONG" && p.Quantity == 1 {
			longLeg = true
		}
		if p.PositionSide == "SHORT" && p.Quantity == 2 {
			shortLeg = true
		}
	}
	if !longLeg || !shortLeg {
		t.Fatalf("hedge legs missing: long=%v short=%v positions=%+v", longLeg, shortLeg, longPos)
	}
	// 双腿各挂一条活跃执行记录，PositionID 不冲突
	execs, _ := store.NewSignalExecutionRepo().List(map[string]any{"symbol": "HEDGEUSDT", "status": "active"}, 10)
	if len(execs) != 2 {
		t.Fatalf("active executions=%d want 2", len(execs))
	}
	for _, e := range execs {
		if e.PositionID != "HEDGEUSDT-LONG" && e.PositionID != "HEDGEUSDT-SHORT" {
			t.Fatalf("bad position id %s", e.PositionID)
		}
		if e.MarginMode != "hedge" {
			t.Fatalf("margin mode lost: %s", e.MarginMode)
		}
	}
}

// ── webhook 落库 + 信号式 payload 走执行管线 ──

func TestGenericWebhookSignalPersisted(t *testing.T) {
	r := setupRouter()
	r.POST("/webhook/generic", GenericWebhook)

	body := `{"symbol":"ETHUSDT","side":"BUY","quantity":0.5,"price":2000,"stop_loss":1900,"take_profit":2200,"margin_mode":"hedge","strategy":"sigtest"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook/generic", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}

	// 信号已落库且进入 EXECUTED
	sigs, err := store.NewSignalRepo().List(map[string]any{"symbol": "ETHUSDT", "strategy": "sigtest"}, 5)
	if err != nil || len(sigs) != 1 {
		t.Fatalf("signal not persisted: %v n=%d", err, len(sigs))
	}
	if sigs[0].Status != "EXECUTED" || sigs[0].MarginMode != "hedge" || sigs[0].SourceID == "" {
		t.Fatalf("signal state wrong: %+v", sigs[0])
	}
	// 执行记录已建（阶梯挂接）
	execs, err := store.NewSignalExecutionRepo().List(map[string]any{"signal_id": sigs[0].ID}, 5)
	if err != nil || len(execs) != 1 {
		t.Fatalf("execution not created: %v n=%d", err, len(execs))
	}
	if execs[0].TP3Price != 2200 || execs[0].CurrentSL != 1900 || execs[0].Status != "active" {
		t.Fatalf("execution ladder wrong: %+v", execs[0])
	}
	sigTPSLManager.detach(execs[0].ID)
}

// TestGenericWebhookLegacyPath：无止盈止损字段保持原流程（不落执行记录、信号 EXECUTED）。
func TestGenericWebhookLegacyPath(t *testing.T) {
	r := setupRouter()
	r.POST("/webhook/generic", GenericWebhook)
	body := `{"symbol":"LEGACYUSDT","side":"BUY","quantity":0.1,"price":100}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook/generic", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	sigs, _ := store.NewSignalRepo().List(map[string]any{"symbol": "LEGACYUSDT"}, 5)
	if len(sigs) != 1 || sigs[0].Status != "EXECUTED" {
		t.Fatalf("legacy signal wrong: %+v", sigs)
	}
	execs, _ := store.NewSignalExecutionRepo().List(map[string]any{"signal_id": sigs[0].ID}, 5)
	if len(execs) != 0 {
		t.Fatalf("legacy path must not create execution: %+v", execs)
	}
}

// ── 订阅定价：profit_share 分成累计 ──

func TestProfitShareAccumulation(t *testing.T) {
	repo := store.NewSignalSourceRepo()
	src := &store.SignalSource{
		Name: "分成源", Type: "webhook", Enabled: true,
		FeeModel: "profit_share", FeePercent: 10,
		TPSLJSON: `{"tp1_pct":40,"tp2_pct":30,"tp3_pct":30,"sl_pct":3}`,
	}
	if err := repo.Create(src); err != nil {
		t.Fatal(err)
	}
	sub := &store.SignalSourceSubscription{
		SourceID: src.ID, UserID: 42, FeeModel: "profit_share", FeePercent: 10,
		Status: "active", CreatedAt: time.Now().UnixMilli(),
	}
	if err := repo.CreateSubscription(sub); err != nil {
		t.Fatal(err)
	}

	// 模拟信号平仓盈利 50 → 分成 5 累计到 pending_share
	if err := repo.AddPendingShare(src.ID, 50*0.10); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetSubscription(src.ID, 42)
	if err != nil {
		t.Fatal(err)
	}
	if got.PendingShare < 4.99 || got.PendingShare > 5.01 {
		t.Fatalf("pending_share=%.4f want 5", got.PendingShare)
	}

	// 端到端：finalizeExecution 走 AddPendingShare（源存在且为 profit_share）
	sig := &store.SignalRecord{Symbol: "SHAREUSDT", Direction: "LONG", Strategy: "share", Status: "EXECUTED", SourceID: src.ID}
	if err := store.NewSignalRepo().Create(sig); err != nil {
		t.Fatal(err)
	}
	e := &store.SignalExecution{
		SignalID: sig.ID, SourceID: src.ID, Symbol: "SHAREUSDT", Direction: "LONG",
		Status: "active", EntryPrice: 100, EntryQty: 1, RemainingQty: 0, RealizedPnL: 100,
		CreatedAt: time.Now().UnixMilli(),
	}
	execRepo := store.NewSignalExecutionRepo()
	if err := execRepo.Create(e); err != nil {
		t.Fatal(err)
	}
	finalizeExecution(e, execRepo, "tp3", time.Now().UnixMilli())
	got, _ = repo.GetSubscription(src.ID, 42)
	// 100 * 10% = 10，累计 5 + 10 = 15
	if got.PendingShare < 14.99 || got.PendingShare > 15.01 {
		t.Fatalf("pending_share after finalize=%.4f want 15", got.PendingShare)
	}
}
