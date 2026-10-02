package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentprofiles"
)

// ── 档案 REST（前端档案面板）──
// 普通用户：仅自己的档案空间；管理员：GET 附带 admin_all（全部用户的档案），
// 且可删除任意用户的非默认档案。会话与定时任务不做档案隔离——档案隔离的是长期知识
// （记忆/技能/看板）。

// profileJSON 组装契约字段：{"id","name","is_active","is_default","created_at"}。
func profileJSON(p agentprofiles.Profile, activeID int64) gin.H {
	return gin.H{
		"id":         p.ID,
		"name":       p.Name,
		"is_active":  p.ID == activeID,
		"is_default": p.IsDefault,
		"created_at": p.CreatedAt,
	}
}

// AgentProfilesList GET /api/agent/profiles
// 契约：{"success":true,"profiles":[...]}；管理员额外带 "admin_all"。
func AgentProfilesList(c *gin.Context) {
	repo := agentprofiles.NewRepo()
	uid := int64(aiBotUserID(c))
	profiles, activeID, err := repo.ListByUser(uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, profileJSON(p, activeID))
	}
	resp := gin.H{"success": true, "profiles": out}
	if c.GetString("role") == "admin" {
		all, err := repo.AdminListAll()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
			return
		}
		adminAll := make([]gin.H, 0, len(all))
		for _, u := range all {
			ps := make([]gin.H, 0, len(u.Profiles))
			for _, p := range u.Profiles {
				ps = append(ps, profileJSON(p, u.ActiveID))
			}
			adminAll = append(adminAll, gin.H{"user_id": u.UserID, "username": u.Username, "profiles": ps})
		}
		resp["admin_all"] = adminAll
	}
	c.JSON(http.StatusOK, resp)
}

type agentProfileCreateRequest struct {
	Name string `json:"name" binding:"required"`
}

// AgentProfileCreate POST /api/agent/profiles（契约：{"success":true,"id":2}）
func AgentProfileCreate(c *gin.Context) {
	var req agentProfileCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request: " + err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "档案名称不能为空"})
		return
	}
	id, err := agentprofiles.NewRepo().Create(int64(aiBotUserID(c)), name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "id": id})
}

// AgentProfileActivate POST /api/agent/profiles/:id/activate（契约：{"success":true}）
// 仅可激活自己的档案（管理员切换也走自己的档案空间）。
func AgentProfileActivate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid profile id"})
		return
	}
	if err := agentprofiles.NewRepo().Activate(int64(aiBotUserID(c)), id); err != nil {
		writeProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// AgentProfileDelete DELETE /api/agent/profiles/:id（契约：{"success":true}）
// 默认档案 400 不可删；删除激活档案后激活状态回落默认；管理员可删任意用户的非默认档案。
func AgentProfileDelete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid profile id"})
		return
	}
	isAdmin := c.GetString("role") == "admin"
	if err := agentprofiles.NewRepo().Delete(int64(aiBotUserID(c)), id, isAdmin); err != nil {
		writeProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// writeProfileError 档案错误映射：默认档案 400，不存在 404，其余 500。
func writeProfileError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, agentprofiles.ErrDefaultProfile):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "detail": err.Error()})
	case errors.Is(err, agentprofiles.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"success": false, "detail": "profile not found"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "detail": err.Error()})
	}
}
