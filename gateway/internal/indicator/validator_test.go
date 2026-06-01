package indicator

import (
	"strings"
	"testing"
)

func TestValidateCode_Empty(t *testing.T) {
	result := ValidateCode("")
	if result.Success {
		t.Error("expected failure for empty code")
	}
	if result.ErrorType != "EmptyCode" {
		t.Errorf("ErrorType = %q, want EmptyCode", result.ErrorType)
	}
}

func TestValidateCode_ValidMinimal(t *testing.T) {
	code := `
my_indicator_name = "Test"
my_indicator_description = "Desc"
df = df.copy()
output = {
    "name": "Test",
    "plots": [],
    "signals": []
}
df['buy'] = 0
df['sell'] = 0
`
	result := ValidateCode(code)
	if !result.Success {
		t.Errorf("expected success, got: %s", result.Msg)
	}
	// Should have info hint about missing df.copy (it IS present, so no hint)
	// Wait: hasDfCopy checks for df = df.copy() — it IS there, so no MISSING_DF_COPY
	for _, h := range result.Hints {
		if h.Code == "MISSING_DF_COPY" {
			t.Error("should not warn about df.copy when present")
		}
	}
}

func TestValidateCode_MissingOutput(t *testing.T) {
	code := `
df = df.copy()
df['buy'] = 0
df['sell'] = 0
`
	result := ValidateCode(code)
	if result.Success {
		t.Error("expected failure due to missing output")
	}
	found := false
	for _, h := range result.Hints {
		if h.Code == "MISSING_OUTPUT" {
			found = true
		}
	}
	if !found {
		t.Error("expected MISSING_OUTPUT hint")
	}
}

func TestValidateCode_MissingMeta(t *testing.T) {
	code := `
df = df.copy()
output = {"name": "X", "plots": [], "signals": []}
`
	result := ValidateCode(code)
	hasNameWarn := false
	hasDescWarn := false
	for _, h := range result.Hints {
		if h.Code == "MISSING_INDICATOR_NAME" {
			hasNameWarn = true
		}
		if h.Code == "MISSING_INDICATOR_DESCRIPTION" {
			hasDescWarn = true
		}
	}
	if !hasNameWarn {
		t.Error("expected MISSING_INDICATOR_NAME warn")
	}
	if !hasDescWarn {
		t.Error("expected MISSING_INDICATOR_DESCRIPTION warn")
	}
}

func TestValidateCode_FutureDataLeak(t *testing.T) {
	code := `
my_indicator_name = "Test"
df = df.copy()
df['future'] = df['close'].shift(-1)
output = {"name": "X", "plots": [], "signals": []}
`
	result := ValidateCode(code)
	found := false
	for _, h := range result.Hints {
		if h.Code == "FUTURE_DATA_LEAK" {
			found = true
		}
	}
	if !found {
		t.Error("expected FUTURE_DATA_LEAK hint")
	}
}

func TestValidateCode_UnsafeImport(t *testing.T) {
	code := `
import os
import pandas as pd
my_indicator_name = "Test"
df = df.copy()
output = {"name": "X", "plots": [], "signals": []}
`
	result := ValidateCode(code)
	found := false
	for _, h := range result.Hints {
		if h.Code == "UNSAFE_IMPORT" {
			found = true
		}
	}
	if !found {
		t.Error("expected UNSAFE_IMPORT hint")
	}
}

func TestValidateCode_NdarrayPandasMisuse(t *testing.T) {
	code := `
my_indicator_name = "Test"
df = df.copy()
cond = np.where(df['close'] > 0, 1, 0)
result = cond.rolling(14).mean()
output = {"name": "X", "plots": [], "signals": []}
`
	result := ValidateCode(code)
	found := false
	for _, h := range result.Hints {
		if h.Code == "NDARRAY_PANDAS_METHOD_MISUSE" {
			found = true
		}
	}
	if !found {
		t.Error("expected NDARRAY_PANDAS_METHOD_MISUSE hint")
	}
}

