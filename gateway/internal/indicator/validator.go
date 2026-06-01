package indicator

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ValidateCode performs static analysis on indicator source code.
// It returns a ValidateResult with hints of severity error/warn/info.
func ValidateCode(code string) ValidateResult {
	result := ValidateResult{
		Success: true,
		Hints:   make([]ValidationHint, 0),
	}

	if strings.TrimSpace(code) == "" {
		result.Success = false
		result.Msg = "Code is empty"
		result.ErrorType = "EmptyCode"
		result.Hints = append(result.Hints, ValidationHint{
			Severity: "error",
			Code:     "EMPTY_CODE",
		})
		return result
	}

	// 1. Check metadata
	name, desc := ExtractIndicatorMeta(code)
	if name == "" {
		result.Hints = append(result.Hints, ValidationHint{
			Severity: "warn",
			Code:     "MISSING_INDICATOR_NAME",
		})
	}
	if desc == "" {
		result.Hints = append(result.Hints, ValidationHint{
			Severity: "warn",
			Code:     "MISSING_INDICATOR_DESCRIPTION",
		})
	}

	// 2. Check df.copy()
	if !hasDfCopy(code) {
		result.Hints = append(result.Hints, ValidationHint{
			Severity: "info",
			Code:     "MISSING_DF_COPY",
		})
	}

	// 3. Check output variable
	if !hasOutputVar(code) {
		result.Success = false
		result.Msg = "Missing 'output' variable"
		result.ErrorType = "MissingOutput"
		result.Hints = append(result.Hints, ValidationHint{
			Severity: "error",
			Code:     "MISSING_OUTPUT",
		})
	}

	// 4. Check buy/sell signals
	if !hasBuySellColumns(code) {
		result.Hints = append(result.Hints, ValidationHint{
			Severity: "warn",
			Code:     "MISSING_BUY_SELL_COLUMNS",
		})
	}

	// 5. Check param declarations are read
	if unread := FindUnreadParams(code); len(unread) > 0 {
		result.Hints = append(result.Hints, ValidationHint{
			Severity: "warn",
			Code:     "DECLARED_PARAMS_NOT_READ_VIA_PARAMS_GET",
			Params:   map[string]any{"names": unread},
		})
	}

	// 6. Check future data leak
	if leaks := findFutureDataLeak(code); len(leaks) > 0 {
		for _, leak := range leaks {
			result.Hints = append(result.Hints, ValidationHint{
				Severity: "error",
				Code:     "FUTURE_DATA_LEAK",
				Params:   leak,
			})
		}
	}

	// 7. Check ndarray-pandas misuse
	if misuses := findNdarrayPandasMisuse(code); len(misuses) > 0 {
		for _, misuse := range misuses {
			result.Hints = append(result.Hints, ValidationHint{
				Severity: "error",
				Code:     "NDARRAY_PANDAS_METHOD_MISUSE",
				Params:   misuse,
			})
		}
	}

	// 8. Check unsafe imports
	if unsafe := findUnsafeImports(code); len(unsafe) > 0 {
		result.Hints = append(result.Hints, ValidationHint{
			Severity: "error",
			Code:     "UNSAFE_IMPORT",
			Params:   map[string]any{"modules": unsafe},
		})
	}

	// 9. Check strategy annotations
	parsed := ParseSource(code)
	if hasTradingSignals(code) {
		cfg := parsed.StrategyConfig
		if cfg.StopLossPct == 0 && cfg.TakeProfitPct == 0 {
			result.Hints = append(result.Hints, ValidationHint{
				Severity: "warn",
				Code:     "NO_STOP_AND_TAKE_PROFIT",
			})
		} else {
			if cfg.StopLossPct == 0 {
				result.Hints = append(result.Hints, ValidationHint{
					Severity: "warn",
					Code:     "NO_STOP_LOSS",
				})
			}
			if cfg.TakeProfitPct == 0 {
				result.Hints = append(result.Hints, ValidationHint{
					Severity: "warn",
					Code:     "NO_TAKE_PROFIT",
				})
			}
		}
	}

	// 10. Check unknown strategy keys
	for _, line := range strings.Split(code, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		if m := strategyRegex.FindStringSubmatch(line); m != nil {
			key := m[1]
			if !ValidStrategyKeys[key] {
				result.Hints = append(result.Hints, ValidationHint{
					Severity: "warn",
					Code:     "UNKNOWN_STRATEGY_KEY",
					Params:   map[string]any{"key": key},
				})
			}
		}
	}

	// Build human-readable msg
	if !result.Success {
		if result.Msg == "" {
			result.Msg = "Validation failed"
		}
	} else if len(result.Hints) > 0 {
		errCount := 0
		for _, h := range result.Hints {
			if h.Severity == "error" {
				errCount++
			}
		}
		if errCount > 0 {
			result.Success = false
			result.Msg = fmt.Sprintf("Found %d error(s)", errCount)
			result.ErrorType = "ValidationError"
		} else {
			result.Msg = "Validation passed with warnings"
		}
	} else {
		result.Msg = "Validation passed"
	}

	return result
}

