package agenttelegram

import (
	"errors"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// 绑定一个 chat 的快捷方式（handoff 用例前置）。
func linkChat(t *testing.T, repo *Repo, userID, chatID int64, username string) {
	t.Helper()
	code, err := repo.CreatePairCode(userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ConsumePairCode(code, chatID, username); err != nil {
		t.Fatal(err)
	}
}

func TestHandoffCodeLifecycle(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 1, 100, "trader")

	code, err := repo.CreateHandoffCode(1, "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 || !isDigits(code) {
		t.Errorf("code = %q, want 6 位数字", code)
	}

	// 错误码
	if _, err := repo.ConsumeHandoffCode("999999", 100); !errors.Is(err, ErrCodeInvalid) {
		t.Errorf("未知码应无效: %v", err)
	}

	// 正确码消费成功 → chat 绑定到目标会话
	convID, err := repo.ConsumeHandoffCode(code, 100)
	if err != nil {
		t.Fatal(err)
	}
	if convID != "conv-1" {
		t.Errorf("convID = %q, want conv-1", convID)
	}
	link, err := repo.GetByChatID(100)
	if err != nil || link.ConversationID != "conv-1" {
		t.Errorf("link 会话绑定: %+v %v", link, err)
	}

	// 一次性：再用即失效
	if _, err := repo.ConsumeHandoffCode(code, 100); !errors.Is(err, ErrCodeInvalid) {
		t.Error("已用码应失效")
	}
}

func TestHandoffCodeExpiry(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 1, 100, "trader")

	code, _ := repo.CreateHandoffCode(1, "conv-1")
	// 手动把过期时间拨到过去
	db := store.GetDB()
	_, err := db.Exec(`UPDATE xt_agent_handoff_codes SET expires_at = ? WHERE code = ?`, time.Now().Unix()-10, code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ConsumeHandoffCode(code, 100); !errors.Is(err, ErrCodeInvalid) {
		t.Errorf("过期码应无效: %v", err)
	}
}

func TestHandoffCodeOldInvalidated(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 1, 100, "trader")

	code1, _ := repo.CreateHandoffCode(1, "conv-1")
	code2, _ := repo.CreateHandoffCode(1, "conv-2")
	if code1 == code2 {
		t.Error("新码不应与旧码相同")
	}
	if _, err := repo.ConsumeHandoffCode(code1, 100); !errors.Is(err, ErrCodeInvalid) {
		t.Error("旧码应被作废")
	}
	if _, err := repo.ConsumeHandoffCode(code2, 100); err != nil {
		t.Errorf("新码应可用: %v", err)
	}
}

func TestHandoffConsumeWrongUserChat(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 1, 100, "trader")
	linkChat(t, repo, 2, 200, "other")

	// 用户 1 的码被绑定到用户 2 的 chat 使用 → 拒绝（防越权接管会话）
	code, _ := repo.CreateHandoffCode(1, "conv-1")
	if _, err := repo.ConsumeHandoffCode(code, 200); !errors.Is(err, ErrNotLinked) {
		t.Errorf("他人 chat 消费应 ErrNotLinked: %v", err)
	}
	// 未绑定的 chat
	code2, _ := repo.CreateHandoffCode(1, "conv-1")
	if _, err := repo.ConsumeHandoffCode(code2, 300); !errors.Is(err, ErrNotLinked) {
		t.Errorf("未绑定 chat 消费应 ErrNotLinked: %v", err)
	}
}

func TestSetConversationID(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	linkChat(t, repo, 1, 100, "trader")

	// 新绑定默认无会话
	link, _ := repo.GetByChatID(100)
	if link.ConversationID != "" {
		t.Errorf("默认会话绑定应为空: %q", link.ConversationID)
	}
	if err := repo.SetConversationID(100, "conv-9"); err != nil {
		t.Fatal(err)
	}
	link, _ = repo.GetByUserID(1)
	if link.ConversationID != "conv-9" {
		t.Errorf("GetByUserID 会话绑定: %q", link.ConversationID)
	}
	// 清除（/new）
	if err := repo.SetConversationID(100, ""); err != nil {
		t.Fatal(err)
	}
	link, _ = repo.GetByChatID(100)
	if link.ConversationID != "" {
		t.Errorf("清除后应为空: %q", link.ConversationID)
	}
}
