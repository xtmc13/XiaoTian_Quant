package config

import (
	"os"
	"strings"
	"sync"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Exchange ExchangeConfig `yaml:"exchange"`
	Risk     RiskConfig     `yaml:"risk"`
	Portfolio PortfolioConfig `yaml:"portfolio"`
	Strategy StrategyConfig `yaml:"strategy"`
	Backtest BacktestConfig `yaml:"backtest"`
	AI       AIConfig       `yaml:"ai"`
	Notify   NotifyConfig   `yaml:"notify"`
	Cache    CacheConfig    `yaml:"cache"`
}

type ServerConfig struct {
	Port    string `yaml:"port"`
	Mode    string `yaml:"mode"` // release, debug
	LogLevel string `yaml:"log_level"`
	LogFormat string `yaml:"log_format"` // json, text
}

type ExchangeConfig struct {
	Default  string           `yaml:"default"`
	Binance  ExchangeCreds    `yaml:"binance"`
	OKX      ExchangeCreds    `yaml:"okx"`
}

type ExchangeCreds struct {
	APIKey    string `yaml:"api_key"`
	APISecret string `yaml:"api_secret"`
	Enabled   bool   `yaml:"enabled"`
}

type RiskConfig struct {
	MaxOrderSize        float64 `yaml:"max_order_size_usdt"`
	DailyLimit          float64 `yaml:"daily_limit"`
	MaxConcurrentOrders int     `yaml:"max_concurrent_orders"`
	MaxPositions        int     `yaml:"max_positions"`
	PositionLimit       float64 `yaml:"position_limit_pct"`
	NetExposureLimit    float64 `yaml:"net_exposure_limit_pct"`
	MaxDrawdown         float64 `yaml:"max_drawdown_pct"`
	ConsecutiveLosses   int     `yaml:"consecutive_losses"`
	FundingRateLimit    float64 `yaml:"funding_rate_limit"`
	MarginRatio         float64 `yaml:"margin_ratio"`
	PriceSanityPct      float64 `yaml:"price_sanity_pct"`
	VolatilityLimit     float64 `yaml:"volatility_limit_pct"`
	PriceSpikeWindow    float64 `yaml:"price_spike_window_pct"`
	RateLimitPerSec     float64 `yaml:"rate_limit_per_sec"`
	CircuitBreakerThreshold int  `yaml:"circuit_breaker_threshold"`
	CircuitBreakerResetSecs int `yaml:"circuit_breaker_reset_secs"`
}

type PortfolioConfig struct {
	InitialBalance    float64 `yaml:"initial_balance"`
	SizingMethod      string  `yaml:"sizing_method"`
	ReconcileIntervalSecs int `yaml:"reconcile_interval_secs"`
	MaxSnapshots      int     `yaml:"max_snapshots"`
}

type StrategyConfig struct {
	MaxStrategies int  `yaml:"max_strategies"`
	HotReload     bool `yaml:"hot_reload"`
}

type BacktestConfig struct {
	DefaultCommission float64 `yaml:"default_commission_pct"`
	DefaultSlippage   float64 `yaml:"default_slippage_pct"`
	MaxDataPoints     int     `yaml:"max_data_points"`
}

type AIConfig struct {
	DefaultProvider string          `yaml:"default_provider"`
	Providers       []AIProviderCfg `yaml:"providers"`
	MultiModel      MultiModelCfg   `yaml:"multi_model"`
}

type AIProviderCfg struct {
	Name    string `yaml:"name"`
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
	Enabled bool   `yaml:"enabled"`
}

type MultiModelCfg struct {
	Enabled        bool `yaml:"enabled"`
	MinConsensus   int  `yaml:"min_consensus"`
	MinConfidence  float64 `yaml:"min_confidence"`
}

type NotifyConfig struct {
	Enabled  bool              `yaml:"enabled"`
	Channels []NotifyChannelCfg `yaml:"channels"`
}

type NotifyChannelCfg struct {
	Type   string            `yaml:"type"` // email, lark, dingtalk, telegram, log
	Config map[string]string `yaml:"config"`
}

type CacheConfig struct {
	Enabled  bool   `yaml:"enabled"`
	RedisURL string `yaml:"redis_url"`
}

var (
	global *Config
	mu     sync.RWMutex
)

// Default returns a Config with sensible defaults.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Port:      "8080",
			Mode:      "release",
			LogLevel:  "INFO",
			LogFormat: "text",
		},
		Exchange: ExchangeConfig{
			Default: "binance",
		},
		Risk: RiskConfig{
			MaxOrderSize:         10000,
			DailyLimit:           100000,
			MaxConcurrentOrders:  5,
			MaxPositions:         10,
			PositionLimit:        50,
			NetExposureLimit:     80,
			MaxDrawdown:          10,
			ConsecutiveLosses:    5,
			FundingRateLimit:     0.00375,
			MarginRatio:          150,
			PriceSanityPct:       5,
			VolatilityLimit:      2,
			PriceSpikeWindow:     3,
			RateLimitPerSec:      10,
			CircuitBreakerThreshold: 5,
			CircuitBreakerResetSecs: 60,
		},
		Portfolio: PortfolioConfig{
			InitialBalance:       100000,
			SizingMethod:         "fixed_fraction",
			ReconcileIntervalSecs: 30,
			MaxSnapshots:         5000,
		},
		Strategy: StrategyConfig{
			MaxStrategies: 20,
			HotReload:     true,
		},
		Backtest: BacktestConfig{
			DefaultCommission: 0.001,
			DefaultSlippage:   0.0005,
			MaxDataPoints:     100000,
		},
		AI: AIConfig{
			DefaultProvider: "deepseek",
			MultiModel: MultiModelCfg{
				Enabled:       false,
				MinConsensus:  2,
				MinConfidence: 0.6,
			},
		},
		Notify: NotifyConfig{
			Enabled: false,
			Channels: []NotifyChannelCfg{
				{Type: "log", Config: map[string]string{}},
			},
		},
		Cache: CacheConfig{
			Enabled: false,
		},
	}
}

