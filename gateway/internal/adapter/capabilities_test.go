package adapter

import "testing"

func TestCapabilityRegistryMatchesImplementedAdapters(t *testing.T) {
	got := map[string]ExchangeCapabilities{}
	for _, c := range ExchangeCapabilityList() {
		got[c.Key] = c
	}
	for _, key := range []string{"binance", "okx", "bybit", "gate", "mexc", "bitget", "coinbase", "kraken"} {
		c, ok := got[key]
		if !ok {
			t.Fatalf("implemented venue %s missing from registry", key)
		}
		if c.Status != "active" {
			t.Errorf("%s status = %q, want active", key, c.Status)
		}
	}
	for _, key := range []string{"kucoin", "htx", "phemex", "deribit"} {
		if got[key].Status != "stub" {
			t.Errorf("%s status = %q, want stub", key, got[key].Status)
		}
	}
	if !got["binance"].Transfer {
		t.Error("binance transfer should be true")
	}
	if !got["binance"].Certified || !got["bybit"].Certified {
		t.Error("binance/bybit should be certified")
	}
	if hasSupport(got["gate"].Supports, "futures") {
		t.Error("gate only implements spot in this repo")
	}
}

func hasSupport(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func TestCapabilityLookupNormalizesAlias(t *testing.T) {
	c, ok := ExchangeCapability("gateio")
	if !ok || c.Key != "gate" {
		t.Fatalf("gateio alias lookup = %+v, %v", c, ok)
	}
	if _, ok := ExchangeCapability("unknown-venue"); ok {
		t.Fatal("unknown venue must not be found")
	}
}

func TestCapabilityListIsACopy(t *testing.T) {
	first := ExchangeCapabilityList()
	first[0].Supports[0] = "mutated"
	second := ExchangeCapabilityList()
	if second[0].Supports[0] == "mutated" {
		t.Fatal("registry was mutated through a returned slice")
	}
}
