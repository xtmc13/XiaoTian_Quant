// Package agentqq QQ 双向通道（官方开放平台 WebSocket 网关模式）：
// WSS 长连接收事件（C2C / 群 AT / 频道 AT）→ 配对 / 斜杠命令 / headless 执行
// → app access token + 被动回复（msg_id）。
//
// 断线重连：指数退避最多 5 次后放弃并记日志（不再自动拉起；重启网关或
// 重新装配执行器后恢复，避免进程内残留一个静默死掉的连接 goroutine）。
package agentqq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// maxChunkRunes 单条消息最大 rune 数（QQ 文本上限 2000，留满额）。
const maxChunkRunes = 2000

// 网关 op 码。
const (
	opDispatch         = 0
	opHeartbeat        = 1
	opIdentify         = 2
	opResume           = 6
	opReconnect        = 7
	opInvalidSession   = 9
	opHello            = 10
	opHeartbeatACK     = 11
	maxReconnectTries  = 5
	defaultHTTPTimeout = 30 * time.Second
)

// intents 订阅：1<<25 = GROUP_AND_C2C_MESSAGE，1<<30 = PUBLIC_GUILD_MESSAGES。
const intents = 1<<25 | 1<<30

// RunRequest 入站消息执行请求：会话绑定 / 模型覆盖可选。
type RunRequest struct {
	UserID         int64
	Prompt         string
	ConversationID string // 绑定的会话 id（空 = 无会话）
	Model          string // 模型覆盖（"provider" 或 "provider:model"）
}

// Executor 支持 ctx 中断（/stop）的执行器（main 注入 headless runner）。
type Executor func(ctx context.Context, req *RunRequest) (string, error)

// credentials 解析 QQ 开放平台凭据（环境变量）。
func credentials() (appID, appSecret string) {
	return strings.TrimSpace(os.Getenv("QQ_APP_ID")), strings.TrimSpace(os.Getenv("QQ_APP_SECRET"))
}

// Configured 应用凭据是否已配置（未配置 = 通道惰性：status configured=false，WS 不启动）。
func Configured() bool {
	id, secret := credentials()
	return id != "" && secret != ""
}

// defaultAPIBase REST 域名（QQ_SANDBOX=1 切沙箱）。
func defaultAPIBase() string {
	if os.Getenv("QQ_SANDBOX") == "1" || strings.EqualFold(os.Getenv("QQ_SANDBOX"), "true") {
		return "https://sandbox.api.sgroup.qq.com"
	}
	return "https://api.sgroup.qq.com"
}

// ── 网关帧与事件结构（只取所需字段） ──

type wsPayload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  int64           `json:"s"`
	T  string          `json:"t"`
}

type helloData struct {
	HeartbeatInterval int64 `json:"heartbeat_interval"`
}

type readyData struct {
	SessionID string `json:"session_id"`
}

// inboundMsg 归一化的入站文本消息（三种事件源统一）。
type inboundMsg struct {
	openID   string // 发送者：c2c=user_openid，group=member_openid，channel=author.id
	chatType string // c2c / group / channel
	chatID   string // 回发目标：c2c=openID，group=group_openid，channel=channel_id
	msgID    string // 事件消息 id（被动回复必须携带）
	text     string
}

// flexInt 兼容 QQ 返回的数字/字符串混合（expires_in 为字符串）。
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*f = flexInt(n)
	return nil
}

// Bot QQ 通道：WSS 事件循环 + 配对 + 单飞执行 + token 缓存回发。
type Bot struct {
	repo     *Repo
	exec     Executor
	healthFn func() string
	client   *http.Client

	// 可注入端点（测试指向 httptest；空 = 按环境变量取默认值）
	apiBase  string
	tokenURL string
	wsURL    string // 非空时跳过 GET /gateway 直接拨号

	stopCh    chan struct{}
	stopOnce  sync.Once
	startOnce sync.Once

	mu       sync.Mutex
	token    string                        // app access token 缓存
	tokenExp time.Time                     // 过期时间（TTL-60s 提前刷新）
	runs     map[string]context.CancelFunc // open_id → 在途 run 取消函数（单飞）
	session  string                        // WSS session_id（断线 resume 用）
	lastSeq  int64                         // 已收事件最大序号（resume 用）
	conn     *websocket.Conn               // 当前连接（Stop 时关闭以解除读阻塞）
}

