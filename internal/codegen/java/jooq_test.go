package java

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/jdbc"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/source"
	parser "github.com/ydb-platform/yql-parsers/go"
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
			require.Nil(t, e)
			sources, e := source.Read(root, []string{queries}, false)
			require.Nil(t, e)
			a, e := analyzer.Analyze(schemas, sources)
			require.Nil(t, e)
			files, e := Generate(a, Options{Package: family + ".jooq", Runtime: "jooq"})
			require.Nil(t, e)
			content := string(files[len(files)-1].Content)
			for _, q := range a.Queries {
				require.Contains(t, content, "// "+model.QueryAnnotation(q), "missing method for %s", q.Name)
				require.False(t, strings.Contains(content, model.WithoutQueryAnnotation(q.SQL)), "embedded query: %s", q.Name)
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
		"SELECT id + 1ul AS next FROM items;",
		"UPDATE items SET id = id + 1ul RETURNING id;",
	} {
		t.Run(sql, func(t *testing.T) {
			a, e := analyzer.Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Unsupported :many\n" + sql}})
			require.Nil(t, e)
			files, e := Generate(a, Options{Runtime: "jooq"})
			require.False(t, e == nil || files != nil, "accepted unsupported syntax: %s", sql)
			require.Contains(t, e.Error(), "Unsupported: unsupported jOOQ syntax", "missing actionable diagnostic: %v", e)
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
			require.NoError(t, err)
			files, err := Generate(a, Options{Runtime: "jooq"})
			require.False(t, err == nil || files != nil, "SELECT-backed DML must not generate a plain SELECT: generated %d files, err=%v", len(files), err)
			require.Contains(t, err.Error(), "WriteItems: SELECT-backed DML is unsupported by the jOOQ DSL; use runtime: jdbc or ydb", "missing actionable diagnostic: %v", err)
		})
	}
}

func TestJooqKeepsEscapedLiteralSemantics(t *testing.T) {
	for _, literal := range []string{`"line\nnext"u`, `"quote\"value"u`} {
		a, e := analyzer.Analyze(nil, []model.Source{{Name: "q.sql", Text: "-- name: Literal :one\nSELECT " + literal + " AS value;"}})
		require.Nil(t, e)
		_, e = Generate(a, Options{Runtime: "jooq"})
		require.NotNil(t, e, "silently changed literal %s", literal)
	}
}

func TestJooqImportsPreserveLiterals(t *testing.T) {
	input := "package db;\n\npublic record Row(org.jooq.JSON data) { String value() { return \"org.jooq.JSON\"; } }\n"
	output := jooqImports(input)
	require.False(t, !strings.Contains(output, `return "org.jooq.JSON";`) || !strings.Contains(output, "import org.jooq.JSON;") || !strings.Contains(output, "Row(JSON data)"), output)
}

func TestJooqExplicitDeclarationsKeepNamedSQLAndTableMapping(t *testing.T) {
	for _, from := range []string{"books AS b", "books"} {
		qualifier := "books"
		if strings.Contains(from, " AS ") {
			qualifier = "b"
		}
		sql := "-- name: Declared :many\nDECLARE $id AS Uint64;\nSELECT " + qualifier + ".id FROM " + from + " WHERE " + qualifier + ".id = $id;"
		analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE books (id Uint64 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "queries.sql", Text: sql}})
		require.NoError(t, err)
		files, err := Generate(analysis, Options{Runtime: "jooq"})
		require.NoError(t, err)
		var generated string
		for _, file := range files {
			if file.Name == "Queries.java" {
				generated = string(file.Content)
			}
		}
		for _, want := range []string{"DECLARE $id AS Uint64;", "YdbPrepareMode.DATA_QUERY", "_prepared.setObject(\"id\"", "dsl.render(BOOKS)", "dsl.fetch(_rows, YdbTypes.UINT64)", qualifier + ".id = $id"} {
			require.Contains(t, generated, want, "missing %q in %s", want, generated)
		}
		require.False(t, qualifier == "books" && !strings.Contains(generated, " AS `books`"), "unaliased source lost qualifier under mapping", generated)
	}
}

