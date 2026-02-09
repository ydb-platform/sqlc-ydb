package mock

import (
	"strings"

	"github.com/sqlc-dev/sqlc-engine-ydb/internal/schema"
)

// Registry is a schema.Registry for tests: tables and columns are set in advance.
type Registry struct {
	tables map[string][]schema.ColumnInfo
}

// New returns a Registry with the given table→columns. Table names are lowercased.
func New(tables map[string][]schema.ColumnInfo) *Registry {
	m := make(map[string][]schema.ColumnInfo)
	for t, cols := range tables {
		m[strings.ToLower(t)] = cols
	}
	return &Registry{tables: m}
}

func (r *Registry) Columns(tableOrView string) ([]schema.ColumnInfo, bool) {
	cols, ok := r.tables[strings.ToLower(tableOrView)]
	return cols, ok
}

func (r *Registry) TableNames() []string {
	names := make([]string, 0, len(r.tables))
	for t := range r.tables {
		names = append(names, t)
	}
	return names
}
