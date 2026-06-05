package strategy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
)

// Indicator types supported by the compiler.
const (
	IndEMA   = "EMA"
	IndSMA   = "SMA"
	IndRSI   = "RSI"
	IndMACD  = "MACD"
	IndBoll  = "BOLL"
	IndATR   = "ATR"
	IndKDJ   = "KDJ"
	IndVWAP  = "VWAP"
	IndOBV   = "OBV"
)

// Operator types for condition matching.
const (
	OpCrossUp     = "cross_up"      // fast crosses above slow
	OpCrossDown   = "cross_down"    // fast crosses below slow
	OpAbove       = "above"         // value > threshold
	OpBelow       = "below"         // value < threshold
	OpGoldenCross = "golden_cross"  // MA golden cross
	OpDeathCross  = "death_cross"   // MA death cross
	OpRSIOversold = "rsi_oversold"  // RSI < 30
	OpRSIOverbought = "rsi_overbought" // RSI > 70
	OpBollLower   = "boll_lower"    // price touches lower band
	OpBollUpper   = "boll_upper"    // price touches upper band
	OpMACDBullish = "macd_bullish"  // MACD > signal
	OpMACDBearish = "macd_bearish"  // MACD < signal
)

// StrategyConfig is the JSON-serializable strategy definition.
type StrategyConfig struct {
	Name        string            `json:"name"`
	Symbol      string            `json:"symbol"`
	Interval    string            `json:"interval"`
	Indicators  []IndicatorDef    `json:"indicators"`
	EntryRules  []RuleDef         `json:"entry_rules"`
	ExitRules   []RuleDef         `json:"exit_rules"`
	RiskMgmt    RiskMgmtDef       `json:"risk_mgmt"`
	MaxPositions int              `json:"max_positions"`
	Enabled     bool              `json:"enabled"`
}

type IndicatorDef struct {
	Type   string         `json:"type"`
	Name   string         `json:"name"`
	Params map[string]int `json:"params"`
}

type RuleDef struct {
	ID       string `json:"id"`
	Operator string `json:"operator"`
	Left     string `json:"left"`
	Right    string `json:"right"`  // threshold or second indicator
	Action   string `json:"action"` // BUY, SELL, CLOSE
}

type RiskMgmtDef struct {
	StopLossPct   float64 `json:"stop_loss_pct"`
	TakeProfitPct float64 `json:"take_profit_pct"`
	TrailingStop  bool    `json:"trailing_stop"`
	MaxDrawdown   float64 `json:"max_drawdown"`
	PositionSize  float64 `json:"position_size"`
	Pyramiding    bool    `json:"pyramiding"`
	PyramidingN   int     `json:"pyramiding_n"`
}

// CompiledStrategy is the output of compilation.
type CompiledStrategy struct {
	Config     StrategyConfig
	SourceCode string
	Warnings   []string
}

// Compile validates and generates code for a strategy config.
func Compile(cfg StrategyConfig) (*CompiledStrategy, error) {
	cs := &CompiledStrategy{Config: cfg}

	if cfg.Name == "" {
		return nil, fmt.Errorf("strategy name is required")
	}
	if cfg.Symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}

	// Validate indicators
	for _, ind := range cfg.Indicators {
		switch ind.Type {
		case IndEMA, IndSMA, IndRSI, IndMACD, IndBoll, IndATR, IndKDJ, IndVWAP, IndOBV:
			// valid
		default:
			cs.Warnings = append(cs.Warnings, fmt.Sprintf("unknown indicator type: %s", ind.Type))
		}
	}

	// Validate rules
	for _, rule := range cfg.EntryRules {
		if err := validateOperator(rule.Operator); err != nil {
			return nil, fmt.Errorf("entry rule %s: %w", rule.ID, err)
		}
	}
	for _, rule := range cfg.ExitRules {
		if err := validateOperator(rule.Operator); err != nil {
			return nil, fmt.Errorf("exit rule %s: %w", rule.ID, err)
		}
	}

	// Generate source code
	code, err := generateCode(cfg)
	if err != nil {
		return nil, fmt.Errorf("code generation: %w", err)
	}
	cs.SourceCode = code
	return cs, nil
}

