// Package agentfeishu 飞书双向通道（webhook 模式）：
// 平台事件回调 → 配对 / 斜杠命令 / headless 执行 → tenant_access_token 回发。
package agentfeishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// maxChunkRunes 单条消息最大 rune 数（飞书文本上限宽松，留 3500 余量）。
const maxChunkRunes = 3500

// dedupeCapacity 事件去重 LRU 容量（event_id）。
const dedupeCapacity = 1000

// RunRequest 入站消息执行请求：会话绑定 / 模型覆盖可选。
type RunRequest struct {
	UserID         int64
	Prompt         string
	ConversationID string // 绑定的会话 id（空 = 无会话）
	Model          string // 模型覆盖（"provider" 或 "provider:model"）
}

// Executor 支持 ctx 中断（/stop）的执行器（main 注入 headless runner）。
type Executor func(ctx context.Context, req *RunRequest) (string, error)

// credentials 解析飞书应用凭据：环境变量优先，回落 config.yaml agent.feishu.*。
func credentials() (appID, appSecret string) {
	appID = strings.TrimSpace(os.Getenv("FEISHU_APP_ID"))
	appSecret = strings.TrimSpace(os.Getenv("FEISHU_APP_SECRET"))
	if appID != "" && appSecret != "" {
		return appID, appSecret
	}
	cfg := store.GetConfig()
	if agent, ok := cfg["agent"].(map[string]any); ok {
		if fs, ok := agent["feishu"].(map[string]any); ok {
			if appID == "" {
				if v, ok := fs["app_id"].(string); ok {
					appID = strings.TrimSpace(v)
				}
			}
			if appSecret == "" {
				if v, ok := fs["app_secret"].(string); ok {
					appSecret = strings.TrimSpace(v)
				}
			}
		}
	}
	return appID, appSecret
}

// Configured 应用凭据是否已配置（未配置 = 通道惰性：status configured=false，webhook 503）。
func Configured() bool {
	id, secret := credentials()
	return id != "" && secret != ""
}

// ── 事件回调结构（只取所需字段） ──

type webhookBody struct {
	// v1 url_verification 握手
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	// v2 事件回调
	Schema string       `json:"schema"`
	Header eventHeader  `json:"header"`
	Event  messageEvent `json:"event"`
}

type eventHeader struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
}

type messageEvent struct {
	Sender struct {
		SenderID struct {
			OpenID string `json:"open_id"`
		} `json:"sender_id"`
		SenderType string `json:"sender_type"`
	} `json:"sender"`
	Message struct {
		MessageID   string `json:"message_id"`
		ChatID      string `json:"chat_id"`
		ChatType    string `json:"chat_type"`
		MessageType string `json:"message_type"`
		Content     string `json:"content"`
	} `json:"message"`
}

// Bot 飞书通道：webhook 路由 + 配对 + 单飞执行 + token 缓存回发。
type Bot struct {
	repo     *Repo
	exec     Executor
	apiBase  string // 可覆盖（测试指向 httptest）
	healthFn func() string
	client   *http.Client

	mu       sync.Mutex
	token    string                        // tenant_access_token 缓存
	tokenExp time.Time                     // 过期时间（TTL-60s 提前刷新）
	runs     map[string]context.CancelFunc // chat_id → 在途 run 取消函数（单飞）
	seen     map[string]struct{}           // event_id 去重
	seenSeq  []string                      // 去重 LRU 顺序
}

