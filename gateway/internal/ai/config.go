package ai

// ApplyConfig 把设置页保存的 ai.{provider} 配置应用到运行时注册表：
// api_key / model / base_url 逐家生效，使所有直接读注册表的调用方
// （决策 worker、多智能体管线、各 handler 的 ai.GetProvider）与设置页一致。
// 启动时与 PUT /config 保存后各调一次；未注册的 provider（如 openrouter）跳过。
// 兼容平铺（ai.{name}）与嵌套（ai.providers.{name}）两种历史形状。
func ApplyConfig(aiCfg map[string]any) {
	if aiCfg == nil {
		return
	}
	applyOne := func(name string, v any) {
		pc, ok := v.(map[string]any)
		if !ok {
			return
		}
		n := NormalizeProviderName(name)
		if GetProvider(n) == nil {
			return
		}
		if key, _ := pc["api_key"].(string); key != "" {
			SetProviderAPIKey(n, key)
		}
		if model, _ := pc["model"].(string); model != "" {
			SetProviderModel(n, model)
		}
		if bu, _ := pc["base_url"].(string); bu != "" {
			SetProviderBaseURL(n, bu)
		}
	}
	for k, v := range aiCfg {
		if k == "providers" {
			if nested, ok := v.(map[string]any); ok {
				for nk, nv := range nested {
					applyOne(nk, nv)
				}
			}
			continue
		}
		applyOne(k, v)
	}
}
