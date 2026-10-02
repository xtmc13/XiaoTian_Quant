package agentdingtalk

import (
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// DingtalkPlugin 钉钉双向通道插件（channel 类，无工具，提供 UI 入口与生命周期）。
type DingtalkPlugin struct {
	Repo *Repo
	Bot  *Bot
}

// Info 插件元信息。
func (p *DingtalkPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "dingtalk",
		Version:     "1.0.0",
		Kind:        plugin.KindChannel,
		Description: "钉钉双向通道：绑定后直接发消息使唤助手，定时结果投递到钉钉",
		Builtin:     true,
	}
}

// UI 前端清单。
func (p *DingtalkPlugin) UI() plugin.UIContribution {
	return plugin.UIContribution{
		Nav:   &plugin.NavItem{ID: "dingtalk", Label: "钉钉", Icon: "dingtalk"},
		Slash: []plugin.SlashItem{{Name: "dingtalk", Description: "打开钉钉接入面板"}},
	}
}

// Register 无工具贡献；通道能力由 webhook 与 Bot 生命周期承载。
func (p *DingtalkPlugin) Register(_ *plugin.Registry, _ plugin.Deps) error { return nil }
