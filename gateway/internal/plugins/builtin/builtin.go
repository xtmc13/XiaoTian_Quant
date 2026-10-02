// Package builtin 装配全部内置插件（万物皆可插件的第一方实现）。
package builtin

import (
	"os"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/agentcron"
	"github.com/xiaotian-quant/gateway/internal/agentmemory"
	"github.com/xiaotian-quant/gateway/internal/agentskills"
	"github.com/xiaotian-quant/gateway/internal/agentsub"
	"github.com/xiaotian-quant/gateway/internal/agenttelegram"
	"github.com/xiaotian-quant/gateway/internal/plugin"
)

// descriptor 纯描述型插件（如 trading-tools：能力在 agent 核心包，此处登记清单）。
type descriptor struct {
	info plugin.Info
	ui   plugin.UIContribution
}

func (d descriptor) Info() plugin.Info                 { return d.info }
func (d descriptor) UI() plugin.UIContribution          { return d.ui }
func (d descriptor) Register(*plugin.Registry, plugin.Deps) error { return nil }

var (
	once      sync.Once
	mgr       *plugin.Manager
	cronRepo  *agentcron.Repo
	cronSched *agentcron.Scheduler
	tgBot     *agenttelegram.Bot
	subPlugin *agentsub.SubagentsPlugin
	buildErr  error
)

func build() {
	cronRepo = agentcron.NewRepo()
	cronSched = agentcron.NewScheduler(cronRepo, nil) // executor 由 main 注入
	tgBot = agenttelegram.NewBot(os.Getenv("TELEGRAM_BOT_TOKEN"), agenttelegram.NewRepo(), nil)
	subPlugin = &agentsub.SubagentsPlugin{}
	mgr, buildErr = plugin.NewManager(plugin.Deps{},
		descriptor{
			info: plugin.Info{
				Name:        "trading-tools",
				Version:     "1.0.0",
				Kind:        plugin.KindTools,
				Description: "交易核心工具集：行情/K线/下单/策略/回测（16 个）",
				Builtin:     true,
			},
		},
		&agentcron.CronPlugin{Repo: cronRepo, Sched: cronSched},
		&agentmemory.MemoryPlugin{Repo: agentmemory.NewRepo()},
		&agentskills.SkillsPlugin{Repo: agentskills.NewRepo()},
		&agenttelegram.TelegramPlugin{Repo: agenttelegram.NewRepo(), Bot: tgBot},
		subPlugin,
	)
}

// Manager 内置插件管理器单例。
func Manager() *plugin.Manager {
	once.Do(build)
	return mgr
}

// CronScheduler 定时任务调度器单例。
func CronScheduler() *agentcron.Scheduler {
	once.Do(build)
	return cronSched
}

// SetCronExecutor 注入到点执行器（handler 的 headless runner，避免 import 环）。
func SetCronExecutor(exec agentcron.Executor) {
	once.Do(build)
	cronSched.SetExecutor(exec)
}

// TelegramBot 入站机器人单例。
func TelegramBot() *agenttelegram.Bot {
	once.Do(build)
	return tgBot
}

// SetTelegramExecutor 注入入站执行器并启动轮询。
func SetTelegramExecutor(exec agenttelegram.Executor) {
	once.Do(build)
	tgBot.SetExecutor(exec)
	tgBot.Start()
}

// SetScheduleResolver 注入自然语言 → cron 解析器（cron 插件用）。
func SetScheduleResolver(resolver func(string) (string, error)) {
	once.Do(build)
	for _, p := range mgr.Plugins() {
		if cp, ok := p.(*agentcron.CronPlugin); ok {
			cp.ScheduleResolver = resolver
		}
	}
}

// SetSubagentExecutor 注入子代理执行器（handler.RunAgentSub，只读 scope）。
func SetSubagentExecutor(exec agentsub.SubagentExecutor) {
	once.Do(build)
	subPlugin.Exec = exec
}

// BuildError 装配期错误（启动时检查）。
func BuildError() error {
	once.Do(build)
	return buildErr
}