func validateOperator(op string) error {
	switch op {
	case OpCrossUp, OpCrossDown, OpAbove, OpBelow,
		OpGoldenCross, OpDeathCross, OpRSIOversold, OpRSIOverbought,
		OpBollLower, OpBollUpper, OpMACDBullish, OpMACDBearish:
		return nil
	}
	return fmt.Errorf("unknown operator: %s", op)
}

var strategyTemplate = template.Must(template.New("strategy").Funcs(template.FuncMap{
	"lower": strings.ToLower,
	"upper": strings.ToUpper,
	"join":  func(arr []string) string { return strings.Join(arr, ", ") },
}).Parse(`package strategies

import (
	"fmt"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// {{.Name}} implements the {{.Name}} strategy.
type {{.Name}} struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	// State
	bars       []model.Bar
	signals    []model.Signal
	position   *model.PositionData
	barCount   int

	// Parameters
	stopLossPct   float64
	takeProfitPct float64
	trailingStop  bool
	maxDrawdown   float64
	positionSize  float64
	pyramiding    bool
	pyramidingN   int
	pyramidingCnt int

	// Indicators state (computed by the engine at runtime)
	indicators map[string]float64

	// Parameter registry
	params *strategy.ParamRegistry
}

func New{{.Name}}() *{{.Name}} {
	s := &{{.Name}}{
		name:    "{{.Name}}",
		symbol:  "{{.Symbol}}",
		indicators: make(map[string]float64),
		{{if .RiskMgmt.StopLossPct}}stopLossPct: {{.RiskMgmt.StopLossPct}},{{end}}
		{{if .RiskMgmt.TakeProfitPct}}takeProfitPct: {{.RiskMgmt.TakeProfitPct}},{{end}}
		{{if .RiskMgmt.TrailingStop}}trailingStop: true,{{end}}
		{{if .RiskMgmt.MaxDrawdown}}maxDrawdown: {{.RiskMgmt.MaxDrawdown}},{{end}}
		{{if .RiskMgmt.PositionSize}}positionSize: {{.RiskMgmt.PositionSize}},{{end}}
		{{if .RiskMgmt.Pyramiding}}pyramiding: true,{{end}}
		{{if .RiskMgmt.PyramidingN}}pyramidingN: {{.RiskMgmt.PyramidingN}},{{end}}
	}
	// Register parameters for hyperopt and frontend configuration
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.FloatParameter("stop_loss_pct", {{.RiskMgmt.StopLossPct}}, 0.005, 0.20, 0.005, "stoploss"))
	s.params.Register(strategy.FloatParameter("take_profit_pct", {{.RiskMgmt.TakeProfitPct}}, 0.01, 0.50, 0.01, "roi"))
	s.params.Register(strategy.BoolParameter("trailing_stop", {{.RiskMgmt.TrailingStop}}, "trailing"))
	s.params.Register(strategy.FloatParameter("max_drawdown", {{.RiskMgmt.MaxDrawdown}}, 0.05, 0.50, 0.05, "protection"))
	s.params.Register(strategy.FloatParameter("position_size", {{.RiskMgmt.PositionSize}}, 100, 10000, 100, "buy"))
	s.params.Register(strategy.BoolParameter("pyramiding", {{.RiskMgmt.Pyramiding}}, "buy"))
	s.params.Register(strategy.IntParameter("pyramiding_n", {{.RiskMgmt.PyramidingN}}, 1, 10, "buy"))
	return s
}

func (s *{{.Name}}) Name() string       { return s.name }
func (s *{{.Name}}) Symbol() string     { return s.symbol }
func (s *{{.Name}}) Params() map[string]any {
	return map[string]any{
		"symbol": s.symbol,
		"stop_loss_pct": s.stopLossPct,
		"take_profit_pct": s.takeProfitPct,
		"trailing_stop": s.trailingStop,
		"max_drawdown": s.maxDrawdown,
		"position_size": s.positionSize,
	}
}

func (s *{{.Name}}) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *{{.Name}}) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Apply parameter values from the incoming map
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("{{.Name}} strategy apply params: %w", err)
	}

	s.running = true
	s.bars = nil
	s.barCount = 0
	s.pyramidingCnt = 0
	return nil
}

func (s *{{.Name}}) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	return nil
}

// ── Parameter system implementation ────────────────────────────

func (s *{{.Name}}) GetParameters() *strategy.ParamRegistry {
	return s.params
}

func (s *{{.Name}}) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}

func (s *{{.Name}}) ApplyParams(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	if err := s.params.FromMap(m); err != nil {
		return err
	}
	// Sync local fields from registry
	if p := s.params.Get("stop_loss_pct"); p != nil {
		s.stopLossPct = p.GetFloat()
	}
	if p := s.params.Get("take_profit_pct"); p != nil {
		s.takeProfitPct = p.GetFloat()
	}
	if p := s.params.Get("trailing_stop"); p != nil {
		s.trailingStop = p.GetBool()
	}
	if p := s.params.Get("max_drawdown"); p != nil {
		s.maxDrawdown = p.GetFloat()
	}
	if p := s.params.Get("position_size"); p != nil {
		s.positionSize = p.GetFloat()
	}
	if p := s.params.Get("pyramiding"); p != nil {
		s.pyramiding = p.GetBool()
	}
	if p := s.params.Get("pyramiding_n"); p != nil {
		s.pyramidingN = p.GetInt()
	}
	return nil
}

func (s *{{.Name}}) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

func (s *{{.Name}}) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *{{.Name}}) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *{{.Name}}) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	s.barCount++
	if len(s.bars) > 500 {
		s.bars = s.bars[len(s.bars)-500:]
	}

	// Evaluate entry rules
	for _, fn := range s.entryRules() {
		if fn() {
			signal := &model.Signal{
				Symbol:    s.symbol,
				Direction: "LONG",
				Strength:  0.8,
				Strategy:  s.name,
				Reason:    "entry rule triggered",
			}
			s.signals = append(s.signals, *signal)
			return signal, nil
		}
	}

	// Evaluate exit rules
	for _, fn := range s.exitRules() {
		if fn() {
			signal := &model.Signal{
				Symbol:    s.symbol,
				Direction: "CLOSE",
				Strength:  0.8,
				Strategy:  s.name,
				Reason:    "exit rule triggered",
			}
			return signal, nil
		}
	}

	return nil, nil
}

func (s *{{.Name}}) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// entryRules returns compiled entry condition functions.
func (s *{{.Name}}) entryRules() []func() bool {
	return []func() bool{
		{{range .EntryRules}}
		func() bool {
			// {{.Operator}}: {{.Left}} {{.Operator}} {{.Right}}
			return s.evalCondition("{{.Operator}}", "{{.Left}}", "{{.Right}}")
		},
		{{end}}
	}
}

// exitRules returns compiled exit condition functions.
func (s *{{.Name}}) exitRules() []func() bool {
	return []func() bool{
		{{range .ExitRules}}
		func() bool {
			// {{.Operator}}: {{.Left}} {{.Operator}} {{.Right}}
			return s.evalCondition("{{.Operator}}", "{{.Left}}", "{{.Right}}")
		},
		{{end}}
	}
}

func (s *{{.Name}}) evalCondition(op, left, right string) bool {
	if len(s.bars) < 2 {
		return false
	}
	lastBar := s.bars[len(s.bars)-1]
	prevBar := s.bars[len(s.bars)-2]

	switch op {
	case "above":
		val := s.resolveValue(left, lastBar)
		thresh := s.resolveValue(right, lastBar)
		return val > thresh
	case "below":
		val := s.resolveValue(left, lastBar)
		thresh := s.resolveValue(right, lastBar)
		return val < thresh
	case "cross_up":
		curr := s.resolveValue(left, lastBar)
		prev := s.resolveValue(left, prevBar)
		cross := s.resolveValue(right, lastBar)
		return prev <= cross && curr > cross
	case "cross_down":
		curr := s.resolveValue(left, lastBar)
		prev := s.resolveValue(left, prevBar)
		cross := s.resolveValue(right, lastBar)
		return prev >= cross && curr < cross
	case "golden_cross":
		return s.evalGoldenCross()
	case "death_cross":
		return s.evalDeathCross()
	case "rsi_oversold":
		return s.indicators["RSI"] < 30
	case "rsi_overbought":
		return s.indicators["RSI"] > 70
	case "boll_lower":
		return lastBar.Close < s.indicators["BB_LOWER"]
	case "boll_upper":
		return lastBar.Close > s.indicators["BB_UPPER"]
	case "macd_bullish":
		return s.indicators["MACD"] > s.indicators["MACD_SIGNAL"]
	case "macd_bearish":
		return s.indicators["MACD"] < s.indicators["MACD_SIGNAL"]
	}
	return false
}

func (s *{{.Name}}) resolveValue(name string, bar model.Bar) float64 {
	switch name {
	case "close": return bar.Close
	case "open": return bar.Open
	case "high": return bar.High
	case "low": return bar.Low
	case "volume": return bar.Volume
	}
	if v, ok := s.indicators[name]; ok {
		return v
	}
	return 0
}

func (s *{{.Name}}) evalGoldenCross() bool {
	// MA50 crosses above MA200
	if len(s.bars) < 200 {
		return false
	}
	ma50Now := s.computeSMA(50)
	ma200Now := s.computeSMA(200)
	prev50 := s.computeSMAN(50, len(s.bars)-1)
	prev200 := s.computeSMAN(200, len(s.bars)-1)
	return prev50 <= prev200 && ma50Now > ma200Now
}

func (s *{{.Name}}) evalDeathCross() bool {
	if len(s.bars) < 200 {
		return false
	}
	ma50Now := s.computeSMA(50)
	ma200Now := s.computeSMA(200)
	prev50 := s.computeSMAN(50, len(s.bars)-1)
	prev200 := s.computeSMAN(200, len(s.bars)-1)
	return prev50 >= prev200 && ma50Now < ma200Now
}

func (s *{{.Name}}) computeSMA(period int) float64 {
	return s.computeSMAN(period, len(s.bars))
}

func (s *{{.Name}}) computeSMAN(period, limit int) float64 {
	if limit > len(s.bars) {
		limit = len(s.bars)
	}
	if period <= 0 || limit < period {
		return 0
	}
	sum := 0.0
	for i := limit - period; i < limit; i++ {
		sum += s.bars[i].Close
	}
	return sum / float64(period)
}
`))

func generateCode(cfg StrategyConfig) (string, error) {
	var buf bytes.Buffer
	err := strategyTemplate.Execute(&buf, map[string]any{
		"Name":       toExportedName(cfg.Name),
		"Symbol":     cfg.Symbol,
		"EntryRules": cfg.EntryRules,
		"ExitRules":  cfg.ExitRules,
		"RiskMgmt":   cfg.RiskMgmt,
	})
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// CompileFromJSON compiles a strategy from a JSON string.
func CompileFromJSON(jsonStr string) (*CompiledStrategy, error) {
	var cfg StrategyConfig
	if err := json.Unmarshal([]byte(jsonStr), &cfg); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return Compile(cfg)
}

func toExportedName(name string) string {
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	parts := strings.Fields(name)
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}
