package ml

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Training Loop（训练闭环编排器）───────────────────────────────
//
// 把 retrainer 的"到点触发训练"补成完整闭环：
//
//   数据准备（本地 K 线 store）
//     → 训练（ml_server HTTP；不可达时按 fallback 策略降级 Go 原生训练或跳过）
//     → 导出模型 JSON（python 路径走 /models/:id/export，Go 路径直接产出）
//     → 原子热加载进 ModelRegistry（旧模型读到最后一刻，崩溃可恢复）
//     → 运行档案落库 xt_ml_training_runs（指标/样本/特征/版本/耗时）
//     → 重建漂移参考分布（drift 触发重训的判据）
//
// TrainingLoop 实现 PipelineRunner 接口，作为 retrainer 的 runner 注入，
// 调度/告警/连续失败暂停仍由 retrainer 负责（本层只管单次闭环执行与记账）。
//
// 降级策略（ML_TRAIN_FALLBACK，ml_settings train_fallback 可覆盖）：
//   off（默认）: ml_server 不可达 → 记 skipped 档案并返回明确错误（retrainer 告警），不 panic
//   go         : 自动降级 Go 原生 stump 集成训练（GoFallbackTrainer），闭环不断流

// ModelExporter 模型导出窄接口（*Client 天然满足）。
type ModelExporter interface {
	ExportModel(modelID string) (*ExportedModel, error)
	Health() error
}

// driftState 一个模型的漂移监控状态。
type driftState struct {
	detector   *DriftDetector
	lastResult *DriftResult
	lastCheck  int64 // ms
}

// TrainingLoop 闭环编排器。
type TrainingLoop struct {
	pipeline *TrainingPipeline
	exporter ModelExporter
	registry *ModelRegistry
	runs     *store.MLTrainingRunRepo
	fallback *GoFallbackTrainer

	FallbackMode string // off | go

	mu    sync.Mutex
	drift map[string]*driftState
}

// NewTrainingLoop 组装闭环。fallbackMode 读取顺序：ml_settings train_fallback > ML_TRAIN_FALLBACK > off。
func NewTrainingLoop(pipeline *TrainingPipeline, exporter ModelExporter, registry *ModelRegistry, runs *store.MLTrainingRunRepo) *TrainingLoop {
	l := &TrainingLoop{
		pipeline: pipeline,
		exporter: exporter,
		registry: registry,
		runs:     runs,
		fallback: NewGoFallbackTrainer(),
		drift:    make(map[string]*driftState),
	}
	mode := ""
	if runs != nil {
		// ml_settings 覆盖走 MLRetrainRepo 的键值表（与 retrainer 配置同一来源）
		mode = store.NewMLRetrainRepo().GetSetting("train_fallback")
	}
	if mode == "" {
		mode = os.Getenv("ML_TRAIN_FALLBACK")
	}
	if mode == "" {
		mode = "off"
	}
	l.FallbackMode = mode
	return l
}

// Run 执行一次完整闭环（PipelineRunner 接口实现）。绝不 panic：
// 所有失败都收敛为 (result, error) 返回并由 record 落档。
func (l *TrainingLoop) Run(cfg PipelineConfig) (result *PipelineResult, err error) {
	start := time.Now()
	recorded := false
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("training loop panic: %v", rec)
			if result == nil {
				result = &PipelineResult{ModelID: cfg.ModelID, Symbol: cfg.Symbol, Error: err.Error()}
			}
			result.Success = false
			if !recorded {
				l.record(cfg, "unknown", result, "", err, time.Since(start))
			}
		}
	}()

	trainer := "python"
	var exported *ExportedModel

	result, err = l.pipeline.Run(cfg)

	// ml_server 不可达 → 按降级策略处理（数据不足等业务错误不降级，如实失败）
	if (err != nil || result == nil || !result.Success) && l.serverUnreachable() {
		switch l.FallbackMode {
		case "go":
			log.Printf("[ml-loop] ml_server 不可达，降级 Go 原生训练: model=%s", cfg.ModelID)
			result, exported, err = l.runGoFallback(cfg, start)
			trainer = "go_fallback"
		default:
			err = fmt.Errorf("ml_server 不可达且降级关闭（ML_TRAIN_FALLBACK=off），本轮跳过: %w", firstErr(err, result))
			if result == nil {
				result = &PipelineResult{ModelID: cfg.ModelID, Symbol: cfg.Symbol}
			}
			result.Success = false
			result.Error = err.Error()
			l.recordWith(cfg, "python", result, "", "skipped", err, time.Since(start))
			return result, err
		}
	}
	if err != nil || result == nil || !result.Success {
		l.record(cfg, trainer, result, "", firstErr(err, result), time.Since(start))
		return result, firstErr(err, result)
	}

	// 导出模型 JSON：python 路径从 ml_server 拉取，Go 路径训练时已产出
	if exported == nil {
		if l.exporter == nil {
			err = fmt.Errorf("训练成功但模型导出失败（闭环中断）: model exporter 未配置")
			result.Success = false
			result.Error = err.Error()
			l.record(cfg, trainer, result, "", err, time.Since(start))
			return result, err
		}
		exported, err = l.exporter.ExportModel(cfg.ModelID)
		if err != nil {
			err = fmt.Errorf("训练成功但模型导出失败（闭环中断）: %w", err)
			result.Success = false
			result.Error = err.Error()
			l.record(cfg, trainer, result, "", err, time.Since(start))
			return result, err
		}
	}

	// 原子热加载（含落盘）；registry 未配置时导出仅记档不加载（最小部署兼容）
	version := ""
	if l.registry != nil {
		var loaded *LoadedModel
		loaded, err = l.registry.HotLoad(cfg.ModelID, exported)
		if err != nil {
			err = fmt.Errorf("模型热加载失败: %w", err)
			result.Success = false
			result.Error = err.Error()
			l.record(cfg, trainer, result, "", err, time.Since(start))
			return result, err
		}
		version = loaded.Version
	}

	l.rebuildDriftReference(cfg.ModelID, result.FeatureSample)
	recorded = true
	l.record(cfg, trainer, result, version, nil, time.Since(start))
	log.Printf("[ml-loop] 闭环完成: model=%s trainer=%s version=%s samples=%d features=%d trigger=%s",
		cfg.ModelID, trainer, version, result.TrainSamples, result.FeaturesGenerated, cfg.Trigger)
	return result, nil
}

