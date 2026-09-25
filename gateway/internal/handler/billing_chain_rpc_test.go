package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// ── EVM 裸 JSON-RPC 通道（BSC_RPC_URL / ETH_RPC_URL）mock 级测试 ──────────
//
// 无法实机联调：以下用例全部基于 httptest mock 的 JSON-RPC 节点，
// 覆盖 USDT Transfer 日志解析、确认数判定、reverted/未上链/金额不足、
// 以及 verifyChainTx 的 RPC 优先分发。真实节点（BSC/ETH 主网 RPC）
// 联调需在部署环境验证（见交接报告）。

// evmRPCMock 起一棵 mock JSON-RPC 节点：按方法名分发，
// receipt 由 receipts[txhash] 提供，latestBlockHex 为当前块高。
func evmRPCMock(t *testing.T, receipts map[string]string, latestBlockHex string) *httptest.Server {
	t.Helper()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.Method {
		case "eth_getTransactionReceipt":
			txh := ""
			if len(req.Params) > 0 {
				txh, _ = req.Params[0].(string)
			}
			if body, ok := receipts[txh]; ok {
				fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":`+body+`}`)
				return
			}
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":null}`)
		case "eth_blockNumber":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":"%s"}`, latestBlockHex)
		default:
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`)
		}
	}))
	t.Cleanup(mock.Close)
	return mock
}

// evmTransferLogJSON 构造一条 USDT Transfer 日志（topics[2]=to 左填充，data=金额 hex）。
func evmTransferLogJSON(contract, to, valueHex string) string {
	padTo := "0x000000000000000000000000" + strings.ToLower(strings.TrimPrefix(to, "0x"))
	return fmt.Sprintf(`{"address":"%s","topics":["%s","0x00","%s"],"data":"%s"}`,
		contract, erc20TransferTopic, padTo, valueHex)
}

func TestBillingVerifyEvmRPC(t *testing.T) {
	const ourAddr = "0x2222222222222222222222222222222222222222"
	const amount = int64(19900000) // = 0x12fa660
	amountHex := "0x12fa660"
	receipts := map[string]string{
		// 确认数充足：交易块 0x64(100)，最新块 0x78(120) → 21 确认 ≥ 15
		"0xconfirmed": fmt.Sprintf(`{"status":"0x1","blockNumber":"0x64","logs":[%s]}`,
			evmTransferLogJSON(bep20USDTContract, ourAddr, amountHex)),
		// 同交易，但用例里把最低确认数抬高 → confirming
		"0xreverted": `{"status":"0x0","blockNumber":"0x64","logs":[]}`,
		// 合约地址不符（别的 token 合约）
		"0xwrongcontract": fmt.Sprintf(`{"status":"0x1","blockNumber":"0x64","logs":[%s]}`,
			evmTransferLogJSON("0x0000000000000000000000000000000000000dead", ourAddr, amountHex)),
		// 金额不足（一半）
		"0xunderpaid": fmt.Sprintf(`{"status":"0x1","blockNumber":"0x64","logs":[%s]}`,
			evmTransferLogJSON(bep20USDTContract, ourAddr, "0x97d330")),
		// 转给别人的
		"0xwrongto": fmt.Sprintf(`{"status":"0x1","blockNumber":"0x64","logs":[%s]}`,
			evmTransferLogJSON(bep20USDTContract, "0x3333333333333333333333333333333333333333", amountHex)),
	}
	mock := evmRPCMock(t, receipts, "0x78")

	// 确认数达标 → Found+Valid+Confirmed
	res := verifyEvmTxRPC(mock.URL, bep20USDTContract, "0xconfirmed", ourAddr, amount, bep20MinConfirmations)
	assertTrue(t, res.Err == nil && res.Found && res.Valid && res.Confirmed,
		fmt.Sprintf("确认数达标必须 confirmed: %+v", res))
	assertTrue(t, res.Confirmations == 21 && res.BlockNumber == 100,
		fmt.Sprintf("确认数/块高解析错误: %+v", res))
	assertTrue(t, res.ReceivedMicro == amount, fmt.Sprintf("实收金额解析错误: %+v", res))

	// 最低确认数抬高 → confirming（Found+Valid 但 !Confirmed）
	res = verifyEvmTxRPC(mock.URL, bep20USDTContract, "0xconfirmed", ourAddr, amount, 100)
	assertTrue(t, res.Found && res.Valid && !res.Confirmed,
		fmt.Sprintf("确认数不足必须 confirming: %+v", res))

	// reverted → invalid
	res = verifyEvmTxRPC(mock.URL, bep20USDTContract, "0xreverted", ourAddr, amount, bep20MinConfirmations)
	assertTrue(t, res.Found && !res.Valid, fmt.Sprintf("reverted 必须 invalid: %+v", res))

	// 未上链（result=null）→ not found
	res = verifyEvmTxRPC(mock.URL, bep20USDTContract, "0xmissing", ourAddr, amount, bep20MinConfirmations)
	assertTrue(t, res.Err == nil && !res.Found, fmt.Sprintf("receipt null 必须 not found: %+v", res))

	// 合约不符 → invalid
	res = verifyEvmTxRPC(mock.URL, bep20USDTContract, "0xwrongcontract", ourAddr, amount, bep20MinConfirmations)
	assertTrue(t, res.Found && !res.Valid, fmt.Sprintf("合约不符必须 invalid: %+v", res))

	// 金额不足 → invalid
	res = verifyEvmTxRPC(mock.URL, bep20USDTContract, "0xunderpaid", ourAddr, amount, bep20MinConfirmations)
	assertTrue(t, res.Found && !res.Valid, fmt.Sprintf("金额不足必须 invalid: %+v", res))

	// 收款地址不符 → invalid
	res = verifyEvmTxRPC(mock.URL, bep20USDTContract, "0xwrongto", ourAddr, amount, bep20MinConfirmations)
	assertTrue(t, res.Found && !res.Valid, fmt.Sprintf("地址不符必须 invalid: %+v", res))
}

// TestBillingVerifyChainDispatchRPC verifyChainTx 在 BSC_RPC_URL/ETH_RPC_URL 配置后
// 优先走裸 RPC（无需 scan API key），未配置任何通道时报中文原因。
func TestBillingVerifyChainDispatchRPC(t *testing.T) {
	const ourAddr = "0x2222222222222222222222222222222222222222"
	receipts := map[string]string{
		"0xconfirmed": fmt.Sprintf(`{"status":"0x1","blockNumber":"0x64","logs":[%s]}`,
			evmTransferLogJSON(bep20USDTContract, ourAddr, "0x12fa660")),
	}
	mock := evmRPCMock(t, receipts, "0x78")

	// BSC_RPC_URL 配置后：无 BSCSCAN_API_KEY 也能核验（RPC 优先）。
	t.Setenv("BSC_RPC_URL", mock.URL)
	t.Setenv("BSCSCAN_API_KEY", "")
	res := verifyChainTx("BEP20", "0xconfirmed", ourAddr, 19900000)
	assertTrue(t, res.Err == nil && res.Found && res.Valid && res.Confirmed,
		fmt.Sprintf("BSC_RPC_URL 通道必须可用: %+v", res))

	// ERC20 同理（ETH_RPC_URL）。
	t.Setenv("ETH_RPC_URL", mock.URL)
	t.Setenv("ETHERSCAN_API_KEY", "")
	res = verifyChainTx("ERC20", "0xconfirmed", ourAddr, 19900000)
	// mock 用的是 BEP20 合约，ERC20 合约不匹配 → invalid；这里只验证通道被分发到 RPC
	assertTrue(t, res.Found && !res.Valid, fmt.Sprintf("ETH_RPC_URL 必须被分发: %+v", res))

	// 全部未配置 → invalid + 中文原因（保持旧测试语义：原因含 key env 名）。
	t.Setenv("BSC_RPC_URL", "")
	t.Setenv("BSCSCAN_API_KEY", "")
	res = verifyChainTx("BEP20", "0xabc", ourAddr, 100)
	assertTrue(t, res.Found && !res.Valid && strings.Contains(res.Reason, "BSCSCAN_API_KEY"),
		fmt.Sprintf("缺通道必须 failed: %+v", res))
}

// TestBillingChainsGating 支付页链列表门控：地址未配置或核验通道未就绪的链不下发。
func TestBillingChainsGating(t *testing.T) {
	// BEP20：仅地址、无任何核验通道 → 不下发
	t.Setenv("USDT_TRC20_ADDRESS", tronTestB58())
	t.Setenv("USDT_BEP20_ADDRESS", "0x2222222222222222222222222222222222222222")
	t.Setenv("USDT_ERC20_ADDRESS", "")
	t.Setenv("USDT_SOL_ADDRESS", "")
	t.Setenv("BSC_RPC_URL", "")
	t.Setenv("BSCSCAN_API_KEY", "")

	r := setupRouter()
	r.GET("/billing/chains", BillingChains)
	code, resp := billingGetList(t, r, "/billing/chains")
	assertEq(t, http.StatusOK, code, "chains 200")
	chains := chainNamesOf(resp)
	assertTrue(t, chains["TRC20"], "TRC20 有地址必须下发")
	assertTrue(t, !chains["BEP20"], "BEP20 缺核验通道不得下发")

	// 配上 BSC_RPC_URL 后 BEP20 出现
	t.Setenv("BSC_RPC_URL", "http://127.0.0.1:18545")
	code, resp = billingGetList(t, r, "/billing/chains")
	assertEq(t, http.StatusOK, code, "chains 200")
	chains = chainNamesOf(resp)
	assertTrue(t, chains["BEP20"], "BSC_RPC_URL 配置后 BEP20 必须下发")
}

// billingGetList GET 返回 JSON 数组的接口。
func billingGetList(t *testing.T, r *gin.Engine, path string) (int, []map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	var parsed []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	return w.Code, parsed
}

func chainNamesOf(list []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, item := range list {
		if name, ok := item["chain"].(string); ok {
			out[name] = true
		}
	}
	return out
}
