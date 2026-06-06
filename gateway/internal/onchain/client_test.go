package onchain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewClient(t *testing.T) {
	c := NewClient("test-api-key")
	assert.NotNil(t, c)
	assert.Equal(t, "test-api-key", c.apiKey)
	assert.NotNil(t, c.httpClient)
}

func TestClient_SetBaseURL(t *testing.T) {
	c := NewClient("")
	c.SetBaseURL("https://api.glassnode.com")
	assert.Equal(t, "https://api.glassnode.com", c.baseURL)
}

func TestClient_GetETHMetrics(t *testing.T) {
	c := NewClient("")
	metrics, err := c.GetETHMetrics()
	assert.NoError(t, err)
	assert.NotNil(t, metrics)
	assert.Greater(t, metrics.Timestamp, int64(0))
	assert.Greater(t, metrics.GasPriceGwei, 0.0)
	assert.Greater(t, metrics.ActiveAddresses, int64(0))
	assert.Greater(t, metrics.TxCount24h, int64(0))
}

func TestClient_GetBTCMetrics(t *testing.T) {
	c := NewClient("")
	metrics, err := c.GetBTCMetrics()
	assert.NoError(t, err)
	assert.NotNil(t, metrics)
	assert.Greater(t, metrics.Timestamp, int64(0))
	assert.Greater(t, metrics.HashRateEH, 0.0)
	assert.Greater(t, metrics.ActiveAddresses, int64(0))
	assert.Greater(t, metrics.TxCount24h, int64(0))
}

func TestClient_GetExchangeFlow(t *testing.T) {
	c := NewClient("")
	flow, err := c.GetExchangeFlow("binance")
	assert.NoError(t, err)
	assert.NotNil(t, flow)
	assert.Equal(t, "binance", flow.Exchange)
	assert.Greater(t, flow.InflowBTC, 0.0)
	assert.Greater(t, flow.OutflowBTC, 0.0)
	assert.Greater(t, flow.Timestamp, int64(0))
}

func TestClient_GetWhaleAlerts(t *testing.T) {
	c := NewClient("")
	alerts, err := c.GetWhaleAlerts(1000000)
	assert.NoError(t, err)
	assert.Len(t, alerts, 2)
	assert.Greater(t, alerts[0].USDValue, 1000000.0)
	assert.NotEmpty(t, alerts[0].TxID)
	assert.NotEmpty(t, alerts[0].Symbol)
}

func TestMockClient_GetETHMetrics(t *testing.T) {
	mc := NewMockClient()
	metrics, err := mc.GetETHMetrics()
	assert.NoError(t, err)
	assert.Equal(t, 25.0, metrics.GasPriceGwei)
	assert.Equal(t, int64(600000), metrics.ActiveAddresses)
	assert.Equal(t, -3000.0, metrics.NetExchangeFlow)
	assert.Equal(t, 1.8, metrics.MVRV)
	assert.Equal(t, 0.15, metrics.NUPL)
}

func TestMockClient_GetBTCMetrics(t *testing.T) {
	mc := NewMockClient()
	metrics, err := mc.GetBTCMetrics()
	assert.NoError(t, err)
	assert.Equal(t, 550.0, metrics.HashRateEH)
	assert.Equal(t, int64(900000), metrics.ActiveAddresses)
	assert.Equal(t, -2000.0, metrics.NetExchangeFlow)
	assert.Equal(t, 0.92, metrics.SOPR)
	assert.Equal(t, 1.5, metrics.MVRV)
	assert.Equal(t, -0.1, metrics.NUPL)
	assert.Equal(t, 0.8, metrics.PuellMultiple)
}

func TestMockClient_GetExchangeFlow(t *testing.T) {
	mc := NewMockClient()
	flow, err := mc.GetExchangeFlow("coinbase")
	assert.NoError(t, err)
	assert.Equal(t, "coinbase", flow.Exchange)
	assert.Equal(t, 100.0, flow.InflowBTC)
	assert.Equal(t, 200.0, flow.OutflowBTC)
	assert.Equal(t, 100.0, flow.NetFlowBTC)
}

func TestMockClient_GetWhaleAlerts(t *testing.T) {
	mc := NewMockClient()
	alerts, err := mc.GetWhaleAlerts(1000000)
	assert.NoError(t, err)
	assert.Len(t, alerts, 1)
	assert.Equal(t, "0xabc", alerts[0].TxID)
	assert.Equal(t, "BTC", alerts[0].Symbol)
	assert.Equal(t, 100.0, alerts[0].Amount)
	assert.Equal(t, 5000000.0, alerts[0].USDValue)
}

func TestAggregator_GetBTCOnChainSignal_Bullish(t *testing.T) {
	mc := NewMockClient()
	agg := NewAggregator(mc)

	// Test with mock data that should produce bullish signal
	// Mock has: net outflow -2000, NUPL -0.1, MVRV 1.5, SOPR 0.92
	signal, err := agg.GetBTCOnChainSignal()
	assert.NoError(t, err)
	assert.NotNil(t, signal)
	assert.Equal(t, "BTC", signal.Symbol)
	// With mock data: outflow (-2000 < -500) = bullish +15
	// NUPL (-0.1 < 0) = bullish +10
	// MVRV (1.5 not < 1.0) = no trigger
	// SOPR (0.92 < 0.95) = bullish +5
	// Total: 50 + 15 + 10 + 5 = 80
	assert.Equal(t, "bullish", signal.Direction)
	assert.Equal(t, 80.0, signal.Strength)
	assert.Contains(t, signal.Indicators, "exchange_outflow")
	assert.Contains(t, signal.Indicators, "nupl_negative")
	assert.Contains(t, signal.Indicators, "sopr_loss")
}

