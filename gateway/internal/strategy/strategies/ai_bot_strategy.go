package strategies

import (
	"fmt"
	"log"
	"math"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── AIBotStrategy ───────────────────────────────────────────────
// AI 机器人真策略：消费 xt_ai_signals 表中【当前用户】的最新 AI 决策信号
//（由 AI 决策引擎 / 扫描 worker 落库），信号驱动开平仓：
//   - 无仓位：最新信号 long/short 且 confidence ≥ 阈值 → 开多/开空
//   - 有仓位：信号反向、confidence 跌破阈值、或信号过期 → 平仓
// 替代原 ai_alpha（EMA cross 套壳）假实现。

type AIBotStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	userID  int64
	running bool
	mu      sync.RWMutex

	// 参数（策略配置 / config_json 可读）
	confidenceThreshold float64 // 开仓置信度阈值，0-100
	signalTTLSeconds    int64   // 信号有效期（秒），过期不再驱动交易

	// 状态
	inPosition   bool
	direction    string // LONG / SHORT
	lastSignalID string // 已消费的信号 id，防同信号重复开仓

	repo   *store.AISignalRepo
	params *strategy.ParamRegistry
}

// NewAIBotStrategy 构造 AI 机器人策略（ai_alpha / ai_alpha_futures 共用）。
func NewAIBotStrategy() *AIBotStrategy {
	s := &AIBotStrategy{
		name:                "ai_bot",
		symbol:              "BTCUSDT",
		confidenceThreshold: 60,
		signalTTLSeconds:    1800,
		repo:                store.DefaultAISignalRepo(),
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.CategoricalParameter("symbol", "BTCUSDT",
		[]string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "XRPUSDT", "DOGEUSDT"}, "buy"))
	s.params.Register(strategy.FloatParameter("confidence_threshold", 60, 1, 100, 1, "buy"))
	s.params.Register(strategy.IntParameter("signal_ttl_seconds", 1800, 60, 86400, "buy"))
	s.params.Register(strategy.IntParameter("user_id", 0, 0, math.MaxInt32, "buy"))
	return s
}

func (s *AIBotStrategy) Name() string   { return s.name }
func (s *AIBotStrategy) Symbol() string { return s.symbol }

func (s *AIBotStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":               s.symbol,
		"user_id":              s.userID,
		"confidence_threshold": s.confidenceThreshold,
		"signal_ttl_seconds":   s.signalTTLSeconds,
	}
}

func (s *AIBotStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *AIBotStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if v, ok := params["symbol"].(string); ok && v != "" {
		s.symbol = v
		if p := s.params.Get("symbol"); p != nil {
			// symbol 不在预置可选项时动态加入，保证 FromMap 校验通过
			found := false
			for _, o := range p.Options {
				if o == v {
					found = true
					break
				}
			}
			if !found {
				p.Options = append(p.Options, v)
			}
			p.Default = v
		}
	}
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("ai_bot strategy apply params: %w", err)
	}
	if p := s.params.Get("confidence_threshold"); p != nil {
		s.confidenceThreshold = p.GetFloat()
	}
	if p := s.params.Get("signal_ttl_seconds"); p != nil {
		s.signalTTLSeconds = int64(p.GetInt())
	}
	if p := s.params.Get("user_id"); p != nil {
		s.userID = int64(p.GetInt())
	}

	s.running = true
	s.inPosition = false
	s.direction = ""
	s.lastSignalID = ""
	log.Printf("[ai_bot_strategy] started: user=%d symbol=%s threshold=%.0f ttl=%ds",
		s.userID, s.symbol, s.confidenceThreshold, s.signalTTLSeconds)
	return nil
}

// ── Parameter system implementation ────────────────────────────

func (s *AIBotStrategy) GetParameters() *strategy.ParamRegistry { return s.params }

func (s *AIBotStrategy) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}

