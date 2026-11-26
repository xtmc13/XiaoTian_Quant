package adapter

import (
	"math/rand"
	"time"
)

// RetryPolicy 429/5xx/传输层不确定错误的指数退避策略。
type RetryPolicy struct {
	MaxRetries int           // 最大额外重试次数（不含首次）
	BaseDelay  time.Duration // 起步退避
	MaxDelay   time.Duration // 退避封顶
}

// DefaultRetryPolicy 保守默认：最多 2 次额外重试，300ms 起步，5s 封顶。
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries: 2,
		BaseDelay:  300 * time.Millisecond,
		MaxDelay:  5 * time.Second,
	}
}

// Delay 返回第 attempt 次重试（0 起）的退避时长：2^attempt 指数增长、封顶
// MaxDelay，并叠加 ±10% 抖动避免同步重试风暴。
func (p RetryPolicy) Delay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	base := p.BaseDelay << attempt
	if base <= 0 || base > p.MaxDelay {
		base = p.MaxDelay
	}
	jitter := time.Duration(rand.Int63n(int64(base) / 5)) // 0~20% base
	// ±10% 抖动：base * (0.9 ~ 1.1)
	return base - base/10 + jitter
}

// ShouldRetry 判定该错误是否值得按退避重试：与 IsUncertainSubmitError 同口径
// （传输层失败 / 429 / 5xx）；确定性 4xx 业务拒绝不重试。
func (p RetryPolicy) ShouldRetry(err error) bool {
	return IsUncertainSubmitError(err)
}
