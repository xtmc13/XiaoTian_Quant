package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/config"
)

// loadTempRiskConfig 用临时 config.yaml 覆盖全局配置（risk.position_limit_pct
// 为指定值），测试结束恢复默认全局配置，避免污染同包其他用例。
func loadTempRiskConfig(t *testing.T, positionLimit int) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.yaml")
	content := fmt.Sprintf("server:\n  port: \"8080\"\n  mode: release\nrisk:\n  position_limit_pct: %d\n", positionLimit)
	assertTrue(t, os.WriteFile(p, []byte(content), 0o644) == nil, "write temp config")
	_, err := config.Load(p)
	assertTrue(t, err == nil, "load temp config")
	t.Cleanup(func() { _, _ = config.Load("") })
}

func adminUnlock(c *gin.Context) {
	c.Set("user_id", int64(1))
	c.Set("role", "admin")
	UnlockLiveTrading(c)
}

// resetLiveOverride 解锁测试会写包级 liveTradingOverride，必须复位为
// 0（=无覆盖），否则同包后续用例会看到实盘已开闸。
func resetLiveOverride(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { atomic.StoreInt32(&liveTradingOverride, 0) })
}

func postUnlock(r *gin.Engine) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/trading/unlock", nil)
	r.ServeHTTP(w, req)
	return w
}

// C2.1 硬校验：position_limit_pct=2500（paper 放宽值，HANDOFF 红线事故值）
// 且无逃逸开关时，/trading/unlock 必须 400 拒绝，且解锁不得生效。
func TestUnlockRejectsRelaxedPositionLimit(t *testing.T) {
	loadTempRiskConfig(t, 2500)
	os.Unsetenv("XIAOTIAN_ALLOW_RELAXED_RISK")
	resetLiveOverride(t)

	r := setupRouter()
	r.POST("/trading/unlock", adminUnlock)

	w := postUnlock(r)
	assertEq(t, w.Code, http.StatusBadRequest, "relaxed limit must block unlock")

	var body map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &body) == nil, "parse error json")
	errMsg, _ := body["error"].(string)
	assertTrue(t, strings.Contains(errMsg, "2500"), "错误信息必须包含当前值, got "+errMsg)
	assertTrue(t, strings.Contains(errMsg, "非实盘安全值"), "错误信息必须说明非实盘安全值")
	assertTrue(t, strings.Contains(errMsg, "XIAOTIAN_ALLOW_RELAXED_RISK"), "错误信息必须说明逃逸开关")
	assertTrue(t, atomic.LoadInt32(&liveTradingOverride) == 0, "被拒后解锁不得生效")
}

// 边界值 100 是实盘安全值，解锁必须放行。
func TestUnlockAllowsPositionLimit100(t *testing.T) {
	loadTempRiskConfig(t, 100)
	os.Unsetenv("XIAOTIAN_ALLOW_RELAXED_RISK")
	resetLiveOverride(t)

	r := setupRouter()
	r.POST("/trading/unlock", adminUnlock)

	w := postUnlock(r)
	assertEq(t, w.Code, http.StatusOK, "position_limit_pct=100 必须放行")
	assertTrue(t, atomic.LoadInt32(&liveTradingOverride) == 1, "解锁必须生效")
}

// 放宽值 + 逃逸开关 XIAOTIAN_ALLOW_RELAXED_RISK=1 → 放行，但必须留痕（日志）。
func TestUnlockAllowsRelaxedWithEscapeHatch(t *testing.T) {
	loadTempRiskConfig(t, 2500)
	os.Setenv("XIAOTIAN_ALLOW_RELAXED_RISK", "1")
	t.Cleanup(func() { os.Unsetenv("XIAOTIAN_ALLOW_RELAXED_RISK") })
	resetLiveOverride(t)

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	r := setupRouter()
	r.POST("/trading/unlock", adminUnlock)

	w := postUnlock(r)
	assertEq(t, w.Code, http.StatusOK, "逃逸开关置位时必须放行")
	assertTrue(t, atomic.LoadInt32(&liveTradingOverride) == 1, "解锁必须生效")
	assertTrue(t, strings.Contains(buf.String(), "XIAOTIAN_ALLOW_RELAXED_RISK"),
		"逃逸开关放行必须留痕（日志）")
}
