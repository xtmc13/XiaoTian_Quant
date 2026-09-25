package handler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/config"
)

// ── 外部集成预检（Twilio / Stripe / IBKR / Turnstile / LLM / USDT 链上核验 / onchain API）──
// 以下集成都依赖真实密钥，历史上从未实机验证。本模块提供统一预检：
//   - configured：所需 env/密钥是否齐全（只报告布尔与变量名，绝不回显取值）
//   - reachable：端点连通性探测（HTTP HEAD 或 TCP dial，3s 超时）
//   - 凭证格式校验：仅检查前缀/形态（如 AC… / sk_live_… / 0x…40hex），不发起真实业务调用
//
// 探测只做连通性 + 格式校验：不发短信、不扣款、不下单、不消耗第三方配额写操作。
// 未配置的集成降级为 configured=false（reachable 恒为 null），不阻塞其他集成。

// IntegrationStatus 单个集成的预检状态（JSON 字段即前端契约）。
type IntegrationStatus struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Category    string `json:"category"`
	Configured  bool   `json:"configured"`
	// Reachable 三态：nil=未探测 / true=可达 / false=不可达。
	Reachable      *bool  `json:"reachable"`
	LastVerifiedAt int64  `json:"last_verified_at,omitempty"` // unix 毫秒；0 = 从未验证
	Notes          string `json:"notes,omitempty"`
}

// integrationSpec 集成注册表条目：Configured 只读 env/config，Endpoint 返回探测目标
// （http(s)://… 走 HTTP HEAD，否则按 host:port 走 TCP dial）。
type integrationSpec struct {
	Name        string
	DisplayName string
	Category    string
	RequiredEnv []string // 仅用于 notes 展示变量名
	Configured  func() (configured bool, note string)
	Endpoint    func() string
}

// integrationProbeTimeout 探测超时（包级变量，测试可缩短）。
var integrationProbeTimeout = 3 * time.Second

// integrationHTTPClient 探测专用客户端；Timeout 由 ctx 控制，这里不重复设。
var integrationHTTPClient = &http.Client{}

var (
	integrationProbeCache   = make(map[string]IntegrationStatus)
	integrationProbeCacheMu sync.Mutex
)

// ── 注册表 ─────────────────────────────────────────────────────

