package adapter

import (
	"errors"
	"math"
	"strings"
	"testing"
)

/* ── FloorToStep（数量向下取整）────────────────────────────── */

func TestFloorToStepTypicalSteps(t *testing.T) {
	cases := []struct {
		name  string
		qty   float64
		step  string
		want  string
		wantF float64
	}{
		{"step 0.001 truncates", 0.1234567, "0.001", "0.123", 0.123},
		{"step 0.01 truncates", 0.1234567, "0.01", "0.12", 0.12},
		{"step 1 integer qty", 2.9, "1", "2", 2},
		{"step 0.1 truncates", 0.1999999, "0.1", "0.1", 0.1},
		{"step with trailing zeros 0.01000000", 1.234567, "0.01000000", "1.23", 1.23},
		{"step 0.10 counts one decimal", 7.77, "0.10", "7.7", 7.7},
		{"step 5", 12.5, "5", "10", 10},
		{"step 0.00001", 0.000456789, "0.00001", "0.00045", 0.00045},
		{"exact multiple unchanged", 0.5, "0.001", "0.500", 0.5},
		{"large qty", 1234.56789, "0.001", "1234.567", 1234.567},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, gotF, err := FloorToStep(c.qty, c.step)
			btAssert(t, err == nil, "no error")
			btAssertEq(t, got, c.want, "decimal string")
			btAssertEq(t, gotF, c.wantF, "float value")
		})
	}
}

func TestFloorToStepFloatTails(t *testing.T) {
	// 0.1+0.2 = 0.30000000000000004：取整后必须是干净的 "0.3"，
	// 绝不能把浮点尾差串发给交易所。
	t.Run("0.1+0.2 tail", func(t *testing.T) {
		v := 0.1 + 0.2
		got, _, err := FloorToStep(v, "0.1")
		btAssert(t, err == nil, "no error")
		btAssertEq(t, got, "0.3", "float tail cleaned")
	})

	// 恰在步进边界下一个 ULP（如策略算出 0.3 但浮点表示略小）：
	// 1e-9 步进容差应吸收，不得砍掉一整步变成 0.2。
	t.Run("one ULP below boundary", func(t *testing.T) {
		v := math.Nextafter(0.3, 0) // 0.3 的下一个更小的 float64
		got, _, err := FloorToStep(v, "0.1")
		btAssert(t, err == nil, "no error")
		btAssertEq(t, got, "0.3", "ULP tail absorbed")
	})

	// 真正不足一步的值不得被容差抬上去：0.2999 step 0.1 → 0.2。
	t.Run("genuinely below step not bumped", func(t *testing.T) {
		got, _, err := FloorToStep(0.2999, "0.1")
		btAssert(t, err == nil, "no error")
		btAssertEq(t, got, "0.2", "no false bump")
	})
}

func TestFloorToStepInvalidStep(t *testing.T) {
	if _, _, err := FloorToStep(1.0, "0"); err == nil {
		t.Fatal("zero step must error")
	}
	if _, _, err := FloorToStep(1.0, "abc"); err == nil {
		t.Fatal("non-numeric step must error")
	}
	if _, _, err := FloorToStep(1.0, "-0.001"); err == nil {
		t.Fatal("negative step must error")
	}
}

/* ── RoundToTick（价格取整）───────────────────────────────── */

func TestRoundToTick(t *testing.T) {
	cases := []struct {
		name  string
		price float64
		tick  string
		want  string
	}{
		{"tick 0.10 half up", 50000.05, "0.10", "50000.1"},
		{"tick 0.10 rounds down", 50000.04, "0.10", "50000.0"},
		{"tick 0.0001", 0.123456789, "0.0001", "0.1235"},
		{"tick 0.5", 100.25, "0.5", "100.5"},
		{"tick 0.05 down", 99.97, "0.05", "99.95"},
		{"tick 1", 49999.6, "1", "50000"},
		{"float tail 0.1+0.2", 0.1 + 0.2, "0.1", "0.3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := RoundToTick(c.price, c.tick)
			btAssert(t, err == nil, "no error")
			btAssertEq(t, got, c.want, "decimal string")
		})
	}
}

/* ── NormalizeQuantity / NormalizePrice 校验拒绝 ──────────── */

