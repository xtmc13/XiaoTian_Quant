package exchange

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ── WebSocket Client ──

// WSConfig holds WebSocket connection configuration.
type WSConfig struct {
	URL             string
	PingInterval    time.Duration
	PongTimeout     time.Duration
	ReconnectDelay  time.Duration
	MaxReconnects   int
	OnMessage       func(message []byte)
	OnConnected     func()
	OnDisconnected  func(err error)
}

// WSClient manages a single WebSocket connection with automatic reconnection.
type WSClient struct {
	cfg       WSConfig
	conn      *websocket.Conn
	mu        sync.Mutex
	connected bool
	stopCh    chan struct{}
	doneCh    chan struct{}
	reconnect int
}

func NewWSClient(cfg WSConfig) *WSClient {
	if cfg.PingInterval <= 0 {
		cfg.PingInterval = 15 * time.Second
	}
	if cfg.PongTimeout <= 0 {
		cfg.PongTimeout = 10 * time.Second
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = 3 * time.Second
	}
	if cfg.MaxReconnects <= 0 {
		cfg.MaxReconnects = 100
	}
	return &WSClient{
		cfg:    cfg,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

func (w *WSClient) Connect() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.connected {
		return nil
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}
	conn, _, err := dialer.Dial(w.cfg.URL, nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}

	w.conn = conn
	w.connected = true

	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(w.cfg.PongTimeout))
		return nil
	})

	go w.readLoop()
	go w.pingLoop()

	if w.cfg.OnConnected != nil {
		w.cfg.OnConnected()
	}

	return nil
}

func (w *WSClient) readLoop() {
	defer func() {
		w.mu.Lock()
		w.connected = false
		w.conn.Close()
		w.mu.Unlock()
		close(w.doneCh)
	}()

	for {
		select {
		case <-w.stopCh:
			return
		default:
		}

		w.conn.SetReadDeadline(time.Now().Add(w.cfg.PongTimeout * 2))
		_, message, err := w.conn.ReadMessage()
		if err != nil {
			if w.cfg.OnDisconnected != nil {
				w.cfg.OnDisconnected(err)
			}
			w.tryReconnect()
			return
		}

		if w.cfg.OnMessage != nil {
			w.cfg.OnMessage(message)
		}
	}
}

func (w *WSClient) pingLoop() {
	ticker := time.NewTicker(w.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.mu.Lock()
			if w.connected && w.conn != nil {
				w.conn.WriteMessage(websocket.PingMessage, nil)
			}
			w.mu.Unlock()
		}
	}
}

func (w *WSClient) tryReconnect() {
	w.mu.Lock()
	if w.reconnect >= w.cfg.MaxReconnects {
		w.mu.Unlock()
		log.Printf("[WS] Max reconnects (%d) reached for %s", w.cfg.MaxReconnects, w.cfg.URL)
		return
	}
	w.reconnect++
	reconnectNum := w.reconnect
	w.mu.Unlock()

	delay := Backoff(reconnectNum, w.cfg.ReconnectDelay, 60*time.Second)
	jitter := time.Duration(rand.Int63n(int64(delay) / 4))
	time.Sleep(delay + jitter)

	log.Printf("[WS] Reconnecting to %s (attempt %d)...", w.cfg.URL, reconnectNum)
	if err := w.Connect(); err != nil {
		log.Printf("[WS] Reconnect failed: %v", err)
	}
}

// Send sends a text message over the WebSocket.
func (w *WSClient) Send(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.connected || w.conn == nil {
		return fmt.Errorf("not connected")
	}
	return w.conn.WriteMessage(websocket.TextMessage, data)
}

// SendJSON marshals and sends a JSON message.
func (w *WSClient) SendJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.Send(data)
}

// Close gracefully shuts down the WebSocket connection.
func (w *WSClient) Close() {
	close(w.stopCh)
	w.mu.Lock()
	if w.conn != nil {
		w.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		w.conn.Close()
	}
	w.connected = false
	w.mu.Unlock()
	<-w.doneCh
}

func (w *WSClient) IsConnected() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.connected
}

// ── WebSocket Stream Hub ──

// StreamHub manages multiple WebSocket clients for different streams.
type StreamHub struct {
	clients map[string]*WSClient
	mu      sync.RWMutex
}

func NewStreamHub() *StreamHub {
	return &StreamHub{
		clients: make(map[string]*WSClient),
	}
}

func (h *StreamHub) Add(name string, client *WSClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[name] = client
}

func (h *StreamHub) Remove(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.clients[name]; ok {
		c.Close()
		delete(h.clients, name)
	}
}

func (h *StreamHub) Get(name string) *WSClient {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.clients[name]
}

func (h *StreamHub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for name, c := range h.clients {
		c.Close()
		delete(h.clients, name)
	}
}

func (h *StreamHub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
