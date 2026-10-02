package agenttelegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// Executor 入站消息执行器（headless agent runner，由 main 注入；旧式一次性回复）。
type Executor func(userID int64, prompt string) (string, error)

// RunRequest 流式执行请求：会话绑定 / 模型覆盖 / 增量回调均可选。
type RunRequest struct {
	UserID         int64
	Prompt         string
	ConversationID string      // handoff 绑定的会话 id（空 = 无会话）
	Model          string      // 每 chat 模型覆盖（"provider" 或 "provider:model"）
	OnDelta        func(string) // 文本分片回调（流式回复）
}

// StreamExecutor 支持 ctx 中断（/stop）与流式增量的执行器（优先于 Executor）。
type StreamExecutor func(ctx context.Context, req *RunRequest) (string, error)

// tgUpdate / tgMessage Telegram getUpdates 响应结构（只取所需字段）。
type tgUpdate struct {
	UpdateID int64     `json:"update_id"`
	Message  tgMessage `json:"message"`
}

type tgMessage struct {
	Chat tgChat `json:"chat"`
	From tgFrom `json:"from"`
	Text string `json:"text"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

type tgFrom struct {
	Username string `json:"username"`
}

// maxChunkRunes 单条消息最大 rune 数（TG 上限 4096，留余量）。
const maxChunkRunes = 3900

// Bot 入站长轮询机器人：配对 + 斜杠命令 + 绑定用户消息 → 流式执行 → 节流编辑回复。
type Bot struct {
	token   string
	apiBase string // 可覆盖（测试指向 httptest）
	repo    *Repo
	exec    Executor
	sexec   StreamExecutor
	// healthFn /status 附加的网关健康一行（main 注入，可为 nil）
	healthFn func() string
	// editThrottle 流式回复的编辑间隔（测试可调小）
	editThrottle time.Duration
	startedAt    time.Time

	mu     sync.Mutex
	offset int64
	models map[int64]string              // chat → 模型覆盖
	runs   map[int64]context.CancelFunc  // chat → 在途 run 取消函数（单飞）
	stopCh chan struct{}
	once   sync.Once
	client *http.Client
}

// NewBot 构造；token 为空时 Start 直接返回（未配置）。
func NewBot(token string, repo *Repo, exec Executor) *Bot {
	return &Bot{
		token:        token,
		apiBase:      "https://api.telegram.org",
		repo:         repo,
		exec:         exec,
		editThrottle: 1500 * time.Millisecond,
		startedAt:    time.Now(),
		models:       map[int64]string{},
		runs:         map[int64]context.CancelFunc{},
		stopCh:       make(chan struct{}),
		client:       &http.Client{Timeout: 45 * time.Second},
	}
}

// SetAPIBase 覆盖 API 地址（测试用）。
func (b *Bot) SetAPIBase(base string) { b.apiBase = base }

// SetExecutor 注入执行器（旧式一次性回复；未注入 StreamExecutor 时回落使用）。
func (b *Bot) SetExecutor(exec Executor) { b.exec = exec }

// SetStreamExecutor 注入流式执行器（支持 ctx 中断 / 会话绑定 / 模型覆盖）。
func (b *Bot) SetStreamExecutor(exec StreamExecutor) { b.sexec = exec }

// SetHealthProvider 注入 /status 的网关健康行。
func (b *Bot) SetHealthProvider(fn func() string) { b.healthFn = fn }

// SetEditThrottle 覆盖流式编辑间隔（测试用）。
func (b *Bot) SetEditThrottle(d time.Duration) { b.editThrottle = d }

// Start 启动轮询（幂等；未配置 token 时仅记日志）。
func (b *Bot) Start() {
	if b.token == "" {
		log.Printf("[agent-telegram] TELEGRAM_BOT_TOKEN 未配置，入站通道未启动")
		return
	}
	if b.exec == nil && b.sexec == nil {
		log.Printf("[agent-telegram] 执行器未注入，入站通道未启动")
		return
	}
	go b.once.Do(b.loop)
}

// Stop 停止轮询。
func (b *Bot) Stop() { close(b.stopCh) }

func (b *Bot) loop() {
	log.Printf("[agent-telegram] 入站轮询已启动")
	for {
		select {
		case <-b.stopCh:
			return
		default:
		}
		updates, err := b.getUpdates()
		if err != nil {
			log.Printf("[agent-telegram] getUpdates: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		for _, u := range updates {
			b.mu.Lock()
			if u.UpdateID >= b.offset {
				b.offset = u.UpdateID + 1
			}
			b.mu.Unlock()
			if u.Message.Text != "" {
				go b.handleMessage(u.Message)
			}
		}
	}
}

func (b *Bot) getUpdates() ([]tgUpdate, error) {
	endpoint := fmt.Sprintf("%s/bot%s/getUpdates?offset=%d&timeout=30", b.apiBase, b.token, b.offset)
	resp, err := b.client.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		OK     bool       `json:"ok"`
		Result []tgUpdate `json:"result"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("telegram api error: %s", truncate(string(body), 200))
	}
	return out.Result, nil
}

