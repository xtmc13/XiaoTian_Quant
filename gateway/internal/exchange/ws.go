package exchange

import (
	"encoding/json"
	"fmt"
	"io"
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

	// AppPing 非空时，心跳改用应用层文本帧发送该内容（如 OKX 的 "ping"、
	// Bybit 的 {"op":"ping"}），而不是协议层 Ping 帧。交易所回复的 pong 以
	// 普通文本消息回到 OnMessage，由调用方识别。
	AppPing []byte
}

// WSClient manages a single WebSocket connection with automatic reconnection.
type WSClient struct {
	cfg       WSConfig
	conn      *websocket.Conn
	mu        sync.Mutex
	connected bool
	closed    bool
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
	if w.connected || w.closed {
		w.mu.Unlock()
		return nil
	}
	w.mu.Unlock()

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}
	conn, resp, err := dialer.Dial(w.cfg.URL, nil)
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			log.Printf("[WS] Dial failed for %s: status=%d body=%s err=%v", w.cfg.URL, resp.StatusCode, string(body), err)
		} else {
			log.Printf("[WS] Dial failed for %s: err=%v", w.cfg.URL, err)
		}
		// The retry chain normally starts from readLoop after a successful
		// dial. If the very first dial fails (transient reset, cold network),
		// kick off backoff retries here so the stream isn't silently dead.
		select {
		case <-w.stopCh:
		default:
			go w.tryReconnect()
		}
		return fmt.Errorf("ws dial: %w", err)
	}

	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(w.cfg.PongTimeout))
		return nil
	})

	w.mu.Lock()
	if w.connected || w.closed {
		// State changed while we were dialing; drop this connection.
		conn.Close()
		w.mu.Unlock()
		return nil
	}
	w.conn = conn
	w.connected = true
	w.reconnect = 0
	w.doneCh = make(chan struct{})
	doneCh := w.doneCh
	w.mu.Unlock()

	go w.readLoop(conn, doneCh)
	go w.pingLoop(conn, doneCh)

	if w.cfg.OnConnected != nil {
		w.cfg.OnConnected()
	}

	return nil
}

func (w *WSClient) readLoop(conn *websocket.Conn, doneCh chan struct{}) {
	defer func() {
		conn.Close()
		w.mu.Lock()
		// Only clear shared state if this generation is still the current one.
		if w.conn == conn {
			w.connected = false
			w.conn = nil
		}
		w.mu.Unlock()
		close(doneCh)
	}()

	for {
		select {
		case <-w.stopCh:
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(w.cfg.PongTimeout * 2))
		_, message, err := conn.ReadMessage()
		if err != nil {
			if w.cfg.OnDisconnected != nil {
				w.cfg.OnDisconnected(err)
			}
			// Don't try to reconnect if the client has been stopped.
			select {
			case <-w.stopCh:
				return
			default:
			}
			// 必须先清理本代连接状态再重连：tryReconnect 同步调用 Connect，
			// 而 defer 里的清理要等 readLoop 返回后才执行；若不在此清理，
			// Connect 首行的 connected 检查会把新连接误判为重复连接直接丢弃，
			// 重连链就此静默中断。
			w.clearConn(conn)
			w.tryReconnect()
			return
		}

		if w.cfg.OnMessage != nil {
			w.cfg.OnMessage(message)
		}
	}
}

func (w *WSClient) pingLoop(conn *websocket.Conn, doneCh chan struct{}) {
	ticker := time.NewTicker(w.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-doneCh:
			return
		case <-ticker.C:
			w.mu.Lock()
			if w.connected && w.conn == conn {
				if len(w.cfg.AppPing) > 0 {
					conn.WriteMessage(websocket.TextMessage, w.cfg.AppPing)
				} else {
					conn.WriteMessage(websocket.PingMessage, nil)
				}
			}
			w.mu.Unlock()
		}
	}
}

// clearConn 清理某代连接占用的共享状态（仅当该代仍是当前代时）。
func (w *WSClient) clearConn(conn *websocket.Conn) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn == conn {
		w.connected = false
		w.conn = nil
	}
}

func (w *WSClient) tryReconnect() {
	w.mu.Lock()
	if w.reconnect >= w.cfg.MaxReconnects || w.closed {
		w.mu.Unlock()
		log.Printf("[WS] Max reconnects (%d) reached or client closed for %s", w.cfg.MaxReconnects, w.cfg.URL)
		return
	}
	w.reconnect++
	reconnectNum := w.reconnect
	w.mu.Unlock()

	delay := Backoff(reconnectNum, w.cfg.ReconnectDelay, 60*time.Second)
	jitter := time.Duration(rand.Int63n(int64(delay) / 4))
	time.Sleep(delay + jitter)

	// Abort if the client was stopped while we were waiting.
	select {
	case <-w.stopCh:
		return
	default:
	}

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
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	conn := w.conn
	w.conn = nil
	w.connected = false
	doneCh := w.doneCh
	w.mu.Unlock()

	close(w.stopCh)
	if conn != nil {
		_ = conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		conn.Close()
	}

	if doneCh != nil {
		<-doneCh
	}
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

// SendJSON sends a JSON message to a named stream client.
func (h *StreamHub) SendJSON(name string, v any) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if c, ok := h.clients[name]; ok {
		c.SendJSON(v)
	}
}

func (h *StreamHub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
