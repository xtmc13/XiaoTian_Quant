package pystrat

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

//go:embed worker.py
var workerScript string

// 沙箱执行约束（v1）：
//   - 单轮 on_bar 调用超时 5s（Go 侧强制，超时 kill 子进程）
//   - 子进程内存上限 256MB（worker 内 POSIX RLIMIT_AS；非 POSIX 环境降级，见文档）
const (
	DefaultCallTimeout   = 5 * time.Second
	DefaultMemoryLimitMB = 256
)

// ErrSandboxDead 表示子进程已退出（崩溃/被杀/超时后被回收），调用方应重建沙箱。
var ErrSandboxDead = errors.New("pystrat: sandbox subprocess dead")

// Action 是策略经 context API 发出的一次动作。
type Action struct {
	Type   string  `json:"type"` // buy / sell / close_position / set_stop_loss / set_take_profit
	Qty    float64 `json:"qty,omitempty"`
	Amount float64 `json:"amount,omitempty"`
	Price  float64 `json:"price,omitempty"`
	Pct    float64 `json:"pct,omitempty"`
	// SkipStake 跳过 custom_stake_amount 钩子（v1.2）：adjust_trade_position
	// 产生的调整单金额是钩子显式返回值，不再被 stake 钩子二次覆盖。
	SkipStake bool `json:"-"`
}

// State 是每根 bar 前注入沙箱的持仓/权益快照。
type State struct {
	PositionQty      float64 `json:"position_qty"`
	PositionAvgPrice float64 `json:"position_avg_price"`
	PositionSide     string  `json:"position_side"` // long / short / ""
	HasPosition      bool    `json:"has_position"`
	Equity           float64 `json:"equity"`
}

// BarResult 是一轮 on_bar 的产出。
type BarResult struct {
	Actions []Action `json:"actions"`
	Logs    []string `json:"logs"`
	Prints  []string `json:"prints,omitempty"`
}

// OrderEvent 是推给沙箱 on_order 的订单回报（OMS model.OrderData 的投影，
// 仅含策略关心的字段；pnl 为订单已实现盈亏）。
type OrderEvent struct {
	ID        string  `json:"id"`
	Symbol    string  `json:"symbol"`
	Side      string  `json:"side"` // buy | sell
	Type      string  `json:"type"` // market | limit | ...
	Qty       float64 `json:"qty"`  // 委托量
	Price     float64 `json:"price"`
	Filled    float64 `json:"filled"`
	AvgPrice  float64 `json:"avg_price"`
	Status    string  `json:"status"` // new/partially_filled/filled/cancelled/rejected/...
	PnL       float64 `json:"pnl"`
	ClientOID string  `json:"client_oid,omitempty"`
}

// OrderResult 是一次 on_order 的产出（v1.1：回调内下单动作被丢弃并记日志，
// 仅 logs/prints 生效）。
type OrderResult struct {
	Logs   []string `json:"logs"`
	Prints []string `json:"prints,omitempty"`
}

// ErrOnOrderTimeout 表示单笔 on_order 调用超时。与 on_bar 超时策略不同：
// 不 kill 沙箱（订单事件容忍度高），事件记日志跳过，worker 内的迟到响应
// 由后续 rpc 的 id 匹配机制丢弃。
var ErrOnOrderTimeout = errors.New("pystrat: on_order timeout")

// Sandbox 是策略沙箱的窄接口（runner 只依赖它，测试注入 fake）。
type Sandbox interface {
	// Load 加载策略源码：AST 校验 → 受限 exec → manifest 校验 → initialize。
	Load(ctx context.Context, code string, params map[string]any, symbol, interval string) (*Manifest, error)
	// OnBar 驱动一根 K 线；单轮超时由实现强制（超时即子进程死亡）。
	OnBar(ctx context.Context, bar model.Bar, state State) (*BarResult, error)
	// OnOrder 分发一笔订单回报给 on_order 回调（策略未定义则跳过）。
	// 单轮超时不杀子进程，返回 ErrOnOrderTimeout。
	OnOrder(ctx context.Context, evt OrderEvent) (*OrderResult, error)
	// Close 杀进程并清理临时文件。
	Close() error
	// Alive 报告沙箱是否存活（诊断用）。
	Alive() bool
}

