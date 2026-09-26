package endtoend

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/config"
	"github.com/ydb-platform/sqlc-ydb/internal/database"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestLiveYDBConfiguredParameterTypes(t *testing.T) {
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING for configured parameter validation")
	}
	settings, err := (config.Database{URI: dsn, Timeout: "30s"}).Resolve(t.TempDir())
	require.NoError(t, err)
	client, err := database.New(settings)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	for _, tc := range []struct {
		name, sql, parameter, typ string
	}{
		{"scalar", "SELECT $value AS value;", "value", "Utf8"},
		{"structured source", "SELECT r.id FROM AS_TABLE($rows) AS r;", "rows", "List<Struct<id:Uint64>>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			typ, err := analyzer.ParseType(tc.typ)
			require.NoError(t, err)
			text := "-- name: Read :many\n" + tc.sql
			options := analyzer.Options{Parameters: map[string]map[string]model.Type{"Read": {tc.parameter: typ}}}
			result, err := analyzer.AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: text}}, options, client)
			require.NoError(t, err)
			require.Equal(t, text, result.Queries[0].SQL)
			require.Equal(t, []model.Parameter{{Name: tc.parameter, Type: typ}}, result.Queries[0].Parameters)
		})
	}
	t.Run("nullable sqlc argument", func(t *testing.T) {
		const text = "-- name: Read :one\nSELECT sqlc.narg('value') AS value;"
		typ := model.Optional(model.Type{Kind: "Utf8"})
		options := analyzer.Options{Parameters: map[string]map[string]model.Type{"Read": {"value": typ}}}
		result, err := analyzer.AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: text}}, options, client)
		require.NoError(t, err)
		require.Equal(t, "-- name: Read :one\nSELECT $`value` AS value;", result.Queries[0].SQL)
		require.Equal(t, []model.Parameter{{Name: "value", Type: typ}}, result.Queries[0].Parameters)
	})
}
