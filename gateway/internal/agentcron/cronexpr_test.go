package agentcron

import (
	"testing"
	"time"
)

func TestParseCron_Valid(t *testing.T) {
	cases := []string{
		"0 8 * * *",         // 每天 8:00
		"*/30 * * * *",      // 每 30 分钟
		"0 9 * * 1-5",       // 工作日 9:00
		"0,30 8-18/2 * * *", // 复合
		"0 0 1 1 *",         // 每年元旦
		"0 0 * * 0",         // 每周日
		"0 0 * * 7",         // 7=周日
		"15 14 1 * *",       // 每月 1 号 14:15
	}
	for _, spec := range cases {
		if _, err := ParseCron(spec); err != nil {
			t.Errorf("%s: %v", spec, err)
		}
	}
}

func TestParseCron_Invalid(t *testing.T) {
	cases := []string{
		"0 8 * *",     // 少一段
		"61 * * * *",  // 分越界
		"* 25 * * *",  // 时越界
		"* * 0 * *",   // 日越界
		"* * * 13 *",  // 月越界
		"* * * * 8",   // 周越界
		"a b c d e",   // 非数字
		"*/0 * * * *", // 步长 0
		"5-1 * * * *", // 逆区间
		"",            // 空
	}
	for _, spec := range cases {
		if _, err := ParseCron(spec); err == nil {
			t.Errorf("%s: expected error, got nil", spec)
		}
	}
}

func TestCronExpr_Next(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	base := time.Date(2026, 10, 2, 10, 30, 0, 0, loc) // 周五

	cases := []struct {
		spec string
		want time.Time
	}{
		{"0 8 * * *", time.Date(2026, 10, 3, 8, 0, 0, 0, loc)},     // 明天 8 点（今天已过）
		{"*/30 * * * *", time.Date(2026, 10, 2, 11, 0, 0, 0, loc)}, // 下个整点半
		{"0 9 * * 1-5", time.Date(2026, 10, 5, 9, 0, 0, 0, loc)},   // 下周一（3-4 是周末）
		{"0 0 * * 6", time.Date(2026, 10, 3, 0, 0, 0, 0, loc)},     // 明天周六 0 点？10/3 是周六
		{"0 10 2 10 *", time.Date(2027, 10, 2, 10, 0, 0, 0, loc)},  // 今天已过 → 明年
	}
	for _, c := range cases {
		expr, err := ParseCron(c.spec)
		if err != nil {
			t.Fatalf("%s: %v", c.spec, err)
		}
		next, err := expr.Next(base)
		if err != nil {
			t.Fatalf("%s: %v", c.spec, err)
		}
		if !next.Equal(c.want) {
			t.Errorf("%s: got %v, want %v", c.spec, next, c.want)
		}
	}
}

func TestCronExpr_Next_SameMinuteExcluded(t *testing.T) {
	expr, _ := ParseCron("* * * * *")
	base := time.Date(2026, 10, 2, 10, 30, 15, 0, time.UTC) // 10:30:15
	next, err := expr.Next(base)
	if err != nil {
		t.Fatal(err)
	}
	if next.Minute() != 31 || next.Second() != 0 {
		t.Errorf("got %v, want 10:31:00", next)
	}
}

func TestNextRun_Timezone(t *testing.T) {
	next, err := NextRun("0 8 * * *", "Asia/Shanghai", time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// 北京时间 10/3 08:00 = UTC 10/3 00:00
	want := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC).Unix()
	if next != want {
		t.Errorf("got %d, want %d", next, want)
	}
}

func TestNextRun_InvalidSpec(t *testing.T) {
	if _, err := NextRun("bad spec", "", time.Now()); err == nil {
		t.Error("expected error for invalid spec")
	}
}

func TestResolveSchedule(t *testing.T) {
	// 合法表达式直接通过
	got, err := ResolveSchedule("0 8 * * *", nil)
	if err != nil || got != "0 8 * * *" {
		t.Errorf("direct expr: %q %v", got, err)
	}

	// 无 resolver：非法表达式报错
	if _, err := ResolveSchedule("每天早上八点", nil); err == nil {
		t.Error("expected error without resolver")
	}

	// 模拟 LLM 转换（带解释文本，需提取表达式并校验）
	fake := func(string) (string, error) { return "表达式：0 8 * * *", nil }
	got, err = ResolveSchedule("每天早上八点", fake)
	if err != nil || got != "0 8 * * *" {
		t.Errorf("nl resolved: %q %v", got, err)
	}

	// 模型给出非法表达式 → 报错
	bad := func(string) (string, error) { return "61 * * * *", nil }
	if _, err := ResolveSchedule("随便", bad); err == nil {
		t.Error("expected error for invalid model output")
	}
}
