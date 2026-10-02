package agentmemory

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/plugin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func setupMemoryTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-agent-memory")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(store.CloseDB)
}

func TestRepo_CreateAndList(t *testing.T) {
	setupMemoryTestDB(t)
	repo := NewRepo()

	if err := repo.Create(&Memory{ID: NewID(), UserID: 1, Kind: "preference", Content: "偏好低杠杆短线", Importance: 4}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Create(&Memory{ID: NewID(), UserID: 1, Kind: "market_note", Content: "BTC 关键支撑 62000", Importance: 2}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// kind 非法回落 fact；importance 越界收敛
	if err := repo.Create(&Memory{ID: NewID(), UserID: 1, Kind: "weird", Content: "普通事实", Importance: 99}); err != nil {
		t.Fatalf("create fallback: %v", err)
	}

	all, err := repo.ListByUser(1, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d memories, want 3", len(all))
	}
	// importance 降序：clamp 到 5 的「普通事实」第一，4 分偏好第二
	if all[0].Content != "普通事实" || all[0].Importance != 5 {
		t.Errorf("first = %+v, want importance 5 在前且越界已收敛", all[0])
	}
	if all[1].Content != "偏好低杠杆短线" {
		t.Errorf("second = %q, want 4 分偏好", all[1].Content)
	}
	for _, m := range all {
		if m.Content == "普通事实" && (m.Kind != "fact" || m.Importance != 5) {
			t.Errorf("fallback 未收敛: %+v", m)
		}
	}

	// kind 过滤
	prefs, err := repo.ListByUser(1, "preference", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefs) != 1 {
		t.Errorf("kind filter got %d, want 1", len(prefs))
	}
}

func TestRepo_Search(t *testing.T) {
	setupMemoryTestDB(t)
	repo := NewRepo()
	repo.Create(&Memory{ID: NewID(), UserID: 1, Content: "BTC 关键支撑 62000"})
	repo.Create(&Memory{ID: NewID(), UserID: 1, Content: "ETH 阻力位 3200"})
	repo.Create(&Memory{ID: NewID(), UserID: 2, Content: "BTC 他人记忆"})

	hits, err := repo.Search(1, "BTC", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !strings.Contains(hits[0].Content, "62000") {
		t.Errorf("got %+v, want 仅本人 BTC 记忆", hits)
	}

	multi, err := repo.Search(1, "BTC 62000", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(multi) != 1 {
		t.Errorf("multi-word AND got %d hits, want 1", len(multi))
	}
}

func TestRepo_RecentForPromptBudget(t *testing.T) {
	setupMemoryTestDB(t)
	repo := NewRepo()
	repo.Create(&Memory{ID: NewID(), UserID: 1, Content: strings.Repeat("重", 600), Importance: 5})
	repo.Create(&Memory{ID: NewID(), UserID: 1, Content: "次要观察", Importance: 1})

	got, err := repo.RecentForPrompt(1, 800, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, m := range got {
		total += len(m.Content)
	}
	if total > 800 {
		t.Errorf("budget exceeded: %d chars", total)
	}
	if len(got) == 0 {
		t.Error("expected at least the high-importance memory")
	}
}

func TestRepo_Delete(t *testing.T) {
	setupMemoryTestDB(t)
	repo := NewRepo()
	m := &Memory{ID: NewID(), UserID: 1, Content: "待删除"}
	repo.Create(m)

	if err := repo.Delete(m.ID, 2); err != nil {
		t.Fatal(err)
	}
	list, _ := repo.ListByUser(1, "", 10)
	if len(list) != 1 {
		t.Error("他人删除不应生效")
	}
	if err := repo.Delete(m.ID, 1); err != nil {
		t.Fatal(err)
	}
	list, _ = repo.ListByUser(1, "", 10)
	if len(list) != 0 {
		t.Error("delete 未生效")
	}
}

func TestPlugin_RegisterAddsFourTools(t *testing.T) {
	p := &MemoryPlugin{Repo: NewRepo()}
	reg := plugin.NewRegistry()
	if err := p.Register(reg, plugin.Deps{}); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range reg.Tools() {
		names[tl.Name] = true
	}
	for _, want := range []string{"save_memory", "search_memory", "list_memories", "delete_memory"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
}

// origin 来源列：默认 manual，显式 agent/auto 透传；CountByOrigin / ExistsContent。
func TestRepo_OriginAndDedup(t *testing.T) {
	setupMemoryTestDB(t)
	repo := NewRepo()

	m1 := &Memory{ID: NewID(), UserID: 1, Content: "手动记忆"}
	if err := repo.Create(m1); err != nil {
		t.Fatalf("create: %v", err)
	}
	if m1.Origin != "manual" {
		t.Errorf("默认 origin = %q, want manual", m1.Origin)
	}
	if err := repo.Create(&Memory{ID: NewID(), UserID: 1, Content: "助手记录", Origin: "agent"}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := repo.Create(&Memory{ID: NewID(), UserID: 1, Content: "自动沉淀", Origin: "auto"}); err != nil {
		t.Fatalf("create auto: %v", err)
	}

	list, err := repo.ListByUser(1, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	origins := map[string]string{}
	for _, m := range list {
		origins[m.Content] = m.Origin
	}
	for content, want := range map[string]string{"手动记忆": "manual", "助手记录": "agent", "自动沉淀": "auto"} {
		if origins[content] != want {
			t.Errorf("%q origin = %q, want %q", content, origins[content], want)
		}
	}

	if n, _ := repo.CountByOrigin(1, "auto"); n != 1 {
		t.Errorf("CountByOrigin(auto) = %d, want 1", n)
	}
	if n, _ := repo.CountByOrigin(1, "agent"); n != 1 {
		t.Errorf("CountByOrigin(agent) = %d, want 1", n)
	}
	if n, _ := repo.CountByOrigin(2, "auto"); n != 0 {
		t.Errorf("他人 CountByOrigin = %d, want 0", n)
	}

	if ok, _ := repo.ExistsContent(1, "自动沉淀"); !ok {
		t.Error("ExistsContent(自动沉淀) = false, want true")
	}
	if ok, _ := repo.ExistsContent(1, "不存在"); ok {
		t.Error("ExistsContent(不存在) = true, want false")
	}
	if ok, _ := repo.ExistsContent(2, "自动沉淀"); ok {
		t.Error("他人 ExistsContent = true, want false")
	}
}
