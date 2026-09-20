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

// Sandbox 是策略沙箱的窄接口（runner 只依赖它，测试注入 fake）。
type Sandbox interface {
	// Load 加载策略源码：AST 校验 → 受限 exec → manifest 校验 → initialize。
	Load(ctx context.Context, code string, params map[string]any, symbol, interval string) (*Manifest, error)
	// OnBar 驱动一根 K 线；单轮超时由实现强制（超时即子进程死亡）。
	OnBar(ctx context.Context, bar model.Bar, state State) (*BarResult, error)
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

// rpc 发送一条请求并等待响应（调用方持有 s.mu，串行协议）。
func (s *SubprocessSandbox) rpc(method string, params map[string]any) (json.RawMessage, error) {
	if !s.started {
		return nil, ErrSandboxDead
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
	if !resp.OK {
		return nil, errors.New(resp.Error)
	}
	return resp.Result, nil
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
