package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

var (
	db          *sql.DB
	mu          sync.RWMutex
	configPath  string
	configCache map[string]any
	configMu    sync.RWMutex

	strategyConfigsPath string
	strategyConfigs     = make(map[string]map[string]any)
	strategyMu          sync.RWMutex

	logsStore   []map[string]any
	templates   []map[string]any
	agentTokens []map[string]any
	inmemMu     sync.RWMutex

	jwtSecret string
)

func InitDB() error {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data/gateway.db"
	}
	// Ensure data directory exists
	dataDir := dbPath[:len(dbPath)-len("/gateway.db")]
	if dataDir == dbPath {
		dataDir = "./data"
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	var err error
	db, err = sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return err
	}
	// WAL mode allows concurrent reads; set reasonable pool limits.
	// Writes are still serialized by SQLite, but reads can overlap.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS xt_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			nickname TEXT DEFAULT '',
			email TEXT DEFAULT '',
			role TEXT DEFAULT 'user',
			token_version INTEGER DEFAULT 1,
			is_active INTEGER DEFAULT 1,
			created_at TEXT DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS xt_orders (
			id TEXT PRIMARY KEY,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			order_type TEXT NOT NULL,
			price REAL,
			stop_price REAL DEFAULT 0,
			quantity REAL,
			filled REAL DEFAULT 0,
			status TEXT DEFAULT 'NEW',
			exchange TEXT DEFAULT 'BINANCE',
			user_id INTEGER DEFAULT 0,
			client_oid TEXT DEFAULT '',
			avg_fill_price REAL DEFAULT 0,
			created_at REAL,
			updated_at REAL,
			market_type TEXT DEFAULT 'spot',
			position_side TEXT DEFAULT '',
			leverage REAL DEFAULT 0,
			margin_mode TEXT DEFAULT 'cross',
			tp_price REAL DEFAULT 0,
			sl_price REAL DEFAULT 0,
			close_position INTEGER DEFAULT 0
		);
	`)
	if err != nil {
		return err
	}

	// Ensure default admin user with a random password (printed to logs on first boot)
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM xt_users WHERE username='admin'").Scan(&count); err != nil {
		return fmt.Errorf("failed to check admin user: %w", err)
	}
	if count == 0 {
		adminPass := randomHex(8)
		hash := HashPassword(adminPass)
		if _, err := db.Exec("INSERT INTO xt_users (username, password_hash, nickname, role) VALUES (?, ?, ?, ?)",
			"admin", hash, "Admin", "admin"); err != nil {
			return fmt.Errorf("failed to create admin user: %w", err)
		}
		fmt.Fprintf(os.Stderr, "\n╔══════════════════════════════════════════════════════════╗\n")
		fmt.Fprintf(os.Stderr, "║  Default admin created:  username=admin                  ║\n")
		fmt.Fprintf(os.Stderr, "║  Temporary password:     %s               ║\n", adminPass)
		fmt.Fprintf(os.Stderr, "║  CHANGE THIS PASSWORD IMMEDIATELY after first login.     ║\n")
		fmt.Fprintf(os.Stderr, "╚══════════════════════════════════════════════════════════╝\n\n")
	}

	// ── JWT Secret ──
	// Priority: 1) SECRET_KEY env  2) /app/data/.jwt_secret file  3) generate & persist (dev only)
	jwtSecret = os.Getenv("SECRET_KEY")
	if jwtSecret == "" {
		secretFile := "./data/.jwt_secret"
		if data, err := os.ReadFile(secretFile); err == nil && len(data) > 0 {
			jwtSecret = string(data)
		} else {
			// Production safety: refuse to start without an explicit secret.
			if os.Getenv("APP_ENV") == "production" || os.Getenv("GIN_MODE") == "release" {
				return fmt.Errorf("SECRET_KEY environment variable is required in production; set it or mount a persistent %s", secretFile)
			}
			jwtSecret = randomHex(32)
			if err := os.WriteFile(secretFile, []byte(jwtSecret), 0600); err != nil {
				fmt.Fprintf(os.Stderr, "\n⚠  WARNING: Failed to persist JWT secret to %s: %v\n", secretFile, err)
			}
			fmt.Fprintf(os.Stderr, "\n⚠  WARNING: SECRET_KEY not set in environment.\n")
			fmt.Fprintf(os.Stderr, "   A random key was generated and persisted to %s.\n", secretFile)
			fmt.Fprintf(os.Stderr, "   Set SECRET_KEY in .env to override.\n\n")
		}
	}

	if configPath == "" {
		configPath = "./data/config.yaml"
	}
	if strategyConfigsPath == "" {
		strategyConfigsPath = "./data/strategy_configs.json"
	}

	// Run schema migrations
	if err := RunMigrations(); err != nil {
		return fmt.Errorf("migration: %w", err)
	}

	// Seed AI bot catalog data
	SeedAIBotCatalog()
	SeedAIBotSignalProviders()

	// Migrate xt_orders with extra columns (for pre-existing DBs)
	if err := migrateXTOrders(); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: xt_orders migration failed: %v\n", err)
	}

	return nil
}

func CloseDB() {
	if db != nil {
		db.Close()
	}
}

// GetDB returns the global database connection.
func GetDB() *sql.DB {
	return db
}

// ── Config ──

func LoadConfig() {
	configMu.Lock()
	defer configMu.Unlock()
	data, err := os.ReadFile(configPath)
	if err != nil {
		configCache = make(map[string]any)
		return
	}
	if err := yaml.Unmarshal(data, &configCache); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Failed to parse config.yaml: %v\n", err)
		configCache = make(map[string]any)
	}
}

// SaveUIConfig saves UI preferences into the config cache.
func SaveUIConfig(ui map[string]any) {
	configMu.Lock()
	defer configMu.Unlock()
	if configCache == nil {
		configCache = make(map[string]any)
	}
	configCache["ui"] = ui
}

// SaveExchangeConfig saves an exchange configuration.
func SaveExchangeConfig(id string, cfg map[string]any) {
	configMu.Lock()
	defer configMu.Unlock()
	if configCache == nil {
		configCache = make(map[string]any)
	}
	exchanges, _ := configCache["exchanges"].(map[string]any)
	if exchanges == nil {
		exchanges = make(map[string]any)
	}
	exchanges[id] = cfg
	configCache["exchanges"] = exchanges
}

func GetConfig() map[string]any {
	configMu.RLock()
	defer configMu.RUnlock()
	cp := make(map[string]any)
	for k, v := range configCache {
		cp[k] = v
	}
	return cp
}

func SaveConfig(cfg map[string]any) error {
	configMu.Lock()
	configCache = cfg
	data, err := yaml.Marshal(cfg)
	configMu.Unlock()
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

// ── Strategy Configs ──

func LoadStrategyConfigs() {
	strategyMu.Lock()
	defer strategyMu.Unlock()
	data, err := os.ReadFile(strategyConfigsPath)
	if err != nil {
		return
	}
	var items []map[string]any
	json.Unmarshal(data, &items)
	for _, item := range items {
		if id, ok := item["id"].(string); ok {
			strategyConfigs[id] = item
		}
	}
}

func saveStrategyConfigs() {
	items := make([]map[string]any, 0, len(strategyConfigs))
	for _, v := range strategyConfigs {
		items = append(items, v)
	}
	data, _ := json.MarshalIndent(items, "", "  ")
	os.WriteFile(strategyConfigsPath, data, 0644)
}

func GetStrategyConfigs() map[string]map[string]any {
	strategyMu.RLock()
	defer strategyMu.RUnlock()
	cp := make(map[string]map[string]any)
	for k, v := range strategyConfigs {
		cp[k] = v
	}
	return cp
}

func GetStrategyConfigMu() *sync.RWMutex {
	return &strategyMu
}

func PersistStrategyConfigs() {
	saveStrategyConfigs()
}

// ── In-Memory Stores ──

func GetLogsStore() *[]map[string]any        { return &logsStore }
func GetTemplatesStore() *[]map[string]any   { return &templates }
func GetAgentTokensStore() *[]map[string]any { return &agentTokens }

// ── Auth ──

// HashPassword hashes a password with bcrypt (cost = bcrypt.DefaultCost).
func HashPassword(password string) string {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return ""
	}
	return string(hash)
}

// VerifyPassword verifies a password against a bcrypt hash.
// Supports legacy sha256$ format for backward compatibility during migration.
func VerifyPassword(password, hash string) bool {
	// Modern bcrypt
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err == nil {
		return true
	}
	// Legacy sha256$ format — will be upgraded on next password change
	parts := splitN(hash, "$", 3)
	if len(parts) != 3 || parts[0] != "sha256" {
		return false
	}
	return parts[2] == sha256Hex(password+parts[1])
}

func GenerateJWT(userID int, username, role string, tokenVersion int) (string, error) {
	claims := jwt.MapClaims{
		"sub":           username,
		"user_id":       userID,
		"role":          role,
		"token_version": tokenVersion,
		"iat":           time.Now().Unix(),
		"exp":           time.Now().Add(24 * time.Hour).Unix(),
		"jti":           randomHex(8),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(jwtSecret))
}

func VerifyJWT(tokenStr string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(jwtSecret), nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, fmt.Errorf("invalid token")
}

func FindUserByUsername(username string) map[string]any {
	row := db.QueryRow("SELECT id, username, password_hash, nickname, email, role, token_version, email_verified, is_active, created_at FROM xt_users WHERE username=? AND is_active=1", username)
	var id, tokenVer, emailVerified, isActive int
	var uname, pwHash, nickname, email, role, createdAt string
	if err := row.Scan(&id, &uname, &pwHash, &nickname, &email, &role, &tokenVer, &emailVerified, &isActive, &createdAt); err != nil {
		return nil
	}
	return map[string]any{
		"id": id, "username": uname, "password_hash": pwHash,
		"nickname": nickname, "email": email, "role": role, "token_version": tokenVer,
		"email_verified": emailVerified, "is_active": isActive, "created_at": createdAt,
	}
}

func CreateUser(username, password, nickname, email, role string) (int, error) {
	pwHash := HashPassword(password)
	res, err := db.Exec(
		"INSERT INTO xt_users (username, password_hash, nickname, email, role) VALUES (?, ?, ?, ?, ?)",
		username, pwHash, nickname, email, role,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

func ListAllUsers() []map[string]any {
	rows, err := db.Query("SELECT id, username, nickname, email, role, is_active, created_at FROM xt_users")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var users []map[string]any
	for rows.Next() {
		var id, isActive int
		var username, nickname, email, role, createdAt string
		rows.Scan(&id, &username, &nickname, &email, &role, &isActive, &createdAt)
		users = append(users, map[string]any{
			"id": id, "username": username, "nickname": nickname,
			"email": email, "role": role, "is_active": isActive, "created_at": createdAt,
		})
	}
	return users
}

// ── Verification Codes ──

// SaveVerificationCode stores a new verification code, invalidating any unused
// codes of the same type for the same email first.
func SaveVerificationCode(email, code, codeType, ip string, ttlSeconds int) error {
	now := time.Now().Unix()
	// Invalidate all unused codes of same type for this email
	db.Exec(`UPDATE xt_verification_codes SET used_at=? WHERE email=? AND code_type=? AND used_at=0`,
		now, email, codeType)
	// Insert new code
	expiresAt := now + int64(ttlSeconds)
	_, err := db.Exec(
		`INSERT INTO xt_verification_codes (email, code, code_type, expires_at, ip_address, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		email, code, codeType, expiresAt, ip, now,
	)
	return err
}

