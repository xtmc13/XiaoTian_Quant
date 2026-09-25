package pairlist

import (
	"fmt"
	"strings"
	"time"
)

// 配置驱动的 producer/filter 工厂。
// handler 的 ConfigurePairlist 与前端表单元数据都从这里取，
// 新增过滤器只需在 specs 里登记一次。

// ParamSpec 描述一个可配置参数（供前端渲染表单）。
type ParamSpec struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"` // number | text | select | tags | bool
	Default any      `json:"default,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Step    *float64 `json:"step,omitempty"`
	Options []string `json:"options,omitempty"`
}

// ComponentSpec 描述一个 producer / filter 组件。
type ComponentSpec struct {
	Name        string      `json:"name"`
	Label       string      `json:"label"`
	Description string      `json:"description"`
	Params      []ParamSpec `json:"params"`
}

func floatPtr(v float64) *float64 { return &v }

// ProducerSpecs 返回全部可用 producer 的元数据。
func ProducerSpecs() []ComponentSpec {
	return []ComponentSpec{
		{
			Name: "StaticPairList", Label: "静态名单", Description: "手动指定交易对",
			Params: []ParamSpec{{Key: "pairs", Label: "交易对", Type: "tags"}},
		},
		{
			Name: "VolumePairList", Label: "成交量排行", Description: "按 24h 成交量取前 N",
			Params: []ParamSpec{
				{Key: "top_n", Label: "前 N 名", Type: "number", Default: 30, Min: floatPtr(1), Max: floatPtr(500)},
				{Key: "min_volume", Label: "最小成交量", Type: "number", Default: 0, Min: floatPtr(0)},
			},
		},
		{
			Name: "MarketCapPairList", Label: "市值排行", Description: "按市值排名取前 N（需市值数据源）",
			Params: []ParamSpec{
				{Key: "number_assets", Label: "取前 N", Type: "number", Default: 30, Min: floatPtr(1), Max: floatPtr(250)},
				{Key: "max_rank", Label: "最大市值排名", Type: "number", Default: 30, Min: floatPtr(1), Max: floatPtr(250)},
				{Key: "refresh_period_sec", Label: "刷新间隔(秒)", Type: "number", Default: 86400, Min: floatPtr(60)},
			},
		},
		{
			Name: "PercentChangePairList", Label: "涨跌幅排行", Description: "按 N 周期涨跌幅排序选币",
			Params: []ParamSpec{
				{Key: "number_assets", Label: "取前 N", Type: "number", Default: 30, Min: floatPtr(1), Max: floatPtr(500)},
				{Key: "lookback_period", Label: "回溯K线数(0=24h ticker)", Type: "number", Default: 0, Min: floatPtr(0), Max: floatPtr(500)},
				{Key: "lookback_timeframe", Label: "K线周期", Type: "select", Default: "1h", Options: []string{"1m", "5m", "15m", "1h", "4h", "1d"}},
				{Key: "sort_direction", Label: "排序方向", Type: "select", Default: "desc", Options: []string{"desc", "asc"}},
				{Key: "min_value", Label: "涨跌幅下限(%)", Type: "number"},
				{Key: "max_value", Label: "涨跌幅上限(%)", Type: "number"},
			},
		},
		{
			Name: "RemotePairList", Label: "远端名单", Description: "从 URL 拉取名单（超时+本地缓存兜底）",
			Params: []ParamSpec{
				{Key: "pairlist_url", Label: "名单 URL", Type: "text"},
				{Key: "number_assets", Label: "取前 N(0=不限)", Type: "number", Default: 0, Min: floatPtr(0)},
				{Key: "refresh_period_sec", Label: "刷新间隔(秒)", Type: "number", Default: 1800, Min: floatPtr(10)},
				{Key: "read_timeout_sec", Label: "读取超时(秒)", Type: "number", Default: 10, Min: floatPtr(1), Max: floatPtr(120)},
				{Key: "bearer_token", Label: "Bearer Token", Type: "text"},
				{Key: "keep_pairlist_on_failure", Label: "失败时用缓存兜底", Type: "bool", Default: true},
			},
		},
	}
}

// FilterSpecs 返回全部可用 filter 的元数据。
func FilterSpecs() []ComponentSpec {
	return []ComponentSpec{
		{Name: "PriceFilter", Label: "价格过滤", Description: "过滤价格超出范围的交易对", Params: []ParamSpec{
			{Key: "min_price", Label: "最小价格", Type: "number", Min: floatPtr(0)},
			{Key: "max_price", Label: "最大价格", Type: "number", Min: floatPtr(0)},
		}},
		{Name: "SpreadFilter", Label: "价差过滤", Description: "过滤买卖价差过大的交易对", Params: []ParamSpec{
			{Key: "max_spread_pct", Label: "最大价差(%)", Type: "number", Default: 0.5, Min: floatPtr(0), Step: floatPtr(0.1)},
		}},
		{Name: "VolatilityFilter", Label: "波动率过滤", Description: "过滤波动率异常的交易对", Params: []ParamSpec{
			{Key: "min_volatility_pct", Label: "最小波动率(%)", Type: "number", Min: floatPtr(0), Step: floatPtr(0.1)},
			{Key: "max_volatility_pct", Label: "最大波动率(%)", Type: "number", Min: floatPtr(0), Step: floatPtr(0.1)},
		}},
		{Name: "PrecisionFilter", Label: "精度过滤", Description: "过滤价格/数量精度不足的交易对", Params: []ParamSpec{
			{Key: "min_price_precision", Label: "最小价格精度", Type: "number", Min: floatPtr(0), Max: floatPtr(8)},
			{Key: "min_qty_precision", Label: "最小数量精度", Type: "number", Min: floatPtr(0), Max: floatPtr(8)},
		}},
		{Name: "MaxPairsFilter", Label: "最大数量限制", Description: "限制最终名单长度", Params: []ParamSpec{
			{Key: "max_pairs", Label: "最大数量", Type: "number", Default: 50, Min: floatPtr(1), Max: floatPtr(500)},
		}},
		{Name: "ShuffleFilter", Label: "随机打乱", Description: "随机打乱名单顺序，避免过拟合", Params: []ParamSpec{
			{Key: "seed", Label: "随机种子", Type: "number", Default: 0},
		}},
		{Name: "CorrelationFilter", Label: "相关性过滤", Description: "剔除与基准高度相关的同质化交易对", Params: []ParamSpec{
			{Key: "max_correlated", Label: "最大相关性", Type: "number", Default: 0.95, Min: floatPtr(0), Max: floatPtr(1), Step: floatPtr(0.01)},
		}},
		{Name: "AgeFilter", Label: "上市时间过滤", Description: "剔除上市时间太短的交易对", Params: []ParamSpec{
			{Key: "min_age_days", Label: "最小上市天数", Type: "number", Default: 7, Min: floatPtr(0)},
		}},
		{Name: "PerformanceFilter", Label: "表现排序", Description: "按近期表现排序并保留前 N", Params: []ParamSpec{
			{Key: "top_n", Label: "保留前 N(0=仅排序)", Type: "number", Default: 0, Min: floatPtr(0)},
		}},
		{Name: "OffsetFilter", Label: "偏移过滤", Description: "跳过名单前 N 个", Params: []ParamSpec{
			{Key: "offset", Label: "跳过数量", Type: "number", Default: 0, Min: floatPtr(0)},
		}},
		{Name: "VolumeFilter", Label: "成交量过滤", Description: "剔除 24h 成交量低于阈值的交易对", Params: []ParamSpec{
			{Key: "min_volume", Label: "最小成交量", Type: "number", Default: 0, Min: floatPtr(0)},
		}},
		{Name: "RangeFilter", Label: "价格区间过滤", Description: "保留价格在参考价一定比例区间内的交易对", Params: []ParamSpec{
			{Key: "reference_price", Label: "参考价格", Type: "number", Min: floatPtr(0)},
			{Key: "min_pct", Label: "最小比例", Type: "number", Default: 0.5, Step: floatPtr(0.1)},
			{Key: "max_pct", Label: "最大比例", Type: "number", Default: 2.0, Step: floatPtr(0.1)},
		}},
		{Name: "LowProfitPairsFilter", Label: "低收益过滤", Description: "剔除历史收益低于阈值的交易对", Params: []ParamSpec{
			{Key: "min_profit_pct", Label: "最小收益率(%)", Type: "number", Default: 0, Step: floatPtr(0.1)},
		}},
		{Name: "RankFilter", Label: "综合排名", Description: "按成交量+表现加权评分取前 N", Params: []ParamSpec{
			{Key: "top_n", Label: "取前 N", Type: "number", Default: 20, Min: floatPtr(1)},
			{Key: "volume_weight", Label: "成交量权重", Type: "number", Default: 0.5, Step: floatPtr(0.1)},
			{Key: "performance_weight", Label: "表现权重", Type: "number", Default: 0.5, Step: floatPtr(0.1)},
		}},
		{Name: "DelistFilter", Label: "退市过滤", Description: "剔除非交易状态/计划退市/长期无成交的交易对", Params: []ParamSpec{
			{Key: "max_days_from_now", Label: "N天内将退市即剔除(-1关闭)", Type: "number", Default: -1},
			{Key: "max_inactive_days", Label: "N天无成交即剔除(0关闭)", Type: "number", Default: 0, Min: floatPtr(0)},
		}},
		{Name: "RangeStabilityFilter", Label: "波动区间过滤", Description: "剔除价格区间过窄的僵尸交易对", Params: []ParamSpec{
			{Key: "min_range_ratio", Label: "最小区间比率", Type: "number", Default: 0.005, Step: floatPtr(0.001)},
		}},
		{Name: "FullTradesFilter", Label: "持仓过滤", Description: "剔除已有持仓的交易对", Params: []ParamSpec{}},
		{Name: "MarketCapFilter", Label: "市值过滤", Description: "按市值上下限过滤", Params: []ParamSpec{
			{Key: "min_market_cap", Label: "最小市值", Type: "number", Min: floatPtr(0)},
			{Key: "max_market_cap", Label: "最大市值", Type: "number", Min: floatPtr(0)},
		}},
		{Name: "VolumeChangeFilter", Label: "成交量变化过滤", Description: "剔除成交量骤变（缩量/异常放量）的交易对", Params: []ParamSpec{
			{Key: "min_change", Label: "最小变化率", Type: "number", Default: -0.8, Step: floatPtr(0.1)},
			{Key: "max_change", Label: "最大变化率", Type: "number", Default: 5.0, Step: floatPtr(0.1)},
		}},
		{Name: "PriceJumpFilter", Label: "暴涨暴跌过滤", Description: "剔除近期涨跌幅过大的交易对", Params: []ParamSpec{
			{Key: "max_jump_pct", Label: "最大涨跌幅(%)", Type: "number", Default: 20, Min: floatPtr(0)},
		}},
		{Name: "LiquidityFilter", Label: "深度过滤", Description: "剔除盘口深度不足的交易对", Params: []ParamSpec{
			{Key: "min_bid_depth", Label: "最小买盘深度", Type: "number", Default: 50000, Min: floatPtr(0)},
			{Key: "min_ask_depth", Label: "最小卖盘深度", Type: "number", Default: 50000, Min: floatPtr(0)},
		}},
		{Name: "FundingRateFilter", Label: "资金费率过滤", Description: "剔除资金费率过高的合约对", Params: []ParamSpec{
			{Key: "max_funding_rate", Label: "最大资金费率", Type: "number", Default: 0.01, Step: floatPtr(0.001)},
		}},
		{Name: "ChangeFilter", Label: "24h涨跌过滤", Description: "保留 24h 涨跌幅在区间内的交易对", Params: []ParamSpec{
			{Key: "min_change_pct", Label: "最小涨跌幅(%)", Type: "number", Step: floatPtr(0.1)},
			{Key: "max_change_pct", Label: "最大涨跌幅(%)", Type: "number", Step: floatPtr(0.1)},
		}},
		{Name: "RemotePairList", Label: "远端名单", Description: "用远端名单过滤/追加/剔除（黑/白名单）", Params: []ParamSpec{
			{Key: "pairlist_url", Label: "名单 URL", Type: "text"},
			{Key: "mode", Label: "模式", Type: "select", Default: "whitelist", Options: []string{"whitelist", "blacklist"}},
			{Key: "processing_mode", Label: "合并方式", Type: "select", Default: "filter", Options: []string{"filter", "append"}},
			{Key: "number_assets", Label: "取前 N(0=不限)", Type: "number", Default: 0, Min: floatPtr(0)},
			{Key: "refresh_period_sec", Label: "刷新间隔(秒)", Type: "number", Default: 1800, Min: floatPtr(10)},
			{Key: "read_timeout_sec", Label: "读取超时(秒)", Type: "number", Default: 10, Min: floatPtr(1), Max: floatPtr(120)},
			{Key: "bearer_token", Label: "Bearer Token", Type: "text"},
			{Key: "keep_pairlist_on_failure", Label: "失败时用缓存兜底", Type: "bool", Default: true},
		}},
	}
}

// ── 参数解析辅助 ──

func paramFloat(params map[string]any, key string, def float64) float64 {
	if params == nil {
		return def
	}
	switch v := params[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return def
}

func paramInt(params map[string]any, key string, def int) int {
	return int(paramFloat(params, key, float64(def)))
}

func paramStr(params map[string]any, key, def string) string {
	if params == nil {
		return def
	}
	if s, ok := params[key].(string); ok && s != "" {
		return s
	}
	return def
}

func paramBool(params map[string]any, key string, def bool) bool {
	if params == nil {
		return def
	}
	if b, ok := params[key].(bool); ok {
		return b
	}
	return def
}

func paramStrSlice(params map[string]any, key string) []string {
	if params == nil {
		return nil
	}
	raw, ok := params[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// remotePairListFromParams 构造 RemotePairList（producer 与 filter 共用）。
func remotePairListFromParams(params map[string]any) (*RemotePairList, error) {
	r, err := NewRemotePairList(paramStr(params, "pairlist_url", ""))
	if err != nil {
		return nil, err
	}
	if mode := paramStr(params, "mode", "whitelist"); mode == "whitelist" || mode == "blacklist" {
		r.Mode = mode
	} else {
		return nil, fmt.Errorf("RemotePairList: mode 仅支持 whitelist/blacklist")
	}
	if pm := paramStr(params, "processing_mode", "filter"); pm == "filter" || pm == "append" {
		r.ProcessingMode = pm
	} else {
		return nil, fmt.Errorf("RemotePairList: processing_mode 仅支持 filter/append")
	}
	r.NumberAssets = paramInt(params, "number_assets", 0)
	if v := paramInt(params, "refresh_period_sec", 0); v > 0 {
		r.RefreshPeriod = time.Duration(v) * time.Second
	}
	if v := paramInt(params, "read_timeout_sec", 0); v > 0 {
		r.ReadTimeout = time.Duration(v) * time.Second
	}
	r.BearerToken = paramStr(params, "bearer_token", "")
	r.KeepOnFailure = paramBool(params, "keep_pairlist_on_failure", true)
	return r, nil
}

// BuildProducerFromConfig 按名称+参数构建 producer。
// VolumePairList / PerformancePairList 依赖 InfoProvider（交易所行情），
// 由调用方在构建后注入；MarketCapPairList / PercentChangePairList 的外部数据源
// 同样由调用方注入（未注入时 Generate 返回明确降级错误）。
func BuildProducerFromConfig(name string, params map[string]any) (IProducer, error) {
	switch name {
	case "StaticPairList":
		pairs := paramStrSlice(params, "pairs")
		if len(pairs) == 0 {
			return nil, fmt.Errorf("StaticPairList requires 'pairs' array")
		}
		return NewStaticPairList(pairs), nil
	case "VolumePairList":
		return NewVolumePairList(
			paramInt(params, "top_n", 30),
			paramFloat(params, "min_volume", 0),
			nil,
		), nil
	case "PerformancePairList":
		return &PerformancePairList{
			TopN:         paramInt(params, "top_n", 30),
			LookbackHours: paramInt(params, "lookback_hours", 24),
		}, nil
	case "MarketCapPairList":
		refresh := time.Duration(paramInt(params, "refresh_period_sec", 86400)) * time.Second
		return NewMarketCapPairList(
			paramInt(params, "number_assets", 30),
			paramInt(params, "max_rank", 30),
			refresh,
			nil, // 数据源由调用方注入；未注入时 Generate 返回降级错误
		), nil
	case "PercentChangePairList":
		p := NewPercentChangePairList(
			paramInt(params, "number_assets", 30),
			paramStr(params, "sort_direction", "desc"),
			paramInt(params, "lookback_period", 0),
			paramStr(params, "lookback_timeframe", "1h"),
		)
		if params != nil {
			if v, ok := params["min_value"].(float64); ok {
				p.MinValue = &v
			}
			if v, ok := params["max_value"].(float64); ok {
				p.MaxValue = &v
			}
		}
		if v := paramInt(params, "refresh_period_sec", 0); v > 0 {
			p.RefreshPeriod = time.Duration(v) * time.Second
		}
		return p, nil
	case "RemotePairList":
		return remotePairListFromParams(params)
	default:
		return nil, fmt.Errorf("unknown producer: %s", name)
	}
}

// BuildFilterFromConfig 按名称+参数构建 filter。
func BuildFilterFromConfig(name string, params map[string]any) (IFilter, error) {
	switch name {
	case "PriceFilter":
		return NewPriceFilter(paramFloat(params, "min_price", 0), paramFloat(params, "max_price", 0)), nil
	case "SpreadFilter":
		return NewSpreadFilter(paramFloat(params, "max_spread_pct", 0.5)), nil
	case "VolatilityFilter":
		return NewVolatilityFilter(paramFloat(params, "min_volatility_pct", 0), paramFloat(params, "max_volatility_pct", 0)), nil
	case "PrecisionFilter":
		return NewPrecisionFilter(paramInt(params, "min_price_precision", 0), paramInt(params, "min_qty_precision", 0)), nil
	case "MaxPairsFilter":
		return NewMaxPairsFilter(paramInt(params, "max_pairs", 0)), nil
	case "ShuffleFilter":
		return NewShuffleFilter(int64(paramInt(params, "seed", 0))), nil
	case "CorrelationFilter":
		return NewCorrelationFilter(paramFloat(params, "max_correlated", 0.95)), nil
	case "AgeFilter":
		return NewAgeFilter(paramInt(params, "min_age_days", 0)), nil
	case "PerformanceFilter":
		return NewPerformanceFilter(paramInt(params, "top_n", 0)), nil
	case "OffsetFilter":
		return NewOffsetFilter(paramInt(params, "offset", 0)), nil
	case "RangeFilter":
		return NewRangeFilter(
			paramFloat(params, "reference_price", 0),
			paramFloat(params, "min_pct", 0.5),
			paramFloat(params, "max_pct", 2.0),
		), nil
	case "LowProfitPairsFilter":
		return NewLowProfitPairsFilter(paramFloat(params, "min_profit_pct", 0)), nil
	case "VolumeFilter":
		return NewVolumeFilter(paramFloat(params, "min_volume", 0)), nil
	case "ChangeFilter":
		return NewChangeFilter(paramFloat(params, "min_change_pct", 0), paramFloat(params, "max_change_pct", 0)), nil
	case "RankFilter":
		return NewRankFilter(
			paramInt(params, "top_n", 20),
			paramFloat(params, "volume_weight", 0.5),
			paramFloat(params, "performance_weight", 0.5),
		), nil
	case "DelistFilter":
		f := NewDelistFilter()
		if ss := paramStrSlice(params, "allowed_statuses"); len(ss) > 0 {
			f.AllowedStatuses = ss
		}
		f.MaxDaysFromNow = paramInt(params, "max_days_from_now", -1)
		f.MaxInactiveDays = paramInt(params, "max_inactive_days", 0)
		return f, nil
	case "RangeStabilityFilter":
		return NewRangeStabilityFilter(paramFloat(params, "min_range_ratio", 0.005)), nil
	case "FullTradesFilter":
		return NewFullTradesFilter(), nil
	case "MarketCapFilter":
		return NewMarketCapFilter(paramFloat(params, "min_market_cap", 0), paramFloat(params, "max_market_cap", 0)), nil
	case "VolumeChangeFilter":
		return NewVolumeChangeFilter(paramFloat(params, "min_change", -0.8), paramFloat(params, "max_change", 5.0)), nil
	case "PriceJumpFilter":
		return NewPriceJumpFilter(paramFloat(params, "max_jump_pct", 20)), nil
	case "LiquidityFilter":
		return NewLiquidityFilter(paramFloat(params, "min_bid_depth", 50000), paramFloat(params, "min_ask_depth", 50000)), nil
	case "FundingRateFilter":
		return NewFundingRateFilter(paramFloat(params, "max_funding_rate", 0.01)), nil
	case "RemotePairList":
		return remotePairListFromParams(params)
	default:
		return nil, fmt.Errorf("unknown filter: %s", name)
	}
}

// KnownProducer 校验名称是否为已登记的 producer。
func KnownProducer(name string) bool {
	for _, s := range ProducerSpecs() {
		if strings.EqualFold(s.Name, name) {
			return true
		}
	}
	return false
}

// KnownFilter 校验名称是否为已登记的 filter。
func KnownFilter(name string) bool {
	for _, s := range FilterSpecs() {
		if strings.EqualFold(s.Name, name) {
			return true
		}
	}
	return false
}