func TestNormalizeQuantityRejects(t *testing.T) {
	ctx := "binance futures ETHUSDT"

	t.Run("below minQty after floor", func(t *testing.T) {
		f := &LotFilters{StepSize: "0.001", MinQty: "0.01"}
		_, _, err := NormalizeQuantity(f, 0.0099, ctx) // floor→0.009 < 0.01
		var ce *OrderConstraintError
		btAssert(t, errors.As(err, &ce), "OrderConstraintError")
		btAssertEq(t, ce.Field, "quantity", "field")
		btAssert(t, strings.Contains(err.Error(), "0.0099"), "raw value in message")
		btAssert(t, strings.Contains(err.Error(), "minQty"), "limit in message")
	})

	t.Run("floors to zero", func(t *testing.T) {
		f := &LotFilters{StepSize: "0.001", MinQty: "0.001"}
		_, _, err := NormalizeQuantity(f, 0.0009, ctx)
		var ce *OrderConstraintError
		btAssert(t, errors.As(err, &ce), "OrderConstraintError")
	})

	t.Run("zero or negative", func(t *testing.T) {
		f := &LotFilters{StepSize: "0.001"}
		if _, _, err := NormalizeQuantity(f, 0, ctx); err == nil {
			t.Fatal("zero qty must error")
		}
		if _, _, err := NormalizeQuantity(f, -0.5, ctx); err == nil {
			t.Fatal("negative qty must error")
		}
	})

	t.Run("above maxQty", func(t *testing.T) {
		f := &LotFilters{StepSize: "0.001", MaxQty: "10"}
		_, _, err := NormalizeQuantity(f, 10.5, ctx)
		var ce *OrderConstraintError
		btAssert(t, errors.As(err, &ce), "OrderConstraintError")
		btAssert(t, strings.Contains(err.Error(), "maxQty"), "limit in message")
	})

	t.Run("valid qty passes", func(t *testing.T) {
		f := &LotFilters{StepSize: "0.001", MinQty: "0.001", MaxQty: "1000"}
		s, v, err := NormalizeQuantity(f, 0.1234567, ctx)
		btAssert(t, err == nil, "no error")
		btAssertEq(t, s, "0.123", "string")
		btAssertEq(t, v, 0.123, "value")
	})
}

func TestNormalizePriceRejects(t *testing.T) {
	ctx := "binance spot BTCUSDT"
	f := &LotFilters{TickSize: "0.1", MinPrice: "1.0"}
	if _, _, err := NormalizePrice(f, 0.55, ctx); err == nil {
		t.Fatal("below minPrice must error")
	}
	f2 := &LotFilters{TickSize: "0.1", MaxPrice: "100"}
	if _, _, err := NormalizePrice(f2, 100.06, ctx); err == nil {
		t.Fatal("above maxPrice must error") // 100.06 → 100.1 > 100
	}
}

/* ── CheckMinNotional ─────────────────────────────────────── */

func TestCheckMinNotional(t *testing.T) {
	ctx := "binance futures BTCUSDT"
	f := &LotFilters{MinNotional: "5"}

	t.Run("below rejects", func(t *testing.T) {
		err := CheckMinNotional(f, 0.001, 1000, ctx) // notional=1 < 5
		var ce *OrderConstraintError
		btAssert(t, errors.As(err, &ce), "OrderConstraintError")
		btAssertEq(t, ce.Field, "notional", "field")
		btAssert(t, strings.Contains(err.Error(), "minNotional 5"), "limit in message")
	})

	t.Run("above passes", func(t *testing.T) {
		btAssert(t, CheckMinNotional(f, 0.001, 6000, ctx) == nil, "notional 6 >= 5")
	})

	t.Run("market order price=0 skipped", func(t *testing.T) {
		btAssert(t, CheckMinNotional(f, 0.001, 0, ctx) == nil, "no price → skip")
	})

	t.Run("no filter → skip", func(t *testing.T) {
		btAssert(t, CheckMinNotional(&LotFilters{}, 0.001, 1, ctx) == nil, "empty minNotional")
	})
}

/* ── decimalPlaces ────────────────────────────────────────── */

func TestDecimalPlaces(t *testing.T) {
	btAssertEq(t, decimalPlaces("0.001"), 3, "0.001")
	btAssertEq(t, decimalPlaces("0.10"), 1, "0.10")
	btAssertEq(t, decimalPlaces("0.01000000"), 2, "0.01000000")
	btAssertEq(t, decimalPlaces("1"), 0, "1")
	btAssertEq(t, decimalPlaces("1e-8"), 8, "1e-8")
	btAssertEq(t, decimalPlaces("100"), 0, "100")
}
