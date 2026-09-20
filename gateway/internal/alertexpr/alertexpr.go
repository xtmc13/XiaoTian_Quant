// Package alertexpr 实现指标信号告警的表达式引擎：
// tokenizer + 递归下降解析 + 对近期 K 线序列求值。
//
// 语法（面向用户，前端与迁移注释中同步展示）：
//
//	表达式 := 比较 ( (&& ||) 比较 )*
//	比较   := 算术 (== != > >= < <= 算术)?
//	算术   := 乘除 (('+' '-') 乘除)*
//	乘除   := 一元 (('*' '/') 一元)*
//	一元   := '-' 一元 | 原子
//	原子   := 数字 | 标识符 | 标识符 '(' 数字参数 ')' | '(' 表达式 ')'
//
// 支持的标识符：
//   - 裸 OHLCV: open / high / low / close / volume（最新一根的值）
//   - 指标函数: rsi ema sma atr macd macd_signal macd_hist
//     bb_upper bb_mid bb_lower bb_width bb_pctb
//     允许尾数简写（period 作为第一参数），如 rsi14、ema20、sma9、atr14、bb_upper20；
//     无尾数时用默认参数（rsi=14, ema/sma=20, atr=14, macd=12/26/9, bb=20/2）。
//     括号参数形式如 rsi(14)、bb_upper(20,2)、macd(12,26,9)。
//
// 示例：
//
//	rsi14 < 30 && close > ema20          （RSI 超卖且站上 EMA20）
//	ema12 > ema50 && close > ema50       （金叉突破）
//	(bb_upper(20,2)-bb_lower(20,2))/bb_mid(20,2) < 0.05   （布林收口）
//
// 安全约束：表达式长度上限 MaxExprLen 字节；只允许 ASCII 字母/数字/空白与
// 有限运算符字符，其余字符直接拒绝；token 数与嵌套深度均有硬上限；函数集为
// 固定白名单，参数只允许数字字面量（period 1..500，mult 0<mult<=10）。
package alertexpr

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// MaxExprLen 表达式最大长度（字节）。
const MaxExprLen = 500

const (
	maxTokens = 512
	maxDepth  = 60
	maxPeriod = 500
	maxMult   = 10.0
)

// ErrNoBars 求值时未传入 K 线。
var ErrNoBars = errors.New("alertexpr: no bars")

// Result 求值结果。Value 为首个比较式左操作数的当前值
// （没有比较式时为整个表达式的数值），供通知展示。
type Result struct {
	Matched bool
	Value   float64
}

// ── Tokenizer ──

type tokenKind int

const (
	tNum tokenKind = iota
	tIdent
	tComma // ,
	tAnd   // &&
	tOr    // ||
	tEq    // ==
	tNotEq // !=
	tGt    // >
	tGe    // >=
	tLt    // <
	tLe    // <=
	tPlus  // +
	tMinus // -
	tStar  // *
	tSlash // /
	tLParen
	tRParen
	tEnd
)

