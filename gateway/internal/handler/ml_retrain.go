package handler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ml"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── ML 自动重训任务 REST（对标 FreqAI live_retrain_hours）────────────
// 路由挂 AuthRequired（router.go /ml 组），任务按属主过滤：
// 普通登录用户只见本人 + 历史无属主，admin 全量（与 reconcile 惯例一致）。
// 调度引擎由 main 启动时 SetMLRetrainer 注入。

var mlRetrainer *ml.Retrainer

// SetMLRetrainer 注入重训调度引擎（生产为 ml.Retrainer）。
func SetMLRetrainer(r *ml.Retrainer) { mlRetrainer = r }

func mlRetrainRepo() *store.MLRetrainRepo { return store.NewMLRetrainRepo() }

// normalizeFeatureSet feature_set 接受 JSON 对象或字符串，统一压成字符串存表。
func normalizeFeatureSet(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return "", err
		}
		return s, nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func retrainJobByID(c *gin.Context) (*store.MLRetrainJob, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job id"})
		return nil, false
	}
	job, err := mlRetrainRepo().GetJob(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "retrain job not found"})
			return nil, false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get job failed"})
		return nil, false
	}
	return job, true
}

// ListRetrainJobs GET /api/ml/retrain-jobs —— 任务列表（按属主过滤）。
func ListRetrainJobs(c *gin.Context) {
	jobs, err := mlRetrainRepo().ListJobs(listOwnerFilter(c), false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list jobs failed"})
		return
	}
	if jobs == nil {
		jobs = []*store.MLRetrainJob{}
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

// CreateRetrainJob POST /api/ml/retrain-jobs —— 新建重训任务。
// body: {"model_name":"BTCUSDT_1h","feature_set":{...},"interval_minutes":1440}
func CreateRetrainJob(c *gin.Context) {
	var req struct {
		ModelName       string          `json:"model_name" binding:"required"`
		FeatureSet      json.RawMessage `json:"feature_set"`
		IntervalMinutes int             `json:"interval_minutes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	featureSet, err := normalizeFeatureSet(req.FeatureSet)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "feature_set must be a JSON object or string"})
		return
	}
	interval := req.IntervalMinutes
	if interval <= 0 {
		interval = ml.DefaultRetrainIntervalMinutes() // 24h，对应 freqtrade live_retrain_hours 默认
	}
	id, err := mlRetrainRepo().CreateJob(getUserID(c), req.ModelName, featureSet, interval)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create job failed"})
		return
	}
	job, err := mlRetrainRepo().GetJob(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load job failed"})
		return
	}
	c.JSON(http.StatusOK, job)
}

// UpdateRetrainJob PUT /api/ml/retrain-jobs/:id —— 更新配置与启停。
// body 可含: model_name / feature_set / interval_minutes / active
func UpdateRetrainJob(c *gin.Context) {
	job, ok := retrainJobByID(c)
	if !ok {
		return
	}
	if !ownsResource(c, job.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"detail": "forbidden: not the resource owner"})
		return
	}
	var req struct {
		ModelName       *string          `json:"model_name"`
		FeatureSet      *json.RawMessage `json:"feature_set"`
		IntervalMinutes *int             `json:"interval_minutes"`
		Active          *bool            `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	patch := store.MLRetrainJobPatch{
		ModelName:       req.ModelName,
		IntervalMinutes: req.IntervalMinutes,
		Active:          req.Active,
	}
	if req.FeatureSet != nil {
		fs, err := normalizeFeatureSet(*req.FeatureSet)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "feature_set must be a JSON object or string"})
			return
		}
		patch.FeatureSet = &fs
	}
	updated, err := mlRetrainRepo().UpdateJob(job.ID, patch)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update job failed"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// RunRetrainJob POST /api/ml/retrain-jobs/:id/run —— 手动触发一次重训（同步等待结果）。
func RunRetrainJob(c *gin.Context) {
	job, ok := retrainJobByID(c)
	if !ok {
		return
	}
	if !ownsResource(c, job.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"detail": "forbidden: not the resource owner"})
		return
	}
	if mlRetrainer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "retrainer not ready"})
		return
	}
	run, err := mlRetrainer.RunJob(job.ID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"run": run})
}

// ListRetrainJobRuns GET /api/ml/retrain-jobs/:id/runs?limit= —— 最近运行记录。
func ListRetrainJobRuns(c *gin.Context) {
	job, ok := retrainJobByID(c)
	if !ok {
		return
	}
	if !ownsResource(c, job.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"detail": "forbidden: not the resource owner"})
		return
	}
	limit := queryInt(c, "limit", 20)
	if limit > 200 {
		limit = 200
	}
	runs, err := mlRetrainRepo().ListRuns(job.ID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list runs failed"})
		return
	}
	if runs == nil {
		runs = []*store.MLRetrainRun{}
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}