func integrationSpecs() []integrationSpec {
	return []integrationSpec{
		{
			Name:        "twilio",
			DisplayName: "Twilio 短信通知",
			Category:    "notify",
			RequiredEnv: []string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_FROM_NUMBER"},
			Configured: func() (bool, string) {
				sid := os.Getenv("TWILIO_ACCOUNT_SID")
				ok := sid != "" && os.Getenv("TWILIO_AUTH_TOKEN") != "" && os.Getenv("TWILIO_FROM_NUMBER") != ""
				if ok && !strings.HasPrefix(sid, "AC") {
					return true, "TWILIO_ACCOUNT_SID 格式异常（应以 AC 开头）"
				}
				return ok, ""
			},
			Endpoint: func() string {
				return envOrDefault("TWILIO_API_BASE", "https://api.twilio.com")
			},
		},
		{
			Name:        "stripe",
			DisplayName: "Stripe 支付/Webhook",
			Category:    "billing",
			RequiredEnv: []string{"STRIPE_SECRET_KEY", "STRIPE_WEBHOOK_SECRET"},
			Configured: func() (bool, string) {
				key := os.Getenv("STRIPE_SECRET_KEY")
				ok := key != "" && os.Getenv("STRIPE_WEBHOOK_SECRET") != ""
				if ok && !strings.HasPrefix(key, "sk_") && !strings.HasPrefix(key, "rk_") {
					return true, "STRIPE_SECRET_KEY 格式异常（应以 sk_ 或 rk_ 开头）"
				}
				return ok, ""
			},
			Endpoint: func() string { return "https://api.stripe.com" },
		},
		{
			Name:        "ibkr",
			DisplayName: "IBKR Client Portal",
			Category:    "broker",
			RequiredEnv: []string{"IBKR_HOST", "IBKR_ACCOUNT_ID"},
			Configured: func() (bool, string) {
				// IBKR 走本地网关会话 cookie 认证，无 API key；host/account 任一配置即视为启用。
				return adapter.IBKRConfigured(), ""
			},
			Endpoint: func() string {
				cfg := adapter.LoadIBKRConfig()
				if cfg.Paper {
					return fmt.Sprintf("%s:%d", firstNonEmptyStr(cfg.PaperHost, cfg.Host), firstNonZeroInt(cfg.PaperPort, cfg.Port))
				}
				return fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
			},
		},
		{
			Name:        "turnstile",
			DisplayName: "Cloudflare Turnstile 人机验证",
			Category:    "security",
			RequiredEnv: []string{"TURNSTILE_SECRET_KEY"},
			Configured: func() (bool, string) {
				return turnstileSecret() != "", ""
			},
			Endpoint: func() string { return turnstileVerifyURL },
		},
		llmIntegrationSpec("deepseek", "DeepSeek LLM", "DEEPSEEK_API_KEY", "https://api.deepseek.com/v1", "sk-"),
		llmIntegrationSpec("openai", "OpenAI LLM", "OPENAI_API_KEY", "https://api.openai.com/v1", "sk-"),
		llmIntegrationSpec("claude", "Anthropic Claude LLM", "ANTHROPIC_API_KEY", "https://api.anthropic.com/v1", "sk-ant-"),
		{
			Name:        "usdt_trc20",
			DisplayName: "USDT 充值核验 · TRC20 (TronGrid)",
			Category:    "billing_chain",
			RequiredEnv: []string{"USDT_TRC20_ADDRESS"},
			Configured: func() (bool, string) {
				addr := os.Getenv("USDT_TRC20_ADDRESS")
				if addr == "" {
					return false, ""
				}
				note := ""
				if !strings.HasPrefix(addr, "T") {
					note = "USDT_TRC20_ADDRESS 格式异常（应以 T 开头）"
				}
				if os.Getenv("TRONGRID_API_KEY") == "" {
					note = joinNotes(note, "未配置 TRONGRID_API_KEY，走公共限流通道")
				}
				return true, note
			},
			Endpoint: func() string { return tronGridBaseURL },
		},
		{
			Name:        "usdt_bep20",
			DisplayName: "USDT 充值核验 · BEP20 (BSC)",
			Category:    "billing_chain",
			RequiredEnv: []string{"USDT_BEP20_ADDRESS", "BSC_RPC_URL 或 BSCSCAN_API_KEY"},
			Configured: func() (bool, string) {
				addr := os.Getenv("USDT_BEP20_ADDRESS")
				if addr == "" || !bscVerifierReady() {
					return false, ""
				}
				if !looksLikeEVMAddress(addr) {
					return true, "USDT_BEP20_ADDRESS 格式异常（应为 0x + 40 位十六进制）"
				}
				return true, ""
			},
			Endpoint: func() string {
				if rpc := os.Getenv("BSC_RPC_URL"); rpc != "" {
					return rpc
				}
				return bscScanBaseURL
			},
		},
		{
			Name:        "usdt_erc20",
			DisplayName: "USDT 充值核验 · ERC20 (Ethereum)",
			Category:    "billing_chain",
			RequiredEnv: []string{"USDT_ERC20_ADDRESS", "ETH_RPC_URL 或 ETHERSCAN_API_KEY"},
			Configured: func() (bool, string) {
				addr := os.Getenv("USDT_ERC20_ADDRESS")
				if addr == "" || !ethVerifierReady() {
					return false, ""
				}
				if !looksLikeEVMAddress(addr) {
					return true, "USDT_ERC20_ADDRESS 格式异常（应为 0x + 40 位十六进制）"
				}
				return true, ""
			},
			Endpoint: func() string {
				if rpc := os.Getenv("ETH_RPC_URL"); rpc != "" {
					return rpc
				}
				return etherScanBaseURL
			},
		},
		{
			Name:        "usdt_sol",
			DisplayName: "USDT 充值核验 · Solana SPL",
			Category:    "billing_chain",
			RequiredEnv: []string{"USDT_SOL_ADDRESS"},
			Configured: func() (bool, string) {
				addr := os.Getenv("USDT_SOL_ADDRESS")
				if addr == "" {
					return false, ""
				}
				if !looksLikeBase58(addr) {
					return true, "USDT_SOL_ADDRESS 格式异常（应为 base58 编码）"
				}
				return true, ""
			},
			Endpoint: solanaRPCURL,
		},
		{
			Name:        "onchain_api",
			DisplayName: "链上数据 API（玻璃节点/公共端点）",
			Category:    "data",
			RequiredEnv: []string{"ONCHAIN_API_KEY"},
			Configured: func() (bool, string) {
				if os.Getenv("ONCHAIN_API_KEY") != "" {
					return true, ""
				}
				// 公共免费端点（blockchain.info）无需密钥也能用，按未配置降级处理。
				return false, "未配置 ONCHAIN_API_KEY 时回退公共免费端点"
			},
			Endpoint: func() string { return "https://blockchain.info" },
		},
	}
}

