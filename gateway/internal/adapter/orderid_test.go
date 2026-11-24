package adapter

import "testing"

func TestExchangeClientOIDIsStableAndWithinLimits(t *testing.T) {
	id := "11111111-2222-3333-4444-555555555555"
	first := ExchangeClientOID(id)
	second := ExchangeClientOID(id)
	if first != second {
		t.Fatalf("not deterministic: %q vs %q", first, second)
	}
	if len(first) != 32 {
		t.Fatalf("length = %d, want 32", len(first))
	}
	if first[:2] != "xt" {
		t.Fatalf("prefix = %q", first[:2])
	}
	if ExchangeClientOID("") != "" {
		t.Fatal("empty order id should yield empty client id")
	}
	if ExchangeClientOID("other") == first {
		t.Fatal("different orders must not collide")
	}
}
