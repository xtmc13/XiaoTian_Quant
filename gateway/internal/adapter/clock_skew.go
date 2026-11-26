package adapter

import (
	"encoding/json"
	"math"
)

// DefaultClockSkewThresholdMs 本地时钟与交易所偏移告警阈值（毫秒）。
// Binance 签名 recvWindow 默认 5000ms，偏移过大会导致签名被拒；1000ms 为提前预警线。
const DefaultClockSkewThresholdMs = 1000

// ServerTimeResponse 交易所服务器时间响应（Binance /api/v3/time、/fapi/v1/time 同构）。
type ServerTimeResponse struct {
	ServerTime int64 `json:"serverTime"`
}

// ParseServerTimeMs 从服务器时间响应体解析 epoch 毫秒。
func ParseServerTimeMs(body []byte) (int64, error) {
	var resp ServerTimeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, err
	}
	return resp.ServerTime, nil
}

// CheckClockSkew 计算本地时钟相对交易所服务器时间的偏移（ms，正=本地偏慢），
// 偏移绝对值超过 thresholdMs 判为超限。thresholdMs<=0 时取默认阈值。
func CheckClockSkew(serverTimeMs, localNowMs int64, thresholdMs int64) (offsetMs int64, exceeded bool) {
	if thresholdMs <= 0 {
		thresholdMs = DefaultClockSkewThresholdMs
	}
	offsetMs = serverTimeMs - localNowMs
	exceeded = math.Abs(float64(offsetMs)) > float64(thresholdMs)
	return offsetMs, exceeded
}
