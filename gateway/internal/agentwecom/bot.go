// Package agentwecom 企业微信双向通道（回调模式）：
// 平台回调（WXBizMsgCrypt 验签 + 解密 XML）→ 配对 / 斜杠命令 / headless 执行
// → cgi-bin access token 缓存 + message/send 主动推送。
package agentwecom

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxChunkRunes 单条消息最大 rune 数（企业微信文本上限 2048 字节，保守取 1900 runes）。
const maxChunkRunes = 1900

// RunRequest 入站消息执行请求：会话绑定 / 模型覆盖可选。
type RunRequest struct {
	UserID         int64
	Prompt         string
	ConversationID string // 绑定的会话 id（空 = 无会话）
	Model          string // 模型覆盖（"provider" 或 "provider:model"）
}

// Executor 支持 ctx 中断（/stop）的执行器（main 注入 headless runner）。
type Executor func(ctx context.Context, req *RunRequest) (string, error)

// credentials 解析企业微信应用凭据（环境变量）。
func credentials() (corpID, secret, token, aesKey, agentID string) {
	return strings.TrimSpace(os.Getenv("WECOM_CORP_ID")),
		strings.TrimSpace(os.Getenv("WECOM_SECRET")),
		strings.TrimSpace(os.Getenv("WECOM_TOKEN")),
		strings.TrimSpace(os.Getenv("WECOM_AES_KEY")),
		strings.TrimSpace(os.Getenv("WECOM_AGENT_ID"))
}

// Configured 应用凭据是否已配置（未配置 = 通道惰性：status configured=false，webhook 503）。
func Configured() bool {
	corpID, secret, token, aesKey, _ := credentials()
	return corpID != "" && secret != "" && token != "" && aesKey != ""
}

// ── 回调 XML 结构（只取所需字段） ──

// encryptedEnvelope POST 回调外层信封（密文在 Encrypt 字段）。
type encryptedEnvelope struct {
	XMLName    xml.Name `xml:"xml"`
	ToUserName string   `xml:"ToUserName"`
	Encrypt    string   `xml:"Encrypt"`
	AgentID    string   `xml:"AgentID"`
}

// plainMessage 解密后的内层消息。
type plainMessage struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
	MsgID        string   `xml:"MsgId"`
	AgentID      string   `xml:"AgentID"`
}

// Bot 企业微信通道：回调验签/解密路由 + 配对 + 单飞执行 + token 缓存推送。
type Bot struct {
	repo     *Repo
	exec     Executor
	healthFn func() string
	client   *http.Client

	// 可注入端点（测试指向 httptest；空 = 官方域名）
	apiBase string

	mu       sync.Mutex
	token    string                        // access_token 缓存
	tokenExp time.Time                     // 过期时间（TTL-60s 提前刷新）
	runs     map[string]context.CancelFunc // staff_id → 在途 run 取消函数（单飞）
}

