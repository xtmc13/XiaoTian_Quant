package backtest

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ── Breakdown Types ───────────────────────────────────────────

// DailyBreakdown holds per-day performance data.
type DailyBreakdown struct {
	Date      string  `json:"date"`
	ReturnPct float64 `json:"return_pct"`
	Equity    float64 `json:"equity"`
	Trades    int     `json:"trades"`
	PnL       float64 `json:"pnl"`
}

// WeeklyBreakdown holds per-week performance data.
type WeeklyBreakdown struct {
	Week      string  `json:"week"`      // "2024-W01"
	ReturnPct float64 `json:"return_pct"`
	Trades    int     `json:"trades"`
	PnL       float64 `json:"pnl"`
	WinRate   float64 `json:"win_rate"`
}

// WeekdayStats holds performance broken down by day of week.
type WeekdayStats struct {
	Weekday    string  `json:"weekday"`     // "Monday", "Tuesday", etc.
	Index      int     `json:"index"`       // 0=Sunday, 1=Monday...
	Trades     int     `json:"trades"`
	Wins       int     `json:"wins"`
	Losses     int     `json:"losses"`
	WinRate    float64 `json:"win_rate"`
	TotalPnL   float64 `json:"total_pnl"`
	AvgPnL     float64 `json:"avg_pnl"`
}

// HourlyBreakdown holds performance by hour of day.
type HourlyBreakdown struct {
	Hour    int     `json:"hour"`
	Trades  int     `json:"trades"`
	Wins    int     `json:"wins"`
	WinRate float64 `json:"win_rate"`
	TotalPnL float64 `json:"total_pnl"`
}

// BreakdownReport contains all breakdown analyses.
type BreakdownReport struct {
	Daily      []DailyBreakdown  `json:"daily"`
	Weekly     []WeeklyBreakdown `json:"weekly"`
	Monthly    []MonthlyReturn   `json:"monthly"`
	Yearly     []YearlyReturn    `json:"yearly"`
	Weekdays   []WeekdayStats    `json:"weekdays"`
	Hourly     []HourlyBreakdown `json:"hourly"`
}

type MonthlyReturn struct {
	Month     string  `json:"month"`
	ReturnPct float64 `json:"return_pct"`
	Trades    int     `json:"trades"`
}

type YearlyReturn struct {
	Year      string  `json:"year"`
	ReturnPct float64 `json:"return_pct"`
	Trades    int     `json:"trades"`
}

// ── Breakdown Generator ────────────────────────────────────────

// GenerateBreakdown creates a comprehensive breakdown report from backtest results.
func GenerateBreakdown(result *RunResult) *BreakdownReport {
	br := &BreakdownReport{}

	if len(result.EquityCurve) > 0 {
		br.Daily = dailyBreakdown(result)
		br.Weekly = weeklyBreakdown(result)
		br.Monthly = monthlyBreakdown(result)
		br.Yearly = yearlyBreakdown(result)
	}

	if len(result.Trades) > 0 {
		br.Weekdays = weekdayBreakdown(result)
		br.Hourly = hourlyBreakdown(result)
	}

	return br
}

// ── Daily ──────────────────────────────────────────────────────

func dailyBreakdown(result *RunResult) []DailyBreakdown {
	if len(result.EquityCurve) < 2 {
		return nil
	}

	type dailyAcc struct {
		equity float64
		trades int
		pnl    float64
		start  float64
	}
	days := make(map[string]*dailyAcc)
	var dayOrder []string

	for _, e := range result.EquityCurve {
		day := time.UnixMilli(e.Timestamp).Format("2006-01-02")
		if _, ok := days[day]; !ok {
			days[day] = &dailyAcc{start: e.Equity}
			dayOrder = append(dayOrder, day)
		}
		days[day].equity = e.Equity
	}

	// Count trades per day
	for _, t := range result.Trades {
		day := time.UnixMilli(t.EntryTime).Format("2006-01-02")
		if d, ok := days[day]; ok {
			d.trades++
			d.pnl += t.RealizedPnL
		}
	}

	sort.Strings(dayOrder)

	result2 := make([]DailyBreakdown, 0, len(dayOrder))
	for _, day := range dayOrder {
		d := days[day]
		ret := 0.0
		if d.start > 0 {
			ret = (d.equity - d.start) / d.start * 100
		}
		result2 = append(result2, DailyBreakdown{
			Date:      day,
			ReturnPct: ret,
			Equity:    d.equity,
			Trades:    d.trades,
			PnL:       d.pnl,
		})
	}
	return result2
}

// ── Weekly ─────────────────────────────────────────────────────

