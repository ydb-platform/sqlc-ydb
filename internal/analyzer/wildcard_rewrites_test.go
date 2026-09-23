package analyzer

import (
	"reflect"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestWildcardAliasInsertionsPreserveSQLBytes(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY(id));"}}
	sql := "-- name: Read :many\r\n-- Привет 😀 *\r\nSELECT \"строка *\"u /* first */, r.*, id > 0ul /* last */\r\nFROM records AS r ORDER BY column2;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	want := "-- name: Read :many\r\n-- Привет 😀 *\r\nSELECT \"строка *\"u AS `column0` /* first */, r.`id` AS `id`, `r`.`label` AS `label`, id > 0ul AS `column2` /* last */\r\nFROM records AS r ORDER BY column2;"
	query := result.Queries[0]
	if query.SQL != want {
		t.Fatalf("SQL bytes = %q, want %q", query.SQL, want)
	}
	var names []string
	for _, column := range query.ResultSets[0].Columns {
		names = append(names, column.ResultName())
	}
	if wantNames := []string{"column0", "id", "label", "column2"}; !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("result names = %v, want %v", names, wantNames)
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
			if err == nil || err.Error() != "invalid or overlapping wildcard source span" {
				t.Fatalf("error = %v, want invalid/overlapping span diagnostic", err)
			}
			if got != "" {
				t.Fatalf("invalid rewrite returned usable SQL: %q", got)
			}
		})
	}
}