// VerifyCode checks if a verification code is valid. Returns (valid bool, message string).
func VerifyCode(email, code, codeType string) (bool, string) {
	var id int
	var storedCode string
	var expiresAt, usedAt int64
	var attempts int

	row := db.QueryRow(
		`SELECT id, code, expires_at, used_at, attempts FROM xt_verification_codes WHERE email=? AND code_type=? AND used_at=0 ORDER BY created_at DESC LIMIT 1`,
		email, codeType,
	)
	if err := row.Scan(&id, &storedCode, &expiresAt, &usedAt, &attempts); err != nil {
		return false, "no verification code found, please send code first"
	}

	// Check expiry
	if time.Now().Unix() > expiresAt {
		db.Exec(`UPDATE xt_verification_codes SET used_at=? WHERE id=?`, time.Now().Unix(), id)
		return false, "verification code has expired"
	}

	// Check attempts limit (max 5)
	if attempts >= 5 {
		db.Exec(`UPDATE xt_verification_codes SET used_at=? WHERE id=?`, time.Now().Unix(), id)
		return false, "too many attempts, please send a new code"
	}

	// Increment attempts
	db.Exec(`UPDATE xt_verification_codes SET attempts=attempts+1 WHERE id=?`, id)

	// Check code match
	if storedCode != code {
		return false, "incorrect verification code"
	}

	// Mark as used
	db.Exec(`UPDATE xt_verification_codes SET used_at=? WHERE id=?`, time.Now().Unix(), id)
	return true, "ok"
}