func TestJooqDeclaredDMLMapsQualifiedTargetColumns(t *testing.T) {
	for _, statement := range []string{"UPDATE books SET title = $title WHERE books.id = $id;", "DELETE FROM books WHERE books.id = $id;", "DELETE FROM books WHERE (books.id = $id);"} {
		sql := "-- name: Declared :exec\nDECLARE $id AS Uint64;\n" + statement
		analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE books (id Uint64 NOT NULL, title Utf8 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "queries.sql", Text: sql}})
		require.NoError(t, err)
		files, err := Generate(analysis, Options{Runtime: "jooq"})
		require.NoError(t, err)
		var code string
		for _, file := range files {
			if file.Name == "Queries.java" {
				code = string(file.Content)
			}
		}
		require.False(t, strings.Count(code, "dsl.render(BOOKS)") != 2 || strings.Contains(code, "books.id"), "target qualifier was not mapped", code)
	}
}

// Evaluate mapped fragments together: text-block indentation is computed for
// each fragment, so a literal-only round trip does not exercise this boundary.
func TestJooqDeclaredSQLBytesThroughJava(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE books (id Uint64 NOT NULL, title Utf8 NOT NULL, INDEX by_title GLOBAL SYNC ON (title), PRIMARY KEY(id));"}}
	header := "-- name: Declared :exec\nDECLARE $id AS Uint64;\n\n-- Автор 🚀\n"
	cases := []struct{ sql, want string }{
		{"UPDATE books\n    SET title = $title\n\n    WHERE (books.id = $id);", "UPDATE `mapped_books`\n    SET title = $title\n\n    WHERE (`mapped_books`.id = $id);"},
		{"DELETE FROM books\n    WHERE books.id = $id;", "DELETE FROM `mapped_books`\n    WHERE `mapped_books`.id = $id;"},
		{"SELECT b.id\n\n    FROM books AS b\n    WHERE b.id = $id;", "SELECT b.id\n\n    FROM `mapped_books` AS b\n    WHERE b.id = $id;"},
		{"SELECT books.id\n    FROM books\n    WHERE books.id = $id;", "SELECT books.id\n    FROM `mapped_books` AS `books`\n    WHERE books.id = $id;"},
		{"SELECT *\n    FROM books\n    WHERE id = $id;", "SELECT `id`, `title`\n    FROM `mapped_books` AS `books`\n    WHERE id = $id;"},
		{"SELECT b./* wildcard */*\n    FROM books AS b\n    WHERE b.id = $id;", "SELECT b./* wildcard */`id` AS `id`, `b`.`title` AS `title`\n    FROM `mapped_books` AS b\n    WHERE b.id = $id;"},
		{"DELETE FROM books WHERE books.id = $id RETURNING *;", "DELETE FROM `mapped_books` WHERE `mapped_books`.id = $id RETURNING `id`, `title`;"},
		{"DECLARE $rows AS List<Struct<id: Uint64, title: Utf8>>;\n\nINSERT INTO books (id, title)\nSELECT\n    id, title\nFROM AS_TABLE($rows);", "DECLARE $rows AS List<Struct<id: Uint64, title: Utf8>>;\n\nINSERT INTO `mapped_books` (id, title)\nSELECT\n    id, title\nFROM AS_TABLE($rows);"},
		{"SELECT b.id FROM books VIEW by_title AS b WHERE b.id = $id;", "SELECT b.id FROM `mapped_books` VIEW by_title AS b WHERE b.id = $id;"},
		{"SELECT books.id FROM books /* index */ VIEW `by_title` WHERE books.id = $id;", "SELECT books.id FROM `mapped_books` /* index */ VIEW `by_title` AS `books` WHERE books.id = $id;"},
		{"SELECT books.id, \"Привет 🪄\"u AS label FROM books /* таблица 🐘 */ VIEW /* индекс 🚀 */ by_title WHERE books.id = $id;", "SELECT books.id, \"Привет 🪄\"u AS label FROM `mapped_books` /* таблица 🐘 */ VIEW /* индекс 🚀 */ by_title AS `books` WHERE books.id = $id;"},
		{"SELECT `a``b`.id FROM books VIEW by_title AS `a``b` WHERE `a``b`.id = $id;", "SELECT `a``b`.id FROM `mapped_books` VIEW by_title AS `a``b` WHERE `a``b`.id = $id;"},
		{"SELECT b.id, indexed.title FROM books AS b LEFT JOIN books /* index */ VIEW by_title AS indexed ON b.id = indexed.id WHERE b.id = $id;", "SELECT b.id, indexed.title FROM `mapped_books` AS b LEFT JOIN `mapped_books` /* index */ VIEW by_title AS indexed ON b.id = indexed.id WHERE b.id = $id;"},
		{"INSERT INTO books SELECT b.* FROM books VIEW by_title AS b WHERE b.id = $id;", "INSERT INTO `mapped_books` SELECT b.`id` AS `id`, `b`.`title` AS `title` FROM `mapped_books` VIEW by_title AS b WHERE b.id = $id;"},
		{"UPDATE books SET id = (books.id + 2ul) * 3ul - 4ul WHERE books.id = $id;", "UPDATE `mapped_books` SET id = (`mapped_books`.id + 2ul) * 3ul - 4ul WHERE `mapped_books`.id = $id;"},
	}
	var program strings.Builder
	program.WriteString("public class Main { static final String BOOKS = \"`mapped_books`\"; static final Main dsl = new Main(); String render(String table) { return table; } public static void main(String[] args) {\n")
	for i, tc := range cases {
		annotation := header
		if strings.HasPrefix(tc.sql, "SELECT") || strings.Contains(tc.sql, "RETURNING") {
			annotation = strings.Replace(header, ":exec", ":many", 1)
		}
		a, err := analyzer.Analyze(schema, []model.Source{{Name: "q.sql", Text: annotation + tc.sql}})
		require.NoError(t, err)
		q := a.Queries[0]
		sql, _ := jdbc.SQL(q)
		expression, err := jooqDeclaredSQL(q, sql)
		if hasStructList(q) {
			expression, err = jooqBatchSQL(q, sql)
		}
		require.NoError(t, err)
		expression = indentExpression(expression, "    ")
		for _, line := range strings.Split(expression, "\n") {
			require.False(t, line != "" && strings.TrimSpace(line) == "", "mapped SQL has a whitespace-only source line: %q", line)
		}
		want := "DECLARE $id AS Uint64;\n\n-- Автор 🚀\n" + tc.want
		if i == 0 {
			want = "DECLARE $title AS Utf8;\n" + want
		}
		fmt.Fprintf(&program, "if (!java.util.Base64.getEncoder().encodeToString((%s).getBytes(java.nio.charset.StandardCharsets.UTF_8)).equals(%q)) throw new AssertionError(\"case %d\");\n", expression, base64.StdEncoding.EncodeToString([]byte(want)), i)
	}
	program.WriteString("}}")
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Main.java"), []byte(program.String()), 0600))
	for _, args := range [][]string{{"javac", "--release", "17", "Main.java"}, {"java", "-cp", dir, "Main"}} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			require.NoError(t, err, "%s: %v\n%s\n%s", args[0], err, out, program.String())
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
		require.NoError(t, err)
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
		require.NoError(t, err, "SDK classpath: %v\n%s", err, out)
	}
	cp, err := os.ReadFile(classpath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Main.java"), []byte(program.String()), 0600))
	for _, args := range [][]string{{"javac", "-cp", strings.TrimSpace(string(cp)), "Main.java"}, {"java", "-cp", dir + string(os.PathListSeparator) + strings.TrimSpace(string(cp)), "Main"}} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			require.NoError(t, err, "%s: %v\n%s", args[0], err, out)
		}
	}
}

