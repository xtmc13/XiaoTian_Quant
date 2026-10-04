// Package analysis 主力行为分析（Wyckoff 量价循环的工程化实现）。
//
// 把"建仓→吸筹→洗盘→拉升→出货"五段主力行为翻译成可计算的状态机：
//
//	观望 ──SC(抛售高潮)──▶ 建仓 ──AR/ST 结构+资金背离──▶ 吸筹 ──Spring──▶ 洗盘
//	                                                          │假跌破收回
//	                                                          ▼
//	出货 ◀──BC/UT+顶部背离── 拉升 ◀──放量破区间+多头排列── 结束洗盘
//	 │
//	 └──▶ 回落出区间 → 循环回观望/下一轮建仓
//
// 分析是纯函数：喂已闭合 K 线出结论。事件层单遍扫描（局部窗口），阶段层
// 逐 bar 分类 + 迟滞成段。由 API（一次性）与 smart_money 策略（逐 bar）复用。
package analysis

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// Phase 主力行为阶段。
type Phase string

const (
	PhaseNone     Phase = "none"         // 观望：无明确结构
	PhaseBuilding Phase = "building"     // 建仓：首个抛售高潮后、结构未确认
	PhaseAccum    Phase = "accumulation" // 吸筹：区间+二次测试/弹簧+资金持续流入
	PhaseShakeout Phase = "shakeout"     // 洗盘：假跌破收回，猎杀止损后快速修复
	PhaseMarkup   Phase = "markup"       // 拉升：放量破区间、结构多头排列
	PhaseDistrib  Phase = "distribution" // 出货：顶部高潮/上冲破+量价背离
)

// PhaseMeta 阶段的中文标签与展示色（前端同款配色）。
var PhaseMeta = map[Phase]struct {
	Label string `json:"label"`
	Color string `json:"color"`
}{
	PhaseNone:     {"观望", "#8A8F98"},
	PhaseBuilding: {"建仓", "#3B82F6"},
	PhaseAccum:    {"吸筹", "#06B6D4"},
	PhaseShakeout: {"洗盘", "#F59E0B"},
	PhaseMarkup:   {"拉升", "#0ECB81"},
	PhaseDistrib:  {"出货", "#F6465D"},
}

// Evidence 单条证据（事件或量价背离）。
type Evidence struct {
	Kind    string  `json:"kind"`    // 机器可读：spring/sc/bc/ut/obv_div/cmf…
	Label   string  `json:"label"`   // 人话
	Bullish bool    `json:"bullish"` // true=多头证据
	Weight  float64 `json:"weight"`  // 0~1
	BarTime int64   `json:"bar_time"`
	Price   float64 `json:"price"`
}

// Segment 一段连续的阶段（历史时间轴用）。
type Segment struct {
	Phase      Phase   `json:"phase"`
	From       int64   `json:"from"`
	To         int64   `json:"to"`
	Confidence float64 `json:"confidence"`
}

// Signal 阶段转换点产生的交易信号。
type Signal struct {
	Direction string  `json:"direction"` // LONG / SHORT / CLOSE
	Reason    string  `json:"reason"`
	Strength  float64 `json:"strength"` // 0~1
}

// Result 一次完整分析结论。
type Result struct {
	Symbol     string             `json:"symbol"`
	Timeframe  string             `json:"timeframe"`
	Phase      Phase              `json:"phase"`
	Confidence float64            `json:"confidence"` // 0~100
	Evidence   []Evidence         `json:"evidence"`
	Segments   []Segment          `json:"segments"`
	Levels     map[string]float64 `json:"levels"` // range_high/range_low/spring_low/bc_high/…
	Signal     *Signal            `json:"signal,omitempty"`
	BarCount   int                `json:"bar_count"`
	UpdatedAt  int64              `json:"updated_at"`
}

// ── 阈值（1.0 初版固定参数）────────────────────────────────────
const (
	rangeWin       = 48  // 区间结构观察窗（根）
	structureWin   = 30  // 弹簧/上冲破的局部窗
	eventWin       = 30  // 证据回溯窗
	divWin         = 40  // 背离观察窗
	climaxVolMult  = 2.0 // 高潮量：≥2×量能均线
	breakoutVolMul = 1.5 // 突破量：≥1.5×量能均线
	rangeMaxATR    = 8.0 // 区间高差 ≤ 8×ATR 才算横盘（含弹簧插针 excursion）
	reclaimSlip    = 0.002
	minBars        = 80
)

