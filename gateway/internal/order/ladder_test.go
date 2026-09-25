package order

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// fakeLadderPlacer 模拟交易所：限价单挂出后不自动成交（测试用 fill/fillAll 驱动），
// 市价单按 markPrice 即时成交。
type fakeLadderPlacer struct {
	mu           sync.Mutex
	orders       map[string]*model.OrderData
	seq          int
	markPrice    float64
	marketReject bool
}

func newFakeLadderPlacer() *fakeLadderPlacer {
	return &fakeLadderPlacer{orders: map[string]*model.OrderData{}, markPrice: 100}
}

func (f *fakeLadderPlacer) PlaceOrder(req *Request) (*model.OrderData, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	ord := &model.OrderData{
		ID:        fmt.Sprintf("lord-%d", f.seq),
		Symbol:    req.Symbol,
		Side:      req.Side,
		OrderType: req.OrderType,
		Price:     req.Price,
		Quantity:  req.Quantity,
		Exchange:  req.Exchange,
		UserID:    req.UserID,
		ClientOID: req.ClientOID,
		CreatedAt: time.Now().UnixMilli(),
		UpdatedAt: time.Now().UnixMilli(),
	}
	if req.OrderType == model.TypeMarket {
		if f.marketReject {
			ord.Status = model.StatusRejected
			f.orders[ord.ID] = ord
			return ord, fmt.Errorf("market order rejected")
		}
		ord.Status = model.StatusFilled
		ord.Filled = req.Quantity
		ord.AvgFillPrice = f.markPrice
	} else {
		ord.Status = model.StatusNew
	}
	f.orders[ord.ID] = ord
	return ord, nil
}

func (f *fakeLadderPlacer) CancelOrder(orderID, symbol string) (*model.OrderData, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ord, ok := f.orders[orderID]
	if !ok {
		return nil, fmt.Errorf("order not found")
	}
	if ord.IsDone() {
		return nil, fmt.Errorf("order already %s", ord.Status)
	}
	ord.Status = model.StatusCancelled
	ord.UpdatedAt = time.Now().UnixMilli()
	return ord, nil
}

func (f *fakeLadderPlacer) GetOrder(orderID string) *model.OrderData {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.orders[orderID]
}

// HandleOrderUpdate 模拟 OMS 内存回填接缝（重启恢复用）。
func (f *fakeLadderPlacer) HandleOrderUpdate(o *model.OrderData) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *o
	f.orders[o.ID] = &cp
}

// fill 把订单成交量推进到 qty（qty >= 总量即全成交）。
func (f *fakeLadderPlacer) fill(orderID string, qty, px float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ord, ok := f.orders[orderID]
	if !ok {
		return
	}
	ord.Filled = qty
	ord.AvgFillPrice = px
	if qty >= ord.Quantity {
		ord.Status = model.StatusFilled
	} else {
		ord.Status = model.StatusPartiallyFilled
	}
	ord.UpdatedAt = time.Now().UnixMilli()
}

func (f *fakeLadderPlacer) fillAll(orderID string) {
	f.mu.Lock()
	ord := f.orders[orderID]
	f.mu.Unlock()
	if ord == nil {
		return
	}
	f.fill(orderID, ord.Quantity, ord.Price)
}

func (f *fakeLadderPlacer) orderCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.orders)
}

func (f *fakeLadderPlacer) marketOrders() []*model.OrderData {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*model.OrderData
	for _, o := range f.orders {
		if o.OrderType == model.TypeMarket {
			out = append(out, o)
		}
	}
	return out
}

// ── 测试辅助 ──

// newTestLadder 返回引擎 + fake 交易所 + 可控最新价。
func newTestLadder() (*LadderEngine, *fakeLadderPlacer, *float64) {
	p := newFakeLadderPlacer()
	px := 100.0
	e := NewLadderEngine(p)
	e.SetPriceSource(func(string) float64 { return px })
	return e, p, &px
}

// testLadderSpec 3 档买入（1+1+2=4）+ 2 个目标（50/50），SL=85，保本@T1，追踪 2%。
func testLadderSpec() *LadderSpec {
	s := &LadderSpec{
		Symbol:   "TESTUSDT",
		Side:     model.SideBuy,
		Exchange: "paper",
		StopLoss: 85, BreakevenAfterTarget: 1, TrailingStepPct: 2,
	}
	s.Entries = []struct {
		Price      float64 `json:"price"`
		Qty        float64 `json:"qty,omitempty"`
		AmountUSDT float64 `json:"amount_usdt,omitempty"`
	}{
		{Price: 100, Qty: 1},
		{Price: 95, Qty: 1},
		{Price: 90, Qty: 2},
	}
	s.Targets = []struct {
		Price    float64 `json:"price"`
		ClosePct float64 `json:"close_pct"`
	}{
		{Price: 110, ClosePct: 50},
		{Price: 120, ClosePct: 50},
	}
	return s
}

