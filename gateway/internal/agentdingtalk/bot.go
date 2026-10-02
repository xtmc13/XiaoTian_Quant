// Package agentdingtalk 钉钉双向通道（outgoing webhook 模式）：
// 群机器人 outgoing 回调（HMAC-SHA256 验签）→ 配对 / 斜杠命令 / headless 执行
// → 回调携带的 sessionWebhook 回发。
package agentdingtalk

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// maxChunkRunes 单条消息最大 rune 数（钉钉 Markdown/文本上限保守取 3000）。
const maxChunkRunes = 3000

// RunRequest 入站消息执行请求：会话绑定 / 模型覆盖可选。
type RunRequest struct {
	UserID         int64
	Prompt         string
	ConversationID string // 绑定的会话 id（空 = 无会话）
	Model          string // 模型覆盖（"provider" 或 "provider:model"）
}

// Executor 支持 ctx 中断（/stop）的执行器（main 注入 headless runner）。
type Executor func(ctx context.Context, req *RunRequest) (string, error)

// credentials 解析钉钉应用凭据：环境变量优先，回落 config.yaml agent.dingtalk.*。
func credentials() (clientID, clientSecret string) {
	clientID = strings.TrimSpace(os.Getenv("DINGTALK_CLIENT_ID"))
	clientSecret = strings.TrimSpace(os.Getenv("DINGTALK_CLIENT_SECRET"))
	if clientID != "" && clientSecret != "" {
		return clientID, clientSecret
	}
	cfg := store.GetConfig()
	if agent, ok := cfg["agent"].(map[string]any); ok {
		if dt, ok := agent["dingtalk"].(map[string]any); ok {
			if clientID == "" {
				if v, ok := dt["client_id"].(string); ok {
					clientID = strings.TrimSpace(v)
				}
			}
			if clientSecret == "" {
				if v, ok := dt["client_secret"].(string); ok {
					clientSecret = strings.TrimSpace(v)
				}
			}
		}
	}
	return clientID, clientSecret
}

// Configured 应用凭据是否已配置（未配置 = 通道惰性：status configured=false，webhook 503）。
func Configured() bool {
	id, secret := credentials()
	return id != "" && secret != ""
}

// incoming outgoing 回调消息体（只取所需字段）。
type incoming struct {
	Msgtype        string `json:"msgtype"`
	ConversationID string `json:"conversationId"`
	MsgID          string `json:"msgId"`
	SenderNick     string `json:"senderNick"`
	SenderStaffID  string `json:"senderStaffId"`
	SessionWebhook string `json:"sessionWebhook"`
	Text           struct {
		Content string `json:"content"`
	} `json:"text"`
}

// Bot 钉钉通道：outgoing webhook 验签路由 + 配对 + 单飞执行 + sessionWebhook 回发。
type Bot struct {
	repo     *Repo
	exec     Executor
	healthFn func() string
	client   *http.Client

	mu    sync.Mutex
	runs  map[string]context.CancelFunc // conversation_id → 在途 run 取消函数（单飞）
	hooks map[string]string             // staff_id → 最近一次 sessionWebhook（cron 直发用）
}

// NewBot 构造（凭据每次调用现取，支持 config 热改）。
func NewBot(repo *Repo) *Bot {
	return &Bot{
		repo:   repo,
		runs:   map[string]context.CancelFunc{},
		hooks:  map[string]string{},
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetExecutor 注入执行器。
func (b *Bot) SetExecutor(exec Executor) { b.exec = exec }

// SetHealthProvider 注入 /status 的网关健康行。
func (b *Bot) SetHealthProvider(fn func() string) { b.healthFn = fn }

// verifySign 校验 outgoing 回调签名：sign = urlEncode(base64(HMAC-SHA256(timestamp+"\n"+secret)))。
// secret 未配置时容忍缺签（开发联调）；配置了就必须验签通过。
func verifySign(timestamp, sign, secret string) bool {
	if secret == "" {
		return true
	}
	if timestamp == "" || sign == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "\n" + secret))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	// 回调方对 base64 做了 URL 编码；两种形态都接受
	if sign == expected || sign == url.QueryEscape(expected) {
		return true
	}
	if unescaped, err := url.QueryUnescape(sign); err == nil && unescaped == expected {
		return true
	}
	return false
}

