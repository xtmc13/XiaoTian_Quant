package handler

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── AI 机器人后台扫描 worker ─────────────────────────────────────
// 按每个用户配置的 scan_interval_seconds 轮询其 watchlist：
// 对每个 symbol 跑 DecisionEngine.Decide（fast/deep 由配置 mode 决定），
// 结果落 xt_ai_signals 表，供 /ai/signals、AIRobotStatus 以及 AI 机器人
// 策略 / paper 模拟器消费。enabled=false 的配置不扫描。

const aiRobotScanTick = 30 * time.Second // 调度粒度：每 30s 检查一次到期配置

var (
	aiRobotScanMu       sync.Mutex
	aiRobotLastScan     = map[int64]time.Time{} // userID → 上次扫描时间
	aiRobotScanRunning  = map[int64]bool{}      // userID → 正在扫描（防重入）
	aiRobotScanDisabled = false                 // 测试可关闭
)

// StartAIRobotScanWorker 启动后台扫描循环（参考 StartAIBotSnapshotWorker 模式）。
// 用 context 无法贯穿 handler 生命周期，沿用包级开关 + 进程退出即停的写法。
func StartAIRobotScanWorker() {
	go func() {
		ticker := time.NewTicker(aiRobotScanTick)
		defer ticker.Stop()
		// 启动后先等一个 tick，让 store/配置就绪。
		for range ticker.C {
			if aiRobotScanDisabled {
				continue
			}
			scanDueAIRobots()
		}
	}()
}

// ResetAIRobotScanState 清空调度记忆（测试用）。
func ResetAIRobotScanState() {
	aiRobotScanMu.Lock()
	defer aiRobotScanMu.Unlock()
	aiRobotLastScan = map[int64]time.Time{}
	aiRobotScanRunning = map[int64]bool{}
}

// scanDueAIRobots 找出到期的 enabled 配置并逐个扫描（各用户并发）。
func scanDueAIRobots() {
	users, err := listAIRobotConfigUsers()
	if err != nil || len(users) == 0 {
		return
	}
	now := time.Now()
	var wg sync.WaitGroup
	for _, userID := range users {
		cfg := getAIRobotConfig(userID)
		enabled, _ := cfg["enabled"].(bool)
		if !enabled {
			continue
		}
		interval := getInt(cfg, "scan_interval_seconds", getInt(cfg, "scan_interval", 300))
		if interval <= 0 {
			interval = 300
		}

		aiRobotScanMu.Lock()
		if aiRobotScanRunning[userID] {
			aiRobotScanMu.Unlock()
			continue // 上一轮还没扫完，跳过
		}
		if last, ok := aiRobotLastScan[userID]; ok && now.Sub(last) < time.Duration(interval)*time.Second {
			aiRobotScanMu.Unlock()
			continue // 未到扫描间隔
		}
		aiRobotScanRunning[userID] = true
		aiRobotLastScan[userID] = now
		aiRobotScanMu.Unlock()

		wg.Add(1)
		go func(uid int64, cfg map[string]any) {
			defer wg.Done()
			defer func() {
				aiRobotScanMu.Lock()
				delete(aiRobotScanRunning, uid)
				aiRobotScanMu.Unlock()
			}()
			runAIRobotScan(uid, cfg)
		}(userID, cfg)
	}
	wg.Wait()
}

// listAIRobotConfigUsers 列出所有保存过配置的用户 id。
func listAIRobotConfigUsers() ([]int64, error) {
	db := store.GetDB()
	if db == nil {
		return nil, nil
	}
	rows, err := db.Query(`SELECT user_id FROM xt_ai_robot_configs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []int64{}
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err == nil && uid > 0 {
			users = append(users, uid)
		}
	}
	return users, rows.Err()
}

// runAIRobotScan 对一个用户的 watchlist 全量扫描一轮。
func runAIRobotScan(userID int64, cfg map[string]any) {
	watchlist := stringSliceFromAnyLocal(cfg["watchlist"])
	if len(watchlist) == 0 {
		watchlist = stringSliceFromAnyLocal(cfg["symbols"])
	}
	if len(watchlist) == 0 {
		return
	}

	// 配置覆盖：provider/model 需要同步进 ai 包的全局 provider（api key 来源）。
	providerName := getString(cfg, "provider", "deepseek")
	if p := ai.GetProvider(providerName); p != nil {
		if model := getString(cfg, "model", ""); model != "" {
			ai.SetProviderModel(providerName, model)
		}
	}

	marketFilter, _ := cfg["market_filter"].(bool)
	marketFilters, _ := cfg["market_filters"].(map[string]any)
	maxVolatility := 10.0
	minVolume := 1_000_000.0
	if marketFilters != nil {
		if v := getFloat(marketFilters, "max_volatility", 10); v > 0 {
			maxVolatility = v
		}
		if v := getFloat(marketFilters, "min_volume_24h", 1_000_000); v > 0 {
			minVolume = v
		}
	}

	engine := ai.NewDecisionEngine(ai.DecisionConfig{
		Provider:      providerName,
		Mode:          getString(cfg, "mode", "fast"),
		MarketFilter:  marketFilter,
		MaxVolatility: maxVolatility,
		MinVolume24h:  minVolume,
	})

	repo := store.DefaultAISignalRepo()
	for _, symbol := range watchlist {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		dec, err := engine.Decide(ctx, symbol)
		cancel()
		if err != nil {
			log.Printf("[ai_robot] user=%d symbol=%s decide failed: %v", userID, symbol, err)
			continue
		}
		rec := &store.AISignalRecord{
			UserID:          userID,
			Symbol:          dec.Symbol,
			Signal:          dec.Signal,
			Confidence:      dec.Confidence,
			Reason:          dec.Reason,
			Filters:         dec.Filters,
			MarketCondition: dec.MarketCondition,
			Mode:            dec.Mode,
			Provider:        dec.Provider,
			CreatedAt:       dec.CreatedAt,
		}
		if err := repo.Insert(rec); err != nil {
			log.Printf("[ai_robot] user=%d symbol=%s insert signal failed: %v", userID, symbol, err)
		}
	}
}
