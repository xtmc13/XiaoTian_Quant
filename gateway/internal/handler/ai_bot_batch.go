package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// AIBotBatchStart starts multiple AI bot instances.
func AIBotBatchStart(c *gin.Context) {
	userID := aiBotUserID(c)
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		aiBotError(c, http.StatusBadRequest, "invalid json")
		return
	}
	started := 0
	for _, id := range body.IDs {
		item := store.GetAIBotInstanceByID(id, userID)
		if item == nil {
			continue
		}
		if getString(item, "status", "stopped") == "running" {
			continue
		}
		strategyItem := aiBotToStrategyConfig(item)
		if err := startAIBotInEngine(id, strategyItem); err != nil {
			continue
		}
		now := time.Now().Unix()
		item["status"] = "running"
		item["started_at"] = now
		item["updated_at"] = now
		item["error_message"] = ""
		store.SaveAIBotInstance(item)
		initialBalance := getFloat(item, "initial_balance", 10000)
		store.SaveAIBotSnapshot(id, initialBalance, 0, 0, 0)
		started++
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "started": started})
}

// AIBotBatchStop stops multiple AI bot instances.
func AIBotBatchStop(c *gin.Context) {
	userID := aiBotUserID(c)
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		aiBotError(c, http.StatusBadRequest, "invalid json")
		return
	}
	stopped := 0
	now := time.Now().Unix()
	for _, id := range body.IDs {
		item := store.GetAIBotInstanceByID(id, userID)
		if item == nil {
			continue
		}
		if getString(item, "status", "stopped") != "running" {
			continue
		}
		stopStrategyInEngine(id)
		resetPaperState(id)
		item["status"] = "stopped"
		item["stopped_at"] = now
		item["updated_at"] = now
		store.SaveAIBotInstance(item)
		stopped++
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "stopped": stopped})
}

// AIBotBatchDelete deletes multiple stopped AI bot instances.
func AIBotBatchDelete(c *gin.Context) {
	userID := aiBotUserID(c)
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		aiBotError(c, http.StatusBadRequest, "invalid json")
		return
	}
	deleted := 0
	for _, id := range body.IDs {
		item := store.GetAIBotInstanceByID(id, userID)
		if item == nil {
			continue
		}
		if getString(item, "status", "stopped") == "running" {
			continue
		}
		resetPaperState(id)
		store.DeleteAIBotInstance(id, userID)
		deleted++
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
}
