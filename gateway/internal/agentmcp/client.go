package agentmcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// 默认超时：握手/列工具 15s，工具调用 30s。
const (
	defaultHandshakeTimeout = 15 * time.Second
	defaultCallTimeout      = 30 * time.Second
)

// RemoteTool 远端 server 通过 tools/list 上报的工具描述。
type RemoteTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// rpcRequest / rpcResponse JSON-RPC 2.0 报文。
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Client 单个 MCP server 的 stdio 连接（每连接一把互斥锁串行化请求 id）。
type Client struct {
	cfg ServerConfig

	// HandshakeTimeout / CallTimeout 可注入覆盖（测试用短超时）；零值取默认。
	HandshakeTimeout time.Duration
	CallTimeout      time.Duration

	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	reader    *bufio.Reader
	nextID    int64
	restarted bool // 意外退出后只允许重启一次
}

// NewClient 建客户端（尚未拉起进程）。
func NewClient(cfg ServerConfig) *Client {
	return &Client{cfg: cfg}
}

// Name 服务名（工具名前缀用）。
func (c *Client) Name() string { return c.cfg.Name }

// Start 拉起子进程并完成 initialize 握手。
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.spawnLocked(); err != nil {
		return err
	}
	return c.initializeLocked(ctx)
}

// spawnLocked 拉起 server 子进程（调用方须持锁）。
func (c *Client) spawnLocked() error {
	cmd := exec.Command(c.cfg.Command, c.cfg.Args...)
	if len(c.cfg.Env) > 0 {
		cmd.Env = append(cmd.Environ(), c.cfg.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp %s: stdin pipe: %w", c.cfg.Name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp %s: stdout pipe: %w", c.cfg.Name, err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mcp %s: 启动失败: %w", c.cfg.Name, err)
	}
	c.cmd = cmd
	c.stdin = stdin
	c.reader = bufio.NewReader(stdout)
	c.nextID = 0
	return nil
}

// initializeLocked 发送 initialize 握手（调用方须持锁）。
func (c *Client) initializeLocked(ctx context.Context) error {
	_, err := c.doRPCLocked(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "xiaotian-quant-gateway", "version": "2.0.0"},
		"capabilities":    map[string]any{},
	}, c.handshakeTimeout())
	return err
}

// ListTools initialize 后的 tools/list。
func (c *Client) ListTools(ctx context.Context) ([]RemoteTool, error) {
	raw, err := c.callWithRestart(ctx, "tools/list", nil, c.handshakeTimeout())
	if err != nil {
		return nil, err
	}
	var result struct {
		Tools []RemoteTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("mcp %s: tools/list 解析失败: %w", c.cfg.Name, err)
	}
	return result.Tools, nil
}

// CallTool 转发 tools/call，返回 result 的 content 原文（JSON）。
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	return c.callWithRestart(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	}, c.callTimeout())
}

// callWithRestart 发一次请求；连接级失败（进程未启动/已死、写读断流、超时
// 已杀进程）时重启一次并重试。远端业务错误与 ctx 取消不触发重启。
func (c *Client) callWithRestart(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	raw, err := c.doRPCLocked(ctx, method, params, timeout)
	if err == nil {
		return raw, nil
	}
	var re *remoteError
	if errors.As(err, &re) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
		return nil, err
	}
	if c.restarted {
		return nil, fmt.Errorf("mcp %s: 重启后仍失败，不再重试: %w", c.cfg.Name, err)
	}
	// 重启一次：先收割旧子进程（可能已退出但未 Wait），再重放握手。
	c.restarted = true
	c.reapLocked()
	if serr := c.spawnLocked(); serr != nil {
		return nil, serr
	}
	if ierr := c.initializeLocked(ctx); ierr != nil {
		return nil, ierr
	}
	return c.doRPCLocked(ctx, method, params, timeout)
}

// deadLocked 进程未启动或连接已被判死（调用方须持锁）。
// 注：子进程退出后 ProcessState 要等 Wait 才有值，所以「死」主要靠
// 读 EOF / 写失败 / 超时后 killLocked 置空来判定，而非轮询进程状态。
func (c *Client) deadLocked() bool {
	return c.cmd == nil || c.stdin == nil
}

// reapLocked 收割可能已退出的旧子进程（调用方须持锁）。
func (c *Client) reapLocked() {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill() // 已退出则返回 error，无害
		_ = c.cmd.Wait()
	}
	c.cmd = nil
	c.stdin = nil
	c.reader = nil
}

// doRPCLocked 写请求行并读响应行（跳过通知/异 id 行）；超时杀进程，
// 让下一次调用走重启路径（调用方须持锁）。
func (c *Client) doRPCLocked(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	if c.deadLocked() {
		return nil, fmt.Errorf("mcp %s: 进程未运行", c.cfg.Name)
	}
	c.nextID++
	id := c.nextID
	reqBody, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if _, err := c.stdin.Write(append(reqBody, '\n')); err != nil {
		c.killLocked()
		return nil, fmt.Errorf("mcp %s: 写入失败: %w", c.cfg.Name, err)
	}

	type readResult struct {
		line []byte
		err  error
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		// 每轮迭代起一个新读协程（上一个已交付结果，串行无竞态）；
		// 超时/取消时杀进程，滞留读协程随管道 EOF 退出。
		lineCh := make(chan readResult, 1)
		go func() {
			line, err := c.reader.ReadBytes('\n')
			lineCh <- readResult{line, err}
		}()
		select {
		case <-ctx.Done():
			c.killLocked()
			return nil, ctx.Err()
		case <-timer.C:
			// 响应超时：杀进程使滞留读协程随 EOF 退出，下次调用触发重启。
			c.killLocked()
			return nil, fmt.Errorf("mcp %s: %s 超时（%v）", c.cfg.Name, method, timeout)
		case rr := <-lineCh:
			if rr.err != nil {
				return nil, fmt.Errorf("mcp %s: 读取响应失败: %w", c.cfg.Name, rr.err)
			}
			var resp rpcResponse
			if err := json.Unmarshal(rr.line, &resp); err != nil {
				continue // 非响应行（日志等），继续读
			}
			if !idMatches(resp.ID, id) {
				continue // 通知或乱序响应，继续读
			}
			if resp.Error != nil {
				return nil, &remoteError{server: c.cfg.Name, method: method, code: resp.Error.Code, msg: resp.Error.Message}
			}
			return resp.Result, nil
		}
	}
}

// killLocked 杀子进程并置空连接状态（调用方须持锁）。
func (c *Client) killLocked() { c.reapLocked() }

// remoteError 远端返回的 JSON-RPC 业务错误（不触发进程重启）。
type remoteError struct {
	server string
	method string
	code   int
	msg    string
}

func (e *remoteError) Error() string {
	return fmt.Sprintf("mcp %s: %s 远端错误 %d: %s", e.server, e.method, e.code, e.msg)
}

// idMatches 响应 id（JSON 数字解为 float64）与请求 id 匹配。
func idMatches(respID any, id int64) bool {
	switch v := respID.(type) {
	case float64:
		return int64(v) == id
	case int64:
		return v == id
	case json.Number:
		n, err := v.Int64()
		return err == nil && n == id
	}
	return false
}

func (c *Client) handshakeTimeout() time.Duration {
	if c.HandshakeTimeout > 0 {
		return c.HandshakeTimeout
	}
	return defaultHandshakeTimeout
}

func (c *Client) callTimeout() time.Duration {
	if c.CallTimeout > 0 {
		return c.CallTimeout
	}
	return defaultCallTimeout
}