// NewBot 构造（凭据每次调用现取，支持环境热改）。
func NewBot(repo *Repo) *Bot {
	return &Bot{
		repo:   repo,
		runs:   map[string]context.CancelFunc{},
		stopCh: make(chan struct{}),
		client: &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// SetAPIBase 覆盖 REST 域名（测试用）。
func (b *Bot) SetAPIBase(base string) { b.apiBase = strings.TrimSuffix(base, "/") }

// SetTokenURL 覆盖 app access token 接口（测试用）。
func (b *Bot) SetTokenURL(u string) { b.tokenURL = u }

// SetWSURL 覆盖 WSS 地址（测试用；设置后跳过 GET /gateway 发现）。
func (b *Bot) SetWSURL(u string) { b.wsURL = u }

// SetExecutor 注入执行器。
func (b *Bot) SetExecutor(exec Executor) { b.exec = exec }

// SetHealthProvider 注入 /status 的网关健康行。
func (b *Bot) SetHealthProvider(fn func() string) { b.healthFn = fn }

// Start 启动 WSS 事件循环（幂等；未配置凭据时仅记日志，不影响网关启动）。
func (b *Bot) Start() {
	if !Configured() {
		log.Printf("[agent-qq] QQ_APP_ID/QQ_APP_SECRET 未配置，入站通道未启动")
		return
	}
	go b.startOnce.Do(b.loop)
}

// Stop 停止事件循环（关闭当前连接解除读阻塞）。
func (b *Bot) Stop() {
	b.stopOnce.Do(func() { close(b.stopCh) })
	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

// loop 断线重连外层：指数退避，连续失败 maxReconnectTries 次后放弃。
func (b *Bot) loop() {
	log.Printf("[agent-qq] WSS 事件循环已启动")
	backoff := time.Second
	tries := 0
	for {
		select {
		case <-b.stopCh:
			return
		default:
		}
		err := b.serve()
		if errors.Is(err, errStopped) {
			return
		}
		tries++
		log.Printf("[agent-qq] WSS 连接断开（第 %d/%d 次）: %v", tries, maxReconnectTries, err)
		if tries >= maxReconnectTries {
			log.Printf("[agent-qq] 重连 %d 次均失败，入站通道挂起（重启网关后恢复）", tries)
			return
		}
		timer := time.NewTimer(backoff)
		select {
		case <-b.stopCh:
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

var errStopped = errors.New("bot stopped")

// serve 单次连接生命周期：hello → identify/resume → 心跳 → 事件分发，直到断线。
func (b *Bot) serve() error {
	token, err := b.accessToken()
	if err != nil {
		return fmt.Errorf("app access token: %w", err)
	}
	wsURL, err := b.gatewayWS(token)
	if err != nil {
		return fmt.Errorf("gateway url: %w", err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	b.mu.Lock()
	b.conn = conn
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		if b.conn == conn {
			b.conn = nil
		}
		b.mu.Unlock()
	}()

	// 等待 hello（op10），取心跳间隔（握手阶段设 30s 读超时，事件循环后由心跳保活）
	var hbInterval time.Duration
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	var hello wsPayload
	if err := json.Unmarshal(raw, &hello); err != nil || hello.Op != opHello {
		return fmt.Errorf("hello: 首帧非 op10: %s", truncate(string(raw), 200))
	}
	var hd helloData
	_ = json.Unmarshal(hello.D, &hd)
	hbInterval = time.Duration(hd.HeartbeatInterval) * time.Millisecond
	if hbInterval <= 0 {
		hbInterval = 40 * time.Second
	}

	// identify 或 resume（有会话则优先续传）
	writeMu := &sync.Mutex{}
	writeJSON := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}
	b.mu.Lock()
	session, seq := b.session, b.lastSeq
	b.mu.Unlock()
	if session != "" {
		if err := writeJSON(map[string]any{"op": opResume, "d": map[string]any{
			"token": "QQBot " + token, "session_id": session, "seq": seq,
		}}); err != nil {
			return fmt.Errorf("resume: %w", err)
		}
	} else {
		if err := writeJSON(map[string]any{"op": opIdentify, "d": map[string]any{
			"token": "QQBot " + token, "intents": intents, "shard": []int{0, 1},
		}}); err != nil {
			return fmt.Errorf("identify: %w", err)
		}
	}

	// 心跳 goroutine
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(hbInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-b.stopCh:
				return
			case <-ticker.C:
				b.mu.Lock()
				s := b.lastSeq
				b.mu.Unlock()
				var d any
				if s > 0 {
					d = s
				}
				if err := writeJSON(map[string]any{"op": opHeartbeat, "d": d}); err != nil {
					return
				}
			}
		}
	}()

	// 事件循环
	_ = conn.SetReadDeadline(time.Time{})
	for {
		select {
		case <-b.stopCh:
			return errStopped
		default:
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			select {
			case <-b.stopCh:
				return errStopped
			default:
			}
			return fmt.Errorf("read: %w", err)
		}
		var p wsPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		switch p.Op {
		case opDispatch:
			if p.S > 0 {
				b.mu.Lock()
				if p.S > b.lastSeq {
					b.lastSeq = p.S
				}
				b.mu.Unlock()
			}
			b.dispatch(p.T, p.D)
		case opReconnect: // 平台要求重连：保留 session 走 resume
			return errors.New("平台下发 reconnect")
		case opInvalidSession: // 会话失效：清 session，下次 identify 全新登录
			b.mu.Lock()
			b.session = ""
			b.lastSeq = 0
			b.mu.Unlock()
			return errors.New("invalid session")
		case opHeartbeatACK:
			// 心跳应答，无需处理
		}
	}
}

// dispatch 事件分发（READY 记 session；消息事件异步处理避免阻塞心跳读循环）。
func (b *Bot) dispatch(eventType string, d json.RawMessage) {
	switch eventType {
	case "READY":
		var rd readyData
		if err := json.Unmarshal(d, &rd); err == nil && rd.SessionID != "" {
			b.mu.Lock()
			b.session = rd.SessionID
			b.mu.Unlock()
		}
	case "RESUMED":
		log.Printf("[agent-qq] 会话已恢复（resume 成功）")
	case "C2C_MESSAGE_CREATE", "GROUP_AT_MESSAGE_CREATE", "AT_MESSAGE_CREATE":
		msg := parseInbound(eventType, d)
		if msg != nil {
			go b.handleMessage(msg)
		}
	}
}

// parseInbound 归一化三种消息事件；非文本或缺关键字段返回 nil。
func parseInbound(eventType string, d json.RawMessage) *inboundMsg {
	var ev struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		Author  struct {
			ID           string `json:"id"`
			UserOpenID   string `json:"user_openid"`
			MemberOpenID string `json:"member_openid"`
		} `json:"author"`
		GroupID     string `json:"group_id"`
		GroupOpenID string `json:"group_openid"`
		ChannelID   string `json:"channel_id"`
	}
	if err := json.Unmarshal(d, &ev); err != nil {
		return nil
	}
	text := strings.TrimSpace(stripMentions(ev.Content))
	if text == "" || ev.ID == "" {
		return nil
	}
	m := &inboundMsg{msgID: ev.ID, text: text}
	switch eventType {
	case "C2C_MESSAGE_CREATE":
		m.openID = firstNonEmpty(ev.Author.UserOpenID, ev.Author.ID)
		m.chatType = "c2c"
		m.chatID = m.openID
	case "GROUP_AT_MESSAGE_CREATE":
		m.openID = firstNonEmpty(ev.Author.MemberOpenID, ev.Author.ID)
		m.chatType = "group"
		m.chatID = firstNonEmpty(ev.GroupOpenID, ev.GroupID)
	case "AT_MESSAGE_CREATE":
		m.openID = ev.Author.ID
		m.chatType = "channel"
		m.chatID = ev.ChannelID
	}
	if m.openID == "" || m.chatID == "" {
		return nil
	}
	return m
}

// stripMentions 去掉频道 AT 消息的 <@!uid> 提及标签。
func stripMentions(s string) string {
	for {
		i := strings.Index(s, "<@!")
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], ">")
		if j < 0 {
			break
		}
		s = s[:i] + s[i+j+1:]
	}
	return s
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// handleMessage 消息路由：/help → 命令清单；6 位数字 → 配对；未绑定 → 指引；
// 其余斜杠命令 → handleCommand；已绑定普通消息 → 单飞 headless 执行并被动回复。
func (b *Bot) handleMessage(m *inboundMsg) {
	if m.text == "/help" {
		b.reply(m, helpText())
		return
	}
	if len(m.text) == 6 && isDigits(m.text) {
		link, err := b.repo.ConsumePairCode(m.text, m.openID, m.chatType, m.chatID)
		if err != nil {
			b.reply(m, "配对码无效或已过期，请在网页端重新生成。")
			return
		}
		b.reply(m, fmt.Sprintf("✅ 绑定成功（user_id=%d）。现在直接发消息即可使唤你的交易助手。", link.UserID))
		return
	}

	link, err := b.repo.GetByOpenID(m.openID)
	if err != nil {
		b.reply(m, "你还没有绑定账号。在网页端 助手 → QQ 接入 生成配对码后发给我。")
		return
	}
	// 回发目标跟随最新会话（用户换群/换频道时）
	if link.ChatType != m.chatType || link.ChatID != m.chatID {
		_ = b.repo.SetChatContext(m.openID, m.chatType, m.chatID)
		link.ChatType, link.ChatID = m.chatType, m.chatID
	}
	if strings.HasPrefix(m.text, "/") {
		b.handleCommand(m, link)
		return
	}
	b.runInbound(m, link)
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
func (b *Bot) handleCommand(m *inboundMsg, link *Link) {
	fields := strings.Fields(m.text)
	cmd := strings.TrimPrefix(fields[0], "/")
	arg := strings.TrimSpace(strings.TrimPrefix(m.text, fields[0]))
	openID := link.OpenID

	switch cmd {
	case "new":
		if err := b.repo.SetConversationID(openID, ""); err != nil {
			b.reply(m, "操作失败："+truncate(err.Error(), 200))
			return
		}
		b.reply(m, "🆕 已开启新会话，下条消息将从头开始。")

	case "stop":
		b.mu.Lock()
		cancel, ok := b.runs[openID]
		b.mu.Unlock()
		if !ok {
			b.reply(m, "当前没有正在执行的任务。")
			return
		}
		cancel()
		b.reply(m, "⏹ 已发送中断信号。")

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
		b.reply(m, fmt.Sprintf("📊 状态\n绑定账号：%s（user_id=%d）\n模型：%s\n会话绑定：%s\n网关：%s",
			openID, link.UserID, model, conv, health))

	case "model":
		if arg == "" {
			cur := link.Model
			if cur == "" {
				cur = "默认（跟随网关配置）"
			}
			b.reply(m, "当前模型："+cur+"\n用 /model <name> 覆盖，/model default 恢复默认。")
			return
		}
		if arg == "default" || arg == "off" || arg == "auto" {
			if err := b.repo.SetModel(openID, ""); err != nil {
				b.reply(m, "操作失败："+truncate(err.Error(), 200))
				return
			}
			b.reply(m, "已恢复默认模型。")
			return
		}
		if err := b.repo.SetModel(openID, arg); err != nil {
			b.reply(m, "操作失败："+truncate(err.Error(), 200))
			return
		}
		b.reply(m, "✅ 后续使用模型："+arg)

	case "help":
		b.reply(m, helpText())

	default:
		b.reply(m, "未知命令 /"+cmd+"，/help 查看可用命令。")
	}
}

// runInbound 绑定用户普通消息：单飞执行 → 被动回复（msg_id，超长分条）。
func (b *Bot) runInbound(m *inboundMsg, link *Link) {
	// 单飞：同 open_id 并发消息直接提示
	b.mu.Lock()
	if _, busy := b.runs[m.openID]; busy {
		b.mu.Unlock()
		b.reply(m, "正在处理上一条消息，/stop 可中断。")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.runs[m.openID] = cancel
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.runs, m.openID)
		b.mu.Unlock()
		cancel()
	}()

	if b.exec == nil {
		b.reply(m, "执行器未配置，请联系管理员。")
		return
	}
	reply, err := b.exec(ctx, &RunRequest{
		UserID:         link.UserID,
		Prompt:         m.text,
		ConversationID: link.ConversationID,
		Model:          link.Model,
	})
	switch {
	case ctx.Err() != nil:
		// /stop 中断：本轮结果丢弃
		b.reply(m, "⏹ 已中断。")
	case err != nil:
		b.reply(m, "执行出错："+truncate(err.Error(), 500))
	default:
		if strings.TrimSpace(reply) == "" {
			reply = "✅ 完成（无文本输出）。"
		}
		b.reply(m, reply)
	}
}

// ── 发送侧（app access token + 被动回复） ──

// Send 向指定会话投递文本（超长自动分条）；msgID 非空 = 被动回复（平台配额），
// 空 = 主动消息（需平台侧开通主动消息权限，否则回发会失败并返回 false）。
func (b *Bot) Send(chatType, chatID, msgID, text string) bool {
	if !Configured() {
		return false
	}
	ok := true
	for _, chunk := range chunkRunes(text, maxChunkRunes) {
		if !b.sendChunk(chatType, chatID, msgID, chunk) {
			ok = false
		}
	}
	return ok
}

// SendToUser 按平台用户投递（查绑定表的最近会话上下文）；未绑定/未配置返回 false
// （调用方回落）。QQ 无 msg_id 的主动消息受平台权限限制，详见 Send 注释。
func (b *Bot) SendToUser(userID int64, text string) bool {
	link, err := b.repo.GetByUserID(userID)
	if err != nil || link.ChatID == "" {
		return false
	}
	return b.Send(link.ChatType, link.ChatID, "", text)
}

// reply 被动回复（始终携带事件 msg_id，忽略失败）。
func (b *Bot) reply(m *inboundMsg, text string) {
	b.Send(m.chatType, m.chatID, m.msgID, text)
}

// sendChunk 发送单条文本（≤2000 runes）。
func (b *Bot) sendChunk(chatType, chatID, msgID, text string) bool {
	token, err := b.accessToken()
	if err != nil {
		log.Printf("[agent-qq] app access token: %v", err)
		return false
	}
	var endpoint string
	payload := map[string]any{"content": text}
	switch chatType {
	case "group":
		endpoint = fmt.Sprintf("%s/v2/groups/%s/messages", b.restBase(), chatID)
		payload["msg_type"] = 0
	case "channel":
		endpoint = fmt.Sprintf("%s/channels/%s/messages", b.restBase(), chatID)
	default: // c2c
		endpoint = fmt.Sprintf("%s/v2/users/%s/messages", b.restBase(), chatID)
		payload["msg_type"] = 0
	}
	if msgID != "" {
		payload["msg_id"] = msgID
	}
	data, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(data)))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "QQBot "+token)
	resp, err := b.client.Do(req)
	if err != nil {
		log.Printf("[agent-qq] send message: %v", err)
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		log.Printf("[agent-qq] send message: status=%d body=%s", resp.StatusCode, truncate(string(body), 200))
		return false
	}
	return true
}

