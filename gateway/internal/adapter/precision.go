package adapter

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// ── Exchange trading-rule precision toolkit ────────────────────────────────
//
// 参考 freqtrade amount_to_precision/price_to_precision 与 ccxt precision 模式：
//   - 数量（amount/qty）按 stepSize 向下取整（TRUNCATE），避免四舍五入导致超仓；
//   - 价格（price）按 tickSize 取整（ROUND，half-up）；
//   - 全部用 math/big.Rat 精确十进制运算，杜绝 0.1+0.2 类浮点尾差进入下单串。
//
// 该工具包与交易所无关：Binance/OKX/Bybit 等适配器拿到各自交易对的
// step/tick/min 限制后共用这里的取整与校验逻辑。

// LotFilters 是一个交易对下单相关的交易所规则（字段均为交易所原始十进制串，
// 原样保留以避免 float64 解析引入的尾差）。
type LotFilters struct {
	StepSize    string // LOT_SIZE.stepSize：数量最小步进
	MinQty      string // LOT_SIZE.minQty：最小数量
	MaxQty      string // LOT_SIZE.maxQty：最大数量（可为空/0 表示不限制）
	TickSize    string // PRICE_FILTER.tickSize：价格最小步进
	MinPrice    string // PRICE_FILTER.minPrice（可为空/0）
	MaxPrice    string // PRICE_FILTER.maxPrice（可为空/0）
	MinNotional string // MIN_NOTIONAL/NOTIONAL：最小名义价值（计价货币，可为空/0）
}

// OrderConstraintError 表示下单参数在发给交易所之前被本地规则校验拒绝。
// 携带取整前/后的数值，便于策略侧定位仓位计算问题。
type OrderConstraintError struct {
	Field    string  // "quantity" / "price" / "notional"
	Raw      float64 // 策略给出的原始值
	Rounded  float64 // 取整后的值（notional 为计算出的名义价值）
	Limit    string  // 违反的限制（如 "minQty 0.001"）
	Exchange string  // 交易所/市场提示（如 "binance futures BTCUSDT"）
}

func (e *OrderConstraintError) Error() string {
	return fmt.Sprintf("%s: %s rejected before send: raw=%s rounded=%s violates %s",
		e.Exchange, e.Field, trimFloat(e.Raw), trimFloat(e.Rounded), e.Limit)
}

func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// parseDecimal 把交易所十进制串解析为精确有理数；空串视为 0。
func parseDecimal(s string) (*big.Rat, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return new(big.Rat), nil
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("invalid decimal %q", s)
	}
	return r, nil
}

// decimalPlaces 返回十进制串隐含的小数位数（"0.001"→3，"0.10"→1，"1"→0）。
// 兼容科学计数法（"1e-8"→8）。
func decimalPlaces(s string) int {
	s = strings.TrimSpace(s)
	if strings.IndexAny(s, "eE") >= 0 {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			s = strconv.FormatFloat(f, 'f', -1, 64)
		}
	}
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return len(strings.TrimRight(s[i+1:], "0"))
	}
	return 0
}

// stepEpsilon 是相对步进的取整容差：吸收 float64 尾差（如 0.3-1e-17），
// 避免把恰在步进边界上的值向下错砍一整个 step。1e-9 个 step 远小于
// 任何真实仓位的有意偏移，不会把“真的不足一步”的值抬上去。
const stepEpsilon = "0.000000001"

// quantize 是取整核心：value 按 step 量化为 step 的整数倍。
// mode<0 向下（floor，数量用），mode>0 四舍五入（half-up，价格用）。
// 返回精确十进制串（小数位与 step 对齐）与 float64 近似值。
func quantize(value float64, step string, mode int) (string, float64, error) {
	stepRat, err := parseDecimal(step)
	if err != nil {
		return "", 0, fmt.Errorf("step: %w", err)
	}
	if stepRat.Sign() <= 0 {
		return "", 0, fmt.Errorf("step must be positive, got %q", step)
	}
	// float64 → 最短可还原十进制串 → 精确有理数（保留原始二进制值）。
	valRat, ok := new(big.Rat).SetString(strconv.FormatFloat(value, 'f', -1, 64))
	if !ok {
		return "", 0, fmt.Errorf("invalid value %v", value)
	}

	// q = value / step（精确）；加 1e-9 容差吸收浮点尾差后再取整。
	q := new(big.Rat).Quo(valRat, stepRat)
	eps, _ := new(big.Rat).SetString(stepEpsilon)
	q.Add(q, eps)
	if mode > 0 {
		half := big.NewRat(1, 2)
		q.Add(q, half)
	}

	// n = floor(q)（big.Int 除法对负数为截断，需手动下调）
	num, den := q.Num(), q.Denom()
	n := new(big.Int).Quo(num, den) // 向零截断
	if q.Sign() < 0 && new(big.Int).Rem(num, den).Sign() != 0 {
		n.Sub(n, big.NewInt(1))
	}

	// result = n * step，精确有理数，按 step 的小数位输出十进制串。
	res := new(big.Rat).Mul(new(big.Rat).SetInt(n), stepRat)
	decimals := decimalPlaces(step)
	out := res.FloatString(decimals)
	f, _ := res.Float64()
	return out, f, nil
}

