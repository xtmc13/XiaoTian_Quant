package alerts

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Mocks ──

type markCall struct {
	id    string
	value float64
	atMs  int64
}

type fakeStore struct {
	byID   map[string]*store.IndicatorAlertRecord
	active []*store.IndicatorAlertRecord
	marked []markCall
}

func (f *fakeStore) ListActive() ([]*store.IndicatorAlertRecord, error) { return f.active, nil }
func (f *fakeStore) GetByID(id string) (*store.IndicatorAlertRecord, error) {
	return f.byID[id], nil
}
func (f *fakeStore) MarkTriggered(id string, value float64, atMs int64) error {
	f.marked = append(f.marked, markCall{id, value, atMs})
	rec := f.byID[id]
	if rec != nil {
		rec.LastTriggeredAt = atMs
		rec.LastValue = value
	}
	return nil
}

type klineKey struct{ symbol, interval string }

type fakeKlines struct {
	bars  map[klineKey][]model.Bar
	calls int
	err   error
}

func (f *fakeKlines) RecentBars(symbol, interval string, limit int) ([]model.Bar, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.bars[klineKey{symbol, interval}], nil
}

type fakeNotifier struct{ msgs []notify.Message }

func (f *fakeNotifier) Send(msg notify.Message) { f.msgs = append(f.msgs, msg) }

// ── Fixtures ──

// bullBars 单边上涨：rsi14=100，close 严格递增。
func bullBars(n int) []model.Bar {
	bars := make([]model.Bar, n)
	for i := 0; i < n; i++ {
		c := 100 + float64(i)
		bars[i] = model.Bar{Symbol: "BTCUSDT", Interval: "1h", Open: c, High: c + 1, Low: c - 1, Close: c, Volume: 10}
	}
	return bars
}

func newAlert(id, expr string, cooldownMin int) *store.IndicatorAlertRecord {
	return &store.IndicatorAlertRecord{
		ID:              id,
		UserID:          1,
		Name:            "test-" + id,
		Symbol:          "BTCUSDT",
		Interval:        "1h",
		ConditionExpr:   expr,
		CooldownMinutes: cooldownMin,
		Active:          true,
	}
}

func newTestService(st *fakeStore, kl *fakeKlines, nt *fakeNotifier) *Service {
	return NewService(st, kl, nt, time.Second, 200)
}

// ── 冷却逻辑 ──

func TestCooldownPassed(t *testing.T) {
	now := time.UnixMilli(1_000_000_000)
	base := now.UnixMilli() - 30*60_000 // 30 分钟前
	cases := []struct {
		last int64
		cool int
		want bool
	}{
		{0, 60, true},               // 从未触发
		{base, 60, false},           // 30 分钟 < 60 分钟冷却中
		{base, 30, true},            // 恰好等于冷却 → 通过
		{base, 15, true},            // 超过冷却 → 通过
		{base, 0, true},             // 冷却关闭
		{-5, 60, true},              // 异常旧值视为未触发
		{now.UnixMilli(), 1, false}, // 刚触发
	}
	for i, tc := range cases {
		if got := cooldownPassed(tc.last, tc.cool, now); got != tc.want {
			t.Errorf("case %d: cooldownPassed(%d, %d) = %v, want %v", i, tc.last, tc.cool, got, tc.want)
		}
	}
}

// ── 扫描引擎 ──

func TestScanTriggerAndCooldown(t *testing.T) {
	a := newAlert("a1", "rsi14 > 80", 60) // 上涨行情命中
	st := &fakeStore{byID: map[string]*store.IndicatorAlertRecord{"a1": a}, active: []*store.IndicatorAlertRecord{a}}
	kl := &fakeKlines{bars: map[klineKey][]model.Bar{{"BTCUSDT", "1h"}: bullBars(100)}}
	nt := &fakeNotifier{}
	svc := newTestService(st, kl, nt)

	t0 := time.UnixMilli(1_700_000_000_000)
	svc.scanBatch(st.active, t0, true)

	if len(nt.msgs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(nt.msgs))
	}
	msg := nt.msgs[0]
	if msg.Title != "test-a1" {
		t.Errorf("title = %q, want task name", msg.Title)
	}
	if !strings.Contains(msg.Content, "BTCUSDT") || !strings.Contains(msg.Content, "rsi14 > 80") {
		t.Errorf("content missing symbol/expr: %q", msg.Content)
	}
	if msg.Tags["source"] != "indicator_alert" || msg.Tags["symbol"] != "BTCUSDT" || msg.Tags["alert_id"] != "a1" {
		t.Errorf("tags wrong: %v", msg.Tags)
	}
	if len(st.marked) != 1 || st.marked[0].value != 100 {
		t.Errorf("mark = %+v, want value 100", st.marked)
	}

	// 第二轮（冷却内）：命中但不触发。
	svc.scanBatch(st.active, t0.Add(10*time.Minute), true)
	if len(nt.msgs) != 1 {
		t.Errorf("cooldown violated: %d notifications", len(nt.msgs))
	}
	if len(st.marked) != 1 {
		t.Errorf("cooldown violated: %d marks", len(st.marked))
	}

	// 第三轮（冷却过了）：再次触发。
	svc.scanBatch(st.active, t0.Add(61*time.Minute), true)
	if len(nt.msgs) != 2 || len(st.marked) != 2 {
		t.Errorf("expected re-trigger after cooldown: msgs=%d marks=%d", len(nt.msgs), len(st.marked))
	}
}

