package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ml"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── ML 训练闭环观测 API ─────────────────────────────────────────
// 闭环链路：retrainer 调度 → TrainingLoop（训练/降级 → 导出 → 热加载 → 落档）
//   GET /api/ml/training-runs  训练运行历史（指标/样本/特征/版本/耗时）
//   GET /api/ml/loop-status    闭环状态汇总（ml_server/调度/任务/模型/漂移/最近运行）
// 路由挂 AuthRequired（router.go registerMLRoutes），运行历史按任务属主过滤
// （普通用户见本人任务 + 无属主公共运行，admin 全量，与 retrain-jobs 惯例一致）。

var mlLoop struct {
	retrainer *ml.Retrainer
	loop      *ml.TrainingLoop
	registry  *ml.ModelRegistry
	serverMgr *ml.ServerManager
	runs      *store.MLTrainingRunRepo
}

// SetMLLoopDeps 注入闭环观测依赖（main 启动时调用；任一 nil 对应字段降级展示）。
func SetMLLoopDeps(retrainer *ml.Retrainer, loop *ml.TrainingLoop, registry *ml.ModelRegistry, serverMgr *ml.ServerManager, runs *store.MLTrainingRunRepo) {
	mlLoop.retrainer = retrainer
	mlLoop.loop = loop
	mlLoop.registry = registry
	mlLoop.serverMgr = serverMgr
	mlLoop.runs = runs
}

// MLTrainingRuns GET /api/ml/training-runs?model=&limit= —— 训练运行历史。
func MLTrainingRuns(c *gin.Context) {
	repo := mlLoop.runs
	if repo == nil {
		repo = store.NewMLTrainingRunRepo()
	}
	limit := queryInt(c, "limit", 50)
	if limit > 200 {
		limit = 200
	}
	runs, err := repo.List(listOwnerFilter(c), c.Query("model"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list training runs failed"})
		return
	}
	if runs == nil {
		runs = []*store.MLTrainingRun{}
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

// MLLoopStatus GET /api/ml/loop-status —— 闭环状态汇总。
func MLLoopStatus(c *gin.Context) {
	resp := gin.H{}

	// ml_server 进程状态（托管）+ 连通性（托管时复用管理器周期健康检查，
	// 非托管才现场探测——避免对端黑洞时挂住状态接口）
	server := gin.H{"url": MLClient.BaseURL()}
	managed := false
	if mlLoop.serverMgr != nil {
		st := mlLoop.serverMgr.Status()
		server["managed"] = st.Managed
		server["running"] = st.Running
		server["adopted"] = st.Adopted
		server["pid"] = st.PID
		server["restarts"] = st.Restarts
		server["healthy"] = st.Healthy
		server["last_error"] = st.LastError
		server["url"] = st.URL
		managed = st.Managed
	} else {
		server["managed"] = false
	}
	if managed {
		server["reachable"] = server["healthy"]
	} else if err := MLClient.Health(); err != nil {
		server["reachable"] = false
		server["healthy"] = false
		if server["last_error"] == nil || server["last_error"] == "" {
			server["last_error"] = err.Error()
		}
	} else {
		server["reachable"] = true
		server["healthy"] = true
	}
	resp["ml_server"] = server

	// 调度器状态
	if mlLoop.retrainer != nil {
		cfg := mlLoop.retrainer.Config()
		resp["retrainer"] = gin.H{
			"running":            mlLoop.retrainer.IsRunning(),
			"enabled":            cfg.Enabled,
			"check_interval_sec": int(cfg.CheckInterval / time.Second),
			"fallback_mode":      loopFallbackMode(),
		}
	} else {
		resp["retrainer"] = gin.H{"running": false, "enabled": false}
	}

	// 重训任务（含下次计划时间）
	now := time.Now().UnixMilli()
	jobsOut := make([]gin.H, 0)
	if jobs, err := mlRetrainRepo().ListJobs(listOwnerFilter(c), false); err == nil {
		for _, j := range jobs {
			var nextRunAt int64
			if j.Active {
				if j.LastRunAt == 0 {
					nextRunAt = now // 从未运行 = 立即到期
				} else {
					nextRunAt = j.LastRunAt + int64(j.IntervalMinutes)*60*1000
				}
			}
			jobsOut = append(jobsOut, gin.H{
				"id":               j.ID,
				"model_name":       j.ModelName,
				"active":           j.Active,
				"interval_minutes": j.IntervalMinutes,
				"last_run_at":      j.LastRunAt,
				"last_status":      j.LastStatus,
				"last_error":       j.LastError,
				"next_run_at":      nextRunAt,
			})
		}
	}
	resp["jobs"] = jobsOut

	// 已热加载模型（Go 原生推理注册表）
	models := make([]*ml.LoadedModel, 0)
	if mlLoop.registry != nil {
		models = mlLoop.registry.List()
	}
	resp["models_loaded"] = models

	// 最近一次闭环运行 + 漂移状态
	repo := mlLoop.runs
	if repo == nil {
		repo = store.NewMLTrainingRunRepo()
	}
	if last, err := repo.Latest(""); err == nil {
		resp["last_run"] = last
	} else if !errors.Is(err, sql.ErrNoRows) {
		resp["last_run"] = nil
	}
	drift := make([]map[string]any, 0)
	if mlLoop.loop != nil {
		drift = mlLoop.loop.DriftStatus()
	}
	resp["drift"] = drift

	c.JSON(http.StatusOK, resp)
}

// loopFallbackMode 闭环降级模式（loop 未注入返回空串）。
func loopFallbackMode() string {
	if mlLoop.loop == nil {
		return ""
	}
	return mlLoop.loop.FallbackMode
}
