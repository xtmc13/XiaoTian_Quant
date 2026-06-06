package main

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/community"
	"github.com/xiaotian-quant/gateway/internal/config"
	"github.com/xiaotian-quant/gateway/internal/experiment"
	"github.com/xiaotian-quant/gateway/internal/handler"
	"github.com/xiaotian-quant/gateway/internal/indicator"
	"github.com/xiaotian-quant/gateway/internal/metrics"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/onchain"
	"github.com/xiaotian-quant/gateway/internal/social"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/strategies"
	"github.com/xiaotian-quant/gateway/internal/ws"
	"github.com/xiaotian-quant/gateway/spa"
)

func main() {
	// ── Load configuration ──
	cfg, err := config.Load("config.yaml")
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
		if isFatalInitErr(err) {
			log.Fatalf("FATAL: %v", err)
		}
		log.Printf("WARNING: SQLite init skipped: %v", err)
	}
	store.LoadConfig()
	store.LoadStrategyConfigs()

	// Register strategy factories for combo engine
	strategy.RegisterStrategyFactory("breakout", func() strategy.Strategy { return strategies.NewBreakoutStrategy() })
	strategy.RegisterStrategyFactory("ema_cross", func() strategy.Strategy { return strategies.NewEMACrossStrategy() })
	strategy.RegisterStrategyFactory("macd", func() strategy.Strategy { return strategies.NewMACDStrategy() })
	strategy.RegisterStrategyFactory("rsi", func() strategy.Strategy { return strategies.NewRSIStrategy() })
	strategy.RegisterStrategyFactory("bollinger_bands", func() strategy.Strategy { return strategies.NewBollingerBandsStrategy() })
	strategy.RegisterStrategyFactory("atr_trailing_stop", func() strategy.Strategy { return strategies.NewATRTrailingStopStrategy() })
	strategy.RegisterStrategyFactory("dual_thrust", func() strategy.Strategy { return strategies.NewDualThrustStrategy() })
	strategy.RegisterStrategyFactory("renko", func() strategy.Strategy { return strategies.NewRenkoStrategy() })
	strategy.RegisterStrategyFactory("grid_trading", func() strategy.Strategy { return strategies.NewGridTradingStrategy() })
	strategy.RegisterStrategyFactory("arbitrage", func() strategy.Strategy { return strategies.NewArbitrageStrategy() })
	strategy.RegisterStrategyFactory("market_making", func() strategy.Strategy { return strategies.NewMarketMakingStrategy() })
	strategy.RegisterStrategyFactory("martingale", func() strategy.Strategy { return strategies.NewMartingaleStrategy() })
	strategy.RegisterStrategyFactory("wallstreet", func() strategy.Strategy { return strategies.NewWallstreetStrategy() })

	// ── Setup Gin ──
	if cfg.Server.Mode == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(middleware.RequestLogger(appCtx.Logger), gin.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.UnifiedResponseWrapper())

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
		// ── Auth (with rate limiting) ──
		auth := api.Group("/auth")
		auth.Use(middleware.StrictRateLimiter())
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

		// ── Authenticated routes ──
		private := api.Group("")
		private.Use(middleware.AuthRequired())

		// ── Config ──
		private.GET("/config", handler.GetConfig)
		private.PUT("/config", handler.SaveConfig)
		private.POST("/config", handler.SaveConfig)
		private.GET("/strategies/global", handler.GetGlobalStrategy)
		private.GET("/strategies/param-defs", handler.GetStrategyParamDefs)
		private.PUT("/strategies/global", handler.SaveGlobalStrategy)
		private.POST("/exchange/save", handler.ExchangeSave)
		private.POST("/exchange/test", handler.ExchangeTest)
		private.POST("/exchange/default", handler.ExchangeDefault)
		private.GET("/exchange/status", handler.ExchangeStatus)
		private.GET("/exchanges/configured", handler.ExchangesConfigured)
		private.POST("/ai/save", handler.AISave)
		private.POST("/ai/test", handler.AITest)
		private.POST("/ai/default", handler.AIDefault)
		private.POST("/restart", handler.Restart)

		// ── Orders ──
		private.GET("/orders", handler.GetOrders)
		private.POST("/orders", handler.PlaceOrder)
		// Static sub-routes (must be BEFORE :order_id param routes to avoid
		// httprouter conflict between param and static segments)
		private.POST("/orders/cancel-all", handler.CancelAllOrders)
		private.GET("/orders/history", handler.OrderHistory)
		// ── Advanced Orders (static paths) ──
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
		// Param routes (must be after static routes to avoid httprouter conflict)
		private.DELETE("/orders/:order_id", handler.CancelOrder)
		private.POST("/orders/:order_id/cancel", handler.CancelOrder)

		// ── Account ──
		private.GET("/account/balance", handler.GetAccountBalance)

		// ── Trades ──
		private.GET("/trades", handler.GetTradeHistory)

		// ── Market ──
		api.GET("/klines/:symbol", handler.GetKlines)
		api.GET("/market/klines", handler.MarketKlines)
		api.GET("/market/orderbook", handler.OrderBook)
		api.GET("/market/trades", handler.MarketTrades)
		private.POST("/backtest/run", handler.RunBacktest)
		private.POST("/native/backtest", handler.NativeBacktest)
		api.GET("/symbols/search", handler.SymbolSearch)
		api.GET("/market/snapshot", handler.MarketSnapshot)
		api.GET("/status", handler.Status)

		// ── Notifications ──
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
		// ── Arbitrage ──
		private.GET("/arbitrage/config", handler.GetArbitrageConfig)
		private.POST("/arbitrage/config", handler.UpdateArbitrageConfig)
		private.POST("/arbitrage/start", handler.StartArbitrage)
		private.POST("/arbitrage/stop", handler.StopArbitrage)
		private.GET("/arbitrage/status", handler.GetArbitrageStatus)
		private.GET("/arbitrage/opportunity", handler.GetArbitrageOpportunity)
		private.GET("/arbitrage/positions", handler.GetArbitragePositions)
		private.GET("/arbitrage/history", handler.GetArbitrageHistory)
		private.POST("/arbitrage/exchanges", handler.RegisterArbitrageExchange)
		private.GET("/arbitrage/exchanges", handler.ListArbitrageExchanges)
		private.POST("/arbitrage/execute", handler.ExecuteArbitrage)

		// ── Data Management ──
		private.GET("/data/coverage", handler.GetDataCoverage)
		private.GET("/data/info", handler.GetDataInfo)
		private.POST("/data/download", handler.StartDataDownload)
		private.GET("/data/download/:id", handler.GetDownloadJob)
		private.GET("/data/bars", handler.GetHistoricalBars)

		// ── Tick Data Management ──
		private.POST("/data/ticks/download", handler.StartTickDownload)
		private.GET("/data/ticks", handler.GetTicks)
		private.GET("/data/ticks/info", handler.GetTickInfo)
		private.POST("/backtest/tick", handler.RunTickBacktest)
		private.GET("/backtest/tick/jobs", handler.ListTickBacktestJobs)
		private.GET("/backtest/tick/jobs/:id", handler.GetTickBacktestJob)

		// ── Strategy ──
		private.GET("/strategies/configs", handler.GetStrategyConfigs)
		private.GET("/strategies/configs/:id", handler.GetStrategyConfig)
		private.POST("/strategies/configs", handler.CreateStrategyConfig)
		private.PUT("/strategies/configs/:id", handler.UpdateStrategyConfig)
		private.DELETE("/strategies/configs/:id", handler.DeleteStrategyConfig)
		private.POST("/strategies/configs/batch-start", handler.BatchStartConfigs)
		private.POST("/strategies/configs/batch-stop", handler.BatchStopConfigs)
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

		// ── Combo ──
		private.GET("/combos", handler.GetCombos)
		private.POST("/combos", handler.CreateCombo)
		private.GET("/combos/:id", handler.GetCombo)
		private.PUT("/combos/:id", handler.UpdateCombo)
		private.DELETE("/combos/:id", handler.DeleteCombo)
		private.POST("/combos/:id/start", handler.StartCombo)
		private.POST("/combos/:id/stop", handler.StopCombo)
		private.GET("/combos/:id/signals", handler.GetComboSignals)

			// ── Pairlist ──
			private.GET("/pairlist/whitelist", handler.GetPairlistWhitelist)
			private.POST("/pairlist/refresh", handler.RefreshPairlist)
			private.GET("/pairlist/config", handler.GetPairlistConfig)
			private.POST("/pairlist/config", handler.ConfigurePairlist)

			// ── Protection ──
			private.GET("/protection/status", handler.GetProtectionStatus)
			private.POST("/protection/config", handler.ConfigureProtection)
			private.POST("/protection/reset", handler.ResetProtection)
			private.POST("/protection/trade", handler.RecordTrade)

			// ── Hyperopt ──
			private.POST("/hyperopt/start", handler.StartHyperopt)
			private.GET("/hyperopt/jobs", handler.ListHyperoptJobs)
			private.GET("/hyperopt/jobs/:id", handler.GetHyperoptJob)
			private.POST("/hyperopt/jobs/:id/cancel", handler.CancelHyperoptJob)
			private.DELETE("/hyperopt/jobs/:id", handler.DeleteHyperoptJob)
			private.GET("/hyperopt/spaces", handler.GetHyperoptSpaces)
			private.POST("/hyperopt/jobs/:id/export", handler.ExportHyperoptParams)

			// ── RL (Reinforcement Learning) ──
			private.POST("/rl/train", handler.RLTrain)
			private.POST("/rl/predict", handler.RLPredict)
			private.POST("/rl/evaluate", handler.RLEvaluate)
			private.GET("/rl/models", handler.ListRLModels)
			private.DELETE("/rl/models/:id", handler.DeleteRLModel)
			private.GET("/rl/jobs/:id", handler.GetRLJob)
			private.POST("/rl/jobs/:id/cancel", handler.CancelRLJob)
			private.GET("/rl/worker/status", handler.GetRLWorkerStatus)
			private.POST("/rl/worker/start", handler.StartRLWorker)

			// ── TensorBoard ──
			private.GET("/tensorboard/runs", handler.ListTensorBoardRuns)
			private.POST("/tensorboard/scalars", handler.QueryTensorBoardScalars)
			private.GET("/tensorboard/runs/:id", handler.GetTensorBoardRun)
			private.DELETE("/tensorboard/runs/:id", handler.DeleteTensorBoardRun)

		// ── Settings ──
		settingsG := private.Group("/settings")
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

		// ── ML ──
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

		// ── Agent ──
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

		// ── Dashboard ──
		private.GET("/dashboard/summary", handler.DashboardSummary)

		// ── Portfolio (new) ──
		private.GET("/portfolio/summary", handler.PortfolioSummary)
		private.GET("/portfolio/positions", handler.PortfolioPositions)
		private.GET("/portfolio/snapshots", handler.PortfolioSnapshots)
		private.GET("/portfolio/calendar", handler.PortfolioCalendar)
		private.GET("/exchange/usdcny", handler.UsdCnyRate)

		// ── Settings ──
		private.GET("/settings/currency", handler.SettingsCurrencyGet)
		private.PUT("/settings/currency", handler.SettingsCurrencySet)

		// ── Watchdog (new) ──
		api.GET("/health", handler.HealthCheck)
		api.GET("/health/components", handler.ComponentHealth)
		api.POST("/admin/config/reload", middleware.AdminRequired(), handler.ReloadConfig)

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

			// ── Review & Moderation ──
			comm.POST("/review/:id", community.ReviewIndicator)
			comm.GET("/reviews/pending", community.PendingReviews)

			// ── Author Revenue ──
			comm.GET("/author/revenue", community.AuthorRevenue)

			// ── Strategy Marketplace ──
			comm.GET("/strategies", community.MarketStrategies)
			comm.GET("/strategies/leaderboard", community.StrategyLeaderboard)
			comm.GET("/strategies/trending", community.TrendingStrategies)
			comm.GET("/strategies/:id", community.StrategyDetail)
			comm.GET("/strategies/:id/overfit", community.GetStrategyOverfitRisk)
			comm.POST("/strategies/publish", community.PublishStrategy)
			comm.POST("/strategies/:id/comment", community.AddStrategyComment)
			comm.POST("/strategies/:id/rate", community.RateStrategy)
		}

		// ── Social Trading ──
		socialG := api.Group("/social")
		socialG.Use(middleware.AuthRequired())
		{
			// Initialize social trading engine
			socialEngine := social.NewEngine()
			social.RegisterRoutes(socialG, socialEngine)
		}

		// ── On-Chain Data ──
		onchainG := api.Group("/onchain")
		onchainG.Use(middleware.AuthRequired())
		{
			onchainClient := onchain.NewClient("")
			onchain.RegisterRoutes(onchainG, onchainClient)
		}

	}

	// ── Webhooks ──
	api.POST("/webhook/tv", handler.TradingViewWebhook)
	api.POST("/webhook/generic", handler.GenericWebhook)

		r.GET("/ws", handler.WSHandler)
		r.GET("/ws/v2", ws.HubHandler)
		api.GET("/ws/stats", ws.Stats)

	// ── Metrics ──
	r.GET("/metrics", func(c *gin.Context) {
		metrics.Handler(c.Writer, c.Request)
	})

	// ── pprof (debug) ──
	// Restricted to local/loopback in production; open in debug mode.
	pprofGroup := r.Group("/debug/pprof")
	pprofGroup.Use(func(c *gin.Context) {
		if cfg.Server.Mode != "debug" {
			clientIP := c.ClientIP()
			if !isLocalhost(clientIP) {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
		}
		c.Next()
	})
	pprofGroup.GET("/*any", gin.WrapF(http.DefaultServeMux.ServeHTTP))

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
	srv := &http.Server{
		Addr:    "0.0.0.0:" + port,
		Handler: metrics.HTTPMiddleware(r),
	}

	// Wait for shutdown signal in a goroutine
	go func() {
		appCtx.WaitForShutdown()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Server forced to shutdown: %v", err)
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}

// isFatalInitErr determines whether a store initialization error should stop the server.
func isFatalInitErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SECRET_KEY") && strings.Contains(msg, "required in production")
}

// isLocalhost checks if an IP address is loopback.
func isLocalhost(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1" || ip == "localhost"
}