func TestAggregator_GetETHOnChainSignal_Bullish(t *testing.T) {
	mc := NewMockClient()
	agg := NewAggregator(mc)

	// Mock ETH: net outflow -3000, NUPL 0.15, MVRV 1.8
	signal, err := agg.GetETHOnChainSignal()
	assert.NoError(t, err)
	assert.NotNil(t, signal)
	assert.Equal(t, "ETH", signal.Symbol)
	// outflow (-3000 < -1000) = bullish +15
	// gas (25 < 100) = no trigger
	// NUPL (0.15 not > 0.5, not < 0) = no trigger
	// MVRV (1.8 not > 3.5, not < 1.0) = no trigger
	// Total: 50 + 15 = 65
	assert.Equal(t, "bullish", signal.Direction)
	assert.Equal(t, 65.0, signal.Strength)
	assert.Contains(t, signal.Indicators, "exchange_outflow")
}

func TestAggregator_GetBTCOnChainSignal_Bearish(t *testing.T) {
	mc := NewMockClient()
	mc.BTCMetricsData.NetExchangeFlow = 2000  // inflow
	mc.BTCMetricsData.NUPL = 0.6             // overheated
	mc.BTCMetricsData.MVRV = 4.0             // overvalued
	mc.BTCMetricsData.SOPR = 1.1             // profit taking

	agg := NewAggregator(mc)
	signal, err := agg.GetBTCOnChainSignal()
	assert.NoError(t, err)
	// inflow (+2000 > 500) = bearish +15
	// NUPL (0.6 > 0.5) = bearish +10
	// MVRV (4.0 > 3.5) = +10
	// SOPR (1.1 > 1.05) = +5
	// Total: 50 + 15 + 10 + 10 + 5 = 90, direction = bearish
	assert.Equal(t, "bearish", signal.Direction)
	assert.Equal(t, 90.0, signal.Strength)
	assert.Contains(t, signal.Indicators, "exchange_inflow")
	assert.Contains(t, signal.Indicators, "nupl_high")
	assert.Contains(t, signal.Indicators, "mvrv_overvalued")
	assert.Contains(t, signal.Indicators, "sopr_profit")
}

func TestAggregator_GetETHOnChainSignal_Bearish(t *testing.T) {
	mc := NewMockClient()
	mc.ETHMetricsData.NetExchangeFlow = 2000
	mc.ETHMetricsData.NUPL = 0.6
	mc.ETHMetricsData.MVRV = 4.0
	mc.ETHMetricsData.GasPriceGwei = 150

	agg := NewAggregator(mc)
	signal, err := agg.GetETHOnChainSignal()
	assert.NoError(t, err)
	// inflow (+2000 > 1000) = bearish +15
	// gas (150 > 100) = +5
	// NUPL (0.6 > 0.5) = bearish +10
	// MVRV (4.0 > 3.5) = +10
	// Total: 50 + 15 + 5 + 10 + 10 = 90
	assert.Equal(t, "bearish", signal.Direction)
	assert.Equal(t, 90.0, signal.Strength)
	assert.Contains(t, signal.Indicators, "exchange_inflow")
	assert.Contains(t, signal.Indicators, "gas_spike")
	assert.Contains(t, signal.Indicators, "nupl_high")
	assert.Contains(t, signal.Indicators, "mvrv_overvalued")
}

func TestAggregator_StrengthCap(t *testing.T) {
	mc := NewMockClient()
	mc.BTCMetricsData.NetExchangeFlow = -2000
	mc.BTCMetricsData.NUPL = -0.5
	mc.BTCMetricsData.MVRV = 0.5
	mc.BTCMetricsData.SOPR = 0.8

	agg := NewAggregator(mc)
	signal, err := agg.GetBTCOnChainSignal()
	assert.NoError(t, err)
	// All bullish triggers: 50 + 15 + 10 + 10 + 5 = 90 (not capped)
	assert.Equal(t, "bullish", signal.Direction)
	assert.LessOrEqual(t, signal.Strength, 100.0)
}

func TestAggregator_NeutralSignal(t *testing.T) {
	mc := NewMockClient()
	mc.BTCMetricsData.NetExchangeFlow = 0
	mc.BTCMetricsData.NUPL = 0.3
	mc.BTCMetricsData.MVRV = 2.0
	mc.BTCMetricsData.SOPR = 1.0

	agg := NewAggregator(mc)
	signal, err := agg.GetBTCOnChainSignal()
	assert.NoError(t, err)
	assert.Equal(t, "neutral", signal.Direction)
	assert.Equal(t, 50.0, signal.Strength)
	assert.Empty(t, signal.Indicators)
}
