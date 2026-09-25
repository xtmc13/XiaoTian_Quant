package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Hyperopt epoch 回写 protection 空间参数 ─────────────────────
//
// protection 空间的 epoch 参数（protection__<Name>__<param>）回写时
// 不进策略顶层字段，而是合并进 config_json 的 protections 数组。

// TestApplyHyperoptEpochProtectionWriteBack 验证：
// 策略参数进顶层、protection 参数合并进 protections 数组（同名合并 params、
// 新 protection 追加）、diff 含 protections 项。
func TestApplyHyperoptEpochProtectionWriteBack(t *testing.T) {
	cfgID := "cfg-epoch-prot"
	seedStrategyConfig(t, cfgID, 1, `{
		"lookback": 10,
		"protections": [
			{"name": "StoplossGuard", "params": {"trade_limit": 4, "timeframe": "1h"}},
			{"name": "CooldownPeriod", "params": {"stop_duration_candles": 5}}
		]
	}`)
	seedEpoch(t, "ep-prot", cfgID, 1, `{
		"lookback": 20,
		"protection__StoplossGuard__trade_limit": 7,
		"protection__StoplossGuard__required_profit": 0.02,
		"protection__MaxDrawdown__max_drawdown_pct": 0.25
	}`)

	r := setupEpochRouter(1, "user")
	w, resp := applyRequest(t, r, "ep-prot")
	assertEq(t, w.Code, http.StatusOK, "apply status")
	assertValEq(t, resp["status"], "applied", "apply status body")

	// diff 应包含 protections 键
	diff, ok := resp["diff"].(map[string]any)
	assertTrue(t, ok, "diff should be an object")
	_, hasProtDiff := diff["protections"]
	assertTrue(t, hasProtDiff, "diff should contain protections entry")
	_, hasLookback := diff["lookback"]
	assertTrue(t, hasLookback, "diff should contain lookback entry")

	// 回读 config_json 验证
	rec, err := store.NewStrategyConfigRepo().GetByID(cfgID)
	assertTrue(t, err == nil && rec != nil, "config readable after apply")
	var cfg map[string]any
	assertTrue(t, json.Unmarshal([]byte(rec.ConfigJSON), &cfg) == nil, "config json parses")

	// 策略参数进顶层
	assertValEq(t, cfg["lookback"], float64(20), "strategy param at top level")
	// protection 参数不得泄漏到顶层
	_, leaked := cfg["protection__StoplossGuard__trade_limit"]
	assertTrue(t, !leaked, "protection dim must not leak to top level")

	// protections 数组合并
	prots, ok := cfg["protections"].([]any)
	assertTrue(t, ok, "protections should be array")
	assertEq(t, len(prots), 3, "StoplossGuard merged + CooldownPeriod kept + MaxDrawdown appended")

	byName := map[string]map[string]any{}
	for _, p := range prots {
		pm := p.(map[string]any)
		params, _ := pm["params"].(map[string]any)
		byName[pm["name"].(string)] = params
	}
	assertValEq(t, byName["StoplossGuard"]["trade_limit"], float64(7), "trade_limit overridden")
	assertValEq(t, byName["StoplossGuard"]["required_profit"], 0.02, "required_profit added")
	assertValEq(t, byName["StoplossGuard"]["timeframe"], "1h", "untouched base param preserved")
	assertValEq(t, byName["CooldownPeriod"]["stop_duration_candles"], float64(5), "other protection preserved")
	assertValEq(t, byName["MaxDrawdown"]["max_drawdown_pct"], 0.25, "new protection appended")
}

// TestApplyHyperoptEpochProtectionEmptyBase 验证 config_json 原本没有
// protections 字段时也能正确创建数组。
func TestApplyHyperoptEpochProtectionEmptyBase(t *testing.T) {
	cfgID := "cfg-epoch-prot-empty"
	seedStrategyConfig(t, cfgID, 1, `{"lookback": 10}`)
	seedEpoch(t, "ep-prot-empty", cfgID, 1, `{"protection__CooldownPeriod__stop_duration_candles": 9}`)

	r := setupEpochRouter(1, "user")
	w, _ := applyRequest(t, r, "ep-prot-empty")
	assertEq(t, w.Code, http.StatusOK, "apply status")

	rec, err := store.NewStrategyConfigRepo().GetByID(cfgID)
	assertTrue(t, err == nil && rec != nil, "config readable")
	var cfg map[string]any
	assertTrue(t, json.Unmarshal([]byte(rec.ConfigJSON), &cfg) == nil, "config json parses")
	assertValEq(t, cfg["lookback"], float64(10), "existing top-level key preserved")

	prots, ok := cfg["protections"].([]any)
	assertTrue(t, ok, "protections created")
	assertEq(t, len(prots), 1, "one protection appended")
	pm := prots[0].(map[string]any)
	assertValEq(t, pm["name"], "CooldownPeriod", "protection name")
	params := pm["params"].(map[string]any)
	assertValEq(t, params["stop_duration_candles"], float64(9), "protection param value")
}
