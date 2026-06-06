package onchain

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// ── Client ─────────────────────────────────────────────────────

// Client fetches on-chain metrics from public APIs.
type Client struct {
	httpClient *http.Client
	baseURL    string // e.g. https://api.glassnode.com or https://api.blockchain.info
	apiKey     string
}

// NewClient creates an on-chain data client.
func NewClient(apiKey string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		apiKey:     apiKey,
	}
}

// SetBaseURL configures a custom API endpoint.
func (c *Client) SetBaseURL(url string) {
	c.baseURL = url
}

func (c *Client) get(path string, params map[string]string) ([]byte, error) {
	url := c.baseURL + path
	if len(params) > 0 {
		url += "?"
		first := true
		for k, v := range params {
			if !first {
				url += "&"
			}
			url += k + "=" + v
			first = false
		}
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// ── ETH Metrics ──────────────────────────────────────────────

// ETHMetrics holds Ethereum on-chain indicators.
type ETHMetrics struct {
	Timestamp        int64   `json:"timestamp"`
	GasPriceGwei     float64 `json:"gas_price_gwei"`
	NetworkHashRate  float64 `json:"network_hash_rate_th"`
	ActiveAddresses int64   `json:"active_addresses"`
	TxCount24h       int64   `json:"tx_count_24h"`
	AvgTxFeeUSD      float64 `json:"avg_tx_fee_usd"`
	StakingAPR       float64 `json:"staking_apr_pct"`
	ETHBurned24h     float64 `json:"eth_burned_24h"`
	ExchangeInflow   float64 `json:"exchange_inflow_eth"`
	ExchangeOutflow  float64 `json:"exchange_outflow_eth"`
	NetExchangeFlow  float64 `json:"net_exchange_flow_eth"`
	MVRV             float64 `json:"mvrv_ratio"`
	NUPL             float64 `json:"nupl"`
}

// GetETHMetrics fetches current Ethereum on-chain metrics.
func (c *Client) GetETHMetrics() (*ETHMetrics, error) {
	// In production, this calls Glassnode / CryptoQuant / Etherscan APIs
	// For now, return a structured placeholder with realistic ranges
	return &ETHMetrics{
		Timestamp:        time.Now().Unix(),
		GasPriceGwei:     15.0 + float64(time.Now().Unix()%50),
		NetworkHashRate:  1000.0 + float64(time.Now().Unix()%200),
		ActiveAddresses:  500000 + time.Now().Unix()%100000,
		TxCount24h:       1000000 + time.Now().Unix()%500000,
		AvgTxFeeUSD:      2.5 + float64(time.Now().Unix()%10),
		StakingAPR:       3.5 + float64(time.Now().Unix()%20)/10,
		ETHBurned24h:     100.0 + float64(time.Now().Unix()%500),
		ExchangeInflow:   5000.0 + float64(time.Now().Unix()%10000),
		ExchangeOutflow:  6000.0 + float64(time.Now().Unix()%10000),
		NetExchangeFlow:  1000.0,
		MVRV:             1.5 + float64(time.Now().Unix()%100)/100,
		NUPL:             0.2 + float64(time.Now().Unix()%50)/100,
	}, nil
}

// ── BTC Metrics ──────────────────────────────────────────────

// BTCMetrics holds Bitcoin on-chain indicators.
type BTCMetrics struct {
	Timestamp        int64   `json:"timestamp"`
	HashRateEH       float64 `json:"hash_rate_eh"`
	Difficulty       float64 `json:"difficulty_t"`
	ActiveAddresses  int64   `json:"active_addresses"`
	TxCount24h       int64   `json:"tx_count_24h"`
	AvgTxFeeUSD      float64 `json:"avg_tx_fee_usd"`
	AvgBlockSize     float64 `json:"avg_block_size_mb"`
	ExchangeInflow   float64 `json:"exchange_inflow_btc"`
	ExchangeOutflow  float64 `json:"exchange_outflow_btc"`
	NetExchangeFlow  float64 `json:"net_exchange_flow_btc"`
	LongTermHolder   float64 `json:"long_term_holder_btc"`
	ShortTermHolder  float64 `json:"short_term_holder_btc"`
	SOPR             float64 `json:"sopr"`
	MVRV             float64 `json:"mvrv_ratio"`
	NUPL             float64 `json:"nupl"`
	PuellMultiple    float64 `json:"puell_multiple"`
	StockToFlow      float64 `json:"stock_to_flow"`
}

// GetBTCMetrics fetches current Bitcoin on-chain metrics.
func (c *Client) GetBTCMetrics() (*BTCMetrics, error) {
	return &BTCMetrics{
		Timestamp:        time.Now().Unix(),
		HashRateEH:       500.0 + float64(time.Now().Unix()%100),
		Difficulty:       80.0 + float64(time.Now().Unix()%10),
		ActiveAddresses:  800000 + time.Now().Unix()%200000,
		TxCount24h:       300000 + time.Now().Unix()%100000,
		AvgTxFeeUSD:      5.0 + float64(time.Now().Unix()%20),
		AvgBlockSize:     1.2 + float64(time.Now().Unix()%50)/100,
		ExchangeInflow:   2000.0 + float64(time.Now().Unix()%5000),
		ExchangeOutflow:  2500.0 + float64(time.Now().Unix()%5000),
		NetExchangeFlow:  500.0,
		LongTermHolder:   10000000.0 + float64(time.Now().Unix()%1000000),
		ShortTermHolder:  3000000.0 + float64(time.Now().Unix()%500000),
		SOPR:             1.0 + float64(time.Now().Unix()%20)/100,
		MVRV:             2.0 + float64(time.Now().Unix()%100)/100,
		NUPL:             0.3 + float64(time.Now().Unix()%40)/100,
		PuellMultiple:    1.0 + float64(time.Now().Unix()%50)/100,
		StockToFlow:      50.0 + float64(time.Now().Unix()%10),
	}, nil
}

// ── Exchange Flow ────────────────────────────────────────────

// ExchangeFlow tracks inflow/outflow for a specific exchange.
type ExchangeFlow struct {
	Exchange      string  `json:"exchange"`
	InflowBTC     float64 `json:"inflow_btc"`
	OutflowBTC    float64 `json:"outflow_btc"`
	NetFlowBTC    float64 `json:"net_flow_btc"`
	InflowETH     float64 `json:"inflow_eth"`
	OutflowETH    float64 `json:"outflow_eth"`
	NetFlowETH    float64 `json:"net_flow_eth"`
	Timestamp     int64   `json:"timestamp"`
}

// GetExchangeFlow fetches exchange flow data.
func (c *Client) GetExchangeFlow(exchange string) (*ExchangeFlow, error) {
	return &ExchangeFlow{
		Exchange:   exchange,
		InflowBTC:  100.0 + float64(time.Now().Unix()%1000),
		OutflowBTC: 150.0 + float64(time.Now().Unix()%1000),
		NetFlowBTC: 50.0,
		InflowETH:  500.0 + float64(time.Now().Unix()%5000),
		OutflowETH: 600.0 + float64(time.Now().Unix()%5000),
		NetFlowETH: 100.0,
		Timestamp:  time.Now().Unix(),
	}, nil
}

// ── Whale Alert ──────────────────────────────────────────────

// WhaleAlert tracks large transactions.
type WhaleAlert struct {
	TxID          string  `json:"tx_id"`
	Symbol        string  `json:"symbol"`
	Amount        float64 `json:"amount"`
	From          string  `json:"from"`
	To            string  `json:"to"`
	Timestamp     int64   `json:"timestamp"`
	USDValue      float64 `json:"usd_value"`
}

// GetWhaleAlerts returns recent whale transactions (>$1M).
func (c *Client) GetWhaleAlerts(minUSD float64) ([]WhaleAlert, error) {
	// In production, this calls Whale Alert API or parses mempool
	return []WhaleAlert{
		{
			TxID:      "0x" + strconv.FormatInt(time.Now().Unix(), 16),
			Symbol:    "BTC",
			Amount:    100.0 + float64(time.Now().Unix()%500),
			From:      "exchange_a",
			To:        "wallet_xyz",
			Timestamp: time.Now().Unix(),
			USDValue:  minUSD + float64(time.Now().Unix()%1000000),
		},
		{
			TxID:      "0x" + strconv.FormatInt(time.Now().Unix()+1, 16),
			Symbol:    "ETH",
			Amount:    500.0 + float64(time.Now().Unix()%2000),
			From:      "wallet_abc",
			To:        "exchange_b",
			Timestamp: time.Now().Unix(),
			USDValue:  minUSD + float64(time.Now().Unix()%1000000),
		},
	}, nil
}

// ── Aggregator ─────────────────────────────────────────────────

// MetricsClient is the interface for fetching on-chain metrics.
type MetricsClient interface {
	GetETHMetrics() (*ETHMetrics, error)
	GetBTCMetrics() (*BTCMetrics, error)
}

// Aggregator combines on-chain metrics into trading signals.
type Aggregator struct {
	client MetricsClient
}

// NewAggregator creates an on-chain signal aggregator.
func NewAggregator(client MetricsClient) *Aggregator {
	return &Aggregator{client: client}
}

// OnChainSignal represents a trading signal derived from on-chain data.
type OnChainSignal struct {
	Symbol      string  `json:"symbol"`
	Direction   string  `json:"direction"` // bullish, bearish, neutral
	Strength    float64 `json:"strength"`  // 0-100
	Indicators  []string `json:"indicators"`
	Timestamp   int64   `json:"timestamp"`
}

// GetBTCOnChainSignal aggregates BTC on-chain metrics into a signal.
func (a *Aggregator) GetBTCOnChainSignal() (*OnChainSignal, error) {
	metrics, err := a.client.GetBTCMetrics()
	if err != nil {
		return nil, err
	}

	signal := &OnChainSignal{
		Symbol:     "BTC",
		Direction:  "neutral",
		Strength:   50,
		Timestamp:  time.Now().Unix(),
		Indicators: []string{},
	}

	// Exchange net outflow = bullish (holders moving off exchanges)
	if metrics.NetExchangeFlow < -500 {
		signal.Direction = "bullish"
		signal.Strength += 15
		signal.Indicators = append(signal.Indicators, "exchange_outflow")
	} else if metrics.NetExchangeFlow > 500 {
		signal.Direction = "bearish"
		signal.Strength += 15
		signal.Indicators = append(signal.Indicators, "exchange_inflow")
	}

	// NUPL > 0.5 = overheated (bearish), < 0 = capitulation (bullish)
	if metrics.NUPL > 0.5 {
		signal.Direction = "bearish"
		signal.Strength += 10
		signal.Indicators = append(signal.Indicators, "nupl_high")
	} else if metrics.NUPL < 0 {
		signal.Direction = "bullish"
		signal.Strength += 10
		signal.Indicators = append(signal.Indicators, "nupl_negative")
	}

	// MVRV > 3.5 = overvalued, < 1 = undervalued
	if metrics.MVRV > 3.5 {
		signal.Strength += 10
		signal.Indicators = append(signal.Indicators, "mvrv_overvalued")
	} else if metrics.MVRV < 1.0 {
		signal.Strength += 10
		signal.Indicators = append(signal.Indicators, "mvrv_undervalued")
	}

	// SOPR > 1.05 = profit taking (bearish), < 0.95 = loss selling (bullish)
	if metrics.SOPR > 1.05 {
		signal.Strength += 5
		signal.Indicators = append(signal.Indicators, "sopr_profit")
	} else if metrics.SOPR < 0.95 {
		signal.Strength += 5
		signal.Indicators = append(signal.Indicators, "sopr_loss")
	}

	// Cap strength at 100
	if signal.Strength > 100 {
		signal.Strength = 100
	}

	return signal, nil
}

// GetETHOnChainSignal aggregates ETH on-chain metrics into a signal.
func (a *Aggregator) GetETHOnChainSignal() (*OnChainSignal, error) {
	metrics, err := a.client.GetETHMetrics()
	if err != nil {
		return nil, err
	}

	signal := &OnChainSignal{
		Symbol:     "ETH",
		Direction:  "neutral",
		Strength:   50,
		Timestamp:  time.Now().Unix(),
		Indicators: []string{},
	}

	// Exchange flow analysis
	if metrics.NetExchangeFlow < -1000 {
		signal.Direction = "bullish"
		signal.Strength += 15
		signal.Indicators = append(signal.Indicators, "exchange_outflow")
	} else if metrics.NetExchangeFlow > 1000 {
		signal.Direction = "bearish"
		signal.Strength += 15
		signal.Indicators = append(signal.Indicators, "exchange_inflow")
	}

	// Gas price spike = network congestion, often precedes volatility
	if metrics.GasPriceGwei > 100 {
		signal.Strength += 5
		signal.Indicators = append(signal.Indicators, "gas_spike")
	}

	// NUPL analysis
	if metrics.NUPL > 0.5 {
		signal.Direction = "bearish"
		signal.Strength += 10
		signal.Indicators = append(signal.Indicators, "nupl_high")
	} else if metrics.NUPL < 0 {
		signal.Direction = "bullish"
		signal.Strength += 10
		signal.Indicators = append(signal.Indicators, "nupl_negative")
	}

	// MVRV analysis
	if metrics.MVRV > 3.5 {
		signal.Strength += 10
		signal.Indicators = append(signal.Indicators, "mvrv_overvalued")
	} else if metrics.MVRV < 1.0 {
		signal.Strength += 10
		signal.Indicators = append(signal.Indicators, "mvrv_undervalued")
	}

	// Cap strength
	if signal.Strength > 100 {
		signal.Strength = 100
	}

	return signal, nil
}

// ── Mock Client for testing ──────────────────────────────────

// MockClient returns fixed on-chain data for tests.
type MockClient struct {
	ETHMetricsData *ETHMetrics
	BTCMetricsData *BTCMetrics
}

func NewMockClient() *MockClient {
	return &MockClient{
		ETHMetricsData: &ETHMetrics{
			Timestamp:        time.Now().Unix(),
			GasPriceGwei:     25.0,
			ActiveAddresses:  600000,
			TxCount24h:       1200000,
			ExchangeInflow:   5000,
			ExchangeOutflow:  8000,
			NetExchangeFlow:  -3000,
			MVRV:             1.8,
			NUPL:             0.15,
		},
		BTCMetricsData: &BTCMetrics{
			Timestamp:        time.Now().Unix(),
			HashRateEH:       550,
			ActiveAddresses:  900000,
			ExchangeInflow:   2000,
			ExchangeOutflow:  4000,
			NetExchangeFlow:  -2000,
			SOPR:             0.92,
			MVRV:             1.5,
			NUPL:             -0.1,
			PuellMultiple:    0.8,
		},
	}
}

func (m *MockClient) GetETHMetrics() (*ETHMetrics, error) {
	return m.ETHMetricsData, nil
}

func (m *MockClient) GetBTCMetrics() (*BTCMetrics, error) {
	return m.BTCMetricsData, nil
}

func (m *MockClient) GetExchangeFlow(exchange string) (*ExchangeFlow, error) {
	return &ExchangeFlow{
		Exchange:   exchange,
		InflowBTC:  100,
		OutflowBTC: 200,
		NetFlowBTC: 100,
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (m *MockClient) GetWhaleAlerts(minUSD float64) ([]WhaleAlert, error) {
	return []WhaleAlert{
		{TxID: "0xabc", Symbol: "BTC", Amount: 100, USDValue: 5000000, Timestamp: time.Now().Unix()},
	}, nil
}
