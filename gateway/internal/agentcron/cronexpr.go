package agentcron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ── 极简 5 段 cron 表达式（分 时 日 月 周），不引第三方依赖 ──
// 支持：*  ,  -  /  数字；周 0-6（0=周日，7 视同 0）。不支持名字（JAN/MON）。

type fieldSet struct {
	allow [60]bool
	any   bool
}

func parseField(field string, min, max int, sundayAlias bool) (*fieldSet, error) {
	fs := &fieldSet{}
	field = strings.TrimSpace(field)
	if field == "" {
		return nil, fmt.Errorf("empty field")
	}
	for _, part := range strings.Split(field, ",") {
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("invalid step %q", part)
			}
			step = n
			part = part[:i]
		}
		// lo/hi 必须在同一作用域内赋值（供下方填充循环使用）
		lo, hi := min, max
		switch {
		case part == "*" || part == "?":
			// 全区间
		case strings.Contains(part, "-"):
			segs := strings.SplitN(part, "-", 2)
			l, err1 := strconv.Atoi(segs[0])
			h, err2 := strconv.Atoi(segs[1])
			if err1 != nil || err2 != nil || l > h {
				return nil, fmt.Errorf("invalid range %q", part)
			}
			lo, hi = l, h
		default:
			v, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid value %q", part)
			}
			lo, hi = v, v
		}
		if lo < min || hi > max {
			return nil, fmt.Errorf("value out of range [%d,%d]: %q", min, max, part)
		}
		for v := lo; v <= hi; v += step {
			fs.allow[normDow(v, max, sundayAlias)] = true
		}
	}
	for _, ok := range fs.allow {
		if ok {
			fs.any = true
			break
		}
	}
	if !fs.any {
		return nil, fmt.Errorf("field matches nothing: %q", field)
	}
	return fs, nil
}

// normDow 周日 7 归一为 0（仅 dow 字段 max==7 时启用）。
func normDow(v, max int, sundayAlias bool) int {
	if sundayAlias && max == 7 && v == 7 {
		return 0
	}
	return v
}

// CronExpr 编译后的表达式。
type CronExpr struct {
	minute, hour, dom, month, dow *fieldSet
}

// ParseCron 解析 5 段 cron 表达式。
func ParseCron(spec string) (*CronExpr, error) {
	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron 表达式需为 5 段（分 时 日 月 周）：%q", spec)
	}
	minute, err := parseField(fields[0], 0, 59, false)
	if err != nil {
		return nil, fmt.Errorf("分字段: %w", err)
	}
	hour, err := parseField(fields[1], 0, 23, false)
	if err != nil {
		return nil, fmt.Errorf("时字段: %w", err)
	}
	dom, err := parseField(fields[2], 1, 31, false)
	if err != nil {
		return nil, fmt.Errorf("日字段: %w", err)
	}
	month, err := parseField(fields[3], 1, 12, false)
	if err != nil {
		return nil, fmt.Errorf("月字段: %w", err)
	}
	dow, err := parseField(fields[4], 0, 7, true)
	if err != nil {
		return nil, fmt.Errorf("周字段: %w", err)
	}
	return &CronExpr{minute: minute, hour: hour, dom: dom, month: month, dow: dow}, nil
}

func (e *CronExpr) matches(t time.Time) bool {
	return e.minute.allow[t.Minute()] &&
		e.hour.allow[t.Hour()] &&
		e.dom.allow[t.Day()] &&
		e.month.allow[int(t.Month())] &&
		e.dow.allow[int(t.Weekday())]
}

// Next 返回 after 之后的下一次触发时间（分钟粒度，本地时区由调用方决定）。
// 一年内无匹配返回 error。
func (e *CronExpr) Next(after time.Time) (time.Time, error) {
	t := after.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 366*24*60; i++ {
		if e.matches(t) {
			return t, nil
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("未来一年内无触发点")
}

// NextRun 解析表达式并计算下次触发（unix 秒）。
func NextRun(spec, timezone string, after time.Time) (int64, error) {
	expr, err := ParseCron(spec)
	if err != nil {
		return 0, err
	}
	loc := time.Local
	if timezone != "" {
		if l, err := time.LoadLocation(timezone); err == nil {
			loc = l
		}
	}
	next, err := expr.Next(after.In(loc))
	if err != nil {
		return 0, err
	}
	return next.Unix(), nil
}
