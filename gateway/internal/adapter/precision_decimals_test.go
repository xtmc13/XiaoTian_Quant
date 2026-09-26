package adapter

import "testing"

/* ── FloorToDecimals / RoundToDecimals（按位数取整入口）─────────
   Kraken pair_decimals/lot_decimals、Alpaca 整股、IBKR 保守规整共用。 */

func TestFloorToDecimals(t *testing.T) {
	cases := []struct {
		name     string
		qty      float64
		decimals int
		want     string
	}{
		{"5dp floor", 0.1234567, 5, "0.12345"},
		{"4dp floor", 1.23456789, 4, "1.2345"},
		{"0dp integer floor", 10.9, 0, "10"},
		{"0dp exact", 3.0, 0, "3"},
		{"2dp floor", 19.999, 2, "19.99"},
		{"float tail 0.1+0.2", 0.1 + 0.2, 4, "0.3000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := FloorToDecimals(c.qty, c.decimals)
			btAssert(t, err == nil, "no error")
			btAssertEq(t, got, c.want, "decimal string")
		})
	}
}

func TestRoundToDecimals(t *testing.T) {
	cases := []struct {
		name     string
		price    float64
		decimals int
		want     string
	}{
		{"2dp half up", 190.256, 2, "190.26"},
		{"2dp rounds down", 190.254, 2, "190.25"},
		{"1dp half up", 50000.05, 1, "50000.1"},
		{"0dp", 99.6, 0, "100"},
		{"4dp", 0.56789, 4, "0.5679"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := RoundToDecimals(c.price, c.decimals)
			btAssert(t, err == nil, "no error")
			btAssertEq(t, got, c.want, "decimal string")
		})
	}
}

func TestDecimalsNegativeRejected(t *testing.T) {
	if _, _, err := FloorToDecimals(1.0, -1); err == nil {
		t.Fatal("negative decimals must error (FloorToDecimals)")
	}
	if _, _, err := RoundToDecimals(1.0, -2); err == nil {
		t.Fatal("negative decimals must error (RoundToDecimals)")
	}
}

func TestDecimalsStep(t *testing.T) {
	btAssertEq(t, decimalsStep(0), "1", "0dp")
	btAssertEq(t, decimalsStep(1), "0.1", "1dp")
	btAssertEq(t, decimalsStep(5), "0.00001", "5dp")
	btAssertEq(t, decimalsStep(-3), "1", "negative clamps to integer")
}
