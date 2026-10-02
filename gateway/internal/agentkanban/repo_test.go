package agentkanban

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/plugin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func setupKanbanTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", t.TempDir()+"/gateway.db")
	t.Setenv("SECRET_KEY", "test-secret-key-agent-kanban")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(store.CloseDB)
}

func TestRepo_CRUDAndOrdering(t *testing.T) {
	setupKanbanTestDB(t)
	repo := NewRepo()

	// 三列各建一张；doing 卡最后触碰，列内应排最前
	mk := func(col, title string) *Card {
		c := &Card{ID: NewID(), UserID: 1, Title: title, Column: col}
		if err := repo.Create(c); err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
		return c
	}
	done1 := mk("done", "已完成一")
	doingOld := mk("doing", "进行中-旧")
	todo1 := mk("todo", "待办一")
	doingNew := mk("doing", "进行中-新")
	// 非法列回落 todo
	weird := mk("weird", "非法列")
	if weird.Column != "todo" {
		t.Errorf("非法列回落 = %q, want todo", weird.Column)
	}
	// 触碰 doingOld 使其 updated_at 最新（列内 DESC 校验）
	time.Sleep(time.Second)
	doingOld.Title = "进行中-旧-改"
	if err := repo.Update(doingOld); err != nil {
		t.Fatalf("update: %v", err)
	}

	all, err := repo.ListByUser(1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("got %d cards, want 5", len(all))
	}
	// 列序 todo → doing → done
	wantCols := []string{"todo", "todo", "doing", "doing", "done"}
	for i, c := range all {
		if c.Column != wantCols[i] {
			t.Errorf("cards[%d].Column = %q, want %q（全部: %+v）", i, c.Column, wantCols[i], all)
		}
	}
	// doing 列内 updated_at DESC：刚改过的在前
	if all[2].ID != doingOld.ID || all[3].ID != doingNew.ID {
		t.Errorf("doing 列内顺序错: %s, %s", all[2].ID, all[3].ID)
	}

	// 列过滤
	doing, err := repo.ListByUser(1, "doing")
	if err != nil {
		t.Fatal(err)
	}
	if len(doing) != 2 {
		t.Errorf("列过滤 got %d, want 2", len(doing))
	}

	// 用户隔离
	other, err := repo.ListByUser(2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Errorf("他人列表 got %d, want 0", len(other))
	}

	// 归属校验：他人改/删均 ErrNotFound
	if err := repo.Delete(todo1.ID, 2); !errors.Is(err, ErrNotFound) {
		t.Errorf("他人删除 err = %v, want ErrNotFound", err)
	}
	done1.UserID = 2
	if err := repo.Update(done1); !errors.Is(err, ErrNotFound) {
		t.Errorf("他人更新 err = %v, want ErrNotFound", err)
	}

	// 正常删除
	if err := repo.Delete(todo1.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetByID(todo1.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("删后 GetByID err = %v, want ErrNotFound", err)
	}
}

// TestTools_Lifecycle 通过注册表跑 add → list → move → complete 全生命周期。
func TestTools_Lifecycle(t *testing.T) {
	setupKanbanTestDB(t)
	p := &KanbanPlugin{Repo: NewRepo()}
	reg := plugin.NewRegistry()
	if err := p.Register(reg, plugin.Deps{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"kanban_add", "kanban_list", "kanban_move", "kanban_complete"} {
		if _, ok := reg.Handler(name); !ok {
			t.Fatalf("missing tool %s", name)
		}
	}
	tc := &agent.ToolContext{UserID: 7}
	call := func(name string, args map[string]any) any {
		t.Helper()
		h, _ := reg.Handler(name)
		out, err := h(tc, context.Background(), args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}

	added := call("kanban_add", map[string]any{"title": "回测网格策略", "description": "跑 3 组参数"}).(map[string]any)
	id, _ := added["id"].(string)
	if id == "" {
		t.Fatal("kanban_add 未返回 id")
	}

	cards := call("kanban_list", map[string]any{}).([]*Card)
	if len(cards) != 1 || cards[0].Column != "todo" || cards[0].CreatedBy != "agent" {
		t.Fatalf("add 后列表不符: %+v", cards)
	}

	call("kanban_move", map[string]any{"id": id, "column": "doing"})
	cards = call("kanban_list", map[string]any{"column": "doing"}).([]*Card)
	if len(cards) != 1 {
		t.Fatalf("move 后 doing 列 got %d, want 1", len(cards))
	}

	call("kanban_complete", map[string]any{"id": id, "comment": "最优参数年化 12%"})
	cards = call("kanban_list", map[string]any{"column": "done"}).([]*Card)
	if len(cards) != 1 || cards[0].Comment != "最优参数年化 12%" {
		t.Fatalf("complete 后不符: %+v", cards)
	}

	// 异常路径：非法列 / 空标题 / 他人卡
	if _, err := mustHandler(t, reg, "kanban_move")(tc, context.Background(), map[string]any{"id": id, "column": "weird"}); err == nil {
		t.Error("非法列未报错")
	}
	if _, err := mustHandler(t, reg, "kanban_add")(tc, context.Background(), map[string]any{}); err == nil {
		t.Error("空标题未报错")
	}
	other := &agent.ToolContext{UserID: 8}
	if _, err := mustHandler(t, reg, "kanban_complete")(other, context.Background(), map[string]any{"id": id}); err == nil {
		t.Error("他人 complete 未报错")
	}
}

func mustHandler(t *testing.T, reg *plugin.Registry, name string) plugin.ToolHandler {
	t.Helper()
	h, ok := reg.Handler(name)
	if !ok {
		t.Fatalf("missing handler %s", name)
	}
	return h
}
