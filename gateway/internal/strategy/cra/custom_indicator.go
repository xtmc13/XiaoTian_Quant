package cra

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// 自定义开仓指标执行：open_indicator=custom 的策略由 CRA 引擎把用户保存的
// 指标代码交给指标沙箱执行，以最后一根 K 线的 buy/sell 信号作为开仓门槛。
// 沙箱不可用/执行出错时 fail-open（放行），保证交易连续性。

const customSignalCacheTTL = 45 * time.Second

var customSignalCache = struct {
	sync.Mutex
	m map[string]cacheEntry
}{m: make(map[string]cacheEntry)}

type cacheEntry struct {
	confirmed bool
	expiresAt time.Time
}

func sandboxBaseURL() string {
	if u := os.Getenv("SANDBOX_URL"); u != "" {
		return u
	}
	return "http://localhost:9000"
}

// customCodeID 从 indicator_params.custom 提取 code_id；缺失/非法返回 false。
func customCodeID(params map[string]any) (int64, bool) {
	if params == nil {
		return 0, false
	}
	sub, ok := params["custom"].(map[string]any)
	if !ok || sub == nil {
		return 0, false
	}
	switch v := sub["code_id"].(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	}
	return 0, false
}

// loadCustomIndicatorCode 按 id 读取指标源码；加密指标拒绝执行。
func loadCustomIndicatorCode(codeID int64) (string, error) {
	db := store.GetDB()
	if db == nil {
		return "", fmt.Errorf("database not available")
	}
	var code string
	var encrypted int
	err := db.QueryRow(`SELECT code, is_encrypted FROM indicator_codes WHERE id = ?`, codeID).Scan(&code, &encrypted)
	if err != nil {
		return "", err
	}
	if encrypted == 1 {
		return "", fmt.Errorf("indicator %d is encrypted", codeID)
	}
	return code, nil
}

type sandboxExecuteRequest struct {
	Code    string           `json:"code"`
	DfJSON  []map[string]any `json:"df_json,omitempty"`
	Params  map[string]any   `json:"params,omitempty"`
	Timeout int              `json:"timeout,omitempty"`
}

type sandboxExecuteResponse struct {
	Success bool           `json:"success"`
	Msg     string         `json:"msg"`
	Output  map[string]any `json:"output,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// executeCustomIndicator 调用沙箱执行指标代码，判断最后一根 K 线是否出现
// side 方向的入场信号。仅成功执行返回的信号会被上层缓存。
func executeCustomIndicator(code string, side PositionSide, bars []model.Bar) (bool, error) {
	if len(bars) == 0 {
		return true, nil
	}
	use := bars
	if len(use) > 200 {
		use = use[len(use)-200:]
	}
	df := make([]map[string]any, len(use))
	for i, b := range use {
		df[i] = map[string]any{
			"time":   b.Time,
			"open":   b.Open,
			"high":   b.High,
			"low":    b.Low,
			"close":  b.Close,
			"volume": b.Volume,
		}
	}
	body, err := json.Marshal(sandboxExecuteRequest{Code: code, DfJSON: df, Timeout: 20})
	if err != nil {
		return true, err
	}
	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Post(sandboxBaseURL()+"/execute", "application/json", bytes.NewReader(body))
	if err != nil {
		return true, err
	}
	defer resp.Body.Close()
	var result sandboxExecuteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return true, fmt.Errorf("decode sandbox response: %w", err)
	}
	if !result.Success {
		return true, fmt.Errorf("sandbox execute failed: %s %s", result.Msg, result.Error)
	}
	return lastBarSignalMatch(result.Output, side), nil
}

// lastBarSignalMatch 检查 output.signals 中最后一根 K 线的信号是否与方向匹配：
// 做多看最后一根 K 线的 buy 信号，做空看 sell 信号。
func lastBarSignalMatch(output map[string]any, side PositionSide) bool {
	if output == nil {
		return false
	}
	signals, ok := output["signals"].([]any)
	if !ok {
		return false
	}
	want := "buy"
	if side == SideShort {
		want = "sell"
	}
	for _, raw := range signals {
		sig, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := sig["type"].(string)
		if typ != want {
			continue
		}
		data, ok := sig["data"].([]any)
		if !ok || len(data) == 0 {
			continue
		}
		if data[len(data)-1] != nil {
			return true
		}
	}
	return false
}

// customOpenIndicatorConfirmed 带缓存的自定义开仓门槛：同一
// (code_id, symbol, bar_time, side) 在 TTL 内复用结果，避免每个 tick 都打沙箱。
// 沙箱/数据库错误不缓存，由调用方决定 fail-open 并记日志。
func customOpenIndicatorConfirmed(symbol string, codeID int64, side PositionSide, bars []model.Bar) (bool, error) {
	if len(bars) == 0 {
		return true, nil
	}
	last := bars[len(bars)-1]
	key := fmt.Sprintf("%d|%s|%d|%s", codeID, symbol, last.Time, side)
	now := time.Now()

	customSignalCache.Lock()
	if e, ok := customSignalCache.m[key]; ok && now.Before(e.expiresAt) {
		customSignalCache.Unlock()
		return e.confirmed, nil
	}
	customSignalCache.Unlock()

	code, err := loadCustomIndicatorCode(codeID)
	if err != nil {
		return true, err
	}
	confirmed, err := executeCustomIndicator(code, side, bars)
	if err != nil {
		return true, err
	}

	customSignalCache.Lock()
	if len(customSignalCache.m) > 512 {
		customSignalCache.m = make(map[string]cacheEntry)
	}
	customSignalCache.m[key] = cacheEntry{confirmed: confirmed, expiresAt: now.Add(customSignalCacheTTL)}
	customSignalCache.Unlock()
	return confirmed, nil
}
