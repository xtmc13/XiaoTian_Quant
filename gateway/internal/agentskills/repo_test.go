package agentskills

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/plugin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func setupSkillsTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-agent-skills")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(store.CloseDB)
}

func TestRepo_UpsertAndGet(t *testing.T) {
	setupSkillsTestDB(t)
	repo := NewRepo()

	if err := repo.Upsert(&Skill{ID: NewID(), UserID: 1, Name: "每日复盘", Description: "收盘后复盘", Body: "1. 看持仓 2. 看信号 3. 写总结"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	s, err := repo.GetByName(1, "每日复盘")
	if err != nil {
		t.Fatal(err)
	}
	if s.Body == "" || s.Source != "user" {
		t.Errorf("unexpected skill: %+v", s)
	}

	// 同名覆盖（对话中沉淀）：行内更新，id 保持不变
	if err := repo.Upsert(&Skill{ID: NewID(), UserID: 1, Name: "每日复盘", Body: "更新后的流程", Source: "agent"}); err != nil {
		t.Fatalf("upsert overwrite: %v", err)
	}
	s2, _ := repo.GetByName(1, "每日复盘")
	if s2.Body != "更新后的流程" || s2.Source != "agent" || s2.ID != s.ID {
		t.Errorf("overwrite failed: %+v", s2)
	}

	// 不同用户互不影响
	if err := repo.Upsert(&Skill{ID: NewID(), UserID: 2, Name: "每日复盘", Body: "别人的流程"}); err != nil {
		t.Fatal(err)
	}
	other, _ := repo.GetByName(2, "每日复盘")
	if other.Body != "别人的流程" {
		t.Error("user isolation broken")
	}
}

func TestRepo_UsageCountAndOrder(t *testing.T) {
	setupSkillsTestDB(t)
	repo := NewRepo()
	repo.Upsert(&Skill{ID: NewID(), UserID: 1, Name: "少用", Body: "x"})
	repo.Upsert(&Skill{ID: NewID(), UserID: 1, Name: "常用", Body: "y"})

	hot, _ := repo.GetByName(1, "常用")
	for i := 0; i < 3; i++ {
		repo.TouchUsage(hot.ID, 1)
	}
	list, err := repo.ListByUser(1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Name != "常用" || list[0].UsageCount != 3 {
		t.Errorf("usage ordering wrong: %+v", list[0])
	}
}

func TestRepo_Delete(t *testing.T) {
	setupSkillsTestDB(t)
	repo := NewRepo()
	repo.Upsert(&Skill{ID: NewID(), UserID: 1, Name: "待删", Body: "x"})
	s, _ := repo.GetByName(1, "待删")
	if err := repo.Delete(s.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetByName(1, "待删"); err != nil {
		t.Fatal("他人删除不应生效")
	}
	repo.Delete(s.ID, 1)
	if _, err := repo.GetByName(1, "待删"); err == nil {
		t.Error("delete 未生效")
	}
}

func TestLoadCatalogSection(t *testing.T) {
	setupSkillsTestDB(t)
	repo := NewRepo()
	repo.Upsert(&Skill{ID: NewID(), UserID: 7, Name: "每日复盘", Description: "收盘复盘流程", Body: "步骤…"})
	repo.Upsert(&Skill{ID: NewID(), UserID: 7, Name: "巡检", Description: "", Body: "步骤…"})

	sec := LoadCatalogSection(7)
	if sec == "" {
		t.Fatal("catalog should not be empty")
	}
	if !strings.Contains(sec, "/每日复盘") || !strings.Contains(sec, "收盘复盘流程") || !strings.Contains(sec, "/巡检") {
		t.Errorf("catalog missing entries:\n%s", sec)
	}
	// 无技能 / 无用户 → 空串
	if LoadCatalogSection(999) != "" {
		t.Error("expected empty for user without skills")
	}
}

func TestPlugin_RegisterAndRun(t *testing.T) {
	setupSkillsTestDB(t)
	repo := NewRepo()
	p := &SkillsPlugin{Repo: repo}

	reg := plugin.NewRegistry()
	if err := p.Register(reg, plugin.Deps{}); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range reg.Tools() {
		names[tl.Name] = true
	}
	for _, want := range []string{"save_skill", "run_skill", "list_skills", "delete_skill"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}

	// save（agent 沉淀）→ run 返回正文并计数 → delete
	tc := &agent.ToolContext{UserID: 1}
	if _, err := p.save(tc, nil, map[string]any{"name": "流程A", "body": "第一步…", "description": "测试"}); err != nil {
		t.Fatal(err)
	}
	out, err := p.run(tc, nil, map[string]any{"name": "流程A"})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := out.(string)
	if !strings.Contains(text, "第一步…") {
		t.Errorf("run should return body: %v", out)
	}
	s, _ := repo.GetByName(1, "流程A")
	if s.UsageCount != 1 {
		t.Errorf("usage = %d, want 1", s.UsageCount)
	}
	if _, err := p.run(tc, nil, map[string]any{"name": "不存在的"}); err == nil {
		t.Error("run missing skill should error")
	}
}