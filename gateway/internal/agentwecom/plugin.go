package agentwecom

import (
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// WecomPlugin 企业微信双向通道插件（channel 类，无工具，提供 UI 入口与生命周期）。
type WecomPlugin struct {
	Repo *Repo
	Bot  *Bot
}

// Info 插件元信息。
func (p *WecomPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "wecom",
		Version:     "1.0.0",
		Kind:        plugin.KindChannel,
		Description: "企业微信双向通道（回调模式）：绑定后直接发消息使唤助手，定时结果投递到企业微信",
		Builtin:     true,
	}
}

// UI 前端清单。
func (p *WecomPlugin) UI() plugin.UIContribution {
	return plugin.UIContribution{
		Nav:   &plugin.NavItem{ID: "wecom", Label: "企业微信", Icon: "wecom"},
		Slash: []plugin.SlashItem{{Name: "wecom", Description: "打开企业微信接入面板"}},
	}
}

// Register 无工具贡献；通道能力由 webhook 与 Bot 生命周期承载。
func (p *WecomPlugin) Register(_ *plugin.Registry, _ plugin.Deps) error { return nil }
