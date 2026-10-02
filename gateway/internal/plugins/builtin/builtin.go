// Package builtin 装配全部内置插件（万物皆可插件的第一方实现）。
package builtin

import (
	"os"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/agentcron"
	"github.com/xiaotian-quant/gateway/internal/agentdingtalk"
	"github.com/xiaotian-quant/gateway/internal/agentfeishu"
	"github.com/xiaotian-quant/gateway/internal/agentfiles"
	"github.com/xiaotian-quant/gateway/internal/agentkanban"
	"github.com/xiaotian-quant/gateway/internal/agentmcp"
	"github.com/xiaotian-quant/gateway/internal/agentmemory"
	"github.com/xiaotian-quant/gateway/internal/agentqq"
	"github.com/xiaotian-quant/gateway/internal/agentskills"
	"github.com/xiaotian-quant/gateway/internal/agentsub"
	"github.com/xiaotian-quant/gateway/internal/agenttelegram"
	"github.com/xiaotian-quant/gateway/internal/agentwebsearch"
	"github.com/xiaotian-quant/gateway/internal/agentwecom"
	"github.com/xiaotian-quant/gateway/internal/plugin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// descriptor 纯描述型插件（如 trading-tools：能力在 agent 核心包，此处登记清单）。
type descriptor struct {
	info plugin.Info
	ui   plugin.UIContribution
}

func (d descriptor) Info() plugin.Info                            { return d.info }
func (d descriptor) UI() plugin.UIContribution                    { return d.ui }
func (d descriptor) Register(*plugin.Registry, plugin.Deps) error { return nil }

var (
	once        sync.Once
	mgr         *plugin.Manager
	cronRepo    *agentcron.Repo
	cronSched   *agentcron.Scheduler
	tgBot       *agenttelegram.Bot
	feishuBot   *agentfeishu.Bot
	dingtalkBot *agentdingtalk.Bot
	qqBot       *agentqq.Bot
	wecomBot    *agentwecom.Bot
	subPlugin   *agentsub.SubagentsPlugin
	filesPlugin *agentfiles.Plugin
	buildErr    error
)

func build() {
	cronRepo = agentcron.NewRepo()
	cronSched = agentcron.NewScheduler(cronRepo, nil) // executor 由 main 注入
	tgBot = agenttelegram.NewBot(os.Getenv("TELEGRAM_BOT_TOKEN"), agenttelegram.NewRepo(), nil)
	// cron 结果每用户 TG 直发（查绑定表）；未绑定/未配置返回 false 由调度器回落全局 notify
	cronSched.SetTelegramSender(tgBot.SendToUser)
	// 飞书 / 钉钉双向通道（webhook 模式，无轮询生命周期）；cron 每用户直发同上回落语义
	feishuBot = agentfeishu.NewBot(agentfeishu.NewRepo())
	cronSched.SetFeishuSender(feishuBot.SendToUser)
	dingtalkBot = agentdingtalk.NewBot(agentdingtalk.NewRepo())
	cronSched.SetDingtalkSender(dingtalkBot.SendToUser)
	// QQ（官方 WSS 网关）/ 企业微信（回调模式）双向通道；cron 每用户直发同上回落语义
	qqBot = agentqq.NewBot(agentqq.NewRepo())
	cronSched.SetQqSender(qqBot.SendToUser)
	wecomBot = agentwecom.NewBot(agentwecom.NewRepo())
	cronSched.SetWecomSender(wecomBot.SendToUser)
	subPlugin = &agentsub.SubagentsPlugin{}
	// 本地文件工具（管理员专属）：沙箱根 agent.ai.file_root，默认 <cwd>/runtime/agent_files。
	filesRoot, err := agentfiles.DefaultRoot(store.GetConfig())
	if err != nil {
		buildErr = err
		return
	}
	filesPlugin = &agentfiles.Plugin{Root: filesRoot, Repo: agentfiles.NewRepo()}
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
		// 学习闭环观测面板：能力在 handler 的 insights/journey 端点，此处登记清单。
		descriptor{
			info: plugin.Info{
				Name:        "insights",
				Version:     "1.0.0",
				Kind:        plugin.KindTools,
				Description: "用量报告：token 用量、活跃天数、工具/技能排行（学习闭环观测）",
				Builtin:     true,
			},
			ui: plugin.UIContribution{
				Nav:   &plugin.NavItem{ID: "insights", Label: "用量报告", Icon: "gauge"},
				Slash: []plugin.SlashItem{{Name: "insights", Description: "打开用量报告"}},
			},
		},
		descriptor{
			info: plugin.Info{
				Name:        "journey",
				Version:     "1.0.0",
				Kind:        plugin.KindTools,
				Description: "学习轨迹：记忆、技能与会话的成长时间线（学习闭环观测）",
				Builtin:     true,
			},
			ui: plugin.UIContribution{
				Nav:   &plugin.NavItem{ID: "journey", Label: "学习轨迹", Icon: "sprout"},
				Slash: []plugin.SlashItem{{Name: "journey", Description: "查看学习轨迹"}},
			},
		},
		// 能力评测：套件定义与运行器在 handler 的 evals 端点，此处登记清单。
		descriptor{
			info: plugin.Info{
				Name:        "evals",
				Version:     "1.0.0",
				Kind:        plugin.KindTools,
				Description: "能力评测：内置套件跑测 agent 的工具使用与知识问答",
				Builtin:     true,
			},
			ui: plugin.UIContribution{
				Nav:   &plugin.NavItem{ID: "evals", Label: "评测", Icon: "flask"},
				Slash: []plugin.SlashItem{{Name: "evals", Description: "打开能力评测"}},
			},
		},
		&agentcron.CronPlugin{Repo: cronRepo, Sched: cronSched},
		&agentmemory.MemoryPlugin{Repo: agentmemory.NewRepo()},
		&agentskills.SkillsPlugin{Repo: agentskills.NewRepo()},
		&agentkanban.KanbanPlugin{Repo: agentkanban.NewRepo()},
		filesPlugin,
		&agenttelegram.TelegramPlugin{Repo: agenttelegram.NewRepo(), Bot: tgBot},
		&agentfeishu.FeishuPlugin{Repo: agentfeishu.NewRepo(), Bot: feishuBot},
		&agentdingtalk.DingtalkPlugin{Repo: agentdingtalk.NewRepo(), Bot: dingtalkBot},
		&agentqq.QQPlugin{Repo: agentqq.NewRepo(), Bot: qqBot},
		&agentwecom.WecomPlugin{Repo: agentwecom.NewRepo(), Bot: wecomBot},
		subPlugin,
		// P3-C：联网搜索（Brave key 从 config 注入，空则走 DuckDuckGo 兜底）。
		&agentwebsearch.Plugin{APIKey: agentwebsearch.BraveKeyFromConfig(store.GetConfig())},
		// P3-C：外部 MCP server 工具桥接（config.yaml mcp_servers，空配置 = 惰性）。
		&agentmcp.ClientPlugin{Clients: newMCPClients()},
	)
}