// stats 预计算序列。
type stats struct {
	atr   []float64
	emaF  []float64 // 20
	emaS  []float64 // 50
	obv   []float64
	cmf   []float64 // 20
	volMA []float64
	rsi   []float64
}

func emaSeries(src []float64, n int) []float64 {
	out := make([]float64, len(src))
	if len(src) == 0 {
		return out
	}
	k := 2.0 / float64(n+1)
	out[0] = src[0]
	for i := 1; i < len(src); i++ {
		out[i] = src[i]*k + out[i-1]*(1-k)
	}
	return out
}

func computeStats(bars []model.Bar) *stats {
	n := len(bars)
	st := &stats{
		atr:   make([]float64, n),
		emaF:  make([]float64, n),
		emaS:  make([]float64, n),
		obv:   make([]float64, n),
		cmf:   make([]float64, n),
		volMA: make([]float64, n),
		rsi:   make([]float64, n),
	}
	closes := make([]float64, n)
	obv := 0.0
	gainSum, lossSum := 0.0, 0.0
	prevClose := 0.0
	for i, b := range bars {
		closes[i] = b.Close
		tr := b.High - b.Low
		if prevClose > 0 {
			tr = math.Max(tr, math.Max(math.Abs(b.High-prevClose), math.Abs(b.Low-prevClose)))
		}
		if i == 0 {
			st.atr[i] = tr
		} else {
			st.atr[i] = (st.atr[i-1]*13 + tr) / 14
		}
		if i > 0 {
			ch := b.Close - prevClose
			if ch > 0 {
				obv += b.Volume
				gainSum += ch
			} else if ch < 0 {
				obv -= b.Volume
				lossSum += -ch
			}
		}
		st.obv[i] = obv
		if lossSum == 0 {
			st.rsi[i] = 100
		} else {
			st.rsi[i] = 100 - 100/(1+gainSum/math.Max(lossSum, 1e-12))
		}
		prevClose = b.Close
	}
	st.emaF = emaSeries(closes, 20)
	st.emaS = emaSeries(closes, 50)
	for i := 0; i < n; i++ {
		from := i - 19
		if from < 0 {
			from = 0
		}
		s := 0.0
		for j := from; j <= i; j++ {
			s += bars[j].Volume
		}
		st.volMA[i] = s / float64(i-from+1)
	}
	for i := 0; i < n; i++ {
		from := i - 19
		if from < 0 {
			from = 0
		}
		num, den := 0.0, 0.0
		for j := from; j <= i; j++ {
			rng := bars[j].High - bars[j].Low
			if rng <= 0 {
				continue
			}
			mfm := ((bars[j].Close - bars[j].Low) - (bars[j].High - bars[j].Close)) / rng
			num += mfm * bars[j].Volume
			den += bars[j].Volume
		}
		if den > 0 {
			st.cmf[i] = num / den
		}
	}
	return st
}

func swingRange(bars []model.Bar, from, to int) (hi, lo float64) {
	if from < 0 {
		from = 0
	}
	for i := from; i <= to && i < len(bars); i++ {
		if bars[i].High > hi {
			hi = bars[i].High
		}
		if lo == 0 || bars[i].Low < lo {
			lo = bars[i].Low
		}
	}
	return
}

func bodyPct(b model.Bar) float64 {
	rng := b.High - b.Low
	if rng <= 0 {
		return 0.5
	}
	return (b.Close - b.Low) / rng
}

// evIndex 单遍扫描得到的全序列事件表（索引升序）。
type evIndex struct {
	sc     map[int]float64 // 抛售高潮：idx → 极值价（只记窗口内最低一次见下）
	bc     map[int]float64
	spr    map[int]float64 // 弹簧线：idx → 被扫的区间低
	ut     map[int]float64 // 上冲破：idx → 被扫的区间高
	st     map[int]float64 // 二次测试：idx → sc 价
	ar     map[int]bool
	arDone bool
	scLow  float64 // 最近一个 SC 的低价（ST 判定用）
	scVol  float64
	scIdx  int
}

