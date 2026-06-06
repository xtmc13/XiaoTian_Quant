package hyperopt

type EarlyStopConfig struct {
	Enabled        bool
	MinTrials      int
	Patience       int
	ImprovementPct float64
}

func DefaultEarlyStopConfig() EarlyStopConfig {
	return EarlyStopConfig{
		Enabled:        true,
		MinTrials:      20,
		Patience:       10,
		ImprovementPct: 0.01,
	}
}

// ShouldStop 判断是否应提前停止
// 返回 true 如果最近 Patience 次试验没有比当前最佳改善 ImprovementPct 以上
func (es *EarlyStopConfig) ShouldStop(trials []Trial, currentBest float64) bool {
	if !es.Enabled || len(trials) < es.MinTrials {
		return false
	}
	if len(trials) < es.Patience {
		return false
	}
	recent := trials[len(trials)-es.Patience:]
	for _, t := range recent {
		if t.Loss < currentBest*(1-es.ImprovementPct) {
			return false
		}
	}
	return true
}
