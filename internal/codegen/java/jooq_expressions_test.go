package java

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestJooqConditionalAggregateExpressions(t *testing.T) {
	analysis, err := analyzer.Analyze(
		[]model.Source{{Name: "schema.sql", Text: "CREATE TABLE items (id Uint64 NOT NULL, note Utf8, PRIMARY KEY(id));"}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Statistics :one\nSELECT COUNT_IF(note IS NOT NULL) AS present, COUNT_IF(note != \"\"u) AS nonempty, CAST(COUNT(*) AS Bool) FROM items;"}},
	)
	require.NoError(t, err)
	files, err := Generate(analysis, Options{Package: "db", Runtime: "jooq"})
	require.NoError(t, err)
	var queries string
	for _, file := range files {
		if file.Name == "Queries.java" {
			queries = string(file.Content)
		}
	}
	for _, expression := range []string{
		`systemName("COUNT_IF")`, `YdbTypes.UINT64`, `ITEMS.NOTE.isNotNull()`,
		`ITEMS.NOTE.ne(inline("", YdbTypes.UTF8))`, `count().coerce(YdbTypes.UINT64).cast(YdbTypes.BOOL)`,
	} {
		assert.Contains(t, queries, expression, "missing %s in generated query:\n%s", expression, queries)
	}
}

func TestJooqNullChecks(t *testing.T) {
	for _, tc := range []struct{ sql, method string }{
		{"IS NULL", "isNull"}, {"IS NOT NULL", "isNotNull"},
		{"ISNULL", "isNull"}, {"NOTNULL", "isNotNull"},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			analysis, err := analyzer.Analyze(
				[]model.Source{{Name: "schema.sql", Text: "CREATE TABLE items (id Uint64 NOT NULL, note Utf8, PRIMARY KEY(id));"}},
				[]model.Source{{Name: "query.sql", Text: "-- name: CheckNote :many\nSELECT note " + tc.sql + " AS missing FROM items;"}},
			)
			require.NoError(t, err)
			files, err := Generate(analysis, Options{Package: "db", Runtime: "jooq"})
			require.NoError(t, err)
			for _, file := range files {
				require.False(t, file.Name == "Queries.java" && !strings.Contains(string(file.Content), "ITEMS.NOTE."+tc.method+"()"), "wrong null predicate: %s", file.Content)
			}
		})
	}
}
