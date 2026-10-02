package agentcron

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/notify"
	"time"
)

// TestScheduler_FeishuDingtalkPerUserDelivery 飞书/钉钉通道：每用户直发成功则剔除回落通道。
func TestScheduler_FeishuDingtalkPerUserDelivery(t *testing.T) {
	cap := &captureChannel{name: "feishu"}
	notify.GetManager().Register(cap)

	fs := newFakeStore(dueTelegramJob("fs1", 21, "feishu,dingtalk"))
	s := NewScheduler(fs, func(int64, string) (string, error) { return "持仓正常", nil })
	var feishuGot, dingtalkGot int64
	s.SetFeishuSender(func(userID int64, text string) bool {
		feishuGot = userID
		return true
	})
	s.SetDingtalkSender(func(userID int64, text string) bool {
		dingtalkGot = userID
		return true
	})

	j, _ := fs.Get("fs1", 21)
	before := cap.count()
	s.run(j)

	if feishuGot != 21 || dingtalkGot != 21 {
		t.Errorf("sender user = %d/%d, want 21/21", feishuGot, dingtalkGot)
	}
	time.Sleep(100 * time.Millisecond)
	if cap.count() != before {
		t.Error("两个通道都直发成功后不应走全局 notify 回落")
	}
}

// TestScheduler_FeishuFallbackWhenUndelivered 飞书直发失败 → 该通道留在回落清单走全局 notify。
func TestScheduler_FeishuFallbackWhenUndelivered(t *testing.T) {
	cap := &captureChannel{name: "feishu"}
	notify.GetManager().Register(cap)

	fs := newFakeStore(dueTelegramJob("fs2", 22, "feishu"))
	s := NewScheduler(fs, func(int64, string) (string, error) { return "结果", nil })
	s.SetFeishuSender(func(userID int64, text string) bool { return false })

	j, _ := fs.Get("fs2", 22)
	before := cap.count()
	s.run(j)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cap.count() > before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("飞书 sender 未送达时应回落全局 notify")
}

// TestScheduler_DingtalkSenderNotInjected 未注入 sender 时通道原样走全局 notify。
func TestScheduler_DingtalkSenderNotInjected(t *testing.T) {
	cap := &captureChannel{name: "dingtalk"}
	notify.GetManager().Register(cap)

	fs := newFakeStore(dueTelegramJob("dt3", 23, "dingtalk"))
	s := NewScheduler(fs, func(int64, string) (string, error) { return "结果", nil })

	j, _ := fs.Get("dt3", 23)
	before := cap.count()
	s.run(j)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cap.count() > before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("未注入钉钉 sender 时应回落全局 notify")
}
