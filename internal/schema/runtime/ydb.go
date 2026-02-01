package runtime

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/sqlc-dev/sqlc-engine-ydb/internal/schema"
	"github.com/sqlc-dev/sqlc/pkg/engine"
	"github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/options"
)

// Registry returns a Registry that uses live YDB metadata via connection params.
func Registry(params *engine.ConnectionParams) schema.Registry {
	if params == nil {
		return schema.Empty()
	}
	return &ydbRegistry{params: params}
}

type ydbRegistry struct {
	params *engine.ConnectionParams
}

// databaseFromDSN extracts database name from "grpc[s]://endpoint/database[?query]".
func databaseFromDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return ""
	}
	p := strings.TrimPrefix(u.Path, "/")
	if i := strings.Index(p, "/"); i >= 0 {
		p = p[:i]
	}
	return strings.Trim(p, "/")
}

func (r *ydbRegistry) Columns(tableOrView string) ([]schema.ColumnInfo, bool) {
	dsn := r.params.GetDsn()
	if dsn == "" || tableOrView == "" {
		return nil, false
	}
	dbName := databaseFromDSN(dsn)
	if dbName == "" {
		dbName = "local"
	}
	path := "/" + dbName + "/" + strings.TrimPrefix(tableOrView, "/")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := ydb.Open(ctx, dsn)
	if err != nil {
		return nil, false
	}
	defer func() { _ = db.Close(ctx) }()

	desc, err := db.Table().DescribeTable(ctx, path)
	if err != nil || desc == nil {
		return nil, false
	}
	out := make([]schema.ColumnInfo, 0, len(desc.Columns))
	for _, c := range desc.Columns {
		out = append(out, schema.ColumnInfo{
			Name:     c.Name,
			DataType: ydbTypeString(c),
			Nullable: true,
		})
	}
	return out, true
}

// ydbTypeString returns a YQL-like type string for schema/codegen.
// options.Column uses internal/types; we use Type.Yql() when available via interface.
func ydbTypeString(c options.Column) string {
	type yqlTyper interface{ Yql() string }
	if t, ok := c.Type.(yqlTyper); ok {
		return t.Yql()
	}
	return "Any"
}
