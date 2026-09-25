package ml

import (
	"fmt"
	"math"
	"sort"
)

// ── Go Fallback Trainer（ml_server 不可用时的原生简化训练路径）────
//
// 纯 Go 实现的决策树桩（depth=1）梯度提升：首棵常数树携带 y 均值
// （Go predictor 的 BaseScore 固定为 0，基线折进树里），后续每棵树
// 拟合残差、叶值预乘 learning_rate——导出语义与 predictor 的
// "树值求和"一致，与 sklearn GBDT 导出同一约定。
//
// 定位是降级兜底不是替代 LightGBM：模型能力有限（stump 无交互项），
// 但闭环不断流——server 恢复后下一轮 schedule 重训自动回到 python 路径。

// GoFallbackTrainer 决策树桩梯度提升训练器。
type GoFallbackTrainer struct {
	Trees         int     // 树桩数量（不含基线常数树），默认 32
	LearningRate  float64 // 收缩系数，默认 0.1
	MaxThresholds int     // 每个特征的候选切分点数，默认 16
	MinLeaf       int     // 叶节点最小样本数，默认 10
}

// NewGoFallbackTrainer 默认参数的降级训练器。
func NewGoFallbackTrainer() *GoFallbackTrainer {
	return &GoFallbackTrainer{Trees: 32, LearningRate: 0.1, MaxThresholds: 16, MinLeaf: 10}
}

func (t *GoFallbackTrainer) withDefaults() *GoFallbackTrainer {
	d := *t
	if d.Trees <= 0 {
		d.Trees = 32
	}
	if d.LearningRate <= 0 {
		d.LearningRate = 0.1
	}
	if d.MaxThresholds <= 0 {
		d.MaxThresholds = 16
	}
	if d.MinLeaf <= 0 {
		d.MinLeaf = 10
	}
	return &d
}

// Train 在特征矩阵上训练 stump 集成，返回可热加载的导出模型与训练指标。
// X 行与 y 一一对应，列序与 featureNames 对齐；时序语义按输入顺序（尾部 20% 为测试集）。
func (t *GoFallbackTrainer) Train(modelID string, featureNames []string, X [][]float64, y []float64) (*ExportedModel, map[string]any, error) {
	cfg := t.withDefaults()
	if len(X) == 0 || len(X) != len(y) {
		return nil, nil, fmt.Errorf("go fallback trainer: bad dataset (%d rows, %d labels)", len(X), len(y))
	}
	if len(featureNames) == 0 {
		return nil, nil, fmt.Errorf("go fallback trainer: no features")
	}
	n := len(X)
	for i, row := range X {
		if len(row) != len(featureNames) {
			return nil, nil, fmt.Errorf("go fallback trainer: row %d has %d features, want %d", i, len(row), len(featureNames))
		}
	}

	// 基线：首棵常数树 = 训练段 y 均值
	base := 0.0
	for _, v := range y {
		base += v
	}
	base /= float64(n)

	residual := make([]float64, n)
	for i := range y {
		residual[i] = y[i] - base
	}

	trees := make([]TreeNode, 0, cfg.Trees+1)
	trees = append(trees, TreeNode{Leaf: &base})

	// 每轮选 SSE 增益最大的 (feature, threshold) 树桩
	for round := 0; round < cfg.Trees; round++ {
		stump, gains := t.bestStump(featureNames, X, residual, cfg)
		if stump == nil {
			break // 无有效切分（如残差已为 0 或样本太薄）
		}
		trees = append(trees, *stump)
		// 更新残差（叶值已含 learning_rate）
		for i, row := range X {
			residual[i] -= predictStump(stump, row, gains.featureIdx)
		}
	}

	// 指标：尾部 20% 为测试段（与 pipeline 时序切分一致）
	split := int(float64(n) * 0.8)
	if split >= n {
		split = n - 1
	}
	if split < 1 {
		split = 1
	}
	trainRMSE, trainR2 := evalEnsemble(trees, featureNames, X[:split], y[:split])
	testRMSE, testR2 := evalEnsemble(trees, featureNames, X[split:], y[split:])
	dirAcc := directionalAccuracy(trees, featureNames, X[split:], y[split:])

	exported := &ExportedModel{
		Success:      true,
		ModelID:      modelID,
		ModelType:    "go_gbdt_stumps",
		TaskType:     "regression",
		FeatureNames: featureNames,
		Trees:        trees,
	}
	metrics := map[string]any{
		"train_rmse": trainRMSE,
		"train_r2":   trainR2,
		"test_rmse":  testRMSE,
		"test_r2":    testR2,
		"test_directional_accuracy": dirAcc,
	}
	return exported, metrics, nil
}

// stumpGain 记录一次最优切分的元信息（拟合后更新残差用）。
type stumpGain struct {
	featureIdx int
	sse        float64
}