type token struct {
	kind  tokenKind
	num   float64
	ident string
	pos   int // 在源码中的字节偏移（报错定位用）
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

// tokenize 把表达式切为 token 序列；非法字符、单 & / | / = 都在这里拒绝。
func tokenize(src string) ([]token, error) {
	if len(src) == 0 {
		return nil, errors.New("empty expression")
	}
	if len(src) > MaxExprLen {
		return nil, fmt.Errorf("expression too long: %d > %d bytes", len(src), MaxExprLen)
	}
	var toks []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
			continue
		case isDigit(c) || (c == '.' && i+1 < len(src) && isDigit(src[i+1])):
			start := i
			for i < len(src) && (isDigit(src[i]) || src[i] == '.') {
				i++
			}
			if i < len(src) && (src[i] == 'e' || src[i] == 'E') {
				j := i + 1
				if j < len(src) && (src[j] == '+' || src[j] == '-') {
					j++
				}
				if j < len(src) && isDigit(src[j]) {
					i = j
					for i < len(src) && isDigit(src[i]) {
						i++
					}
				}
			}
			v, err := strconv.ParseFloat(src[start:i], 64)
			if err != nil {
				return nil, fmt.Errorf("invalid number at %d: %q", start, src[start:i])
			}
			toks = append(toks, token{kind: tNum, num: v, pos: start})
		case isAlpha(c):
			start := i
			for i < len(src) && (isAlpha(src[i]) || isDigit(src[i])) {
				i++
			}
			toks = append(toks, token{kind: tIdent, ident: strings.ToLower(src[start:i]), pos: start})
		default:
			tok, n := scanOperator(src, i)
			if tok.kind == tEnd { // 未识别（含单 & | = 与非 ASCII 字符）
				return nil, fmt.Errorf("illegal character at %d: %q", i, string(c))
			}
			tok.pos = i
			toks = append(toks, tok)
			i += n
		}
		if len(toks) > maxTokens {
			return nil, fmt.Errorf("expression too complex: more than %d tokens", maxTokens)
		}
	}
	toks = append(toks, token{kind: tEnd, pos: len(src)})
	return toks, nil
}

// scanOperator 识别运算符；无法识别返回 tEnd 表示失败（由上层报非法字符）。
func scanOperator(src string, i int) (token, int) {
	c := src[i]
	next := func() byte {
		if i+1 < len(src) {
			return src[i+1]
		}
		return 0
	}
	switch c {
	case '(':
		return token{kind: tLParen}, 1
	case ')':
		return token{kind: tRParen}, 1
	case ',':
		return token{kind: tComma}, 1
	case '+':
		return token{kind: tPlus}, 1
	case '-':
		return token{kind: tMinus}, 1
	case '*':
		return token{kind: tStar}, 1
	case '/':
		return token{kind: tSlash}, 1
	case '&':
		if next() == '&' {
			return token{kind: tAnd}, 2
		}
	case '|':
		if next() == '|' {
			return token{kind: tOr}, 2
		}
	case '=':
		if next() == '=' {
			return token{kind: tEq}, 2
		}
	case '!':
		if next() == '=' {
			return token{kind: tNotEq}, 2
		}
	case '>':
		if next() == '=' {
			return token{kind: tGe}, 2
		}
		return token{kind: tGt}, 1
	case '<':
		if next() == '=' {
			return token{kind: tLe}, 2
		}
		return token{kind: tLt}, 1
	}
	return token{kind: tEnd}, 0
}

// ── AST ──

type node interface {
	eval(ctx *evalCtx) (float64, error)
}

type numberNode struct{ v float64 }

func (n *numberNode) eval(*evalCtx) (float64, error) { return n.v, nil }

// callNode 是解析期已绑定到具体指标函数的原子节点。
type callNode struct {
	name string
	args []float64
	fn   func(*evalCtx, []float64) (float64, error)
}

func (n *callNode) eval(ctx *evalCtx) (float64, error) {
	v, err := n.fn(ctx, n.args)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", n.name, err)
	}
	if !isFinite(v) {
		return 0, fmt.Errorf("%s: non-finite result", n.name)
	}
	return v, nil
}

type unaryNode struct{ x node }

func (n *unaryNode) eval(ctx *evalCtx) (float64, error) {
	v, err := n.x.eval(ctx)
	if err != nil {
		return 0, err
	}
	return -v, nil
}

type binaryNode struct {
	op    tokenKind
	left  node
	right node
}

func (n *binaryNode) eval(ctx *evalCtx) (float64, error) {
	l, err := n.left.eval(ctx)
	if err != nil {
		return 0, err
	}
	r, err := n.right.eval(ctx)
	if err != nil {
		return 0, err
	}
	switch n.op {
	case tPlus:
		return l + r, nil
	case tMinus:
		return l - r, nil
	case tStar:
		return l * r, nil
	case tSlash:
		if r == 0 {
			return 0, errors.New("division by zero")
		}
		return l / r, nil
	case tAnd:
		if l != 0 && r != 0 {
			return 1, nil
		}
		return 0, nil
	case tOr:
		if l != 0 || r != 0 {
			return 1, nil
		}
		return 0, nil
	}
	return 0, fmt.Errorf("unknown operator %d", n.op)
}