func weeklyBreakdown(result *RunResult) []WeeklyBreakdown {
	type weeklyAcc struct {
		returnPct float64
		trades    int
		pnl       float64
		wins      int
		losses    int
		start     float64
		end       float64
	}
	weeks := make(map[string]*weeklyAcc)
	var weekOrder []string

	for _, e := range result.EquityCurve {
		t := time.UnixMilli(e.Timestamp)
		year, wk := t.ISOWeek()
		key := fmt.Sprintf("%d-W%02d", year, wk)
		if _, ok := weeks[key]; !ok {
			weeks[key] = &weeklyAcc{start: e.Equity}
			weekOrder = append(weekOrder, key)
		}
		weeks[key].end = e.Equity
	}

	for _, t := range result.Trades {
		time := time.UnixMilli(t.EntryTime)
		year, wk := time.ISOWeek()
		key := fmt.Sprintf("%d-W%02d", year, wk)
		if w, ok := weeks[key]; ok {
			w.trades++
			w.pnl += t.RealizedPnL
			if t.RealizedPnL > 0 {
				w.wins++
			} else {
				w.losses++
			}
		}
	}

	for _, key := range weekOrder {
		w := weeks[key]
		if w.start > 0 {
			w.returnPct = (w.end - w.start) / w.start * 100
		}
	}

	result2 := make([]WeeklyBreakdown, 0, len(weekOrder))
	for _, key := range weekOrder {
		w := weeks[key]
		wr := 0.0
		if w.wins+w.losses > 0 {
			wr = float64(w.wins) / float64(w.wins+w.losses) * 100
		}
		result2 = append(result2, WeeklyBreakdown{
			Week:      key,
			ReturnPct: w.returnPct,
			Trades:    w.trades,
			PnL:       w.pnl,
			WinRate:   wr,
		})
	}
	return result2
}

// ── Monthly ────────────────────────────────────────────────────

func monthlyBreakdown(result *RunResult) []MonthlyReturn {
	if len(result.EquityCurve) < 2 {
		return nil
	}

	type mAcc struct {
		returnPct float64
		trades    int
		start     float64
		end       float64
	}
	months := make(map[string]*mAcc)
	var mOrder []string

	for _, e := range result.EquityCurve {
		key := time.UnixMilli(e.Timestamp).Format("2006-01")
		if _, ok := months[key]; !ok {
			months[key] = &mAcc{start: e.Equity}
			mOrder = append(mOrder, key)
		}
		months[key].end = e.Equity
	}

	for _, t := range result.Trades {
		key := time.UnixMilli(t.EntryTime).Format("2006-01")
		if m, ok := months[key]; ok {
			m.trades++
		}
	}

	sort.Strings(mOrder)

	result2 := make([]MonthlyReturn, 0, len(mOrder))
	for _, key := range mOrder {
		m := months[key]
		if m.start > 0 {
			m.returnPct = (m.end - m.start) / m.start * 100
		}
		result2 = append(result2, MonthlyReturn{
			Month:     key,
			ReturnPct: m.returnPct,
			Trades:    m.trades,
		})
	}
	return result2
}

// ── Yearly ─────────────────────────────────────────────────────

func yearlyBreakdown(result *RunResult) []YearlyReturn {
	if len(result.EquityCurve) < 2 {
		return nil
	}

	type yAcc struct {
		returnPct float64
		trades    int
		start     float64
		end       float64
	}
	years := make(map[string]*yAcc)
	var yOrder []string

	for _, e := range result.EquityCurve {
		key := time.UnixMilli(e.Timestamp).Format("2006")
		if _, ok := years[key]; !ok {
			years[key] = &yAcc{start: e.Equity}
			yOrder = append(yOrder, key)
		}
		years[key].end = e.Equity
	}

	for _, t := range result.Trades {
		key := time.UnixMilli(t.EntryTime).Format("2006")
		if y, ok := years[key]; ok {
			y.trades++
		}
	}

	sort.Strings(yOrder)

	result2 := make([]YearlyReturn, 0, len(yOrder))
	for _, key := range yOrder {
		y := years[key]
		if y.start > 0 {
			y.returnPct = (y.end - y.start) / y.start * 100
		}
		result2 = append(result2, YearlyReturn{
			Year:      key,
			ReturnPct: y.returnPct,
			Trades:    y.trades,
		})
	}
	return result2
}

// ── Weekday ────────────────────────────────────────────────────