// NewBot 构造（凭据每次调用现取，支持环境热改）。
func NewBot(repo *Repo) *Bot {
	return &Bot{
		repo:   repo,
		runs:   map[string]context.CancelFunc{},
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetAPIBase 覆盖 API 地址（测试用）。
func (b *Bot) SetAPIBase(base string) { b.apiBase = strings.TrimSuffix(base, "/") }

// SetExecutor 注入执行器。
func (b *Bot) SetExecutor(exec Executor) { b.exec = exec }

// SetHealthProvider 注入 /status 的网关健康行。
func (b *Bot) SetHealthProvider(fn func() string) { b.healthFn = fn }

// restBase API 域名（注入值优先，其次官方默认）。
func (b *Bot) restBase() string {
	if b.apiBase != "" {
		return b.apiBase
	}
	return "https://qyapi.weixin.qq.com"
}

// HandleWebhook /api/agent/wecom/webhook（公开路由，无 JWT）：
// GET = URL 验证（验签 + 解密 echostr，明文回写）；POST = 消息回调
// （验签 + 解密 XML，立即 200 空体、异步处理）。
func (b *Bot) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if !Configured() {
		http.Error(w, `{"detail":"企业微信未配置（WECOM_CORP_ID/WECOM_SECRET/WECOM_TOKEN/WECOM_AES_KEY）"}`, http.StatusServiceUnavailable)
		return
	}
	corpID, _, token, aesKey, _ := credentials()
	key, err := decodeAESKey(aesKey)
	if err != nil {
		http.Error(w, `{"detail":"WECOM_AES_KEY 非法"}`, http.StatusInternalServerError)
		return
	}
	q := r.URL.Query()
	sign, timestamp, nonce := q.Get("msg_signature"), q.Get("timestamp"), q.Get("nonce")

	switch r.Method {
	case http.MethodGet:
		echostr := q.Get("echostr")
		if echostr == "" || msgSignature(token, timestamp, nonce, echostr) != sign {
			http.Error(w, `{"detail":"签名验证失败"}`, http.StatusUnauthorized)
			return
		}
		plain, rcvCorp, err := decryptMsg(key, echostr)
		if err != nil || rcvCorp != corpID {
			http.Error(w, `{"detail":"echostr 解密失败"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(plain)

	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var env encryptedEnvelope
		if err := xml.Unmarshal(body, &env); err != nil || env.Encrypt == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if msgSignature(token, timestamp, nonce, env.Encrypt) != sign {
			http.Error(w, `{"detail":"签名验证失败"}`, http.StatusUnauthorized)
			return
		}
		plain, rcvCorp, err := decryptMsg(key, env.Encrypt)
		if err != nil || rcvCorp != corpID {
			http.Error(w, `{"detail":"消息解密失败"}`, http.StatusUnauthorized)
			return
		}
		var msg plainMessage
		if err := xml.Unmarshal(plain, &msg); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		// 立即 200 空体（企业微信要求秒级响应），异步处理
		w.WriteHeader(http.StatusOK)
		if msg.MsgType == "text" {
			m := msg
			go b.handleMessage(&m)
		}

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleMessage 消息路由：/help → 命令清单；6 位数字 → 配对；未绑定 → 指引；
// 其余斜杠命令 → handleCommand；已绑定普通消息 → 单飞 headless 执行并主动推送。
func (b *Bot) handleMessage(msg *plainMessage) {
	staffID := msg.FromUserName
	text := strings.TrimSpace(msg.Content)
	if staffID == "" || text == "" {
		return
	}

	switch {
	case text == "/help":
		b.reply(staffID, helpText())
		return
	case len(text) == 6 && isDigits(text):
		link, err := b.repo.ConsumePairCode(text, staffID)
		if err != nil {
			b.reply(staffID, "配对码无效或已过期，请在网页端重新生成。")
			return
		}
		b.reply(staffID, fmt.Sprintf("✅ 绑定成功（user_id=%d）。现在直接发消息即可使唤你的交易助手。", link.UserID))
		return
	}

	link, err := b.repo.GetByStaffID(staffID)
	if err != nil {
		b.reply(staffID, "你还没有绑定账号。在网页端 助手 → 企业微信接入 生成配对码后发给我。")
		return
	}
	if strings.HasPrefix(text, "/") {
		b.handleCommand(staffID, link, text)
		return
	}
	b.runInbound(staffID, link, text)
}

func helpText() string {
	return "可用命令：\n" +
		"/new — 开启新会话（清除会话绑定）\n" +
		"/stop — 中断当前正在执行的任务\n" +
		"/status — 绑定状态 / 模型 / 网关健康\n" +
		"/model <name> — 设置模型覆盖（/model 查看，/model default 恢复默认）\n" +
		"/help — 本清单"
}

// handleCommand 已绑定用户的斜杠命令分发。
func (b *Bot) handleCommand(staffID string, link *Link, text string) {
	fields := strings.Fields(text)
	cmd := strings.TrimPrefix(fields[0], "/")
	arg := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))

	switch cmd {
	case "new":
		if err := b.repo.SetConversationID(staffID, ""); err != nil {
			b.reply(staffID, "操作失败："+truncate(err.Error(), 200))
			return
		}
		b.reply(staffID, "🆕 已开启新会话，下条消息将从头开始。")

	case "stop":
		b.mu.Lock()
		cancel, ok := b.runs[staffID]
		b.mu.Unlock()
		if !ok {
			b.reply(staffID, "当前没有正在执行的任务。")
			return
		}
		cancel()
		b.reply(staffID, "⏹ 已发送中断信号。")

	case "status":
		model := link.Model
		if model == "" {
			model = "默认（跟随网关配置）"
		}
		conv := "未绑定"
		if link.ConversationID != "" {
			conv = link.ConversationID
		}
		health := "运行中"
		if b.healthFn != nil {
			health = b.healthFn()
		}
		b.reply(staffID, fmt.Sprintf("📊 状态\n绑定账号：%s（user_id=%d）\n模型：%s\n会话绑定：%s\n网关：%s",
			staffID, link.UserID, model, conv, health))

	case "model":
		if arg == "" {
			cur := link.Model
			if cur == "" {
				cur = "默认（跟随网关配置）"
			}
			b.reply(staffID, "当前模型："+cur+"\n用 /model <name> 覆盖，/model default 恢复默认。")
			return
		}
		if arg == "default" || arg == "off" || arg == "auto" {
			if err := b.repo.SetModel(staffID, ""); err != nil {
				b.reply(staffID, "操作失败："+truncate(err.Error(), 200))
				return
			}
			b.reply(staffID, "已恢复默认模型。")
			return
		}
		if err := b.repo.SetModel(staffID, arg); err != nil {
			b.reply(staffID, "操作失败："+truncate(err.Error(), 200))
			return
		}
		b.reply(staffID, "✅ 后续使用模型："+arg)

	case "help":
		b.reply(staffID, helpText())

	default:
		b.reply(staffID, "未知命令 /"+cmd+"，/help 查看可用命令。")
	}
}

// runInbound 绑定用户普通消息：单飞执行 → 主动推送（超长分条）。
func (b *Bot) runInbound(staffID string, link *Link, text string) {
	// 单飞：同 staff 并发消息直接提示
	b.mu.Lock()
	if _, busy := b.runs[staffID]; busy {
		b.mu.Unlock()
		b.reply(staffID, "正在处理上一条消息，/stop 可中断。")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.runs[staffID] = cancel
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.runs, staffID)
		b.mu.Unlock()
		cancel()
	}()

	if b.exec == nil {
		b.reply(staffID, "执行器未配置，请联系管理员。")
		return
	}
	reply, err := b.exec(ctx, &RunRequest{
		UserID:         link.UserID,
		Prompt:         text,
		ConversationID: link.ConversationID,
		Model:          link.Model,
	})
	switch {
	case ctx.Err() != nil:
		// /stop 中断：本轮结果丢弃
		b.reply(staffID, "⏹ 已中断。")
	case err != nil:
		b.reply(staffID, "执行出错："+truncate(err.Error(), 500))
	default:
		if strings.TrimSpace(reply) == "" {
			reply = "✅ 完成（无文本输出）。"
		}
		b.reply(staffID, reply)
	}
}

// ── 发送侧（access_token + cgi-bin/message/send 主动推送） ──

// Send 向指定成员投递文本（超长自动分条）；全部成功返回 true。
func (b *Bot) Send(staffID, text string) bool {
	if !Configured() {
		return false
	}
	ok := true
	for _, chunk := range chunkRunes(text, maxChunkRunes) {
		if !b.sendChunk(staffID, chunk) {
			ok = false
		}
	}
	return ok
}

// SendToUser 按平台用户投递（查绑定表）；未绑定/未配置返回 false（调用方回落）。
func (b *Bot) SendToUser(userID int64, text string) bool {
	link, err := b.repo.GetByUserID(userID)
	if err != nil {
		return false
	}
	return b.Send(link.StaffID, text)
}

// reply 简单文本回复（命令响应等，忽略失败）。
func (b *Bot) reply(staffID, text string) {
	b.Send(staffID, text)
}

// sendChunk 发送单条文本（≤1900 runes）。
func (b *Bot) sendChunk(staffID, text string) bool {
	token, err := b.accessToken()
	if err != nil {
		log.Printf("[agent-wecom] access_token: %v", err)
		return false
	}
	_, _, _, _, agentID := credentials()
	payload := map[string]any{
		"touser":  staffID,
		"msgtype": "text",
		"text":    map[string]string{"content": text},
	}
	if agentID != "" {
		if n, err := strconv.Atoi(agentID); err == nil {
			payload["agentid"] = n
		}
	}
	data, _ := json.Marshal(payload)
	resp, err := b.client.Post(b.restBase()+"/cgi-bin/message/send?access_token="+token,
		"application/json", strings.NewReader(string(data)))
	if err != nil {
		log.Printf("[agent-wecom] message send: %v", err)
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.ErrCode != 0 {
		log.Printf("[agent-wecom] message send: %s", truncate(string(body), 200))
		return false
	}
	return true
}

// accessToken 取 access_token（缓存至 TTL-60s）。
func (b *Bot) accessToken() (string, error) {
	b.mu.Lock()
	if b.token != "" && time.Now().Before(b.tokenExp) {
		defer b.mu.Unlock()
		return b.token, nil
	}
	b.mu.Unlock()

	corpID, secret, _, _, _ := credentials()
	if corpID == "" || secret == "" {
		return "", fmt.Errorf("企业微信凭据未配置")
	}
	resp, err := b.client.Get(fmt.Sprintf("%s/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		b.restBase(), corpID, secret))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.ErrCode != 0 || out.AccessToken == "" {
		return "", fmt.Errorf("access_token 获取失败: errcode=%d errmsg=%s", out.ErrCode, out.ErrMsg)
	}
	ttl := time.Duration(out.ExpiresIn)*time.Second - 60*time.Second
	if ttl <= 0 {
		ttl = time.Minute
	}
	b.mu.Lock()
	b.token = out.AccessToken
	b.tokenExp = time.Now().Add(ttl)
	b.mu.Unlock()
	return out.AccessToken, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// chunkRunes 按 rune 上限切分文本（至少返回一段）。
func chunkRunes(s string, n int) []string {
	r := []rune(s)
	if len(r) == 0 {
		return []string{""}
	}
	out := []string{}
	for i := 0; i < len(r); i += n {
		end := i + n
		if end > len(r) {
			end = len(r)
		}
		out = append(out, string(r[i:end]))
	}
	return out
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}
