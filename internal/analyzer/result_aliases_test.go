package analyzer

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestResultAliasSpansPreserveSQL(t *testing.T) {
	const sql = "-- name: Names :one\nDECLARE /* тип */ $value AS Utf8;\nSELECT $value AS display_name, 'привет' AS greeting;"
	result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	q := result.Queries[0]
	if len(q.ResultAliases) != 2 {
		t.Fatalf("aliases: %+v", q.ResultAliases)
	}
	runes := []rune(q.SQLWithoutDeclarations)
	span := q.ResultAliases[0]
	if string(runes[span.Start:span.End]) != "display_name" {
		t.Fatalf("alias span: %q", string(runes[span.Start:span.End]))
	}
	rewritten := string(runes[:span.Start]) + "displayName" + string(runes[span.End:])
	if rewritten != strings.Replace(q.SQLWithoutDeclarations, "AS display_name", "AS displayName", 1) {
		t.Fatalf("unexpected rewrite: %q", rewritten)
	}
	if _, diagnostics := parseYQL("query.sql", "DECLARE $value AS Utf8;\n"+rewritten, 0); len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
}

func TestResultAliasInsertionAndUnsafeProjections(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE t (author_id Uint64 NOT NULL, PRIMARY KEY (author_id));"}}
	for _, tc := range []struct {
		sql      string
		editable bool
	}{
		{"SELECT author_id FROM t;", true},
		{"SELECT * FROM t;", false},
		{"SELECT author_id FROM t UNION ALL SELECT author_id FROM t;", false},
		{"SELECT author_id AS author_id FROM t ORDER BY author_id;", false},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Names :many\n" + tc.sql}})
			if err != nil {
				t.Fatal(err)
			}
			q := result.Queries[0]
			if (len(q.ResultAliases) != 0) != tc.editable {
				t.Fatalf("aliases: %+v", q.ResultAliases)
			}
			if tc.editable {
				span := q.ResultAliases[0]
				runes := []rune(q.SQLWithoutDeclarations)
				got := string(runes[:span.Start]) + " AS authorId" + string(runes[span.End:])
				if !strings.Contains(got, "SELECT author_id AS authorId FROM t") {
					t.Fatal(got)
				}
			}
		})
	}
}