// scanEvents 对全序列单遍扫描 Wyckoff 事件（局部窗口，窗口不含当前 bar）。
func scanEvents(bars []model.Bar, st *stats) *evIndex {
	n := len(bars)
	ev := &evIndex{
		sc: map[int]float64{}, bc: map[int]float64{},
		spr: map[int]float64{}, ut: map[int]float64{},
		st: map[int]float64{}, ar: map[int]bool{},
		scIdx: -1,
	}
	for i := 1; i < n; i++ {
		b := bars[i]
		vm := st.volMA[i]
		if vm <= 0 {
			continue
		}
		// 局部摆动窗（不含 i）。
		wHi, wLo := swingRange(bars, i-structureWin, i-1)
		if wHi > wLo {
			if b.Low < wLo*(1-reclaimSlip) && b.Close > wLo && b.Low < bars[i-1].Low && bodyPct(b) > 0.6 {
				ev.spr[i] = wLo
			}
			if b.High > wHi*(1+reclaimSlip) && b.Close < wHi && b.High > bars[i-1].High && bodyPct(b) < 0.4 {
				ev.ut[i] = wHi
			}
		}
		// SC：窗口新低 + 巨量 + 下影回收。
		if wLo > 0 && b.Low <= wLo && b.Low < bars[i-1].Low &&
			b.Volume >= climaxVolMult*vm && bodyPct(b) > 0.6 {
			if ev.scIdx == -1 || b.Low < ev.scLow {
				ev.scLow, ev.scVol, ev.scIdx = b.Low, b.Volume, i
			}
			ev.sc[i] = b.Low
		}
		// BC：窗口新高 + 巨量 + 顶部滞涨。
		if b.High >= wHi && b.High > bars[i-1].High &&
			b.Volume >= climaxVolMult*vm && bodyPct(b) < 0.45 {
			ev.bc[i] = b.High
		}
		// ST：SC 之后回踩 SC 低附近且缩量（只记首次）。
		if ev.scIdx >= 0 && i > ev.scIdx {
			if _, done := ev.st[ev.scIdx]; !done && b.Low <= ev.scLow*1.02 && b.Volume < 0.7*ev.scVol {
				ev.st[i] = ev.scLow
				ev.st[ev.scIdx] = ev.scLow // 标记该 SC 已产出 ST
			}
		}
		// AR：SC 后 10 根内强力反弹（每个 SC 只记一次）。
		if !ev.arDone && ev.scIdx >= 0 && i > ev.scIdx && i <= ev.scIdx+10 &&
			b.Close > ev.scLow+2*st.atr[i] {
			ev.ar[i] = true
			ev.arDone = true
		}
	}
	return ev
}

// recent 事件表中 [cur-eventWin, cur] 范围内的事件键（升序）。
func recent(m map[int]float64, cur int) []int {
	out := make([]int, 0, 4)
	for k := range m {
		if k >= 0 && k <= cur && cur-k <= eventWin {
			out = append(out, k)
		}
	}
	sort.Ints(out)
	return out
}

// rangeFrom 自适应区间窗起点：锚定最近 90 根的恐慌最低点（区间应相对恐慌
// 低点定义；固定窗会带进前一轮下跌前的高价，把真区间撑到 inRange 失格；
// 事件锚则会被后验事件覆盖到未来——2026-10-04 合成生命周期实证两者皆败）。
func rangeFrom(bars []model.Bar, cur int) int {
	from := cur - 89
	if from < 0 {
		from = 0
	}
	panicIdx := from
	for i := from + 1; i <= cur; i++ {
		if bars[i].Low < bars[panicIdx].Low {
			panicIdx = i
		}
	}
	if cur-panicIdx > rangeWin {
		// 恐慌低点太遥远（可能已在拉升中段），退化为近窗。
		return cur - rangeWin
	}
	return panicIdx
}

