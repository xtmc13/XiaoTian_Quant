package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── WebSocket Hub ──────────────────────────────────────────────

// Hub manages all WebSocket connections and broadcasts messages.
type Hub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan Message
	mu         sync.RWMutex
}

// Client represents a single WebSocket connection with subscriptions.
type Client struct {
	hub           *Hub
	conn          *websocket.Conn
	send          chan []byte
	subscriptions map[string]bool // e.g., "price", "order", "position", "signal", "klines"
	mu            sync.RWMutex
}

// Message is a message to broadcast to clients.
type Message struct {
	Channel string `json:"channel"` // target channel
	Type    string `json:"type"`    // message type
	Data    any    `json:"data"`
	Symbol  string `json:"symbol,omitempty"`
}

var (
	wsHub     *Hub
	wsHubOnce sync.Once
)

// GetHub returns the global WebSocket hub.
func GetHub() *Hub {
	wsHubOnce.Do(func() {
		wsHub = &Hub{
			clients:    make(map[*Client]bool),
			register:   make(chan *Client),
			unregister: make(chan *Client),
			broadcast:  make(chan Message, 256),
		}
		go wsHub.run()
	})
	return wsHub
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("[ws] client connected, total: %d", h.ClientCount())

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("[ws] client disconnected, total: %d", h.ClientCount())

		case msg := <-h.broadcast:
			h.mu.RLock()
			clients := make([]*Client, 0, len(h.clients))
			for client := range h.clients {
				if client.isSubscribed(msg.Channel) {
					clients = append(clients, client)
				}
			}
			h.mu.RUnlock()

			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}

			for _, client := range clients {
				select {
				case client.send <- data:
				default:
					h.unregister <- client
				}
			}
		}
	}
}

// Broadcast sends a message to all subscribed clients.
func (h *Hub) Broadcast(msg Message) {
	select {
	case h.broadcast <- msg:
	default:
	}
}

// BroadcastPrice sends a price update.
func (h *Hub) BroadcastPrice(symbol string, price, changePct, high, low, volume float64) {
	h.Broadcast(Message{
		Channel: "price",
		Type:    "price_update",
		Symbol:  symbol,
		Data: map[string]any{
			"symbol":     symbol,
			"price":      price,
			"change_pct": changePct,
			"high":       high,
			"low":        low,
			"volume":     volume,
			"timestamp":  time.Now().UnixMilli(),
		},
	})
}

// BroadcastKline sends a kline/bar update.
func (h *Hub) BroadcastKline(symbol, interval string, bar model.Bar) {
	h.Broadcast(Message{
		Channel: "klines",
		Type:    "kline",
		Symbol:  symbol,
		Data: map[string]any{
			"symbol":   symbol,
			"interval": interval,
			"time":     bar.Time,
			"open":     bar.Open,
			"high":     bar.High,
			"low":      bar.Low,
			"close":    bar.Close,
			"volume":   bar.Volume,
		},
	})
}

// BroadcastSignal sends a trading signal.
func (h *Hub) BroadcastSignal(signal model.Signal) {
	h.Broadcast(Message{
		Channel: "signal",
		Type:    "signal",
		Symbol:  signal.Symbol,
		Data:    signal,
	})
}

// BroadcastOrder sends an order update.
func (h *Hub) BroadcastOrder(order model.OrderData) {
	h.Broadcast(Message{
		Channel: "order",
		Type:    "order_update",
		Symbol:  order.Symbol,
		Data:    order,
	})
}

// BroadcastTrade sends a trade execution.
func (h *Hub) BroadcastTrade(trade model.TradeData) {
	h.Broadcast(Message{
		Channel: "trade",
		Type:    "trade",
		Symbol:  trade.Symbol,
		Data:    trade,
	})
}

// BroadcastPosition sends a position update.
func (h *Hub) BroadcastPosition(symbol string, position map[string]any) {
	h.Broadcast(Message{
		Channel: "position",
		Type:    "position_update",
		Symbol:  symbol,
		Data:    position,
	})
}

// BroadcastProtection sends a protection alert.
func (h *Hub) BroadcastProtection(name, symbol, action, reason string) {
	h.Broadcast(Message{
		Channel: "protection",
		Type:    "protection_trigger",
		Symbol:  symbol,
		Data: map[string]any{
			"protection": name,
			"symbol":     symbol,
			"action":     action,
			"reason":     reason,
			"time":       time.Now().UnixMilli(),
		},
	})
}

// BroadcastSystem sends a system event.
func (h *Hub) BroadcastSystem(event, message string) {
	h.Broadcast(Message{
		Channel: "system",
		Type:    "system_event",
		Data: map[string]any{
			"event":   event,
			"message": message,
			"time":    time.Now().UnixMilli(),
		},
	})
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ── Client Methods ─────────────────────────────────────────────

func (c *Client) isSubscribed(channel string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.subscriptions[channel] || c.subscriptions["all"]
}

func (c *Client) subscribe(channels []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range channels {
		c.subscriptions[ch] = true
	}
}

func (c *Client) unsubscribe(channels []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range channels {
		delete(c.subscriptions, ch)
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(4096)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[ws] read error: %v", err)
			}
			break
		}

		var cmd struct {
			Action   string   `json:"action"`
			Channels []string `json:"channels"`
			Symbol   string   `json:"symbol,omitempty"`
			Interval string   `json:"interval,omitempty"`
		}
		if err := json.Unmarshal(message, &cmd); err == nil {
			switch cmd.Action {
			case "subscribe":
				c.subscribe(cmd.Channels)
				log.Printf("[ws] client subscribed: %v", cmd.Channels)
			case "unsubscribe":
				c.unsubscribe(cmd.Channels)
				log.Printf("[ws] client unsubscribed: %v", cmd.Channels)
			}
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ── HTTP Handler ───────────────────────────────────────────────

var hubUpgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// HubHandler handles WebSocket connections with subscription support.
func HubHandler(c *gin.Context) {
	conn, err := hubUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ws] upgrade failed: %v", err)
		return
	}

	hub := GetHub()
	client := &Client{
		hub:           hub,
		conn:          conn,
		send:          make(chan []byte, 256),
		subscriptions: map[string]bool{"all": true},
	}

	client.hub.register <- client

	go client.writePump()
	go client.readPump()
}

// Stats returns WebSocket hub statistics.
func Stats(c *gin.Context) {
	hub := GetHub()
	c.JSON(http.StatusOK, gin.H{
		"connected_clients": hub.ClientCount(),
	})
}
