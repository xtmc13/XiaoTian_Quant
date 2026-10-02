package agenttelegram

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Executor 入站消息执行器（headless agent runner，由 main 注入）。
type Executor func(userID int64, prompt string) (string, error)

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

// Bot 入站长轮询机器人：配对 + 绑定用户消息 → headless 执行 → 回复。
type Bot struct {
	token   string
	apiBase string // 可覆盖（测试指向 httptest）
	repo    *Repo
	exec    Executor

	mu     sync.Mutex
	offset int64
	stopCh chan struct{}
	once   sync.Once
	client *http.Client
}

// NewBot 构造；token 为空时 Start 直接返回（未配置）。
func NewBot(token string, repo *Repo, exec Executor) *Bot {
	return &Bot{
		token:   token,
		apiBase: "https://api.telegram.org",
		repo:    repo,
		exec:    exec,
		stopCh:  make(chan struct{}),
		client:  &http.Client{Timeout: 45 * time.Second},
	}
}

// SetAPIBase 覆盖 API 地址（测试用）。
func (b *Bot) SetAPIBase(base string) { b.apiBase = base }

// SetExecutor 注入执行器。
func (b *Bot) SetExecutor(exec Executor) { b.exec = exec }

// Start 启动轮询（幂等；未配置 token 时仅记日志）。
func (b *Bot) Start() {
	if b.token == "" {
		log.Printf("[agent-telegram] TELEGRAM_BOT_TOKEN 未配置，入站通道未启动")
		return
	}
	if b.exec == nil {
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

// handleMessage 消息路由：/start → 指引；6 位数字 → 配对；已绑定 → 执行并回复；其余 → 提示。
func (b *Bot) handleMessage(m tgMessage) {
	text := strings.TrimSpace(m.Text)
	switch {
	case text == "/start":
		b.reply(m.Chat.ID, "小天量化助手已就绪。\n1) 在网页端 助手 → Telegram 接入 生成配对码\n2) 把 6 位配对码发给我即可完成绑定\n绑定后直接发消息即可使唤你的交易助手。")
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
	if b.exec == nil {
		b.reply(m.Chat.ID, "执行器未配置，请联系管理员。")
		return
	}
	reply, err := b.exec(link.UserID, text)
	if err != nil {
		b.reply(m.Chat.ID, "执行出错："+truncate(err.Error(), 500))
		return
	}
	b.reply(m.Chat.ID, truncate(reply, 3900))
}

func (b *Bot) reply(chatID int64, text string) {
	payload := map[string]any{"chat_id": chatID, "text": text}
	data, _ := json.Marshal(payload)
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", b.apiBase, b.token)
	resp, err := b.client.Post(endpoint, "application/json", strings.NewReader(string(data)))
	if err != nil {
		log.Printf("[agent-telegram] sendMessage: %v", err)
		return
	}
	resp.Body.Close()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func isDigits(s string) bool {
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}
