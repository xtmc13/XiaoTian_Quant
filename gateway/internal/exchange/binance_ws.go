package exchange

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// BinanceWSStream connects to Binance WebSocket for real-time market data.
// Publishes tick/bar data to the event bus for strategy consumption.
type BinanceWSStream struct {
	symbols    []string
	bus        *event.EventBus
	conn       *websocket.Conn
	mu         sync.RWMutex
	running    bool
	stopCh     chan struct{}
	reconnect  chan struct{}

	// Last prices for bar building
	prices     map[string]float64
	pricesMu   sync.RWMutex

	// Callbacks
	onTick      func(tick model.Tick)
	onBar       func(bar model.Bar)
	onRealPrice func(symbol string, price float64)
}

// NewBinanceWSStream creates a new Binance WebSocket stream.
func NewBinanceWSStream(symbols []string, bus *event.EventBus) *BinanceWSStream {
	if len(symbols) == 0 {
		symbols = []string{"btcusdt", "ethusdt", "solusdt"}
	}
	return &BinanceWSStream{
		symbols:   symbols,
		bus:       bus,
		stopCh:    make(chan struct{}),
		reconnect: make(chan struct{}, 1),
		prices:    make(map[string]float64),
	}
}

// Start connects to Binance and begins streaming.
func (s *BinanceWSStream) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	// Default to FrontendPriceFeed if not set explicitly
	if s.onRealPrice == nil && FrontendPriceFeed != nil {
		s.onRealPrice = FrontendPriceFeed
	}
	s.mu.Unlock()

	go s.connect()
	go s.keepalive()

	log.Printf("[binance_ws] started: %v", s.symbols)
	return nil
}

// Stop disconnects and stops the stream.
func (s *BinanceWSStream) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.running = false
	close(s.stopCh)
	if s.conn != nil {
		s.conn.Close()
	}
	log.Printf("[binance_ws] stopped")
}

func (s *BinanceWSStream) connect() {
	// Build combined stream URL: wss://stream.binance.com:9443/stream?streams=btcusdt@trade/ethusdt@trade
	var streams []string
	for _, sym := range s.symbols {
		sym = strings.ToLower(sym)
		streams = append(streams, fmt.Sprintf("%s@trade", sym))
		streams = append(streams, fmt.Sprintf("%s@ticker", sym))
	}
	url := fmt.Sprintf("wss://stream.binance.com:9443/stream?streams=%s", strings.Join(streams, "/"))

	dialer := wsDialer()

	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		log.Printf("[binance_ws] connecting to %s...", url)
		conn, _, err := dialer.Dial(url, nil)
		if err != nil {
			log.Printf("[binance_ws] connect failed: %v, retrying in 5s", err)
			select {
			case <-s.stopCh:
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		s.mu.Lock()
		s.conn = conn
		s.mu.Unlock()

		log.Printf("[binance_ws] connected")
		s.readLoop(conn)

		// Connection lost — reconnect
		select {
		case <-s.stopCh:
			return
		case <-time.After(3 * time.Second):
			log.Printf("[binance_ws] reconnecting...")
		}
	}
}

// wsDialer creates a websocket dialer that uses the system proxy
// or auto-detected proxy (e.g., Clash/SS/V2Ray on common ports).
func wsDialer() *websocket.Dialer {
	// Check environment variables first
	proxyURL := os.Getenv("HTTP_PROXY")
	if proxyURL == "" {
		proxyURL = os.Getenv("http_proxy")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("HTTPS_PROXY")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("https_proxy")
	}
	if proxyURL == "" {
		// Try common proxy ports
		for _, addr := range []string{"http://127.0.0.1:7897", "http://127.0.0.1:7890", "http://127.0.0.1:1080", "http://127.0.0.1:10808"} {
			if testProxy(addr) {
				proxyURL = addr
				log.Printf("[binance_ws] auto-detected proxy: %s", proxyURL)
				break
			}
		}
	}

	dialer := websocket.DefaultDialer
	if proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err == nil {
			dialer = &websocket.Dialer{
				Proxy:            http.ProxyURL(proxy),
				HandshakeTimeout: 10 * time.Second,
			}
			log.Printf("[binance_ws] using proxy: %s", proxyURL)
		}
	}
	dialer.HandshakeTimeout = 10 * time.Second
	return dialer
}

// testProxy checks if a proxy is reachable.
func testProxy(proxyURL string) bool {
	proxy, err := url.Parse(proxyURL)
	if err != nil {
		return false
	}
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxy)},
		Timeout:   2 * time.Second,
	}
	resp, err := client.Get("http://www.gstatic.com/generate_204")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

func (s *BinanceWSStream) readLoop(conn *websocket.Conn) {
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[binance_ws] read error: %v", err)
			return
		}

		s.handleMessage(msg)
	}
}

