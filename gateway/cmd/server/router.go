package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/community"
	"github.com/xiaotian-quant/gateway/internal/experiment"
	"github.com/xiaotian-quant/gateway/internal/handler"
	"github.com/xiaotian-quant/gateway/internal/indicator"
	"github.com/xiaotian-quant/gateway/internal/metrics"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/onchain"
	"github.com/xiaotian-quant/gateway/internal/social"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/ws"
	"os"
	"strings"
)

// serverConfig exposes only the fields needed by route setup.
type serverConfig struct {
	ServerMode string
}

// setupRoutes configures all Gin routes.
// Public routes (no auth) are registered at the top level,
// while private routes are under the /api group with AuthRequired middleware.
func setupRoutes(r *gin.Engine, cfg *serverConfig) *gin.Engine {
	// Gateway no longer serves the frontend SPA. The frontend is deployed
	// independently and communicates via /api and /ws.

	api := r.Group("/api")
	{
		registerAuthRoutes(api)
		registerOAuthRoutes(api)
		registerHealthRoutes(api)
		registerBillingRoutes(api)
		registerUserRoutes(api)
		registerAdminRoutes(api)
		registerConfigRoutes(api)
		registerOrderRoutes(api)
		registerAccountRoutes(api)
		registerTradeRoutes(api)
		registerTradingSafetyRoutes(api)
		registerMarketRoutes(api)
		registerNotificationRoutes(api)
		registerArbitrageRoutes(api)
		registerTriangularRoutes(api)
		registerDataRoutes(api)
		registerPythonStrategyRoutes(api)
		registerStrategyRoutes(api)
		registerComboRoutes(api)
		registerPortfolioRoutes(api)
		registerPairlistRoutes(api)
		registerProtectionRoutes(api)
		registerRiskConfigRoutes(api)
		registerHyperoptRoutes(api)
		registerRLRoutes(api)
		registerTensorBoardRoutes(api)
		registerSettingsRoutes(api)
		registerAIRoutes(api)
		registerMLRoutes(api)
		registerAgentRoutes(api)
		registerDashboardRoutes(api)
		registerIndicatorRoutes(api)
		registerExperimentRoutes(api)
		registerCommunityRoutes(api)
		registerSocialRoutes(api)
		registerOnChainRoutes(api)
		registerAIBotRoutes(api)
		registerGridRoutes(api)
		registerDCABotRoutes(api)
		registerLayeredMartinRoutes(api)
		registerReconcileRoutes(api)
		registerFactorResearchRoutes(api)
		registerPortfolioBacktestRoutes(api)
	}

	// ── Webhooks ──
	// M8: 未配置 WEBHOOK_SECRET 时启动警告（公开下单通道无签名校验）。
	handler.WarnIfWebhookSecretMissing()
	api.POST("/webhook/tv", handler.TradingViewWebhook)
	api.POST("/webhook/generic", handler.GenericWebhook)

	// ── WebSocket ──
	r.GET("/ws", handler.WSHandler)
	r.GET("/ws/v2", ws.HubHandler)
	api.GET("/ws/stats", ws.Stats)

	// ── Metrics（PROMETHEUS_ENABLED=false 时不挂载；可配 token 或 localhost-only）──
	if metrics.Enabled() {
		r.GET("/metrics", metricsAccessGuard(), func(c *gin.Context) {
			metrics.Handler(c.Writer, c.Request)
		})
	}

	// ── pprof (debug) ──
	pprofGroup := r.Group("/debug/pprof")
	pprofGroup.Use(func(c *gin.Context) {
		if cfg.ServerMode != "debug" {
			clientIP := c.ClientIP()
			if !isLocalhost(clientIP) {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
		}
		c.Next()
	})
	pprofGroup.GET("/*any", gin.WrapF(http.DefaultServeMux.ServeHTTP))

	return r
}

// ── Route registration helpers ──

func registerAuthRoutes(api *gin.RouterGroup) {
	auth := api.Group("/auth")
	auth.Use(middleware.StrictRateLimiter())
	{
		auth.POST("/login", handler.Login)
		auth.POST("/login-code", handler.LoginByCode)
		auth.POST("/register", handler.Register)
		auth.POST("/send-code", handler.SendVerificationCode)
		auth.POST("/reset-password", handler.ResetPassword)
		auth.POST("/mfa/verify", handler.MFAVerify)
		auth.GET("/me", middleware.AuthRequired(), handler.GetMe)
		auth.POST("/refresh", middleware.AuthRequired(), handler.RefreshToken)
		auth.POST("/logout", middleware.AuthRequired(), handler.Logout)
		// A3.4: 与 /api/user/change-password 等价，强制改密期间的两个放行路径之一。
		auth.POST("/change-password", middleware.AuthRequired(), handler.ChangePassword)
	}

	// ── MFA / TOTP 管理（A3.1，均需登录） ──
	mfaG := auth.Group("/mfa")
	mfaG.Use(middleware.AuthRequired())
	{
		mfaG.POST("/setup", handler.MFASetup)
		mfaG.POST("/enable", handler.MFAEnable)
		mfaG.POST("/disable", handler.MFADisable)
	}
}

func registerOAuthRoutes(api *gin.RouterGroup) {
	api.GET("/auth/oauth/google/login", handler.OAuthGoogleLogin)
	api.GET("/auth/oauth/google/callback", handler.OAuthGoogleCallback)
	api.GET("/auth/oauth/github/login", handler.OAuthGitHubLogin)
	api.GET("/auth/oauth/github/callback", handler.OAuthGitHubCallback)
}

func registerBillingRoutes(api *gin.RouterGroup) {
	// M6: billing 订单涉及资金，全部要求登录；stripe webhook 例外——
	// 它由 Stripe 服务端回调，用 HMAC-SHA256 验签替代 AuthRequired（见下方公开挂载）。
	billingG := api.Group("/billing")
	billingG.Use(middleware.AuthRequired())
	{
		billingG.GET("/plans", handler.BillingPlans)
		billingG.GET("/chains", handler.BillingChains)
		billingG.GET("/subscription", handler.BillingSubscription)
		billingG.GET("/orders", handler.BillingListOrders)
		billingG.POST("/orders", handler.BillingCreateOrder)
		billingG.GET("/orders/:id", handler.BillingOrderStatus)
		billingG.POST("/orders/:id/tx", handler.BillingSubmitTx)
		billingG.GET("/orders/:id/verification", handler.BillingOrderVerification)
		billingG.GET("/stripe/config", handler.BillingStripeConfig)
		billingG.POST("/stripe/checkout", handler.BillingStripeCheckout)
	}

	// 公开路由：Stripe webhook（验签在 handler 内完成，不能挂 AuthRequired）。
	api.POST("/billing/stripe/webhook", handler.BillingStripeWebhook)
}

func registerUserRoutes(api *gin.RouterGroup) {
	userG := api.Group("/user")
	userG.Use(middleware.AuthRequired())
	{
		userG.GET("/profile", handler.GetProfile)
		userG.PUT("/profile", handler.UpdateProfile)
		userG.POST("/change-password", handler.ChangePassword)
		userG.GET("/notification-settings", handler.GetNotificationSettings)
		userG.PUT("/notification-settings", handler.UpdateNotificationSettings)
	}
}

func registerAdminRoutes(api *gin.RouterGroup) {
	adminG := api.Group("/admin")
	adminG.Use(middleware.AdminRequired())
	{
		adminG.GET("/users", handler.ListUsers)
		adminG.GET("/users/:id", handler.AdminGetUser)
		adminG.PUT("/users/:id", handler.AdminUpdateUser)
		adminG.GET("/stats", handler.EnhancedAdminStats)
		adminG.GET("/summary", handler.AdminDashboardSummary)
		adminG.GET("/audit-log", handler.AdminAuditLog)
		adminG.GET("/activity", handler.AdminRecentActivity)
		adminG.POST("/users/:id/disable", handler.AdminUserDisable)
		adminG.POST("/users/:id/enable", handler.AdminUserEnable)
		adminG.POST("/config/reload", handler.ReloadConfig)
	}
}

func registerConfigRoutes(api *gin.RouterGroup) {
	// Public config lookup endpoints (no auth required)
	api.GET("/config/markets", handler.GetMarkets)
	api.GET("/config/indices", handler.GetIndices)
	api.GET("/config/exchanges", handler.GetExchanges)
	api.GET("/config/ai-models", handler.GetAIModels)
	api.GET("/config/rate", handler.GetConversionRate)

	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/config", handler.GetConfig)
	// M5: 全局配置写操作收敛为 admin-only（含重启级配置、AI provider 设置）；
	// 凭证写入同属敏感操作（C2 缓解，保险库按用户隔离留待第二波）。
	private.PUT("/config", middleware.AdminRequired(), handler.SaveConfig)
	private.POST("/config", middleware.AdminRequired(), handler.SaveConfig)
	private.GET("/strategies/global", handler.GetGlobalStrategy)
	private.GET("/strategies/param-defs", handler.GetStrategyParamDefs)
	private.PUT("/strategies/global", handler.SaveGlobalStrategy)
	private.GET("/strategies/defaults", handler.GetStrategyDefaults)
	private.GET("/strategies/contract-defaults", handler.GetContractDefaults)
	private.POST("/exchange/save", handler.ExchangeSave)
	private.PUT("/config/exchanges/credentials", middleware.AdminRequired(), handler.SaveExchangeCredentials)
	private.POST("/exchange/test", handler.ExchangeTest)
	private.POST("/exchange/default", handler.ExchangeDefault)
	private.GET("/exchange/status", handler.ExchangeStatus)
	private.GET("/exchanges/configured", handler.ExchangesConfigured)
	private.POST("/ai/save", handler.AISave)
	private.POST("/ai/test", handler.AITest)
	private.POST("/ai/default", handler.AIDefault)
	private.POST("/restart", handler.Restart)

	// ── SignalExecutor ──
	private.GET("/executor/status", handler.ExecutorStatus)
	private.GET("/executor/positions", handler.ExecutorPositions)
	private.GET("/executor/records", handler.ExecutionRecords)
	private.GET("/executor/signal-sources", handler.ExecutorSignalSources)

	// ── Contract ──
	private.GET("/contract/leverage", handler.ContractLeverageGet)
	private.POST("/contract/leverage", handler.ContractLeverageSet)
	private.GET("/contract/margin", handler.ContractMarginInfo)
	private.GET("/contract/liquidation-price", handler.ContractLiquidationPrice)
	private.GET("/contract/params", handler.ContractParamsGet)
	private.POST("/contract/params", handler.ContractParamsSave)

	// ── AI Robot ──
	private.GET("/ai/status", handler.AIRobotStatus)
	private.GET("/ai/signals", handler.AISignals)

	// ── Martin / WallStreet Strategies ──
	private.GET("/strategies/martin", handler.StrategyMartinList)
	private.POST("/strategies/martin", handler.StrategyMartinCreate)
	private.PUT("/strategies/martin/:id", handler.StrategyMartinUpdate)
	private.DELETE("/strategies/martin/:id", handler.StrategyMartinDelete)
	private.GET("/strategies/wallstreet", handler.StrategyWallStreetList)
	private.POST("/strategies/wallstreet", handler.StrategyWallStreetCreate)
	private.PUT("/strategies/wallstreet/:id", handler.StrategyWallStreetUpdate)
	private.DELETE("/strategies/wallstreet/:id", handler.StrategyWallStreetDelete)
}

func registerOrderRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())

	private.GET("/orders", handler.GetOrders)
	private.POST("/orders", handler.PlaceOrder)
	private.POST("/orders/cancel-all", handler.CancelAllOrders)
	private.GET("/orders/history", handler.OrderHistory)

	// Advanced Orders
	private.POST("/orders/oco", handler.PlaceOCO)
	private.GET("/orders/oco", handler.ListOCO)
	private.GET("/orders/oco/:id", handler.GetOCO)
	private.DELETE("/orders/oco/:id", handler.CancelOCO)
	private.POST("/orders/bracket", handler.PlaceBracket)
	private.GET("/orders/bracket", handler.ListBracket)
	private.GET("/orders/bracket/:id", handler.GetBracket)
	private.DELETE("/orders/bracket/:id", handler.CancelBracket)
	private.POST("/orders/iceberg", handler.PlaceIceberg)
	private.GET("/orders/iceberg", handler.ListIceberg)
	private.GET("/orders/iceberg/:id", handler.GetIceberg)
	private.DELETE("/orders/iceberg/:id", handler.CancelIceberg)
	private.POST("/orders/bracket/calculate", handler.CalculateBracket)

	private.DELETE("/orders/:order_id", handler.CancelOrder)
	private.POST("/orders/:order_id/cancel", handler.CancelOrder)
}

func registerAccountRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/account/balance", handler.GetAccountBalance)
	private.POST("/account/transfer", handler.Transfer)
	private.POST("/account/buy", handler.BuyCrypto)
	private.POST("/account/swap", handler.SwapCurrency)
}

func registerTradeRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/trades", handler.GetTradeHistory)
}

func registerTradingSafetyRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/trading/safety", handler.GetTradingSafetyStatus)
	private.POST("/trading/unlock", handler.UnlockLiveTrading)
	private.POST("/trading/lock", handler.LockLiveTrading)
}

func registerMarketRoutes(api *gin.RouterGroup) {
	api.GET("/klines/:symbol", handler.GetKlines)
	api.GET("/market/klines", handler.MarketKlines)
	api.GET("/market/orderbook", handler.OrderBook)
	api.GET("/market/trades", handler.MarketTrades)
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.POST("/backtest/run", handler.RunBacktest)
	private.POST("/native/backtest", handler.NativeBacktest)
	api.GET("/symbols/search", handler.SymbolSearch)
	api.GET("/market/snapshot", handler.MarketSnapshot)
	api.GET("/market/funding", handler.MarketFunding)
	api.GET("/status", handler.Status)
}

func registerNotificationRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/notifications", handler.GetNotifications)
	private.GET("/notifications/unread-count", handler.GetUnreadCount)
	private.POST("/notifications/:id/read", handler.MarkNotificationRead)
	private.POST("/notifications/read-all", handler.MarkAllNotificationsRead)
	private.DELETE("/notifications", handler.ClearNotifications)
	private.GET("/notify/channels", handler.GetNotifyChannels)
	private.GET("/notify/routes", handler.GetNotifyRoutes)
	private.POST("/notify/routes", handler.UpdateNotifyRoute)
	private.DELETE("/notify/routes/:id", handler.DeleteNotifyRoute)
	private.POST("/notify/test", handler.TestNotifyChannel)
	private.POST("/notify/send", handler.SendCustomNotification)
	private.GET("/chart", handler.Chart)
}

func registerArbitrageRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/arbitrage/config", handler.GetArbitrageConfig)
	private.POST("/arbitrage/config", handler.UpdateArbitrageConfig)
	private.POST("/arbitrage/start", handler.StartArbitrage)
	private.POST("/arbitrage/stop", handler.StopArbitrage)
	private.GET("/arbitrage/status", handler.GetArbitrageStatus)
	private.GET("/arbitrage/performance", handler.GetArbitragePerformance)
	private.GET("/arbitrage/opportunity", handler.GetArbitrageOpportunity)
	private.GET("/arbitrage/positions", handler.GetArbitragePositions)
	private.GET("/arbitrage/history", handler.GetArbitrageHistory)
	private.POST("/arbitrage/exchanges", handler.RegisterArbitrageExchange)
	private.GET("/arbitrage/exchanges", handler.ListArbitrageExchanges)
	private.DELETE("/arbitrage/exchanges/:name", handler.UnregisterArbitrageExchange)
	private.POST("/arbitrage/execute", handler.ExecuteArbitrage)
	private.POST("/arbitrage/positions/:id/close", handler.CloseArbitragePosition)
	private.POST("/arbitrage/positions/:id/fail", handler.FailArbitragePosition)
}

func registerTriangularRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/triangular/config", handler.GetTriangularConfig)
	private.POST("/triangular/config", handler.UpdateTriangularConfig)
	private.POST("/triangular/start", handler.StartTriangular)
	private.POST("/triangular/stop", handler.StopTriangular)
	private.GET("/triangular/status", handler.GetTriangularStatus)
	private.GET("/triangular/performance", handler.GetTriangularPerformance)
	private.GET("/triangular/opportunity", handler.GetTriangularOpportunity)
	private.GET("/triangular/positions", handler.GetTriangularPositions)
	private.GET("/triangular/history", handler.GetTriangularHistory)
	private.POST("/triangular/execute", handler.ExecuteTriangular)
	private.POST("/triangular/positions/:id/close", handler.CloseTriangularPosition)
	private.POST("/triangular/positions/:id/fail", handler.FailTriangularPosition)
}

func registerDataRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/data/coverage", handler.GetDataCoverage)
	private.GET("/data/info", handler.GetDataInfo)
	private.POST("/data/download", handler.StartDataDownload)
	private.GET("/data/download/:id", handler.GetDownloadJob)
	private.GET("/data/bars", handler.GetHistoricalBars)
	private.POST("/data/ticks/download", handler.StartTickDownload)
	private.GET("/data/ticks", handler.GetTicks)
	private.GET("/data/ticks/info", handler.GetTickInfo)
	private.POST("/backtest/tick", handler.RunTickBacktest)
	private.GET("/backtest/tick/jobs", handler.ListTickBacktestJobs)
	private.GET("/backtest/tick/jobs/:id", handler.GetTickBacktestJob)
}

func registerStrategyRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/strategies/configs", handler.GetStrategyConfigs)
	private.GET("/strategies/configs/:id", handler.GetStrategyConfig)
	private.GET("/strategies/configs/:id/runtime", handler.GetStrategyRuntime)
	private.POST("/strategies/configs", handler.CreateStrategyConfig)
	private.PUT("/strategies/configs/:id", handler.UpdateStrategyConfig)
	private.DELETE("/strategies/configs/:id", handler.DeleteStrategyConfig)
	private.POST("/strategies/configs/batch-start", handler.BatchStartConfigs)
	private.POST("/strategies/configs/batch-stop", handler.BatchStopConfigs)
	private.POST("/strategies/configs/batch-close", handler.BatchCloseConfigs)
	private.POST("/strategies/configs/batch-delete", handler.BatchDeleteConfigs)
	private.POST("/strategies/configs/:id/start", handler.StartStrategyConfig)
	private.POST("/strategies/configs/:id/stop", handler.StopStrategyConfig)
	private.GET("/strategies/logs", handler.GetStrategyLogs)
	private.DELETE("/strategies/logs", handler.ClearStrategyLogs)
	private.GET("/strategies/templates", handler.GetTemplates)
	private.POST("/strategies/templates", handler.CreateTemplate)
	private.DELETE("/strategies/templates/:id", handler.DeleteTemplate)
	private.GET("/strategies/spot", handler.GetStrategiesSpot)
	private.GET("/strategies/contract", handler.GetStrategiesContract)
	private.GET("/strategies/ranking", handler.GetStrategiesRanking)
	private.GET("/strategies", handler.GetStrategyConfigs)
}

func registerPythonStrategyRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.POST("/strategies-python/run", handler.RunPythonStrategy)
}

func registerComboRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/combos", handler.GetCombos)
	private.POST("/combos", handler.CreateCombo)
	private.GET("/combos/:id", handler.GetCombo)
	private.PUT("/combos/:id", handler.UpdateCombo)
	private.DELETE("/combos/:id", handler.DeleteCombo)
	private.POST("/combos/:id/start", handler.StartCombo)
	private.POST("/combos/:id/stop", handler.StopCombo)
	private.GET("/combos/:id/signals", handler.GetComboSignals)
}

func registerPortfolioRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/portfolio/summary", handler.PortfolioSummary)
	private.GET("/portfolio/positions", handler.PortfolioPositions)
	private.GET("/portfolio/snapshots", handler.PortfolioSnapshots)
	private.GET("/portfolio/calendar", handler.PortfolioCalendar)
	private.GET("/exchange/usdcny", handler.UsdCnyRate)
	private.GET("/positions", handler.PortfolioPositions)
}

func registerPairlistRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/pairlist/whitelist", handler.GetPairlistWhitelist)
	private.POST("/pairlist/refresh", handler.RefreshPairlist)
	private.GET("/pairlist/refresh", handler.RefreshPairlist)
	private.GET("/pairlist/config", handler.GetPairlistConfig)
	private.POST("/pairlist/config", handler.ConfigurePairlist)
}

// registerRiskConfigRoutes 风控参数 API：GET 任意登录用户；PUT 仅 admin
// （private 组已带 AuthRequired，再叠加 AdminRequired）。
func registerRiskConfigRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/risk/config", handler.GetRiskConfig)
	private.PUT("/risk/config", middleware.AdminRequired(), handler.UpdateRiskConfig)
}

func registerProtectionRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/protection/status", handler.GetProtectionStatus)
	private.GET("/protection/config", handler.GetProtectionConfig)
	private.POST("/protection/config", handler.ConfigureProtection)
	private.POST("/protection/reset", handler.ResetProtection)
	private.POST("/protection/trade", handler.RecordTrade)
}

func registerHyperoptRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.POST("/hyperopt/start", handler.StartHyperopt)
	private.GET("/hyperopt/jobs", handler.ListHyperoptJobs)
	private.GET("/hyperopt/jobs/:id", handler.GetHyperoptJob)
	private.POST("/hyperopt/jobs/:id/cancel", handler.CancelHyperoptJob)
	private.DELETE("/hyperopt/jobs/:id", handler.DeleteHyperoptJob)
	private.GET("/hyperopt/spaces", handler.GetHyperoptSpaces)
	private.POST("/hyperopt/jobs/:id/export", handler.ExportHyperoptParams)
}

func registerRLRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.POST("/rl/train", handler.RLTrain)
	private.POST("/rl/predict", handler.RLPredict)
	private.POST("/rl/evaluate", handler.RLEvaluate)
	private.GET("/rl/models", handler.ListRLModels)
	private.DELETE("/rl/models/:id", handler.DeleteRLModel)
	private.GET("/rl/jobs/:id", handler.GetRLJob)
	private.POST("/rl/jobs/:id/cancel", handler.CancelRLJob)
	private.GET("/rl/worker/status", handler.GetRLWorkerStatus)
	private.POST("/rl/worker/start", handler.StartRLWorker)
}

func registerTensorBoardRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/tensorboard/runs", handler.ListTensorBoardRuns)
	private.POST("/tensorboard/scalars", handler.QueryTensorBoardScalars)
	private.GET("/tensorboard/runs/:id", handler.GetTensorBoardRun)
	private.DELETE("/tensorboard/runs/:id", handler.DeleteTensorBoardRun)
}

func registerSettingsRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	settingsG := private.Group("/settings")
	{
		settingsG.GET("/agent/models", handler.SettingsAgentModels)
		settingsG.GET("/agent-models", handler.SettingsAgentModels)
		settingsG.GET("/defaults", handler.SettingsDefaultsGet)
		settingsG.POST("/defaults", handler.SettingsDefaultsSave)
		settingsG.POST("/ui", handler.SettingsUISave)
		settingsG.POST("/exchange/:id/test", handler.SettingsExchangeTest)
		// C2 缓解：交易所/AI 凭证写操作收敛为 admin-only
		// （保险库按用户隔离留待第二波）。
		settingsG.PUT("/exchange/:id", middleware.AdminRequired(), handler.SettingsExchangeSave)
		settingsG.POST("/ai/:id/test", handler.SettingsAITest)
		settingsG.PUT("/ai/:id", middleware.AdminRequired(), handler.SettingsAISave)
	}
	private.GET("/settings/currency", handler.SettingsCurrencyGet)
	private.PUT("/settings/currency", handler.SettingsCurrencySet)
}

func registerAIRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/ai/snapshot", handler.AISnapshot)
	private.GET("/ai/klines", handler.AIKlines)
	private.POST("/ai/generate", handler.AIGenerate)
	private.POST("/strategies/ai-generate", handler.StrategyAIGenerate)
	private.POST("/ai/multi-agent", handler.AIMultiAgent)
	private.POST("/ai/backtest", handler.AIBacktest)
	private.POST("/ai/optimize", handler.AIOptimize)
	private.POST("/ai/deploy", handler.AIDeploy)
	private.POST("/ai/validate", handler.AIValidate)
	private.POST("/ai/fix", handler.AIFix)
	private.POST("/ai/analyze", handler.AIAnalyze)
	private.GET("/ai/quickscan", handler.AIQuickScan)
	private.POST("/ai/chat", handler.AIChat)
}

func registerMLRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/ml/health", handler.MLHealth)
	private.POST("/ml/train", handler.MLTrain)
	private.POST("/ml/predict", handler.MLPredict)
	private.GET("/ml/models", handler.MLModels)
	private.GET("/ml/models/:id", handler.MLModelDetail)
	private.DELETE("/ml/models/:id", handler.MLDeleteModel)
	private.GET("/ml/models/:id/importance", handler.MLFeatureImportance)
	private.POST("/ml/features", handler.MLGenerateFeatures)
	private.POST("/ml/deploy", handler.MLDeployStrategy)
	private.GET("/ml/strategy-models", handler.MLStrategyModels)
	private.POST("/ai/models/config", handler.AIModelConfigSave)
	private.GET("/models/list", handler.AIModelsList)
	private.GET("/ai/models", handler.AIModels)
	private.GET("/auto-trade/config", handler.AIAutoTradeGet)
	private.PUT("/auto-trade/config", handler.AIAutoTradeSave)
	private.POST("/analysis/start", handler.AIAnalysisStart)
	private.GET("/analysis/result", handler.AIAnalysisResult)
	private.POST("/chat/send", handler.ChatSend)
}

func registerAgentRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	agent := private.Group("/agent")
	{
		agent.GET("/tokens", handler.GetAgentTokens)
		agent.POST("/tokens", handler.CreateAgentToken)
		agent.DELETE("/tokens/:id", handler.DeleteAgentToken)
		agent.GET("/audit-log", handler.GetAgentAuditLog)
		agent.GET("/cc-switch", handler.CCSwitchStatus)
		agent.POST("/cc-switch/configure", handler.CCSwitchConfigure)
		agent.POST("/cc-switch/start", handler.CCSwitchStart)
		agent.POST("/cc-switch/stop", handler.CCSwitchStop)
		agent.GET("/ai-config", handler.GetAgentAIConfig)
		agent.PUT("/ai-config", handler.SaveAgentAIConfig)
		agent.POST("/ai-config", handler.SaveAgentAIConfig)
		agent.POST("/ai-test", handler.AgentAITest)
		agent.POST("/chat", handler.AgentChat)
	}
}

func registerDashboardRoutes(api *gin.RouterGroup) {
	private := api.Group("")
	private.Use(middleware.AuthRequired())
	private.GET("/dashboard/summary", handler.DashboardSummary)
}

func registerHealthRoutes(api *gin.RouterGroup) {
	api.GET("/health", handler.HealthCheck)
	api.GET("/health/components", handler.ComponentHealth)
	// M4: 服务日志可能含敏感信息，至少要求登录。
	api.GET("/logs", middleware.AuthRequired(), handler.GetLogs)
}

func registerIndicatorRoutes(api *gin.RouterGroup) {
	indicatorG := api.Group("/indicator")
	indicatorG.Use(middleware.AuthRequired())
	{
		indicatorG.POST("/parse", indicator.ParseIndicator)
		indicatorG.POST("/validate", indicator.ValidateIndicator)
		indicatorG.POST("/save", indicator.SaveIndicator)
		indicatorG.POST("/saveIndicator", indicator.SaveIndicator)
		indicatorG.POST("/getIndicators", indicator.GetIndicators)
		indicatorG.POST("/getDecryptKey", indicator.DecryptIndicator)
		indicatorG.GET("/kline", indicator.GetIndicatorKline)
		indicatorG.GET("/list", indicator.ListIndicators)
		indicatorG.GET("/:id", indicator.GetIndicator)
		indicatorG.DELETE("/:id", indicator.DeleteIndicator)
		indicatorG.POST("/applyParamDefaults", indicator.ApplyParamDefaults)
		indicatorG.POST("/execute", indicator.SandboxExecute)
		indicatorG.POST("/analyze", indicator.SandboxAnalyze)
		indicatorG.POST("/ai-generate", indicator.IndicatorAIGenerate)
		indicatorG.POST("/backtest", indicator.BacktestIndicator)
		indicatorG.POST("/publish", indicator.PublishIndicator)
	}
	api.GET("/watchlist", middleware.AuthRequired(), indicator.GetWatchlist)
	api.POST("/watchlist", middleware.AuthRequired(), indicator.AddWatchlist)
	api.GET("/indicators", middleware.AuthRequired(), indicator.ListIndicators)
	api.POST("/indicator/internal-call", indicator.InternalCallIndicator)
}

