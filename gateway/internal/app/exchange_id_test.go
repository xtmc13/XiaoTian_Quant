package app

import (
	"encoding/json"
	"testing"
)

func TestStringifyOrderID(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"string trimmed", " 671122224734 ", "671122224734"},
		{"float64 exact", float64(671122224734), "671122224734"},
		{"json.Number", json.Number("671122224734"), "671122224734"},
		{"int64", int64(12345678901), "12345678901"},
	}
	for _, c := range cases {
		if got := stringifyOrderID(c.in); got != c.want {
			t.Fatalf("%s: stringifyOrderID(%v) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}