func (s *BinanceWSStream) handleMessage(msg []byte) {
	var envelope struct {
		Stream string          `json:"stream"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(msg, &envelope); err != nil {
		return
	}

	switch {
	case strings.HasSuffix(envelope.Stream, "@trade"):
		s.handleTrade(envelope.Data, envelope.Stream)
	case strings.HasSuffix(envelope.Stream, "@ticker"):
		s.handleTicker(envelope.Data, envelope.Stream)
	}
}

func (s *BinanceWSStream) handleTrade(data json.RawMessage, stream string) {
	var trade struct {
		Symbol string `json:"s"`
		Price  string `json:"p"`
		Qty    string `json:"q"`
		Time   int64  `json:"T"`
		Buyer  bool   `json:"m"` // true = buyer is maker (sell trade)
	}
	if err := json.Unmarshal(data, &trade); err != nil {
		return
	}

	price := parseFloat(trade.Price)
	qty := parseFloat(trade.Qty)

	s.pricesMu.Lock()
	s.prices[trade.Symbol] = price
	s.pricesMu.Unlock()

	tick := model.Tick{
		Symbol:    trade.Symbol,
		Last:      price,
		Bid:       price * 0.9999,
		Ask:       price * 1.0001,
		Volume:    qty,
		Timestamp: trade.Time,
	}

	// Publish to event bus for strategies
	if s.bus != nil {
		s.bus.PublishBlocking(event.Event{
			Type:   event.TypeTick,
			Symbol: trade.Symbol,
			Data:   tick,
		})
	}

	if s.onTick != nil {
		s.onTick(tick)
	}
	// Feed real price to frontend WS
	if s.onRealPrice != nil {
		s.onRealPrice(trade.Symbol, price)
	}
}

func (s *BinanceWSStream) handleTicker(data json.RawMessage, stream string) {
	var ticker struct {
		Symbol       string `json:"s"`
		LastPrice    string `json:"c"`
		OpenPrice    string `json:"o"`
		HighPrice    string `json:"h"`
		LowPrice     string `json:"l"`
		Volume       string `json:"v"`
		BidPrice     string `json:"b"`
		AskPrice     string `json:"a"`
		PriceChange  string `json:"p"`
		PriceChangePct string `json:"P"`
	}
	if err := json.Unmarshal(data, &ticker); err != nil {
		return
	}

	last := parseFloat(ticker.LastPrice)
	high := parseFloat(ticker.HighPrice)
	low := parseFloat(ticker.LowPrice)
	vol := parseFloat(ticker.Volume)
	_ = parseFloat(ticker.BidPrice)
	_ = parseFloat(ticker.AskPrice)

	s.pricesMu.Lock()
	s.prices[ticker.Symbol] = last
	s.pricesMu.Unlock()

	// Build bar-like data from ticker (approximate, for strategies that need bars)
	bar := model.Bar{
		Symbol:   ticker.Symbol,
		Open:     parseFloat(ticker.OpenPrice),
		High:     high,
		Low:      low,
		Close:    last,
		Volume:   vol,
		Interval: "1m",
		Time:     time.Now().UnixMilli(),
	}

	if s.bus != nil {
		s.bus.Publish(event.Event{
			Type:   event.TypeBar,
			Symbol: ticker.Symbol,
			Data:   bar,
		})
	}

	if s.onBar != nil {
		s.onBar(bar)
	}
}

func (s *BinanceWSStream) keepalive() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.RLock()
			conn := s.conn
			s.mu.RUnlock()
			if conn != nil {
				conn.WriteMessage(websocket.PingMessage, nil)
			}
		}
	}
}

// GetPrice returns the last known price for a symbol.
func (s *BinanceWSStream) GetPrice(symbol string) float64 {
	s.pricesMu.RLock()
	defer s.pricesMu.RUnlock()
	return s.prices[strings.ToUpper(symbol)]
}

// SetOnTick sets the tick callback.
func (s *BinanceWSStream) SetOnTick(fn func(tick model.Tick)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onTick = fn
}

// SetOnBar sets the bar callback.
func (s *BinanceWSStream) SetOnBar(fn func(bar model.Bar)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onBar = fn
}

// FrontendPriceFeed is called by BinanceWSStream when real price data arrives.
// Set by handler.init() to feed prices to the frontend WebSocket.
var FrontendPriceFeed func(symbol string, price float64)

// SetOnRealPrice sets the real price callback for frontend WS feed.
func (s *BinanceWSStream) SetOnRealPrice(fn func(symbol string, price float64)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onRealPrice = fn
}

// IsRunning returns true if the stream is active.
func (s *BinanceWSStream) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}
