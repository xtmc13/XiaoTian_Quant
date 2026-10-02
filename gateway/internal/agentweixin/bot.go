// Package agentweixin 个人微信双向通道（腾讯官方 iLink Bot API，非 GeWeChat）：
// QR 扫码登录（get_bot_qrcode + get_qrcode_status 轮询）→ bot_token 落库持久化 →
// getupdates 长轮询收消息（35s hold，游标 get_updates_buf 持久化传递）→ 配对 /
// 斜杠命令 / headless 单飞执行 → sendmessage 回复（必带 context_token）/
// sendtyping 输入指示。
//
// 协议实现以官方 TS 源码为准（@tencent-weixin/openclaw-weixin）：
//   - 固定请求头：Content-Type: application/json、AuthorizationType: ilink_bot_token、
//     X-WECHAT-UIN: base64(十进制随机 uint32)（每次请求随机）
//   - 登录后带 Authorization: Bearer <bot_token>
//   - QR 流程：POST ilink/bot/get_bot_qrcode?bot_type=3 → GET ilink/bot/get_qrcode_status
//     （长轮询，含 scaned_but_redirect IDC 切换 / expired 重刷上限 / need_verifycode 等状态）
//   - 消息：message_type 1=用户 2=BOT；message_state 2=FINISH；item_list[].type 1=文本
//   - errcode -14 = 登录态过期（服务端建议暂停重试）
//
// 断线重连：长轮询网络错误指数退避（2s→30s），连续失败仅记日志不退出主循环；
// token 失效（-14）退避 10 分钟后自动重试；重启网关后凭库中 bot_token 自动恢复轮询。
package agentweixin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxChunkRunes 单条消息最大 rune 数（微信文本上限保守切分）。
const maxChunkRunes = 1900

// iLink 协议常量（对齐官方 TS 实现）。
const (
	defaultAPIBase        = "https://ilinkai.weixin.qq.com"
	defaultBotType        = "3"
	channelVersion        = "1.0.0"
	botAgent              = "XiaoTianQuant/1.0.0"
	qrLoginTTL            = 5 * time.Minute
	qrPollTimeout         = 35 * time.Second
	longPollTimeout       = 35 * time.Second
	apiTimeout            = 15 * time.Second
	staleTokenErrCode     = -14
	staleTokenRetryAfter  = 10 * time.Minute
	maxConsecutiveFailure = 3
	failureRetryDelay     = 2 * time.Second
	failureBackoffDelay   = 30 * time.Second
	loginChangePollDelay  = 2 * time.Second
)

// 消息类型 / 消息状态 / item 类型（proto 枚举）。
const (
	messageTypeUser = 1
	messageTypeBot  = 2
	messageStateNew = 0
	stateFinish     = 2
	itemTypeText    = 1
	typingStatusOn  = 1
)

// RunRequest 入站消息执行请求：会话绑定 / 模型覆盖可选。
type RunRequest struct {
	UserID         int64
	Prompt         string
	ConversationID string // 绑定的会话 id（空 = 无会话）
	Model          string // 模型覆盖（"provider" 或 "provider:model"）
}

// Executor 支持 ctx 中断（/stop）的执行器（main 注入 headless runner）。
type Executor func(ctx context.Context, req *RunRequest) (string, error)

// Configured 通道是否可用（iLink QR 登录无需任何环境凭据，恒为 true；
// 保留该函数与前端面板 configured 字段的契约一致性）。
func Configured() bool { return true }

// ── iLink 协议结构（只取所需字段） ──

// baseInfo 每个 CGI 请求附带的元数据。
type baseInfo struct {
	ChannelVersion string `json:"channel_version"`
	BotAgent       string `json:"bot_agent"`
}

func newBaseInfo() baseInfo { return baseInfo{ChannelVersion: channelVersion, BotAgent: botAgent} }