func weekdayBreakdown(result *RunResult) []WeekdayStats {
	if len(result.Trades) == 0 {
		return nil
	}

	type acc struct {
		trades int
		wins   int
		losses int
		pnl    float64
	}
	weekdays := make(map[int]*acc) // 0=Sun..6=Sat

	for _, t := range result.Trades {
		wd := int(time.UnixMilli(t.EntryTime).Weekday())
		if weekdays[wd] == nil {
			weekdays[wd] = &acc{}
		}
		weekdays[wd].trades++
		weekdays[wd].pnl += t.RealizedPnL
		if t.RealizedPnL > 0 {
			weekdays[wd].wins++
		} else {
			weekdays[wd].losses++
		}
	}

	names := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	result2 := make([]WeekdayStats, 0, 7)
	for i := 0; i < 7; i++ {
		a := weekdays[i]
		if a == nil {
			a = &acc{}
		}
		wr := 0.0
		avg := 0.0
		if a.trades > 0 {
			wr = float64(a.wins) / float64(a.trades) * 100
			avg = a.pnl / float64(a.trades)
		}
		result2 = append(result2, WeekdayStats{
			Weekday:  names[i],
			Index:    i,
			Trades:   a.trades,
			Wins:     a.wins,
			Losses:   a.losses,
			WinRate:  wr,
			TotalPnL: a.pnl,
			AvgPnL:   avg,
		})
	}
	return result2
}

// ── Hourly ─────────────────────────────────────────────────────

func hourlyBreakdown(result *RunResult) []HourlyBreakdown {
	if len(result.Trades) == 0 {
		return nil
	}

	type acc struct {
		trades int
		wins   int
		pnl    float64
	}
	hours := make(map[int]*acc)

	for _, t := range result.Trades {
		h := time.UnixMilli(t.EntryTime).Hour()
		if hours[h] == nil {
			hours[h] = &acc{}
		}
		hours[h].trades++
		hours[h].pnl += t.RealizedPnL
		if t.RealizedPnL > 0 {
			hours[h].wins++
		}
	}

	result2 := make([]HourlyBreakdown, 0, 24)
	for h := 0; h < 24; h++ {
		a := hours[h]
		if a == nil {
			a = &acc{}
		}
		wr := 0.0
		if a.trades > 0 {
			wr = float64(a.wins) / float64(a.trades) * 100
		}
		result2 = append(result2, HourlyBreakdown{
			Hour:     h,
			Trades:   a.trades,
			Wins:     a.wins,
			WinRate:  wr,
			TotalPnL: a.pnl,
		})
	}
	return result2
}

// ── Formatting ─────────────────────────────────────────────────

// FormatBreakdown returns a human-readable summary of the breakdown.
func (br *BreakdownReport) FormatSummary() string {
	var sb strings.Builder
	sb.WriteString("=== Breakdown Summary ===\n\n")

	// Best/worst month
	if len(br.Monthly) > 0 {
		best, worst := br.Monthly[0], br.Monthly[0]
		for _, m := range br.Monthly[1:] {
			if m.ReturnPct > best.ReturnPct {
				best = m
			}
			if m.ReturnPct < worst.ReturnPct {
				worst = m
			}
		}
		sb.WriteString(fmt.Sprintf("Best Month:  %s (%.2f%%)\n", best.Month, best.ReturnPct))
		sb.WriteString(fmt.Sprintf("Worst Month: %s (%.2f%%)\n\n", worst.Month, worst.ReturnPct))
	}

	// Best/worst weekday
	if len(br.Weekdays) > 0 {
		best, worst := br.Weekdays[0], br.Weekdays[0]
		for _, w := range br.Weekdays[1:] {
			if w.AvgPnL > best.AvgPnL {
				best = w
			}
			if w.AvgPnL < worst.AvgPnL {
				worst = w
			}
		}
		sb.WriteString(fmt.Sprintf("Best Day:  %s (avg PnL: %.2f)\n", best.Weekday, best.AvgPnL))
		sb.WriteString(fmt.Sprintf("Worst Day: %s (avg PnL: %.2f)\n\n", worst.Weekday, worst.AvgPnL))
	}

	// Monthly returns table
	if len(br.Monthly) > 0 {
		sb.WriteString("Monthly Returns:\n")
		for _, m := range br.Monthly {
			bar := ""
			for i := 0; i < int(m.ReturnPct/2); i++ {
				bar += "█"
			}
			if m.ReturnPct < 0 {
				bar = ""
				for i := 0; i > int(m.ReturnPct/2); i-- {
					bar += "█"
				}
			}
			sb.WriteString(fmt.Sprintf("  %s: %6.2f%% %s\n", m.Month, m.ReturnPct, bar))
		}
	}

	return sb.String()
}