// FloorToStep 把数量向下取整为 stepSize 的整数倍（freqtrade amount_to_precision
// 的 TRUNCATE 语义）。返回的十进制串与 stepSize 精度对齐（如 step=0.001 → "0.123"）。
func FloorToStep(quantity float64, stepSize string) (string, float64, error) {
	return quantize(quantity, stepSize, -1)
}

// RoundToTick 把价格取整为 tickSize 的整数倍（nearest，half-up；
// freqtrade price_to_precision 默认 ROUND 语义）。
func RoundToTick(price float64, tickSize string) (string, float64, error) {
	return quantize(price, tickSize, 1)
}

// NormalizeQuantity 按交易规则校验并取整下单数量：
// 先 floor 到 stepSize，再检查 minQty/maxQty；不满足时返回 *OrderConstraintError，
// 调用方不得再把该单发给交易所（注定被拒）。stepSize 为空时只做范围检查。
func NormalizeQuantity(f *LotFilters, quantity float64, ctx string) (string, float64, error) {
	qtyStr := trimFloat(quantity)
	qty := quantity
	if f.StepSize != "" {
		var err error
		qtyStr, qty, err = FloorToStep(quantity, f.StepSize)
		if err != nil {
			return "", 0, fmt.Errorf("%s: quantity precision: %w", ctx, err)
		}
	}
	if qty <= 0 {
		return "", 0, &OrderConstraintError{
			Field: "quantity", Raw: quantity, Rounded: qty,
			Limit: "positive quantity", Exchange: ctx,
		}
	}
	if min, err := parseDecimal(f.MinQty); err == nil && min.Sign() > 0 {
		q, _ := new(big.Rat).SetString(strconv.FormatFloat(qty, 'f', -1, 64))
		if q.Cmp(min) < 0 {
			return "", 0, &OrderConstraintError{
				Field: "quantity", Raw: quantity, Rounded: qty,
				Limit: "minQty " + f.MinQty, Exchange: ctx,
			}
		}
	}
	if max, err := parseDecimal(f.MaxQty); err == nil && max.Sign() > 0 {
		q, _ := new(big.Rat).SetString(strconv.FormatFloat(qty, 'f', -1, 64))
		if q.Cmp(max) > 0 {
			return "", 0, &OrderConstraintError{
				Field: "quantity", Raw: quantity, Rounded: qty,
				Limit: "maxQty " + f.MaxQty, Exchange: ctx,
			}
		}
	}
	return qtyStr, qty, nil
}

// NormalizePrice 按 tickSize 取整价格并检查 min/maxPrice。
// tickSize 为空时原样返回（最短十进制）。
func NormalizePrice(f *LotFilters, price float64, ctx string) (string, float64, error) {
	priceStr := trimFloat(price)
	p := price
	if f.TickSize != "" {
		var err error
		priceStr, p, err = RoundToTick(price, f.TickSize)
		if err != nil {
			return "", 0, fmt.Errorf("%s: price precision: %w", ctx, err)
		}
	}
	if min, err := parseDecimal(f.MinPrice); err == nil && min.Sign() > 0 {
		r, _ := new(big.Rat).SetString(strconv.FormatFloat(p, 'f', -1, 64))
		if r.Cmp(min) < 0 {
			return "", 0, &OrderConstraintError{
				Field: "price", Raw: price, Rounded: p,
				Limit: "minPrice " + f.MinPrice, Exchange: ctx,
			}
		}
	}
	if max, err := parseDecimal(f.MaxPrice); err == nil && max.Sign() > 0 {
		r, _ := new(big.Rat).SetString(strconv.FormatFloat(p, 'f', -1, 64))
		if r.Cmp(max) > 0 {
			return "", 0, &OrderConstraintError{
				Field: "price", Raw: price, Rounded: p,
				Limit: "maxPrice " + f.MaxPrice, Exchange: ctx,
			}
		}
	}
	return priceStr, p, nil
}

// CheckMinNotional 校验 数量×价格 ≥ minNotional（计价货币）。
// price<=0（市价单无价格）时无法精确计算，跳过——由交易所侧最终把关。
func CheckMinNotional(f *LotFilters, qty, price float64, ctx string) error {
	min, err := parseDecimal(f.MinNotional)
	if err != nil || min.Sign() <= 0 || price <= 0 || qty <= 0 {
		return nil
	}
	q, _ := new(big.Rat).SetString(strconv.FormatFloat(qty, 'f', -1, 64))
	p, _ := new(big.Rat).SetString(strconv.FormatFloat(price, 'f', -1, 64))
	notional := new(big.Rat).Mul(q, p)
	if notional.Cmp(min) < 0 {
		nf, _ := notional.Float64()
		return &OrderConstraintError{
			Field: "notional", Raw: qty * price, Rounded: nf,
			Limit: "minNotional " + f.MinNotional, Exchange: ctx,
		}
	}
	return nil
}
