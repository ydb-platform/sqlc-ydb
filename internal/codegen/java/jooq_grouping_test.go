package java

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestJooqComputedGroupingPreservesYQLAliases(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, foo Utf8 NOT NULL, bar Uint64 NOT NULL, PRIMARY KEY(id));`}}
	queries := []model.Source{{Name: "queries.sql", Text: `-- name: Grouped :many
SELECT foo, COUNT(*) AS total FROM records WHERE foo > 0ul GROUP BY bar AS foo;`}}
	analysis, err := analyzer.Analyze(schema, queries)
	require.NoError(t, err)
	files, err := Generate(analysis, Options{Package: "grouping", Runtime: "jooq"})
	require.NoError(t, err)
	for _, file := range files {
		if file.Name == "Queries.java" {
			code := string(file.Content)
			require.Contains(t, code, `prepareStatement(`)
			require.Contains(t, code, `WHERE foo > 0ul GROUP BY bar AS foo`)
			require.Contains(t, code, `dsl.render(RECORDS)`)
			require.NotContains(t, code, `.groupBy(`)
			return
		}
	}
	t.Fatal("Queries.java was not generated")
}
