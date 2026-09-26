package handler

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/xiaotian-quant/gateway/internal/protection"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// 策略级 protections（config_json["protections"]，与全局 /api/protection/config
// 同 schema，hyperopt epoch 回写）的实盘加载：
//   - 启动/重启（startStrategyInEngine）时构建策略级 ProtectionManager 注入引擎，
//     实盘信号先过策略级再过全局；
//   - 配置更新（UpdateStrategyConfig）写库前校验，运行中的策略热重载替换；
//   - 策略停止/删除经 eng.Unregister 自动释放（engine.Unregister 清理）。

// strategyProtectionManagerFromConfig 从策略配置构建策略级 ProtectionManager。
// 无 protections 返回 (nil, nil)；配置非法返回明确错误（调用方拒绝启动，
// 而不是静默裸奔）。
func strategyProtectionManagerFromConfig(item map[string]any) (*protection.ProtectionManager, error) {
	cj, _ := item["config_json"].(string)
	if cj == "" {
		return nil, nil
	}
	var cfgMap map[string]any
	if err := json.Unmarshal([]byte(cj), &cfgMap); err != nil {
		return nil, fmt.Errorf("config_json 解析失败: %w", err)
	}
	raw, ok := cfgMap["protections"]
	if !ok || raw == nil {
		return nil, nil
	}
	cfgs := parseProtectionConfigs(raw)
	if len(cfgs) == 0 {
		return nil, fmt.Errorf("protections 结构非法（应为 [{\"name\":..., \"params\":...}] 数组）")
	}
	mgr, err := protection.BuildManagerFromConfig(protection.Config{Protections: cfgs})
	if err != nil {
		return nil, fmt.Errorf("策略级 protections 构建失败: %w", err)
	}
	return mgr, nil
}

// validateStrategyProtections 校验合并后策略配置里 protections 的合法性
// （Update 写库前调用，与类型-参数防呆同一层）。无该字段时放行。
func validateStrategyProtections(mergedCfg map[string]any) error {
	raw, ok := mergedCfg["protections"]
	if !ok || raw == nil {
		return nil
	}
	cfgs := parseProtectionConfigs(raw)
	if len(cfgs) == 0 {
		return fmt.Errorf("protections 结构非法（应为 [{\"name\":..., \"params\":...}] 数组）")
	}
	if _, err := protection.BuildManagerFromConfig(protection.Config{Protections: cfgs}); err != nil {
		return fmt.Errorf("protections 配置无效: %w", err)
	}
	return nil
}

// refreshStrategyProtection 配置更新后的热重载：策略在运行则按最新 config_json
// 重建策略级 manager（无 protections 则清除）；未运行不动作（下次启动装配）。
// 构建失败保留旧配置并记 WARN（Update 路径已校验，失败多半是并发改库）。
func refreshStrategyProtection(id string, item map[string]any) {
	eng := strategy.GetEngine(nil)
	if eng == nil || eng.Get(id) == nil {
		return
	}
	mgr, err := strategyProtectionManagerFromConfig(item)
	if err != nil {
		log.Printf("[strategy] WARN reload strategy-level protections for %s failed（保留旧配置）: %v", id, err)
		return
	}
	eng.SetStrategyProtectionManager(id, mgr)
	if mgr == nil {
		log.Printf("[strategy] strategy-level protections cleared for %s", id)
	} else {
		log.Printf("[strategy] strategy-level protections reloaded for %s (%d rules)", id, len(mgr.Protections()))
	}
}

// strategyProtectionsView 提取 config_json["protections"] 供列表/详情 API 返回
// （前端展示用；无配置时返回 nil，字段缺省不出现在响应里）。
func strategyProtectionsView(it map[string]any) any {
	if cfg, ok := it["config"].(map[string]any); ok {
		if prot, ok := cfg["protections"]; ok && prot != nil {
			return prot
		}
	}
	if cj, ok := it["config_json"].(string); ok && cj != "" {
		var cfg map[string]any
		if json.Unmarshal([]byte(cj), &cfg) == nil {
			if prot, ok := cfg["protections"]; ok && prot != nil {
				return prot
			}
		}
	}
	return nil
}
