package agentfiles

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func setupFilesTest(t *testing.T) *Plugin {
	t.Helper()
	t.Setenv("DB_PATH", t.TempDir()+"/gateway.db")
	t.Setenv("SECRET_KEY", "test-secret-key-agent-files")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(store.CloseDB)
	root, err := CanonicalRoot(t.TempDir() + "/sandbox")
	if err != nil {
		t.Fatalf("canonical root: %v", err)
	}
	return &Plugin{Root: root, Repo: NewRepo()}
}

func tc(userID uint64) *agent.ToolContext {
	return &agent.ToolContext{UserID: userID, ConversationID: "conv_x"}
}

// call 执行插件工具并断言无错误。
func call(t *testing.T, h func(*agent.ToolContext, context.Context, map[string]any) (any, error), uid uint64, args map[string]any) map[string]any {
	t.Helper()
	res, err := h(tc(uid), context.Background(), args)
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	m, _ := res.(map[string]any)
	return m
}

func TestResolve_RootConfinement(t *testing.T) {
	p := setupFilesTest(t)

	// 正常相对路径
	abs, rel, err := p.resolve("notes/a.txt")
	if err != nil || rel != "notes/a.txt" || !strings.HasPrefix(abs, p.Root) {
		t.Fatalf("resolve 正常路径: abs=%q rel=%q err=%v", abs, rel, err)
	}
	// 前导 / 视为根内写法
	if _, rel, err = p.resolve("/b.txt"); err != nil || rel != "b.txt" {
		t.Fatalf("resolve 根内绝对写法: rel=%q err=%v", rel, err)
	}
	// .. 逃逸必须拒绝
	for _, bad := range []string{"../outside.txt", "a/../../outside.txt", "/../../etc/passwd"} {
		if _, _, err := p.resolve(bad); err == nil {
			t.Errorf("resolve(%q) 应拒绝逃逸", bad)
		}
	}
	// 空路径
	if _, _, err := p.resolve("  "); err == nil {
		t.Error("空路径应报错")
	}

	// 符号链接指向根外：写入/读取都必须拒绝
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("top secret"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(p.Root, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.resolve("link.txt"); err == nil {
		t.Error("符号链接越界应拒绝")
	}
}

func TestReadWritePatchSearch(t *testing.T) {
	p := setupFilesTest(t)

	// write_file：自动建父目录；新建文件无检查点
	out := call(t, p.writeFile, 1, map[string]any{"path": "notes/hello.txt", "content": "hello world"})
	if out["written"] != true || out["path"] != "notes/hello.txt" {
		t.Fatalf("write_file 返回: %v", out)
	}
	if _, has := out["checkpoint_id"]; has {
		t.Error("新建文件不应产生检查点")
	}

	// read_file
	out = call(t, p.readFile, 1, map[string]any{"path": "notes/hello.txt"})
	if out["content"] != "hello world" || out["truncated"] != false {
		t.Fatalf("read_file 返回: %v", out)
	}

	// 覆盖写：产生检查点，内容为旧内容
	out = call(t, p.writeFile, 1, map[string]any{"path": "notes/hello.txt", "content": "hello v2"})
	cpID, _ := out["checkpoint_id"].(string)
	if !strings.HasPrefix(cpID, "cp_") {
		t.Fatalf("覆盖写应产生 cp_* 检查点: %v", out)
	}

	// patch：恰好一次替换成功，并产生检查点
	out = call(t, p.patch, 1, map[string]any{"path": "notes/hello.txt", "find": "v2", "replace": "v3"})
	if out["patched"] != true || !strings.HasPrefix(out["checkpoint_id"].(string), "cp_") {
		t.Fatalf("patch 返回: %v", out)
	}
	out = call(t, p.readFile, 1, map[string]any{"path": "notes/hello.txt"})
	if out["content"] != "hello v3" {
		t.Fatalf("patch 后内容: %v", out["content"])
	}

	// patch 0 次匹配报错
	if _, err := p.patch(tc(1), context.Background(), map[string]any{"path": "notes/hello.txt", "find": "不存在", "replace": "x"}); err == nil {
		t.Error("patch 0 匹配应报错")
	}
	// patch 多次匹配报错
	call(t, p.writeFile, 1, map[string]any{"path": "dup.txt", "content": "aa aa aa"})
	if _, err := p.patch(tc(1), context.Background(), map[string]any{"path": "dup.txt", "find": "aa", "replace": "b"}); err == nil {
		t.Error("patch 多匹配应报错")
	}

	// search_files：文件名匹配
	out = call(t, p.searchFiles, 1, map[string]any{"pattern": "hello"})
	if out["count"].(int) != 1 {
		t.Fatalf("search_files 名称匹配: %v", out)
	}
	// search_files：内容过滤给出 文件:行号
	out = call(t, p.searchFiles, 1, map[string]any{"pattern": "hello", "content": "v3"})
	if out["count"].(int) != 1 {
		t.Fatalf("search_files 内容匹配: %v", out)
	}
	matches, _ := out["matches"].([]searchHit)
	if len(matches) != 1 || matches[0].Path != "notes/hello.txt" || matches[0].Line != 1 || !strings.Contains(matches[0].Text, "v3") {
		t.Fatalf("search_files 内容命中字段: %+v", matches)
	}
	// search_files：仅 content 全文检索（pattern 留空，模型的常见用法）
	out = call(t, p.searchFiles, 1, map[string]any{"content": "v3"})
	if out["count"].(int) != 1 {
		t.Fatalf("search_files 全文检索: %v", out)
	}
	// search_files：别名容错（query 当作 pattern）
	out = call(t, p.searchFiles, 1, map[string]any{"query": "hello"})
	if out["count"].(int) != 1 {
		t.Fatalf("search_files query 别名: %v", out)
	}
	// 两者皆空应报错且附用法提示
	if _, err := p.searchFiles(tc(1), context.Background(), map[string]any{}); err == nil {
		t.Error("空参数应报错")
	}
	// 检查点备份目录不参与检索
	out = call(t, p.searchFiles, 1, map[string]any{"pattern": "cp_"})
	if out["count"].(int) != 0 {
		t.Errorf("备份目录不应被检索到: %v", out)
	}

	// 越界路径工具层同样拒绝
	if _, err := p.readFile(tc(1), context.Background(), map[string]any{"path": "../x.txt"}); err == nil {
		t.Error("read_file 越界应报错")
	}
}

func TestCheckpointsAndRollback(t *testing.T) {
	p := setupFilesTest(t)

	call(t, p.writeFile, 7, map[string]any{"path": "doc.txt", "content": "v1"})
	out := call(t, p.writeFile, 7, map[string]any{"path": "doc.txt", "content": "v2"})
	cpID := out["checkpoint_id"].(string)

	// 列表契约字段
	cps, err := p.Repo.ListByUser(7, 100)
	if err != nil || len(cps) != 1 {
		t.Fatalf("ListByUser: n=%d err=%v", len(cps), err)
	}
	cp := cps[0]
	if cp.ID != cpID || cp.Path != "doc.txt" || cp.Size != int64(len("v1")) || cp.ConversationID != "conv_x" || cp.CreatedAt <= 0 {
		t.Fatalf("检查点字段: %+v", cp)
	}
	// 他人不可见
	if cps, _ = p.Repo.ListByUser(8, 100); len(cps) != 0 {
		t.Error("他人检查点应不可见")
	}

	// 回滚恢复旧内容
	restored, err := p.Rollback(7, cpID)
	if err != nil || restored != "doc.txt" {
		t.Fatalf("Rollback: restored=%q err=%v", restored, err)
	}
	data, _ := os.ReadFile(filepath.Join(p.Root, "doc.txt"))
	if string(data) != "v1" {
		t.Fatalf("回滚后内容 = %q, want v1", data)
	}
	// 检查点行保留
	if cps, _ = p.Repo.ListByUser(7, 100); len(cps) != 1 {
		t.Error("回滚后检查点行应保留")
	}
	// 他人不可回滚；不存在 id 报错
	if _, err := p.Rollback(8, cpID); err != ErrCheckpointNotFound {
		t.Errorf("他人回滚应 ErrCheckpointNotFound: %v", err)
	}
	if _, err := p.Rollback(7, "cp_404"); err != ErrCheckpointNotFound {
		t.Errorf("不存在回滚应 ErrCheckpointNotFound: %v", err)
	}
}

func TestCheckpointPrune(t *testing.T) {
	p := setupFilesTest(t)

	call(t, p.writeFile, 9, map[string]any{"path": "p.txt", "content": "v0"})
	// 连续覆盖 105 次，只保留最近 100 个检查点
	for i := 1; i <= 105; i++ {
		call(t, p.writeFile, 9, map[string]any{"path": "p.txt", "content": fmt.Sprintf("v%d", i)})
	}
	cps, err := p.Repo.ListByUser(9, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(cps) != keepCheckpoints {
		t.Fatalf("剪枝后检查点 = %d, want %d", len(cps), keepCheckpoints)
	}
	// 被剪掉的备份文件已删除
	backups, _ := os.ReadDir(filepath.Join(p.Root, checkpointsDir))
	if len(backups) != keepCheckpoints {
		t.Errorf("备份文件数 = %d, want %d", len(backups), keepCheckpoints)
	}
}
