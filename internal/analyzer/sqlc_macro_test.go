package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestAnalyzeRejectsUnsupportedSQLCMacrosInEveryQueryContext(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE foo (
 id Uint64 NOT NULL,
 name Utf8 NOT NULL,
 PRIMARY KEY (id)
);`}}
	tests := []struct {
		name    string
		command model.Command
		query   string
	}{
		{name: "slice in nested select", command: model.Many, query: "SELECT id FROM (SELECT id FROM foo WHERE id IN sqlc.slice(ids)) nested;"},
		{name: "narg in where", command: model.Many, query: "SELECT id FROM foo WHERE name = sqlc.narg(name);"},
		{name: "narg with quoted name", command: model.Many, query: "SELECT id FROM foo WHERE name = sqlc.narg('name');"},
		{name: "arg in insert", command: model.Exec, query: "INSERT INTO foo (id, name) VALUES ($id, sqlc.arg(name));"},
		{name: "slice in update where", command: model.Exec, query: "UPDATE foo SET name = $name WHERE id IN sqlc.slice(ids);"},
		{name: "arg in delete where", command: model.Exec, query: "DELETE FROM foo WHERE id = sqlc.arg(id);"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := "-- name: Invalid " + string(tt.command) + "\n" + tt.query
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.Error(t, err)
			require.NotNil(t, result)
			require.NotEqual(t, 0, len(result.Diagnostics))
			require.Contains(t, err.Error(), "sqlc macros are unsupported; use DECLARE parameters and explicit result columns instead")
		})
	}
}

func TestAnalyzeIgnoresSQLCMacroTextInStringsCommentsAndQuotedIdentifiers(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (`sqlc.arg` Utf8 NOT NULL, PRIMARY KEY (`sqlc.arg`));"}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: LiteralText :many
-- sqlc.slice(ids) is documentation, not a call.
SELECT 'sqlc.narg(name)' AS macro_text, ` + "`sqlc.arg`" + `
FROM foo /* sqlc.arg(name) */;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	require.Len(t, got.Queries, 1)
	require.Len(t, got.Queries[0].ResultSets, 1)
}