// cmpNode 比较式；求值时把左操作数记入 ctx（通知展示用）。
type cmpNode struct {
	op    tokenKind
	left  node
	right node
}

func (n *cmpNode) eval(ctx *evalCtx) (float64, error) {
	l, err := n.left.eval(ctx)
	if err != nil {
		return 0, err
	}
	if !ctx.hasValue {
		ctx.value = l
		ctx.hasValue = true
	}
	r, err := n.right.eval(ctx)
	if err != nil {
		return 0, err
	}
	var ok bool
	switch n.op {
	case tEq:
		ok = l == r
	case tNotEq:
		ok = l != r
	case tGt:
		ok = l > r
	case tGe:
		ok = l >= r
	case tLt:
		ok = l < r
	case tLe:
		ok = l <= r
	default:
		return 0, fmt.Errorf("unknown comparison %d", n.op)
	}
	if ok {
		return 1, nil
	}
	return 0, nil
}

// ── Parser（递归下降） ──

type parser struct {
	toks []token
	pos  int
}

// Parse 校验并编译表达式；返回的 Expr 可并发复用（每次 Eval 新建上下文）。
func Parse(src string) (*Expr, error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	root, err := p.parseOr(0)
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEnd {
		return nil, fmt.Errorf("unexpected token at %d", p.peek().pos)
	}
	return &Expr{src: src, root: root}, nil
}

// Expr 是编译后的告警条件表达式。
type Expr struct {
	src  string
	root node
}

func (e *Expr) String() string { return e.src }