func TestScanBatchesKlineFetch(t *testing.T) {
	// 同 symbol+interval 的 3 个任务共享一次 K 线拉取。
	recs := []*store.IndicatorAlertRecord{
		newAlert("a", "close > 0", 60),
		newAlert("b", "close > 0", 60),
		newAlert("c", "close > 0", 60),
	}
	st := &fakeStore{active: recs}
	kl := &fakeKlines{bars: map[klineKey][]model.Bar{{"BTCUSDT", "1h"}: bullBars(50)}}
	nt := &fakeNotifier{}
	svc := newTestService(st, kl, nt)

	svc.scanBatch(recs, time.Now(), true)
	if kl.calls != 1 {
		t.Errorf("kline fetch calls = %d, want 1 (batched)", kl.calls)
	}
	if len(nt.msgs) != 3 {
		t.Errorf("notifications = %d, want 3", len(nt.msgs))
	}
}

func TestScanPerTaskFailureIsolation(t *testing.T) {
	bad := newAlert("bad", "nosuchfn < 30", 60)
	noData := newAlert("nodata", "rsi14 < 30", 60)
	noData.Symbol = "EMPTYUSDT"
	good := newAlert("good", "rsi14 > 80", 60)
	recs := []*store.IndicatorAlertRecord{bad, noData, good}
	st := &fakeStore{active: recs}
	kl := &fakeKlines{bars: map[klineKey][]model.Bar{
		{"BTCUSDT", "1h"}: bullBars(100),
		// EMPTYUSDT 无数据 → RecentBars 返回 nil
	}}
	nt := &fakeNotifier{}
	svc := newTestService(st, kl, nt)

	svc.scanBatch(recs, time.Now(), true) // 不得 panic
	if len(nt.msgs) != 1 || nt.msgs[0].Tags["alert_id"] != "good" {
		t.Errorf("only good alert should notify, got %v", nt.msgs)
	}
}

func TestScanPanicIsolation(t *testing.T) {
	panicky := newAlert("p", "close > 0", 60)
	good := newAlert("g", "close > 0", 60)
	st := &fakeStore{active: []*store.IndicatorAlertRecord{panicky, good}}
	kl := &fakeKlines{bars: map[klineKey][]model.Bar{{"BTCUSDT", "1h"}: bullBars(10)}}
	nt := &fakeNotifier{}
	svc := newTestService(st, kl, nt)
	svc.runOne(panicky, nil, time.Now(), true) // nil bars → recover 兜住
	svc.scanBatch(st.active, time.Now(), true)
	if len(nt.msgs) != 2 {
		t.Errorf("good alerts should still notify after peer panic, got %d", len(nt.msgs))
	}
}

func TestScanFetchFailureSkipsGroup(t *testing.T) {
	st := &fakeStore{active: []*store.IndicatorAlertRecord{newAlert("a", "close > 0", 60)}}
	kl := &fakeKlines{err: errors.New("network down")}
	nt := &fakeNotifier{}
	svc := newTestService(st, kl, nt)
	svc.scanBatch(st.active, time.Now(), true)
	if len(nt.msgs) != 0 || len(st.marked) != 0 {
		t.Error("fetch failure must not notify or mark")
	}
}

// ── RunAlert（手动立即执行） ──

