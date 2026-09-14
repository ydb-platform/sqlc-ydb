package java

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/source"
)

func TestJooqAllExampleQueries(t *testing.T) {
	for _, family := range []string{"authors", "batch", "booktest", "jets", "ondeck"} {
		t.Run(family, func(t *testing.T) {
			root := filepath.Join("..", "..", "..", "examples", family)
			schema, queries := "schema.sql", "queries.sql"
			if family == "ondeck" {
				schema, queries = "schema", "query"
			}
			schemas, e := source.Read(root, []string{schema}, true)
			if e != nil {
				t.Fatal(e)
			}
			sources, e := source.Read(root, []string{queries}, false)
			if e != nil {
				t.Fatal(e)
			}
			a, e := analyzer.Analyze(schemas, sources)
			if e != nil {
				t.Fatal(e)
			}
			files, e := Generate(a, Options{Package: family + ".jooq", Runtime: "jooq"})
			if e != nil {
				t.Fatal(e)
			}
			content := string(files[len(files)-1].Content)
			for _, q := range a.Queries {
				if !strings.Contains(content, "// "+model.QueryAnnotation(q)) {
					t.Fatalf("missing method for %s", q.Name)
				}
				if strings.Contains(content, model.WithoutQueryAnnotation(q.SQL)) {
					t.Fatalf("embedded query: %s", q.Name)
				}
			}
		})
	}
}

func TestJooqRejectsUnsupportedSyntax(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE items (id Uint64 NOT NULL, name Utf8, PRIMARY KEY(id));"}}
	for _, sql := range []string{
		"SELECT id FROM items UNION ALL SELECT id FROM items;",
		"SELECT id FROM items WHERE id IN ($id);",
		"SELECT id FROM items LIMIT 2 OFFSET 1;",
		"SELECT CAST(id AS Int64) AS id FROM items;",
	} {
		t.Run(sql, func(t *testing.T) {
			a, e := analyzer.Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Unsupported :many\n" + sql}})
			if e != nil {
				t.Fatal(e)
			}
			files, e := Generate(a, Options{Runtime: "jooq"})
			if e == nil || files != nil {
				t.Fatalf("accepted unsupported syntax: %s", sql)
			}
			if !strings.Contains(e.Error(), "Unsupported: unsupported jOOQ syntax") {
				t.Fatalf("missing actionable diagnostic: %v", e)
			}
		})
	}
}

func TestJooqKeepsEscapedLiteralSemantics(t *testing.T) {
	for _, literal := range []string{`"line\nnext"u`, `"quote\"value"u`} {
		a, e := analyzer.Analyze(nil, []model.Source{{Name: "q.sql", Text: "-- name: Literal :one\nSELECT " + literal + " AS value;"}})
		if e != nil {
			t.Fatal(e)
		}
		_, e = Generate(a, Options{Runtime: "jooq"})
		if e == nil {
			t.Fatalf("silently changed literal %s", literal)
		}
	}
}

func TestJooqImportsPreserveLiterals(t *testing.T) {
	input := "package db;\n\npublic record Row(org.jooq.JSON data) { String value() { return \"org.jooq.JSON\"; } }\n"
	output := jooqImports(input)
	if !strings.Contains(output, `return "org.jooq.JSON";`) || !strings.Contains(output, "import org.jooq.JSON;") || !strings.Contains(output, "Row(JSON data)") {
		t.Fatal(output)
	}
}

func TestJooqExplicitDeclarationsKeepNamedSQLAndTableMapping(t *testing.T) {
	for _, from := range []string{"books AS b", "books"} {
		qualifier := "books"
		if strings.Contains(from, " AS ") {
			qualifier = "b"
		}
		sql := "-- name: Declared :many\nDECLARE $id AS Uint64;\nSELECT " + qualifier + ".id FROM " + from + " WHERE " + qualifier + ".id = $id;"
		analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE books (id Uint64 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "queries.sql", Text: sql}})
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(analysis, Options{Runtime: "jooq"})
		if err != nil {
			t.Fatal(err)
		}
		var generated string
		for _, file := range files {
			if file.Name == "Queries.java" {
				generated = string(file.Content)
			}
		}
		for _, want := range []string{"DECLARE $id AS Uint64;", "YdbPrepareMode.DATA_QUERY", "_prepared.setObject(\"id\"", "dsl.render(BOOKS)", "dsl.fetch(_rows, YdbTypes.UINT64)", qualifier + ".id = $id"} {
			if !strings.Contains(generated, want) {
				t.Fatalf("missing %q in %s", want, generated)
			}
		}
		if qualifier == "books" && !strings.Contains(generated, " AS `books`") {
			t.Fatal("unaliased source lost qualifier under mapping", generated)
		}
	}
}

func TestJooqDeclaredDMLMapsQualifiedTargetColumns(t *testing.T) {
	for _, statement := range []string{"UPDATE books SET title = $title WHERE books.id = $id;", "DELETE FROM books WHERE books.id = $id;", "DELETE FROM books WHERE (books.id = $id);"} {
		sql := "-- name: Declared :exec\nDECLARE $id AS Uint64;\n" + statement
		analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE books (id Uint64 NOT NULL, title Utf8 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "queries.sql", Text: sql}})
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(analysis, Options{Runtime: "jooq"})
		if err != nil {
			t.Fatal(err)
		}
		var code string
		for _, file := range files {
			if file.Name == "Queries.java" {
				code = string(file.Content)
			}
		}
		if strings.Count(code, "dsl.render(BOOKS)") != 2 || strings.Contains(code, "books.id") {
			t.Fatal("target qualifier was not mapped", code)
		}
	}
}
