package store

import (
	"os"
	"testing"
)

// 测试数据库辅助（仅供 _test.go 调用）。
//
// 口径：主库为 PostgreSQL。测试优先读 TEST_DATABASE_URL，回退 DATABASE_URL；
// 两者均未配置时视为"无测试库"，测试应当跳过（t.Skip / TestMain 直接退出），
// 而不是回退到嵌入式 SQLite 或因缺库失败。

// TestDatabaseConfigured 判断测试 PostgreSQL 是否已配置连接串。
func TestDatabaseConfigured() bool {
	return os.Getenv("TEST_DATABASE_URL") != "" || os.Getenv("DATABASE_URL") != ""
}

// SetupTestDB 连接测试 PG、执行迁移并完成初始化；未配置连接串时跳过。
// 各包测试的 setup 函数调用本辅助即可，无需再设置 DB_PATH 临时文件。
// 重复调用时由调用方控制作用域（一般每个测试函数一次，配合 t.Cleanup 关闭）。
func SetupTestDB(t *testing.T) {
	t.Helper()
	if !TestDatabaseConfigured() {
		t.Skip("postgres test DB not configured: set TEST_DATABASE_URL (or DATABASE_URL)")
	}
	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		_ = os.Setenv("DATABASE_URL", dsn)
	}
	if os.Getenv("SECRET_KEY") == "" {
		_ = os.Setenv("SECRET_KEY", "test-secret-key-not-for-production-use-only")
	}
	if err := InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	resetTestTables()
	t.Cleanup(CloseDB)
}

// resetTestTables 清空 public schema 下业务表并重置自增序列，保留迁移台账
// （sql_migrations / schema_version），避免重复应用迁移撞唯一键。
// 每个用例拿到与旧 SQLite 临时文件等价的干净业务库，固定 id 种子不跨用例残留。
func resetTestTables() {
	if db == nil {
		return
	}
	_, _ = db.Exec(`DO $$
		DECLARE r record;
		BEGIN
			FOR r IN SELECT tablename FROM pg_tables
				WHERE schemaname = 'public'
				AND tablename NOT IN ('sql_migrations', 'schema_version')
			LOOP
				EXECUTE 'TRUNCATE TABLE ' || quote_ident(r.tablename) || ' RESTART IDENTITY CASCADE';
			END LOOP;
		END $$;`)
}