// SubprocessSandbox 是一个长驻 python3 子进程（stdin/stdout JSON-RPC 行协议）。
// 每个运行中的策略独占一个子进程（隔离性优先；进程池大小=运行中策略数）。
type SubprocessSandbox struct {
	PythonBin   string        // 默认 python3
	CallTimeout time.Duration // 单轮超时，默认 DefaultCallTimeout
	MemoryMB    int           // 记录用途（worker 内硬编码 256，保持一致）

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   *bufio.Writer
	stdout  *bufio.Reader
	dir     string
	nextID  int
	started bool
	// lateDone 非 nil 表示有 on_order 超时遗留的孤儿读取 goroutine 正在等待
	// 迟到响应；其关闭即"迟到响应已被消费"，下一条 rpc 才能开始读（维持
	// bufio.Reader 单读者约束，同时满足 on_order 超时不杀子进程）。
	lateDone chan struct{}
}

// NewSubprocessSandbox 创建沙箱（不启动子进程；首次 Load 时才拉进程）。
func NewSubprocessSandbox() *SubprocessSandbox {
	return &SubprocessSandbox{
		PythonBin:   "python3",
		CallTimeout: DefaultCallTimeout,
		MemoryMB:    DefaultMemoryLimitMB,
	}
}

// ensureStarted 惰性拉起 worker 子进程并把 worker.py 落到临时文件。
func (s *SubprocessSandbox) ensureStarted() error {
	if s.started {
		return nil
	}
	if s.dir == "" {
		dir, err := os.MkdirTemp("", "pystrat-*")
		if err != nil {
			return err
		}
		s.dir = dir
	}
	scriptPath := filepath.Join(s.dir, "worker.py")
	if _, err := os.Stat(scriptPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(scriptPath, []byte(workerScript), 0600); err != nil {
			return err
		}
	}

	bin := s.PythonBin
	if bin == "" {
		bin = "python3"
	}
	// -I 隔离模式：忽略用户 site-packages 与环境污染，进一步收窄导入面。
	cmd := exec.Command(bin, "-I", scriptPath)
	cmd.Dir = s.dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr // worker 协议外错误直接进网关日志
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("pystrat: start python sandbox: %w", err)
	}
	s.cmd = cmd
	s.stdin = bufio.NewWriter(stdin)
	s.stdout = bufio.NewReader(stdout)
	s.started = true
	return nil
}

// waitLateLocked 等待上一次 on_order 超时遗留的孤儿读取 goroutine 退出
// （持 s.mu 调用）。worker 彻底卡死时超时杀进程自愈——孤儿因管道关闭而
// 退出，不会泄漏。
func (s *SubprocessSandbox) waitLateLocked() error {
	if s.lateDone == nil {
		return nil
	}
	ch := s.lateDone
	select {
	case <-ch:
		s.lateDone = nil
		return nil
	case <-time.After(s.timeout()):
		s.markDead()
		s.lateDone = nil
		return fmt.Errorf("pystrat: wait late sandbox response timeout (%s): %w", s.timeout(), ErrSandboxDead)
	}
}

// armOrphanLocked 把已放弃等待的 on_order 调用转为孤儿读取：留守 goroutine
// 读到迟到响应（或进程死亡）后关闭 lateDone，后续 rpc 方能开始读——
// 以此维持 bufio.Reader 单读者约束且不杀子进程。
func (s *SubprocessSandbox) armOrphanLocked(done chan struct{}) {
	ch := make(chan struct{})
	prev := s.lateDone
	s.lateDone = ch
	go func() {
		if prev != nil {
			<-prev
		}
		<-done
		close(ch)
	}()
}

