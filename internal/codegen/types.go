package codegen

import (
	"strings"

	"github.com/sqlc-dev/sqlc-engine-ydb/internal/codegen/pb"
)

func ydbTypeName(c *pb.Column) string {
	if c == nil || c.Type == nil {
		return ""
	}
	return strings.ToLower(c.Type.Name)
}

// goTypeForDB returns the Go type for database/sql (nullable => *T, notNull => T).
func goTypeForDB(yt string, notNull bool) string {
	t := goTypeFromYDB(yt, notNull)
	return t
}

// goTypeFromYDB returns the Go type for a YDB column. Optional types become *T.
func goTypeFromYDB(yt string, notNull bool) string {
	var t string
	switch yt {
	case "uint64", "uint32", "uint16", "uint8":
		t = yt
	case "int64", "int32", "int16", "int8":
		t = yt
	case "utf8", "string", "text":
		t = "string"
	case "bool", "boolean":
		t = "bool"
	case "float", "double":
		t = "float64"
	case "timestamp", "datetime", "date", "interval":
		t = "time.Time"
	case "uuid", "yson", "json":
		t = "string"
	default:
		t = "interface{}"
	}
	if !notNull {
		return "*" + t
	}
	return t
}

// pythonType returns Python type hint (Optional[T] for nullable).
func pythonType(yt string, notNull bool) string {
	var t string
	switch yt {
	case "uint64", "uint32", "uint16", "uint8", "int64", "int32", "int16", "int8":
		t = "int"
	case "utf8", "string", "text":
		t = "str"
	case "bool", "boolean":
		t = "bool"
	case "float", "double":
		t = "float"
	case "timestamp", "datetime", "date", "interval":
		t = "datetime"
	case "uuid", "yson", "json":
		t = "str"
	default:
		t = "Any"
	}
	if !notNull {
		return "Optional[" + t + "]"
	}
	return t
}

// toSnake converts CamelCase to snake_case (used by Python and const names).
func toSnake(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

func toGoStruct(s string) string {
	if s == "" {
		return "T"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// singularize converts a plural table name to singular for use as row/struct type (e.g. authors → author).
func singularize(tableName string) string {
	if tableName == "" {
		return ""
	}
	lower := strings.ToLower(tableName)
	if len(lower) <= 1 {
		return tableName
	}
	if strings.HasSuffix(lower, "ies") && len(lower) > 3 {
		return lower[:len(lower)-3] + "y"
	}
	if strings.HasSuffix(lower, "es") && len(lower) > 2 {
		return lower[:len(lower)-2]
	}
	if strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss") {
		return lower[:len(lower)-1]
	}
	return tableName
}

// tableNameToGoStruct returns the Go struct name for a table: singular, PascalCase (e.g. authors → Author).
func tableNameToGoStruct(tableName string) string {
	return toGoStruct(singularize(tableName))
}

func toGoField(s string) string {
	if s == "" {
		return "F"
	}
	switch strings.ToLower(s) {
	case "id":
		return "ID"
	default:
		return strings.ToUpper(s[:1]) + s[1:]
	}
}

// toGoParamName returns the variable name for a method parameter (e.g. id, name, bio).
// All-caps field names like ID, UUID become lowercase id, uuid.
func toGoParamName(s string) string {
	f := toGoField(s)
	if len(f) == 0 {
		return "p"
	}
	if f == strings.ToUpper(f) && len(f) >= 2 {
		return strings.ToLower(f)
	}
	return strings.ToLower(f[:1]) + f[1:]
}

func toGoConst(s string) string {
	return strings.ToLower(s[:1]) + s[1:]
}

func catalogHasTable(cat *pb.Catalog, tableName string) bool {
	if cat == nil || tableName == "" {
		return false
	}
	for _, s := range cat.GetSchemas() {
		for _, t := range s.GetTables() {
			if t.GetRel().GetName() == tableName {
				return true
			}
		}
	}
	return false
}

func scanTargets(cols []*pb.Column) string {
	var parts []string
	for _, c := range cols {
		parts = append(parts, "&i."+toGoField(c.GetName()))
	}
	return strings.Join(parts, ", ")
}
