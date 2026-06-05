package ws

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

func TestGetHub(t *testing.T) {
	hub1 := GetHub()
	hub2 := GetHub()
	if hub1 != hub2 {
		t.Error("GetHub should return the same instance")
	}
}

func TestHubBroadcast(t *testing.T) {
	hub := GetHub()
	// Just verify it doesn't panic
	hub.Broadcast(Message{
		Channel: "test",
		Type:    "test",
		Data:    map[string]string{"key": "value"},
	})
}

func TestHubBroadcastPrice(t *testing.T) {
	hub := GetHub()
	hub.BroadcastPrice("BTCUSDT", 68000, 1.5, 69000, 67000, 1000)
}

func TestHubBroadcastSignal(t *testing.T) {
	hub := GetHub()
	hub.BroadcastSignal(model.Signal{
		Symbol:    "BTCUSDT",
		Direction: "LONG",
		Strength:  0.85,
		Strategy:  "breakout",
		Reason:    "breakout above resistance",
		Timestamp: time.Now().UnixMilli(),
	})
}

func TestHubBroadcastProtection(t *testing.T) {
	hub := GetHub()
	hub.BroadcastProtection("CooldownPeriod", "BTCUSDT", "block", "cooldown active")
}

func TestHubBroadcastSystem(t *testing.T) {
	hub := GetHub()
	hub.BroadcastSystem("restart", "system restarting for update")
}

func TestHubClientCount(t *testing.T) {
	hub := GetHub()
	count := hub.ClientCount()
	if count != 0 {
		t.Errorf("expected 0 clients, got %d", count)
	}
}

func TestClientSubscribe(t *testing.T) {
	c := &Client{
		subscriptions: map[string]bool{},
	}
	c.subscribe([]string{"price", "signal"})
	if !c.isSubscribed("price") {
		t.Error("expected subscribed to price")
	}
	if !c.isSubscribed("signal") {
		t.Error("expected subscribed to signal")
	}
	if c.isSubscribed("order") {
		t.Error("expected not subscribed to order")
	}
}

func TestClientUnsubscribe(t *testing.T) {
	c := &Client{
		subscriptions: map[string]bool{"price": true, "signal": true},
	}
	c.unsubscribe([]string{"price"})
	if c.isSubscribed("price") {
		t.Error("expected unsubscribed from price")
	}
	if !c.isSubscribed("signal") {
		t.Error("expected still subscribed to signal")
	}
}

func TestClientAllSubscription(t *testing.T) {
	c := &Client{
		subscriptions: map[string]bool{"all": true},
	}
	if !c.isSubscribed("price") {
		t.Error("all subscription should match any channel")
	}
	if !c.isSubscribed("signal") {
		t.Error("all subscription should match any channel")
	}
}