// Eval 对 K 线序列求值（最后一根视为最新），至少传 1 根。
func (e *Expr) Eval(bars []model.Bar) (Result, error) {
	if len(bars) == 0 {
		return Result{}, ErrNoBars
	}
	ctx := &evalCtx{bars: bars}
	v, err := e.root.eval(ctx)
	if err != nil {
		return Result{}, err
	}
	res := Result{Matched: v != 0}
	if ctx.hasValue {
		res.Value = ctx.value
	} else {
		res.Value = v
	}
	return res, nil
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token { t := p.toks[p.pos]; p.pos++; return t }

func (p *parser) parseOr(depth int) (node, error) {
	left, err := p.parseAnd(depth)
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tOr {
		p.next()
		right, err := p.parseAnd(depth)
		if err != nil {
			return nil, err
		}
		left = &binaryNode{op: tOr, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseAnd(depth int) (node, error) {
	left, err := p.parseCmp(depth)
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tAnd {
		p.next()
		right, err := p.parseCmp(depth)
		if err != nil {
			return nil, err
		}
		left = &binaryNode{op: tAnd, left: left, right: right}
	}
	return left, nil
}

// parseCmp 比较式不可链式（1<2<3 报错）。
func (p *parser) parseCmp(depth int) (node, error) {
	left, err := p.parseAdd(depth)
	if err != nil {
		return nil, err
	}
	switch p.peek().kind {
	case tEq, tNotEq, tGt, tGe, tLt, tLe:
		op := p.next().kind
		right, err := p.parseAdd(depth)
		if err != nil {
			return nil, err
		}
		return &cmpNode{op: op, left: left, right: right}, nil
	}
	return left, nil
}

func (p *parser) parseAdd(depth int) (node, error) {
	left, err := p.parseMul(depth)
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tPlus || p.peek().kind == tMinus {
		op := p.next().kind
		right, err := p.parseMul(depth)
		if err != nil {
			return nil, err
		}
		left = &binaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseMul(depth int) (node, error) {
	left, err := p.parseUnary(depth)
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tStar || p.peek().kind == tSlash {
		op := p.next().kind
		right, err := p.parseUnary(depth)
		if err != nil {
			return nil, err
		}
		left = &binaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseUnary(depth int) (node, error) {
	if depth >= maxDepth {
		return nil, fmt.Errorf("expression nested too deep (>%d)", maxDepth)
	}
	if p.peek().kind == tMinus {
		p.next()
		x, err := p.parseUnary(depth + 1)
		if err != nil {
			return nil, err
		}
		return &unaryNode{x: x}, nil
	}
	return p.parsePrimary(depth)
}

func (p *parser) parsePrimary(depth int) (node, error) {
	if depth >= maxDepth {
		return nil, fmt.Errorf("expression nested too deep (>%d)", maxDepth)
	}
	t := p.peek()
	switch t.kind {
	case tNum:
		p.next()
		return &numberNode{v: t.num}, nil
	case tLParen:
		p.next()
		inner, err := p.parseOr(depth + 1)
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tRParen {
			return nil, fmt.Errorf("missing ')' at %d", p.peek().pos)
		}
		p.next()
		return inner, nil
	case tIdent:
		p.next()
		return p.parseCall(t)
	}
	return nil, fmt.Errorf("unexpected token at %d", t.pos)
}

// parseCall 解析标识符为函数节点：支持裸标识符（open/close/rsi14 等
// 尾数简写）与括号数字参数列表 rsi(14) / bb_upper(20,2) / macd(12,26,9)。
// 尾数拆分为：标识符末尾的数字整体作为第一参数（bb_upper20 → bb_upper(20)）。
func (p *parser) parseCall(t token) (node, error) {
	name, trailing := splitIdent(t.ident)
	def, ok := funcRegistry[name]
	if !ok {
		return nil, fmt.Errorf("unknown identifier %q at %d", t.ident, t.pos)
	}
	var args []float64
	if p.peek().kind == tLParen {
		p.next()
		if p.peek().kind != tRParen {
			for {
				if p.peek().kind != tNum {
					return nil, fmt.Errorf("function %s: arguments must be numeric literals", name)
				}
				args = append(args, p.next().num)
				if p.peek().kind == tComma {
					p.next()
					continue
				}
				break
			}
		}
		if p.peek().kind != tRParen {
			return nil, fmt.Errorf("function %s: missing ')'", name)
		}
		p.next()
	} else if trailing > 0 {
		args = append(args, trailing)
	}
	if len(args) < def.minArgs || len(args) > len(def.defaults) {
		return nil, fmt.Errorf("function %s: expects %d..%d args, got %d",
			name, def.minArgs, len(def.defaults), len(args))
	}
	full := make([]float64, len(def.defaults))
	copy(full, def.defaults)
	copy(full, args)
	if err := def.check(full); err != nil {
		return nil, fmt.Errorf("function %s: %w", name, err)
	}
	return &callNode{name: name, args: full, fn: def.eval}, nil
}

// splitIdent 把标识符拆成 名字+尾数：rsi14 → (rsi, 14)，bb_upper20 → (bb_upper, 20)。
// 无尾数返回 trailing=0。名字部分未知时 trailing 无意义，由调用方报错。
func splitIdent(ident string) (string, float64) {
	i := len(ident)
	for i > 0 && ident[i-1] >= '0' && ident[i-1] <= '9' {
		i--
	}
	if i == len(ident) {
		return ident, 0
	}
	n, err := strconv.ParseFloat(ident[i:], 64)
	if err != nil {
		return ident, 0
	}
	return ident[:i], n
}

// ── 求值上下文 ──

type evalCtx struct {
	bars      []model.Bar
	closes    []float64
	hasCloses bool
	value     float64
	hasValue  bool
}

func (ctx *evalCtx) getCloses() []float64 {
	if !ctx.hasCloses {
		ctx.closes = make([]float64, len(ctx.bars))
		for i, b := range ctx.bars {
			ctx.closes[i] = b.Close
		}
		ctx.hasCloses = true
	}
	return ctx.closes
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
