package agentqq

import (
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// QQPlugin QQ 双向通道插件（channel 类，无工具，提供 UI 入口与生命周期）。
type QQPlugin struct {
	Repo *Repo
	Bot  *Bot
}

// Info 插件元信息。
func (p *QQPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "qq",
		Version:     "1.0.0",
		Kind:        plugin.KindChannel,
		Description: "QQ 双向通道（官方开放平台 WSS）：绑定后直接发消息使唤助手，定时结果投递到 QQ",
		Builtin:     true,
	}
}

// UI 前端清单。
func (p *QQPlugin) UI() plugin.UIContribution {
	return plugin.UIContribution{
		Nav:   &plugin.NavItem{ID: "qq", Label: "QQ", Icon: "qq"},
		Slash: []plugin.SlashItem{{Name: "qq", Description: "打开 QQ 接入面板"}},
	}
}

// Register 无工具贡献；通道能力由 Bot 生命周期承载。
func (p *QQPlugin) Register(_ *plugin.Registry, _ plugin.Deps) error { return nil }
