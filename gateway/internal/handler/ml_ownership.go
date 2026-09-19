package handler

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

// ── ML 模型 / TensorBoard run 属主登记（H6/H10 越权修复）────────────────
//
// ML Server 是跨进程共享的，模型/run 元数据里暂时没有 user 维度
// （需在 ML Server 侧存属主，见审计报告 H6，留待后续波次）。
// 网关在本地登记「经由本网关创建」的模型与 run 属主：
//   - 已登记的资源按属主校验，非属主（且非 admin）一律 404（避免枚举）；
//   - 未登记的历史/共享资源保持原行为（所有人可见）。

var mlResourceOwners sync.Map // key: "<kind>:<id>" → int64 userID

// registerMLResourceOwner 登记模型/run 属主；id 为空或 userID<=0 时忽略。
func registerMLResourceOwner(kind, id string, userID int64) {
	if id == "" || userID <= 0 {
		return
	}
	mlResourceOwners.Store(kind+":"+id, userID)
}

// mlResourceOwner 返回已登记属主；未登记返回 0。
func mlResourceOwner(kind, id string) int64 {
	if v, ok := mlResourceOwners.Load(kind + ":" + id); ok {
		if uid, ok := v.(int64); ok {
			return uid
		}
	}
	return 0
}

// checkMLResourceAccess 校验 model/run 访问权限：
// 未登记（历史共享资源）、未注入用户、admin、属主本人放行；
// 否则写出 404 并返回 false。
func checkMLResourceAccess(c *gin.Context, kind, id string) bool {
	owner := mlResourceOwner(kind, id)
	if owner == 0 {
		return true
	}
	uid, injected := ctxUserID(c)
	if !injected || ctxIsAdmin(c) {
		return true
	}
	if int64(uid) == owner {
		return true
	}
	c.JSON(http.StatusNotFound, gin.H{"error": kind + " not found"})
	return false
}