// restBase REST 域名（注入值优先，其次环境默认）。
func (b *Bot) restBase() string {
	if b.apiBase != "" {
		return b.apiBase
	}
	return defaultAPIBase()
}

// appTokenURL token 接口（注入值优先）。
func (b *Bot) appTokenURL() string {
	if b.tokenURL != "" {
		return b.tokenURL
	}
	return "https://bots.qq.com/app/getAppAccessToken"
}

// accessToken 取 app access token（缓存至 TTL-60s）。
func (b *Bot) accessToken() (string, error) {
	b.mu.Lock()
	if b.token != "" && time.Now().Before(b.tokenExp) {
		defer b.mu.Unlock()
		return b.token, nil
	}
	b.mu.Unlock()

	appID, appSecret := credentials()
	if appID == "" || appSecret == "" {
		return "", fmt.Errorf("QQ 应用凭据未配置")
	}
	payload, _ := json.Marshal(map[string]string{"appID": appID, "clientSecret": appSecret})
	resp, err := b.client.Post(b.appTokenURL(), "application/json", strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		AccessToken string  `json:"access_token"`
		ExpiresIn   flexInt `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("app access token 获取失败: %s", truncate(string(body), 200))
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

// gatewayWS 取 WSS 地址：注入值优先，否则 GET {apiBase}/gateway。
func (b *Bot) gatewayWS(token string) (string, error) {
	if b.wsURL != "" {
		return b.wsURL, nil
	}
	req, err := http.NewRequest(http.MethodGet, b.restBase()+"/gateway", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "QQBot "+token)
	resp, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", fmt.Errorf("gateway url 为空: %s", truncate(string(body), 200))
	}
	return out.URL, nil
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
