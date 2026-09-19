package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// 链上核验器：TRC20(TronGrid) / BEP20(BscScan) / ERC20(Etherscan) / SOL(Solana RPC)。
// 全部基于标准库 net/http，base URL 做成包级变量便于 httptest 注入。
// 金额一律微单位整数比较。

var (
	tronGridBaseURL   = envOrDefault("TRONGRID_BASE_URL", "https://api.trongrid.io")
	bscScanBaseURL    = envOrDefault("BSCSCAN_BASE_URL", "https://api.bscscan.com")
	etherScanBaseURL  = envOrDefault("ETHERSCAN_BASE_URL", "https://api.etherscan.io")
	solanaRPCBaseURL  = envOrDefault("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com")
	chainVerifyClient = &http.Client{Timeout: 10 * time.Second}
)

// 链上核验参数（可被环境变量覆盖）：
//   - TRON_MIN_CONFIRMATIONS：TRC20 到账所需区块确认数（默认 19，TRON 常用安全确认）
//   - BILLING_AMOUNT_TOLERANCE_PCT：金额容许偏差百分比（默认 1%，少付在该比例内仍认可）
func tronMinConfirmations() int64 {
	if v := os.Getenv("TRON_MIN_CONFIRMATIONS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 19
}

func amountTolerancePct() int64 {
	if v := os.Getenv("BILLING_AMOUNT_TOLERANCE_PCT"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 && n <= 100 {
			return n
		}
	}
	return 1
}

// minAcceptableMicro 容许 tolerancePct% 少付后的最低可接受金额（微单位整数比较）。
func minAcceptableMicro(amountMicro, tolerancePct int64) int64 {
	if tolerancePct <= 0 {
		return amountMicro
	}
	return amountMicro * (100 - tolerancePct) / 100
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// USDT 合约地址（链原生格式）。
const (
	tronUSDTContract   = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"           // TRC20 (base58)
	erc20USDTContract  = "0xdac17f958d2ee523a2206206994597c13d831ec7"   // Ethereum
	bep20USDTContract  = "0x55d398326f99059ff775485246999027b3197955"   // BSC
	solUSDTMint        = "Es9vMFrzaCERmJfrF4H2FYD4PFbLDsad9tubN9syQ2a4" // Solana SPL
	erc20TransferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
)

// 最低区块确认数（低于该值视为 confirming）。
const (
	erc20MinConfirmations = int64(12)
	bep20MinConfirmations = int64(15)
)

// chainVerifyResult 核验结果三态：
//   - Err != nil：网络/接口错误（只累加尝试次数，不改状态）
//   - !Found：链上还没查到该交易（累加尝试次数，超上限转 failed）
//   - Found：交易已上链；Valid=地址/金额/合约校验是否通过；Confirmed=确认数是否达标
// Details 附带链上细节（GET /verification 展示），无则零值。
type chainVerifyResult struct {
	Err       error // 网络/接口错误（只累加尝试次数，不改状态）
	Found     bool
	Valid     bool
	Confirmed bool
	Reason    string // 中文原因（Valid=false 时写入 fail_reason）

	// 链上细节
	BlockNumber           int64
	Confirmations         int64
	RequiredConfirmations int64
	ReceivedMicro         int64
	ToAddress             string
}

// verifyChainTx 按链分发核验。
func verifyChainTx(chain, txHash, address string, amountMicro int64) chainVerifyResult {
	switch chain {
	case "TRC20":
		return verifyTRC20Tx(txHash, address, amountMicro)
	case "BEP20":
		key := os.Getenv("BSCSCAN_API_KEY")
		if key == "" {
			return chainVerifyResult{Found: true, Valid: false, Reason: "未配置核验密钥（BSCSCAN_API_KEY）"}
		}
		return verifyEvmTx(bscScanBaseURL, key, bep20USDTContract, txHash, address, amountMicro, bep20MinConfirmations)
	case "ERC20":
		key := os.Getenv("ETHERSCAN_API_KEY")
		if key == "" {
			return chainVerifyResult{Found: true, Valid: false, Reason: "未配置核验密钥（ETHERSCAN_API_KEY）"}
		}
		return verifyEvmTx(etherScanBaseURL, key, erc20USDTContract, txHash, address, amountMicro, erc20MinConfirmations)
	case "SOL":
		return verifySOLTx(txHash, address, amountMicro)
	}
	return chainVerifyResult{Found: true, Valid: false, Reason: "不支持的链"}
}

// getJSON 发起 GET 并把响应体读出来（限制 2MB，防止异常大包）。
func getJSON(url string, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := chainVerifyClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return body, resp.StatusCode, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	return body, resp.StatusCode, nil
}

// ── TRC20 (TronGrid) ──

// verifyTRC20Tx 通过 TronGrid 交易事件核验 USDT 转账：
// 合约地址匹配、to 地址匹配（base58/hex 归一化比较）、金额 ≥ 订单额（容许 1% 少付）、
// 区块确认数 ≥ TRON_MIN_CONFIRMATIONS（默认 19）。事件带 block_number 时严格计算确认数；
// TronGrid 事件缺 block_number（部分历史/镜像节点）时退化为"已确认"（保持原行为）。
func verifyTRC20Tx(txHash, address string, amountMicro int64) chainVerifyResult {
	url := fmt.Sprintf("%s/v1/transactions/%s/events", tronGridBaseURL, txHash)
	headers := map[string]string{}
	if key := os.Getenv("TRONGRID_API_KEY"); key != "" {
		headers["TRON-PRO-API-KEY"] = key
	}
	body, _, err := getJSON(url, headers)
	if err != nil {
		return chainVerifyResult{Err: err}
	}
	// TronGrid 对不存在/未确认交易返回 success=false 或空 data。
	var payload struct {
		Success bool `json:"success"`
		Data    []struct {
			Contract    string `json:"contract_address"`
			EventName   string `json:"event_name"`
			BlockNumber int64  `json:"block_number"`
			Result      struct {
				Value     string `json:"value"`
				ToAddress string `json:"to_address"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return chainVerifyResult{Err: fmt.Errorf("trongrid 响应解析失败: %w", err)}
	}
	if !payload.Success && len(payload.Data) == 0 {
		return chainVerifyResult{Found: false}
	}

	wantAddr, err := tronAddressBytes(address)
	if err != nil {
		return chainVerifyResult{Found: true, Valid: false, Reason: "本站收款地址配置非法"}
	}
	minAccept := minAcceptableMicro(amountMicro, amountTolerancePct())
	var (
		matched      bool
		matchedBlock int64
		received     int64
	)
	for _, ev := range payload.Data {
		if ev.EventName != "Transfer" || !strings.EqualFold(ev.Contract, tronUSDTContract) {
			continue
		}
		to, err := tronAddressBytes(ev.Result.ToAddress)
		if err != nil || !bytes.Equal(to, wantAddr) {
			continue
		}
		v, ok := new(big.Int).SetString(ev.Result.Value, 10)
		if !ok {
			continue
		}
		received += v.Int64()
		if v.Int64() >= minAccept {
			matched = true
			if ev.BlockNumber > matchedBlock {
				matchedBlock = ev.BlockNumber
			}
			break
		}
	}
	res := chainVerifyResult{
		Found:                 true,
		ReceivedMicro:         received,
		ToAddress:             address,
		RequiredConfirmations: tronMinConfirmations(),
		BlockNumber:           matchedBlock,
	}
	if !matched {
		res.Valid = false
		res.Reason = fmt.Sprintf("交易未向本站地址转入足额 USDT（实收 %d 微单位，要求 ≥%d，容许偏差 %d%%）",
			received, minAccept, amountTolerancePct())
		return res
	}
	res.Valid = true

	// 确认数：事件带 block_number 时查最新区块严格计算；查不到最新区块属网络
	// 错误，不下结论（保持 confirming/pending 下轮重试），绝不误判 confirmed。
	if matchedBlock <= 0 {
		// TronGrid 部分响应无 block_number，无法计算确认数 → 维持原"视为已确认"行为。
		res.Confirmed = true
		res.Confirmations = res.RequiredConfirmations
		return res
	}
	latest, err := tronLatestBlock()
	if err != nil {
		return chainVerifyResult{Err: err}
	}
	res.Confirmations = latest - matchedBlock + 1
	res.Confirmed = res.Confirmations >= res.RequiredConfirmations
	return res
}

// tronLatestBlock 查 TronGrid 最新区块号（/v1/blocks/latest/detail）。
func tronLatestBlock() (int64, error) {
	headers := map[string]string{}
	if key := os.Getenv("TRONGRID_API_KEY"); key != "" {
		headers["TRON-PRO-API-KEY"] = key
	}
	body, _, err := getJSON(tronGridBaseURL+"/v1/blocks/latest/detail", headers)
	if err != nil {
		return 0, err
	}
	var payload struct {
		Number int64 `json:"number"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Number <= 0 {
		return 0, fmt.Errorf("trongrid 最新区块解析失败: %w", err)
	}
	return payload.Number, nil
}

// tronAddressBytes 把 TRON 地址（base58 或 41 开头 hex）归一化为 21 字节形式。
func tronAddressBytes(addr string) ([]byte, error) {
	addr = strings.TrimSpace(addr)
	if len(addr) == 42 && strings.HasPrefix(strings.ToLower(addr), "41") {
		b, err := hexDecode(addr)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	raw, err := base58Decode(addr)
	if err != nil {
		return nil, err
	}
	if len(raw) < 21 {
		return nil, fmt.Errorf("bad tron address length %d", len(raw))
	}
	return raw[:21], nil
}

func hexDecode(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd hex length")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi, ok1 := hexVal(s[2*i])
		lo, ok2 := hexVal(s[2*i+1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("bad hex char")
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}

func hexVal(ch byte) (byte, bool) {
	switch {
	case ch >= '0' && ch <= '9':
		return ch - '0', true
	case ch >= 'a' && ch <= 'f':
		return ch - 'a' + 10, true
	}
	return 0, false
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58Decode 解码 base58（比特币字母表），用于 TRON 地址。
func base58Decode(s string) ([]byte, error) {
	n := big.NewInt(0)
	base := big.NewInt(58)
	for i := 0; i < len(s); i++ {
		idx := strings.IndexByte(base58Alphabet, s[i])
		if idx < 0 {
			return nil, fmt.Errorf("bad base58 char %q", s[i])
		}
		n.Mul(n, base)
		n.Add(n, big.NewInt(int64(idx)))
	}
	dec := n.Bytes()
	// 补前导 '1' 对应的 0x00
	lead := 0
	for lead < len(s) && s[lead] == '1' {
		lead++
	}
	out := make([]byte, lead+len(dec))
	copy(out[lead:], dec)
	return out, nil
}

// ── BEP20 / ERC20 (Etherscan 同构 API) ──

// verifyEvmTx 核验 EVM 链 USDT 转账：receipt 日志中合约+Transfer(to=本站)+金额合计，
// 再用 eth_blockNumber 计算确认数。
func verifyEvmTx(baseURL, apiKey, contract, txHash, address string, amountMicro, minConfirmations int64) chainVerifyResult {
	receiptURL := fmt.Sprintf("%s/api?module=proxy&action=eth_getTransactionReceipt&txhash=%s&apikey=%s", baseURL, txHash, apiKey)
	body, _, err := getJSON(receiptURL, nil)
	if err != nil {
		return chainVerifyResult{Err: err}
	}
	var receiptResp struct {
		Result *struct {
			Status      string `json:"status"` // "0x1" 成功
			BlockNumber string `json:"blockNumber"`
			Logs        []struct {
				Address string   `json:"address"`
				Topics  []string `json:"topics"`
				Data    string   `json:"data"`
			} `json:"logs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &receiptResp); err != nil {
		return chainVerifyResult{Err: fmt.Errorf("receipt 响应解析失败: %w", err)}
	}
	if receiptResp.Result == nil {
		// 交易未上链或还未打包（节点返回 null）
		return chainVerifyResult{Found: false}
	}
	rc := receiptResp.Result
	if rc.Status != "0x1" {
		return chainVerifyResult{Found: true, Valid: false, Reason: "链上交易执行失败（reverted）"}
	}

	wantTo := strings.ToLower(strings.TrimPrefix(address, "0x"))
	sum := new(big.Int)
	for _, lg := range rc.Logs {
		if len(lg.Topics) < 3 || !strings.EqualFold(lg.Address, contract) {
			continue
		}
		if !strings.EqualFold(lg.Topics[0], erc20TransferTopic) {
			continue
		}
		// topics[2] = to 地址（32 字节左填充），归一化为 40 位 hex 比较
		to := strings.TrimLeft(strings.ToLower(strings.TrimPrefix(lg.Topics[2], "0x")), "0")
		for len(to) < 40 {
			to = "0" + to
		}
		if len(to) != 40 || !strings.EqualFold(to, wantTo) {
			continue
		}
		data, err := hexDecode(strings.TrimPrefix(lg.Data, "0x"))
		if err != nil {
			continue
		}
		sum.Add(sum, new(big.Int).SetBytes(data))
	}
	if sum.Cmp(big.NewInt(minAcceptableMicro(amountMicro, amountTolerancePct()))) < 0 {
		return chainVerifyResult{Found: true, Valid: false, Reason: fmt.Sprintf("交易未向本站地址转入足额 USDT（实收 %s 微单位，容许偏差 %d%%）", sum.String(), amountTolerancePct()),
			ReceivedMicro: sum.Int64(), ToAddress: address, RequiredConfirmations: minConfirmations}
	}

	// 确认数：eth_blockNumber - tx blockNumber
	confirmed := false
	var confirmations int64
	blockURL := fmt.Sprintf("%s/api?module=proxy&action=eth_blockNumber&apikey=%s", baseURL, apiKey)
	if body, _, err := getJSON(blockURL, nil); err == nil {
		var bnResp struct {
			Result string `json:"result"`
		}
		if json.Unmarshal(body, &bnResp) == nil {
			latest, _ := strconv.ParseInt(strings.TrimPrefix(bnResp.Result, "0x"), 16, 64)
			txBlock, _ := strconv.ParseInt(strings.TrimPrefix(rc.BlockNumber, "0x"), 16, 64)
			if latest > 0 {
				confirmations = latest - txBlock + 1
				if confirmations >= minConfirmations {
					confirmed = true
				}
			}
		}
	}
	txBlockNum, _ := strconv.ParseInt(strings.TrimPrefix(rc.BlockNumber, "0x"), 16, 64)
	return chainVerifyResult{Found: true, Valid: true, Confirmed: confirmed,
		Confirmations: confirmations, RequiredConfirmations: minConfirmations,
		BlockNumber: txBlockNum, ReceivedMicro: sum.Int64(), ToAddress: address}
}

// ── SOL (Solana public RPC) ──

// verifySOLTx 通过 getTransaction(jsonParsed) 核验 SPL USDT 转入：
// pre/postTokenBalances 差额（本站地址、USDT mint）≥ 订单额且交易执行成功。
func verifySOLTx(txHash, address string, amountMicro int64) chainVerifyResult {
	payload := map[string]any{
		"jsonrpc": "2.0", "id": 1,
		"method": "getTransaction",
		"params": []any{txHash, map[string]any{
			"encoding":                       "jsonParsed",
			"commitment":                     "confirmed",
			"maxSupportedTransactionVersion": 0,
		}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return chainVerifyResult{Err: err}
	}
	req, err := http.NewRequest(http.MethodPost, solanaRPCBaseURL, bytes.NewReader(raw))
	if err != nil {
		return chainVerifyResult{Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := chainVerifyClient.Do(req)
	if err != nil {
		return chainVerifyResult{Err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return chainVerifyResult{Err: err}
	}
	var rpcResp struct {
		Result *struct {
			Meta *struct {
				Err               any               `json:"err"`
				PreTokenBalances  []solTokenBalance `json:"preTokenBalances"`
				PostTokenBalances []solTokenBalance `json:"postTokenBalances"`
			} `json:"meta"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return chainVerifyResult{Err: fmt.Errorf("solana 响应解析失败: %w", err)}
	}
	if rpcResp.Result == nil || rpcResp.Result.Meta == nil {
		return chainVerifyResult{Found: false} // 未上链或查询节点未命中
	}
	meta := rpcResp.Result.Meta
	if meta.Err != nil {
		return chainVerifyResult{Found: true, Valid: false, Reason: "链上交易执行失败"}
	}

	balanceOf := func(list []solTokenBalance) map[int64]*big.Int {
		m := map[int64]*big.Int{}
		for _, b := range list {
			if b.Mint != solUSDTMint {
				continue
			}
			v, ok := new(big.Int).SetString(b.UiTokenAmount.Amount, 10)
			if !ok {
				continue
			}
			m[int64(b.AccountIndex)] = v
		}
		return m
	}
	pre := balanceOf(meta.PreTokenBalances)
	post := balanceOf(meta.PostTokenBalances)

	want := int64(0)
	sum := new(big.Int)
	// 本站地址涉及的账户：用 postTokenBalances 里的 owner 字段定位
	for _, b := range meta.PostTokenBalances {
		if b.Mint != solUSDTMint || !strings.EqualFold(b.Owner, address) {
			continue
		}
		idx := int64(b.AccountIndex)
		postV := post[idx]
		preV := pre[idx]
		if preV == nil {
			preV = big.NewInt(0)
		}
		if postV == nil {
			continue
		}
		sum.Add(sum, new(big.Int).Sub(postV, preV))
		want++
	}
	if want == 0 || sum.Cmp(big.NewInt(minAcceptableMicro(amountMicro, amountTolerancePct()))) < 0 {
		return chainVerifyResult{Found: true, Valid: false, Reason: fmt.Sprintf("交易未向本站地址转入足额 USDT（实收 %s 微单位，容许偏差 %d%%）", sum.String(), amountTolerancePct()),
			ReceivedMicro: sum.Int64(), ToAddress: address}
	}
	// commitment=confirmed 返回即视为已确认
	return chainVerifyResult{Found: true, Valid: true, Confirmed: true,
		Confirmations: 1, RequiredConfirmations: 1,
		ReceivedMicro: sum.Int64(), ToAddress: address}
}

type solTokenBalance struct {
	AccountIndex  int    `json:"accountIndex"`
	Mint          string `json:"mint"`
	Owner         string `json:"owner"`
	UiTokenAmount struct {
		Amount   string `json:"amount"` // 原始最小单位字符串
		Decimals int    `json:"decimals"`
	} `json:"uiTokenAmount"`
}
