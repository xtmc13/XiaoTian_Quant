package store

import (
	"testing"
)

/* Helpers */
func saAssert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

func saAssertEq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func TestCountFunctionsNoDB(t *testing.T) {
	// Without DB connection, all should return 0 gracefully
	saAssertEq(t, CountOrders(), 0, "orders")
	saAssertEq(t, CountPendingOrders(), 0, "pending")
	saAssertEq(t, CountTrades(), 0, "trades")
	saAssertEq(t, CountStrategies(), 0, "strategies")
	saAssertEq(t, CountActiveStrategies(), 0, "active strategies")
	saAssertEq(t, CountAuditLog(), 0, "audit log")
}

func TestRecentFunctionsNoDB(t *testing.T) {
	// Should return nil gracefully
	trades := GetRecentTrades(5)
	saAssert(t, trades == nil, "nil trades")

	events := GetRecentRiskEvents(5)
	saAssert(t, events == nil, "nil events")

	logs := GetAuditLog(5, 0)
	saAssert(t, logs == nil, "nil audit")
}

func TestSetUserActiveNoDB(t *testing.T) {
	err := SetUserActive("1", true)
	saAssert(t, err != nil, "should error without DB")
}

func TestAddAuditLogNoDB(t *testing.T) {
	// Should not panic
	AddAuditLog("admin", "test", "detail")
	// Just test it doesn't crash
}
