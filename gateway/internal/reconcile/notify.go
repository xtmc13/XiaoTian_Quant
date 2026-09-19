package reconcile

import (
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/ws"
)

// notifyDiff 对账差异/偏差的统一出口：
// 1) notify.Manager 异步投递到已配置渠道（log/email/lark/dingtalk/telegram）
// 2) NotificationStore 落库（前端通知中心可见，userID=0 系统广播）
// 3) WS 广播（channel=system，前端弹窗/标记）
// 三个出口都尽力而为，单个失败不影响主流程。
func notifyDiff(title, content, level, channel, eventType string, data map[string]any) {
	msg := notify.Message{
		Title:     title,
		Content:   content,
		Level:     level,
		Timestamp: 0,
		Tags:      map[string]string{"source": "reconcile", "channel": channel},
	}
	notify.GetManager().Send(msg)
	notify.GetNotificationStore().Add(title, content, level, "reconcile")

	if data == nil {
		data = map[string]any{}
	}
	ws.GetHub().Broadcast(ws.Message{
		Channel: channel,
		Type:    eventType,
		Data:    data,
	})
}
