package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/community"
	"github.com/xiaotian-quant/gateway/internal/config"
	"github.com/xiaotian-quant/gateway/internal/experiment"
	"github.com/xiaotian-quant/gateway/internal/handler"
	"github.com/xiaotian-quant/gateway/internal/indicator"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/spa"
)

func main() {
	// ── Load configuration ──
	cfg, err := config.Load("gateway.yaml")
	if err != nil {
		log.Printf("WARNING: Config load failed, using defaults: %v", err)
		cfg = config.Default()
	}

	// ── Initialize application context (all services) ──
	appCtx := app.Get()
	if err := appCtx.Init(cfg); err != nil {
		log.Printf("WARNING: App init partial: %v", err)
	}
	defer appCtx.Shutdown()

	// Ensure legacy store is initialized (handlers depend on it directly)
	if err := store.InitDB(); err != nil {
		log.Printf("WARNING: SQLite init skipped: %v", err)
	}
	store.LoadConfig()
	store.LoadStrategyConfigs()

	// ── Setup Gin ──
	if cfg.Server.Mode == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(middleware.CORS())

	// ── Public Routes ──
	// Serve static assets from embedded spa/assets/
	assetsFS := spa.AssetsFS()
	r.GET("/assets/*filepath", func(c *gin.Context) {
		fileServer := http.FileServer(assetsFS)
		c.Request.URL.Path = c.Param("filepath")
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
	// Fallback all non-API routes to index.html for React Router
	r.GET("/", handler.Index)
	r.NoRoute(handler.Index)

	api := r.Group("/api")
	{
		// ── Auth ──
		auth := api.Group("/auth")
		{
			auth.POST("/login", handler.Login)
			auth.POST("/login-code", handler.LoginByCode)
			auth.POST("/register", handler.Register)
			auth.POST("/send-code", handler.SendVerificationCode)
			auth.POST("/reset-password", handler.ResetPassword)
			auth.GET("/me", middleware.AuthRequired(), handler.GetMe)
			auth.POST("/refresh", middleware.AuthRequired(), handler.RefreshToken)
			auth.GET("/admin/users", middleware.AdminRequired(), handler.ListUsers)
		}

		// ── OAuth ──
		api.GET("/auth/oauth/google/login", handler.OAuthGoogleLogin)
		api.GET("/auth/oauth/google/callback", handler.OAuthGoogleCallback)
		api.GET("/auth/oauth/github/login", handler.OAuthGitHubLogin)
		api.GET("/auth/oauth/github/callback", handler.OAuthGitHubCallback)

		// ── Billing ──
		api.GET("/billing/plans", handler.BillingPlans)
		api.GET("/billing/chains", handler.BillingChains)
		api.POST("/billing/orders", handler.BillingCreateOrder)
		api.GET("/billing/orders/:id", handler.BillingOrderStatus)

		// ── User Profile ──
		userG := api.Group("/user")
		userG.Use(middleware.AuthRequired())
		{
			userG.GET("/profile", handler.GetProfile)
			userG.PUT("/profile", handler.UpdateProfile)
			userG.POST("/change-password", handler.ChangePassword)
			userG.GET("/notification-settings", handler.GetNotificationSettings)
			userG.PUT("/notification-settings", handler.UpdateNotificationSettings)
		}

		// ── Admin ──
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
		}

		// ── Config ──
		api.GET("/config", handler.GetConfig)
		api.PUT("/config", handler.SaveConfig)
		api.POST("/config", handler.SaveConfig)
		api.GET("/strategies/global", handler.GetGlobalStrategy)
		api.PUT("/strategies/global", handler.SaveGlobalStrategy)
		api.POST("/exchange/save", handler.ExchangeSave)
		api.POST("/exchange/test", handler.ExchangeTest)
		api.POST("/exchange/default", handler.ExchangeDefault)
		api.GET("/exchange/status", handler.ExchangeStatus)
		api.GET("/exchanges/configured", handler.ExchangesConfigured)
		api.POST("/ai/save", handler.AISave)
		api.POST("/ai/test", handler.AITest)
		api.POST("/ai/default", handler.AIDefault)
		api.POST("/restart", handler.Restart)

		// ── Orders ──
		api.GET("/orders", handler.GetOrders)
		api.POST("/orders", handler.PlaceOrder)
		api.POST("/orders/cancel-all", handler.CancelAllOrders)
		api.DELETE("/orders/:order_id", handler.CancelOrder)
		api.POST("/orders/:order_id/cancel", handler.CancelOrder)
		api.GET("/orders/history", handler.OrderHistory)

		// ── Account ──
		api.GET("/account/balance", handler.GetAccountBalance)

		// ── Trades ──
		api.GET("/trades", handler.GetTradeHistory)

		// ── Market ──
		api.GET("/klines/:symbol", handler.GetKlines)
			api.GET("/market/klines", handler.MarketKlines)
		api.GET("/market/orderbook", handler.OrderBook)
		api.GET("/market/trades", handler.MarketTrades)
		api.POST("/backtest/run", handler.RunBacktest)
		api.POST("/native/backtest", handler.NativeBacktest)
		api.GET("/symbols/search", handler.SymbolSearch)
		api.GET("/market/snapshot", handler.MarketSnapshot)
		api.GET("/status", handler.Status)

		// ── Notifications ──
		api.GET("/notifications", handler.GetNotifications)
		api.GET("/notifications/unread-count", handler.GetUnreadCount)
		api.POST("/notifications/:id/read", handler.MarkNotificationRead)
		api.POST("/notifications/read-all", handler.MarkAllNotificationsRead)
		api.DELETE("/notifications", handler.ClearNotifications)
		api.GET("/chart", handler.Chart)

		// ── Data Management ──
		api.POST("/data/download", handler.DataDownload)
		api.GET("/data/download/:jobId", handler.DataDownloadStatus)
		api.GET("/data/coverage", handler.DataCoverage)
		api.GET("/data/load", handler.DataLoad)
		api.GET("/data/validate", handler.DataValidate)
		api.DELETE("/data/prune", handler.DataPrune)
		api.GET("/data/symbols", handler.DataSymbols)
		api.GET("/data/intervals", handler.DataIntervals)

		// ── Strategy ──
		api.GET("/strategies/configs", handler.GetStrategyConfigs)
		api.GET("/strategies/configs/:id", handler.GetStrategyConfig)
		api.POST("/strategies/configs", handler.CreateStrategyConfig)
		api.PUT("/strategies/configs/:id", handler.UpdateStrategyConfig)
		api.DELETE("/strategies/configs/:id", handler.DeleteStrategyConfig)
		api.POST("/strategies/configs/batch-start", handler.BatchStartConfigs)
		api.POST("/strategies/configs/batch-stop", handler.BatchStopConfigs)
		api.POST("/strategies/configs/:id/start", handler.StartStrategyConfig)
		api.POST("/strategies/configs/:id/stop", handler.StopStrategyConfig)
		api.GET("/strategies/logs", handler.GetStrategyLogs)
		api.DELETE("/strategies/logs", handler.ClearStrategyLogs)
		api.GET("/strategies/templates", handler.GetTemplates)
		api.POST("/strategies/templates", handler.CreateTemplate)
		api.DELETE("/strategies/templates/:id", handler.DeleteTemplate)
			api.GET("/strategies/spot", handler.GetStrategiesSpot)
			api.GET("/strategies/contract", handler.GetStrategiesContract)
			api.GET("/strategies/ranking", handler.GetStrategiesRanking)

			// ── Settings ──
			settingsG := api.Group("/settings")
			{
				settingsG.GET("/agent/models", handler.SettingsAgentModels)
				settingsG.GET("/defaults", handler.SettingsDefaultsGet)
				settingsG.POST("/defaults", handler.SettingsDefaultsSave)
				settingsG.POST("/ui", handler.SettingsUISave)
				settingsG.POST("/exchange/:id/test", handler.SettingsExchangeTest)
				settingsG.PUT("/exchange/:id", handler.SettingsExchangeSave)
				settingsG.POST("/ai/:id/test", handler.SettingsAITest)
				settingsG.PUT("/ai/:id", handler.SettingsAISave)
			}

			// ── AI ──
		api.GET("/ai/snapshot", handler.AISnapshot)
		api.GET("/ai/klines", handler.AIKlines)
		api.POST("/ai/generate", handler.AIGenerate)
		api.POST("/strategies/ai-generate", handler.StrategyAIGenerate)
		api.POST("/ai/multi-agent", handler.AIMultiAgent)
		api.POST("/ai/backtest", handler.AIBacktest)
		api.POST("/ai/optimize", handler.AIOptimize)
		api.POST("/ai/deploy", handler.AIDeploy)
		api.POST("/ai/validate", handler.AIValidate)
		api.POST("/ai/fix", handler.AIFix)
		api.POST("/ai/analyze", handler.AIAnalyze)
		api.GET("/ai/quickscan", handler.AIQuickScan)
		api.POST("/ai/chat", handler.AIChat)

	// ── ML ──
	api.GET("/ml/health", handler.MLHealth)
	api.POST("/ml/train", handler.MLTrain)
	api.POST("/ml/predict", handler.MLPredict)
	api.GET("/ml/models", handler.MLModels)
	api.GET("/ml/models/:id", handler.MLModelDetail)
	api.DELETE("/ml/models/:id", handler.MLDeleteModel)
	api.GET("/ml/models/:id/importance", handler.MLFeatureImportance)
	api.POST("/ml/features", handler.MLGenerateFeatures)
api.POST("/ml/deploy", handler.MLDeployStrategy)
api.GET("/ml/strategy-models", handler.MLStrategyModels)
		api.POST("/ai/models/config", handler.AIModelConfigSave)
		api.GET("/models/list", handler.AIModelsList)
		api.GET("/ai/models", handler.AIModels)
		api.GET("/auto-trade/config", handler.AIAutoTradeGet)
		api.PUT("/auto-trade/config", handler.AIAutoTradeSave)
		api.POST("/analysis/start", handler.AIAnalysisStart)
		api.GET("/analysis/result", handler.AIAnalysisResult)
		api.POST("/chat/send", handler.ChatSend)

		// ── Agent ──
		agent := api.Group("/agent")
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

		// ── Dashboard ──
		api.GET("/dashboard/summary", handler.DashboardSummary)

		// ── Portfolio (new) ──
		api.GET("/portfolio/summary", handler.PortfolioSummary)
		api.GET("/portfolio/positions", handler.PortfolioPositions)
		api.GET("/portfolio/snapshots", handler.PortfolioSnapshots)
		api.GET("/portfolio/calendar", handler.PortfolioCalendar)
		api.GET("/exchange/usdcny", handler.UsdCnyRate)

		// ── Settings ──
		api.GET("/settings/currency", handler.SettingsCurrencyGet)
		api.PUT("/settings/currency", handler.SettingsCurrencySet)

		// ── Watchdog (new) ──
		api.GET("/health", handler.HealthCheck)
		api.GET("/health/components", handler.ComponentHealth)

		// ── Indicator IDE ──
		indicatorG := api.Group("/indicator")
		indicatorG.Use(middleware.AuthRequired())
		{
			indicatorG.POST("/parse", indicator.ParseIndicator)
			indicatorG.POST("/validate", indicator.ValidateIndicator)
			indicatorG.POST("/save", indicator.SaveIndicator)
			indicatorG.GET("/list", indicator.ListIndicators)
			indicatorG.GET("/:id", indicator.GetIndicator)
			indicatorG.DELETE("/:id", indicator.DeleteIndicator)
			indicatorG.POST("/applyParamDefaults", indicator.ApplyParamDefaults)
			indicatorG.POST("/execute", indicator.SandboxExecute)
			indicatorG.POST("/analyze", indicator.SandboxAnalyze)
			indicatorG.POST("/ai-generate", indicator.IndicatorAIGenerate)
			indicatorG.POST("/publish", indicator.PublishIndicator)
		}

		// Internal indicator call (for sandbox call_indicator()) — no auth, IP-restricted in production
		api.POST("/indicator/internal-call", indicator.InternalCallIndicator)

		// ── Experiment Pipeline ──
		experimentG := api.Group("/experiment")
		experimentG.Use(middleware.AuthRequired())
		{
			experimentG.POST("/run", experiment.RunExperimentHandler)
			experimentG.POST("/sensitivity", experiment.SensitivityAnalysisHandler)
			experimentG.POST("/walk-forward", experiment.WalkForwardHandler)
			experimentG.GET("/status/:id", experiment.ExperimentStatusHandler)
		}

		// ── Community ──
		comm := api.Group("/community")
		comm.Use(middleware.AuthRequired())
		{
			comm.GET("/indicators", community.MarketIndicators)
			comm.POST("/publish", community.PublishIndicator)
			comm.POST("/purchase/:id", community.PurchaseIndicator)
			comm.GET("/comments/:id", community.GetComments)
			comm.POST("/comments/:id", community.AddComment)

		// ── Strategy Marketplace ──
		comm.GET("/strategies", community.MarketStrategies)
		comm.GET("/strategies/leaderboard", community.StrategyLeaderboard)
		comm.GET("/strategies/:id", community.StrategyDetail)
		comm.POST("/strategies/publish", community.PublishStrategy)
		comm.POST("/strategies/:id/comment", community.AddStrategyComment)
		comm.POST("/strategies/:id/rate", community.RateStrategy)
		}

	}

	// ── Webhooks ──
	api.POST("/webhook/tv", handler.TradingViewWebhook)
	api.POST("/webhook/generic", handler.GenericWebhook)

	// ── WebSocket ──
	r.GET("/ws", handler.WSHandler)

	// ── Start background tasks ──
	go handler.StartBackgroundTasks()

	port := cfg.Server.Port
	if port == "" {
		port = os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
	}

	appCtx.Logger.Info("XiaoTianQuant Gateway starting", "port", port)

	// ── Start server with graceful shutdown ──
	go func() {
		appCtx.WaitForShutdown()
	}()

	if err := r.Run("0.0.0.0:" + port); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