// llmIntegrationSpec LLM provider 条目：configured = env key 或 config 中同名
// provider 已启用且带 key；endpoint 取 config 的 base_url（未配置回退默认）。
func llmIntegrationSpec(name, display, keyEnv, defaultBase, keyPrefix string) integrationSpec {
	return integrationSpec{
		Name:        "llm_" + name,
		DisplayName: display,
		Category:    "llm",
		RequiredEnv: []string{keyEnv},
		Configured: func() (bool, string) {
			key := os.Getenv(keyEnv)
			if p := config.Get().AIProvider(name); p != nil && p.APIKey != "" {
				key = p.APIKey
			}
			if key == "" {
				return false, ""
			}
			if keyPrefix != "" && !strings.HasPrefix(key, keyPrefix) {
				return true, fmt.Sprintf("%s 格式异常（应以 %s 开头）", keyEnv, keyPrefix)
			}
			return true, ""
		},
		Endpoint: func() string {
			if p := config.Get().AIProvider(name); p != nil && p.BaseURL != "" {
				return p.BaseURL
			}
			return defaultBase
		},
	}
}

// ── 状态聚合（纯逻辑，单测覆盖） ────────────────────────────────

// currentIntegrationStatus 聚合成当前对外状态：configured 实时计算，
// reachable/last_verified_at 取自探测缓存；未配置时丢弃缓存探测结果（降级）。
func currentIntegrationStatus(spec integrationSpec) IntegrationStatus {
	configured, note := spec.Configured()
	st := IntegrationStatus{
		Name:        spec.Name,
		DisplayName: spec.DisplayName,
		Category:    spec.Category,
		Configured:  configured,
		Notes:       note,
	}
	if !configured {
		if st.Notes == "" {
			st.Notes = fmt.Sprintf("未配置（需要: %s）", strings.Join(spec.RequiredEnv, ", "))
		} else {
			st.Notes = joinNotes(st.Notes, fmt.Sprintf("需要: %s", strings.Join(spec.RequiredEnv, ", ")))
		}
		return st
	}
	integrationProbeCacheMu.Lock()
	cached, ok := integrationProbeCache[spec.Name]
	integrationProbeCacheMu.Unlock()
	if ok {
		st.Reachable = cached.Reachable
		st.LastVerifiedAt = cached.LastVerifiedAt
		st.Notes = joinNotes(st.Notes, cached.Notes)
	}
	return st
}

