package ml_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/ml"
)

// managedTestConfig 测试用托管配置：注入命令行，健康检查指向可控端口。
func managedTestConfig(command []string, port int) ml.ServerManagerConfig {
	return ml.ServerManagerConfig{
		Enabled:        true,
		Host:           "127.0.0.1",
		Port:           port,
		AdoptExisting:  false,
		StartupTimeout: 500 * time.Millisecond,
		HealthInterval: 50 * time.Millisecond,
		Command:        command,
	}
}

// TestServerManagerDisabled 未启用时 Start 为 no-op。
func TestServerManagerDisabled(t *testing.T) {
	mgr := ml.NewServerManager(ml.ServerManagerConfig{Enabled: false}, nil)
	mgr.Start()
	st := mgr.Status()
	if st.Managed || st.Running {
		t.Fatalf("disabled manager should be inert: %+v", st)
	}
	mgr.Stop() // 不 panic
}

// TestServerManagerLifecycle 长驻命令拉起 → Running，Stop 后退出。
func TestServerManagerLifecycle(t *testing.T) {
	// 健康端口指向一个必失败的空闲端口即可——本用例只验证进程生命周期
	mgr := ml.NewServerManager(managedTestConfig([]string{"sh", "-c", "sleep 30"}, 18990), nil)
	mgr.Start()
	defer mgr.Stop()

	waitFor(t, 3*time.Second, func() bool { return mgr.Status().Running }, "process running")
	st := mgr.Status()
	if st.PID <= 0 {
		t.Fatalf("expected pid > 0: %+v", st)
	}
	mgr.Stop()
	waitFor(t, 3*time.Second, func() bool { return !mgr.Status().Running }, "process stopped")
}

// TestServerManagerCrashRestart 立即退出的命令触发自动重启计数。
func TestServerManagerCrashRestart(t *testing.T) {
	mgr := ml.NewServerManager(managedTestConfig([]string{"sh", "-c", "exit 1"}, 18991), nil)
	mgr.Start()
	defer mgr.Stop()

	waitFor(t, 5*time.Second, func() bool { return mgr.Status().Restarts >= 1 }, "restart count >= 1")
}

// TestServerManagerAdoptExisting 端口已有健康实例时收养复用，不拉起子进程。
func TestServerManagerAdoptExisting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	cfg := managedTestConfig(nil, port)
	cfg.AdoptExisting = true
	mgr := ml.NewServerManager(cfg, nil)
	mgr.Start()
	defer mgr.Stop()

	waitFor(t, 2*time.Second, func() bool {
		st := mgr.Status()
		return st.Adopted && st.Healthy
	}, "adopted healthy external instance")
	if st := mgr.Status(); st.PID != 0 {
		t.Fatalf("adopted instance must not spawn process: %+v", st)
	}
}
