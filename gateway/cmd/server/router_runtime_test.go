package main

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// TestRegisterStrategyRoutesNoConflict 注册策略路由（含 /configs/:id/runtime）
// 不得触发 gin 的 wildcard 冲突 panic。
func TestRegisterStrategyRoutesNoConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registerStrategyRoutes panicked: %v", r)
		}
	}()
	r := gin.New()
	api := r.Group("/api")
	registerStrategyRoutes(api)
}
