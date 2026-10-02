package agentfeishu

import (
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// FeishuPlugin 飞书双向通道插件（channel 类，无工具，提供 UI 入口与生命周期）。
type FeishuPlugin struct {
	Repo *Repo
	Bot  *Bot
}

// Info 插件元信息。
func (p *FeishuPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "feishu",
		Version:     "1.0.0",
		Kind:        plugin.KindChannel,
		Description: "飞书双向通道：绑定后直接发消息使唤助手，定时结果投递到飞书",
		Builtin:     true,
	}
}

// UI 前端清单。
func (p *FeishuPlugin) UI() plugin.UIContribution {
	return plugin.UIContribution{
		Nav:   &plugin.NavItem{ID: "feishu", Label: "飞书", Icon: "feishu"},
		Slash: []plugin.SlashItem{{Name: "feishu", Description: "打开飞书接入面板"}},
	}
}

// Register 无工具贡献；通道能力由 webhook 与 Bot 生命周期承载。
func (p *FeishuPlugin) Register(_ *plugin.Registry, _ plugin.Deps) error { return nil }
