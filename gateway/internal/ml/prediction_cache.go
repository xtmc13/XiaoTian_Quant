package ml

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Prediction Cache（预测落盘复用，对标 FreqAI 回测/hyperopt 加速）────
//
// Predictor 的缓存层：预测前按 (model_name, symbol, bar_time, features_hash)
// 查 xt_ml_predictions，命中直接返回；未命中走树模型推理并落库。
// 内存 FIFO 前挡缓存吸收回测/hyperopt 热循环里的重复查询，DB 做跨进程复用。
//
// features_hash：特征向量 key 排序后稳定序列化，sha1 取前 16 位 hex。

// PredictionKey 定位一条缓存预测所需的上下文。
type PredictionKey struct {
	ModelName string // 模型名（重训任务固定 model_id，重训后 InvalidateModel 失效）
	Symbol    string // 交易对
	BarTimeMs int64  // K 线收盘时间（毫秒）
}

// HashFeatures 对特征向量做稳定 hash：key 排序 + 定长格式化，sha1 取前 16 位 hex。
// 与 map 遍历顺序无关；特征值微小变化即产生不同 hash。
func HashFeatures(features map[string]float64) string {
	keys := make([]string, 0, len(features))
	for k := range features {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(strconv.FormatFloat(features[k], 'g', 12, 64))
	}
	sum := sha1.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:16]
}

// PredictionCache 预测缓存（内存 FIFO + SQLite 持久层）。
type PredictionCache struct {
	repo *store.MLPredictionRepo
	cap  int

	mu   sync.Mutex
	mem  map[string]float64
	fifo []string // 定长环形淘汰队列（len == cap 后复用）
	head int
}

// NewPredictionCache 创建缓存层；repo 可为 nil（纯内存，测试用）。
func NewPredictionCache(repo *store.MLPredictionRepo) *PredictionCache {
	const defaultCap = 100000
	return &PredictionCache{
		repo: repo,
		cap:  defaultCap,
		mem:  make(map[string]float64, defaultCap),
		fifo: make([]string, 0, defaultCap),
	}
}

var (
	defaultPredictionCacheOnce sync.Once
	defaultPredictionCache     *PredictionCache
)

// DefaultPredictionCache 进程级共享缓存（策略/回测路径直接用）。
func DefaultPredictionCache() *PredictionCache {
	defaultPredictionCacheOnce.Do(func() {
		defaultPredictionCache = NewPredictionCache(store.NewMLPredictionRepo())
	})
	return defaultPredictionCache
}

// Predict 带缓存的 PredictFromMap：命中直接返回，未命中计算并落库。
// key 不完整（无模型名/无 bar_time）或缓存层为 nil 时退化为裸推理。
func (c *PredictionCache) Predict(p *Predictor, key PredictionKey, features map[string]float64) (float64, error) {
	if p == nil {
		return 0, fmt.Errorf("prediction cache: nil predictor")
	}
	if c == nil || key.ModelName == "" || key.BarTimeMs <= 0 || len(features) == 0 {
		return p.PredictFromMap(features)
	}

	hash := HashFeatures(features)
	ck := key.ModelName + "|" + key.Symbol + "|" + strconv.FormatInt(key.BarTimeMs, 10) + "|" + hash

	if v, ok := c.memGet(ck); ok {
		return v, nil
	}
	if c.repo != nil {
		if v, ok, err := c.repo.Get(key.ModelName, key.Symbol, key.BarTimeMs, hash); err == nil && ok {
			c.memSet(ck, v)
			return v, nil
		}
	}

	v, err := p.PredictFromMap(features)
	if err != nil {
		return 0, err
	}
	if c.repo != nil {
		// 落库失败不影响预测结果（INSERT OR IGNORE，唯一约束去重）。
		_ = c.repo.Insert(key.ModelName, key.Symbol, key.BarTimeMs, hash, v)
	}
	c.memSet(ck, v)
	return v, nil
}

// InvalidateModel 模型重训成功后失效该模型全部缓存（内存 + DB）。
func (c *PredictionCache) InvalidateModel(modelName string) (int64, error) {
	if c == nil {
		return 0, nil
	}
	prefix := modelName + "|"
	c.mu.Lock()
	for k := range c.mem {
		if strings.HasPrefix(k, prefix) {
			delete(c.mem, k)
		}
	}
	c.fifo = c.fifo[:0]
	c.head = 0
	c.mu.Unlock()
	if c.repo != nil {
		return c.repo.DeleteForModel(modelName)
	}
	return 0, nil
}

func (c *PredictionCache) memGet(k string) (float64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.mem[k]
	return v, ok
}

func (c *PredictionCache) memSet(k string, v float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.mem[k]; ok {
		c.mem[k] = v
		return
	}
	if len(c.mem) >= c.cap {
		// 环形淘汰：覆盖最旧槽位。
		old := c.fifo[c.head]
		delete(c.mem, old)
		c.fifo[c.head] = k
		c.head = (c.head + 1) % c.cap
	} else {
		c.fifo = append(c.fifo, k)
	}
	c.mem[k] = v
}
