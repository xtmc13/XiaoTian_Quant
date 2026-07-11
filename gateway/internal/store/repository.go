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
func buildFilter(filter map[string]any, allowedCols map[string]bool) ([]any, string) {
	if len(filter) == 0 {
		return nil, ""
	}
	var clauses []string
	var args []any
	for key, val := range filter {
		if !allowedCols[key] {
			continue
		}
		clauses = append(clauses, fmt.Sprintf("%s=?", key))
		args = append(args, val)
	}
	if len(clauses) == 0 {
		return nil, ""
	}
	return args, strings.Join(clauses, " AND ")
}