// weixinMessage 统一消息结构（proto WeixinMessage）。
type weixinMessage struct {
	FromUserID   string        `json:"from_user_id"`
	ToUserID     string        `json:"to_user_id"`
	ClientID     string        `json:"client_id"`
	MessageType  int           `json:"message_type"`
	MessageState int           `json:"message_state"`
	ContextToken string        `json:"context_token"`
	RunID        string        `json:"run_id"`
	ItemList     []messageItem `json:"item_list"`
}

// messageItem 消息内容项（type 1=文本 2=图 3=语音 4=文件 5=视频）。
type messageItem struct {
	Type     int      `json:"type"`
	TextItem *textItem `json:"text_item,omitempty"`
}

type textItem struct {
	Text string `json:"text"`
}

// getUpdatesResp 长轮询响应。
type getUpdatesResp struct {
	Ret                  int             `json:"ret"`
	ErrCode              int             `json:"errcode"`
	ErrMsg               string          `json:"errmsg"`
	Msgs                 []weixinMessage `json:"msgs"`
	GetUpdatesBuf        string          `json:"get_updates_buf"`
	LongpollingTimeoutMs int             `json:"longpolling_timeout_ms"`
}

// sendMessageResp 发送响应。
type sendMessageResp struct {
	Ret    int    `json:"ret"`
	ErrMsg string `json:"errmsg"`
}

// qrStatusResp QR 状态轮询响应。
type qrStatusResp struct {
	Status      string `json:"status"`
	BotToken    string `json:"bot_token"`
	ILinkBotID  string `json:"ilink_bot_id"`
	BaseURL     string `json:"baseurl"`
	ILinkUserID string `json:"ilink_user_id"`
	RedirectHost string `json:"redirect_host"`
}

// qrCodeResp get_bot_qrcode 响应（qrcode_img_content 为 base64 PNG）。
type qrCodeResp struct {
	QRCode        string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
}

// getConfigResp getconfig 响应（typing_ticket 用于 sendtyping）。
type getConfigResp struct {
	Ret         int    `json:"ret"`
	ErrMsg      string `json:"errmsg"`
	TypingTicket string `json:"typing_ticket"`
}

// qrLogin 一次进行中的 QR 登录会话（内存态）。
type qrLogin struct {
	mu        sync.Mutex
	qrcode    string
	img       string
	status    string // wait/scaned/confirmed/expired/binded_redirect/need_verifycode/verify_code_blocked/error/""
	errMsg    string
	baseURL   string    // 轮询基址（scaned_but_redirect 时切换 IDC）
	startedAt time.Time
	done      bool
}

// Bot 微信通道：QR 登录 + bot_token 持久化 + getupdates 长轮询 + 配对 + 单飞执行 + sendmessage 回发。
type Bot struct {
	repo     *Repo
	exec     Executor
	healthFn func() string
	client   *http.Client

	// 可注入端点（测试指向 httptest；空 = 官方固定基址）
	apiBase string

	stopCh    chan struct{}
	stopOnce  sync.Once
	startOnce sync.Once

	mu      sync.Mutex
	login   *LoginState            // 当前登录态（内存镜像；nil = 未登录）
	notify  chan struct{}          // 登录态变更广播（close + 重建）
	runs    map[string]context.CancelFunc // wxid → 在途 run 取消函数（单飞）
	qr      *qrLogin
	typing  map[string]string // wxid → typing_ticket 缓存
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewBot 构造。
func NewBot(repo *Repo) *Bot {
	ctx, cancel := context.WithCancel(context.Background())
	return &Bot{
		repo:   repo,
		client: &http.Client{Timeout: apiTimeout},
		stopCh: make(chan struct{}),
		notify: make(chan struct{}),
		runs:   map[string]context.CancelFunc{},
		typing: map[string]string{},
		ctx:    ctx,
		cancel: cancel,
	}
}

// SetAPIBase 覆盖 iLink 基址（测试用）。
func (b *Bot) SetAPIBase(base string) { b.apiBase = strings.TrimSuffix(base, "/") }

// SetExecutor 注入执行器。
func (b *Bot) SetExecutor(exec Executor) { b.exec = exec }

// SetHealthProvider 注入 /status 的网关健康行。
func (b *Bot) SetHealthProvider(fn func() string) { b.healthFn = fn }

func (b *Bot) baseURL() string {
	if b.apiBase != "" {
		return b.apiBase
	}
	return defaultAPIBase
}

// Start 启动长轮询主循环（幂等；凭库中 bot_token 恢复登录态，未登录则等待 QR 绑定）。
func (b *Bot) Start() {
	go b.startOnce.Do(b.bootstrap)
}

func (b *Bot) bootstrap() {
	st, err := b.repo.LoadLogin()
	if err != nil {
		log.Printf("[agent-weixin] 登录态读取失败: %v", err)
	} else if st != nil && st.BotToken != "" {
		b.mu.Lock()
		b.login = st
		b.broadcastLocked()
		b.mu.Unlock()
		log.Printf("[agent-weixin] 已凭持久化 token 恢复登录（bot_id=%s）", st.BotID)
	}
	b.pollLoop()
}

// Stop 停止轮询与在途执行（网关关闭时调用；不可重启）。
func (b *Bot) Stop() {
	b.stopOnce.Do(func() { close(b.stopCh) })
	b.mu.Lock()
	cancel := b.cancel
	for _, c := range b.runs {
		c()
	}
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// resetCtx 换新的请求 ctx（解绑 / 重新登录后使在途长轮询立即失败）。
func (b *Bot) resetCtx() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
	}
	b.ctx, b.cancel = context.WithCancel(context.Background())
}

