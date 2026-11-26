package adapter

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/reconcile"
)

// ClientOrderQuery 按本地恒定的 clientOrderId 反查到的交易所订单结果。
type ClientOrderQuery struct {
	Found           bool // 交易所是否已存在该订单（false = 确实没挂上）
	ExchangeOrderID string
	Info            reconcile.OrderStatusInfo
}

// ClientOrderQuerier 是适配器可选能力：下单结果不确定（超时/5xx/429）时，
// 用同一 clientOrderId 反查订单是否已在交易所挂上，避免虚拒与重复下单。
type ClientOrderQuerier interface {
	QueryOrderByClientOID(symbol, clientOID string) (ClientOrderQuery, error)
}

// QueryOrderByClientOID 按客户端订单号查询 Binance 订单：U 本位合约优先，现货兜底，
// 口径与 QueryOrderStatus 一致。订单不存在时返回 Found=false、nil error。
func (b *BinanceAdapter) QueryOrderByClientOID(symbol, clientOID string) (ClientOrderQuery, error) {
	if strings.TrimSpace(clientOID) == "" {
		return ClientOrderQuery{}, fmt.Errorf("empty client order id")
	}
	if q, err := b.futuresOrderByClientOID(symbol, clientOID); err == nil {
		return q, nil
	} else if !isBinanceOrderNotFound(err) {
		return ClientOrderQuery{}, err
	}
	return b.spotOrderByClientOID(symbol, clientOID)
}

func (b *BinanceAdapter) futuresOrderByClientOID(symbol, cid string) (ClientOrderQuery, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("origClientOrderId", cid)
	raw, err := b.futuresRawRequest("GET", "/fapi/v1/order", params)
	if err != nil {
		return ClientOrderQuery{}, err
	}
	var r struct {
		OrderID     json.Number `json:"orderId"`
		Status      string      `json:"status"`
		ExecutedQty string      `json:"executedQty"`
		AvgPrice    string      `json:"avgPrice"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return ClientOrderQuery{}, fmt.Errorf("parse futures order by client oid: %w", err)
	}
	if r.Status == "" {
		return ClientOrderQuery{}, fmt.Errorf("empty futures order status")
	}
	return ClientOrderQuery{
		Found:           true,
		ExchangeOrderID: r.OrderID.String(),
		Info: reconcile.OrderStatusInfo{
			Status:    r.Status,
			FilledQty: parseQty(r.ExecutedQty),
			AvgPrice:  parseQty(r.AvgPrice),
		},
	}, nil
}

func (b *BinanceAdapter) spotOrderByClientOID(symbol, cid string) (ClientOrderQuery, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("origClientOrderId", cid)
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	params.Set("recvWindow", "5000")
	params.Set("signature", b.sign(params))

	u, _ := url.Parse(b.baseURL() + "/order") // baseURL 已含 /api/v3 前缀
	u.RawQuery = params.Encode()
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return ClientOrderQuery{}, err
	}
	req.Header.Set("X-MBX-APIKEY", b.apiKey)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return ClientOrderQuery{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		if isBinanceOrderNotFoundRaw(raw) {
			return ClientOrderQuery{Found: false}, nil
		}
		return ClientOrderQuery{}, fmt.Errorf("binance spot order by client oid: status %d: %s", resp.StatusCode, truncateStr(string(raw), 200))
	}
	var r struct {
		OrderID             json.Number `json:"orderId"`
		Status              string      `json:"status"`
		ExecutedQty         string      `json:"executedQty"`
		CummulativeQuoteQty  string      `json:"cummulativeQuoteQty"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return ClientOrderQuery{}, fmt.Errorf("parse spot order by client oid: %w", err)
	}
	info := reconcile.OrderStatusInfo{Status: r.Status, FilledQty: parseQty(r.ExecutedQty)}
	if info.FilledQty > 0 {
		info.AvgPrice = parseQty(r.CummulativeQuoteQty) / info.FilledQty
	}
	return ClientOrderQuery{
		Found:           true,
		ExchangeOrderID: r.OrderID.String(),
		Info:            info,
	}, nil
}

// isBinanceOrderNotFound 识别"订单不存在"：fapi/spot 对无此订单返回 code -2013，
// 部分场景为 -2011。这类不算查询失败，按 Found=false 处理。
func isBinanceOrderNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "-2013") || strings.Contains(msg, "-2011")
}

func isBinanceOrderNotFoundRaw(raw []byte) bool {
	var r struct {
		Code int64 `json:"code"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return false
	}
	return r.Code == -2013 || r.Code == -2011
}