// cleanupLadderRecord 测试结束把记录写成终态，避免污染其他用例的恢复扫描。
func cleanupLadderRecord(t *testing.T, id string) {
	t.Helper()
	t.Cleanup(func() {
		if store.GetDB() == nil {
			return
		}
		rec, err := store.NewLadderOrderRepo().GetByID(id)
		if err != nil || rec == nil {
			return
		}
		if rec.Status == LadderStatusActive || rec.Status == LadderStatusStopping {
			rec.Status = LadderStatusCompleted
			_ = store.NewLadderOrderRepo().Upsert(rec)
		}
	})
}

// ── 规格校验 ──

func TestLadderSpecValidate(t *testing.T) {
	// close_pct 合计必须 = 100
	bad := testLadderSpec()
	bad.Targets[1].ClosePct = 40
	if _, _, _, err := bad.Validate(); err == nil {
		t.Fatal("close_pct 合计 != 100 必须拒绝")
	}
	// 档数上限 10
	many := testLadderSpec()
	for len(many.Entries) < 11 {
		many.Entries = append(many.Entries, many.Entries[0])
	}
	if _, _, _, err := many.Validate(); err == nil {
		t.Fatal("超过 10 档必须拒绝")
	}
	// amount_usdt 自动换算 qty
	amt := &LadderSpec{Symbol: "X", Side: model.SideBuy}
	amt.Entries = append(amt.Entries, struct {
		Price      float64 `json:"price"`
		Qty        float64 `json:"qty,omitempty"`
		AmountUSDT float64 `json:"amount_usdt,omitempty"`
	}{Price: 50, AmountUSDT: 200})
	amt.Targets = append(amt.Targets, struct {
		Price    float64 `json:"price"`
		ClosePct float64 `json:"close_pct"`
	}{Price: 60, ClosePct: 100})
	entries, _, total, err := amt.Validate()
	if err != nil {
		t.Fatalf("amount_usdt 换算应通过: %v", err)
	}
	if entries[0].Qty != 4 || total != 4 {
		t.Fatalf("200 USDT @50 应换算为 4: %+v total=%v", entries[0], total)
	}
	// 非法参数
	be := testLadderSpec()
	be.BreakevenAfterTarget = 3 // 只有 2 个目标
	if _, _, _, err := be.Validate(); err == nil {
		t.Fatal("breakeven_after_target 超过目标数必须拒绝")
	}
	ts := testLadderSpec()
	ts.TrailingStepPct = 100
	if _, _, _, err := ts.Validate(); err == nil {
		t.Fatal("trailing_step_pct >= 100 必须拒绝")
	}
}

// ── 状态机 ──