func TestJooqIndexViewDSL(t *testing.T) {
	for _, tc := range []struct{ view, alias, name string }{
		{"by_title", "", "by_title"},
		{"by_title", " AS b", "by_title"},
		{"`by``title`", "", "by`title"},
		{"`by```", "", "by`"},
		{"```title`", "", "`title"},
	} {
		analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE books (id Uint64 NOT NULL, title Utf8 NOT NULL, INDEX " + tc.view + " GLOBAL SYNC ON (title), PRIMARY KEY(id));"}}, []model.Source{{Name: "queries.sql", Text: "-- name: Indexed :many\nSELECT * FROM books VIEW " + tc.view + tc.alias + " WHERE title = $title;"}})
		require.NoError(t, err)
		files, err := Generate(analysis, Options{Runtime: "jooq"})
		require.NoError(t, err)
		want := `table("{0} VIEW {1}", BOOKS, name(` + strconv.Quote(tc.name) + `))`
		for _, file := range files {
			require.False(t, file.Name == "Queries.java" && !strings.Contains(string(file.Content), want), "VIEW was lost: %s", file.Content)
		}
	}
}

func TestJooqIndexJoins(t *testing.T) {
	schema := `CREATE TABLE books (id Uint64 NOT NULL, author_id Uint64 NOT NULL, title Utf8 NOT NULL, INDEX by_title GLOBAL SYNC ON(title), PRIMARY KEY(id));
CREATE TABLE authors (id Uint64 NOT NULL, name Utf8 NOT NULL, INDEX by_name GLOBAL SYNC ON(name), PRIMARY KEY(id));`
	queries := `-- name: JoinAuthors :many
SELECT b.id, a.name FROM books AS b JOIN authors VIEW by_name AS a ON b.author_id = a.id WHERE a.name = $name;
-- name: LeftJoinAuthors :many
SELECT books.id, authors.name FROM books VIEW by_title LEFT JOIN authors VIEW by_name ON books.author_id = authors.id WHERE books.title = $title;`
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "queries.sql", Text: queries}})
	require.NoError(t, err)
	files, err := Generate(analysis, Options{Package: "indexjoin", Runtime: "jooq"})
	require.NoError(t, err)
	for _, file := range files {
		if file.Name != "Queries.java" {
			continue
		}
		for _, want := range []string{`.join(table("{0} VIEW {1}", AUTHORS, name("by_name")).as("a"))`, `.leftJoin(table("{0} VIEW {1}", AUTHORS, name("by_name")))`, `.from(table("{0} VIEW {1}", BOOKS, name("by_title")))`} {
			require.Contains(t, string(file.Content), want, "missing indexed join source %s in %s", want, file.Content)
		}
	}
	t.Run("published dialect", func(t *testing.T) {
		maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
		if maven == "" {
			t.Skip("set SQLC_YDB_TEST_MAVEN to compile and render index joins against the published dialect")
		}
		dir := t.TempDir()
		classpath := filepath.Join(dir, "classpath")
		cmd := exec.Command(maven, "-q", "dependency:build-classpath", "-Dmdep.outputFile="+classpath)
		cmd.Dir = filepath.Join("..", "..", "..", "tests", "examples", "java", "jooq")
		if out, err := cmd.CombinedOutput(); err != nil {
			require.NoError(t, err, "SDK classpath: %v\n%s", err, out)
		}
		cp, err := os.ReadFile(classpath)
		require.NoError(t, err)
		program := `package indexjoin;
import java.util.*;
import org.jooq.conf.*;
import org.jooq.tools.jdbc.*;
import org.jooq.types.ULong;
import tech.ydb.jooq.YDB;
public class Main {
    public static void main(String[] args) throws Exception {
        var statements = new ArrayList<String>();
        try (var connection = new MockConnection(ctx -> {
            String sql = ctx.sql().replace("` + "`" + `", "").replaceAll("\\s+", " ").toLowerCase(Locale.ROOT);
            statements.add(sql);
            if (!sql.contains("join mapped_authors view by_name") || !sql.contains("mapped_books")) throw new AssertionError(sql);
            String value = statements.size() == 1 ? "Author" : "Title";
            if (!Arrays.equals(ctx.bindings(), new Object[]{value})) throw new AssertionError(Arrays.toString(ctx.bindings()));
            var dsl = YDB.using();
            var result = dsl.newResult(Tables.BOOKS.ID, Tables.AUTHORS.NAME);
            result.add(dsl.newRecord(Tables.BOOKS.ID, Tables.AUTHORS.NAME).values(ULong.valueOf(42), statements.size() == 1 ? "Author" : null));
            return new MockResult[]{new MockResult(1, result)};
        })) {
            var settings = new Settings().withRenderMapping(new RenderMapping().withSchemata(new MappedSchema().withInput("").withTables(
                    new MappedTable().withInput("books").withOutput("mapped_books"), new MappedTable().withInput("authors").withOutput("mapped_authors"))));
            var queries = new Queries(YDB.using(connection, settings));
            var joined = queries.joinAuthors("Author");
            if (joined.size() != 1 || !joined.get(0).id().equals(ULong.valueOf(42)) || !joined.get(0).name().equals("Author")) throw new AssertionError(joined);
            var left = queries.leftJoinAuthors("Title");
            if (left.size() != 1 || !left.get(0).id().equals(ULong.valueOf(42)) || left.get(0).name() != null) throw new AssertionError(left);
            if (statements.size() != 2 || !statements.get(0).contains("b.author_id = a.id") || !statements.get(1).contains("from mapped_books view by_title") || !statements.get(1).contains("left outer join")) throw new AssertionError(statements);
        }
    }
}`
		files = append(files, model.File{Name: "Main.java", Content: []byte(program)})
		compile := []string{"-cp", strings.TrimSpace(string(cp)), "-d", dir}
		for _, file := range files {
			path := filepath.Join(dir, file.Name)
			require.NoError(t, os.WriteFile(path, file.Content, 0600))
			compile = append(compile, path)
		}
		for _, args := range [][]string{append([]string{"javac"}, compile...), {"java", "-cp", dir + string(os.PathListSeparator) + strings.TrimSpace(string(cp)), "indexjoin.Main"}} {
			if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
				require.NoError(t, err, "%s: %v\n%s", args[0], err, out)
			}
		}
	})
}

