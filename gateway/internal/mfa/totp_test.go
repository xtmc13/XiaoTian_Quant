package mfa

import (
	"strings"
	"testing"
	"time"
)

// TestTOTPValidateCurrentCode: 当前 30s 窗口的码必须通过。
func TestTOTPValidateCurrentCode(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	now := time.Now()
	code, err := CodeAt(secret, now)
	if err != nil {
		t.Fatalf("CodeAt: %v", err)
	}
	if !Validate(secret, code, now) {
		t.Fatalf("current code %s should validate", code)
	}
}

// TestTOTPValidatePreviousWindow: 上一个窗口（t-30s）的码必须通过（时钟偏移容忍）。
func TestTOTPValidatePreviousWindow(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	now := time.Now()
	prev, err := CodeAt(secret, now.Add(-Step))
	if err != nil {
		t.Fatalf("CodeAt: %v", err)
	}
	if !Validate(secret, prev, now) {
		t.Fatalf("previous-window code %s should validate", prev)
	}
}

// TestTOTPValidateRejectsWrongCode: 错误码必须拒绝。
func TestTOTPValidateRejectsWrongCode(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	now := time.Now()
	code, _ := CodeAt(secret, now)
	wrong := code
	if wrong[0] != '0' {
		wrong = "0" + wrong[1:]
	} else {
		wrong = "1" + wrong[1:]
	}
	if Validate(secret, wrong, now) {
		t.Fatalf("wrong code %s must be rejected", wrong)
	}
	if Validate(secret, "abc123", now) {
		t.Fatal("non-numeric code must be rejected")
	}
	if Validate(secret, "", now) {
		t.Fatal("empty code must be rejected")
	}
}

// TestTOTPRFC6238Vector: RFC 6238 SHA-1 测试向量（T=59s → 94287082，
// 6 位截断为 287082），验证 HMAC-SHA1 截断实现与标准一致。
func TestTOTPRFC6238Vector(t *testing.T) {
	// RFC 6238 附录 B 共享秘密：ASCII "12345678901234567890" 的 base32 编码。
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	tm := time.Unix(59, 0).UTC()
	got, err := CodeAt(secret, tm)
	if err != nil {
		t.Fatalf("CodeAt: %v", err)
	}
	if got != "287082" {
		t.Fatalf("RFC 6238 T=59: got %s, want 287082", got)
	}
	if !Validate(secret, "287082", tm) {
		t.Fatal("RFC vector code must validate at T=59")
	}
}

// TestGenerateBackupCodes: 8 个 9 字符（4+1+4）且互不相同的备用码。
func TestGenerateBackupCodes(t *testing.T) {
	codes := GenerateBackupCodes(8)
	if len(codes) != 8 {
		t.Fatalf("want 8 codes, got %d", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 9 {
			t.Fatalf("code %q should be 9 chars (XXXX-YYYY)", c)
		}
		if seen[c] {
			t.Fatalf("duplicate code %q", c)
		}
		seen[c] = true
	}
}

// TestOTPAuthURI: otpauth URI 必须包含 secret/issuer/digits/period。
func TestOTPAuthURI(t *testing.T) {
	uri := OTPAuthURI("XiaoTian", "alice", "JBSWY3DPEHPK3PXP")
	for _, part := range []string{"otpauth://totp/XiaoTian:alice?", "secret=JBSWY3DPEHPK3PXP", "issuer=XiaoTian", "digits=6", "period=30"} {
		if !strings.Contains(uri, part) {
			t.Fatalf("URI %q missing %q", uri, part)
		}
	}
}
