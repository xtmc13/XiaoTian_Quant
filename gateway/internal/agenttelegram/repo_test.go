package agenttelegram

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/store"
)

func setupTGTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-agent-tg")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(store.CloseDB)
}

func TestPairCodeLifecycle(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()

	code, err := repo.CreatePairCode(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 || !isDigits(code) {
		t.Errorf("code = %q, want 6 位数字", code)
	}

	// 第二次生成，旧码作废
	code2, _ := repo.CreatePairCode(1)
	if code2 == code {
		t.Error("新码不应与旧码相同")
	}
	if _, err := repo.ConsumePairCode(code, 100, "olduser"); !errors.Is(err, ErrCodeInvalid) {
		t.Errorf("旧码应无效: %v", err)
	}

	// 正确码消费成功
	link, err := repo.ConsumePairCode(code2, 100, "someone")
	if err != nil {
		t.Fatal(err)
	}
	if link.UserID != 1 || link.TelegramChatID != 100 || link.TelegramUsername != "someone" {
		t.Errorf("unexpected link: %+v", link)
	}

	// 一次性：再用即失效
	if _, err := repo.ConsumePairCode(code2, 101, "other"); !errors.Is(err, ErrCodeInvalid) {
		t.Error("已用码应失效")
	}
	// 错误码
	if _, err := repo.ConsumePairCode("999999", 102, "x"); !errors.Is(err, ErrCodeInvalid) {
		t.Error("未知码应无效")
	}

	// 双向可查
	byUser, err := repo.GetByUserID(1)
	if err != nil || byUser.TelegramChatID != 100 {
		t.Errorf("GetByUserID: %+v %v", byUser, err)
	}
	byChat, err := repo.GetByChatID(100)
	if err != nil || byChat.UserID != 1 {
		t.Errorf("GetByChatID: %+v %v", byChat, err)
	}
	if _, err := repo.GetByChatID(404); !errors.Is(err, ErrNotLinked) {
		t.Error("未绑定 chat 应返回 ErrNotLinked")
	}
}

func TestChatRebind(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()

	code1, _ := repo.CreatePairCode(1)
	repo.ConsumePairCode(code1, 100, "first")

	// 同一 chat 被另一用户配对 → 换绑
	code2, _ := repo.CreatePairCode(2)
	link, err := repo.ConsumePairCode(code2, 100, "second")
	if err != nil {
		t.Fatal(err)
	}
	if link.UserID != 2 {
		t.Errorf("rebind user = %d, want 2", link.UserID)
	}
	if _, err := repo.GetByUserID(1); !errors.Is(err, ErrNotLinked) {
		t.Error("旧用户绑定应被清除")
	}
}

func TestUnlink(t *testing.T) {
	setupTGTestDB(t)
	repo := NewRepo()
	code, _ := repo.CreatePairCode(1)
	repo.ConsumePairCode(code, 100, "u")

	if err := repo.Unlink(1); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetByChatID(100); !errors.Is(err, ErrNotLinked) {
		t.Error("unlink 后 chat 查不到")
	}
}

func TestPluginInfoAndUI(t *testing.T) {
	p := &TelegramPlugin{Repo: NewRepo(), Bot: nil}
	info := p.Info()
	if info.Name != "telegram" || info.Kind != "channel" {
		t.Errorf("info: %+v", info)
	}
	ui := p.UI()
	if ui.Nav == nil || ui.Nav.ID != "telegram" {
		t.Errorf("ui.nav: %+v", ui.Nav)
	}
	if !strings.Contains(ui.Slash[0].Name, "telegram") {
		t.Errorf("ui.slash: %+v", ui.Slash)
	}
}
