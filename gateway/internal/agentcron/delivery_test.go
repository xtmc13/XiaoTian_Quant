package agentcron

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/notify"
)

// captureChannel 记录收到的 notify 消息（注册进全局 manager 观察回落投递）。
type captureChannel struct {
	name string
	mu   sync.Mutex
	got  []notify.Message
}

func (c *captureChannel) Name() string    { return c.name }
func (c *captureChannel) IsEnabled() bool { return true }
func (c *captureChannel) Send(msg notify.Message) error {
	c.mu.Lock()
	c.got = append(c.got, msg)
	c.mu.Unlock()
	return nil
}

func (c *captureChannel) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.got)
}

func dueTelegramJob(id string, userID int64, channel string) *Job {
	return &Job{
		ID: id, UserID: userID, Name: "投递测试", Prompt: "看一眼 BTC",
		Schedule: "0 8 * * *", Timezone: "Asia/Shanghai", Channel: channel,
		Enabled: true, NextRunAt: time.Now().Unix() - 10,
	}
}

func TestScheduler_TelegramPerUserDelivery(t *testing.T) {
	cap := &captureChannel{name: "telegram"}
	notify.GetManager().Register(cap)

	fs := newFakeStore(dueTelegramJob("tg1", 7, "telegram"))
	s := NewScheduler(fs, func(int64, string) (string, error) { return "持仓正常", nil })
	var gotUser int64
	var gotText string
	s.SetTelegramSender(func(userID int64, text string) bool {
		gotUser, gotText = userID, text
		return true
	})

	j, _ := fs.Get("tg1", 7)
	before := cap.count()
	s.run(j)

	if gotUser != 7 {
		t.Errorf("sender user = %d, want 7", gotUser)
	}
	if gotText == "" || !containsAll(gotText, "投递测试", "持仓正常") {
		t.Errorf("sender text = %q", gotText)
	}
	// 已直发：不应再回落到全局 notify
	time.Sleep(100 * time.Millisecond)
	if cap.count() != before {
		t.Error("每用户直发成功后不应走全局 notify 回落")
	}
	// last_result 照常落库
	fs.mu.Lock()
	if fs.jobs["tg1"].LastResult != "持仓正常" {
		t.Errorf("last_result = %q", fs.jobs["tg1"].LastResult)
	}
	fs.mu.Unlock()
}

func TestScheduler_TelegramFallbackWhenUndelivered(t *testing.T) {
	cap := &captureChannel{name: "telegram"}
	notify.GetManager().Register(cap)

	fs := newFakeStore(dueTelegramJob("tg2", 8, "telegram"))
	s := NewScheduler(fs, func(int64, string) (string, error) { return "结果", nil })
	// 用户未绑定 TG → sender 返回 false → 回落全局 notify
	s.SetTelegramSender(func(userID int64, text string) bool { return false })

	j, _ := fs.Get("tg2", 8)
	before := cap.count()
	s.run(j)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cap.count() > before {
			return // 收到回落投递，通过
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("sender 未送达时应回落全局 notify")
}

func TestScheduler_WebChannelSkipsDelivery(t *testing.T) {
	fs := newFakeStore(dueTelegramJob("tg3", 9, "web"))
	s := NewScheduler(fs, func(int64, string) (string, error) { return "结果", nil })
	called := false
	s.SetTelegramSender(func(int64, string) bool { called = true; return true })

	j, _ := fs.Get("tg3", 9)
	s.run(j)
	if called {
		t.Error("web 通道不应触发 TG 投递")
	}
	fs.mu.Lock()
	if fs.jobs["tg3"].LastResult != "结果" {
		t.Errorf("web 通道 last_result 应照常落库: %q", fs.jobs["tg3"].LastResult)
	}
	fs.mu.Unlock()
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func TestChannelListHelpers(t *testing.T) {
	if !hasChannel("web,telegram", "telegram") || !hasChannel("telegram", "telegram") {
		t.Error("hasChannel 应命中")
	}
	if hasChannel("web,email", "telegram") || hasChannel("telegraph", "telegram") {
		t.Error("hasChannel 不应误命中")
	}
	if got := removeChannel("web,telegram", "telegram"); got != "web" {
		t.Errorf("removeChannel = %q", got)
	}
	if got := removeChannel("telegram", "telegram"); got != "" {
		t.Errorf("removeChannel 全剔除应为空: %q", got)
	}
}