// bestStump 搜索全特征 × 分位数候选阈值的最优树桩；叶值 = 侧内残差均值 × lr。
// 无满足 MinLeaf 约束的切分时返回 nil。
func (t *GoFallbackTrainer) bestStump(featureNames []string, X [][]float64, residual []float64, cfg *GoFallbackTrainer) (*TreeNode, *stumpGain) {
	n := len(X)
	bestSSE := math.Inf(1)
	var best *TreeNode
	var bestGain *stumpGain

	baseSSE := 0.0
	for _, r := range residual {
		baseSSE += r * r
	}

	for fi, name := range featureNames {
		// 收集该特征取值并排序，取分位数候选阈值
		vals := make([]float64, n)
		for i, row := range X {
			vals[i] = row[fi]
		}
		sorted := make([]float64, n)
		copy(sorted, vals)
		sort.Float64s(sorted)

		for k := 1; k <= cfg.MaxThresholds; k++ {
			thr := sorted[k*(n-1)/(cfg.MaxThresholds+1)]
			var sumL, sumR float64
			var cntL, cntR int
			for i, row := range X {
				if row[fi] <= thr {
					sumL += residual[i]
					cntL++
				} else {
					sumR += residual[i]
					cntR++
				}
			}
			if cntL < cfg.MinLeaf || cntR < cfg.MinLeaf {
				continue
			}
			meanL, meanR := sumL/float64(cntL), sumR/float64(cntR)
			sse := 0.0
			for i, row := range X {
				var d float64
				if row[fi] <= thr {
					d = residual[i] - meanL
				} else {
					d = residual[i] - meanR
				}
				sse += d * d
			}
			if sse < bestSSE {
				leafL, leafR := meanL*cfg.LearningRate, meanR*cfg.LearningRate
				bestSSE = sse
				best = &TreeNode{
					Feature:   name,
					Threshold: thr,
					Left:      &TreeNode{Leaf: &leafL},
					Right:     &TreeNode{Leaf: &leafR},
				}
				bestGain = &stumpGain{featureIdx: fi, sse: sse}
			}
		}
	}

	// 切分无增益（相对不切的 SSE）时提前停止
	if best == nil || bestGain.sse >= baseSSE {
		return nil, nil
	}
	return best, bestGain
}

// predictStump 单棵树桩预测（叶值已含 learning_rate）。
func predictStump(stump *TreeNode, row []float64, featureIdx int) float64 {
	if featureIdx < 0 || featureIdx >= len(row) {
		return 0
	}
	if row[featureIdx] <= stump.Threshold && stump.Left != nil && stump.Left.Leaf != nil {
		return *stump.Left.Leaf
	}
	if stump.Right != nil && stump.Right.Leaf != nil {
		return *stump.Right.Leaf
	}
	return 0
}

// evalEnsemble 在数据段上评估 (RMSE, R²)。
func evalEnsemble(trees []TreeNode, featureNames []string, X [][]float64, y []float64) (float64, float64) {
	if len(X) == 0 {
		return 0, 0
	}
	featIdx := make(map[string]int, len(featureNames))
	for i, name := range featureNames {
		featIdx[name] = i
	}
	var sse, meanY float64
	for _, v := range y {
		meanY += v
	}
	meanY /= float64(len(y))
	var ssTot float64
	for i, row := range X {
		pred := 0.0
		for ti := range trees {
			pred += evalTreeNode(&trees[ti], row, featIdx)
		}
		d := y[i] - pred
		sse += d * d
		dy := y[i] - meanY
		ssTot += dy * dy
	}
	rmse := math.Sqrt(sse / float64(len(y)))
	r2 := 0.0
	if ssTot > 0 {
		r2 = 1 - sse/ssTot
	}
	return rmse, r2
}

// evalTreeNode 与 predictor.predictTree 同语义的本地求值（避免依赖已加载 predictor）。
func evalTreeNode(node *TreeNode, row []float64, featIdx map[string]int) float64 {
	if node.Leaf != nil {
		return *node.Leaf
	}
	idx, ok := featIdx[node.Feature]
	if !ok {
		if node.Left != nil {
			return evalTreeNode(node.Left, row, featIdx)
		}
		return 0
	}
	if row[idx] <= node.Threshold {
		if node.Left != nil {
			return evalTreeNode(node.Left, row, featIdx)
		}
		return 0
	}
	if node.Right != nil {
		return evalTreeNode(node.Right, row, featIdx)
	}
	return 0
}

// directionalAccuracy 方向准确率（预测与真实收益率同号占比，%）。
func directionalAccuracy(trees []TreeNode, featureNames []string, X [][]float64, y []float64) float64 {
	if len(X) == 0 {
		return 0
	}
	featIdx := make(map[string]int, len(featureNames))
	for i, name := range featureNames {
		featIdx[name] = i
	}
	correct := 0
	for i, row := range X {
		pred := 0.0
		for ti := range trees {
			pred += evalTreeNode(&trees[ti], row, featIdx)
		}
		if (pred > 0 && y[i] > 0) || (pred < 0 && y[i] < 0) || (pred == 0 && y[i] == 0) {
			correct++
		}
	}
	return float64(correct) / float64(len(y)) * 100
}
