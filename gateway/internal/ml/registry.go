package ml

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// ── Model Registry（导出模型的原子热加载注册表）─────────────────
//
// 闭环的"导出 → 加载"环节：训练完成后把 ml_server 导出的 JSON 树
//   1. 构建 Go 原生 Predictor（复用 Predictor.Load 校验）
//   2. 原子落盘（tmp + rename，崩溃不留半截文件）
//   3. 原子换入注册表（读侧无锁切换，预测中不会看到半更新模型）
// 网关重启后 LoadFromDir 恢复全部已加载模型。
// 模型版本 = 导出 JSON 的 sha256 前 12 位（热加载对账 / 闭环状态 API 用）。

// LoadedModel 一个已热加载模型的元信息。
type LoadedModel struct {
	ModelID   string `json:"model_id"`
	Version   string `json:"version"` // 导出 JSON hash 前 12 位
	ModelType string `json:"model_type"`
	TaskType  string `json:"task_type"`
	Features  int    `json:"features"`
	Trees     int    `json:"trees"`
	LoadedAt  int64  `json:"loaded_at"` // ms
}

// ModelRegistry 管理模型 ID → Go 原生 Predictor 的热加载映射。
type ModelRegistry struct {
	mu   sync.RWMutex
	dir  string // 导出 JSON 落盘目录；空串 = 只驻留内存（测试用）
	pred map[string]*Predictor
	info map[string]*LoadedModel
}

var modelIDSafe = regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`)

// NewModelRegistry 创建注册表。dir 非空时 HotLoad 会同步落盘，目录不存在自动创建。
func NewModelRegistry(dir string) *ModelRegistry {
	return &ModelRegistry{
		dir:  dir,
		pred: make(map[string]*Predictor),
		info: make(map[string]*LoadedModel),
	}
}

// HotLoad 原子热加载：先构建并校验 predictor、落盘，全部成功后才换入注册表。
// 任一步失败旧模型不受影响。
func (r *ModelRegistry) HotLoad(modelID string, exported *ExportedModel) (*LoadedModel, error) {
	if exported == nil {
		return nil, fmt.Errorf("model registry: nil exported model")
	}
	if modelID == "" {
		modelID = exported.ModelID
	}
	if modelID == "" {
		return nil, fmt.Errorf("model registry: empty model id")
	}
	if !modelIDSafe.MatchString(modelID) {
		return nil, fmt.Errorf("model registry: unsafe model id %q", modelID)
	}
	if len(exported.Trees) == 0 {
		return nil, fmt.Errorf("model registry: model %s has no trees", modelID)
	}

	data, err := json.Marshal(exported)
	if err != nil {
		return nil, fmt.Errorf("model registry: marshal %s: %w", modelID, err)
	}

	p := NewPredictor()
	if err := p.Load(data); err != nil {
		return nil, fmt.Errorf("model registry: load %s: %w", modelID, err)
	}

	sum := sha256.Sum256(data)
	loaded := &LoadedModel{
		ModelID:   modelID,
		Version:   hex.EncodeToString(sum[:])[:12],
		ModelType: exported.ModelType,
		TaskType:  exported.TaskType,
		Features:  len(exported.FeatureNames),
		Trees:     len(exported.Trees),
		LoadedAt:  time.Now().UnixMilli(),
	}

	// 先落盘（tmp + rename），成功后才换内存——磁盘与内存不一致时宁可不动内存
	if r.dir != "" {
		if err := r.persist(modelID, data); err != nil {
			return nil, err
		}
	}

	r.mu.Lock()
	r.pred[modelID] = p
	r.info[modelID] = loaded
	r.mu.Unlock()
	return loaded, nil
}

// persist tmp + rename 原子写导出 JSON。
func (r *ModelRegistry) persist(modelID string, data []byte) error {
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return fmt.Errorf("model registry: mkdir %s: %w", r.dir, err)
	}
	final := filepath.Join(r.dir, modelID+".json")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("model registry: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("model registry: rename %s: %w", final, err)
	}
	return nil
}

// Get 取模型的 predictor；未加载 ok=false。
func (r *ModelRegistry) Get(modelID string) (*Predictor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.pred[modelID]
	return p, ok
}

// Info 取模型的加载元信息；未加载 ok=false。
func (r *ModelRegistry) Info(modelID string) (*LoadedModel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.info[modelID]
	return info, ok
}

// List 列出全部已加载模型（按 model_id 排序）。
func (r *ModelRegistry) List() []*LoadedModel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*LoadedModel, 0, len(r.info))
	for _, info := range r.info {
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModelID < out[j].ModelID })
	return out
}

// LoadFromDir 启动恢复：扫描落盘目录加载全部导出 JSON，返回成功加载数。
// 单个文件损坏只记日志不影响其他模型。
func (r *ModelRegistry) LoadFromDir() int {
	if r.dir == "" {
		return 0
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[ml-registry] read dir %s: %v", r.dir, err)
		}
		return 0
	}
	loaded := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		modelID := e.Name()[:len(e.Name())-len(".json")]
		data, err := os.ReadFile(filepath.Join(r.dir, e.Name()))
		if err != nil {
			log.Printf("[ml-registry] read %s: %v", e.Name(), err)
			continue
		}
		var exported ExportedModel
		if err := json.Unmarshal(data, &exported); err != nil {
			log.Printf("[ml-registry] parse %s: %v", e.Name(), err)
			continue
		}
		if _, err := r.HotLoad(modelID, &exported); err != nil {
			log.Printf("[ml-registry] reload %s: %v", modelID, err)
			continue
		}
		loaded++
	}
	if loaded > 0 {
		log.Printf("[ml-registry] restored %d models from %s", loaded, r.dir)
	}
	return loaded
}