// runIntegrationCheck 主动单项探测：未配置直接降级不探测；
// 已配置则对 Endpoint 做连通性探测并写缓存。
func runIntegrationCheck(spec integrationSpec) IntegrationStatus {
	configured, note := spec.Configured()
	if !configured {
		integrationProbeCacheMu.Lock()
		delete(integrationProbeCache, spec.Name)
		integrationProbeCacheMu.Unlock()
		return currentIntegrationStatus(spec)
	}

	ctx, cancel := context.WithTimeout(context.Background(), integrationProbeTimeout)
	defer cancel()
	reachable, probeNote := probeEndpoint(ctx, spec.Endpoint())

	// 缓存只存探测结果（probeNote）；凭证格式等配置侧 note 在聚合时实时拼接，
	// 避免配置 note 在缓存与实时计算间重复。
	cached := IntegrationStatus{
		Name:           spec.Name,
		DisplayName:    spec.DisplayName,
		Category:       spec.Category,
		Configured:     true,
		Reachable:      &reachable,
		LastVerifiedAt: time.Now().UnixMilli(),
		Notes:          probeNote,
	}
	integrationProbeCacheMu.Lock()
	integrationProbeCache[spec.Name] = cached
	integrationProbeCacheMu.Unlock()
	st := cached
	st.Notes = joinNotes(note, probeNote)
	return st
}

// probeEndpoint 连通性探测：http(s):// 走 HTTP HEAD（任何 HTTP 响应都视为可达，
// 包括 401/405——只证明网络与 TLS 通，不碰业务语义）；否则按 host:port TCP dial。
func probeEndpoint(ctx context.Context, endpoint string) (bool, string) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return false, "端点为空"
	}
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
		if err != nil {
			return false, fmt.Sprintf("端点解析失败: %v", err)
		}
		resp, err := integrationHTTPClient.Do(req)
		if err != nil {
			return false, probeErrNote(ctx, err)
		}
		resp.Body.Close()
		return true, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", endpoint)
	if err != nil {
		return false, probeErrNote(ctx, err)
	}
	conn.Close()
	return true, "TCP 连接成功"
}

// probeErrNote 区分超时与其他网络错误（超时要明确标注，便于区分"挂了"与"慢"）。
func probeErrNote(ctx context.Context, err error) string {
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("探测超时（>%s）", integrationProbeTimeout)
	}
	return fmt.Sprintf("连接失败: %v", err)
}

// ── HTTP handlers ──────────────────────────────────────────────

// IntegrationsStatus GET /api/integrations/status（登录可见）。
// configured 实时反映 env；reachable 为最近一次主动探测的缓存结果（null=未探测）。
func IntegrationsStatus(c *gin.Context) {
	specs := integrationSpecs()
	out := make([]IntegrationStatus, 0, len(specs))
	for _, spec := range specs {
		out = append(out, currentIntegrationStatus(spec))
	}
	c.JSON(http.StatusOK, gin.H{"integrations": out})
}

// IntegrationCheck POST /api/integrations/:name/check（admin only，路由层挂 AdminRequired）。
func IntegrationCheck(c *gin.Context) {
	name := c.Param("name")
	for _, spec := range integrationSpecs() {
		if spec.Name == name {
			c.JSON(http.StatusOK, runIntegrationCheck(spec))
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("未知集成: %s", name)})
}

// ── 小工具 ─────────────────────────────────────────────────────

func joinNotes(parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "；")
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonZeroInt(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

// looksLikeEVMAddress 0x + 40 位十六进制。
func looksLikeEVMAddress(s string) bool {
	if len(s) != 42 || !strings.HasPrefix(s, "0x") {
		return false
	}
	for _, r := range s[2:] {
		if !('0' <= r && r <= '9' || 'a' <= r && r <= 'f' || 'A' <= r && r <= 'F') {
			return false
		}
	}
	return true
}

// looksLikeBase58 base58 字符集（BTC/SOL 地址编码），长度 26–64。
func looksLikeBase58(s string) bool {
	if len(s) < 26 || len(s) > 64 {
		return false
	}
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	for _, r := range s {
		if !strings.ContainsRune(alphabet, r) {
			return false
		}
	}
	return true
}