// CanSendCode checks if a new code can be sent (rate limit: 60s between sends).
func CanSendCode(email, codeType string) (bool, int) {
	var created_at int64
	row := db.QueryRow(
		`SELECT created_at FROM xt_verification_codes WHERE email=? AND code_type=? ORDER BY created_at DESC LIMIT 1`,
		email, codeType,
	)
	if err := row.Scan(&created_at); err != nil {
		return true, 0 // No prior code, allow
	}
	elapsed := int(time.Now().Unix() - created_at)
	if elapsed < 60 {
		return false, 60 - elapsed
	}
	return true, 0
}

// SetEmailVerified marks a user's email as verified.
func SetEmailVerified(userID int) error {
	_, err := db.Exec(`UPDATE xt_users SET email_verified=1 WHERE id=?`, userID)
	return err
}

// FindUserByEmail finds a user by email address.
func FindUserByEmail(email string) map[string]any {
	row := db.QueryRow("SELECT id, username, password_hash, nickname, role, token_version, email_verified FROM xt_users WHERE email=? AND is_active=1", email)
	var id, tokenVer, emailVerified int
	var uname, pwHash, nickname, role string
	if err := row.Scan(&id, &uname, &pwHash, &nickname, &role, &tokenVer, &emailVerified); err != nil {
		return nil
	}
	return map[string]any{
		"id": id, "username": uname, "password_hash": pwHash,
		"nickname": nickname, "role": role, "token_version": tokenVer,
		"email_verified": emailVerified,
	}
}