// broadcastLocked 广播登录态变更（持 b.mu 调用）。
func (b *Bot) broadcastLocked() {
	close(b.notify)
	b.notify = make(chan struct{})
}

// setLogin 更新内存登录态并广播。
func (b *Bot) setLogin(st *LoginState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.login = st
	b.broadcastLocked()
}

// currentLogin 取当前登录态（无则 nil）。
func (b *Bot) currentLogin() *LoginState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.login
}

// requestCtx 取当前请求 ctx。
func (b *Bot) requestCtx() context.Context {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ctx
}

// stopped 是否已收到停机信号。
func (b *Bot) stopped() bool {
	select {
	case <-b.stopCh:
		return true
	default:
		return false
	}
}

// ── iLink HTTP 客户端 ──

// randomUIN X-WECHAT-UIN：随机 uint32 → 十进制字符串 → base64（每次请求重新生成）。
func randomUIN() string {
	var u [4]byte
	if _, err := rand.Read(u[:]); err != nil {
		return base64.StdEncoding.EncodeToString([]byte("0"))
	}
	n := binary.BigEndian.Uint32(u[:])
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(n), 10)))
}

// headers 组装固定请求头（token 非空时带 Bearer）。
func (b *Bot) headers(token string) http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("AuthorizationType", "ilink_bot_token")
	h.Set("X-WECHAT-UIN", randomUIN())
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	return h
}