// classifyAt 在给定 bar 索引处评估阶段。事件表全序列预扫，这里只做加权。
func classifyAt(bars []model.Bar, st *stats, ev *evIndex, cur int) (Phase, float64, []Evidence) {
	b := bars[cur]
	rf := rangeFrom(bars, cur)
	rHi, _ := swingRange(bars, rf, cur-3) // 高滞后 3 根：突破 bar 不得抬高自家天花板
	if rHi <= 0 {
		rHi, _ = swingRange(bars, rf, cur)
	}
	_, rLo := swingRange(bars, rf, cur)
	mid := (rHi + rLo) / 2
	atr := math.Max(st.atr[cur], 1e-9)
	inRange := (rHi-rLo) < rangeMaxATR*atr && b.Close > rLo && b.Close < rHi
	upperThird := b.Close > rLo+(rHi-rLo)*0.66
	emaBull := st.emaF[cur] > st.emaS[cur]

	var evs []Evidence
	add := func(kind, label string, bullish bool, w float64, idx int, price float64) {
		evs = append(evs, Evidence{Kind: kind, Label: label, Bullish: bullish, Weight: w, BarTime: bars[idx].Time, Price: price})
	}

	for _, idx := range recent(ev.sc, cur) {
		add("sc", fmt.Sprintf("抛售高潮 SC @ %.4f（恐慌巨量+下影回收）", ev.sc[idx]), true, 0.5, idx, ev.sc[idx])
	}
	for idx := range ev.ar {
		if idx >= 0 && idx <= cur && cur-idx <= eventWin {
			add("ar", "自动反弹 AR（恐慌后资金承接）", true, 0.25, idx, bars[idx].Close)
		}
	}
	for _, idx := range recent(ev.st, cur) {
		add("st", fmt.Sprintf("二次测试 ST：回踩 %.4f 缩量（抛压衰竭）", ev.st[idx]), true, 0.4, idx, ev.st[idx])
	}
	for _, idx := range recent(ev.spr, cur) {
		add("spring", fmt.Sprintf("弹簧线：假跌破 %.4f 同根收回", ev.spr[idx]), true, 0.65, idx, ev.spr[idx])
	}
	for _, idx := range recent(ev.ut, cur) {
		add("ut", fmt.Sprintf("上冲破 UT：假突破 %.4f 同根回落", ev.ut[idx]), false, 0.6, idx, ev.ut[idx])
	}
	for _, idx := range recent(ev.bc, cur) {
		add("bc", fmt.Sprintf("买入高潮 BC @ %.4f（新高巨量滞涨）", ev.bc[idx]), false, 0.6, idx, ev.bc[idx])
	}

	// 量价背离。
	divFrom := cur - divWin
	if divFrom < 0 {
		divFrom = 0
	}
	flat := math.Abs(b.Close-bars[divFrom].Close)/math.Max(bars[divFrom].Close, 1e-9) < 0.01
	obvBase := math.Max(math.Abs(st.obv[divFrom]), 1e-9)
	obvChg := (st.obv[cur] - st.obv[divFrom]) / obvBase
	if flat && obvChg > 0.02 {
		add("obv_div", fmt.Sprintf("价平量升：横盘但 OBV 资金净流入 +%.1f%%", obvChg*100), true, 0.45, cur, b.Close)
	}
	hiDiv, _ := swingRange(bars, divFrom, cur)
	if b.Close >= hiDiv-1e-9 && cur-divFrom > 10 &&
		st.rsi[cur] < st.rsi[divFrom]-3 && st.obv[cur] < st.obv[divFrom] {
		add("top_div", "顶部背离：价格新高但 RSI/OBV 走低（派发特征）", false, 0.55, cur, b.Close)
	}
	if st.cmf[cur] < -0.05 && b.Close > mid {
		add("cmf_neg", fmt.Sprintf("CMF %.2f：高位资金净流出", st.cmf[cur]), false, 0.35, cur, b.Close)
	}
	if st.cmf[cur] > 0.05 && inRange && b.Close < mid {
		add("cmf_pos", fmt.Sprintf("CMF +%.2f：低位资金持续流入", st.cmf[cur]), true, 0.35, cur, b.Close)
	}

	score := func(kinds ...string) float64 {
		s := 0.0
		for _, e := range evs {
			for _, k := range kinds {
				if e.Kind == k {
					s += e.Weight
				}
			}
		}
		return s
	}

	// ── 五段判定（优先级：出货 > 拉升 > 洗盘 > 吸筹 > 建仓）──

	// 出货。
	distribScore := score("bc", "ut", "top_div", "cmf_neg")
	if distribScore >= 0.55 && upperThird {
		return PhaseDistrib, math.Min(1, distribScore), evs
	}

	// 拉升：站上区间（或放量突破中）+ 多头排列，顶部证据扣分。
	breakout := b.Close > rHi*(1+reclaimSlip) && b.Volume >= breakoutVolMul*st.volMA[cur]
	aboveRange := b.Close > rHi
	if aboveRange || (breakout && emaBull) {
		s := 0.5
		if breakout {
			s += 0.25
		}
		if emaBull {
			s += 0.2
		}
		structOK := cur >= 3 && bars[cur].Low > bars[cur-2].Low && bars[cur-1].Low > bars[cur-3].Low
		if structOK {
			s += 0.15
		}
		s -= score("top_div", "cmf_neg", "bc", "ut")
		if s >= 0.5 {
			return PhaseMarkup, math.Min(1, math.Max(0.4, s)), evs
		}
	}

	// 洗盘：最近弹簧线且价格已收回区间内/上方。
	springs := recent(ev.spr, cur)
	if len(springs) > 0 && cur-springs[len(springs)-1] <= 10 && b.Close > rLo*0.995 {
		return PhaseShakeout, 0.7, evs
	}

	// 吸筹：区间内 + (ST 或弹簧 或 资金证据≥0.6——双资金证据可替代结构事件，
	// 让吸筹在 ST 确认前就能被识别，避免早期区间被判成观望)。
	if inRange && (len(recent(ev.st, cur)) > 0 || len(springs) > 0 ||
		score("obv_div") > 0 || score("cmf_pos") > 0) {
		s := 0.45
		if len(recent(ev.st, cur)) > 0 {
			s += 0.2
		}
		if len(springs) > 0 {
			s += 0.2
		}
		s += score("obv_div", "cmf_pos")
		if s >= 0.6 {
			return PhaseAccum, math.Min(1, s), evs
		}
	}

	// 建仓：早期 SC，结构未确认。
	scRecent := recent(ev.sc, cur)
	if len(scRecent) > 0 && cur-scRecent[len(scRecent)-1] <= 45 {
		s := 0.4 + score("ar") + score("obv_div")*0.5
		if s >= 0.45 && !inRange {
			return PhaseBuilding, math.Min(0.85, s), evs
		}
	}

	return PhaseNone, 0, evs
}

