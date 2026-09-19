package strategy

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 轻量计划调度（A7.2） ──
// 分钟级调度器，不引第三方依赖。支持三种声明：
//
//	@every 4h        每 4 小时（Go duration：m/h/d 后缀，如 30m/4h/1d）
//	daily@14:30      每天 UTC 14:30
//	weekly@mon       每周一 UTC 00:00
//	weekly@mon@09:30 每周一 UTC 09:30
//
// 也兼容 @daily（=daily@00:00）与 @weekly（=weekly@mon）。

// ScheduleSpec 是解析后的计划。
type ScheduleSpec struct {
	Raw     string
	Kind    string        // every | daily | weekly
	Every   time.Duration // Kind=every 时的间隔
	Hour    int           // daily/weekly 的 UTC 时
	Minute  int           // daily/weekly 的 UTC 分
	Weekday int           // Kind=weekly：0=周日 .. 6=周六
}

var weekdayNames = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

// ParseSchedule 解析调度声明，非法输入返回错误。
func ParseSchedule(raw string) (*ScheduleSpec, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return nil, fmt.Errorf("empty schedule")
	}
	// @every 4h
	if rest, ok := strings.CutPrefix(s, "@every"); ok {
		d, err := parseSimpleDuration(strings.TrimSpace(rest))
		if err != nil {
			return nil, fmt.Errorf("schedule %q: %w", raw, err)
		}
		if d < time.Minute {
			return nil, fmt.Errorf("schedule %q: interval must be >= 1m", raw)
		}
		return &ScheduleSpec{Raw: raw, Kind: "every", Every: d}, nil
	}
	if s == "@daily" {
		s = "daily@00:00"
	}
	if s == "@weekly" {
		s = "weekly@mon"
	}
	if rest, ok := strings.CutPrefix(s, "daily@"); ok {
		h, m, err := parseHHMM(rest)
		if err != nil {
			return nil, fmt.Errorf("schedule %q: %w", raw, err)
		}
		return &ScheduleSpec{Raw: raw, Kind: "daily", Hour: h, Minute: m}, nil
	}
	if rest, ok := strings.CutPrefix(s, "weekly@"); ok {
		parts := strings.Split(rest, "@")
		day, ok := weekdayNames[strings.TrimSpace(parts[0])]
		if !ok {
			return nil, fmt.Errorf("schedule %q: weekday must be sun|mon|tue|wed|thu|fri|sat", raw)
		}
		spec := &ScheduleSpec{Raw: raw, Kind: "weekly", Weekday: day, Hour: 0, Minute: 0}
		if len(parts) == 2 {
			h, m, err := parseHHMM(parts[1])
			if err != nil {
				return nil, fmt.Errorf("schedule %q: %w", raw, err)
			}
			spec.Hour, spec.Minute = h, m
		} else if len(parts) > 2 {
			return nil, fmt.Errorf("schedule %q: too many @ segments", raw)
		}
		return spec, nil
	}
	return nil, fmt.Errorf("schedule %q: unsupported format（支持 @every <dur> / daily@hh:mm / weekly@day[@hh:mm]）", raw)
}

// parseSimpleDuration 解析 30m/4h/1d 形式的时长（也接受 Go duration 串）。
func parseSimpleDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("missing duration")
	}
	// 30m / 4h / 1d 简写
	if len(s) >= 2 {
		num, err := strconv.Atoi(s[:len(s)-1])
		if err == nil && num > 0 {
			switch s[len(s)-1] {
			case 'm':
				return time.Duration(num) * time.Minute, nil
			case 'h':
				return time.Duration(num) * time.Hour, nil
			case 'd':
				return time.Duration(num) * 24 * time.Hour, nil
			}
		}
	}
	return time.ParseDuration(s)
}

func parseHHMM(s string) (h, m int, err error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("time must be hh:mm")
	}
	h, err = strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("hour out of range")
	}
	m, err = strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("minute out of range")
	}
	return h, m, nil
}

// NextAfter 返回严格晚于 t 的下一次触发时间（UTC）。
func (s *ScheduleSpec) NextAfter(t time.Time) time.Time {
	t = t.UTC()
	switch s.Kind {
	case "every":
		return t.Add(s.Every)
	case "daily":
		next := time.Date(t.Year(), t.Month(), t.Day(), s.Hour, s.Minute, 0, 0, time.UTC)
		if !next.After(t) {
			next = next.Add(24 * time.Hour)
		}
		return next
	case "weekly":
		days := (s.Weekday - int(t.Weekday()) + 7) % 7
		next := time.Date(t.Year(), t.Month(), t.Day()+days, s.Hour, s.Minute, 0, 0, time.UTC)
		if !next.After(t) {
			next = next.AddDate(0, 0, 7)
		}
		return next
	}
	return t.Add(time.Hour)
}

// ── 引擎侧调度注册 ──

// ScheduleProvider 可选接口：策略声明计划调度（通常读 params["schedule"]）。
type ScheduleProvider interface {
	Schedule() string
}

// OnScheduler 可选接口：到点回调。未实现时引擎默认用最近一根主周期 K 线调 OnBar。
type OnScheduler interface {
	OnSchedule(t time.Time, bus *event.EventBus) (*model.Signal, error)
}
