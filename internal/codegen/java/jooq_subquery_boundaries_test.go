package java

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestJooqSubqueryTargetBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, schema, sql, want string
	}{
		{
			name:   "local scalar binding is not a DSL statement",
			schema: jooqSubquerySchema,
			sql:    `$selected = 1ul; SELECT id FROM records WHERE id IN (SELECT id FROM selected WHERE id = $selected);`,
			want:   `Read: unsupported jOOQ syntax "$selected=1ul"`,
		},
		{
			name:   "collection output alias cannot become a scalar order field",
			schema: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id)); CREATE TABLE selected (id Uint64 NOT NULL, tags Json NOT NULL, PRIMARY KEY(id));`,
			sql:    `SELECT id FROM records WHERE NULL IN (SELECT Yson::ConvertToStringList(tags) AS keys FROM selected ORDER BY keys);`,
			want:   "jOOQ does not support type List<String>",
		},
		{
			name:   "inner HAVING is explicitly unsupported by the DSL",
			schema: jooqSubquerySchema,
			sql:    `SELECT id FROM records WHERE id IN (SELECT id FROM selected GROUP BY id HAVING COUNT(*) > 0);`,
			want:   `Read: unsupported jOOQ syntax "SELECTidFROMselectedGROUPBYidHAVINGCOUNT(*)>0"`,
		},
		{
			name:   "update source remains distinct from membership predicate",
			schema: jooqSubquerySchema,
			sql:    `UPDATE records ON SELECT id, label FROM selected WHERE id IN (SELECT id FROM records);`,
			want:   "SELECT-backed DML is unsupported by the jOOQ DSL; use runtime: jdbc or ydb",
		},
		{
			name:   "delete source remains distinct from membership predicate",
			schema: jooqSubquerySchema,
			sql:    `DELETE FROM records ON SELECT id FROM selected WHERE id IN (SELECT id FROM records);`,
			want:   "SELECT-backed DML is unsupported by the jOOQ DSL; use runtime: jdbc or ydb",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := ":many"
			if strings.HasPrefix(tc.sql, "UPDATE") || strings.HasPrefix(tc.sql, "DELETE") {
				command = ":exec"
			}
			analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: tc.schema}}, []model.Source{{Name: "queries.sql", Text: "-- name: Read " + command + "\n" + tc.sql}})
			if err != nil {
				t.Fatal(err)
			}
			files, err := Generate(analysis, Options{Package: "subqueries", Runtime: "jooq"})
			if files != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("files = %v, error = %v; want %q and no output", files, err, tc.want)
			}
		})
	}
}

func TestJooqSubqueryPreservesNegationAndDistinct(t *testing.T) {
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqSubquerySchema}}, []model.Source{{Name: "queries.sql", Text: "-- name: Read :many\nSELECT id FROM records WHERE NOT (id IN (SELECT DISTINCT id FROM selected));"}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(analysis, Options{Package: "subqueries", Runtime: "jooq"})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Name != "Queries.java" {
			continue
		}
		text := string(file.Content)
		if !strings.Contains(text, `"{0} IN ({1})"`) || !strings.Contains(text, "dsl.selectDistinct(SELECTED.ID)") || !strings.Contains(text, ").not()") {
			t.Fatalf("inner DISTINCT or outer negation was lost:\n%s", text)
		}
		return
	}
	t.Fatal("Queries.java was not generated")
}