func registerExperimentRoutes(api *gin.RouterGroup) {
	experimentG := api.Group("/experiment")
	experimentG.Use(middleware.AuthRequired())
	{
		experimentG.POST("/run", experiment.RunExperimentHandler)
		experimentG.POST("/sensitivity", experiment.SensitivityAnalysisHandler)
		experimentG.POST("/walk-forward", experiment.WalkForwardHandler)
		experimentG.POST("/ai-optimize", experiment.RunExperimentHandler)
		experimentG.POST("/structured-tune", experiment.RunExperimentHandler)
		experimentG.GET("/status/:id", experiment.ExperimentStatusHandler)
	}
	api.GET("/experiments", middleware.AuthRequired(), experiment.ExperimentListHandler)
}

func registerCommunityRoutes(api *gin.RouterGroup) {
	comm := api.Group("/community")
	comm.Use(middleware.AuthRequired())
	{
		comm.GET("/indicators", community.MarketIndicators)
		comm.POST("/publish", community.PublishIndicator)
		comm.POST("/purchase/:id", community.PurchaseIndicator)
		comm.GET("/comments/:id", community.GetComments)
		comm.POST("/comments/:id", community.AddComment)
		// M3: 社区审核为管理操作，注释标注 "admin only" 但此前仅有 AuthRequired，
		// 收敛为 AdminRequired 防止任意登录用户通过/拒绝上架、看待审队列。
		comm.POST("/review/:id", middleware.AdminRequired(), community.ReviewIndicator)
		comm.GET("/reviews/pending", middleware.AdminRequired(), community.PendingReviews)
		comm.GET("/author/revenue", community.AuthorRevenue)
		comm.GET("/strategies", community.MarketStrategies)
		comm.GET("/strategies/leaderboard", community.StrategyLeaderboard)
		comm.GET("/strategies/trending", community.TrendingStrategies)
		comm.GET("/strategies/:id", community.StrategyDetail)
		comm.GET("/strategies/:id/overfit", community.GetStrategyOverfitRisk)
		comm.POST("/strategies/publish", community.PublishStrategy)
		comm.POST("/strategies/:id/comment", community.AddStrategyComment)
		comm.POST("/strategies/:id/rate", community.RateStrategy)
	}
}

func registerSocialRoutes(api *gin.RouterGroup) {
	socialG := api.Group("/social")
	socialG.Use(middleware.AuthRequired())
	{
		socialEngine := social.NewEngine()
		// Seed signal providers from AI bot catalog so follow/auto-follow works.
		// Use small sequential IDs to stay within strconv.Atoi range.
		idx := 1
		for _, item := range store.GetAIBotCatalog() {
			if strategyType, _ := item["strategy_type"].(string); strategyType == "signal_provider" {
				providerID := idx
				idx++
				monthlyFee := 0.0
				if v, ok := item["monthly_fee"].(float64); ok {
					monthlyFee = v
				}
				socialEngine.RegisterProvider(providerID, monthlyFee, true)
			}
		}
		social.RegisterRoutes(socialG, socialEngine)
	}
}

func registerOnChainRoutes(api *gin.RouterGroup) {
	onchainG := api.Group("/onchain")
	onchainG.Use(middleware.AuthRequired())
	{
		onchainClient := onchain.NewClient(os.Getenv("ONCHAIN_API_KEY"))
		onchain.RegisterRoutes(onchainG, onchainClient)
	}
}

func registerAIBotRoutes(api *gin.RouterGroup) {
	private := api.Group("/ai-bots")
	private.Use(middleware.AuthRequired())
	{
		// Catalog
		private.GET("/catalog", handler.AIBotCatalogList)
		private.GET("/catalog/:id", handler.AIBotCatalogGet)

		// Instances
		private.GET("/instances", handler.AIBotInstanceList)
		private.POST("/instances", handler.AIBotInstanceCreate)
		private.GET("/instances/:id", handler.AIBotInstanceGet)
		private.PUT("/instances/:id", handler.AIBotInstanceUpdate)
		private.DELETE("/instances/:id", handler.AIBotInstanceDelete)
		private.POST("/instances/:id/start", handler.AIBotInstanceStart)
		private.POST("/instances/:id/pause", handler.AIBotInstancePause)
		private.POST("/instances/:id/resume", handler.AIBotInstanceResume)
		private.POST("/instances/:id/stop", handler.AIBotInstanceStop)
		private.POST("/instances/:id/clone", handler.AIBotInstanceClone)
		private.GET("/instances/:id/analytics", handler.AIBotInstanceAnalytics)
		private.GET("/instances/:id/trades", handler.AIBotInstanceTrades)

		// Batch operations
		private.POST("/instances/batch-start", handler.AIBotBatchStart)
		private.POST("/instances/batch-stop", handler.AIBotBatchStop)
		private.POST("/instances/batch-delete", handler.AIBotBatchDelete)

		// Subscriptions
		private.GET("/subscriptions", handler.AIBotSubscriptionList)
		private.POST("/subscriptions", handler.AIBotSubscriptionCreate)
		private.POST("/subscriptions/:id/cancel", handler.AIBotSubscriptionCancel)
	}
}