// runGoFallback Go 原生降级训练：数据准备复用 pipeline（本地 K 线 + 特征 + 标签），
// 训练走 GoFallbackTrainer，直接产出导出模型。
func (l *TrainingLoop) runGoFallback(cfg PipelineConfig, start time.Time) (*PipelineResult, *ExportedModel, error) {
	result := &PipelineResult{ModelID: cfg.ModelID, Symbol: cfg.Symbol}
	if l.pipeline == nil || l.pipeline.downloader == nil {
		err := fmt.Errorf("go fallback: 本地数据源不可用")
		result.Error = err.Error()
		return result, nil, err
	}

	bars, err := l.pipeline.loadBars(cfg)
	if err != nil {
		result.Error = err.Error()
		return result, nil, err
	}
	result.BarsLoaded = len(bars)
	if len(bars) < 100 {
		err := fmt.Errorf("insufficient data (< 100 bars)")
		result.Error = err.Error()
		return result, nil, err
	}

	featureBars, labels := l.pipeline.generateFeaturesAndLabels(bars, cfg.FeaturePeriods, cfg.LabelHorizon)
	if len(featureBars) == 0 {
		err := fmt.Errorf("no feature vectors generated")
		result.Error = err.Error()
		return result, nil, err
	}
	result.FeaturesGenerated = len(featureBars)

	featureNames := l.pipeline.getFeatureNames(cfg.FeaturePeriods)
	X := make([][]float64, 0, len(featureBars))
	y := make([]float64, 0, len(labels))
	for i, fb := range featureBars {
		row := make([]float64, len(featureNames))
		for j, name := range featureNames {
			if v, ok := fb[name].(float64); ok {
				row[j] = v
			}
		}
		X = append(X, row)
		y = append(y, labels[i])
	}

	exported, metrics, err := l.fallback.Train(cfg.ModelID, featureNames, X, y)
	if err != nil {
		result.Error = err.Error()
		return result, nil, err
	}

	split := int(float64(len(y)) * 0.8)
	result.Success = true
	result.TrainSamples = split
	result.TestSamples = len(y) - split
	result.Metrics = metrics
	result.FeatureNames = featureNames
	result.FeatureSample = sampleFeatureVectors(featureBars, featureNames, 1000)
	result.DurationMs = time.Since(start).Milliseconds()
	return result, exported, nil
}

// serverUnreachable ml_server 健康检查（exporter 为 nil 视为不可达）。
func (l *TrainingLoop) serverUnreachable() bool {
	if l.exporter == nil {
		return true
	}
	return l.exporter.Health() != nil
}