// HandleWebhook POST /api/agent/dingtalk/webhook（公开路由，无 JWT）：
// 验签 → 仅 text → 200 先回、异步处理。
func (b *Bot) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if !Configured() {
		http.Error(w, `{"detail":"钉钉未配置（DINGTALK_CLIENT_ID/DINGTALK_CLIENT_SECRET）"}`, http.StatusServiceUnavailable)
		return
	}
	_, secret := credentials()
	if !verifySign(r.Header.Get("timestamp"), r.Header.Get("sign"), secret) {
		http.Error(w, `{"detail":"签名验证失败"}`, http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var in incoming
	if err := json.Unmarshal(body, &in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// 立即 200，异步处理（钉钉 outgoing 要求秒级响应）
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
	if in.Msgtype == "text" {
		go b.handleMessage(&in)
	}
}

// handleMessage 消息路由：6 位数字 → 配对；未绑定 → 指引；斜杠命令 → handleCommand；
// 已绑定普通消息 → 单飞 headless 执行并经 sessionWebhook 回复。
func (b *Bot) handleMessage(in *incoming) {
	staffID := in.SenderStaffID
	if staffID == "" || in.SessionWebhook == "" {
		return
	}
	text := strings.TrimSpace(in.Text.Content)
	if text == "" {
		return
	}
	// 记录最近 sessionWebhook（cron 每用户直发的回发通道）
	b.mu.Lock()
	b.hooks[staffID] = in.SessionWebhook
	b.mu.Unlock()

	switch {
	case text == "/help":
		b.reply(in.SessionWebhook, helpText())
		return
	case len(text) == 6 && isDigits(text):
		link, err := b.repo.ConsumePairCode(text, staffID)
		if err != nil {
			b.reply(in.SessionWebhook, "配对码无效或已过期，请在网页端重新生成。")
			return
		}
		b.reply(in.SessionWebhook, fmt.Sprintf("✅ 绑定成功，%s（user_id=%d）。现在直接发消息即可使唤你的交易助手。", in.SenderNick, link.UserID))
		return
	}

	link, err := b.repo.GetByStaffID(staffID)
	if err != nil {
		b.reply(in.SessionWebhook, "你还没有绑定账号。在网页端 助手 → 钉钉接入 生成配对码后发给我。")
		return
	}
	if strings.HasPrefix(text, "/") {
		b.handleCommand(in.SessionWebhook, in.ConversationID, link, text)
		return
	}
	b.runInbound(in.SessionWebhook, in.ConversationID, link, text)
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
func (b *Bot) handleCommand(webhook, convID string, link *Link, text string) {
	fields := strings.Fields(text)
	cmd := strings.TrimPrefix(fields[0], "/")
	arg := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	staffID := link.StaffID

	switch cmd {
	case "new":
		if err := b.repo.SetConversationID(staffID, ""); err != nil {
			b.reply(webhook, "操作失败："+truncate(err.Error(), 200))
			return
		}
		b.reply(webhook, "🆕 已开启新会话，下条消息将从头开始。")

	case "stop":
		b.mu.Lock()
		cancel, ok := b.runs[convID]
		b.mu.Unlock()
		if !ok {
			b.reply(webhook, "当前没有正在执行的任务。")
			return
		}
		cancel()
		b.reply(webhook, "⏹ 已发送中断信号。")

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
		b.reply(webhook, fmt.Sprintf("📊 状态\n绑定账号：%s（user_id=%d）\n模型：%s\n会话绑定：%s\n网关：%s",
			staffID, link.UserID, model, conv, health))

	case "model":
		if arg == "" {
			cur := link.Model
			if cur == "" {
				cur = "默认（跟随网关配置）"
			}
			b.reply(webhook, "当前模型："+cur+"\n用 /model <name> 覆盖，/model default 恢复默认。")
			return
		}
		if arg == "default" || arg == "off" || arg == "auto" {
			if err := b.repo.SetModel(staffID, ""); err != nil {
				b.reply(webhook, "操作失败："+truncate(err.Error(), 200))
				return
			}
			b.reply(webhook, "已恢复默认模型。")
			return
		}
		if err := b.repo.SetModel(staffID, arg); err != nil {
			b.reply(webhook, "操作失败："+truncate(err.Error(), 200))
			return
		}
		b.reply(webhook, "✅ 后续使用模型："+arg)

	case "help":
		b.reply(webhook, helpText())

	default:
		b.reply(webhook, "未知命令 /"+cmd+"，/help 查看可用命令。")
	}
}

// runInbound 绑定用户普通消息：单飞执行（按 conversation 单飞）→ 回发（超长分条）。
func (b *Bot) runInbound(webhook, convID string, link *Link, text string) {
	key := convID
	if key == "" {
		key = link.StaffID
	}
	// 单飞：同会话并发消息直接提示
	b.mu.Lock()
	if _, busy := b.runs[key]; busy {
		b.mu.Unlock()
		b.reply(webhook, "正在处理上一条消息，/stop 可中断。")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.runs[key] = cancel
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.runs, key)
		b.mu.Unlock()
		cancel()
	}()

	if b.exec == nil {
		b.reply(webhook, "执行器未配置，请联系管理员。")
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
		b.reply(webhook, "⏹ 已中断。")
	case err != nil:
		b.reply(webhook, "执行出错："+truncate(err.Error(), 500))
	default:
		if strings.TrimSpace(reply) == "" {
			reply = "✅ 完成（无文本输出）。"
		}
		b.reply(webhook, reply)
	}
}

// ── 发送侧（sessionWebhook 回发） ──

// SendToUser 按平台用户投递（查绑定表 + 最近 sessionWebhook）；未绑定 /
// 无可用回发通道返回 false（调用方回落全局 notify）。
func (b *Bot) SendToUser(userID int64, text string) bool {
	if !Configured() {
		return false
	}
	link, err := b.repo.GetByUserID(userID)
	if err != nil {
		return false
	}
	b.mu.Lock()
	webhook := b.hooks[link.StaffID]
	b.mu.Unlock()
	if webhook == "" {
		return false
	}
	return b.Send(webhook, text)
}

// Send 经 sessionWebhook 投递文本（超长自动分条）；全部成功返回 true。
func (b *Bot) Send(webhook, text string) bool {
	ok := true
	for _, chunk := range chunkRunes(text, maxChunkRunes) {
		if !b.sendChunk(webhook, chunk) {
			ok = false
		}
	}
	return ok
}

// reply 简单文本回复（命令响应等，忽略失败）。
func (b *Bot) reply(webhook, text string) {
	b.Send(webhook, text)
}

// sendChunk 发送单条文本（≤3000 runes）。
func (b *Bot) sendChunk(webhook, text string) bool {
	payload, _ := json.Marshal(map[string]any{
		"msgtype": "text",
		"text":    map[string]string{"content": text},
	})
	resp, err := b.client.Post(webhook, "application/json", strings.NewReader(string(payload)))
	if err != nil {
		log.Printf("[agent-dingtalk] sessionWebhook send: %v", err)
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	// 钉钉自定义机器人回 {"errcode":0}；outgoing sessionWebhook 部分场景返回空体
	var out struct {
		ErrCode int `json:"errcode"`
	}
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &out); err == nil && out.ErrCode != 0 {
			log.Printf("[agent-dingtalk] sessionWebhook send: %s", truncate(string(body), 200))
			return false
		}
	}
	return resp.StatusCode < 400
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
