package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/analysis"
)

// SmartMoneyAnalysis GET /api/analysis/smart-money?symbol=BTCUSDT&timeframe=1h
// 主力行为全解：建仓/吸筹/洗盘/拉升/出货 五段状态机 + 证据链 + 关键位 + 信号。
// 数据取 Binance 公共 K 线（已闭合），纯计算无状态。
func SmartMoneyAnalysis(c *gin.Context) {
	symbol := strings.ToUpper(strings.TrimSpace(c.DefaultQuery("symbol", "BTCUSDT")))
	tf := strings.ToLower(strings.TrimSpace(c.DefaultQuery("timeframe", "1h")))
	switch tf {
	case "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"detail": "timeframe 不支持: " + tf})
		return
	}
	bars, err := analysis.FetchBinanceKlines(symbol, tf, 300)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": "K 线获取失败: " + err.Error()})
		return
	}
	res := analysis.Analyze(bars, symbol, tf)
	c.JSON(http.StatusOK, res)
}

// SmartMoneyParamDefs smart_money 策略的创建表单参数（symbol/timeframe +
// position_size USDT 本金，阈值固定）。供 GetStrategyParamDefs 的 case 调用。
func SmartMoneyParamDefs() []map[string]any {
	return []map[string]any{
		{"name": "symbol", "type": "string", "required": true, "default": "BTCUSDT", "description": "交易对"},
		{"name": "timeframe", "type": "interval", "default": "1h", "description": "分析周期（建议 ≥1h，结构更清晰）"},
		{"name": "position_size", "type": "float", "default": 100, "min": 10, "max": 100000, "description": "单次下单 USDT 本金"},
	}
}
