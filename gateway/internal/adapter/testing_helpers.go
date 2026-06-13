package adapter

import (
	"testing"
)

// btAssert is a shared test assertion helper.
func btAssert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

// btAssertEq is a shared type-safe equality assertion helper.
func btAssertEq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}
