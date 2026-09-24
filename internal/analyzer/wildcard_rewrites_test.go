package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestWildcardAliasInsertionsPreserveSQLBytes(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY(id));"}}
	sql := "-- name: Read :many\r\n-- Привет 😀 *\r\nSELECT \"строка *\"u /* first */, r.*, id > 0ul /* last */\r\nFROM records AS r ORDER BY column2;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	want := "-- name: Read :many\r\n-- Привет 😀 *\r\nSELECT \"строка *\"u AS `column0` /* first */, r.`id` AS `id`, `r`.`label` AS `label`, id > 0ul AS `column2` /* last */\r\nFROM records AS r ORDER BY column2;"
	query := result.Queries[0]
	require.Equal(t, want, query.SQL)
	var names []string
	for _, column := range query.ResultSets[0].Columns {
		names = append(names, column.ResultName())
	}
	{
		wantNames := []string{"column0", "id", "label", "column2"}
		require.Equal(t, wantNames, names)
	}
}

func TestWildcardRewritesRejectInvalidSpans(t *testing.T) {
	// Invalid offsets cannot come from a successfully parsed query. The rewrite
	// boundary must still reject them without returning partially modified SQL.
	const sql = "SELECT * FROM records;"
	for _, tc := range []struct {
		name         string
		replacements []wildcardReplacement
	}{
		{"negative start", []wildcardReplacement{{start: -1, end: 0, text: "`id`"}}},
		{"past source end", []wildcardReplacement{{start: len(sql), end: len(sql) + 1, text: " AS `column0`"}}},
		{"reversed span", []wildcardReplacement{{start: 8, end: 7, text: "`id`"}}},
		{"overlapping spans", []wildcardReplacement{{start: 7, end: 10, text: "`id`"}, {start: 9, end: 13, text: "FROM"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rewrites := wildcardRewrites{source: sql, replacements: tc.replacements}
			got, err := rewrites.apply()
			require.Error(t, err)
			require.Equal(t, "invalid or overlapping wildcard source span", err.Error())
			require.Equal(t, "", got)
		})
	}
}
