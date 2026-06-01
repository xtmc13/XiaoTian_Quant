package exchange

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

// ── Rate Limiter ──

// RateLimiter implements a token bucket rate limiter.
type RateLimiter struct {
	rate       float64 // tokens per second
	maxTokens  float64
	tokens     float64
	lastRefill time.Time
	mu         sync.Mutex
}

func NewRateLimiter(rate float64, maxTokens int) *RateLimiter {
	if maxTokens <= 0 {
		maxTokens = int(rate)
		if maxTokens < 1 {
			maxTokens = 1
		}
	}
	return &RateLimiter{
		rate:       rate,
		maxTokens:  float64(maxTokens),
		tokens:     float64(maxTokens),
		lastRefill: time.Now(),
	}
}

func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	rl.tokens += elapsed * rl.rate
	if rl.tokens > rl.maxTokens {
		rl.tokens = rl.maxTokens
	}
	rl.lastRefill = now

	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}
	return false
}

func (rl *RateLimiter) Wait() {
	for !rl.Allow() {
		time.Sleep(50 * time.Millisecond)
	}
}

// ── Exchange Session ──

// Session manages HTTP connections with retry and rate limiting.
type Session struct {
	client      *http.Client
	rateLimiter *RateLimiter
	signer      Signer
	maxRetries  int
	baseURL     string
	emaLatency  float64
	mu          sync.RWMutex
}

// Signer produces authentication signatures for exchange API requests.
type Signer interface {
	Sign(method, path, body string, timestamp int64) string
	APIKey() string
}

type SessionConfig struct {
	BaseURL     string
	APIKey      string
	SecretKey   string
	MaxRetries  int
	RateLimit   float64
	Timeout     time.Duration
}

func NewSession(cfg SessionConfig) *Session {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 10
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	s := &Session{
		client: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:    20,
				IdleConnTimeout: 90 * time.Second,
			},
		},
		rateLimiter: NewRateLimiter(cfg.RateLimit, int(cfg.RateLimit)),
		maxRetries:  cfg.MaxRetries,
		baseURL:     cfg.BaseURL,
	}

	if cfg.APIKey != "" && cfg.SecretKey != "" {
		s.signer = &HMACSigner{
			apiKey:    cfg.APIKey,
			secretKey: cfg.SecretKey,
		}
	}

	return s
}

func (s *Session) Client() *http.Client {
	return s.client
}

func (s *Session) BaseURL() string {
	return s.baseURL
}

func (s *Session) Signer() Signer {
	return s.signer
}

func (s *Session) RateLimiter() *RateLimiter {
	return s.rateLimiter
}

func (s *Session) MaxRetries() int {
	return s.maxRetries
}

func (s *Session) UpdateLatency(latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	alpha := 0.2
	s.emaLatency = alpha*float64(latency.Milliseconds()) + (1-alpha)*s.emaLatency
}

func (s *Session) AvgLatencyMs() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.emaLatency
}

// ── HMAC Signer ──

type HMACSigner struct {
	apiKey    string
	secretKey string
}

func (h *HMACSigner) Sign(method, path, body string, timestamp int64) string {
	// Default: HMAC-SHA256 signing (used by Binance)
	return h.SignSHA256(body)
}

func (h *HMACSigner) SignSHA256(payload string) string {
	mac := hmac.New(sha256.New, []byte(h.secretKey))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *HMACSigner) SignSHA512(payload string) string {
	mac := hmac.New(sha512.New, []byte(h.secretKey))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *HMACSigner) APIKey() string {
	return h.apiKey
}

// ── Exponential Backoff ──

// Backoff returns a duration for the nth retry with exponential backoff and jitter.
func Backoff(retry int, base time.Duration, max time.Duration) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	if max <= 0 {
		max = 30 * time.Second
	}
	d := base * time.Duration(1<<uint(retry))
	if d > max {
		d = max
	}
	return d
}

// ── Exchange Interface ──

// Exchange defines the unified interface for all exchange adapters.
type Exchange interface {
	Name() string
	Start() error
	Stop() error
	IsConnected() bool

	PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error)
	CancelOrder(symbol, orderID string) (map[string]any, error)
	GetBalance() ([]map[string]any, error)
	GetPositions() ([]map[string]any, error)
	GetOpenOrders(symbol string) ([]map[string]any, error)
	GetKlines(symbol, interval string, limit int) ([][]any, error)
	GetTicker(symbol string) (map[string]any, error)

	// Market data streams
	StartMarketStream(symbols []string) error
	StartUserStream() error
}

// Ensure Session implements needed patterns
var _ Signer = (*HMACSigner)(nil)