func registerGridRoutes(api *gin.RouterGroup) {
	private := api.Group("/grid")
	private.Use(middleware.AuthRequired())
	{
		private.GET("/bots", handler.GridBotList)
		private.POST("/bots", handler.GridBotCreate)
		private.GET("/bots/:id", handler.GridBotGet)
		private.PUT("/bots/:id", handler.GridBotUpdate)
		private.DELETE("/bots/:id", handler.GridBotDelete)
		private.POST("/bots/:id/start", handler.GridBotStart)
		private.POST("/bots/:id/stop", handler.GridBotStop)
	}
}

// registerDCABotRoutes DCA 定投机器人（A1.2）：CRUD + start/stop，挂 /api/dca-bots/。
func registerDCABotRoutes(api *gin.RouterGroup) {
	private := api.Group("/dca-bots")
	private.Use(middleware.AuthRequired())
	{
		private.GET("/", handler.DCABotList)
		private.POST("/", handler.DCABotCreate)
		private.GET("/:id", handler.DCABotGet)
		private.PUT("/:id", handler.DCABotUpdate)
		private.DELETE("/:id", handler.DCABotDelete)
		private.POST("/:id/start", handler.DCABotStart)
		private.POST("/:id/stop", handler.DCABotStop)
	}
}

// registerReconcileRoutes A8 对账体系：差异/偏差查询与人工确认 + 配置（全 AuthRequired）。
func registerReconcileRoutes(api *gin.RouterGroup) {
	private := api.Group("/reconcile")
	private.Use(middleware.AuthRequired())
	{
		private.GET("/diffs", handler.ReconcileDiffsList)
		private.POST("/diffs/:id/resolve", handler.ReconcileDiffResolve)
		private.GET("/status", handler.ReconcileStatus)
		private.GET("/deviations", handler.ReconcileDeviationsList)
		private.POST("/deviations/:id/resolve", handler.ReconcileDeviationResolve)
		private.GET("/config", handler.ReconcileConfigGet)
		private.PUT("/config", middleware.AdminRequired(), handler.ReconcileConfigPut)
	}
}

// registerLayeredMartinRoutes 分层马丁格尔机器人（A1.3）：CRUD + start/stop，
// 挂 /api/layered-martin-bots/。
func registerLayeredMartinRoutes(api *gin.RouterGroup) {
	private := api.Group("/layered-martin-bots")
	private.Use(middleware.AuthRequired())
	{
		private.GET("/", handler.LayeredMartinBotList)
		private.POST("/", handler.LayeredMartinBotCreate)
		private.GET("/:id", handler.LayeredMartinBotGet)
		private.PUT("/:id", handler.LayeredMartinBotUpdate)
		private.DELETE("/:id", handler.LayeredMartinBotDelete)
		private.POST("/:id/start", handler.LayeredMartinBotStart)
		private.POST("/:id/stop", handler.LayeredMartinBotStop)
	}
}

// registerFactorResearchRoutes A6.1 因子研究：因子列表/取值/IC 评价/分层回测
// （全 AuthRequired；评价结果带 user_id 落库 factors_evaluations）。
func registerFactorResearchRoutes(api *gin.RouterGroup) {
	private := api.Group("/factors")
	private.Use(middleware.AuthRequired())
	{
		private.GET("", handler.GetFactors)
		private.GET("/", handler.GetFactors)
		private.GET("/evaluations", handler.ListFactorEvaluations)
		private.POST("/evaluate", handler.EvaluateFactor)
		private.POST("/layers", handler.FactorLayers)
		private.GET("/:name/values", handler.GetFactorValues)
	}
}

// registerPortfolioBacktestRoutes A6.2 组合回测：同步执行/历史/详情/删除
// （全 AuthRequired；user_id 落库 xt_portfolio_backtests，详情/删除走属主校验）。
func registerPortfolioBacktestRoutes(api *gin.RouterGroup) {
	private := api.Group("/backtests")
	private.Use(middleware.AuthRequired())
	{
		private.POST("/portfolio", handler.RunPortfolioBacktest)
		private.GET("/portfolio", handler.ListPortfolioBacktests)
		private.GET("/portfolio/:id", handler.GetPortfolioBacktest)
		private.DELETE("/portfolio/:id", handler.DeletePortfolioBacktest)
	}
}

// metricsAccessGuard /metrics 访问控制（A9.1，环境变量驱动，读取见 metrics 包）：
//   - PROMETHEUS_LOCALHOST_ONLY=true：仅本机回环地址可访问；
//   - PROMETHEUS_TOKEN 非空：要求 Authorization: Bearer <token> 或 ?token=<token>。
func metricsAccessGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if metrics.LocalhostOnly() && !isLocalhost(c.ClientIP()) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		if tok := metrics.Token(); tok != "" {
			got := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
			if got == "" {
				got = c.Query("token")
			}
			if got != tok {
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
		}
		c.Next()
	}
}
