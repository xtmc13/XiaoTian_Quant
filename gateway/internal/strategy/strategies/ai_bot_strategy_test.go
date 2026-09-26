package strategies

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// setupAIBotStrategyDB 初始化临时 sqlite，并返回清理函数。
func setupAIBotStrategyDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-ai-bot-strategy")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	// DefaultAISignalRepo 的 sync.Once 只在首个库上Ensure过，换库后需显式建表。
	if err := store.EnsureAISignalsSchema(); err != nil {
		t.Fatalf("ensure ai_signals schema: %v", err)
	}
	t.Cleanup(store.CloseDB)
}

func insertSignal(t *testing.T, userID int64, symbol, signal string, confidence float64, createdAt int64) *store.AISignalRecord {
	t.Helper()
	rec := &store.AISignalRecord{
		UserID:     userID,
		Symbol:     symbol,
		Signal:     signal,
		Confidence: confidence,
		Reason:     "test signal",
		Mode:       "fast",
		Provider:   "mock",
		CreatedAt:  createdAt,
	}
	if err := store.DefaultAISignalRepo().Insert(rec); err != nil {
		t.Fatalf("insert signal: %v", err)
	}
	return rec
}

func nowMs() int64 { return time.Now().UnixMilli() }

func TestAIBotStrategy_LongSignalOpensLong(t *testing.T) {
	setupAIBotStrategyDB(t)
	s := NewAIBotStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT", "user_id": 7, "confidence_threshold": 60.0}); err != nil {
		t.Fatalf("start: %v", err)
	}
	insertSignal(t, 7, "BTCUSDT", "long", 75, time.Now().Unix())

	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 60000, Time: nowMs()}, nil)
	if err != nil {
		t.Fatalf("onbar: %v", err)
	}
	if sig == nil {
		t.Fatal("应有开仓信号")
	}
	if sig.Direction != "LONG" {
		t.Errorf("direction = %q, want LONG", sig.Direction)
	}
	if sig.Strength != 0.75 {
		t.Errorf("strength = %v, want 0.75", sig.Strength)
	}
	if !s.InPosition() || s.PositionDirection() != "LONG" {
		t.Error("状态应记录持多")
	}
}

func TestAIBotStrategy_ShortSignalOpensShort(t *testing.T) {
	setupAIBotStrategyDB(t)
	s := NewAIBotStrategy()
	if err := s.Start(map[string]any{"symbol": "ETHUSDT", "user_id": 7}); err != nil {
		t.Fatalf("start: %v", err)
	}
	insertSignal(t, 7, "ETHUSDT", "short", 80, time.Now().Unix())

	sig, _ := s.OnBar(model.Bar{Symbol: "ETHUSDT", Close: 3000, Time: nowMs()}, nil)
	if sig == nil || sig.Direction != "SHORT" {
		t.Fatalf("应开空, got %+v", sig)
	}
}

func TestAIBotStrategy_LowConfidenceNoEntry(t *testing.T) {
	setupAIBotStrategyDB(t)
	s := NewAIBotStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT", "user_id": 7}); err != nil {
		t.Fatalf("start: %v", err)
	}
	insertSignal(t, 7, "BTCUSDT", "long", 40, time.Now().Unix()) // 低于默认阈值 60

	sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 60000, Time: nowMs()}, nil)
	if sig != nil {
		t.Errorf("低置信度不应开仓, got %+v", sig)
	}
}

func TestAIBotStrategy_NeutralNoEntry(t *testing.T) {
	setupAIBotStrategyDB(t)
	s := NewAIBotStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT", "user_id": 7}); err != nil {
		t.Fatalf("start: %v", err)
	}
	insertSignal(t, 7, "BTCUSDT", "neutral", 90, time.Now().Unix())

	sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 60000, Time: nowMs()}, nil)
	if sig != nil {
		t.Errorf("neutral 不应开仓, got %+v", sig)
	}
}

func TestAIBotStrategy_StaleSignalIgnored(t *testing.T) {
	setupAIBotStrategyDB(t)
	s := NewAIBotStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT", "user_id": 7, "signal_ttl_seconds": 60}); err != nil {
		t.Fatalf("start: %v", err)
	}
	insertSignal(t, 7, "BTCUSDT", "long", 90, time.Now().Unix()-3600) // 1 小时前，TTL 60s

	sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 60000, Time: nowMs()}, nil)
	if sig != nil {
		t.Errorf("过期信号不应开仓, got %+v", sig)
	}
}