// These helpers also reject unsupported input at their own boundary. The full
// pipeline currently rejects these sources and table names before calling them.
func TestJooqTableSourceRejectsUnsupportedSources(t *testing.T) {
	for _, source := range []string{"(SELECT id FROM books) AS b", "AS_TABLE($rows) AS r"} {
		t.Run(source, func(t *testing.T) {
			lexer := parser.NewYQLLexer(antlr.NewInputStream(source))
			p := parser.NewYQLParser(antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel))
			p.SetErrorHandler(antlr.NewBailErrorStrategy())
			context := p.Flatten_source()
			require.False(t, lexer.HasError() || p.HasError() || len(jooqNodes[antlr.ErrorNode](context)) != 0, "test source did not parse")
			r := jooqRenderer{}
			if sql := r.tableSource(context, model.TableBinding{}); sql != "" || r.err == nil || !strings.Contains(r.err.Error(), "unsupported jOOQ syntax") {
				require.FailNow(t, fmt.Sprintf("unsupported source produced SQL %q, error %v", sql, r.err))
			}
		})
	}
}

func TestJooqMappingHelpersRejectInvalidTableNames(t *testing.T) {
	schema := "CREATE TABLE `a``b` (id Uint64 NOT NULL, INDEX by_id GLOBAL SYNC ON(id), PRIMARY KEY(id));"
	query := "-- name: Read :many\nDECLARE $id AS Uint64; SELECT id FROM `a``b` VIEW by_id WHERE id = $id;"
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	q := analysis.Queries[0]
	r := jooqRenderer{query: q}
	source := jooqNodes[*parser.Flatten_sourceContext](q.Syntax.Root)[0]
	if sql := r.tableSource(source, q.Syntax.Relations[0]); sql != "" || r.err == nil || r.err.Error() != "cannot represent \"a`b\" as a Java identifier" {
		require.FailNow(t, fmt.Sprintf("invalid table produced SQL %q, error %v", sql, r.err))
	}
	text, _ := jdbc.SQL(q)
	if sql, err := jooqDeclaredSQL(q, text); sql != "" || err == nil || err.Error() != "cannot represent \"a`b\" as a Java identifier" {
		require.FailNow(t, fmt.Sprintf("invalid declared table produced SQL %q, error %v", sql, err))
	}
}

// Table names must also name a generated Java class. Reject names outside that
// contract before attempting declared SQL mapping, including explicit aliases.
func TestJooqRejectsBackticksInTableNames(t *testing.T) {
	for _, table := range []string{"a`b", "a`", "`b", "path/a`b"} {
		quotedTable := "`" + strings.ReplaceAll(table, "`", "``") + "`"
		schema := "CREATE TABLE " + quotedTable + " (id Uint64 NOT NULL, INDEX by_id GLOBAL SYNC ON(id), PRIMARY KEY(id));"
		for _, suffix := range []string{"", " VIEW by_id", " VIEW by_id AS b"} {
			t.Run(table+suffix, func(t *testing.T) {
				sql := "-- name: Declared :many\nDECLARE $id AS Uint64; SELECT id FROM " + quotedTable + suffix + " WHERE id = $id;"
				analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "query.sql", Text: sql}})
				require.NoError(t, err)
				files, err := Generate(analysis, Options{Runtime: "jooq"})
				want := fmt.Sprintf("cannot represent %q as a Java identifier", table)
				require.False(t, err == nil || err.Error() != want || files != nil, "generated unsupported table name: files=%v err=%v, want %q", files, err, want)
			})
		}
	}
}
