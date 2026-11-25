package store

import (
	"fmt"
	"strings"
)

// ── Repository generic interface ──

// Repository provides typed CRUD for a domain entity.
type Repository[T any] interface {
	Create(item *T) error
	GetByID(id string) (*T, error)
	Update(item *T) error
	Delete(id string) error
	List(filter map[string]any, limit int) ([]*T, error)
}

// ── Helpers ──

// buildFilter builds a parameterized WHERE clause from a filter map.
// Only keys present in allowedCols are used; unknown keys are ignored to prevent SQL injection.
//
// PG 占位符从 start 编号（通常为 1）起按过滤项顺序分配 `$n`；返回绑定参数、
// WHERE 子句以及下一个可用占位符编号（供 LIMIT 等后续占位符续号）。
func buildFilter(filter map[string]any, allowedCols map[string]bool, start int) ([]any, string, int) {
	if len(filter) == 0 {
		return nil, "", start
	}
	var clauses []string
	var args []any
	n := start
	for key, val := range filter {
		if !allowedCols[key] {
			continue
		}
		clauses = append(clauses, fmt.Sprintf("%s=$%d", key, n))
		args = append(args, val)
		n++
	}
	if len(clauses) == 0 {
		return nil, "", start
	}
	return args, strings.Join(clauses, " AND "), n
}
