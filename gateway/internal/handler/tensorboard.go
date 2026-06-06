package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ml"
)

// TensorBoardClient is the global TensorBoard service client.
var TensorBoardClient *ml.TensorBoardClient

func init() {
	TensorBoardClient = ml.NewTensorBoardClient("")
}

// ── TensorBoard Handlers ───────────────────────────────────────

// ListTensorBoardRuns returns all TensorBoard runs.
func ListTensorBoardRuns(c *gin.Context) {
	summary, err := TensorBoardClient.ListRuns()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, summary)
}

// QueryTensorBoardScalarsRequest configures a scalar query.
type QueryTensorBoardScalarsRequest struct {
	RunID    string   `json:"run_id" binding:"required"`
	Tags     []string `json:"tags,omitempty"`
	FromStep int      `json:"from_step,omitempty"`
	ToStep   int      `json:"to_step,omitempty"`
}

// QueryTensorBoardScalars queries scalar metrics for a run.
func QueryTensorBoardScalars(c *gin.Context) {
	var req QueryTensorBoardScalarsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := TensorBoardClient.QueryScalars(ml.TensorBoardQueryRequest{
		RunID:    req.RunID,
		Tags:     req.Tags,
		FromStep: req.FromStep,
		ToStep:   req.ToStep,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetTensorBoardRun returns details for a specific run.
func GetTensorBoardRun(c *gin.Context) {
	runID := c.Param("id")
	if runID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "run_id required"})
		return
	}

	run, err := TensorBoardClient.GetRun(runID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, run)
}

// DeleteTensorBoardRun removes a TensorBoard run.
func DeleteTensorBoardRun(c *gin.Context) {
	runID := c.Param("id")
	if runID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "run_id required"})
		return
	}
	if err := TensorBoardClient.DeleteRun(runID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "run_id": runID})
}
