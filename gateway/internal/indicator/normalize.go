package indicator

import "strings"

// Output normalization for the raw sandbox output map, aligned with the
// QuantDinger indicator contract (INDICATOR_DEV_GUIDE.md §7-§9).
// The executor already normalizes its own output; this is the gateway-side
// second pass so older sandboxes or hand-written outputs stay compatible
// before the payload is forwarded to the frontend.

// plotTypeAliases maps accepted plot type aliases to their canonical type.
var plotTypeAliases = map[string]string{
	"line":      "line",
	"bar":       "bar",
	"histogram": "bar",
	"column":    "bar",
	"circle":    "circle",
	"dot":       "circle",
	"point":     "circle",
	"scatter":   "circle",
	"lamp":      "lamp",
}

// NormalizePlotType maps a plot type alias to its canonical public type.
// Unknown types are returned lower-cased but unchanged.
func NormalizePlotType(t string) string {
	if canonical, ok := plotTypeAliases[lowerTrim(t)]; ok {
		return canonical
	}
	return lowerTrim(t)
}

// NormalizeOutputMap normalizes plots/signals of a raw sandbox output map in
// place and returns it. Layers are passed through untouched.
func NormalizeOutputMap(output map[string]any) map[string]any {
	if output == nil {
		return output
	}
	if plots, ok := output["plots"].([]any); ok {
		for _, p := range plots {
			if pm, ok := p.(map[string]any); ok {
				normalizePlotMap(pm)
			}
		}
	}
	if signals, ok := output["signals"].([]any); ok {
		for _, s := range signals {
			if sm, ok := s.(map[string]any); ok {
				normalizeSignalMap(sm)
			}
		}
	}
	return output
}

// normalizePlotMap applies §7 style aliases and per-point object aliases.
func normalizePlotMap(plot map[string]any) {
	if t, ok := plot["type"].(string); ok && t != "" {
		plot["type"] = NormalizePlotType(t)
	}
	// lineStyle alias: "style"
	if _, has := plot["lineStyle"]; !has {
		if v, ok := plot["style"]; ok {
			plot["lineStyle"] = v
			delete(plot, "style")
		}
	}
	// size alias: "radius"
	if _, has := plot["size"]; !has {
		if v, ok := plot["radius"]; ok {
			plot["size"] = v
			delete(plot, "radius")
		}
	}
	if data, ok := plot["data"].([]any); ok {
		for i, pt := range data {
			if pm, ok := pt.(map[string]any); ok {
				data[i] = normalizePointObject(pm)
			}
		}
	}
}

// normalizeSignalMap applies §8 aliases: "mode" -> "renderMode".
func normalizeSignalMap(signal map[string]any) {
	if _, has := signal["renderMode"]; !has {
		if v, ok := signal["mode"]; ok {
			signal["renderMode"] = v
			delete(signal, "mode")
		}
	}
}

// normalizePointObject canonicalizes a per-point plot object:
// value aliases: value/y/data; color aliases: color/fillColor/backgroundColor;
// size aliases: size/radius/r.
func normalizePointObject(pt map[string]any) map[string]any {
	if _, has := pt["value"]; !has {
		for _, alias := range []string{"y", "data"} {
			if v, ok := pt[alias]; ok {
				pt["value"] = v
				delete(pt, alias)
				break
			}
		}
	}
	if _, has := pt["color"]; !has {
		for _, alias := range []string{"fillColor", "backgroundColor"} {
			if v, ok := pt[alias]; ok {
				pt["color"] = v
				delete(pt, alias)
				break
			}
		}
	}
	if _, has := pt["size"]; !has {
		for _, alias := range []string{"radius", "r"} {
			if v, ok := pt[alias]; ok {
				pt["size"] = v
				delete(pt, alias)
				break
			}
		}
	}
	return pt
}

func lowerTrim(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
