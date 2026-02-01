package ydb

import (
	"strings"

	"github.com/sqlc-dev/sqlc-engine-ydb/internal/codegen/pb"
)

// ydbTypeName returns the YDB type name from plugin Column (e.g. "uuid", "utf8", "uint64").
func ydbTypeName(c *pb.Column) string {
	if c == nil || c.Type == nil {
		return ""
	}
	return strings.ToLower(c.Type.Name)
}

// goTypeFromYDB returns the Go type for a YDB column (e.g. "uint64" -> "uint64", "utf8" -> "string").
// Optional types become *T.
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
	case "uuid":
		t = "string"
	case "yson", "json":
		t = "string"
	default:
		t = "interface{}"
	}
	if !notNull {
		return "*" + t
	}
	return t
}
