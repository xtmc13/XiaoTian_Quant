package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// ── PostgreSQL 连接层 ──────────────────────────────────────────
//
// 主库为 PostgreSQL（对标 QuantDinger）。连接通过 DATABASE_URL 或
// POSTGRES_* 拆分环境变量配置；不再使用嵌入式 SQLite 文件库。
//
// 环境变量：
//   DATABASE_URL            完整 PG DSN，如 postgresql://user:pass@host:5432/db?sslmode=disable
//   POSTGRES_HOST / PORT / USER / PASSWORD / DB_NAME
//   DB_POOL_MIN / DB_POOL_MAX / DB_CONN_MAX_LIFETIME (分钟)

// resolveDSN 解析 PostgreSQL 连接串。优先 DATABASE_URL；否则用 POSTGRES_* 拼装。
func resolveDSN() (string, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn, nil
	}

	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		return "", fmt.Errorf("database not configured: set DATABASE_URL or POSTGRES_HOST (PostgreSQL)")
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("POSTGRES_USER")
	if user == "" {
		user = "xtquant"
	}
	password := os.Getenv("POSTGRES_PASSWORD")
	dbName := os.Getenv("POSTGRES_DB")
	if dbName == "" {
		dbName = os.Getenv("POSTGRES_DB_NAME")
	}
	if dbName == "" {
		dbName = "xtquant"
	}

	sslmode := os.Getenv("POSTGRES_SSLMODE")
	if sslmode == "" {
		sslmode = "disable"
	}

	u := &url.URL{
		Scheme: "postgresql",
		Host:   host + ":" + port,
		Path:   dbName,
		User:   url.UserPassword(user, password),
	}
	q := u.Query()
	q.Set("sslmode", sslmode)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// openPostgres 打开 PostgreSQL 连接并配置连接池，随后 ping 验证连通性。
func openPostgres() (*sql.DB, error) {
	dsn, err := resolveDSN()
	if err != nil {
		return nil, err
	}

	// pgx stdlib 注册驱动名 "pgx"，复用 database/sql 接口。
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	min := envInt("DB_POOL_MIN", 2)
	max := envInt("DB_POOL_MAX", 20)
	if max < 1 {
		max = 20
	}
	if min > max {
		min = max
	}
	lifetime := time.Duration(envInt("DB_CONN_MAX_LIFETIME_MIN", 60)) * time.Minute

	db.SetMaxOpenConns(max)
	db.SetMaxIdleConns(min)
	db.SetConnMaxLifetime(lifetime)
	db.SetConnMaxIdleTime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres ping failed: %w", err)
	}
	return db, nil
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// RuntimeDir 返回运行时文件持久化目录（策略配置/日志/模板/凭证沙箱等
// JSON 文件落盘位置，对应容器 /app/data 卷）。优先 DATA_DIR；兼容旧部署
// 从 DB_PATH 目录推导；兜底 ./runtime。
func RuntimeDir() string {
	if d := os.Getenv("DATA_DIR"); d != "" {
		return d
	}
	if p := os.Getenv("DB_PATH"); p != "" {
		if dir := filepath.Dir(p); dir != "" && dir != "." {
			return dir
		}
	}
	return "./runtime"
}

// EnsureRuntimeDir 确保运行时目录存在。
func EnsureRuntimeDir() (string, error) {
	dir := RuntimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create runtime dir %s: %w", dir, err)
	}
	return dir, nil
}