func (s *AIBotStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	if err := s.params.FromMap(m); err != nil {
		return err
	}
	if p := s.params.Get("confidence_threshold"); p != nil {
		s.confidenceThreshold = p.GetFloat()
	}
	if p := s.params.Get("signal_ttl_seconds"); p != nil {
		s.signalTTLSeconds = int64(p.GetInt())
	}
	if p := s.params.Get("user_id"); p != nil {
		s.userID = int64(p.GetInt())
	}
	return nil
}

func (s *AIBotStrategy) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

func (s *AIBotStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	s.direction = ""
	log.Printf("[ai_bot_strategy] %s stopped", s.name)
	return nil
}

func (s *AIBotStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	return s.consumeSignal(tick.Symbol, tick.Last, tick.Timestamp), nil
}

func (s *AIBotStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *AIBotStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *AIBotStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	return s.consumeSignal(bar.Symbol, bar.Close, bar.Time), nil
}

// ── 信号消费核心 ────────────────────────────────────────────────

// consumeSignal 按最新 AI 决策信号产出开/平仓信号。
// nowMs 用于信号过期判断（tick/bar 时间，毫秒）。
func (s *AIBotStrategy) consumeSignal(symbol string, price float64, nowMs int64) *model.Signal {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}
	if symbol != "" && symbol != s.symbol {
		return nil
	}

	sig, err := s.repo.LatestByUserSymbol(s.userID, s.symbol)
	if err != nil || sig == nil {
		return nil // 无信号不动作
	}
	// 信号过期：引擎扫描周期之外的老信号不再驱动交易。
	if s.signalTTLSeconds > 0 && nowMs > 0 {
		ageSec := nowMs/1000 - sig.CreatedAt
		if ageSec < 0 {
			ageSec = -ageSec
		}
		if ageSec > s.signalTTLSeconds {
			return nil
		}
	}

	// 已有仓位：反向信号 / 置信度跌破阈值 → 平仓。
	if s.inPosition {
		reversed := (s.direction == "LONG" && sig.Signal == "short") ||
			(s.direction == "SHORT" && sig.Signal == "long")
		thresholdDrop := sig.Signal != "neutral" && sig.Confidence < s.confidenceThreshold
		if reversed || thresholdDrop {
			dir := s.direction
			s.inPosition = false
			s.direction = ""
			reason := fmt.Sprintf("AI signal exit: %s (signal=%s conf=%.0f)",
				map[bool]string{true: "reversed", false: "below_threshold"}[reversed],
				sig.Signal, sig.Confidence)
			return &model.Signal{
				Symbol:    s.symbol,
				Direction: "CLOSE",
				Strength:  math.Min(sig.Confidence/100, 1),
				Strategy:  s.name,
				Reason:    reason + " | " + dir,
			}
		}
		return nil
	}

	// 无仓位：同信号不重复开仓；confidence ≥ 阈值且方向明确才开仓。
	if sig.ID == s.lastSignalID {
		return nil
	}
	if sig.Confidence < s.confidenceThreshold {
		return nil
	}
	switch sig.Signal {
	case "long":
		s.inPosition = true
		s.direction = "LONG"
		s.lastSignalID = sig.ID
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "LONG",
			Strength:  math.Min(sig.Confidence/100, 1),
			Strategy:  s.name,
			Reason:    fmt.Sprintf("AI long signal conf=%.0f: %s", sig.Confidence, truncateStr(sig.Reason, 120)),
		}
	case "short":
		s.inPosition = true
		s.direction = "SHORT"
		s.lastSignalID = sig.ID
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "SHORT",
			Strength:  math.Min(sig.Confidence/100, 1),
			Strategy:  s.name,
			Reason:    fmt.Sprintf("AI short signal conf=%.0f: %s", sig.Confidence, truncateStr(sig.Reason, 120)),
		}
	}
	return nil
}

// truncateStr 截断长字符串。
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// InPosition / PositionDirection 暴露运行状态（测试与诊断用）。
func (s *AIBotStrategy) InPosition() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inPosition
}

func (s *AIBotStrategy) PositionDirection() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.direction
}

// Ensure AIBotStrategy 满足接口（编译期校验）。
var _ strategy.Strategy = (*AIBotStrategy)(nil)