// readMatched 读取响应直到 id 匹配（防御性丢弃迟到响应）。致命错误时已
// markDead。调用方必须持有 s.mu 或保证协议串行。
func (s *SubprocessSandbox) readMatched(id int) (json.RawMessage, error) {
	for {
		respLine, err := s.stdout.ReadBytes('\n')
		if err != nil {
			s.markDead()
			return nil, ErrSandboxDead
		}
		var resp struct {
			ID     int             `json:"id"`
			OK     bool            `json:"ok"`
			Error  string          `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(respLine, &resp); err != nil {
			s.markDead()
			return nil, fmt.Errorf("pystrat: malformed sandbox response: %w", err)
		}
		if resp.ID != id {
			continue // 迟到响应（正常已被孤儿机制消费，这里是双保险）
		}
		if !resp.OK {
			return nil, errors.New(resp.Error)
		}
		return resp.Result, nil
	}
}

// rpc 发送一条请求并等待响应（调用方持有 s.mu，串行协议）。
func (s *SubprocessSandbox) rpc(method string, params map[string]any) (json.RawMessage, error) {
	if !s.started {
		return nil, ErrSandboxDead
	}
	if err := s.waitLateLocked(); err != nil {
		return nil, err
	}
	s.nextID++
	req, err := json.Marshal(map[string]any{"id": s.nextID, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if _, err := s.stdin.Write(req); err != nil {
		s.markDead()
		return nil, ErrSandboxDead
	}
	if err := s.stdin.WriteByte('\n'); err != nil {
		s.markDead()
		return nil, ErrSandboxDead
	}
	if err := s.stdin.Flush(); err != nil {
		s.markDead()
		return nil, ErrSandboxDead
	}
	return s.readMatched(s.nextID)
}

func (s *SubprocessSandbox) markDead() {
	s.started = false
	// 先捕获再置空：rpc 超时路径与 rpc 内失败路径会并发进入 markDead
	// （后者不持 s.mu），捕获到局部变量可避免二次读取时 s.cmd 已被置 nil
	// 导致的 nil 解引用/重复 Wait。
	cmd := s.cmd
	s.cmd = nil
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

func (s *SubprocessSandbox) timeout() time.Duration {
	if s.CallTimeout > 0 {
		return s.CallTimeout
	}
	return DefaultCallTimeout
}

// Load 见 Sandbox 接口。
func (s *SubprocessSandbox) Load(ctx context.Context, code string, params map[string]any, symbol, interval string) (*Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureStarted(); err != nil {
		return nil, err
	}
	type loadOut struct {
		raw json.RawMessage
		err error
	}
	done := make(chan loadOut, 1)
	go func() {
		raw, err := s.rpc("load", map[string]any{
			"code": code, "params": params, "symbol": symbol, "interval": interval,
		})
		done <- loadOut{raw, err}
	}()
	select {
	case out := <-done:
		if out.err != nil {
			if errors.Is(out.err, ErrSandboxDead) {
				return nil, out.err
			}
			return nil, fmt.Errorf("pystrat: load: %w", out.err)
		}
		var m Manifest
		if err := json.Unmarshal(out.raw, &m); err != nil {
			return nil, fmt.Errorf("pystrat: decode manifest: %w", err)
		}
		return &m, nil
	case <-ctx.Done():
		s.markDead()
		return nil, fmt.Errorf("pystrat: load: %w", ctx.Err())
	case <-time.After(s.timeout()):
		s.markDead()
		return nil, fmt.Errorf("pystrat: load timeout (%s): %w", s.timeout(), ErrSandboxDead)
	}
}

// OnBar 见 Sandbox 接口。
func (s *SubprocessSandbox) OnBar(ctx context.Context, bar model.Bar, state State) (*BarResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil, ErrSandboxDead
	}
	pos := map[string]any{}
	if state.HasPosition && state.PositionQty > 0 {
		pos["qty"] = state.PositionQty
		pos["avg_price"] = state.PositionAvgPrice
		pos["side"] = state.PositionSide
	}
	barMap := map[string]any{
		"time": bar.Time, "open": bar.Open, "high": bar.High,
		"low": bar.Low, "close": bar.Close, "volume": bar.Volume,
	}
	type barOut struct {
		res *BarResult
		err error
	}
	done := make(chan barOut, 1)
	go func() {
		raw, err := s.rpc("on_bar", map[string]any{
			"bar":   barMap,
			"state": map[string]any{"position": pos, "equity": state.Equity},
		})
		if err != nil {
			done <- barOut{nil, err}
			return
		}
		var br BarResult
		if err := json.Unmarshal(raw, &br); err != nil {
			done <- barOut{nil, fmt.Errorf("pystrat: decode bar result: %w", err)}
			return
		}
		done <- barOut{&br, nil}
	}()

	select {
	case out := <-done:
		if out.err != nil {
			if errors.Is(out.err, ErrSandboxDead) {
				return nil, out.err
			}
			return nil, fmt.Errorf("pystrat: on_bar: %w", out.err)
		}
		return out.res, nil
	case <-ctx.Done():
		s.markDead()
		return nil, fmt.Errorf("pystrat: on_bar: %w", ctx.Err())
	case <-time.After(s.timeout()):
		// 单轮超时：策略可能死循环。kill 子进程（协议已不可信），
		// 调用方拿到 ErrSandboxDead 后重建沙箱重载策略。
		s.markDead()
		return nil, fmt.Errorf("pystrat: on_bar timeout (%s): %w", s.timeout(), ErrSandboxDead)
	}
}

// OnOrder 见 Sandbox 接口。与 OnBar 的超时策略不同：订单事件容忍度高，
// 单轮超时**不杀**子进程——留守 goroutine（孤儿）读到迟到响应后自行退出，
// 后续 rpc 经 lateDone 等待维持单读者约束；worker 彻底卡死时由后续调用的
// waitLateLocked 超时杀进程自愈。
func (s *SubprocessSandbox) OnOrder(ctx context.Context, evt OrderEvent) (*OrderResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil, ErrSandboxDead
	}
	if err := s.waitLateLocked(); err != nil {
		return nil, err
	}
	s.nextID++
	id := s.nextID
	req, err := json.Marshal(map[string]any{"id": id, "method": "on_order", "params": map[string]any{"order": evt}})
	if err != nil {
		return nil, err
	}
	if _, err := s.stdin.Write(req); err != nil {
		s.markDead()
		return nil, ErrSandboxDead
	}
	if err := s.stdin.WriteByte('\n'); err != nil {
		s.markDead()
		return nil, ErrSandboxDead
	}
	if err := s.stdin.Flush(); err != nil {
		s.markDead()
		return nil, ErrSandboxDead
	}

	done := make(chan struct{})
	type orderOut struct {
		res *OrderResult
		err error
	}
	out := make(chan orderOut, 1)
	go func() {
		defer close(done)
		raw, err := s.readMatched(id)
		if err != nil {
			out <- orderOut{nil, err}
			return
		}
		var or OrderResult
		if err := json.Unmarshal(raw, &or); err != nil {
			out <- orderOut{nil, fmt.Errorf("pystrat: decode order result: %w", err)}
			return
		}
		out <- orderOut{&or, nil}
	}()

	select {
	case o := <-out:
		if o.err != nil {
			if errors.Is(o.err, ErrSandboxDead) {
				return nil, o.err
			}
			return nil, fmt.Errorf("pystrat: on_order: %w", o.err)
		}
		return o.res, nil
	case <-ctx.Done():
		s.armOrphanLocked(done)
		return nil, fmt.Errorf("pystrat: on_order: %w", ctx.Err())
	case <-time.After(s.timeout()):
		s.armOrphanLocked(done)
		return nil, fmt.Errorf("pystrat: on_order timeout (%s): %w", s.timeout(), ErrOnOrderTimeout)
	}
}

// ── v1.2 契约钩子（对标 freqtrade IStrategy；HookSandbox 接口见 runner.go）──

// HookResult 是钩子回调的统一产出：Value 携带金额类钩子的数值（custom_stake/
// adjust_position），Allow 携带确认/撤单类钩子的决定；Logs 是钩子内
// context.log 输出（runner 转发进运行日志）。
type HookResult struct {
	Value float64
	Allow bool
	Logs  []string
}

// hookRPC 是 v1.2 可选钩子的共用调用：与 OnBar 同一超时语义（单轮超时杀
// 子进程并返回 ErrSandboxDead，由 runner 走既有重建路径）。调用方持有
// s.mu，经 rpc() 串行化协议。
func (s *SubprocessSandbox) hookRPC(ctx context.Context, method string, params map[string]any, res *HookResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return ErrSandboxDead
	}
	type rpcOut struct {
		raw json.RawMessage
		err error
	}
	done := make(chan rpcOut, 1)
	go func() {
		raw, err := s.rpc(method, params)
		done <- rpcOut{raw, err}
	}()
	select {
	case o := <-done:
		if o.err != nil {
			if errors.Is(o.err, ErrSandboxDead) {
				return o.err
			}
			return fmt.Errorf("pystrat: %s: %w", method, o.err)
		}
		var decoded struct {
			Value  float64  `json:"amount"`
			Allow  bool     `json:"allow"`
			Cancel bool     `json:"cancel"`
			Logs   []string `json:"logs"`
			Prints []string `json:"prints"`
		}
		if err := json.Unmarshal(o.raw, &decoded); err != nil {
			return fmt.Errorf("pystrat: decode %s result: %w", method, err)
		}
		res.Value = decoded.Value
		res.Allow = decoded.Allow || decoded.Cancel
		res.Logs = append(decoded.Logs, prefixPrints(decoded.Prints)...)
		return nil
	case <-ctx.Done():
		s.markDead()
		return fmt.Errorf("pystrat: %s: %w", method, ctx.Err())
	case <-time.After(s.timeout()):
		s.markDead()
		return fmt.Errorf("pystrat: %s timeout (%s): %w", method, s.timeout(), ErrSandboxDead)
	}
}

// prefixPrints 把 print 捕获行加上前缀（与 runner 打印 prints 的口径一致）。
func prefixPrints(prints []string) []string {
	out := make([]string, 0, len(prints))
	for _, p := range prints {
		out = append(out, "print: "+p)
	}
	return out
}

// ConfirmTrade 询问 confirm_entry / confirm_exit（kind="entry"|"exit"）：
// 下单前最后一刻确认，Allow=false 表示策略否决。策略未定义钩子时
// worker 返回 allow=true（freqtrade 缺省行为）。
func (s *SubprocessSandbox) ConfirmTrade(ctx context.Context, kind, side string, price, amount float64) (HookResult, error) {
	var res HookResult
	err := s.hookRPC(ctx, "confirm", map[string]any{
		"kind": kind, "side": side, "price": price, "amount": amount,
	}, &res)
	return res, err
}

// CustomStake 询问 custom_stake_amount：Value>0 替换默认入场金额（USDT），
// Value=0（未定义/返回<=0）保持策略动作自带金额。
func (s *SubprocessSandbox) CustomStake(ctx context.Context, proposedAmount, price float64, side string) (HookResult, error) {
	var res HookResult
	err := s.hookRPC(ctx, "custom_stake", map[string]any{
		"proposed_amount": proposedAmount, "price": price, "side": side,
	}, &res)
	return res, err
}

// AdjustPosition 询问 adjust_trade_position（DCA）：Value>0 加仓 USDT 金额、
// Value<0 减仓、0 不调整。bar/position 投影与 OnBar 注入的口径一致。
func (s *SubprocessSandbox) AdjustPosition(ctx context.Context, bar model.Bar, qty, avgPrice float64, side string) (HookResult, error) {
	var res HookResult
	barMap := map[string]any{
		"time": bar.Time, "open": bar.Open, "high": bar.High,
		"low": bar.Low, "close": bar.Close, "volume": bar.Volume,
	}
	pos := map[string]any{"qty": qty, "avg_price": avgPrice, "side": side}
	err := s.hookRPC(ctx, "adjust_position", map[string]any{
		"bar": barMap, "position": pos,
	}, &res)
	return res, err
}

// CheckEntryTimeout 询问 check_entry_timeout：未成交入场挂单超时由策略决定
// 撤单行为——Allow=true 撤单（策略未定义时 worker 默认 true，同现有超时
// 逻辑），false 保留挂单。
func (s *SubprocessSandbox) CheckEntryTimeout(ctx context.Context, evt OrderEvent) (HookResult, error) {
	var res HookResult
	err := s.hookRPC(ctx, "entry_timeout", map[string]any{"order": evt}, &res)
	return res, err
}

// Close 杀进程、清临时目录；幂等。
func (s *SubprocessSandbox) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markDead()
	if s.dir != "" {
		_ = os.RemoveAll(s.dir)
		s.dir = ""
	}
	return nil
}

// Alive 报告子进程是否存活（诊断/状态接口用）。
func (s *SubprocessSandbox) Alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}
