package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// flushRecorder 实现 http.Flusher，用于验证 Flush 委托。
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed int
}

func (f *flushRecorder) Flush() { f.flushed++ }

// 包装的 responseWriter 必须实现 http.Flusher，
// 否则 SSE 处理器（gin c.Writer.Flush()）会触发 interface conversion panic（生产实测）。
func TestResponseWriterImplementsFlusher(t *testing.T) {
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	var w http.ResponseWriter = &responseWriter{ResponseWriter: rec, statusCode: 200}

	f, ok := w.(http.Flusher)
	if !ok {
		t.Fatal("responseWriter 未实现 http.Flusher")
	}
	f.Flush()
	if rec.flushed != 1 {
		t.Fatalf("Flush 未委托到底层 writer, flushed=%d", rec.flushed)
	}
}

// Hijack 既有能力保持：底层不支持时返回错误而非 panic。
func TestResponseWriterHijackUnsupported(t *testing.T) {
	var w http.ResponseWriter = &responseWriter{ResponseWriter: httptest.NewRecorder(), statusCode: 200}
	h, ok := w.(http.Hijacker)
	if !ok {
		t.Fatal("responseWriter 未实现 http.Hijacker")
	}
	if _, _, err := h.Hijack(); err == nil {
		t.Fatal("底层不支持 Hijack 时应返回错误")
	}
}

// ReadFrom 委托：内容必须写入底层。
func TestResponseWriterReadFrom(t *testing.T) {
	rec := httptest.NewRecorder()
	var w http.ResponseWriter = &responseWriter{ResponseWriter: rec, statusCode: 200}
	rf, ok := w.(io.ReaderFrom)
	if !ok {
		t.Fatal("responseWriter 未实现 io.ReaderFrom")
	}
	if _, err := rf.ReadFrom(strings.NewReader("hello")); err != nil {
		t.Fatalf("ReadFrom err = %v", err)
	}
	if rec.Body.String() != "hello" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

// Push：底层不支持时返回 http.ErrNotSupported 而非 panic。
func TestResponseWriterPushUnsupported(t *testing.T) {
	var w http.ResponseWriter = &responseWriter{ResponseWriter: httptest.NewRecorder(), statusCode: 200}
	p, ok := w.(http.Pusher)
	if !ok {
		t.Fatal("responseWriter 未实现 http.Pusher")
	}
	if err := p.Push("/x", nil); err != http.ErrNotSupported {
		t.Fatalf("err = %v", err)
	}
}
