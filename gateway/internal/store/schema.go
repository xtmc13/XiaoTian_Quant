package store

// Schema migration constants and DDL for all 18 tables.

const currentSchemaVersion = 5

// MigrationFunc is a function that upgrades the schema by one version.
type MigrationFunc func(tx *dbTx) error

var migrations = map[int]MigrationFunc{
	1: migrateV1,
	2: migrateV2,
	3: migrateV3,
	4: migrateV4,
	5: migrateV5,
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

		// ── Position Snapshots ──
		`CREATE TABLE IF NOT EXISTS position_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			position_id TEXT NOT NULL,
			symbol TEXT NOT NULL,
			quantity REAL NOT NULL,
			avg_entry_price REAL NOT NULL,
			current_price REAL,
			unrealized_pnl REAL,
			realized_pnl REAL,
			timestamp INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pos_snap_pos ON position_snapshots(position_id)`,

		// ── Accounts (enhanced) ──
		`CREATE TABLE IF NOT EXISTS accounts (
			id TEXT PRIMARY KEY,
			name TEXT DEFAULT '',
			exchange TEXT NOT NULL,
			api_key_hash TEXT DEFAULT '',
			initial_balance REAL DEFAULT 0,
			currency TEXT DEFAULT 'USDT',
			is_active INTEGER DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,

		// ── Account Snapshots ──
		`CREATE TABLE IF NOT EXISTS account_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id TEXT NOT NULL,
			total_equity REAL NOT NULL,
			available_balance REAL NOT NULL,
			margin_used REAL DEFAULT 0,
			drawdown REAL DEFAULT 0,
			net_exposure REAL DEFAULT 0,
			timestamp INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_acct_snap_acct ON account_snapshots(account_id)`,

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

		// ── Strategy Trade Logs ──
		`CREATE TABLE IF NOT EXISTS strategy_trade_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			strategy_name TEXT NOT NULL,
			symbol TEXT NOT NULL,
			signal_id INTEGER,
			order_id TEXT DEFAULT '',
			side TEXT NOT NULL,
			price REAL NOT NULL,
			quantity REAL NOT NULL,
			pnl REAL DEFAULT 0,
			reason TEXT DEFAULT '',
			timestamp INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_strat_trades_strat ON strategy_trade_logs(strategy_name)`,

		// ── Strategy Run Logs ──
		`CREATE TABLE IF NOT EXISTS strategy_run_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			strategy_name TEXT NOT NULL,
			event_type TEXT NOT NULL,
			status TEXT NOT NULL,
			message TEXT DEFAULT '',
			duration_ms INTEGER DEFAULT 0,
			timestamp INTEGER NOT NULL
		)`,

		// ── Strategy Global Settings ──
		`CREATE TABLE IF NOT EXISTS strategy_global_settings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			setting_key TEXT UNIQUE NOT NULL,
			setting_value TEXT NOT NULL,
			description TEXT DEFAULT '',
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

// migrateV5 adds community marketplace tables and indicator i18n fields.
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