// NewBot 构造（凭据每次调用现取，支持 config 热改）。
func NewBot(repo *Repo) *Bot {
	return &Bot{
		repo:    repo,
		apiBase: "https://open.feishu.cn",
		runs:    map[string]context.CancelFunc{},
		seen:    map[string]struct{}{},
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// SetAPIBase 覆盖 API 地址（测试用）。
func (b *Bot) SetAPIBase(base string) { b.apiBase = strings.TrimSuffix(base, "/") }

// SetExecutor 注入执行器。
func (b *Bot) SetExecutor(exec Executor) { b.exec = exec }

// SetHealthProvider 注入 /status 的网关健康行。
func (b *Bot) SetHealthProvider(fn func() string) { b.healthFn = fn }

// HandleWebhook POST /api/agent/feishu/webhook（公开路由，无 JWT）：
// url_verification 回 challenge；event_callback 去重后 200 先回、异步处理。
func (b *Bot) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if !Configured() {
		http.Error(w, `{"detail":"飞书未配置（FEISHU_APP_ID/FEISHU_APP_SECRET）"}`, http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var in webhookBody
	if err := json.Unmarshal(body, &in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// 握手：返回 challenge
	if in.Type == "url_verification" {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"challenge":%q}`, in.Challenge)
		return
	}
	// 事件去重（飞书超时重推同一 event_id）
	if in.Header.EventID != "" && b.markSeen(in.Header.EventID) {
		w.WriteHeader(http.StatusOK)
		return
	}
	// 立即 200，异步处理（飞书要求秒级响应）
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"code":0}`))
	if in.Header.EventType == "im.message.receive_v1" {
		ev := in.Event
		go b.handleMessage(&ev)
	}
}

// markSeen 记录 event_id；已见过返回 true（重复事件）。LRU 容量 1000。
func (b *Bot) markSeen(eventID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.seen[eventID]; ok {
		return true
	}
	b.seen[eventID] = struct{}{}
	b.seenSeq = append(b.seenSeq, eventID)
	if len(b.seenSeq) > dedupeCapacity {
		delete(b.seen, b.seenSeq[0])
		b.seenSeq = b.seenSeq[1:]
	}
	return false
}

// handleMessage 消息路由：仅文本消息；6 位数字 → 配对；未绑定 → 指引；
// 斜杠命令 → handleCommand；已绑定普通消息 → 单飞 headless 执行并回复。
func (b *Bot) handleMessage(ev *messageEvent) {
	msg := ev.Message
	if msg.MessageType != "text" {
		return
	}
	openID := ev.Sender.SenderID.OpenID
	if openID == "" {
		return
	}
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(msg.Content), &content); err != nil {
		return
	}
	// 群聊 @机器人 会带 @_user_1 占位符，剥掉后再路由
	text := strings.TrimSpace(stripMentions(content.Text))
	if text == "" {
		return
	}
	chatID := msg.ChatID

	switch {
	case text == "/help":
		b.reply(chatID, helpText())
		return
	case len(text) == 6 && isDigits(text):
		link, err := b.repo.ConsumePairCode(text, openID, chatID)
		if err != nil {
			b.reply(chatID, "配对码无效或已过期，请在网页端重新生成。")
			return
		}
		b.reply(chatID, fmt.Sprintf("✅ 绑定成功（user_id=%d）。现在直接发消息即可使唤你的交易助手。", link.UserID))
		return
	}

	link, err := b.repo.GetByOpenID(openID)
	if err != nil {
		b.reply(chatID, "你还没有绑定账号。在网页端 助手 → 飞书接入 生成配对码后发给我。")
		return
	}
	// 回发目标跟随最新会话（用户换群/换会话时）
	if link.ChatID != chatID {
		_ = b.repo.SetChatID(openID, chatID)
		link.ChatID = chatID
	}
	if strings.HasPrefix(text, "/") {
		b.handleCommand(chatID, link, text)
		return
	}
	b.runInbound(chatID, link, text)
}

// stripMentions 去掉飞书 @提及占位符（@_user_1 等）。
func stripMentions(s string) string {
	fields := strings.Fields(s)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.HasPrefix(f, "@_user_") {
			continue
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
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
func (b *Bot) handleCommand(chatID string, link *Link, text string) {
	fields := strings.Fields(text)
	cmd := strings.TrimPrefix(fields[0], "/")
	if i := strings.Index(cmd, "@"); i >= 0 { // /cmd@BotName 形式
		cmd = cmd[:i]
	}
	arg := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	openID := link.OpenID

	switch cmd {
	case "new":
		if err := b.repo.SetConversationID(openID, ""); err != nil {
			b.reply(chatID, "操作失败："+truncate(err.Error(), 200))
			return
		}
		b.reply(chatID, "🆕 已开启新会话，下条消息将从头开始。")

	case "stop":
		b.mu.Lock()
		cancel, ok := b.runs[chatID]
		b.mu.Unlock()
		if !ok {
			b.reply(chatID, "当前没有正在执行的任务。")
			return
		}
		cancel()
		b.reply(chatID, "⏹ 已发送中断信号。")

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
		b.reply(chatID, fmt.Sprintf("📊 状态\n绑定账号：%s（user_id=%d）\n模型：%s\n会话绑定：%s\n网关：%s",
			openID, link.UserID, model, conv, health))

	case "model":
		if arg == "" {
			cur := link.Model
			if cur == "" {
				cur = "默认（跟随网关配置）"
			}
			b.reply(chatID, "当前模型："+cur+"\n用 /model <name> 覆盖，/model default 恢复默认。")
			return
		}
		if arg == "default" || arg == "off" || arg == "auto" {
			if err := b.repo.SetModel(openID, ""); err != nil {
				b.reply(chatID, "操作失败："+truncate(err.Error(), 200))
				return
			}
			b.reply(chatID, "已恢复默认模型。")
			return
		}
		if err := b.repo.SetModel(openID, arg); err != nil {
			b.reply(chatID, "操作失败："+truncate(err.Error(), 200))
			return
		}
		b.reply(chatID, "✅ 后续使用模型："+arg)

	case "help":
		b.reply(chatID, helpText())

	default:
		b.reply(chatID, "未知命令 /"+cmd+"，/help 查看可用命令。")
	}
}

// runInbound 绑定用户普通消息：单飞执行 → 回发（超长分条）。
func (b *Bot) runInbound(chatID string, link *Link, text string) {
	// 单飞：同 chat 并发消息直接提示
	b.mu.Lock()
	if _, busy := b.runs[chatID]; busy {
		b.mu.Unlock()
		b.reply(chatID, "正在处理上一条消息，/stop 可中断。")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.runs[chatID] = cancel
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.runs, chatID)
		b.mu.Unlock()
		cancel()
	}()

	if b.exec == nil {
		b.reply(chatID, "执行器未配置，请联系管理员。")
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
		b.reply(chatID, "⏹ 已中断。")
	case err != nil:
		b.reply(chatID, "执行出错："+truncate(err.Error(), 500))
	default:
		if strings.TrimSpace(reply) == "" {
			reply = "✅ 完成（无文本输出）。"
		}
		b.reply(chatID, reply)
	}
}

// ── 发送侧（tenant_access_token + im/v1/messages） ──

// Send 向指定 chat 投递文本（超长自动分条）；全部成功返回 true。
func (b *Bot) Send(chatID, text string) bool {
	if !Configured() {
		return false
	}
	ok := true
	for _, chunk := range chunkRunes(text, maxChunkRunes) {
		if !b.sendChunk(chatID, chunk) {
			ok = false
		}
	}
	return ok
}

// SendToUser 按平台用户投递（查绑定表）；未绑定/未配置返回 false（调用方回落）。
func (b *Bot) SendToUser(userID int64, text string) bool {
	link, err := b.repo.GetByUserID(userID)
	if err != nil || link.ChatID == "" {
		return false
	}
	return b.Send(link.ChatID, text)
}

// reply 简单文本回复（命令响应等，忽略失败）。
func (b *Bot) reply(chatID, text string) {
	b.Send(chatID, text)
}

// sendChunk 发送单条文本（≤3500 runes）。
func (b *Bot) sendChunk(chatID, text string) bool {
	token, err := b.tenantToken()
	if err != nil {
		log.Printf("[agent-feishu] tenant_access_token: %v", err)
		return false
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	payload, _ := json.Marshal(map[string]any{
		"receive_id": chatID,
		"msg_type":   "text",
		"content":    string(content),
	})
	req, err := http.NewRequest(http.MethodPost,
		b.apiBase+"/open-apis/im/v1/messages?receive_id_type=chat_id",
		strings.NewReader(string(payload)))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := b.client.Do(req)
	if err != nil {
		log.Printf("[agent-feishu] send message: %v", err)
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Code != 0 {
		log.Printf("[agent-feishu] send message: code=%d msg=%s", out.Code, truncate(string(body), 200))
		return false
	}
	return true
}

// tenantToken 取 tenant_access_token（缓存至 TTL-60s）。
func (b *Bot) tenantToken() (string, error) {
	b.mu.Lock()
	if b.token != "" && time.Now().Before(b.tokenExp) {
		defer b.mu.Unlock()
		return b.token, nil
	}
	b.mu.Unlock()

	appID, appSecret := credentials()
	if appID == "" || appSecret == "" {
		return "", fmt.Errorf("飞书应用凭据未配置")
	}
	payload, _ := json.Marshal(map[string]string{"app_id": appID, "app_secret": appSecret})
	resp, err := b.client.Post(b.apiBase+"/open-apis/auth/v3/tenant_access_token/internal",
		"application/json", strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.Code != 0 || out.TenantAccessToken == "" {
		return "", fmt.Errorf("tenant_access_token 获取失败: code=%d msg=%s", out.Code, out.Msg)
	}
	ttl := time.Duration(out.Expire)*time.Second - 60*time.Second
	if ttl <= 0 {
		ttl = time.Minute
	}
	b.mu.Lock()
	b.token = out.TenantAccessToken
	b.tokenExp = time.Now().Add(ttl)
	b.mu.Unlock()
	return out.TenantAccessToken, nil
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
