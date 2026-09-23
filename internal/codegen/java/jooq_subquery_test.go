package java

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

const jooqSubquerySchema = `CREATE TABLE records (id Uint64 NOT NULL, label Utf8 NOT NULL, PRIMARY KEY(id));
CREATE TABLE selected (id Uint64 NOT NULL, label Utf8 NOT NULL, PRIMARY KEY(id));
CREATE TABLE small_keys (id Int32 NOT NULL, PRIMARY KEY(id));`

const jooqSubqueryQueries = `-- name: ReadSelected :many
SELECT r.id, r.label FROM records AS r
WHERE r.id IN (SELECT r.id FROM selected AS r WHERE r.label = $label)
ORDER BY r.id;
-- name: ReadExcluded :many
SELECT id FROM records WHERE id NOT IN (SELECT id FROM selected) ORDER BY id;
-- name: ReadNested :many
SELECT id FROM records WHERE id IN (SELECT id FROM selected WHERE id IN (SELECT id FROM records WHERE label = $label)) ORDER BY id;
-- name: UpdateSelected :exec
UPDATE records SET label = $new_label WHERE id IN (SELECT id FROM selected WHERE label = $label);
-- name: DeleteSelected :many
DELETE FROM records WHERE id IN (SELECT id FROM selected WHERE label = $label) RETURNING id, label;
-- name: ReadMixedTypes :many
SELECT id FROM records WHERE id IN (SELECT id FROM small_keys WHERE id >= $minimum);
-- name: ReadInnerOrder :many
SELECT id FROM records WHERE label IN (SELECT label AS chosen FROM selected ORDER BY chosen DESC LIMIT 1) ORDER BY id;
-- name: ReadAggregate :many
SELECT id FROM records WHERE id IN (SELECT COUNT(*) AS n FROM selected GROUP BY label ORDER BY n DESC LIMIT 1) ORDER BY id;`

func TestJooqScalarSubqueries(t *testing.T) {
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqSubquerySchema}}, []model.Source{{Name: "queries.sql", Text: jooqSubqueryQueries}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(analysis, Options{Package: "subqueries", Runtime: "jooq"})
	if err != nil {
		t.Fatal(err)
	}
	var queries string
	for _, file := range files {
		if file.Name == "Queries.java" {
			queries = string(file.Content)
		}
	}
	for _, want := range []string{`"{0} IN ({1})"`, `"{0} NOT IN ({1})"`, `dsl.select(SELECTED.as("r").ID)`, `SELECTED.as("r").LABEL.eq(val(label, YdbTypes.UTF8))`, `dsl.update(RECORDS)`, `dsl.deleteFrom(RECORDS)`, `YDB RETURNING produces a result set`} {
		if !strings.Contains(queries, want) {
			t.Errorf("missing %s:\n%s", want, queries)
		}
	}
	if strings.Count(queries, ".orderBy(") != 7 {
		t.Fatalf("ORDER BY leaked into nested SELECT:\n%s", queries)
	}
}

func TestJooqTupleSubqueryRequiresDeclaredPath(t *testing.T) {
	for _, declare := range []bool{false, true} {
		sql := "-- name: Read :many\n"
		if declare {
			sql += "DECLARE $label AS Utf8;\n"
		}
		sql += `SELECT id FROM records WHERE (id, label) IN (SELECT (id, label) FROM selected WHERE label = $label);`
		analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqSubquerySchema}}, []model.Source{{Name: "queries.sql", Text: sql}})
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(analysis, Options{Package: "subqueries", Runtime: "jooq"})
		if !declare {
			if files != nil || err == nil || !strings.Contains(err.Error(), "tuple IN subqueries require explicit DECLARE parameters or runtime: jdbc or ydb") {
				t.Fatalf("tuple DSL: files=%v, error=%v", files, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if file.Name == "Queries.java" && (!strings.Contains(string(file.Content), "SELECT (id, label) FROM") || !strings.Contains(string(file.Content), "dsl.render(SELECTED)")) {
				t.Fatalf("declared tuple SQL/mapped table changed:\n%s", file.Content)
			}
		}
	}
}

func TestJooqSubqueryPrefixAndAliases(t *testing.T) {
	for _, declaration := range []string{"", "DECLARE $label AS Utf8;\n"} {
		analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: "PRAGMA TablePathPrefix('/local/nested');\n" + jooqSubquerySchema}}, []model.Source{{Name: "queries.sql", Text: "-- name: Read :many\nPRAGMA TablePathPrefix('/local/nested');\n" + declaration + `SELECT r.id FROM records AS r WHERE r.id IN (SELECT r.id FROM selected AS r WHERE r.label = $label);`}})
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
			queries := string(file.Content)
			for _, want := range []string{"PRAGMA TablePathPrefix('/local/nested')", "LOCAL_NESTED_RECORDS", "LOCAL_NESTED_SELECTED"} {
				if !strings.Contains(queries, want) {
					t.Errorf("missing %q in prefixed subquery:\n%s", want, queries)
				}
			}
			if declaration != "" && (!strings.Contains(queries, `dsl.render(LOCAL_NESTED_SELECTED)`) || !strings.Contains(queries, ` AS r WHERE r.label = $label`)) {
				t.Fatalf("declared SQL lost inner mapping or source alias:\n%s", queries)
			}
		}
	}
}
