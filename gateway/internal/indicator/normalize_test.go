package indicator

import "testing"

func TestNormalizePlotType(t *testing.T) {
	cases := map[string]string{
		"line":      "line",
		"bar":       "bar",
		"histogram": "bar",
		"column":    "bar",
		"circle":    "circle",
		"dot":       "circle",
		"point":     "circle",
		"scatter":   "circle",
		"lamp":      "lamp",
		"Area":      "area", // unknown types pass through lower-cased
	}
	for in, want := range cases {
		if got := NormalizePlotType(in); got != want {
			t.Errorf("NormalizePlotType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeOutputMap_PlotAliases(t *testing.T) {
	output := map[string]any{
		"plots": []any{
			map[string]any{
				"name":   "Hist",
				"type":   "histogram",
				"style":  "dashed",
				"radius": 4,
				"data": []any{
					nil,
					map[string]any{"y": 1.5, "fillColor": "#fff", "r": 3},
				},
			},
		},
		"signals": []any{
			map[string]any{"type": "buy", "mode": "state", "data": []any{nil, 1.0}},
		},
		"layers": []any{
			map[string]any{"type": "zone", "startIndex": 1, "endIndex": 5},
		},
	}

	NormalizeOutputMap(output)

	plot := output["plots"].([]any)[0].(map[string]any)
	if plot["type"] != "bar" {
		t.Errorf("type = %v, want bar", plot["type"])
	}
	if plot["lineStyle"] != "dashed" {
		t.Errorf("lineStyle = %v, want dashed", plot["lineStyle"])
	}
	if _, ok := plot["style"]; ok {
		t.Error("style alias should be removed")
	}
	if plot["size"] != 4 {
		t.Errorf("size = %v, want 4", plot["size"])
	}
	pt := plot["data"].([]any)[1].(map[string]any)
	if pt["value"] != 1.5 || pt["color"] != "#fff" || pt["size"] != 3 {
		t.Errorf("point object not normalized: %v", pt)
	}

	sig := output["signals"].([]any)[0].(map[string]any)
	if sig["renderMode"] != "state" {
		t.Errorf("renderMode = %v, want state", sig["renderMode"])
	}
	if _, ok := sig["mode"]; ok {
		t.Error("mode alias should be removed")
	}

	// layers pass through untouched
	layer := output["layers"].([]any)[0].(map[string]any)
	if layer["type"] != "zone" {
		t.Errorf("layer type = %v, want zone", layer["type"])
	}
}

func TestValidateOutputJSON_ContractExtensions(t *testing.T) {
	jsonStr := `{
		"name": "T",
		"plots": [{"name": "P", "type": "column", "data": [1, {"value": 2, "color": "#000", "size": 4}], "lineWidth": 2, "lineStyle": "dashed", "opacity": 0.8, "borderColor": "#fff", "borderSize": 1, "baseValue": 0}],
		"signals": [{"type": "buy", "data": [null, {"active": true, "price": 1.2, "text": "B", "color": "#0f0"}], "renderMode": "events", "textData": [null, "B"]}],
		"layers": [{"type": "zone", "startIndex": 0, "endIndex": 1, "top": 3, "bottom": 1, "fillColor": "#22C55E", "opacity": 0.12}],
		"latest_signal": {"type": "buy", "bar_index": 1}
	}`
	output, hints := ValidateOutputJSON(jsonStr)
	if len(hints) > 0 {
		t.Fatalf("unexpected hints: %v", hints)
	}
	if output.Plots[0].Type != "bar" {
		t.Errorf("plot type = %q, want bar", output.Plots[0].Type)
	}
	if output.Plots[0].LineWidth != 2 || output.Plots[0].LineStyle != "dashed" {
		t.Errorf("plot style fields not decoded: %+v", output.Plots[0])
	}
	if output.Signals[0].RenderMode != "events" || len(output.Signals[0].TextData) != 2 {
		t.Errorf("signal metadata not decoded: %+v", output.Signals[0])
	}
	if len(output.Layers) != 1 || output.Layers[0].Type != "zone" || output.Layers[0].Top != 3 {
		t.Errorf("layers not decoded: %+v", output.Layers)
	}
	if output.LatestSignal["type"] != "buy" {
		t.Errorf("latest_signal not decoded: %+v", output.LatestSignal)
	}
}

func TestValidateOutputJSON_UnknownLayerType(t *testing.T) {
	jsonStr := `{"name":"T","plots":[],"signals":[],"layers":[{"type":"rect"}]}`
	_, hints := ValidateOutputJSON(jsonStr)
	found := false
	for _, h := range hints {
		if h.Code == "LAYER_UNKNOWN_TYPE" {
			found = true
		}
	}
	if !found {
		t.Error("expected LAYER_UNKNOWN_TYPE hint")
	}
}
