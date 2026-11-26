package adapter

import (
	"errors"
	"net"
	"net/url"
	"testing"
)

func TestIsUncertainSubmitError(t *testing.T) {
	// 传输层包装错误：不确定。
	urlErr := &url.Error{Op: "Post", URL: "https://api.binance.com/order", Err: errors.New("connection reset by peer")}
	if !IsUncertainSubmitError(urlErr) {
		t.Fatal("url.Error 传输层失败应判定为不确定")
	}

	// net.Error 超时：不确定。
	var nerr net.Error = &timeoutError{}
	if !IsUncertainSubmitError(nerr) {
		t.Fatal("net.Error 超时应判定为不确定")
	}

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"5xx", errors.New("binance POST /order: HTTP 500 code=0 internal"), true},
		{"429", errors.New("binance POST /order: HTTP 429 code=-1003 rate limit"), true},
		{"4xx business reject", errors.New("binance POST /order: HTTP 400 code=-2010 insufficient balance"), false},
		{"local validation", errors.New("symbol is required"), false},
	}
	for _, c := range cases {
		if got := IsUncertainSubmitError(c.err); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
