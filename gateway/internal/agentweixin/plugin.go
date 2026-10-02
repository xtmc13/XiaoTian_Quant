package agentweixin

import (
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// WeixinPlugin 微信双向通道插件（channel 类，无工具，提供 UI 入口与生命周期）。
// 导航排在企业微信（wecom）之前。
type WeixinPlugin struct {
	Repo *Repo
	Bot  *Bot
}

// Info 插件元信息。
func (p *WeixinPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "weixin",
		Version:     "1.0.0",
		Kind:        plugin.KindChannel,
		Description: "微信双向通道（腾讯官方 iLink Bot API）：扫码登录后绑定发消息使唤助手，定时结果投递到微信",
		Builtin:     true,
	}
}

// UI 前端清单。
func (p *WeixinPlugin) UI() plugin.UIContribution {
	return plugin.UIContribution{
		Nav:   &plugin.NavItem{ID: "weixin", Label: "微信", Icon: "weixin"},
		Slash: []plugin.SlashItem{{Name: "weixin", Description: "打开微信接入面板"}},
	}
}

// Register 无工具贡献；通道能力由 Bot 生命周期承载。
func (p *WeixinPlugin) Register(_ *plugin.Registry, _ plugin.Deps) error { return nil }