// handleMessage 消息路由：/start → 指引；/help → 命令清单；6 位数字 → 配对；
// 其余斜杠命令 → handleCommand；已绑定普通消息 → 流式执行并回复。
func (b *Bot) handleMessage(m tgMessage) {
	text := strings.TrimSpace(m.Text)
	switch {
	case text == "/start":
		b.reply(m.Chat.ID, "小天量化助手已就绪。\n1) 在网页端 助手 → Telegram 接入 生成配对码\n2) 把 6 位配对码发给我即可完成绑定\n绑定后直接发消息即可使唤你的交易助手。")
		return
	case text == "/help":
		b.reply(m.Chat.ID, helpText())
		return
	case len(text) == 6 && isDigits(text):
		link, err := b.repo.ConsumePairCode(text, m.Chat.ID, m.From.Username)
		if err != nil {
			b.reply(m.Chat.ID, "配对码无效或已过期，请在网页端重新生成。")
			return
		}
		b.reply(m.Chat.ID, fmt.Sprintf("✅ 绑定成功，@%s。现在直接发消息即可。", link.TelegramUsername))
		return
	}

	link, err := b.repo.GetByChatID(m.Chat.ID)
	if err != nil {
		b.reply(m.Chat.ID, "你还没有绑定账号。在网页端 助手 → Telegram 接入 生成配对码后发给我。")
		return
	}
	// 模型覆盖以绑定表为准：内存缓存缺失时从持久化值恢复（进程重启场景）。
	b.mu.Lock()
	if _, ok := b.models[m.Chat.ID]; !ok && link.Model != "" {
		b.models[m.Chat.ID] = link.Model
	}
	b.mu.Unlock()
	if strings.HasPrefix(text, "/") {
		b.handleCommand(m, link, text)
		return
	}
	b.runInbound(m.Chat.ID, link, text)
}

func helpText() string {
	return "可用命令：\n" +
		"/new — 开启新会话（清除会话绑定）\n" +
		"/stop — 中断当前正在执行的任务\n" +
		"/status — 绑定状态 / 模型 / 网关健康\n" +
		"/model <name> — 设置本聊天的模型覆盖（/model 查看，/model default 恢复默认）\n" +
		"/handoff <code> — 接管网页端会话（在网页端会话菜单生成移交码）\n" +
		"/help — 本清单"
}