func TestRunAlertForceBypassesCooldown(t *testing.T) {
	a := newAlert("a1", "rsi14 > 80", 60)
	a.LastTriggeredAt = time.Now().UnixMilli() // 刚触发过
	st := &fakeStore{byID: map[string]*store.IndicatorAlertRecord{"a1": a}}
	kl := &fakeKlines{bars: map[klineKey][]model.Bar{{"BTCUSDT", "1h"}: bullBars(100)}}
	nt := &fakeNotifier{}
	svc := newTestService(st, kl, nt)

	// respectCooldown=true → 命中但冷却中，不通知。
	res, err := svc.RunAlert("a1", true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Matched || res.Triggered {
		t.Errorf("expected matched-but-cooldown, got %+v", res)
	}
	if res.CooldownRemainingSec <= 0 {
		t.Errorf("cooldown remaining should be positive, got %d", res.CooldownRemainingSec)
	}

	// force → 触发。
	res, err = svc.RunAlert("a1", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Matched || !res.Triggered {
		t.Errorf("force run should trigger, got %+v", res)
	}
	if len(nt.msgs) != 1 {
		t.Errorf("notifications = %d, want 1", len(nt.msgs))
	}
}

func TestRunAlertEvalErrorReported(t *testing.T) {
	a := newAlert("bad", "nosuchfn < 30", 60)
	st := &fakeStore{byID: map[string]*store.IndicatorAlertRecord{"bad": a}}
	kl := &fakeKlines{bars: map[klineKey][]model.Bar{{"BTCUSDT", "1h"}: bullBars(100)}}
	nt := &fakeNotifier{}
	svc := newTestService(st, kl, nt)
	res, err := svc.RunAlert("bad", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Error == "" || res.Matched || res.Triggered {
		t.Errorf("expected eval error result, got %+v", res)
	}
	if len(nt.msgs) != 0 {
		t.Error("eval error must not notify")
	}
}

func TestRunAlertNotFound(t *testing.T) {
	svc := newTestService(&fakeStore{byID: map[string]*store.IndicatorAlertRecord{}}, &fakeKlines{}, &fakeNotifier{})
	if _, err := svc.RunAlert("missing", false); err == nil {
		t.Error("expected not-found error")
	}
}

// ── 触发历史环形 ──

func TestHistoryRing(t *testing.T) {
	st := &fakeStore{active: []*store.IndicatorAlertRecord{newAlert("a", "close > 0", 0)}}
	kl := &fakeKlines{bars: map[klineKey][]model.Bar{{"BTCUSDT", "1h"}: bullBars(10)}}
	nt := &fakeNotifier{}
	svc := newTestService(st, kl, nt)

	base := time.UnixMilli(1_700_000_000_000)
	// 触发 60 次（cooldown=0 不冷却），环形上限 50。
	for i := 0; i < 60; i++ {
		svc.scanBatch(st.active, base.Add(time.Duration(i)*time.Minute), true)
	}
	h := svc.History("a", 0)
	if len(h) != maxHistoryPerAlert {
		t.Fatalf("history len = %d, want %d", len(h), maxHistoryPerAlert)
	}
	// 最新在前。
	if h[0].At <= h[1].At {
		t.Errorf("history not newest-first: %v", h[:2])
	}
	if h[0].Expr != "close > 0" || h[0].Symbol != "BTCUSDT" {
		t.Errorf("record fields wrong: %+v", h[0])
	}
	if got := svc.History("a", 5); len(got) != 5 {
		t.Errorf("limit=5 returned %d", len(got))
	}
	if got := svc.History("nobody", 0); len(got) != 0 {
		t.Errorf("unknown alert history = %d, want 0", len(got))
	}
}

// ── 生命周期 ──

func TestStartStop(t *testing.T) {
	st := &fakeStore{active: []*store.IndicatorAlertRecord{newAlert("a", "close > 0", 60)}}
	kl := &fakeKlines{bars: map[klineKey][]model.Bar{{"BTCUSDT", "1h"}: bullBars(10)}}
	nt := &fakeNotifier{}
	svc := newTestService(st, kl, nt)
	svc.Start()
	if !svc.IsRunning() {
		t.Error("should be running after Start")
	}
	svc.Start() // 幂等
	svc.Stop()
	if svc.IsRunning() {
		t.Error("should be stopped after Stop")
	}
	svc.Stop() // 幂等
	if kl.calls == 0 {
		t.Error("loop should have run at least one round")
	}
}

func TestFormatValue(t *testing.T) {
	cases := map[float64]string{
		100:        "100",
		27.3456789: "27.345679",
		0.5:        "0.5",
		0:          "0",
	}
	for in, want := range cases {
		if got := formatValue(in); got != want {
			t.Errorf("formatValue(%v) = %q, want %q", in, got, want)
		}
	}
}
