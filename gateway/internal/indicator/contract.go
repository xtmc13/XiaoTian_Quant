package indicator

// IndicatorOutput defines the chart output contract expected from indicator code.
type IndicatorOutput struct {
	Name          string            `json:"name"`
	Plots         []Plot            `json:"plots"`
	Signals       []Signal          `json:"signals"`
	Layers        []Layer           `json:"layers,omitempty"`
	LatestSignal  map[string]any    `json:"latest_signal,omitempty"`
	CalculatedVars map[string]any   `json:"calculatedVars,omitempty"`
}

// Plot represents a single plot line/band on the chart.
type Plot struct {
	Name    string  `json:"name"`
	Data    []any   `json:"data"` // length must match len(df); element may be a number, nil, or {"value","color","size"}
	Color   string  `json:"color,omitempty"`
	Overlay bool    `json:"overlay,omitempty"`
	Type    string  `json:"type,omitempty"` // "line", "bar", "circle", "lamp"; aliases normalized by NormalizeOutputMap
	// Style pass-through fields (QuantDinger contract §7)
	LineWidth   float64 `json:"lineWidth,omitempty"`
	LineStyle   string  `json:"lineStyle,omitempty"` // "solid" | "dashed"; "style" accepted as input alias
	Opacity     float64 `json:"opacity,omitempty"`
	Size        float64 `json:"size,omitempty"` // "radius" accepted as input alias
	BorderColor string  `json:"borderColor,omitempty"`
	BorderSize  float64 `json:"borderSize,omitempty"`
	BaseValue   float64 `json:"baseValue,omitempty"` // bar baseline
}

// Signal represents buy/sell markers on the chart.
type Signal struct {
	Type string `json:"type"` // "buy" or "sell"
	Text string `json:"text,omitempty"`
	Data []any  `json:"data"` // length must match len(df); float price, nil, bool, or {"active","price","text","color"}
	Color string `json:"color,omitempty"`
	// RenderMode controls how persistent conditions become markers:
	// "events" | "points"/"markers"/"raw" | "state"/"continuous"/"condition".
	// When omitted, dense series (>18% active) are edge-ified by the executor.
	RenderMode string `json:"renderMode,omitempty"`
	TextData   []any  `json:"textData,omitempty"` // optional per-bar labels, length len(df)
}

// Layer represents a sparse visual object (zone, horizontal/sloped line, label).
// Layers do not need per-bar arrays; indices or times must be meaningful for df.
type Layer struct {
	Type        string  `json:"type"` // "zone" | "line" | "label"
	StartIndex  int     `json:"startIndex,omitempty"`
	EndIndex    int     `json:"endIndex,omitempty"`
	Index       int     `json:"index,omitempty"` // label anchor
	StartTime   int64   `json:"startTime,omitempty"`
	EndTime     int64   `json:"endTime,omitempty"`
	Time        int64   `json:"time,omitempty"`
	Top         float64 `json:"top,omitempty"`    // zone
	Bottom      float64 `json:"bottom,omitempty"` // zone
	Price       float64 `json:"price,omitempty"`  // horizontal line / label
	StartPrice  float64 `json:"startPrice,omitempty"` // sloped line
	EndPrice    float64 `json:"endPrice,omitempty"`   // sloped line
	Text        string  `json:"text,omitempty"`
	Color       string  `json:"color,omitempty"`
	FillColor   string  `json:"fillColor,omitempty"`
	BorderColor string  `json:"borderColor,omitempty"`
	TextColor   string  `json:"textColor,omitempty"`
	Opacity     float64 `json:"opacity,omitempty"`
	Dashed      bool    `json:"dashed,omitempty"`
}

// ParamDecl represents a tunable parameter declared via # @param.
type ParamDecl struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // "int", "float", "bool", "str"
	Default     any      `json:"default"`
	Description string   `json:"description"`
	Range       *ParamRange `json:"range,omitempty"`
	Values      []any    `json:"values,omitempty"`
}

// ParamRange defines a numeric scan range for a parameter.
type ParamRange struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

// StrategyConfig holds default strategy settings parsed from # @strategy.
type StrategyConfig struct {
	StopLossPct           float64 `json:"stopLossPct,omitempty"`
	TakeProfitPct         float64 `json:"takeProfitPct,omitempty"`
	EntryPct              float64 `json:"entryPct,omitempty"`
	TrailingEnabled       bool    `json:"trailingEnabled,omitempty"`
	TrailingStopPct       float64 `json:"trailingStopPct,omitempty"`
	TrailingActivationPct float64 `json:"trailingActivationPct,omitempty"`
	TradeDirection        string  `json:"tradeDirection,omitempty"` // "long", "short", "both"
}

// ValidationHint represents a single code quality hint.
type ValidationHint struct {
	Severity string         `json:"severity"` // "error", "warn", "info"
	Code     string         `json:"code"`
	Params   map[string]any `json:"params,omitempty"`
}

// ParseResult is the response from parsing indicator source code.
type ParseResult struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Params         []ParamDecl    `json:"params"`
	StrategyConfig StrategyConfig `json:"strategyConfig"`
}

// ValidateResult is the response from validating indicator source code.
type ValidateResult struct {
	Success       bool             `json:"success"`
	Msg           string           `json:"msg,omitempty"`
	ErrorType     string           `json:"errorType,omitempty"`
	Details       string           `json:"details,omitempty"`
	PlotsCount    int              `json:"plotsCount"`
	SignalsCount  int              `json:"signalsCount"`
	Hints         []ValidationHint `json:"hints"`
}

// SavedIndicator represents a stored indicator in the database.
type SavedIndicator struct {
	ID           int     `json:"id"`
	UserID       int     `json:"user_id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Code         string  `json:"code"`
	ParamsJSON   string  `json:"params_json"`
	StrategyJSON string  `json:"strategy_json"`
	IsEncrypted  int     `json:"is_encrypted"`
	CreatedAt    int64   `json:"created_at"`
	UpdatedAt    int64   `json:"updated_at"`
	// Community metrics
	PricingType   string  `json:"pricing_type,omitempty"`
	Price         float64 `json:"price,omitempty"`
	PurchaseCount int     `json:"purchase_count,omitempty"`
	AvgRating     float64 `json:"avg_rating,omitempty"`
	RatingCount   int     `json:"rating_count,omitempty"`
	ViewCount     int     `json:"view_count,omitempty"`
	ReviewStatus  string  `json:"review_status,omitempty"`
	Status        string  `json:"status,omitempty"`
	Revenue       float64 `json:"revenue,omitempty"`
	// Performance metrics (placeholder, populated by backtest or community)
	TotalReturn float64 `json:"total_return,omitempty"`
	Sharpe      float64 `json:"sharpe,omitempty"`
	MaxDrawdown float64 `json:"max_drawdown,omitempty"`
}

// ValidStrategyKeys defines the allowed keys for # @strategy annotations.
var ValidStrategyKeys = map[string]bool{
	"stopLossPct":           true,
	"takeProfitPct":         true,
	"entryPct":              true,
	"trailingEnabled":       true,
	"trailingStopPct":       true,
	"trailingActivationPct": true,
	"tradeDirection":        true,
}

// ValidParamTypes defines allowed types for # @param annotations.
var ValidParamTypes = map[string]bool{
	"int":    true,
	"float":  true,
	"bool":   true,
	"str":    true,
	"string": true,
}

// ValidTradeDirections defines allowed values for tradeDirection.
var ValidTradeDirections = map[string]bool{
	"long":  true,
	"short": true,
	"both":  true,
}