// handleCommand 已绑定 chat 的斜杠命令分发。
func (b *Bot) handleCommand(m tgMessage, link *Link, text string) {
	fields := strings.Fields(text)
	cmd := strings.TrimPrefix(fields[0], "/")
	if i := strings.Index(cmd, "@"); i >= 0 { // /cmd@BotName 形式
		cmd = cmd[:i]
	}
	arg := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	chatID := m.Chat.ID

	switch cmd {
	case "new":
		if err := b.repo.SetConversationID(chatID, ""); err != nil {
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
		b.reply(chatID, b.statusText(chatID, link))

	case "model":
		if arg == "" {
			cur := b.modelFor(chatID)
			if cur == "" {
				cur = "默认（跟随网关配置）"
			}
			b.reply(chatID, "当前模型："+cur+"\n用 /model <name> 覆盖，/model default 恢复默认。")
			return
		}
		if arg == "default" || arg == "off" || arg == "auto" {
			if err := b.repo.SetModel(chatID, ""); err != nil {
				b.reply(chatID, "操作失败："+truncate(err.Error(), 200))
				return
			}
			b.mu.Lock()
			delete(b.models, chatID)
			b.mu.Unlock()
			b.reply(chatID, "已恢复默认模型。")
			return
		}
		if err := b.repo.SetModel(chatID, arg); err != nil {
			b.reply(chatID, "操作失败："+truncate(err.Error(), 200))
			return
		}
		b.mu.Lock()
		b.models[chatID] = arg
		b.mu.Unlock()
		b.reply(chatID, "✅ 本聊天后续使用模型："+arg)

	case "handoff":
		if len(arg) != 6 || !isDigits(arg) {
			b.reply(chatID, "用法：/handoff <6 位移交码>（在网页端会话菜单生成）")
			return
		}
		convID, err := b.repo.ConsumeHandoffCode(arg, chatID)
		if err == ErrNotLinked {
			b.reply(chatID, "该移交码不属于你绑定的账号。")
			return
		}
		if err != nil {
			b.reply(chatID, "移交码无效或已过期，请在网页端重新生成。")
			return
		}
		title := convID
		if rec, err := store.DefaultAgentChatRepo().GetConversation(convID); err == nil && rec != nil && rec.Title != "" {
			title = rec.Title
		}
		b.reply(chatID, fmt.Sprintf("✅ 已接管会话「%s」，后续消息将在该会话中续跑；/new 可退出。", title))

	default:
		b.reply(chatID, "未知命令 /"+cmd+"，/help 查看可用命令。")
	}
}

// statusText /status 输出：绑定 + 模型覆盖 + 会话绑定 + 网关健康。
func (b *Bot) statusText(chatID int64, link *Link) string {
	model := b.modelFor(chatID)
	if model == "" {
		model = "默认（跟随网关配置）"
	}
	conv := "未绑定"
	if link.ConversationID != "" {
		conv = link.ConversationID
		if rec, err := store.DefaultAgentChatRepo().GetConversation(link.ConversationID); err == nil && rec != nil && rec.Title != "" {
			conv = fmt.Sprintf("「%s」(%s)", rec.Title, link.ConversationID)
		}
	}
	health := "运行中"
	if b.healthFn != nil {
		health = b.healthFn()
	}
	return fmt.Sprintf("📊 状态\n绑定账号：@%s（user_id=%d）\n模型：%s\n会话绑定：%s\n网关：%s",
		link.TelegramUsername, link.UserID, model, conv, health)
}

// modelFor 取本 chat 的模型覆盖（空 = 默认）。
func (b *Bot) modelFor(chatID int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.models[chatID]
}

// runInbound 绑定用户普通消息：单飞执行 → 流式回复（初条 + 节流编辑 + 完结编辑/分条）。
func (b *Bot) runInbound(chatID int64, link *Link, text string) {
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

	// 旧式执行器回落（未装配流式执行器时保持原有一次性回复行为）
	if b.sexec == nil {
		if b.exec == nil {
			b.reply(chatID, "执行器未配置，请联系管理员。")
			return
		}
		reply, err := b.exec(link.UserID, text)
		if err != nil {
			b.reply(chatID, "执行出错："+truncate(err.Error(), 500))
			return
		}
		b.reply(chatID, truncate(reply, maxChunkRunes))
		return
	}

	req := &RunRequest{
		UserID:         link.UserID,
		Prompt:         text,
		ConversationID: link.ConversationID,
		Model:          b.modelFor(chatID),
	}

	msgID, hasPlaceholder := b.sendMessage(chatID, "思考中…")

	// 增量缓冲：执行器回调追加，编辑 goroutine 节流快照
	var bufMu sync.Mutex
	var buf strings.Builder
	lastEdited := ""
	req.OnDelta = func(delta string) {
		bufMu.Lock()
		buf.WriteString(delta)
		bufMu.Unlock()
	}

	done := make(chan struct{})
	if hasPlaceholder {
		go func() {
			ticker := time.NewTicker(b.editThrottle)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					bufMu.Lock()
					snapshot := buf.String()
					changed := snapshot != lastEdited
					bufMu.Unlock()
					if snapshot != "" && changed {
						b.editMessage(chatID, msgID, truncateRunes(snapshot, maxChunkRunes))
						bufMu.Lock()
						lastEdited = snapshot
						bufMu.Unlock()
					}
				}
			}
		}()
	}

	reply, err := b.sexec(ctx, req)
	close(done)

	switch {
	case ctx.Err() != nil:
		// /stop 中断：本轮结果丢弃
		b.finishText(chatID, msgID, hasPlaceholder, "⏹ 已中断。")
	case err != nil:
		b.finishText(chatID, msgID, hasPlaceholder, "执行出错："+truncate(err.Error(), 500))
	default:
		if strings.TrimSpace(reply) == "" {
			reply = "✅ 完成（无文本输出）。"
		}
		chunks := chunkRunes(reply, maxChunkRunes)
		b.finishText(chatID, msgID, hasPlaceholder, chunks[0])
		for _, rest := range chunks[1:] {
			b.Send(chatID, rest)
		}
	}
}

