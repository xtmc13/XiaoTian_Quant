package indicator

import "testing"

// 回归：fallbackTemplateCode 的日志截断曾在 prompt 短于 200 字节时 panic
// （生产实证 slice bounds [:200] len=33，2026-10-10）。
func TestFallbackTemplateCodeShortPromptNoPanic(t *testing.T) {
	for _, p := range []string{"", "短", "RSI 策略，30 字节内"} {
		if code := fallbackTemplateCode(p); code == "" {
			t.Fatalf("prompt %q: empty fallback code", p)
		}
	}
	long := string(make([]rune, 500))
	for i, r := range long {
		_ = i
		_ = r
	}
	if code := fallbackTemplateCode("超长" + string(make([]byte, 500))); code == "" {
		t.Fatal("long prompt: empty fallback code")
	}
}