// doPOST 发送 POST JSON 请求（ctx 来自 bot 生命周期，可由 Stop/resetCtx 取消）。
func (b *Bot) doPOST(ctx context.Context, baseURL, endpoint, token string, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/"+endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header = b.headers(token)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s status=%d body=%s", endpoint, resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// doGET 发送 GET 请求（带 35s 长轮询超时控制由调用方通过 ctx/Timeout 实现）。
func (b *Bot) doGET(ctx context.Context, baseURL, endpoint, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/"+endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header = b.headers(token)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s status=%d body=%s", endpoint, resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// ── QR 登录 ──

// StartQRLogin 生成登录二维码并后台轮询登录进展；返回 base64 PNG 与有效期（秒）。
// 已登录或已有进行中会话时返回当前二维码（幂等）。
func (b *Bot) StartQRLogin(ctx context.Context) (img string, expiresIn int, err error) {
	if st := b.currentLogin(); st != nil && st.BotToken != "" {
		return "", 0, errors.New("微信机器人已登录，请先解绑")
	}
	b.mu.Lock()
	if b.qr != nil && !b.qr.done && time.Since(b.qr.startedAt) < qrLoginTTL {
		qr := b.qr
		b.mu.Unlock()
		qr.mu.Lock()
		defer qr.mu.Unlock()
		return qr.img, int((qrLoginTTL - time.Since(qr.startedAt)).Seconds()), nil
	}
	b.mu.Unlock()

	body, err := b.doPOST(ctx, b.baseURL(), "ilink/bot/get_bot_qrcode?bot_type="+defaultBotType, "", map[string]any{
		"local_token_list": []string{},
	})
	if err != nil {
		return "", 0, fmt.Errorf("获取登录二维码: %w", err)
	}
	var qrResp qrCodeResp
	if err := json.Unmarshal(body, &qrResp); err != nil {
		return "", 0, fmt.Errorf("解析二维码响应: %w", err)
	}
	if qrResp.QRCode == "" || qrResp.QRCodeImgContent == "" {
		return "", 0, fmt.Errorf("二维码响应缺字段: %s", truncate(string(body), 200))
	}

	qr := &qrLogin{
		qrcode:    qrResp.QRCode,
		img:       qrResp.QRCodeImgContent,
		status:    "wait",
		baseURL:   b.baseURL(),
		startedAt: time.Now(),
	}
	b.mu.Lock()
	b.qr = qr
	b.mu.Unlock()
	go b.pollQRLogin(qr)
	return qr.img, int(qrLoginTTL.Seconds()), nil
}

// QRStatus 返回当前 QR 登录进展：none/wait/scaned/confirmed/expired/binded_redirect/
// need_verifycode/verify_code_blocked/error。
func (b *Bot) QRStatus() string {
	b.mu.Lock()
	qr := b.qr
	b.mu.Unlock()
	if qr == nil {
		return "none"
	}
	qr.mu.Lock()
	defer qr.mu.Unlock()
	if qr.status == "" {
		return "none"
	}
	return qr.status
}

// pollQRLogin 后台轮询 QR 状态直到 confirmed/expired/超时（参考官方 login-qr.ts）。
func (b *Bot) pollQRLogin(qr *qrLogin) {
	deadline := time.Now().Add(qrLoginTTL)
	for time.Now().Before(deadline) {
		if b.stopped() {
			return
		}
		qr.mu.Lock()
		currentBase := qr.baseURL
		code := qr.qrcode
		qr.mu.Unlock()

		// 长轮询 35s；网关 524 / 网络错误视为 wait 继续
		ctx, cancel := context.WithTimeout(b.requestCtx(), qrPollTimeout)
		body, err := b.doGET(ctx, currentBase, "ilink/bot/get_qrcode_status?qrcode="+codeURLQueryEscape(code), "")
		cancel()
		if err != nil {
			if b.stopped() || b.requestCtx().Err() != nil {
				return
			}
			if isTimeoutErr(err) {
				continue
			}
			log.Printf("[agent-weixin] QR 状态轮询网络错误，继续等待: %v", err)
			if !b.sleepStopAware(time.Second) {
				return
			}
			continue
		}
		var st qrStatusResp
		if err := json.Unmarshal(body, &st); err != nil {
			log.Printf("[agent-weixin] QR 状态解析失败: %v", err)
			if !b.sleepStopAware(time.Second) {
				return
			}
			continue
		}
		qr.mu.Lock()
		qr.status = st.Status
		qr.mu.Unlock()

		switch st.Status {
		case "wait", "scaned", "":
			// 继续轮询
		case "scaned_but_redirect":
			// IDC 重定向：切换轮询基址
			if st.RedirectHost != "" {
				qr.mu.Lock()
				qr.baseURL = "https://" + st.RedirectHost
				qr.mu.Unlock()
				log.Printf("[agent-weixin] QR 轮询 IDC 重定向 → %s", st.RedirectHost)
			}
		case "need_verifycode", "verify_code_blocked":
			// 需要手机端配对验证码：本通道无人机会话，提示重新扫码
			qr.mu.Lock()
			qr.status = st.Status
			qr.errMsg = "需要在手机微信确认配对验证码，请重新扫码登录"
			qr.done = true
			qr.mu.Unlock()
			return
		case "binded_redirect":
			// 该微信已绑定过此机器人，无需重复连接
			qr.mu.Lock()
			qr.done = true
			qr.mu.Unlock()
			return
		case "expired":
			qr.mu.Lock()
			qr.done = true
			qr.mu.Unlock()
			return
		case "confirmed":
			if st.ILinkBotID == "" {
				qr.mu.Lock()
				qr.status = "error"
				qr.errMsg = "登录失败：服务器未返回 ilink_bot_id"
				qr.done = true
				qr.mu.Unlock()
				return
			}
			base := st.BaseURL
			if base == "" {
				base = defaultAPIBase
			}
			login := &LoginState{
				BotToken:   st.BotToken,
				BaseURL:    base,
				BotID:      st.ILinkBotID,
				LoggedInAt: time.Now().Unix(),
			}
			if err := b.repo.SaveLogin(login); err != nil {
				qr.mu.Lock()
				qr.status = "error"
				qr.errMsg = "登录态保存失败: " + truncate(err.Error(), 200)
				qr.done = true
				qr.mu.Unlock()
				return
			}
			b.resetCtx()
			b.setLogin(login)
			qr.mu.Lock()
			qr.done = true
			qr.mu.Unlock()
			log.Printf("[agent-weixin] ✅ QR 登录成功（bot_id=%s）", st.ILinkBotID)
			return
		default:
			log.Printf("[agent-weixin] 未知 QR 状态: %s", st.Status)
		}
		if !b.sleepStopAware(time.Second) {
			return
		}
	}
	qr.mu.Lock()
	if !qr.done {
		qr.status = "expired"
		qr.done = true
	}
	qr.mu.Unlock()
}

// codeURLQueryEscape qrcode 参数编码。
func codeURLQueryEscape(s string) string {
	var sb strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			sb.WriteByte(c)
		} else {
			sb.WriteString(fmt.Sprintf("%%%02X", c))
		}
	}
	return sb.String()
}

