package store

import (
	"testing"
)

// TestEnsureSystemTemplates: 系统预设模板幂等注册——两次调用不重复，
// 任意用户 List 可见（user_id=0 OR user_id=?），且完整覆盖 12 个模板
// （8 CTA + 4 组合）。
func TestEnsureSystemTemplates(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if err := EnsureSystemTemplates(); err != nil {
		t.Fatalf("EnsureSystemTemplates: %v", err)
	}
	if err := EnsureSystemTemplates(); err != nil {
		t.Fatalf("EnsureSystemTemplates (2nd, idempotent): %v", err)
	}

	repo := NewStrategyTemplateRepo()
	items, err := repo.List(0, "", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != len(SystemTemplates()) {
		t.Fatalf("system templates = %d, want %d", len(items), len(SystemTemplates()))
	}
	cta, combo := 0, 0
	seen := map[string]bool{}
	for _, it := range items {
		if it.UserID != 0 {
			t.Errorf("template %s user_id = %d, want 0 (system)", it.ID, it.UserID)
		}
		if it.DefaultConfigJSON == "" || it.DefaultConfigJSON == "{}" {
			t.Errorf("template %s missing default config", it.ID)
		}
		if seen[it.ID] {
			t.Errorf("duplicate template id %s (seed not idempotent)", it.ID)
		}
		seen[it.ID] = true
		switch it.StrategyType {
		case "combo":
			combo++
		default:
			cta++
		}
	}
	if cta != 8 || combo != 4 {
		t.Errorf("cta=%d combo=%d, want 8/4", cta, combo)
	}
}

// TestSystemTemplatesVisibleToAllUsers: 普通用户 List 能看到系统模板；
// 用户不能删除系统模板（user_id 条件保护）。
func TestSystemTemplatesVisibleToAllUsers(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if err := EnsureSystemTemplates(); err != nil {
		t.Fatalf("EnsureSystemTemplates: %v", err)
	}

	repo := NewStrategyTemplateRepo()
	items, err := repo.List(777, "contract", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 12 {
		t.Fatalf("user 777 sees %d contract templates, want 12 system presets", len(items))
	}
	for _, it := range items {
		if it.UserID != 0 {
			t.Errorf("user 777 should only see system templates before creating any, got user_id=%d", it.UserID)
		}
	}

	// 用户模板与系统模板并存可见。
	if err := repo.Create(&StrategyTemplateRecord{
		UserID: 777, Name: "我的模板", Category: "contract", StrategyType: "macd",
		DefaultConfigJSON: `{"symbol":"ETHUSDT"}`,
	}); err != nil {
		t.Fatalf("Create user template: %v", err)
	}
	items, err = repo.List(777, "contract", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 13 {
		t.Fatalf("user 777 sees %d templates, want 13 (12 system + 1 own)", len(items))
	}

	// 系统模板不可被普通用户删除；自己的可以。
	if deleted, err := repo.Delete("tpl-cta-ema-cross", 777); err != nil || deleted {
		t.Errorf("user must not delete system template: deleted=%v err=%v", deleted, err)
	}
	if deleted, err := repo.Delete("tpl-cta-ema-cross", 0); err != nil || !deleted {
		t.Errorf("system cleanup by owner id 0 should work: deleted=%v err=%v", deleted, err)
	}
}
