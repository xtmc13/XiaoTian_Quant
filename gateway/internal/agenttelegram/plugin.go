package agenttelegram

import (
	"os"

	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// TelegramPlugin Telegram 双向通道插件（channel 类，无工具，提供 UI 入口与生命周期）。
type TelegramPlugin struct {
	Repo *Repo
	Bot  *Bot
}

// Info 插件元信息。
func (p *TelegramPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "telegram",
		Version:     "1.0.0",
		Kind:        plugin.KindChannel,
		Description: "Telegram 双向通道：绑定后直接发消息使唤助手，定时结果投递到 TG",
		Builtin:     true,
	}
}

// UI 前端清单。
func (p *TelegramPlugin) UI() plugin.UIContribution {
	return plugin.UIContribution{
		Nav:   &plugin.NavItem{ID: "telegram", Label: "Telegram", Icon: "send"},
		Slash: []plugin.SlashItem{{Name: "telegram", Description: "打开 Telegram 接入面板"}},
	}
}

// Register 无工具贡献；通道能力由 Bot 生命周期承载。
func (p *TelegramPlugin) Register(_ *plugin.Registry, _ plugin.Deps) error { return nil }

// Configured Bot Token 是否已配置。
func Configured() bool { return os.Getenv("TELEGRAM_BOT_TOKEN") != "" }