// Analyze 对完整已闭合 K 线序列跑一遍分析。
func Analyze(bars []model.Bar, symbol, timeframe string) *Result {
	res := &Result{
		Symbol:    symbol,
		Timeframe: timeframe,
		Phase:     PhaseNone,
		Levels:    map[string]float64{},
		UpdatedAt: time.Now().UnixMilli(),
	}
	n := len(bars)
	res.BarCount = n
	if n < minBars {
		res.Evidence = []Evidence{{Kind: "insufficient", Label: fmt.Sprintf("K 线不足（%d<%d）", n, minBars)}}
		return res
	}
	st := computeStats(bars)
	ev := scanEvents(bars, st)
	cur := n - 1

	rHi, rLo := swingRange(bars, cur-rangeWin, cur)
	res.Levels["range_high"] = rHi
	res.Levels["range_low"] = rLo
	res.Levels["current"] = bars[cur].Close
	res.Levels["atr"] = st.atr[cur]

	// 分段遍历（迟滞：新阶段置信度需 ≥0.5 且高于当前 +0.15，否则延续）。
	start := rangeWin + 25 // 留出指标预热
	if start >= cur {
		start = cur
	}
	var segs []Segment
	runPhase, runConf := PhaseNone, 0.0
	segStart := 0
	for i := start; i <= cur; i++ {
		ph, cf, _ := classifyAt(bars, st, ev, i)
		// 迟滞：新阶段置信度需 ≥0.5 且高于当前 +0.15。洗盘是瞬态阶段
		// （弹簧线后 10 根有效），豁免增量——否则 0.7 的洗盘会把 0.65~0.8
		// 的拉升/吸筹卡在门外（合成生命周期实证）。
		barrier := runConf + 0.15
		if runPhase == PhaseShakeout {
			barrier = 0.55
		}
		switchPhase := ph != runPhase && cf >= 0.5 && cf >= barrier
		// 出货风险优先：高置信出货证据直接切换，不被迟滞挡在门外
		// （仅限跨阶段——同阶段重复切换会把段切碎）。
		if ph != runPhase && ph == PhaseDistrib && cf >= 0.8 {
			switchPhase = true
		}
		if switchPhase {
			if i > start || runPhase != PhaseNone {
				segs = append(segs, Segment{Phase: runPhase, From: bars[segStart].Time, To: bars[i].Time, Confidence: runConf * 100})
			}
			runPhase, runConf, segStart = ph, cf, i
		} else if ph == runPhase {
			runConf = math.Max(runConf, cf)
		}
	}
	segs = append(segs, Segment{Phase: runPhase, From: bars[segStart].Time, To: bars[cur].Time, Confidence: runConf * 100})
	// 合并相邻同阶段碎段（迟滞豁免/抖动可能切成零头）。
	merged := segs[:0]
	for _, sg := range segs {
		if n := len(merged); n > 0 && merged[n-1].Phase == sg.Phase {
			merged[n-1].To = sg.To
			if sg.Confidence > merged[n-1].Confidence {
				merged[n-1].Confidence = sg.Confidence
			}
			continue
		}
		merged = append(merged, sg)
	}
	res.Segments = merged

	phase, conf, evs := classifyAt(bars, st, ev, cur)
	if runPhase != PhaseNone && phase == PhaseNone {
		phase, conf = runPhase, runConf
	}
	res.Phase = phase
	res.Confidence = math.Round(conf * 100)
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].Weight > evs[j].Weight })
	if len(evs) > 8 {
		evs = evs[:8]
	}
	res.Evidence = evs

	if ev.scIdx >= 0 {
		res.Levels["sc_low"] = ev.scLow
	}
	for idx, price := range ev.bc {
		res.Levels["bc_high"] = price
		_ = idx
		break
	}
	if s := recent(ev.spr, cur); len(s) > 0 {
		res.Levels["spring_low"] = ev.spr[s[len(s)-1]]
	}

	res.Signal = transitionSignal(segs, bars, cur)
	return res
}