// UpdateUserPassword updates a user's password hash.
func UpdateUserPassword(userID int, passwordHash string) error {
	_, err := db.Exec(`UPDATE xt_users SET password_hash=?, token_version=token_version+1 WHERE id=?`, passwordHash, userID)
	return err
}

// UpdateUserProfile updates a user profile field by username.
// Uses a strict switch statement — no SQL string concatenation.
func UpdateUserProfile(username, field, value string) error {
	var err error
	switch field {
	case "nickname":
		_, err = db.Exec(`UPDATE xt_users SET nickname=? WHERE username=?`, value, username)
	case "email":
		_, err = db.Exec(`UPDATE xt_users SET email=? WHERE username=?`, value, username)
	case "email_verified":
		_, err = db.Exec(`UPDATE xt_users SET email_verified=? WHERE username=?`, value, username)
	case "role":
		_, err = db.Exec(`UPDATE xt_users SET role=? WHERE username=?`, value, username)
	case "is_active":
		_, err = db.Exec(`UPDATE xt_users SET is_active=? WHERE username=?`, value, username)
	default:
		return fmt.Errorf("invalid profile field: %s", field)
	}
	return err
}

// AdminUpdateUser updates any user field by user ID.
// Uses a strict switch statement — no SQL string concatenation.
func AdminUpdateUser(userID int, updates map[string]any) error {
	for field, value := range updates {
		var err error
		switch field {
		case "nickname":
			if strVal, ok := value.(string); ok && strVal != "" {
				_, err = db.Exec(`UPDATE xt_users SET nickname=? WHERE id=?`, strVal, userID)
			}
		case "email":
			if strVal, ok := value.(string); ok && strVal != "" {
				_, err = db.Exec(`UPDATE xt_users SET email=? WHERE id=?`, strVal, userID)
			}
		case "role":
			if strVal, ok := value.(string); ok && strVal != "" {
				_, err = db.Exec(`UPDATE xt_users SET role=? WHERE id=?`, strVal, userID)
			}
		case "is_active":
			if intVal, ok := value.(float64); ok {
				_, err = db.Exec(`UPDATE xt_users SET is_active=? WHERE id=?`, int(intVal), userID)
			}
		default:
			err = fmt.Errorf("invalid admin update field: %s", field)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ── Orders ──

var (
	orderRepo    *OrderRepo
	orderRepoOnce sync.Once
	ordersMu      sync.RWMutex
	// In-memory order store for fast lookups (synced with DB via OrderRepo)
	orders      = make(map[string]*OrderRecord)
	orderCount  int
)

// getOrderRepo lazily initializes the OrderRepo.
func getOrderRepo() *OrderRepo {
	orderRepoOnce.Do(func() {
		orderRepo = NewOrderRepo()
	})
	return orderRepo
}

// GetOrderRepo returns the global OrderRepo (exported for cross-package access).
func GetOrderRepo() *OrderRepo {
	return getOrderRepo()
}

// migrateXTOrders adds missing columns to xt_orders for existing databases.
func migrateXTOrders() error {
	// Try to add columns one by one (SQLite ALTER TABLE ignores errors for existing columns)
	columns := []string{
		"stop_price REAL DEFAULT 0",
		"user_id INTEGER DEFAULT 0",
		"client_oid TEXT DEFAULT ''",
		"avg_fill_price REAL DEFAULT 0",
		"updated_at REAL",
		"market_type TEXT DEFAULT 'spot'",
		"position_side TEXT DEFAULT ''",
		"leverage REAL DEFAULT 0",
		"margin_mode TEXT DEFAULT 'cross'",
		"tp_price REAL DEFAULT 0",
		"sl_price REAL DEFAULT 0",
		"close_position INTEGER DEFAULT 0",
	}
	for _, col := range columns {
		// Split on first space to get column name
		parts := strings.SplitN(col, " ", 2)
		if len(parts) == 2 {
			db.Exec(fmt.Sprintf("ALTER TABLE xt_orders ADD COLUMN %s %s", parts[0], parts[1]))
		}
	}
	return nil
}

func GetOrders(symbol string) []map[string]any {
	ordersMu.RLock()
	defer ordersMu.RUnlock()
	var result []map[string]any
	for _, o := range orders {
		if symbol == "" || o.Symbol == symbol {
			result = append(result, orderToMap(o))
		}
	}
	if result == nil {
		result = []map[string]any{}
	}
	return result
}

func PlaceOrder(order map[string]any) string {
	ordersMu.Lock()
	defer ordersMu.Unlock()
	orderCount++

	// Build OrderRecord from the map
	rec := &OrderRecord{
		Symbol:       getString(order, "symbol", ""),
		Side:         getString(order, "side", ""),
		OrderType:    getString(order, "order_type", "LIMIT"),
		Price:        getFloat(order, "price", 0),
		StopPrice:    getFloat(order, "stop_price", 0),
		Quantity:     getFloat(order, "quantity", 0),
		Filled:       getFloat(order, "filled", 0),
		Status:       getString(order, "status", "NEW"),
		Exchange:     getString(order, "exchange", "BINANCE"),
		UserID:       uint64(getFloat(order, "user_id", 0)),
		ClientOID:    getString(order, "client_oid", ""),
		AvgFillPrice: getFloat(order, "avg_fill_price", 0),
		CreatedAt:    int64(getFloat(order, "created_at", 0)),
		MarketType:   getString(order, "market_type", "spot"),
		PositionSide: getString(order, "position_side", ""),
		Leverage:     getFloat(order, "leverage", 0),
		MarginMode:   getString(order, "margin_mode", "cross"),
		TPPrice:      getFloat(order, "tp_price", 0),
		SLPrice:      getFloat(order, "sl_price", 0),
		ClosePosition: getBool(order, "close_position", false),
	}
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().UnixMilli()
	}
	rec.UpdatedAt = rec.CreatedAt
	if rec.ID == "" {
		rec.ID = fmt.Sprintf("ord-%d-%d", rec.CreatedAt, orderCount)
	}

	// Persist to DB immediately
	if err := getOrderRepo().Create(rec); err != nil {
		fmt.Fprintf(os.Stderr, "[Order] DB write error: %v (order kept in memory)\n", err)
	}

	// Keep in-memory copy too
	orders[rec.ID] = rec
	return rec.ID
}

func GetOrderByID(id string) map[string]any {
	ordersMu.RLock()
	defer ordersMu.RUnlock()
	if o, ok := orders[id]; ok {
		return orderToMap(o)
	}
	return nil
}

func CancelOrder(id string) error {
	ordersMu.Lock()
	defer ordersMu.Unlock()
	o, ok := orders[id]
	if !ok {
		// Try DB as fallback
		if db != nil {
			row := db.QueryRow("SELECT status FROM xt_orders WHERE id=?", id)
			var status string
			if err := row.Scan(&status); err == nil {
				if status == "CANCELLED" || status == "FILLED" {
					return fmt.Errorf("already %s", status)
				}
				_, err := db.Exec("UPDATE xt_orders SET status='CANCELLED', updated_at=? WHERE id=?", time.Now().UnixMilli(), id)
				return err
			}
		}
		return fmt.Errorf("not found")
	}
	status := o.Status
	if status == "CANCELLED" || status == "FILLED" {
		return fmt.Errorf("already %s", status)
	}
	o.Status = "CANCELLED"
	o.UpdatedAt = time.Now().UnixMilli()
	orders[id] = o
	// Persist cancellation to DB
	return getOrderRepo().Update(o)
}

// PersistOrder persists an in-memory order to the database (for status updates).
func PersistOrder(orderID string, status string, filled float64, avgFillPrice float64) error {
	ordersMu.Lock()
	o, ok := orders[orderID]
	if !ok {
		ordersMu.Unlock()
		return fmt.Errorf("order %s not found in memory", orderID)
	}
	o.Status = status
	o.Filled = filled
	o.AvgFillPrice = avgFillPrice
	o.UpdatedAt = time.Now().UnixMilli()
	ordersMu.Unlock()
	return getOrderRepo().Update(o)
}

// GetOrderCount returns the number of orders persisted in the database.
func GetOrderCount() (int, error) {
	if db == nil {
		return 0, nil
	}
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM xt_orders").Scan(&count)
	return count, err
}

// GetDBOrders returns orders from the database (for recovery after restart).
func GetDBOrders(symbol string, limit int) ([]*OrderRecord, error) {
	return getOrderRepo().List(map[string]any{"symbol": symbol}, limit)
}

// ── Helpers ──

// ── Helpers ──

func getString(m map[string]any, key, def string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

func getFloat(m map[string]any, key string, def float64) float64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case int64:
			return float64(val)
		case string:
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				return f
			}
		}
	}
	return def
}

