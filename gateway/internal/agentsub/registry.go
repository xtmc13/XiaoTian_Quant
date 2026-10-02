package agentsub

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// ── 子代理运行登记（GET /agent/subagents 契约）──
// 进程内存环形缓冲：单进程部署，每用户上限 50 条（超 cap 丢最旧），重启即清空。

// RunRegistryCap 每用户保留的运行记录上限。
const RunRegistryCap = 50

// RunRecord 一次子代理运行的观测记录（JSON 字段与前端契约逐字对应）。
type RunRecord struct {
	ID           string `json:"id"`            // sa_<rand hex>
	Task         string `json:"task"`          // 子任务摘要（≤80 rune）
	Status       string `json:"status"`        // running | done | error
	StartedAt    int64  `json:"started_at"`    // unix 秒
	FinishedMs   int64  `json:"finished_ms"`   // 结束耗时（running 时为 0）
	ResultSummary string `json:"result_summary"` // 结果/错误摘要（≤160 rune）
}

var runRegistry = struct {
	sync.Mutex
	byUser map[int64][]*RunRecord // 每用户按开始时间升序
}{byUser: map[int64][]*RunRecord{}}

// StartRun 登记一条 running 记录（task 截 80 rune），返回供 FinishRun 回写。
func StartRun(userID int64, task string) *RunRecord {
	rec := &RunRecord{
		ID:        newRunID(),
		Task:      truncateRunes(task, 80),
		Status:    "running",
		StartedAt: time.Now().Unix(),
	}
	runRegistry.Lock()
	list := append(runRegistry.byUser[userID], rec)
	if len(list) > RunRegistryCap {
		list = list[len(list)-RunRegistryCap:]
	}
	runRegistry.byUser[userID] = list
	runRegistry.Unlock()
	return rec
}

// FinishRun 结束一条记录：failed=true 记 error，否则 done；摘要截 160 rune。
func FinishRun(rec *RunRecord, result string, failed bool) {
	if rec == nil {
		return
	}
	runRegistry.Lock()
	if failed {
		rec.Status = "error"
	} else {
		rec.Status = "done"
	}
	rec.FinishedMs = time.Since(time.Unix(rec.StartedAt, 0)).Milliseconds()
	rec.ResultSummary = truncateRunes(result, 160)
	runRegistry.Unlock()
}

// ListRuns 取该用户的运行记录（最新在前，上限 RunRegistryCap）。
func ListRuns(userID int64) []RunRecord {
	runRegistry.Lock()
	defer runRegistry.Unlock()
	list := runRegistry.byUser[userID]
	out := make([]RunRecord, 0, len(list))
	for i := len(list) - 1; i >= 0; i-- {
		out = append(out, *list[i])
	}
	return out
}

func newRunID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "sa_" + time.Now().Format("150405000000")
	}
	return "sa_" + hex.EncodeToString(buf)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