// newMCPClients 从 store 配置的 mcp_servers 解析并构造 MCP 客户端（未配置 = nil）。
func newMCPClients() []*agentmcp.Client {
	cfgs := agentmcp.ParseConfigs(store.GetConfig()["mcp_servers"])
	clients := make([]*agentmcp.Client, 0, len(cfgs))
	for _, cfg := range cfgs {
		clients = append(clients, agentmcp.NewClient(cfg))
	}
	return clients
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

// SetTelegramStreamExecutor 注入流式入站执行器（ctx 中断 / 会话绑定 / 模型覆盖 /
// 增量回调）并启动轮询；优先于 SetTelegramExecutor 的旧式执行器。
func SetTelegramStreamExecutor(exec agenttelegram.StreamExecutor) {
	once.Do(build)
	tgBot.SetStreamExecutor(exec)
	tgBot.Start()
}

// SetTelegramHealthProvider 注入 /status 的网关健康行。
func SetTelegramHealthProvider(fn func() string) {
	once.Do(build)
	tgBot.SetHealthProvider(fn)
}

// FeishuBot 飞书通道单例（webhook 入口与 cron 直发共用）。
func FeishuBot() *agentfeishu.Bot {
	once.Do(build)
	return feishuBot
}

// DingtalkBot 钉钉通道单例（webhook 入口与 cron 直发共用）。
func DingtalkBot() *agentdingtalk.Bot {
	once.Do(build)
	return dingtalkBot
}

// QqBot QQ 通道单例（WSS 事件循环与 cron 直发共用）。
func QqBot() *agentqq.Bot {
	once.Do(build)
	return qqBot
}

// WecomBot 企业微信通道单例（webhook 入口与 cron 直发共用）。
func WecomBot() *agentwecom.Bot {
	once.Do(build)
	return wecomBot
}

// SetQqExecutor 注入 QQ 入站执行器（ctx 中断 / 会话绑定 / 模型覆盖）并启动 WSS 循环。
func SetQqExecutor(exec agentqq.Executor) {
	once.Do(build)
	qqBot.SetExecutor(exec)
	qqBot.Start()
}

// SetWecomExecutor 注入企业微信入站执行器（ctx 中断 / 会话绑定 / 模型覆盖）。
func SetWecomExecutor(exec agentwecom.Executor) {
	once.Do(build)
	wecomBot.SetExecutor(exec)
}

// SetQqHealthProvider 注入 QQ /status 的网关健康行。
func SetQqHealthProvider(fn func() string) {
	once.Do(build)
	qqBot.SetHealthProvider(fn)
}

// SetWecomHealthProvider 注入企业微信 /status 的网关健康行。
func SetWecomHealthProvider(fn func() string) {
	once.Do(build)
	wecomBot.SetHealthProvider(fn)
}

// SetFeishuExecutor 注入飞书入站执行器（ctx 中断 / 会话绑定 / 模型覆盖）。
func SetFeishuExecutor(exec agentfeishu.Executor) {
	once.Do(build)
	feishuBot.SetExecutor(exec)
}

// SetDingtalkExecutor 注入钉钉入站执行器（ctx 中断 / 会话绑定 / 模型覆盖）。
func SetDingtalkExecutor(exec agentdingtalk.Executor) {
	once.Do(build)
	dingtalkBot.SetExecutor(exec)
}

// SetFeishuHealthProvider 注入飞书 /status 的网关健康行。
func SetFeishuHealthProvider(fn func() string) {
	once.Do(build)
	feishuBot.SetHealthProvider(fn)
}

// SetDingtalkHealthProvider 注入钉钉 /status 的网关健康行。
func SetDingtalkHealthProvider(fn func() string) {
	once.Do(build)
	dingtalkBot.SetHealthProvider(fn)
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

// FilesPlugin 本地文件工具插件单例（检查点回滚 REST 用）。
func FilesPlugin() *agentfiles.Plugin {
	once.Do(build)
	return filesPlugin
}
