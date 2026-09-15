package store

import (
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"
)

// 纯 SQL 迁移机制（2026-09-15 引入）：
// 历史上的 Go 版本迁移(schema.go, V1..V15)继续保留并先执行；
// 此后所有新 schema 变更只写 .sql 文件，放在 migrations/sql/ 下，
// 按文件名字典序执行，每个文件在独立事务中 exactly-once 应用，
//  applied 记录见 sql_migrations 表。
//
// 规则：
//   - 文件名 0002_xxx.sql、0003_xxx.sql 递增，禁止改已发布的文件；
//   - 单文件可含多条语句，按分号切分执行——语句内不要出现分号
//     （字符串字面量、注释里也不行）；
//   - 只做增量变更（CREATE/ALTER/CREATE INDEX），不要 DROP。

//go:embed migrations/sql/*.sql
var sqlMigrationsFS embed.FS

// RunSQLMigrations applies pending .sql migrations under migrations/sql.
func RunSQLMigrations() error {
	if db == nil {
		return nil
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS sql_migrations (
		name TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("sql migrations: create tracking table: %w", err)
	}

	entries, err := sqlMigrationsFS.ReadDir("migrations/sql")
	if err != nil {
		return fmt.Errorf("sql migrations: read dir: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var applied int
		if err := db.QueryRow("SELECT COUNT(*) FROM sql_migrations WHERE name = ?", name).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}

		data, err := sqlMigrationsFS.ReadFile("migrations/sql/" + name)
		if err != nil {
			return err
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}

		for _, stmt := range splitSQLStatements(string(data)) {
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("sql migration %s [%s]: %w", name, stmt, err)
			}
		}

		if _, err := tx.Exec("INSERT INTO sql_migrations (name, applied_at) VALUES (?, ?)",
			name, time.Now().UnixMilli()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

// splitSQLStatements splits a migration file into executable statements.
// Semicolons are not allowed inside statement strings (see package doc).
func splitSQLStatements(content string) []string {
	var stmts []string
	for _, chunk := range strings.Split(content, ";") {
		stmt := strings.TrimSpace(chunk)
		// 去掉整行注释，避免 "--" 注释污染语句
		var lines []string
		for _, line := range strings.Split(stmt, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "--") {
				continue
			}
			lines = append(lines, line)
		}
		stmt = strings.TrimSpace(strings.Join(lines, "\n"))
		if stmt != "" {
			stmts = append(stmts, stmt)
		}
	}
	return stmts
}
