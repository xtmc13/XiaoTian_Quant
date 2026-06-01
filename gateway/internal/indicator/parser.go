package indicator

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// paramRegex matches: # @param name type default [description] [range=... or values=...]
// Example: # @param fast_period int 12 Fast MA period range=5:50:1
// Example: # @param mode str "ema" MA type values=ema,sma,wma
var paramRegex = regexp.MustCompile(`^\s*#\s*@param\s+(\S+)\s+(\S+)\s+(\S+)(?:\s+(.+))?$`)

// strategyRegex matches: # @strategy key value
var strategyRegex = regexp.MustCompile(`^\s*#\s*@strategy\s+(\S+)\s+(.+)$`)

// metaNameRegex matches: my_indicator_name = "..." or my_indicator_name = '...'
var metaNameRegex = regexp.MustCompile(`(?m)^\s*my_indicator_name\s*=\s*(?:"([^"]*)"|'([^']*)')\s*$`)

// metaDescRegex matches: my_indicator_description = "..." or my_indicator_description = '...'
var metaDescRegex = regexp.MustCompile(`(?m)^\s*my_indicator_description\s*=\s*(?:"([^"]*)"|'([^']*)')\s*$`)

// rangeRegex matches: range=min:max:step
var rangeRegex = regexp.MustCompile(`range\s*=\s*([\d.]+)\s*:\s*([\d.]+)\s*:\s*([\d.]+)`)

// valuesRegex matches: values=a,b,c
var valuesRegex = regexp.MustCompile(`values\s*=\s*(.+)$`)

// ParseSource extracts all metadata from indicator source code.
func ParseSource(code string) ParseResult {
	result := ParseResult{
		Params: make([]ParamDecl, 0),
	}

	// Extract name and description
	if m := metaNameRegex.FindStringSubmatch(code); m != nil {
		result.Name = strings.TrimSpace(firstNonEmpty(m[1], m[2]))
	}
	if m := metaDescRegex.FindStringSubmatch(code); m != nil {
		result.Description = strings.TrimSpace(firstNonEmpty(m[1], m[2]))
	}

	// Extract @param and @strategy from comments
	lines := strings.Split(code, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}

		// Try @param
		if m := paramRegex.FindStringSubmatch(line); m != nil {
			param := parseParamLine(m[1], m[2], m[3], m[4])
			result.Params = append(result.Params, param)
			continue
		}

		// Try @strategy
		if m := strategyRegex.FindStringSubmatch(line); m != nil {
			applyStrategyLine(&result.StrategyConfig, m[1], strings.TrimSpace(m[2]))
		}
	}

	return result
}

func parseParamLine(name, typ, defaultVal, rest string) ParamDecl {
	param := ParamDecl{
		Name:        name,
		Type:        normalizeParamType(typ),
		Description: strings.TrimSpace(rest),
	}

	// Extract range or values from the description tail
	if m := rangeRegex.FindStringSubmatch(rest); m != nil {
		min, _ := strconv.ParseFloat(m[1], 64)
		max, _ := strconv.ParseFloat(m[2], 64)
		step, _ := strconv.ParseFloat(m[3], 64)
		param.Range = &ParamRange{Min: min, Max: max, Step: step}
		// Strip the range part from description
		param.Description = strings.TrimSpace(rangeRegex.ReplaceAllString(rest, ""))
	}

	if m := valuesRegex.FindStringSubmatch(rest); m != nil {
		raw := strings.Split(m[1], ",")
		param.Values = make([]any, 0, len(raw))
		for _, v := range raw {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			// Try to parse as number, else keep as string
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				param.Values = append(param.Values, f)
			} else {
				param.Values = append(param.Values, v)
			}
		}
		param.Description = strings.TrimSpace(valuesRegex.ReplaceAllString(rest, ""))
	}

	// Parse default value according to type
	param.Default = parseDefaultValue(param.Type, defaultVal)

	return param
}

