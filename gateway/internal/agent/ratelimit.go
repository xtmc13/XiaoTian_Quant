package agent

import (
	"sync"
	"time"
)

// ── Agent token 限流（rate_limit_rps 落地）──
// 单进程内存实现：1 秒滑动窗口，按 token id 计数。多副本部署需换集中式限流
// （当前网关单进程运行，进程重启窗口清零，可接受）。

// agentChatRateWindow 滑动窗口长度。
const agentChatRateWindow = time.Second

var agentChatLimiter = struct {
	sync.Mutex
	hits map[int][]time.Time // tokenID → 窗口内请求时间戳（升序）
}{hits: map[int][]time.Time{}}

// AllowAgentChatRequest 判定该 token 的本次 /agent/chat 请求是否放行；
// rps <= 0 表示不限流。放行时把本次请求计入窗口。
func AllowAgentChatRequest(tokenID, rps int) bool {
	if rps <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-agentChatRateWindow)

	agentChatLimiter.Lock()
	defer agentChatLimiter.Unlock()
	kept := agentChatLimiter.hits[tokenID][:0]
	for _, ts := range agentChatLimiter.hits[tokenID] {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= rps {
		agentChatLimiter.hits[tokenID] = kept
		return false
	}
	agentChatLimiter.hits[tokenID] = append(kept, now)
	return true
}
