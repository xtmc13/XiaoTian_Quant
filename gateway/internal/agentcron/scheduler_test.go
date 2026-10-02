package agentcron

import (
	"errors"
	"sync"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/plugin"
	"time"
)

type fakeStore struct {
	mu     sync.Mutex
	jobs   map[string]*Job
	marked []markRecord
}

type markRecord struct {
	id       string
	status   string
	result   string
	nextRun  int64
	disabled bool
}

func newFakeStore(jobs ...*Job) *fakeStore {
	fs := &fakeStore{jobs: map[string]*Job{}}
	for _, j := range jobs {
		fs.jobs[j.ID] = j
	}
	return fs
}

func (f *fakeStore) DueJobs(now int64, limit int) ([]*Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*Job{}
	for _, j := range f.jobs {
		if j.Enabled && j.NextRunAt <= now {
			out = append(out, j)
		}
	}
	return out, nil
}

func (f *fakeStore) Get(id string, userID int64) (*Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[id]
	if !ok || j.UserID != userID {
		return nil, errors.New("not found")
	}
	return j, nil
}

func (f *fakeStore) SetEnabled(id string, userID int64, enabled bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if j, ok := f.jobs[id]; ok && j.UserID == userID {
		j.Enabled = enabled
	}
	return nil
}

func (f *fakeStore) MarkRun(id, status, result string, lastRunAt, nextRunAt int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if j, ok := f.jobs[id]; ok {
		j.LastStatus, j.LastResult, j.LastRunAt, j.NextRunAt = status, result, lastRunAt, nextRunAt
	}
	f.marked = append(f.marked, markRecord{id: id, status: status, result: result, nextRun: nextRunAt, disabled: !f.jobs[id].Enabled})
	return nil
}

func TestScheduler_RunExecutesAndMarks(t *testing.T) {
	fs := newFakeStore(&Job{
		ID: "j1", UserID: 7, Name: "测试", Prompt: "看一眼 BTC",
		Schedule: "0 8 * * *", Timezone: "Asia/Shanghai", Channel: "web",
		Enabled: true, NextRunAt: time.Now().Unix() - 10,
	})
	s := NewScheduler(fs, func(userID int64, prompt string) (string, error) {
		if userID != 7 || prompt != "看一眼 BTC" {
			t.Errorf("unexpected args: %d %q", userID, prompt)
		}
		return "回复内容", nil
	})

	j, err := fs.Get("j1", 7)
	if err != nil {
		t.Fatal(err)
	}
	s.run(j)

	fs.mu.Lock()
	defer fs.mu.Unlock()
	got := fs.jobs["j1"]
	if got.LastStatus != "ok" {
		t.Errorf("status = %q, want ok", got.LastStatus)
	}
	if got.LastResult != "回复内容" {
		t.Errorf("result = %q", got.LastResult)
	}
	if got.NextRunAt <= time.Now().Unix() {
		t.Errorf("next run not advanced: %d", got.NextRunAt)
	}
}

func TestScheduler_ExecErrorMarked(t *testing.T) {
	fs := newFakeStore(&Job{ID: "j2", UserID: 1, Name: "x", Schedule: "0 8 * * *", Enabled: true, NextRunAt: 1})
	s := NewScheduler(fs, func(int64, string) (string, error) { return "", errors.New("boom") })

	j, _ := fs.Get("j2", 1)
	s.run(j)

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.jobs["j2"].LastStatus != "error" {
		t.Errorf("status = %q, want error", fs.jobs["j2"].LastStatus)
	}
}

func TestScheduler_InvalidSpecDisables(t *testing.T) {
	// 非法表达式：任务执行后会被停用，避免每分钟重试
	fs := newFakeStore(&Job{ID: "j3", UserID: 1, Name: "x", Schedule: "bad", Enabled: true, NextRunAt: 1})
	s := NewScheduler(fs, func(int64, string) (string, error) { return "ok", nil })

	j, _ := fs.Get("j3", 1)
	s.run(j)

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.jobs["j3"].Enabled {
		t.Error("job should be disabled on invalid spec")
	}
}

func TestScheduler_NilExecutor(t *testing.T) {
	fs := newFakeStore(&Job{ID: "j4", UserID: 1, Name: "x", Schedule: "0 8 * * *", Enabled: true, NextRunAt: 1})
	s := NewScheduler(fs, nil)

	j, _ := fs.Get("j4", 1)
	s.run(j)

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.jobs["j4"].LastStatus != "error" {
		t.Errorf("status = %q, want error", fs.jobs["j4"].LastStatus)
	}
}

func TestCronPlugin_RegisterAddsFiveTools(t *testing.T) {
	fs := newFakeStore()
	s := NewScheduler(fs, nil)
	p := &CronPlugin{Repo: NewRepo(), Sched: s}

	reg := plugin.NewRegistry()
	if err := p.Register(reg, plugin.Deps{}); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tl := range reg.Tools() {
		names = append(names, tl.Name)
	}
	want := []string{"create_scheduled_job", "list_scheduled_jobs", "toggle_scheduled_job", "delete_scheduled_job", "run_scheduled_job"}
	for _, w := range want {
		found := false
		for _, n := range names {
			if n == w {
				found = true
			}
		}
		if !found {
			t.Errorf("missing tool %s", w)
		}
	}
	if len(names) != 5 {
		t.Errorf("got %d tools, want 5", len(names))
	}
}
