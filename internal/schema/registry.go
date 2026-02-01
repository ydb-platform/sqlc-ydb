package schema

// ColumnInfo describes a table/view column for codegen.
type ColumnInfo struct {
	Name      string
	DataType  string
	Nullable  bool
	IsArray   bool
	ArrayDims int32
}

// Registry returns column metadata by table or view name.
// Implementations: mock (tests), ddl (parse schema SQL), runtime (live YDB).
type Registry interface {
	Columns(tableOrView string) ([]ColumnInfo, bool)
}

// emptyRegistry implements Registry with no tables.
type emptyRegistry struct{}

func (e *emptyRegistry) Columns(tableOrView string) ([]ColumnInfo, bool) {
	return nil, false
}

// Empty returns a Registry that has no tables (no schema_sql, no connection).
func Empty() Registry {
	return &emptyRegistry{}
}