// finishText 完结一条流式回复：有占位消息则编辑之，否则新发一条。
func (b *Bot) finishText(chatID int64, msgID int, hasPlaceholder bool, text string) {
	if hasPlaceholder {
		b.editMessage(chatID, msgID, text)
		return
	}
	b.Send(chatID, text)
}

// ── Bot API 发送侧 ──

// Send 向指定 chat 投递文本（超长自动分条），供 cron 结果投递；全部成功返回 true。
func (b *Bot) Send(chatID int64, text string) bool {
	if b.token == "" {
		return false
	}
	ok := true
	for _, chunk := range chunkRunes(text, maxChunkRunes) {
		if _, sent := b.sendMessage(chatID, chunk); !sent {
			ok = false
		}
	}
	return ok
}

// SendToUser 按平台用户投递（查绑定表）；未绑定或 token 未配置返回 false（调用方回落）。
func (b *Bot) SendToUser(userID int64, text string) bool {
	link, err := b.repo.GetByUserID(userID)
	if err != nil {
		return false
	}
	return b.Send(link.TelegramChatID, text)
}

// reply 简单文本回复（命令响应等，忽略失败）。
func (b *Bot) reply(chatID int64, text string) {
	b.Send(chatID, text)
}

// sendMessage 发送一条消息，返回 message_id 与是否成功。
func (b *Bot) sendMessage(chatID int64, text string) (int, bool) {
	payload := map[string]any{"chat_id": chatID, "text": text}
	var out struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	if err := b.post("sendMessage", payload, &out); err != nil {
		log.Printf("[agent-telegram] sendMessage: %v", err)
		return 0, false
	}
	return out.Result.MessageID, out.OK
}

// editMessage 编辑既有消息文本（流式回复用；失败静默——内容未变时 TG 会报 400）。
func (b *Bot) editMessage(chatID int64, messageID int, text string) {
	payload := map[string]any{"chat_id": chatID, "message_id": messageID, "text": text}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := b.post("editMessageText", payload, &out); err != nil {
		log.Printf("[agent-telegram] editMessageText: %v", err)
	}
}

// post 调 Bot API 并解析响应到 out。
func (b *Bot) post(method string, payload map[string]any, out any) error {
	data, _ := json.Marshal(payload)
	endpoint := fmt.Sprintf("%s/bot%s/%s", b.apiBase, b.token, method)
	resp, err := b.client.Post(endpoint, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return json.Unmarshal(body, out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// truncateRunes 按 rune 截断（避免切断多字节字符）。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
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
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}