func TestValidateCode_DeclaredParamsNotRead(t *testing.T) {
	code := `
# @param length int 14
my_indicator_name = "Test"
df = df.copy()
output = {"name": "X", "plots": [], "signals": []}
`
	result := ValidateCode(code)
	found := false
	for _, h := range result.Hints {
		if h.Code == "DECLARED_PARAMS_NOT_READ_VIA_PARAMS_GET" {
			found = true
			names, ok := h.Params["names"].([]string)
			if !ok || len(names) != 1 || names[0] != "length" {
				t.Errorf("unexpected names: %v", h.Params["names"])
			}
		}
	}
	if !found {
		t.Error("expected DECLARED_PARAMS_NOT_READ_VIA_PARAMS_GET hint")
	}
}

func TestValidateCode_NoStopLossTakeProfit(t *testing.T) {
	code := `
my_indicator_name = "Test"
df = df.copy()
df['buy'] = 1
df['sell'] = 1
output = {"name": "X", "plots": [], "signals": []}
`
	result := ValidateCode(code)
	found := false
	for _, h := range result.Hints {
		if h.Code == "NO_STOP_AND_TAKE_PROFIT" {
			found = true
		}
	}
	if !found {
		t.Error("expected NO_STOP_AND_TAKE_PROFIT hint")
	}
}

func TestValidateCode_UnknownStrategyKey(t *testing.T) {
	code := `
# @strategy unknownKey 123
my_indicator_name = "Test"
df = df.copy()
output = {"name": "X", "plots": [], "signals": []}
`
	result := ValidateCode(code)
	found := false
	for _, h := range result.Hints {
		if h.Code == "UNKNOWN_STRATEGY_KEY" {
			found = true
		}
	}
	if !found {
		t.Error("expected UNKNOWN_STRATEGY_KEY hint")
	}
}

func TestHasDfCopy(t *testing.T) {
	if !hasDfCopy("df = df.copy()") {
		t.Error("expected true")
	}
	if hasDfCopy("x = df.copy()") {
		t.Error("expected false for x = df.copy()")
	}
}

func TestHasOutputVar(t *testing.T) {
	if !hasOutputVar("output = {") {
		t.Error("expected true")
	}
	if hasOutputVar("x = 1") {
		t.Error("expected false")
	}
}

func TestHasBuySellColumns(t *testing.T) {
	if !hasBuySellColumns("df['buy'] = 0\ndf['sell'] = 0") {
		t.Error("expected true")
	}
	if hasBuySellColumns("df['buy'] = 0") {
		t.Error("expected false when only buy")
	}
}

func TestStripComments(t *testing.T) {
	code := `x = 1 # inline comment
# full line comment
y = "string with # hash"
`
	clean := stripComments(code)
	if strings.Contains(clean, "inline comment") {
		t.Error("inline comment should be stripped")
	}
	if strings.Contains(clean, "full line comment") {
		t.Error("full line comment should be stripped")
	}
	if !strings.Contains(clean, `y = "string with # hash"`) {
		t.Error("string with # should be preserved")
	}
}

func TestExtractSnippet(t *testing.T) {
	s := "01234567890123456789"
	snippet := extractSnippet(s, 10, 6)
	if snippet != "789012" {
		t.Errorf("snippet = %q", snippet)
	}
}

func TestValidateOutputJSON(t *testing.T) {
	validJSON := `{"name":"Test","plots":[{"name":"Line","data":[1,2,3]}],"signals":[]}`
	output, hints := ValidateOutputJSON(validJSON)
	if output.Name != "Test" {
		t.Errorf("Name = %q", output.Name)
	}
	if len(hints) > 0 {
		t.Errorf("expected no hints for valid JSON, got %v", hints)
	}

	invalidJSON := `{invalid`
	_, hints = ValidateOutputJSON(invalidJSON)
	if len(hints) == 0 {
		t.Error("expected error hint for invalid JSON")
	}

	missingName := `{"plots":[],"signals":[]}`
	_, hints = ValidateOutputJSON(missingName)
	found := false
	for _, h := range hints {
		if h.Code == "OUTPUT_MISSING_NAME" {
			found = true
		}
	}
	if !found {
		t.Error("expected OUTPUT_MISSING_NAME hint")
	}
}