func getBool(m map[string]any, key string, def bool) bool {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case bool:
			return val
		case float64:
			return val != 0
		case int:
			return val != 0
		case string:
			return val == "true" || val == "1"
		}
	}
	return def
}

func orderToMap(o *OrderRecord) map[string]any {
	return map[string]any{
		"order_id":       o.ID,
		"id":             o.ID,
		"symbol":         o.Symbol,
		"side":           o.Side,
		"order_type":     o.OrderType,
		"price":          o.Price,
		"stop_price":     o.StopPrice,
		"quantity":       o.Quantity,
		"filled":         o.Filled,
		"status":         o.Status,
		"exchange":       o.Exchange,
		"user_id":        o.UserID,
		"client_oid":     o.ClientOID,
		"avg_fill_price": o.AvgFillPrice,
		"created_at":     o.CreatedAt,
		"updated_at":     o.UpdatedAt,
		"market_type":    o.MarketType,
		"position_side":  o.PositionSide,
		"leverage":       o.Leverage,
		"margin_mode":    o.MarginMode,
		"tp_price":       o.TPPrice,
		"sl_price":       o.SLPrice,
		"close_position": o.ClosePosition,
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func splitN(s, sep string, n int) []string {
	parts := make([]string, 0, n)
	for i := 0; i < n-1; i++ {
		idx := indexOf(s, sep)
		if idx < 0 {
			break
		}
		parts = append(parts, s[:idx])
		s = s[idx+len(sep):]
	}
	parts = append(parts, s)
	return parts
}

func indexOf(s, sub string) int {
	for i := 0; i < len(s); i++ {
		if i+len(sub) <= len(s) && s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ── Math helpers ──

func RoundFloat(v float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(v*p) / p
}

func SanitizeFloat(v float64) any {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return nil
	}
	return v
}

// ── JSON helpers ──

func MustMarshal(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}

func MustMarshalIndent(v any) []byte {
	data, _ := json.MarshalIndent(v, "", "  ")
	return data
}

func ParseJSON(data []byte) (map[string]any, error) {
	var result map[string]any
	err := json.Unmarshal(data, &result)
	return result, err
}

// ── Agent Audit Log ──

func GetAgentAuditLog(limit int) []map[string]any {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`SELECT id, token_id, name, endpoint, method, params_summary, status_code, ip, user_agent, timestamp FROM agent_audit_log ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var logs []map[string]any
	for rows.Next() {
		var id, tokenID, statusCode, timestamp int
		var name, endpoint, method, paramsSummary, ip, userAgent string
		rows.Scan(&id, &tokenID, &name, &endpoint, &method, &paramsSummary, &statusCode, &ip, &userAgent, &timestamp)
		logs = append(logs, map[string]any{
			"id": id, "token_id": tokenID, "name": name,
			"endpoint": endpoint, "method": method, "params_summary": paramsSummary,
			"status_code": statusCode, "ip": ip, "user_agent": userAgent, "timestamp": timestamp,
		})
	}
	return logs
}

// Global symbols list (same as Python _SYMBOL_LIST)
var SymbolList = []string{
	"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "XRPUSDT", "ADAUSDT", "DOGEUSDT", "AVAXUSDT",
	"DOTUSDT", "LINKUSDT", "MATICUSDT", "UNIUSDT", "SHIBUSDT", "LTCUSDT", "ATOMUSDT", "ETCUSDT",
	"FILUSDT", "APTUSDT", "ARBUSDT", "OPUSDT", "NEARUSDT", "VETUSDT", "GRTUSDT", "ALGOUSDT",
	"ICPUSDT", "SANDUSDT", "AAVEUSDT", "FTMUSDT", "EGLDUSDT", "THETAUSDT", "AXSUSDT", "KSMUSDT",
	"XTZUSDT", "EOSUSDT", "ZECUSDT", "DASHUSDT", "COMPUSDT", "MKRUSDT", "SNXUSDT", "CRVUSDT",
	"1INCHUSDT", "ENJUSDT", "CHZUSDT", "MANAUSDT", "GALAUSDT", "APEUSDT", "FLOWUSDT", "MINAUSDT",
	"ROSEUSDT", "RUNEUSDT", "KAVAUSDT", "WAVESUSDT", "OCEANUSDT", "FETUSDT", "AGIXUSDT", "LDOUSDT",
	"GMXUSDT", "DYDXUSDT", "FXSUSDT", "SSVUSDT", "BLURUSDT", "SUIUSDT", "PEPEUSDT", "WLDUSDT",
	"SEIUSDT", "TIAUSDT", "ORDIUSDT", "1000SATSUSDT", "BONKUSDT", "JTOUSDT", "ENAUSDT", "STRKUSDT",
	"ETHBTC", "BNBBTC", "SOLBTC", "XRPBTC", "ADABTC",
}

// Order book matching service reference (will be set from service package)
var MatchingService any