func normalizeParamType(t string) string {
	t = strings.ToLower(t)
	switch t {
	case "string":
		return "str"
	case "integer":
		return "int"
	case "boolean":
		return "bool"
	case "double", "number":
		return "float"
	default:
		return t
	}
}

func parseDefaultValue(typ, val string) any {
	switch typ {
	case "int":
		if v, err := strconv.ParseInt(val, 10, 64); err == nil {
			return int(v)
		}
	case "float":
		if v, err := strconv.ParseFloat(val, 64); err == nil {
			return v
		}
	case "bool":
		v := strings.ToLower(val)
		return v == "true" || v == "1" || v == "yes"
	case "str":
		// Strip surrounding quotes if present
		val = strings.Trim(val, `"'`)
		return val
	}
	return val
}

func applyStrategyLine(cfg *StrategyConfig, key, value string) {
	switch key {
	case "stopLossPct":
		cfg.StopLossPct, _ = strconv.ParseFloat(value, 64)
	case "takeProfitPct":
		cfg.TakeProfitPct, _ = strconv.ParseFloat(value, 64)
	case "entryPct":
		cfg.EntryPct, _ = strconv.ParseFloat(value, 64)
	case "trailingEnabled":
		v := strings.ToLower(value)
		cfg.TrailingEnabled = v == "true" || v == "1" || v == "yes"
	case "trailingStopPct":
		cfg.TrailingStopPct, _ = strconv.ParseFloat(value, 64)
	case "trailingActivationPct":
		cfg.TrailingActivationPct, _ = strconv.ParseFloat(value, 64)
	case "tradeDirection":
		cfg.TradeDirection = value
	}
}

// ExtractIndicatorMeta extracts name/description from code variables.
func ExtractIndicatorMeta(code string) (name, description string) {
	if m := metaNameRegex.FindStringSubmatch(code); m != nil {
		name = strings.TrimSpace(firstNonEmpty(m[1], m[2]))
	}
	if m := metaDescRegex.FindStringSubmatch(code); m != nil {
		description = strings.TrimSpace(firstNonEmpty(m[1], m[2]))
	}
	return
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// FindDeclaredParamNames returns just the parameter names declared in code.
func FindDeclaredParamNames(code string) []string {
	result := ParseSource(code)
	names := make([]string, 0, len(result.Params))
	for _, p := range result.Params {
		names = append(names, p.Name)
	}
	return names
}

// FindUnreadParams returns parameter names that are declared but not read via params.get().
func FindUnreadParams(code string) []string {
	declared := FindDeclaredParamNames(code)
	if len(declared) == 0 {
		return nil
	}

	var unread []string
	for _, name := range declared {
		// Check if params.get('name' or "name") appears in code
		pattern := fmt.Sprintf("params\\.get\\s*\\(\\s*['\"`]%s['\"`]", regexp.QuoteMeta(name))
		matched, _ := regexp.MatchString(pattern, code)
		if !matched {
			unread = append(unread, name)
		}
	}
	return unread
}

// StrategyConfigToMap converts StrategyConfig to a flat map for JSON serialization.
func StrategyConfigToMap(cfg StrategyConfig) map[string]any {
	m := make(map[string]any)
	if cfg.StopLossPct != 0 {
		m["stopLossPct"] = cfg.StopLossPct
	}
	if cfg.TakeProfitPct != 0 {
		m["takeProfitPct"] = cfg.TakeProfitPct
	}
	if cfg.EntryPct != 0 {
		m["entryPct"] = cfg.EntryPct
	}
	if cfg.TrailingEnabled {
		m["trailingEnabled"] = true
	}
	if cfg.TrailingStopPct != 0 {
		m["trailingStopPct"] = cfg.TrailingStopPct
	}
	if cfg.TrailingActivationPct != 0 {
		m["trailingActivationPct"] = cfg.TrailingActivationPct
	}
	if cfg.TradeDirection != "" {
		m["tradeDirection"] = cfg.TradeDirection
	}
	return m
}