// isTimeoutErr ctx 超时 / DeadlineExceeded 视为长轮询正常返回。
func isTimeoutErr(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

// sleepStopAware 睡眠 d 或直到停机；返回 false = 已停机。
func (b *Bot) sleepStopAware(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-b.stopCh:
		return false
	case <-timer.C:
		return true
	}
}

// Unlink 解绑：停轮询（取消在途长轮询）、清 bot_token 登录态。
// 用户级绑定（wxid ↔ 平台账号）由 Repo.Unlink 另行清除。
func (b *Bot) Unlink() error {
	if err := b.repo.ClearLogin(); err != nil {
		return err
	}
	b.setLogin(nil)
	b.resetCtx()
	log.Printf("[agent-weixin] 已解绑：bot_token 清除，轮询停止")
	return nil
}

// Status 登录态概览（前端状态卡）。
func (b *Bot) Status() (loggedIn bool, botID string) {
	st := b.currentLogin()
	if st == nil || st.BotToken == "" {
		return false, ""
	}
	return true, st.BotID
}

// ── 长轮询收消息主循环 ──

// pollLoop 单飞主循环：未登录时等待登录态变更；登录后持续 getupdates 长轮询。
// 错误处理对齐官方 monitor.ts：网络错误 2s 重试、连续 3 次退避 30s、-14 登录态过期退避 10 分钟。
func (b *Bot) pollLoop() {
	consecutiveFailures := 0
	nextTimeout := longPollTimeout
	for {
		if b.stopped() {
			return
		}
		st := b.currentLogin()
		if st == nil || st.BotToken == "" {
			consecutiveFailures = 0
			if !b.waitLoginChange() {
				return
			}
			continue
		}
		// 快照登录态，避免与游标持久化路径并发读写同一结构体字段
		snapshot := *st
		st = &snapshot
		base := st.BaseURL
		if base == "" {
			base = defaultAPIBase
			st.BaseURL = base // 快照副本，可安全改写
		}
		buf := st.GetUpdatesBuf
		resp, err := b.getUpdates(st, buf, nextTimeout)
		if err != nil {
			if b.stopped() || b.requestCtx().Err() != nil {
				// Stop / Unlink / 重新登录：回到循环头部重新评估登录态
				continue
			}
			if isTimeoutErr(err) {
				continue // 长轮询客户端超时：正常，直接重试
			}
			consecutiveFailures++
			log.Printf("[agent-weixin] getupdates 失败（%d/%d）: %v", consecutiveFailures, maxConsecutiveFailure, err)
			if !b.sleepStopAware(b.failureDelay(consecutiveFailures)) {
				return
			}
			continue
		}
		// 服务端建议的下一轮长轮询超时
		if resp.LongpollingTimeoutMs > 0 {
			nextTimeout = time.Duration(resp.LongpollingTimeoutMs) * time.Millisecond
		}
		if resp.ErrCode == staleTokenErrCode || resp.Ret == staleTokenErrCode {
			log.Printf("[agent-weixin] bot_token 失效（-14），%d 分钟后重试", int(staleTokenRetryAfter.Minutes()))
			consecutiveFailures = 0
			if !b.sleepStopAware(staleTokenRetryAfter) {
				return
			}
			continue
		}
		if resp.Ret != 0 || resp.ErrCode != 0 {
			consecutiveFailures++
			log.Printf("[agent-weixin] getupdates 返回错误（%d/%d）: ret=%d errcode=%d errmsg=%s",
				consecutiveFailures, maxConsecutiveFailure, resp.Ret, resp.ErrCode, resp.ErrMsg)
			if !b.sleepStopAware(b.failureDelay(consecutiveFailures)) {
				return
			}
			continue
		}
		consecutiveFailures = 0
		// 游标持久化（必须落库后再消费消息，否则重启会重复收）
		if resp.GetUpdatesBuf != "" && resp.GetUpdatesBuf != buf {
			if err := b.repo.SaveSyncBuf(resp.GetUpdatesBuf); err != nil {
				log.Printf("[agent-weixin] 游标落库失败: %v", err)
			}
			b.mu.Lock()
			if b.login != nil {
				b.login.GetUpdatesBuf = resp.GetUpdatesBuf
			}
			b.mu.Unlock()
			buf = resp.GetUpdatesBuf
		}
		for i := range resp.Msgs {
			msg := resp.Msgs[i]
			if msg.MessageType != messageTypeUser || msg.MessageState != stateFinish {
				continue
			}
			go b.handleMessage(&msg)
		}
	}
}

