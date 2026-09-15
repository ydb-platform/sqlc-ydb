package java

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/jdbc"
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

func TestJooqRejectsSelectBackedDML(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE items (id Uint64 NOT NULL, name Utf8, PRIMARY KEY(id));"}}
	for _, sql := range []string{
		"INSERT INTO items (id, name) SELECT id, name FROM items;",
		"UPSERT INTO items (id, name) SELECT id, name FROM items;",
		"UPDATE items ON SELECT id, name FROM items;",
		"DELETE FROM items ON SELECT id FROM items;",
	} {
		t.Run(sql, func(t *testing.T) {
			a, err := analyzer.Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: WriteItems :exec\n" + sql}})
			if err != nil {
				t.Fatal(err)
			}
			files, err := Generate(a, Options{Runtime: "jooq"})
			if err == nil || files != nil {
				t.Fatalf("SELECT-backed DML must not generate a plain SELECT: generated %d files, err=%v", len(files), err)
			}
			if !strings.Contains(err.Error(), "WriteItems: SELECT-backed DML is unsupported by the jOOQ DSL; use runtime: jdbc or ydb") {
				t.Fatalf("missing actionable diagnostic: %v", err)
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

// Evaluate mapped fragments together: text-block indentation is computed for
// each fragment, so a literal-only round trip does not exercise this boundary.
func TestJooqDeclaredSQLBytesThroughJava(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE books (id Uint64 NOT NULL, title Utf8 NOT NULL, PRIMARY KEY(id));"}}
	header := "-- name: Declared :exec\nDECLARE $id AS Uint64;\n\n-- Автор 🚀\n"
	cases := []struct{ sql, want string }{
		{"UPDATE books\n    SET title = $title\n\n    WHERE (books.id = $id);", "UPDATE `mapped_books`\n    SET title = $title\n\n    WHERE (`mapped_books`.id = $id);"},
		{"DELETE FROM books\n    WHERE books.id = $id;", "DELETE FROM `mapped_books`\n    WHERE `mapped_books`.id = $id;"},
		{"SELECT b.id\n\n    FROM books AS b\n    WHERE b.id = $id;", "SELECT b.id\n\n    FROM `mapped_books` AS b\n    WHERE b.id = $id;"},
		{"SELECT books.id\n    FROM books\n    WHERE books.id = $id;", "SELECT books.id\n    FROM `mapped_books` AS `books`\n    WHERE books.id = $id;"},
		{"DECLARE $rows AS List<Struct<id: Uint64, title: Utf8>>;\n\nINSERT INTO books (id, title)\nSELECT\n    id, title\nFROM AS_TABLE($rows);", "DECLARE $rows AS List<Struct<id: Uint64, title: Utf8>>;\n\nINSERT INTO `mapped_books` (id, title)\nSELECT\n    id, title\nFROM AS_TABLE($rows);"},
	}
	var program strings.Builder
	program.WriteString("public class Main { static final String BOOKS = \"`mapped_books`\"; static final Main dsl = new Main(); String render(String table) { return table; } public static void main(String[] args) {\n")
	for i, tc := range cases {
		annotation := header
		if strings.HasPrefix(tc.sql, "SELECT") {
			annotation = strings.Replace(header, ":exec", ":many", 1)
		}
		a, err := analyzer.Analyze(schema, []model.Source{{Name: "q.sql", Text: annotation + tc.sql}})
		if err != nil {
			t.Fatal(err)
		}
		q := a.Queries[0]
		sql, _ := jdbc.SQL(q)
		expression, err := jooqDeclaredSQL(q, sql)
		if hasStructList(q) {
			expression, err = jooqBatchSQL(q, sql)
		}
		if err != nil {
			t.Fatal(err)
		}
		want := "DECLARE $id AS Uint64;\n\n-- Автор 🚀\n" + tc.want
		if i == 0 {
			want = "DECLARE $title AS Utf8;\n" + want
		}
		fmt.Fprintf(&program, "if (!java.util.Base64.getEncoder().encodeToString((%s).getBytes(java.nio.charset.StandardCharsets.UTF_8)).equals(%q)) throw new AssertionError(\"case %d\");\n", expression, base64.StdEncoding.EncodeToString([]byte(want)), i)
	}
	program.WriteString("}}")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(program.String()), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"javac", "--release", "17", "Main.java"}, {"java", "-cp", dir, "Main"}} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s\n%s", args[0], err, out, program.String())
		}
	}
}

func TestJooqDeclaredCarrierValuesWithSDK(t *testing.T) {
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN to verify jOOQ carrier values against the published SDK")
	}
	cases := []struct{ kind, input, read, want string }{
		{"Uint8", "org.jooq.types.UByte.valueOf(255)", "getUint8()", "255"},
		{"Uint16", "org.jooq.types.UShort.valueOf(65535)", "getUint16()", "65535"},
		{"Uint32", "org.jooq.types.UInteger.valueOf(4294967295L)", "getUint32()", "4294967295L"},
		{"Uint64", "org.jooq.types.ULong.valueOf(\"18446744073709551615\")", "getUint64()", "-1L"},
		{"Utf8", "\"Автор\"", "getText()", "\"Автор\""},
		{"String", "new byte[]{0, -1}", "getBytes()", "new byte[]{0, -1}"},
		{"Json", `org.jooq.JSON.valueOf("{\"a\":1}")`, "getJson()", `"{\"a\":1}"`},
		{"JsonDocument", `org.jooq.JSONB.valueOf("{\"a\":1}")`, "getJsonDocument()", `"{\"a\":1}"`},
	}
	var program strings.Builder
	program.WriteString("import tech.ydb.table.values.*; public class Main { public static void main(String[] args) {\n")
	for _, tc := range cases {
		typ := model.Type{Kind: tc.kind}
		carrier, _, err := jooqType(typ)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&program, "{ %s input = %s; PrimitiveValue value = %s; if (!java.util.Objects.deepEquals(value.%s, %s)) throw new AssertionError(%q);\n", carrier, tc.input, jooqDeclaredValue(model.Parameter{Type: typ}, "input"), tc.read, tc.want, tc.kind)
		expression := jooqDeclaredValue(model.Parameter{Type: model.Optional(typ)}, "input")
		fmt.Fprintf(&program, "OptionalValue present = %s; if (!present.get().equals(value)) throw new AssertionError(\"optional payload\"); input = null; OptionalValue empty = %s; if (empty.isPresent() || !empty.getType().equals(present.getType())) throw new AssertionError(\"optional schema\"); }\n", expression, expression)
	}
	program.WriteString("}}")
	dir := t.TempDir()
	classpath := filepath.Join(dir, "classpath")
	cmd := exec.Command(maven, "-q", "dependency:build-classpath", "-Dmdep.outputFile="+classpath)
	cmd.Dir = filepath.Join("..", "..", "..", "tests", "examples", "java", "jooq")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("SDK classpath: %v\n%s", err, out)
	}
	cp, err := os.ReadFile(classpath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(program.String()), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"javac", "-cp", strings.TrimSpace(string(cp)), "Main.java"}, {"java", "-cp", dir + string(os.PathListSeparator) + strings.TrimSpace(string(cp)), "Main"}} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", args[0], err, out)
		}
	}
}
