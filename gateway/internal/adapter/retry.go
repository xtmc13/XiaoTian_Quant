package adapter

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strconv"
)

// 适配器把 HTTP 状态码拼进错误串，形如：
//
//	binance POST /order: HTTP 400 code=-2010 insufficient balance
var httpStatusRe = regexp.MustCompile(`HTTP (\d{3})`)

// IsUncertainSubmitError 判定一次下单返回的错误是否"结果不确定"。
//
// 不确定的情形：传输层失败（连接拒绝/重置/超时/TLS 中断，拿不到交易所响应）、
// HTTP 5xx、429 限频。这类情况下订单可能已在交易所挂上，直接本地置 REJECTED
// 会造成虚拒/虚持仓，重发前必须先按 clientOrderId 查询是否已存在。
//
// 确定性 4xx 业务拒绝（余额不足、参数非法、风控拒绝等）不在此列：交易所已明确
// 拒收，查单无意义，直接失败即可。无法归类为传输层/5xx/429 的本地校验错误
// （参数归一化失败等，请求根本没发出）也按确定失败处理。
func IsUncertainSubmitError(err error) bool {
	if err == nil {
		return false
	}

	// 传输层：http.Client.Do 返回 *url.Error，包装 net.Error / 系统调用错误，
	// 此时没有任何交易所响应可供判断业务结果。
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return true
	}
	var nerr net.Error
	if errors.As(err, &nerr) {
		return true
	}

	// 业务/HTTP 层：从错误串里解析状态码。
	if m := httpStatusRe.FindStringSubmatch(err.Error()); len(m) == 2 {
		code, _ := strconv.Atoi(m[1])
		if code == 429 || code >= 500 {
			return true
		}
		return false
	}

	return false
}