// loadDotEnv reads .env file and sets environment variables (no external dependency).
// Supports KEY=VALUE lines, # comments, quoted values, and export prefix.
func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // silently skip if no .env file
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		// Strip optional "export " prefix
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq < 1 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		// Strip surrounding quotes
		if len(val) >= 2 {
			q := val[0]
			if q == '"' || q == '\'' || q == '`' {
				if val[len(val)-1] == q {
					val = val[1 : len(val)-1]
				}
			}
		}
		// Check key is a valid identifier
		if key == "" {
			continue
		}
		for _, r := range key {
			if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
				continue
			}
		}
		os.Setenv(key, val)
	}
}

// Load reads config from YAML file, falling back to defaults and env overrides.
func Load(path string) (*Config, error) {
	cfg := Default()

	// Load .env file if it exists (search up to 2 levels up for flexibility)
	for _, p := range []string{".env", "../.env", "../../.env"} {
		if _, err := os.Stat(p); err == nil {
			loadDotEnv(p)
			break
		}
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, err
			}
		}
	}

	// Environment variable overrides
	if v := os.Getenv("PORT"); v != "" {
		cfg.Server.Port = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Server.LogLevel = v
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		cfg.Server.LogFormat = v
	}
	if v := os.Getenv("BINANCE_API_KEY"); v != "" {
		cfg.Exchange.Binance.APIKey = v
		cfg.Exchange.Binance.Enabled = true
	}
	if v := os.Getenv("BINANCE_API_SECRET"); v != "" {
		cfg.Exchange.Binance.APISecret = v
	}
	if v := os.Getenv("OKX_API_KEY"); v != "" {
		cfg.Exchange.OKX.APIKey = v
		cfg.Exchange.OKX.Enabled = true
	}
	if v := os.Getenv("OKX_API_SECRET"); v != "" {
		cfg.Exchange.OKX.APISecret = v
	}
	if v := os.Getenv("DEEPSEEK_API_KEY"); v != "" {
		setOrAppendProvider(cfg, "deepseek", "https://api.deepseek.com/v1", v, "deepseek-chat")
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		setOrAppendProvider(cfg, "openai", "https://api.openai.com/v1", v, "gpt-4o")
	}
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		setOrAppendProvider(cfg, "claude", "https://api.anthropic.com/v1", v, "claude-opus-4-7")
	}
	if v := os.Getenv("CACHE_ENABLED"); v == "true" {
		cfg.Cache.Enabled = true
	}
	if v := os.Getenv("REDIS_URL"); v != "" {
		cfg.Cache.RedisURL = v
	}

	mu.Lock()
	global = cfg
	mu.Unlock()

	return cfg, nil
}

func setOrAppendProvider(cfg *Config, name, baseURL, apiKey, model string) {
	for i, p := range cfg.AI.Providers {
		if strings.EqualFold(p.Name, name) {
			cfg.AI.Providers[i].APIKey = apiKey
			cfg.AI.Providers[i].Enabled = true
			return
		}
	}
	cfg.AI.Providers = append(cfg.AI.Providers, AIProviderCfg{
		Name:    name,
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		Enabled: true,
	})
}

// Get returns the global config, or Default if not loaded.
func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	if global != nil {
		return global
	}
	return Default()
}

// Exchange returns credentials for a named exchange.
func (c *Config) ExchangeCreds(name string) (apiKey, apiSecret string, enabled bool) {
	switch strings.ToLower(name) {
	case "binance":
		return c.Exchange.Binance.APIKey, c.Exchange.Binance.APISecret, c.Exchange.Binance.Enabled
	case "okx":
		return c.Exchange.OKX.APIKey, c.Exchange.OKX.APISecret, c.Exchange.OKX.Enabled
	}
	return "", "", false
}

// AIProvider returns the configured provider by name.
func (c *Config) AIProvider(name string) *AIProviderCfg {
	for i := range c.AI.Providers {
		if strings.EqualFold(c.AI.Providers[i].Name, name) && c.AI.Providers[i].Enabled {
			return &c.AI.Providers[i]
		}
	}
	return nil
}

// FirstEnabledProvider returns the first enabled AI provider.
func (c *Config) FirstEnabledProvider() *AIProviderCfg {
	for i := range c.AI.Providers {
		if c.AI.Providers[i].Enabled {
			return &c.AI.Providers[i]
		}
	}
	// Fallback to default with env key
	p := c.AIProvider(c.AI.DefaultProvider)
	if p != nil {
		return p
	}
	return &AIProviderCfg{
		Name:    c.AI.DefaultProvider,
		BaseURL: "https://api.deepseek.com/v1",
		APIKey:  os.Getenv("DEEPSEEK_API_KEY"),
		Model:   "deepseek-chat",
		Enabled: true,
	}
}