// rebuildDriftReference 训练成功后重建漂移参考分布（特征向量采样为空则跳过）。
func (l *TrainingLoop) rebuildDriftReference(modelID string, sample []map[string]float64) {
	if modelID == "" || len(sample) < DefaultDriftConfig().MinSamples {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.drift[modelID]
	if st == nil {
		st = &driftState{detector: NewDriftDetector(DefaultDriftConfig())}
		l.drift[modelID] = st
	}
	st.detector.BuildReference(sample)
	st.detector.ResetWindow()
}

// CheckJobDrift 对一个重训任务做漂移检查（retrainer 周期调用，DriftChecker 接口）。
// 加载最近 7 天 K 线计算特征，与最近训练参考分布比对 PSI；无参考分布时先建参考。
func (l *TrainingLoop) CheckJobDrift(job *store.MLRetrainJob) (bool, error) {
	if l.pipeline == nil || l.pipeline.downloader == nil || job == nil {
		return false, nil
	}
	cfg := JobPipelineConfig(job)
	cfg.LookbackDays = 7 // 漂移看近期窗口，与训练回看窗口解耦
	bars, err := l.pipeline.loadBars(cfg)
	if err != nil || len(bars) < 100 {
		return false, nil // 数据不足时不误报
	}
	featureBars, _ := l.pipeline.generateFeaturesAndLabels(bars, cfg.FeaturePeriods, cfg.LabelHorizon)
	featureNames := l.pipeline.getFeatureNames(cfg.FeaturePeriods)
	sample := sampleFeatureVectors(featureBars, featureNames, 500)
	if len(sample) < DefaultDriftConfig().MinSamples/2 {
		return false, nil
	}

	l.mu.Lock()
	st := l.drift[job.ModelName]
	if st == nil {
		st = &driftState{detector: NewDriftDetector(DefaultDriftConfig())}
		l.drift[job.ModelName] = st
	}
	if len(st.detector.reference) == 0 {
		st.detector.BuildReference(sample) // 尚无训练参考（如重启后），先建基线不误报
		st.lastCheck = time.Now().UnixMilli()
		l.mu.Unlock()
		return false, nil
	}
	st.detector.ResetWindow()
	for _, vec := range sample {
		st.detector.AddSample(vec)
	}
	res := st.detector.CheckDrift()
	st.lastResult = &res
	st.lastCheck = time.Now().UnixMilli()
	l.mu.Unlock()

	if res.Drifted {
		log.Printf("[ml-loop] 漂移检出: model=%s overall_psi=%.3f drifted_features=%d",
			job.ModelName, res.OverallPSI, len(res.DriftedFeatures))
	}
	return res.Drifted, nil
}

// DriftStatus 全部模型的最近漂移检查结果（loop-status API 用）。
func (l *TrainingLoop) DriftStatus() []map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]map[string]any, 0, len(l.drift))
	for modelID, st := range l.drift {
		item := map[string]any{
			"model_id":    modelID,
			"has_baseline": len(st.detector.reference) > 0,
			"checked_at":  st.lastCheck,
			"drifted":     false,
		}
		if st.lastResult != nil {
			item["drifted"] = st.lastResult.Drifted
			item["overall_psi"] = st.lastResult.OverallPSI
			item["drifted_features"] = st.lastResult.DriftedFeatures
		}
		out = append(out, item)
	}
	return out
}

// firstErr 从 (err, result) 提取首个非空错误。
func firstErr(err error, result *PipelineResult) error {
	if err != nil {
		return err
	}
	if result != nil && result.Error != "" {
		return fmt.Errorf("%s", result.Error)
	}
	if result == nil {
		return fmt.Errorf("pipeline returned nil result")
	}
	return nil
}

// record 按 result 状态落训练档案（success|failed）。
func (l *TrainingLoop) record(cfg PipelineConfig, trainer string, result *PipelineResult, version string, runErr error, elapsed time.Duration) {
	status := "success"
	if runErr != nil || result == nil || !result.Success {
		status = "failed"
	}
	l.recordWith(cfg, trainer, result, version, status, runErr, elapsed)
}

// recordWith 显式指定状态落档案；repo 为 nil 时只记日志（测试兼容）。
func (l *TrainingLoop) recordWith(cfg PipelineConfig, trainer string, result *PipelineResult, version, status string, runErr error, elapsed time.Duration) {
	run := &store.MLTrainingRun{
		JobID:        cfg.JobID,
		ModelName:    cfg.ModelID,
		TriggerSrc:   cfg.Trigger,
		Trainer:      trainer,
		Status:       status,
		Symbol:       cfg.Symbol,
		Interval:     cfg.Interval,
		ModelVersion: version,
		DurationMs:   elapsed.Milliseconds(),
	}
	if runErr != nil {
		run.Error = runErr.Error()
		if len(run.Error) > 500 {
			run.Error = run.Error[:500]
		}
	}
	if result != nil {
		run.BarsLoaded = result.BarsLoaded
		run.TrainSamples = result.TrainSamples
		run.TestSamples = result.TestSamples
		run.FeatureCount = result.FeaturesGenerated
		if result.Metrics != nil {
			if b, err := json.Marshal(result.Metrics); err == nil {
				run.MetricsJSON = string(b)
			}
		}
		if result.DurationMs > 0 {
			run.DurationMs = result.DurationMs
		}
	}
	if l.runs == nil {
		log.Printf("[ml-loop] training run repo 未注入，档案仅记日志: %+v", run)
		return
	}
	if _, err := l.runs.Record(run); err != nil {
		log.Printf("[ml-loop] record training run: %v", err)
	}
}