// hasDfCopy checks if df.copy() is called in code.
func hasDfCopy(code string) bool {
	re := regexp.MustCompile(`(?m)^\s*df\s*=\s*df\.copy\s*\(\s*\)`)
	return re.MatchString(code)
}

// hasOutputVar checks if 'output' variable is assigned as a dict.
func hasOutputVar(code string) bool {
	re := regexp.MustCompile(`\boutput\s*=\s*\{`)
	return re.MatchString(code)
}

// hasBuySellColumns checks if df['buy'] and df['sell'] are assigned.
func hasBuySellColumns(code string) bool {
	hasBuy := regexp.MustCompile(`df\s*\[\s*['"']buy['"']\s*\]`).MatchString(code)
	hasSell := regexp.MustCompile(`df\s*\[\s*['"']sell['"']\s*\]`).MatchString(code)
	return hasBuy && hasSell
}

// hasTradingSignals checks if there are likely trading signals in the code.
func hasTradingSignals(code string) bool {
	return hasBuySellColumns(code)
}

// findFutureDataLeak detects usage of future data.
func findFutureDataLeak(code string) []map[string]any {
	var leaks []map[string]any
	clean := stripComments(code)

	// Pattern 1: .shift(-N) where N is positive
	shiftRe := regexp.MustCompile(`\.shift\s*\(\s*-\s*(\d+)\s*\)`)
	for _, m := range shiftRe.FindAllStringSubmatchIndex(clean, -1) {
		snippet := extractSnippet(clean, m[0], 40)
		leaks = append(leaks, map[string]any{
			"kind":    "shift",
			"snippet": snippet,
		})
	}

	// Pattern 2: .iloc[i+N] or .iloc[var+N] where N is positive (forward look)
	ilocRe := regexp.MustCompile(`\.iloc\s*\[\s*\w+\s*\+\s*(\d+)\s*\]`)
	for _, m := range ilocRe.FindAllStringSubmatchIndex(clean, -1) {
		snippet := extractSnippet(clean, m[0], 40)
		leaks = append(leaks, map[string]any{
			"kind":    "iloc",
			"snippet": snippet,
		})
	}

	// Pattern 3: bars_ago(-N)
	barsRe := regexp.MustCompile(`bars_ago\s*\(\s*-\s*(\d+)\s*\)`)
	for _, m := range barsRe.FindAllStringSubmatchIndex(clean, -1) {
		snippet := extractSnippet(clean, m[0], 40)
		leaks = append(leaks, map[string]any{
			"kind":    "bars_ago",
			"snippet": snippet,
		})
	}

	return leaks
}

