package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
)

// ── 资源级 ownership 校验（多用户越权防护，P0 安全项）────────────────
//
// 鉴权中间件把登录用户写进 gin context（middleware.UserIDKey/RoleKey）。
// 本文件是所有资源 handler 共用的属主校验入口：
//
//   - 未注入用户（旧单用户部署/内部调用/单测直挂 handler）→ 一律放行，
//     保持既有行为，不引入 401/403 回归；
//   - admin → 放行全部；
//   - ownerUserID = 0 → 历史无属主/系统资源（webhook 单、系统通知规则），
//     对所有登录用户可见，保持单用户时代行为；
//   - 其余情况仅属主本人放行，否则统一 403。

// ctxUserID 取当前登录用户 id；第二个返回值为 context 是否注入了用户。
func ctxUserID(c *gin.Context) (int, bool) {
	v, exists := c.Get(middleware.UserIDKey)
	if !exists {
		return 0, false
	}
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	}
	return 0, false
}

// ctxUserRole 取当前登录用户角色（未注入返回空串）。
func ctxUserRole(c *gin.Context) string {
	if v, ok := c.Get(middleware.RoleKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getInt64Of 从 map 里读 int64（user_id 等属主字段的宽松读取，
// JSON 往返后是 float64，DB 直出是 int64）。
func getInt64Of(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		var n int64
		fmt.Sscanf(v, "%d", &n)
		return n
	}
	return 0
}

// ctxIsAdmin 判断当前请求是否 admin。未注入用户时按非 admin 处理，
// 但 requireOwner 在未注入时直接放行，单用户行为不变。
func ctxIsAdmin(c *gin.Context) bool {
	return ctxUserRole(c) == "admin"
}

// ownsResource 不写出响应的属主判定，供批量循环里逐条过滤使用
// （批量接口对无权属主的条目跳过/记失败，而不是整个请求 403）。
func ownsResource(c *gin.Context, ownerUserID int64) bool {
	uid, injected := ctxUserID(c)
	if !injected {
		// 未走鉴权中间件（单用户模式/内部调用）：保持现状放行。
		return true
	}
	if ctxIsAdmin(c) {
		return true
	}
	if ownerUserID == 0 {
		// 历史无属主/系统资源：所有登录用户可见（与单用户时代一致）。
		return true
	}
	return int64(uid) == ownerUserID
}

// requireOwner 校验当前请求能否访问属主为 ownerUserID 的资源。
// 不允许时已写出 403（{"detail": ...}，与相邻 handler 错误风格一致），
// 返回 false；允许时返回 true 且不写响应。
func requireOwner(c *gin.Context, ownerUserID int64) bool {
	if ownsResource(c, ownerUserID) {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{"detail": "forbidden: not the resource owner"})
	return false
}

// requireAdmin 校验当前请求是否 admin（全局系统资源的写操作使用）。
// 未注入用户时放行（单用户兼容）；非 admin 写出 403 并返回 false。
func requireAdmin(c *gin.Context) bool {
	_, injected := ctxUserID(c)
	if !injected {
		return true
	}
	if ctxIsAdmin(c) {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{"detail": "Admin access required"})
	return false
}