// waitLoginChange 等待登录态变更广播或定时复查；false = 停机。
func (b *Bot) waitLoginChange() bool {
	b.mu.Lock()
	ch := b.notify
	logged := b.login != nil && b.login.BotToken != ""
	b.mu.Unlock()
	if logged {
		return true
	}
	timer := time.NewTimer(loginChangePollDelay)
	defer timer.Stop()
	select {
	case <-b.stopCh:
		return false
	case <-ch:
		return true
	case <-timer.C:
		return true
	}
}

// failureDelay 连续失败退避：2s 递增，满 3 次后 30s。
func (b *Bot) failureDelay(consecutive int) time.Duration {
	if consecutive >= maxConsecutiveFailure {
		return failureBackoffDelay
	}
	return failureRetryDelay
}

// getUpdates 单次长轮询（客户端超时 = 服务端 hold 上限 + 5s 余量）。
func (b *Bot) getUpdates(st *LoginState, buf string, timeout time.Duration) (*getUpdatesResp, error) {
	ctx, cancel := context.WithTimeout(b.requestCtx(), timeout+5*time.Second)
	defer cancel()
	body, err := b.doPOST(ctx, st.BaseURL, "ilink/bot/getupdates", st.BotToken, map[string]any{
		"get_updates_buf": buf,
		"base_info":       newBaseInfo(),
	})
	if err != nil {
		return nil, err
	}
	var resp getUpdatesResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 getupdates 响应: %w", err)
	}
	return &resp, nil
}

// ── 入站消息处理 ──