// 分档成交：entry 成交激活目标挂单；更多 entry 成交后目标单加量（撤旧换新）。
func TestLadderEntryFillActivatesTargets(t *testing.T) {
	e, p, _ := newTestLadder()
	l, err := e.Create(testLadderSpec(), 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupLadderRecord(t, l.ID)
	if l.Status != LadderStatusActive {
		t.Fatalf("应 active: %s", l.Status)
	}
	// 3 档限价买入全部挂出
	for i, en := range l.Entries {
		if en.OrderID == "" || en.Status != ladderLegOpen {
			t.Fatalf("entry %d 应挂出: %+v", i, en)
		}
	}
	// entry0 成交 → target0 挂出 1（min(50%×4, 已成交1)）@110 SELL
	p.fillAll(l.Entries[0].OrderID)
	e.processLadder(l)
	if l.FilledQty != 1 || l.AvgEntry != 100 {
		t.Fatalf("已成交量/均价错误: %+v", l)
	}
	tg0 := &l.Targets[0]
	if tg0.OrderID == "" || tg0.Status != ladderLegOpen {
		t.Fatalf("target0 应挂出: %+v", tg0)
	}
	ord0 := p.GetOrder(tg0.OrderID)
	if ord0 == nil || ord0.Quantity != 1 || ord0.Side != model.SideSell || ord0.Price != 110 {
		t.Fatalf("target0 止盈单错误: %+v", ord0)
	}
	if l.Targets[1].OrderID != "" {
		t.Fatal("未分配量前 target1 不应挂单")
	}
	// entry1 成交 → target0 加量到 2（撤旧换新）
	oldT0 := tg0.OrderID
	p.fillAll(l.Entries[1].OrderID)
	e.processLadder(l)
	if l.FilledQty != 2 {
		t.Fatalf("FilledQty 应为 2: %v", l.FilledQty)
	}
	if want := (100.0 + 95.0) / 2; l.AvgEntry != want {
		t.Fatalf("加权均价应为 %.2f: %.4f", want, l.AvgEntry)
	}
	if p.GetOrder(oldT0).Status != model.StatusCancelled {
		t.Fatal("加量应撤掉旧止盈单")
	}
	tg0 = &l.Targets[0]
	if tg0.OrderID == "" || tg0.OrderID == oldT0 {
		t.Fatal("加量应挂出新止盈单")
	}
	if q := p.GetOrder(tg0.OrderID).Quantity; q != 2 {
		t.Fatalf("新止盈单应为 2: %v", q)
	}
}

// 部分止盈 + 保本位 + 目标追踪：T1 成交后 SL 棘轮上移。
func TestLadderPartialTakeProfitSLRules(t *testing.T) {
	e, p, px := newTestLadder()
	l, err := e.Create(testLadderSpec(), 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupLadderRecord(t, l.ID)
	// 全部 4 单位入场（均价 93.75）
	for i := range l.Entries {
		p.fillAll(l.Entries[i].OrderID)
	}
	e.processLadder(l)
	if l.FilledQty != 4 {
		t.Fatalf("FilledQty 应为 4: %v", l.FilledQty)
	}
	wantAvg := (100.0 + 95.0 + 90.0*2) / 4
	if l.AvgEntry != wantAvg {
		t.Fatalf("加权均价应为 %.2f: %.4f", wantAvg, l.AvgEntry)
	}
	// 价格走到 111（T1=110 已越过），目标 1 成交 2 → 部分止盈。
	// 注意：T1 成交后 SL 会棘轮到 107.8，市价必须在其上方，否则应立即止损。
	*px = 111
	p.fillAll(l.Targets[0].OrderID)
	e.processLadder(l)
	if l.ClosedQty != 2 {
		t.Fatalf("ClosedQty 应为 2: %v", l.ClosedQty)
	}
	if l.Targets[0].Status != ladderLegFilled {
		t.Fatalf("target0 应 filled: %+v", l.Targets[0])
	}
	// 保本（N=1）+ 追踪 2%：候选 = max(93.75, 110×0.98=107.8) → 107.8
	if l.CurrentSL != 110*0.98 {
		t.Fatalf("T1 成交后 SL 应棘轮到 107.8: %.4f", l.CurrentSL)
	}
	if !l.BreakevenArmed {
		t.Fatal("保本规则应已激活")
	}
	if l.Status != LadderStatusActive {
		t.Fatalf("还有未止盈仓位，不应终结: %s", l.Status)
	}
	// target2 挂出剩余 2 @120
	if l.Targets[1].OrderID == "" {
		t.Fatal("target1 应挂出剩余 2")
	}
	if q := p.GetOrder(l.Targets[1].OrderID).Quantity; q != 2 {
		t.Fatalf("target1 止盈单应为 2: %v", q)
	}
	// 全部止盈 → completed，无残留挂单
	p.fillAll(l.Targets[1].OrderID)
	e.processLadder(l)
	if l.Status != LadderStatusCompleted {
		t.Fatalf("全部止盈应 completed: %s", l.Status)
	}
	if l.ClosedQty != 4 {
		t.Fatalf("ClosedQty 应为 4: %v", l.ClosedQty)
	}
}

// 纯保本规则（不开追踪）：T1 成交后 SL = 加权平均入场价。
func TestLadderBreakevenOnly(t *testing.T) {
	e, p, _ := newTestLadder()
	spec := testLadderSpec()
	spec.TrailingStepPct = 0
	l, err := e.Create(spec, 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupLadderRecord(t, l.ID)
	for i := range l.Entries {
		p.fillAll(l.Entries[i].OrderID)
	}
	e.processLadder(l)
	p.fillAll(l.Targets[0].OrderID)
	e.processLadder(l)
	wantAvg := (100.0 + 95.0 + 180.0) / 4
	if l.CurrentSL != wantAvg {
		t.Fatalf("保本位应为加权均价 %.4f: %.4f", wantAvg, l.CurrentSL)
	}
}

// SL 触发：撤全部挂单 + 市价平剩余 → stopped。
func TestLadderStopLossTrigger(t *testing.T) {
	e, p, px := newTestLadder()
	l, err := e.Create(testLadderSpec(), 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupLadderRecord(t, l.ID)
	// 入场 2（entry0+entry1），止盈未成交
	p.fillAll(l.Entries[0].OrderID)
	p.fillAll(l.Entries[1].OrderID)
	e.processLadder(l)
	openTarget := l.Targets[0].OrderID
	if openTarget == "" {
		t.Fatal("应有止盈挂单")
	}
	// 价格砸到 84 < SL 85 → 触发
	*px = 84
	e.processLadder(l)
	if l.Status != LadderStatusStopped {
		t.Fatalf("SL 触发应 stopped: %s", l.Status)
	}
	if p.GetOrder(openTarget).Status != model.StatusCancelled {
		t.Fatal("SL 应撤掉止盈挂单")
	}
	if p.GetOrder(l.Entries[2].OrderID).Status != model.StatusCancelled {
		t.Fatal("SL 应撤掉未成交入场档")
	}
	mkts := p.marketOrders()
	if len(mkts) != 1 || mkts[0].Quantity != 2 || mkts[0].Side != model.SideSell {
		t.Fatalf("SL 应市价卖出剩余 2: %+v", mkts)
	}
	if l.StopReason != "stop_loss" {
		t.Fatalf("StopReason 应为 stop_loss: %s", l.StopReason)
	}
}

// 一键全平：撤全部挂单 + 市价平剩余 → flattened。
func TestLadderFlatten(t *testing.T) {
	e, p, _ := newTestLadder()
	l, err := e.Create(testLadderSpec(), 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupLadderRecord(t, l.ID)
	p.fillAll(l.Entries[0].OrderID)
	e.processLadder(l)
	if _, err := e.Flatten(l.ID); err != nil {
		t.Fatalf("flatten: %v", err)
	}
	if l.Status != LadderStatusFlattened {
		t.Fatalf("全平应 flattened: %s", l.Status)
	}
	for i := range l.Entries {
		if l.Entries[i].Status != ladderLegFilled && l.Entries[i].Status != ladderLegCancelled {
			t.Fatalf("entry %d 应终结: %+v", i, l.Entries[i])
		}
	}
	mkts := p.marketOrders()
	if len(mkts) != 1 || mkts[0].Quantity != 1 {
		t.Fatalf("全平应市价卖出已成交的 1: %+v", mkts)
	}
	if l.StopReason != "manual_flatten" {
		t.Fatalf("StopReason 应为 manual_flatten: %s", l.StopReason)
	}
	// 终态后重复操作报错
	if _, err := e.Flatten(l.ID); err == nil {
		t.Fatal("终态后重复全平应报错")
	}
}

// 撤销：撤挂单、保留仓位、不发市价单。
func TestLadderCancelKeepsPosition(t *testing.T) {
	e, p, _ := newTestLadder()
	l, err := e.Create(testLadderSpec(), 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupLadderRecord(t, l.ID)
	p.fillAll(l.Entries[0].OrderID)
	e.processLadder(l)
	before := len(p.marketOrders())
	if _, err := e.Cancel(l.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if l.Status != LadderStatusCancelled {
		t.Fatalf("撤销应 cancelled: %s", l.Status)
	}
	if len(p.marketOrders()) != before {
		t.Fatal("撤销不得发市价平仓单（仓位保留）")
	}
	if l.FilledQty != 1 {
		t.Fatalf("已建仓位应保留在账本: %v", l.FilledQty)
	}
}

// 拖动改价：未成交档改价（撤旧挂新）；已成交档拒绝；SL/规则可改。
func TestLadderAmend(t *testing.T) {
	e, p, _ := newTestLadder()
	l, err := e.Create(testLadderSpec(), 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupLadderRecord(t, l.ID)
	// 改未成交 entry0 价格 100 → 99
	am := &LadderAmend{}
	am.Entries = append(am.Entries, struct {
		Index int     `json:"index"`
		Price float64 `json:"price"`
	}{Index: 0, Price: 99})
	oldID := l.Entries[0].OrderID
	if _, err := e.Amend(l.ID, am); err != nil {
		t.Fatalf("amend entry: %v", err)
	}
	if p.GetOrder(oldID).Status != model.StatusCancelled {
		t.Fatal("改价应撤掉旧单")
	}
	if l.Entries[0].Price != 99 || l.Entries[0].OrderID == oldID {
		t.Fatalf("改价后应以新价挂新单: %+v", l.Entries[0])
	}
	// 已成交档拒绝改价
	p.fillAll(l.Entries[0].OrderID)
	e.processLadder(l)
	am2 := &LadderAmend{}
	am2.Entries = append(am2.Entries, struct {
		Index int     `json:"index"`
		Price float64 `json:"price"`
	}{Index: 0, Price: 98})
	if _, err := e.Amend(l.ID, am2); err == nil {
		t.Fatal("已成交档改价必须拒绝")
	}
	// 改 SL 与规则
	sl := 80.0
	be := 2
	am3 := &LadderAmend{StopLoss: &sl, BreakevenAfterTarget: &be}
	if _, err := e.Amend(l.ID, am3); err != nil {
		t.Fatalf("amend sl: %v", err)
	}
	if l.CurrentSL != 80 || l.BreakevenAfterTarget != 2 {
		t.Fatalf("SL/规则未生效: %+v", l)
	}
	// 改未成交 target 价
	am4 := &LadderAmend{}
	am4.Targets = append(am4.Targets, struct {
		Index int     `json:"index"`
		Price float64 `json:"price"`
	}{Index: 1, Price: 125})
	if _, err := e.Amend(l.ID, am4); err != nil {
		t.Fatalf("amend target: %v", err)
	}
	if l.Targets[1].Price != 125 {
		t.Fatalf("目标价未更新: %+v", l.Targets[1])
	}
}

// 重启恢复：状态落库 → 新引擎/新 OMS（内存全空）恢复 → 状态机继续推进。
func TestLadderRestoreFromStore(t *testing.T) {
	e1, p1, _ := newTestLadder()
	l, err := e1.Create(testLadderSpec(), 7)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupLadderRecord(t, l.ID)
	p1.fillAll(l.Entries[0].OrderID)
	e1.processLadder(l)
	if l.Targets[0].OrderID == "" {
		t.Fatal("目标应已挂出")
	}
	// 模拟 OMS 宕机前已把子单持久化到 xt_orders
	persistChild := func(id string) {
		o := p1.GetOrder(id)
		if o == nil {
			return
		}
		if err := store.GetOrderRepo().Upsert(&store.OrderRecord{
			ID: o.ID, Symbol: o.Symbol, Side: string(o.Side), OrderType: string(o.OrderType),
			Price: o.Price, Quantity: o.Quantity, Filled: o.Filled, Status: string(o.Status),
			Exchange: o.Exchange, UserID: 7, ClientOID: o.ClientOID, AvgFillPrice: o.AvgFillPrice,
			CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
		}); err != nil {
			t.Fatalf("persist child %s: %v", id, err)
		}
	}
	for i := range l.Entries {
		if l.Entries[i].OrderID != "" {
			persistChild(l.Entries[i].OrderID)
		}
	}
	persistChild(l.Targets[0].OrderID)

	// ── 第二次"进程"：新引擎 + 新 placer（内存全空）──
	p2 := newFakeLadderPlacer()
	e2 := NewLadderEngine(p2)
	px := 100.0
	e2.SetPriceSource(func(string) float64 { return px })
	n := e2.RestoreFromStore()
	if n < 1 {
		t.Fatalf("应至少恢复 1 个阶梯单, got %d", n)
	}
	rl := e2.Get(l.ID)
	if rl == nil {
		t.Fatalf("恢复的阶梯单应能被 Get: %s", l.ID)
	}
	if rl.Status != LadderStatusActive || rl.FilledQty != 1 {
		t.Fatalf("恢复状态错误: %+v", rl)
	}
	// 子单必须回填新 OMS 内存
	if p2.GetOrder(l.Entries[0].OrderID) == nil {
		t.Fatal("入场子单必须回填 OMS")
	}
	if p2.GetOrder(l.Targets[0].OrderID) == nil {
		t.Fatal("止盈子单必须回填 OMS")
	}
	// 幂等：重复恢复不得重复跟踪
	if n2 := e2.RestoreFromStore(); n2 != 0 {
		t.Fatalf("已跟踪不得重复恢复, got %d", n2)
	}
	// 恢复后状态机继续：止盈单成交 → 部分止盈 + SL 棘轮
	p2.fillAll(l.Targets[0].OrderID)
	e2.processLadder(rl)
	if rl.ClosedQty != 1 {
		t.Fatalf("恢复后止盈成交应累计 ClosedQty=1: %v", rl.ClosedQty)
	}
	if rl.CurrentSL != 110*0.98 {
		t.Fatalf("恢复后 SL 应棘轮到 107.8: %.4f", rl.CurrentSL)
	}
}

// 市价平仓失败 → failed + 告警钩子。
func TestLadderFlattenMarketFailure(t *testing.T) {
	e, p, _ := newTestLadder()
	l, err := e.Create(testLadderSpec(), 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupLadderRecord(t, l.ID)
	p.fillAll(l.Entries[0].OrderID)
	e.processLadder(l)
	var notified string
	e.SetNotifyHook(func(l *LadderOrder, msg string) { notified = msg })
	p.marketReject = true
	if _, err := e.Flatten(l.ID); err != nil {
		t.Fatalf("flatten 调用本身不应报错: %v", err)
	}
	if l.Status != LadderStatusFailed {
		t.Fatalf("平仓失败应 failed: %s", l.Status)
	}
	if notified == "" {
		t.Fatal("平仓失败必须触发告警钩子")
	}
}

// SELL 阶梯（做空/出货）：方向镜像——目标在下方买回，SL 在上方。
func TestLadderSellSide(t *testing.T) {
	e, p, px := newTestLadder()
	spec := &LadderSpec{
		Symbol: "TESTUSDT", Side: model.SideSell, Exchange: "paper",
		StopLoss: 115, BreakevenAfterTarget: 1, TrailingStepPct: 2,
	}
	spec.Entries = append(spec.Entries, struct {
		Price      float64 `json:"price"`
		Qty        float64 `json:"qty,omitempty"`
		AmountUSDT float64 `json:"amount_usdt,omitempty"`
	}{Price: 100, Qty: 2})
	spec.Targets = append(spec.Targets, struct {
		Price    float64 `json:"price"`
		ClosePct float64 `json:"close_pct"`
	}{Price: 90, ClosePct: 100})
	l, err := e.Create(spec, 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupLadderRecord(t, l.ID)
	p.fillAll(l.Entries[0].OrderID)
	e.processLadder(l)
	// 目标是 BUY 限价 @90
	tg := p.GetOrder(l.Targets[0].OrderID)
	if tg == nil || tg.Side != model.SideBuy || tg.Price != 90 {
		t.Fatalf("SELL 阶梯目标应为买回单: %+v", tg)
	}
	p.fillAll(l.Targets[0].OrderID)
	e.processLadder(l)
	// 空头保本：SL 下移到均价 100；追踪候选 90×1.02=91.8 < 100 → 取 91.8
	if l.CurrentSL != 90*1.02 {
		t.Fatalf("SELL 阶梯 SL 应下移到 91.8: %.4f", l.CurrentSL)
	}
	if l.Status != LadderStatusCompleted {
		t.Fatalf("全部止盈应 completed: %s", l.Status)
	}
	// 空头 SL 方向：价格上穿触发
	l2, err := e.Create(spec, 1)
	if err != nil {
		t.Fatalf("create2: %v", err)
	}
	cleanupLadderRecord(t, l2.ID)
	p.fillAll(l2.Entries[0].OrderID)
	e.processLadder(l2)
	*px = 116 // > SL 115
	e.processLadder(l2)
	if l2.Status != LadderStatusStopped {
		t.Fatalf("空头 SL 上穿应 stopped: %s", l2.Status)
	}
	mkts := p.marketOrders()
	last := mkts[len(mkts)-1]
	if last.Side != model.SideBuy {
		t.Fatalf("空头平仓应为市价买回: %+v", last)
	}
}

// 恢复扫描跳过终态单。
func TestLadderRestoreSkipsTerminal(t *testing.T) {
	if store.GetDB() == nil {
		t.Skip("store 未初始化")
	}
	rec := &store.LadderOrderRecord{
		ID: "lad-terminal-skip", UserID: 1, Symbol: "X", Side: "BUY", Exchange: "paper",
		Status: LadderStatusCompleted, SpecJSON: "{}", StateJSON: "{}",
	}
	if err := store.NewLadderOrderRepo().Upsert(rec); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	e := NewLadderEngine(newFakeLadderPlacer())
	if e.Get(rec.ID) != nil {
		t.Fatal("终态单不得被恢复跟踪")
	}
	e.RestoreFromStore()
	if e.Get(rec.ID) != nil {
		t.Fatal("终态单不得被恢复跟踪")
	}
}
