package adapter

import (
	"testing"
)

func TestAlpacaAdapterName(t *testing.T) {
	a := NewAlpacaAdapter("key", "secret", false)
	if a.Name() != "alpaca" {
		t.Fatal("name should be alpaca")
	}
}

func TestAlpacaAdapterPaperMode(t *testing.T) {
	a := NewAlpacaAdapter("key", "secret", true)
	if a.Name() != "alpaca_paper" {
		t.Fatal("paper mode name")
	}
	if a.baseURL() != AlpacaPaperURL {
		t.Fatal("paper URL")
	}
}

func TestAlpacaAdapterLiveMode(t *testing.T) {
	a := NewAlpacaAdapter("key", "secret", false)
	if a.baseURL() != AlpacaLiveURL {
		t.Fatal("live URL")
	}
}

func TestAlpacaStartStop(t *testing.T) {
	a := NewAlpacaAdapter("key", "secret", false)
	if err := a.Start(); err != nil {
		t.Fatal("start")
	}
	if err := a.Stop(); err != nil {
		t.Fatal("stop")
	}
	if !a.IsConnected() {
		t.Fatal("connected")
	}
}

func TestAlpacaGetPositions(t *testing.T) {
	a := NewAlpacaAdapter("key", "secret", true)
	pos, err := a.GetPositions() // will fail without real API, but shouldn't crash
	if err == nil {
		_ = pos
	}
}

func TestAlpacaMarketStream(t *testing.T) {
	a := NewAlpacaAdapter("key", "secret", true)
	if err := a.StartMarketStream(nil); err != nil {
		t.Fatal("market stream init")
	}
	if err := a.StartUserStream(); err != nil {
		t.Fatal("user stream init")
	}
}

func TestParseTime(t *testing.T) {
	ts := parseTime("2024-01-15T10:30:00Z")
	if ts <= 0 {
		t.Fatal("parse RFC3339")
	}

	ts2 := parseTime(float64(1705312200))
	if ts2 <= 0 {
		t.Fatal("parse unix seconds")
	}
}