// handleMessage 消息路由：/help → 命令清单；6 位数字 → 配对；未绑定 → 指引；
// 其余斜杠命令 → handleCommand；已绑定普通消息 → 单飞 headless 执行并回复。
func (b *Bot) handleMessage(m *weixinMessage) {
	text := strings.TrimSpace(extractText(m))
	if text == "" {
		return // 非文本消息暂不处理
	}
	if text == "/help" {
		b.reply(m, helpText())
		return
	}
	if len(text) == 6 && isDigits(text) {
		link, err := b.repo.ConsumePairCode(text, m.FromUserID)
		if err != nil {
			b.reply(m, "配对码无效或已过期，请在网页端重新生成。")
			return
		}
		b.reply(m, fmt.Sprintf("✅ 绑定成功（user_id=%d）。现在直接发消息即可使唤你的交易助手。", link.UserID))
		return
	}

	link, err := b.repo.GetByWXID(m.FromUserID)
	if err != nil {
		b.reply(m, "你还没有绑定账号。在网页端 助手 → 微信 接入页生成配对码后发给我。")
		return
	}
	if strings.HasPrefix(text, "/") {
		b.handleCommand(m, link)
		return
	}
	b.runInbound(m, link)
}

// extractText 拼接 item_list 中的文本项。
func extractText(m *weixinMessage) string {
	var sb strings.Builder
	for _, it := range m.ItemList {
		if it.Type == itemTypeText && it.TextItem != nil {
			sb.WriteString(it.TextItem.Text)
		}
	}
	return sb.String()
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
func (b *Bot) handleCommand(m *weixinMessage, link *Link) {
	text := extractText(m)
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return
	}
	cmd := strings.TrimPrefix(fields[0], "/")
	arg := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	wxid := link.WXID

	switch cmd {
	case "new":
		if err := b.repo.SetConversationID(wxid, ""); err != nil {
			b.reply(m, "操作失败："+truncate(err.Error(), 200))
			return
		}
		b.reply(m, "🆕 已开启新会话，下条消息将从头开始。")

	case "stop":
		b.mu.Lock()
		cancel, ok := b.runs[wxid]
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
			wxid, link.UserID, model, conv, health))

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
			if err := b.repo.SetModel(wxid, ""); err != nil {
				b.reply(m, "操作失败："+truncate(err.Error(), 200))
				return
			}
			b.reply(m, "已恢复默认模型。")
			return
		}
		if err := b.repo.SetModel(wxid, arg); err != nil {
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

// runInbound 绑定用户普通消息：单飞执行 → 回复（sendmessage，超长分条）。
func (b *Bot) runInbound(m *weixinMessage, link *Link) {
	// 单飞：同 wxid 并发消息直接提示
	parent := b.requestCtx()
	b.mu.Lock()
	if _, busy := b.runs[m.FromUserID]; busy {
		b.mu.Unlock()
		b.reply(m, "正在处理上一条消息，/stop 可中断。")
		return
	}
	ctx, cancel := context.WithCancel(parent)
	b.runs[m.FromUserID] = cancel
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.runs, m.FromUserID)
		b.mu.Unlock()
		cancel()
	}()

	if b.exec == nil {
		b.reply(m, "执行器未配置，请联系管理员。")
		return
	}
	go b.sendTyping(m.FromUserID, m.ContextToken)
	reply, err := b.exec(ctx, &RunRequest{
		UserID:         link.UserID,
		Prompt:         extractText(m),
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

// ── 发送侧（sendmessage + sendtyping） ──

// Send 向指定微信用户投递文本（超长自动分条）；ctxToken 为入站消息原样带回的
// context_token（主动消息传空串）。返回 false = 未登录 / 发送失败。
func (b *Bot) Send(wxid, text, ctxToken string) bool {
	st := b.currentLogin()
	if st == nil || st.BotToken == "" {
		return false
	}
	base := st.BaseURL
	if base == "" {
		base = defaultAPIBase
	}
	ok := true
	for _, chunk := range chunkRunes(text, maxChunkRunes) {
		if !b.sendChunk(base, st.BotToken, wxid, chunk, ctxToken) {
			ok = false
		}
	}
	return ok
}

// SendToUser 按平台用户投递（查绑定表）；未绑定/未登录返回 false（调用方回落）。
func (b *Bot) SendToUser(userID int64, text string) bool {
	link, err := b.repo.GetByUserID(userID)
	if err != nil || link.WXID == "" {
		return false
	}
	return b.Send(link.WXID, text, "")
}

// reply 回复入站消息（始终携带 context_token，忽略失败）。
func (b *Bot) reply(m *weixinMessage, text string) {
	b.Send(m.FromUserID, text, m.ContextToken)
}

// sendChunk 发送单条文本（≤maxChunkRunes runes）。
func (b *Bot) sendChunk(base, token, wxid, text, ctxToken string) bool {
	msg := map[string]any{
		"to_user_id":     wxid,
		"client_id":      genClientID(),
		"message_type":   messageTypeBot,
		"message_state":  stateFinish,
		"item_list":      []messageItem{{Type: itemTypeText, TextItem: &textItem{Text: text}}},
	}
	if ctxToken != "" {
		msg["context_token"] = ctxToken
	}
	body, err := b.doPOST(b.requestCtx(), base, "ilink/bot/sendmessage", token, map[string]any{"msg": msg})
	if err != nil {
		log.Printf("[agent-weixin] sendmessage: %v", err)
		return false
	}
	var resp sendMessageResp
	if err := json.Unmarshal(body, &resp); err != nil {
		log.Printf("[agent-weixin] sendmessage 响应解析失败: %v", err)
		return false
	}
	if resp.Ret != 0 {
		log.Printf("[agent-weixin] sendmessage ret=%d errmsg=%s", resp.Ret, resp.ErrMsg)
		return false
	}
	return true
}

// sendTyping 发送输入指示（best-effort；typing_ticket 经 getconfig 缓存）。
func (b *Bot) sendTyping(wxid, ctxToken string) {
	st := b.currentLogin()
	if st == nil || st.BotToken == "" {
		return
	}
	ticket := b.typingTicket(wxid, ctxToken)
	base := st.BaseURL
	if base == "" {
		base = defaultAPIBase
	}
	_, err := b.doPOST(b.requestCtx(), base, "ilink/bot/sendtyping", st.BotToken, map[string]any{
		"ilink_user_id": wxid,
		"typing_ticket": ticket,
		"status":        typingStatusOn,
	})
	if err != nil {
		log.Printf("[agent-weixin] sendtyping: %v", err)
		// ticket 可能过期：清缓存下次重取
		b.mu.Lock()
		delete(b.typing, wxid)
		b.mu.Unlock()
	}
}

// typingTicket 取 wxid 的 typing_ticket（缓存；miss 时经 getconfig 拉取）。
func (b *Bot) typingTicket(wxid, ctxToken string) string {
	b.mu.Lock()
	if t, ok := b.typing[wxid]; ok {
		b.mu.Unlock()
		return t
	}
	b.mu.Unlock()

	st := b.currentLogin()
	if st == nil || st.BotToken == "" {
		return ""
	}
	payload := map[string]any{"ilink_user_id": wxid}
	if ctxToken != "" {
		payload["context_token"] = ctxToken
	}
	base := st.BaseURL
	if base == "" {
		base = defaultAPIBase
	}
	body, err := b.doPOST(b.requestCtx(), base, "ilink/bot/getconfig", st.BotToken, payload)
	if err != nil {
		return ""
	}
	var resp getConfigResp
	if err := json.Unmarshal(body, &resp); err != nil || resp.TypingTicket == "" {
		return ""
	}
	b.mu.Lock()
	b.typing[wxid] = resp.TypingTicket
	b.mu.Unlock()
	return resp.TypingTicket
}

// genClientID 生成 client_id（"xt-weixin-" + 16 随机 hex）。
func genClientID() string {
	var u [8]byte
	if _, err := rand.Read(u[:]); err != nil {
		return "xt-weixin-0"
	}
	return fmt.Sprintf("xt-weixin-%x", u[:])
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