// findNdarrayPandasMisuse detects ndarray variables calling pandas methods.
func findNdarrayPandasMisuse(code string) []map[string]any {
	var misuses []map[string]any
	clean := stripComments(code)

	// Pattern 1: Direct chain on np.where/maximum/minimum/abs result
	directRe := regexp.MustCompile(`np\.(?:where|maximum|minimum|abs)\s*\([^)]{0,200}\)\s*\.(rolling|fillna|shift|ewm|iloc|tolist)`)
	for _, m := range directRe.FindAllStringSubmatchIndex(clean, -1) {
		snippet := extractSnippet(clean, m[0], 50)
		method := ""
		if sub := directRe.FindStringSubmatch(snippet); len(sub) > 1 {
			method = sub[1]
		}
		misuses = append(misuses, map[string]any{
			"symbol": "np.ndarray",
			"method": method,
			"snippet": snippet,
		})
	}

	// Pattern 2: Track tainted variables from np.where/maximum/minimum
	// Find assignments like: x = np.where(...)
	assignRe := regexp.MustCompile(`(?m)^\s*(\w+)\s*=\s*np\.(?:where|maximum|minimum|abs)\s*\(`)
	tainted := make(map[string]bool)
	for _, m := range assignRe.FindAllStringSubmatch(clean, -1) {
		if len(m) > 1 {
			tainted[m[1]] = true
		}
	}

	// Check if tainted vars are used with pandas methods
	for varName := range tainted {
		pattern := fmt.Sprintf(`\b%s\b\s*\.(rolling|fillna|shift|ewm|iloc|tolist)`, regexp.QuoteMeta(varName))
		re := regexp.MustCompile(pattern)
		for _, m := range re.FindAllStringSubmatchIndex(clean, -1) {
			snippet := extractSnippet(clean, m[0], 50)
			method := ""
			if sub := re.FindStringSubmatch(snippet); len(sub) > 1 {
				method = sub[1]
			}
			misuses = append(misuses, map[string]any{
				"symbol":  varName,
				"method":  method,
				"snippet": snippet,
			})
		}
	}

	return misuses
}

// unsafeModules lists Python modules that should not be imported.
var unsafeModules = []string{
	"os", "sys", "subprocess", "socket", "threading", "multiprocessing",
	"requests", "urllib", "http", "ftplib", "smtplib", "sqlite3",
	"pickle", "marshal", "ctypes", "builtins",
}

// findUnsafeImports detects import statements for unsafe modules.
func findUnsafeImports(code string) []string {
	var unsafe []string
	clean := stripComments(code)

	// Match "import os" or "from os import ..."
	for _, mod := range unsafeModules {
		// import os
		impRe := regexp.MustCompile(fmt.Sprintf(`(?m)^\s*import\s+%s\b`, mod))
		if impRe.MatchString(clean) {
			unsafe = append(unsafe, mod)
			continue
		}
		// from os import ...
		fromRe := regexp.MustCompile(fmt.Sprintf(`(?m)^\s*from\s+%s\s+import`, mod))
		if fromRe.MatchString(clean) {
			unsafe = append(unsafe, mod)
		}
	}

	return unsafe
}

// stripComments removes Python comments from code while preserving strings.
func stripComments(code string) string {
	var result strings.Builder
	lines := strings.Split(code, "\n")
	for _, line := range lines {
		stripped := stripLineComment(line)
		result.WriteString(stripped)
		result.WriteString("\n")
	}
	return result.String()
}

// stripLineComment removes the comment part from a single line, preserving strings.
func stripLineComment(line string) string {
	inString := false
	stringChar := rune(0)
	escaped := false

	for i, ch := range line {
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if !inString && (ch == '"' || ch == '\'') {
			inString = true
			stringChar = ch
			continue
		}
		if inString && ch == stringChar {
			inString = false
			continue
		}
		if !inString && ch == '#' {
			return line[:i]
		}
	}
	return line
}

// extractSnippet returns a substring around the given position with max length.
func extractSnippet(s string, pos, maxLen int) string {
	start := pos - maxLen/2
	if start < 0 {
		start = 0
	}
	end := pos + maxLen/2
	if end > len(s) {
		end = len(s)
	}
	snippet := s[start:end]
	snippet = strings.ReplaceAll(snippet, "\n", " ")
	return strings.TrimSpace(snippet)
}

// ValidateOutputJSON checks if the output JSON string conforms to the contract.
func ValidateOutputJSON(outputJSON string) (IndicatorOutput, []ValidationHint) {
	var output IndicatorOutput
	var hints []ValidationHint

	if err := json.Unmarshal([]byte(outputJSON), &output); err != nil {
		hints = append(hints, ValidationHint{
			Severity: "error",
			Code:     "INVALID_OUTPUT_JSON",
			Params:   map[string]any{"error": err.Error()},
		})
		return output, hints
	}

	if output.Name == "" {
		hints = append(hints, ValidationHint{
			Severity: "warn",
			Code:     "OUTPUT_MISSING_NAME",
		})
	}

	for i, plot := range output.Plots {
		if plot.Name == "" {
			hints = append(hints, ValidationHint{
				Severity: "warn",
				Code:     "PLOT_MISSING_NAME",
				Params:   map[string]any{"index": i},
			})
		}
	}

	return output, hints
}