func TestAIBotStrategy_ReverseSignalCloses(t *testing.T) {
	setupAIBotStrategyDB(t)
	s := NewAIBotStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT", "user_id": 7}); err != nil {
		t.Fatalf("start: %v", err)
	}
	rec := insertSignal(t, 7, "BTCUSDT", "long", 80, time.Now().Unix())
	sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 60000, Time: nowMs()}, nil)
	if sig == nil || sig.Direction != "LONG" {
		t.Fatalf("应先开多, got %+v", sig)
	}

	// 新信号反向且更新（新 id、更晚时间戳）
	rec2 := &store.AISignalRecord{
		UserID: 7, Symbol: "BTCUSDT", Signal: "short", Confidence: 85,
		Reason: "reversal", CreatedAt: time.Now().Unix() + 2,
	}
	rec2.ID = rec.ID + "-b"
	if err := store.DefaultAISignalRepo().Insert(rec2); err != nil {
		t.Fatalf("insert: %v", err)
	}

	sig2, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 61000, Time: nowMs()}, nil)
	if sig2 == nil || sig2.Direction != "CLOSE" {
		t.Fatalf("反向信号应平仓, got %+v", sig2)
	}
	if s.InPosition() {
		t.Error("平仓后不应再持仓")
	}
}

func TestAIBotStrategy_ThresholdDropCloses(t *testing.T) {
	setupAIBotStrategyDB(t)
	s := NewAIBotStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT", "user_id": 7}); err != nil {
		t.Fatalf("start: %v", err)
	}
	insertSignal(t, 7, "BTCUSDT", "long", 80, time.Now().Unix())
	sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 60000, Time: nowMs()}, nil)
	if sig == nil || sig.Direction != "LONG" {
		t.Fatalf("应先开多, got %+v", sig)
	}

	// 同向但 confidence 跌破阈值
	insertSignal(t, 7, "BTCUSDT", "long", 30, time.Now().Unix()+2)
	sig2, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 60500, Time: nowMs()}, nil)
	if sig2 == nil || sig2.Direction != "CLOSE" {
		t.Fatalf("跌破阈值应平仓, got %+v", sig2)
	}
}

func TestAIBotStrategy_SameSignalNoDuplicateEntry(t *testing.T) {
	setupAIBotStrategyDB(t)
	s := NewAIBotStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT", "user_id": 7}); err != nil {
		t.Fatalf("start: %v", err)
	}
	rec := insertSignal(t, 7, "BTCUSDT", "long", 80, time.Now().Unix())
	sig1, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 60000, Time: nowMs()}, nil)
	if sig1 == nil {
		t.Fatal("应开仓")
	}
	// 平仓后再来同一信号 → 不重复开仓（lastSignalID 防护）
	s.inPosition = false
	s.direction = ""
	sig2, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 60100, Time: nowMs()}, nil)
	if sig2 != nil {
		t.Errorf("同一信号不应重复开仓, got %+v (signalID=%s)", sig2, rec.ID)
	}
}

func TestAIBotStrategy_OtherUserSignalIsolated(t *testing.T) {
	setupAIBotStrategyDB(t)
	s := NewAIBotStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT", "user_id": 7}); err != nil {
		t.Fatalf("start: %v", err)
	}
	// 只有别的用户的信号 → 不开仓
	insertSignal(t, 99, "BTCUSDT", "long", 90, time.Now().Unix())
	sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 60000, Time: nowMs()}, nil)
	if sig != nil {
		t.Errorf("不应消费他人信号, got %+v", sig)
	}
}

func TestAIBotStrategy_NotRunningNoSignal(t *testing.T) {
	setupAIBotStrategyDB(t)
	s := NewAIBotStrategy()
	insertSignal(t, 0, "BTCUSDT", "long", 90, time.Now().Unix())
	sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 60000, Time: nowMs()}, nil)
	if sig != nil {
		t.Errorf("未启动不应出信号, got %+v", sig)
	}
}

func TestAIBotStrategy_Lifecycle(t *testing.T) {
	setupAIBotStrategyDB(t)
	s := NewAIBotStrategy()
	if s.IsRunning() {
		t.Error("初始不应 running")
	}
	if err := s.Start(map[string]any{"symbol": "SOLUSDT", "user_id": 3, "confidence_threshold": 55.0}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !s.IsRunning() {
		t.Error("启动后应 running")
	}
	if s.Symbol() != "SOLUSDT" {
		t.Errorf("symbol = %q", s.Symbol())
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if s.IsRunning() {
		t.Error("停止后不应 running")
	}
	// 参数注册表完整性
	defs := s.ParamDefs()
	if len(defs) == 0 {
		t.Error("应有参数定义（前端配置依赖）")
	}
}
