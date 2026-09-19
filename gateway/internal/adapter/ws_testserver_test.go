package adapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// wsTestServer 用 httptest + gorilla upgrader 模拟交易所 WS 端点。
type wsTestServer struct {
	srv       *httptest.Server
	connCount atomic.Int32
}

// newWSTestServer 启动模拟服务器；handler 在每个已升级连接的独立 goroutine 中
// 运行，connIdx 从 1 开始计数（用于验证重连）。
func newWSTestServer(t *testing.T, handler func(conn *websocket.Conn, connIdx int)) *wsTestServer {
	t.Helper()
	ts := &wsTestServer{}
	upgrader := websocket.Upgrader{}
	ts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		idx := int(ts.connCount.Add(1))
		go handler(conn, idx)
	}))
	t.Cleanup(ts.srv.Close)
	return ts
}

// wsURL 返回测试服务器对应的 ws:// 地址。
func (ts *wsTestServer) wsURL() string {
	return "ws" + strings.TrimPrefix(ts.srv.URL, "http")
}

// waitFor 轮询条件直到为真或超时。
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", msg)
}

// readJSON 从连接读一条文本消息并解析为 map（测试服务器侧 goroutine 使用；
// 出错直接返回 error，不在非测试 goroutine 里调用 t.Fatal）。
func readJSON(conn *websocket.Conn) (map[string]any, error) {
	_, msg, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(msg, &m); err != nil {
		return nil, err
	}
	return m, nil
}
