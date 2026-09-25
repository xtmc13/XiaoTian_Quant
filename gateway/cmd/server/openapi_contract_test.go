package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/apispec"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// TestMain 为 cmd/server 包测试准备隔离的临时 sqlite（沿用 internal/handler
// 测试设施的模式）：绝不触碰 ./runtime/gateway.db。setupRoutes 依赖
// store.GetAIBotCatalog / social.MarketService 等需要 DB 句柄的调用。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	dir, err := os.MkdirTemp("", "server_route_test")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("DB_PATH", filepath.Join(dir, "gateway.db"))
	_ = os.Setenv("CONFIG_PATH", filepath.Join(dir, "test_server_config.yaml"))
	_ = os.Setenv("SECRET_KEY", "test-secret-key-not-for-production-use-only")
	store.VaultFilePath = filepath.Join(dir, "credentials_vault.json")
	store.VaultKeyPath = filepath.Join(dir, ".vault_key")
	if err := store.InitDB(); err != nil {
		panic(err)
	}
	code := m.Run()
	store.CloseDB()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// TestOpenAPIContractMatchesRuntimeRoutes 契约守护的核心断言：
// gin 运行期实际注册的路由集合（method+path）必须与 docs/api/openapi.yaml
// 中的路径集合完全一致。任何"裸奔"的新路由（改了 router.go 但没重新生成
// 契约）都会在这里失败；重新生成命令：
//
//	cd gateway && go run ./cmd/apidump -openapi ../docs/api/openapi.yaml
func TestOpenAPIContractMatchesRuntimeRoutes(t *testing.T) {
	// 与生成器对齐：静态提取无条件包含全部路由（含条件挂载的 /metrics、
	// /api/docs、/debug/pprof），测试里把条件全部打开，使运行期集合 ==
	// 静态全集。ServerMode=debug 同理（pprof 注册是无条件的，debug 只是
	// 跳过 localhost 守卫，不影响路由树）。
	t.Setenv("PROMETHEUS_ENABLED", "true")
	t.Setenv("API_DOCS_ENABLED", "true")

	r := gin.New()
	setupRoutes(r, &serverConfig{ServerMode: "debug"})

	runtime := map[string]bool{}
	for _, ri := range r.Routes() {
		key := ri.Method + " " + apispec.GinToOpenAPIPath(ri.Path)
		if runtime[key] {
			t.Fatalf("运行期路由重复注册: %s", key)
		}
		runtime[key] = true
	}

	specPath, err := apispec.FindSpecFile()
	if err != nil {
		t.Fatalf("定位契约文件失败: %v", err)
	}
	specOps, err := apispec.LoadSpecOperations(specPath)
	if err != nil {
		t.Fatalf("读取契约文件失败: %v", err)
	}
	spec := map[string]bool{}
	for p, methods := range specOps {
		for m := range methods {
			key := m + " " + p
			if spec[key] {
				t.Fatalf("契约中 operation 重复: %s", key)
			}
			spec[key] = true
		}
	}

	var missingInSpec, missingAtRuntime []string
	for key := range runtime {
		if !spec[key] {
			missingInSpec = append(missingInSpec, key)
		}
	}
	for key := range spec {
		if !runtime[key] {
			missingAtRuntime = append(missingAtRuntime, key)
		}
	}
	sort.Strings(missingInSpec)
	sort.Strings(missingAtRuntime)

	if len(missingInSpec) > 0 {
		t.Errorf("运行期已注册但契约缺失 %d 条（请重新生成契约）:\n  %s",
			len(missingInSpec), strings.Join(missingInSpec, "\n  "))
	}
	if len(missingAtRuntime) > 0 {
		t.Errorf("契约中存在但运行期未注册 %d 条（请重新生成契约）:\n  %s",
			len(missingAtRuntime), strings.Join(missingAtRuntime, "\n  "))
	}
	if t.Failed() {
		t.Fatal("契约漂移：cd gateway && go run ./cmd/apidump -openapi ../docs/api/openapi.yaml")
	}
	t.Logf("契约一致：运行期 %d 条路由 == openapi.yaml %d 个 operation", len(runtime), len(spec))
}

// TestGinToOpenAPIPath 路径转换约定（提取器与契约测试共用同一函数，
// 这里锁定语义防止意外改动）。
func TestGinToOpenAPIPath(t *testing.T) {
	cases := map[string]string{
		"/api/orders/:order_id":  "/api/orders/{order_id}",
		"/debug/pprof/*any":      "/debug/pprof/{any}",
		"/api/pystrategies/":     "/api/pystrategies/",
		"/api/factors":           "/api/factors",
		"/api/strategies/:id/:x": "/api/strategies/{id}/{x}",
		"/api/klines/:symbol":    "/api/klines/{symbol}",
	}
	for in, want := range cases {
		if got := apispec.GinToOpenAPIPath(in); got != want {
			t.Errorf("GinToOpenAPIPath(%q) = %q, want %q", in, got, want)
		}
	}
	// JoinGinPath 复刻 gin joinPaths：尾斜杠保留、空 rel 原样返回。
	joinCases := []struct{ base, rel, want string }{
		{"/api/pystrategies", "/", "/api/pystrategies/"},
		{"/api/factors", "", "/api/factors"},
		{"/api", "/auth/login", "/api/auth/login"},
		{"/api/factors", "/:name/values", "/api/factors/:name/values"},
	}
	for _, c := range joinCases {
		if got := apispec.JoinGinPath(c.base, c.rel); got != c.want {
			t.Errorf("JoinGinPath(%q, %q) = %q, want %q", c.base, c.rel, got, c.want)
		}
	}
}

// TestRouteCountSanity 规模哨兵：路由总数不得意外大幅缩水（提取器静默漏抓
// 时契约测试会因双向 diff 失败，此条给出一个更直观的数量级断言）。
func TestRouteCountSanity(t *testing.T) {
	t.Setenv("PROMETHEUS_ENABLED", "true")
	t.Setenv("API_DOCS_ENABLED", "true")
	r := gin.New()
	setupRoutes(r, &serverConfig{ServerMode: "debug"})
	if n := len(r.Routes()); n < 400 {
		t.Fatalf("运行期路由数 %d < 400，疑似注册链路损坏", n)
	}
}

// TestAPIDocsEndpoints /api/docs 在 API_DOCS_ENABLED=true 时应可用：
// HTML 页面与 openapi.yaml 文件服务都返回 200。
func TestAPIDocsEndpoints(t *testing.T) {
	t.Setenv("API_DOCS_ENABLED", "true")
	r := gin.New()
	setupRoutes(r, &serverConfig{ServerMode: "debug"})

	for _, path := range []string{"/api/docs", "/api/docs/openapi.yaml"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, w.Code)
		}
	}
}
