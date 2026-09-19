package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestContractLeverageHonest：GET /contract/leverage 不得再返回写死的 20（P1-2）。
func TestContractLeverageHonest(t *testing.T) {
	r := setupRouter()
	r.GET("/contract/leverage", ContractLeverageGet)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/contract/leverage", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "leverage status")

	var body map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &body) == nil, "leverage JSON")
	if v, exists := body["leverage"]; exists && v != nil {
		t.Fatalf("leverage must be null/omitted (no real data source), got %v", v)
	}
}

// TestContractMarginHonest：数值字段不得返回写死的 20/1.0/0/10。
func TestContractMarginHonest(t *testing.T) {
	r := setupRouter()
	r.GET("/contract/margin", ContractMarginInfo)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/contract/margin", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "margin status")

	var body struct {
		Data struct {
			Leverage         *float64 `json:"leverage"`
			AvailableMargin  *float64 `json:"available_margin"`
			MarginRatio      *float64 `json:"margin_ratio"`
			LiquidationPrice *float64 `json:"liquidation_price"`
			MaxPositions     *int     `json:"max_positions"`
			MarginMode       string   `json:"margin_mode"`
		} `json:"data"`
	}
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &body) == nil, "margin JSON")
	assertTrue(t, body.Data.Leverage == nil, "leverage must be null")
	assertTrue(t, body.Data.MarginRatio == nil, "margin_ratio must be null (was fake 1.0)")
	assertTrue(t, body.Data.AvailableMargin == nil, "available_margin must be null")
	assertTrue(t, body.Data.LiquidationPrice == nil, "liquidation_price must be null")
	assertTrue(t, body.Data.MaxPositions == nil, "max_positions must be null")
	assertTrue(t, body.Data.MarginMode != "", "margin_mode config default kept")
}
