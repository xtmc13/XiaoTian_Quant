package store

// Schema migration constants and DDL for all 18 tables.

const currentSchemaVersion = 13

// MigrationFunc is a function that upgrades the schema by one version.
type MigrationFunc func(tx *dbTx) error

var migrations = map[int]MigrationFunc{
	1:  migrateV1,
	2:  migrateV2,
	3:  migrateV3,
	4:  migrateV4,
	5:  migrateV5,
	6:  migrateV6,
	7:  migrateV7,
	8:  migrateV8,
	9:  migrateV9,
	10: migrateV10,
	11: migrateV11,
	12: migrateV12,
	13: migrateV13,
}

// dbTx wraps a database transaction for migrations.
type dbTx struct {
	exec func(query string, args ...any) error
}

// RunMigrations applies any pending schema migrations.
func RunMigrations() error {
	if db == nil {
		return nil
	}

	// Ensure version tracking table
	db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)`)

	var currentVersion int
	row := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&currentVersion); err != nil {
		currentVersion = 0
	}

	for v := currentVersion + 1; v <= currentSchemaVersion; v++ {
		migrateFn, ok := migrations[v]
		if !ok {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}

		wrapper := &dbTx{
			exec: func(query string, args ...any) error {
				_, err := tx.Exec(query, args...)
				return err
			},
		}

		if err := migrateFn(wrapper); err != nil {
			tx.Rollback()
			return err
		}

		if _, err := tx.Exec("INSERT OR REPLACE INTO schema_version (version) VALUES (?)", v); err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

// migrateV1 creates the initial tables (already in InitDB).
func migrateV1(tx *dbTx) error {
	return nil
}

// migrateV2 adds all the missing tables for the full trading platform.
func migrateV2(tx *dbTx) error {
	tables := []string{
		// ── Trading tables ──
		`CREATE TABLE IF NOT EXISTS trades (
			id TEXT PRIMARY KEY,
			order_id TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			price REAL NOT NULL,
			quantity REAL NOT NULL,
			fee REAL DEFAULT 0,
			fee_currency TEXT DEFAULT 'USDT',
			exchange TEXT DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trades_symbol ON trades(symbol)`,
		`CREATE INDEX IF NOT EXISTS idx_trades_order ON trades(order_id)`,
		`CREATE INDEX IF NOT EXISTS idx_trades_time ON trades(created_at)`,

		// ── Positions ──
		`CREATE TABLE IF NOT EXISTS positions (
			id TEXT PRIMARY KEY,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			quantity REAL NOT NULL DEFAULT 0,
			avg_entry_price REAL NOT NULL DEFAULT 0,
			current_price REAL DEFAULT 0,
			unrealized_pnl REAL DEFAULT 0,
			realized_pnl REAL DEFAULT 0,
			cost_basis REAL DEFAULT 0,
			exchange TEXT DEFAULT '',
			status TEXT DEFAULT 'OPEN',
			opened_at INTEGER NOT NULL,
			closed_at INTEGER DEFAULT 0,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_positions_symbol ON positions(symbol)`,
		`CREATE INDEX IF NOT EXISTS idx_positions_status ON positions(status)`,

		// ── Signals ──
		`CREATE TABLE IF NOT EXISTS xt_signals (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol TEXT NOT NULL,
			direction TEXT NOT NULL,
			strength REAL DEFAULT 0,
			strategy TEXT NOT NULL,
			reason TEXT DEFAULT '',
			entry_price REAL DEFAULT 0,
			stop_loss REAL DEFAULT 0,
			take_profit REAL DEFAULT 0,
			position_size REAL DEFAULT 0,
			status TEXT DEFAULT 'PENDING',
			executed_order_id TEXT DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_signals_symbol ON xt_signals(symbol)`,
		`CREATE INDEX IF NOT EXISTS idx_signals_strategy ON xt_signals(strategy)`,
		`CREATE INDEX IF NOT EXISTS idx_signals_status ON xt_signals(status)`,

		// ── Risk Events ──
		`CREATE TABLE IF NOT EXISTS risk_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			level TEXT NOT NULL,
			check_name TEXT NOT NULL,
			message TEXT NOT NULL,
			symbol TEXT DEFAULT '',
			context_json TEXT DEFAULT '{}',
			timestamp INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_riskevents_time ON risk_events(timestamp)`,

		// ── Market Bars ──
		`CREATE TABLE IF NOT EXISTS market_bars (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol TEXT NOT NULL,
			interval TEXT NOT NULL,
			open REAL NOT NULL,
			high REAL NOT NULL,
			low REAL NOT NULL,
			close REAL NOT NULL,
			volume REAL NOT NULL DEFAULT 0,
			timestamp INTEGER NOT NULL,
			UNIQUE(symbol, interval, timestamp)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bars_symbol_interval ON market_bars(symbol, interval, timestamp)`,

		// ── Strategy Configs (backed by json file, schema for DB fallback) ──
		`CREATE TABLE IF NOT EXISTS strategy_configs (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			symbol TEXT NOT NULL,
			config_json TEXT NOT NULL DEFAULT '{}',
			compiled_code TEXT DEFAULT '',
			is_enabled INTEGER DEFAULT 0,
			is_running INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,

		// ── Portfolio Snapshots ──
		`CREATE TABLE IF NOT EXISTS xt_portfolio_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			total_equity REAL NOT NULL,
			available_balance REAL NOT NULL,
			margin_used REAL DEFAULT 0,
			drawdown REAL DEFAULT 0,
			net_exposure REAL DEFAULT 0,
			positions_json TEXT DEFAULT '[]',
			balances_json TEXT DEFAULT '[]',
			timestamp INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_port_snap_time ON xt_portfolio_snapshots(timestamp)`,

		// ── Backtests ──
		`CREATE TABLE IF NOT EXISTS xt_backtests (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			strategy TEXT NOT NULL,
			symbol TEXT NOT NULL,
			start_time INTEGER NOT NULL,
			end_time INTEGER NOT NULL,
			duration_ms INTEGER DEFAULT 0,
			status TEXT DEFAULT 'PENDING',
			report_json TEXT DEFAULT '{}',
			created_at INTEGER NOT NULL,
			completed_at INTEGER DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backtests_status ON xt_backtests(status)`,

		// ── Agent Tokens ──
		`CREATE TABLE IF NOT EXISTS agent_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			token_hash TEXT UNIQUE NOT NULL,
			token_prefix TEXT NOT NULL,
			scopes TEXT DEFAULT 'read',
			rate_limit_rps INTEGER DEFAULT 10,
			is_active INTEGER DEFAULT 1,
			expires_at INTEGER DEFAULT 0,
			last_used_at INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL
		)`,

		// ── Agent Audit Log ──
		`CREATE TABLE IF NOT EXISTS agent_audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token_id INTEGER,
			name TEXT DEFAULT '',
			endpoint TEXT NOT NULL,
			method TEXT DEFAULT 'POST',
			params_summary TEXT DEFAULT '',
			status_code INTEGER DEFAULT 200,
			ip TEXT DEFAULT '',
			user_agent TEXT DEFAULT '',
			timestamp INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_token ON agent_audit_log(token_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_time ON agent_audit_log(timestamp)`,
	}

	for _, ddl := range tables {
		if err := tx.exec(ddl); err != nil {
			return err
		}
	}

	return nil
}

// migrateV3 adds email verification support.
func migrateV3(tx *dbTx) error {
	// Add email_verified column to xt_users (ignore error if column already exists)
	tx.exec("ALTER TABLE xt_users ADD COLUMN email_verified INTEGER DEFAULT 0")

	// Verification codes table
	tables := []string{
		`CREATE TABLE IF NOT EXISTS xt_verification_codes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL,
			code TEXT NOT NULL,
			code_type TEXT NOT NULL,
			expires_at INTEGER NOT NULL,
			used_at INTEGER DEFAULT 0,
			attempts INTEGER DEFAULT 0,
			ip_address TEXT DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_vcode_email_type ON xt_verification_codes(email, code_type)`,
	}

	for _, ddl := range tables {
		if err := tx.exec(ddl); err != nil {
			return err
		}
	}

	return nil
}

// migrateV4 adds indicator_codes table for the Indicator IDE.
func migrateV4(tx *dbTx) error {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS indicator_codes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			code TEXT NOT NULL,
			params_json TEXT DEFAULT '{}',
			strategy_json TEXT DEFAULT '{}',
			is_encrypted INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_indicator_user ON indicator_codes(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_indicator_updated ON indicator_codes(updated_at)`,
	}
	for _, ddl := range tables {
		if err := tx.exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

// migrateV6 adds social trading tables (signals and copy_trades).
func migrateV6(tx *dbTx) error {
	tables := []string{
		// Social trading signals
		`CREATE TABLE IF NOT EXISTS social_signals (
			id TEXT PRIMARY KEY,
			provider_id INTEGER NOT NULL,
			provider_name TEXT DEFAULT '',
			symbol TEXT NOT NULL,
			direction TEXT NOT NULL,
			price REAL DEFAULT 0,
			stop_loss REAL DEFAULT 0,
			take_profit REAL DEFAULT 0,
			size REAL DEFAULT 0,
			confidence REAL DEFAULT 0,
			strategy TEXT DEFAULT '',
			reason TEXT DEFAULT '',
			timestamp INTEGER NOT NULL,
			expires_at INTEGER DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_social_signals_provider ON social_signals(provider_id)`,
		`CREATE INDEX IF NOT EXISTS idx_social_signals_symbol ON social_signals(symbol)`,
		`CREATE INDEX IF NOT EXISTS idx_social_signals_time ON social_signals(timestamp)`,

		// Copy trades execution log
		`CREATE TABLE IF NOT EXISTS copy_trades (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			follower_id INTEGER NOT NULL,
			signal_id TEXT NOT NULL,
			provider_id INTEGER NOT NULL,
			symbol TEXT NOT NULL,
			direction TEXT NOT NULL,
			executed INTEGER DEFAULT 0,
			reason TEXT DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_copy_trades_follower ON copy_trades(follower_id)`,
		`CREATE INDEX IF NOT EXISTS idx_copy_trades_provider ON copy_trades(provider_id)`,
		`CREATE INDEX IF NOT EXISTS idx_copy_trades_signal ON copy_trades(signal_id)`,
	}
	for _, ddl := range tables {
		if err := tx.exec(ddl); err != nil {
			return err
		}
	}
	return nil
}
func migrateV5(tx *dbTx) error {
	tables := []string{
		// Extend indicator_codes with community fields
		`ALTER TABLE indicator_codes ADD COLUMN publish_to_community INTEGER DEFAULT 0`,
		`ALTER TABLE indicator_codes ADD COLUMN pricing_type TEXT DEFAULT 'free'`,
		`ALTER TABLE indicator_codes ADD COLUMN price REAL DEFAULT 0`,
		`ALTER TABLE indicator_codes ADD COLUMN review_status TEXT DEFAULT 'pending'`,
		`ALTER TABLE indicator_codes ADD COLUMN review_note TEXT DEFAULT ''`,
		`ALTER TABLE indicator_codes ADD COLUMN reviewed_by INTEGER DEFAULT 0`,
		`ALTER TABLE indicator_codes ADD COLUMN purchase_count INTEGER DEFAULT 0`,
		`ALTER TABLE indicator_codes ADD COLUMN avg_rating REAL DEFAULT 0`,
		`ALTER TABLE indicator_codes ADD COLUMN rating_count INTEGER DEFAULT 0`,
		`ALTER TABLE indicator_codes ADD COLUMN view_count INTEGER DEFAULT 0`,
		`ALTER TABLE indicator_codes ADD COLUMN source_indicator_id INTEGER DEFAULT 0`,
		`ALTER TABLE indicator_codes ADD COLUMN source_language TEXT DEFAULT ''`,
		`ALTER TABLE indicator_codes ADD COLUMN name_i18n TEXT DEFAULT ''`,
		`ALTER TABLE indicator_codes ADD COLUMN description_i18n TEXT DEFAULT ''`,

		// Community indexes
		`CREATE INDEX IF NOT EXISTS idx_indicator_publish ON indicator_codes(publish_to_community)`,
		`CREATE INDEX IF NOT EXISTS idx_indicator_review ON indicator_codes(review_status)`,
		`CREATE INDEX IF NOT EXISTS idx_indicator_source ON indicator_codes(source_indicator_id)`,

		// Purchases table
		`CREATE TABLE IF NOT EXISTS indicator_purchases (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			indicator_id INTEGER NOT NULL,
			buyer_id INTEGER NOT NULL,
			seller_id INTEGER NOT NULL,
			price REAL DEFAULT 0,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_purchases_buyer ON indicator_purchases(buyer_id)`,
		`CREATE INDEX IF NOT EXISTS idx_purchases_indicator ON indicator_purchases(indicator_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_purchases_unique ON indicator_purchases(indicator_id, buyer_id)`,

		// Comments / ratings table
		`CREATE TABLE IF NOT EXISTS indicator_comments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			indicator_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			rating INTEGER DEFAULT 5,
			content TEXT DEFAULT '',
			parent_id INTEGER DEFAULT 0,
			is_deleted INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_indicator ON indicator_comments(indicator_id)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_user ON indicator_comments(user_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_comments_unique ON indicator_comments(indicator_id, user_id, parent_id)`,
	}
	for _, ddl := range tables {
		// SQLite ALTER TABLE may fail if column already exists; ignore those errors
		_ = tx.exec(ddl)
	}
	return nil
}

// migrateV8 adds strategy_overfit table for KPI overfit persistence.
func migrateV8(tx *dbTx) error {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS strategy_overfit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			strategy_id INTEGER NOT NULL UNIQUE,
			score REAL DEFAULT 0,
			risk_level TEXT DEFAULT 'low',
			in_sample_return REAL DEFAULT 0,
			out_sample_return REAL DEFAULT 0,
			return_ratio REAL DEFAULT 0,
			stability_score REAL DEFAULT 0,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_overfit_strategy ON strategy_overfit(strategy_id)`,
	}
	for _, ddl := range tables {
		if err := tx.exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

// migrateV7 adds missing indexes for frequently queried tables.
// These indexes target the hottest query paths identified by code audit.
func migrateV7(tx *dbTx) error {
	indexes := []string{
		// ── xt_users: login lookups (FindUserByUsername, FindUserByEmail) ──
		`CREATE INDEX IF NOT EXISTS idx_users_username ON xt_users(username)`,
		`CREATE INDEX IF NOT EXISTS idx_users_email ON xt_users(email)`,
		`CREATE INDEX IF NOT EXISTS idx_users_active ON xt_users(is_active)`,

		// ── xt_orders: order listing by symbol / status ──
		`CREATE INDEX IF NOT EXISTS idx_orders_symbol ON xt_orders(symbol)`,
		`CREATE INDEX IF NOT EXISTS idx_orders_status ON xt_orders(status)`,
		`CREATE INDEX IF NOT EXISTS idx_orders_created ON xt_orders(created_at)`,

		// ── strategy_configs: list / filter by status ──
		`CREATE INDEX IF NOT EXISTS idx_stratcfg_name ON strategy_configs(name)`,
		`CREATE INDEX IF NOT EXISTS idx_stratcfg_symbol ON strategy_configs(symbol)`,
		`CREATE INDEX IF NOT EXISTS idx_stratcfg_enabled ON strategy_configs(is_enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_stratcfg_running ON strategy_configs(is_running)`,

		// ── agent_tokens: list active / check expiry ──
		`CREATE INDEX IF NOT EXISTS idx_agent_tokens_active ON agent_tokens(is_active)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_tokens_expires ON agent_tokens(expires_at)`,
	}
	for _, ddl := range indexes {
		if err := tx.exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

// migrateV9 adds AI Bot marketplace, instance, subscription and snapshot tables.
func migrateV9(tx *dbTx) error {
	tables := []string{
		// ── AI Bot Catalog (built-in bots marketplace) ──
		`CREATE TABLE IF NOT EXISTS ai_bot_catalog (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			strategy_type TEXT NOT NULL,
			market_type TEXT DEFAULT 'spot',
			risk_level TEXT DEFAULT 'medium',
			fee_model TEXT DEFAULT 'free',
			fee_percent REAL DEFAULT 0,
			monthly_fee REAL DEFAULT 0,
			performance_json TEXT DEFAULT '{}',
			config_json TEXT DEFAULT '{}',
			is_builtin INTEGER DEFAULT 1,
			is_active INTEGER DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_aibot_catalog_type ON ai_bot_catalog(strategy_type)`,
		`CREATE INDEX IF NOT EXISTS idx_aibot_catalog_risk ON ai_bot_catalog(risk_level)`,
		`CREATE INDEX IF NOT EXISTS idx_aibot_catalog_active ON ai_bot_catalog(is_active)`,

		// ── AI Bot Instances (user-deployed bots) ──
		`CREATE TABLE IF NOT EXISTS ai_bot_instances (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			catalog_id TEXT DEFAULT '',
			name TEXT NOT NULL,
			strategy_type TEXT NOT NULL,
			symbol TEXT NOT NULL,
			market_type TEXT DEFAULT 'spot',
			status TEXT DEFAULT 'stopped',
			execution_mode TEXT DEFAULT 'paper',
			config_json TEXT DEFAULT '{}',
			exchange_id TEXT DEFAULT '',
			unrealized_pnl REAL DEFAULT 0,
			realized_pnl REAL DEFAULT 0,
			total_return_pct REAL DEFAULT 0,
			max_drawdown_pct REAL DEFAULT 0,
			sharpe_ratio REAL DEFAULT 0,
			win_rate REAL DEFAULT 0,
			total_trades INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			started_at INTEGER DEFAULT 0,
			stopped_at INTEGER DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_aibot_inst_user ON ai_bot_instances(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_aibot_inst_status ON ai_bot_instances(status)`,
		`CREATE INDEX IF NOT EXISTS idx_aibot_inst_symbol ON ai_bot_instances(symbol)`,

		// ── AI Bot Subscriptions (profit-sharing / monthly fee) ──
		`CREATE TABLE IF NOT EXISTS ai_bot_subscriptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			bot_instance_id TEXT NOT NULL,
			fee_type TEXT DEFAULT 'profit_share',
			fee_percent REAL DEFAULT 0,
			monthly_fee REAL DEFAULT 0,
			next_billing_at INTEGER DEFAULT 0,
			status TEXT DEFAULT 'active',
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_aibot_sub_user ON ai_bot_subscriptions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_aibot_sub_bot ON ai_bot_subscriptions(bot_instance_id)`,

		// ── AI Bot Performance Snapshots (for analytics charts) ──
		`CREATE TABLE IF NOT EXISTS ai_bot_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			bot_instance_id TEXT NOT NULL,
			total_equity REAL DEFAULT 0,
			unrealized_pnl REAL DEFAULT 0,
			realized_pnl REAL DEFAULT 0,
			total_return_pct REAL DEFAULT 0,
			timestamp INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_aibot_snap_bot ON ai_bot_snapshots(bot_instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_aibot_snap_time ON ai_bot_snapshots(timestamp)`,
	}
	for _, ddl := range tables {
		if err := tx.exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

// migrateV10 adds AI bot trade history, initial balance and subscription cancel support.
func migrateV10(tx *dbTx) error {
	tables := []string{
		// Track initial balance per bot instance for realistic paper trading equity.
		`ALTER TABLE ai_bot_instances ADD COLUMN initial_balance REAL DEFAULT 10000`,
		`ALTER TABLE ai_bot_instances ADD COLUMN error_message TEXT DEFAULT ''`,

		// AI Bot trade history (entry/exit, TP/SL, PnL).
		`CREATE TABLE IF NOT EXISTS ai_bot_trades (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			bot_instance_id TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			entry_price REAL DEFAULT 0,
			exit_price REAL DEFAULT 0,
			quantity REAL DEFAULT 0,
			pnl REAL DEFAULT 0,
			pnl_pct REAL DEFAULT 0,
			tp_price REAL DEFAULT 0,
			sl_price REAL DEFAULT 0,
			close_reason TEXT DEFAULT '',
			opened_at INTEGER NOT NULL,
			closed_at INTEGER DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_aibot_trades_bot ON ai_bot_trades(bot_instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_aibot_trades_time ON ai_bot_trades(closed_at)`,
	}
	for _, ddl := range tables {
		// SQLite ALTER may fail if column exists; migrations run once per DB so safe.
		_ = tx.exec(ddl)
	}
	return nil
}

// migrateV11 adds persistent storage for arbitrage trade pairs.
func migrateV11(tx *dbTx) error {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS arbitrage_trades (
			id TEXT PRIMARY KEY,
			symbol TEXT NOT NULL,
			buy_exchange TEXT NOT NULL,
			sell_exchange TEXT NOT NULL,
			buy_price REAL NOT NULL,
			sell_price REAL NOT NULL,
			quantity REAL NOT NULL,
			buy_order_id TEXT DEFAULT '',
			sell_order_id TEXT DEFAULT '',
			gross_profit REAL DEFAULT 0,
			net_profit REAL DEFAULT 0,
			fees REAL DEFAULT 0,
			status TEXT DEFAULT 'pending',
			opened_at INTEGER NOT NULL,
			closed_at INTEGER DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_arb_trades_symbol ON arbitrage_trades(symbol)`,
		`CREATE INDEX IF NOT EXISTS idx_arb_trades_status ON arbitrage_trades(status)`,
		`CREATE INDEX IF NOT EXISTS idx_arb_trades_opened ON arbitrage_trades(opened_at)`,
	}
	for _, ddl := range tables {
		if err := tx.exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

// migrateV12 adds persistent storage for notifications and notification routes.
func migrateV12(tx *dbTx) error {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			level TEXT NOT NULL,
			category TEXT NOT NULL,
			read INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_created_at ON notifications(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_read ON notifications(read)`,
		`CREATE TABLE IF NOT EXISTS notification_routes (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			events TEXT NOT NULL,
			levels TEXT NOT NULL,
			channels TEXT NOT NULL,
			enabled INTEGER DEFAULT 1,
			min_return_pct REAL DEFAULT 0
		)`,
	}
	for _, ddl := range tables {
		if err := tx.exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

// migrateV13 adds persistent storage for triangular arbitrage trades.
func migrateV13(tx *dbTx) error {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS triangular_trades (
			id TEXT PRIMARY KEY,
			exchange TEXT NOT NULL,
			cycle_json TEXT NOT NULL DEFAULT '[]',
			legs_json TEXT NOT NULL DEFAULT '[]',
			start_asset TEXT NOT NULL,
			start_qty REAL NOT NULL,
			end_qty REAL DEFAULT 0,
			gross_profit REAL DEFAULT 0,
			net_profit REAL DEFAULT 0,
			total_fees REAL DEFAULT 0,
			status TEXT DEFAULT 'pending',
			opened_at INTEGER NOT NULL,
			closed_at INTEGER DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_triangular_trades_exchange ON triangular_trades(exchange)`,
		`CREATE INDEX IF NOT EXISTS idx_triangular_trades_status ON triangular_trades(status)`,
		`CREATE INDEX IF NOT EXISTS idx_triangular_trades_opened ON triangular_trades(opened_at)`,
	}
	for _, ddl := range tables {
		if err := tx.exec(ddl); err != nil {
			return err
		}
	}
	return nil
}