// transitionSignal 阶段在最近几根内发生转换时给出交易信号。
func transitionSignal(segs []Segment, bars []model.Bar, cur int) *Signal {
	if len(segs) < 2 || len(bars) == 0 {
		return nil
	}
	last, prev := segs[len(segs)-1], segs[len(segs)-2]
	if bars[cur].Time-last.From > 3*barSpanMs(bars) {
		return nil
	}
	switch last.Phase {
	case PhaseShakeout:
		return &Signal{Direction: "LONG", Reason: "洗盘确认：弹簧线假跌破收回，主力护盘点明牌", Strength: 0.7}
	case PhaseMarkup:
		if prev.Phase == PhaseAccum || prev.Phase == PhaseShakeout || prev.Phase == PhaseNone || prev.Phase == PhaseBuilding {
			return &Signal{Direction: "LONG", Reason: "拉升启动：放量突破吸筹区间", Strength: 0.75}
		}
	case PhaseDistrib:
		return &Signal{Direction: "SHORT", Reason: "出货信号：顶部高潮/上冲破+量价背离", Strength: 0.7}
	case PhaseAccum:
		if prev.Phase == PhaseDistrib || prev.Phase == PhaseMarkup {
			return &Signal{Direction: "CLOSE", Reason: "拉升结束转入吸筹，落袋观望", Strength: 0.6}
		}
	}
	return nil
}

// barSpanMs 相邻 bar 时间间隔（"最近几根"判定用）。
func barSpanMs(bars []model.Bar) int64 {
	if len(bars) < 2 {
		return int64(time.Hour)
	}
	span := bars[1].Time - bars[0].Time
	if span <= 0 {
		return int64(time.Hour)
	}
	return span
}
