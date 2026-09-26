package typescript

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/source"
)

func TestApprovedExamples(t *testing.T) {
	for _, family := range []string{"authors", "batch", "booktest", "jets", "ondeck"} {
		t.Run(family, func(t *testing.T) {
			base := filepath.Join("../../../examples", family)
			schemaPath, queryPath := "schema.sql", "queries.sql"
			if family == "ondeck" {
				schemaPath, queryPath = "schema", "query"
			}
			schema, err := source.Read(base, []string{schemaPath}, true)
			require.NoError(t, err)
			queries, err := source.Read(base, []string{queryPath}, false)
			require.NoError(t, err)
			options := analyzer.Options{}
			if family == "authors" {
				options.Parameters = map[string]map[string]model.Type{"EchoAuthorIDText": {"author_id": {Kind: "Utf8"}}}
			}
			a, err := analyzer.AnalyzeWithOptions(schema, queries, options)
			require.NoError(t, err)
			files, err := Generate(a, Options{})
			require.NoError(t, err)
			want, err := os.ReadFile(filepath.Join(base, "typescript/native/queries.ts"))
			require.NoError(t, err)
			require.Equal(t, string(files[0].Content), string(want), "generated source differs from maintainer-approved example")
		})
	}
}

func TestSDKNativeTypes(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Echo", Command: model.One,
		SQL:        "SELECT $created, $payload;",
		Parameters: []model.Parameter{{Name: "created", Type: model.Type{Kind: "Timestamp"}}, {Name: "payload", Type: model.Optional(model.Type{Kind: "Json"})}},
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "created", Type: model.Type{Kind: "Timestamp"}}, {Name: "payload", Type: model.Optional(model.Type{Kind: "Json"})}}}},
	}}}
	files, err := Generate(a, Options{})
	require.NoError(t, err)
	got := fileContent(t, files, "queries.ts")
	for _, want := range []string{"readonly created: Date;", "readonly payload: string | null;", "readonly payload: JSValue;", "new Timestamp(args.created)", "new Optional(args.payload === null ? null : new Json(args.payload), new JsonType())", "this.#sql<[EchoRow]>"} {
		assert.Contains(t, got, want, "missing %s", want)
	}
	for _, unwanted := range []string{".raw()", "function _", "_SQL", "Record<string, unknown>"} {
		assert.False(t, strings.Contains(got, unwanted), "unexpected %s", unwanted)
	}
}

func TestResultKeysAreNotNormalized(t *testing.T) {
	keys := []string{"book_id", "bookId", "b.book_id", "default", "имя", "two words"}
	var columns []model.Column
	for _, key := range keys {
		columns = append(columns, model.Column{Name: key, Type: model.Type{Kind: "Utf8"}})
	}
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Rows", Command: model.Many, SQL: "SELECT 1;", ResultSets: []model.ResultSet{{Columns: columns}}}}}
	files, err := Generate(a, Options{})
	require.NoError(t, err)
	got := fileContent(t, files, "queries.ts")
	for _, field := range []string{"book_id", "bookId", `"b.book_id"`, `"default"`, `"имя"`, `"two words"`} {
		assert.Contains(t, got, "readonly "+field+": string;", "missing exact result key %s", field)
	}
	for _, unwanted := range []string{"WireRow", "rows.map", " AS "} {
		assert.False(t, strings.Contains(got, unwanted), "unexpected %s", unwanted)
	}
	a.Queries[0].ResultSets[0].Columns = append(columns, columns[0])
	if _, err := Generate(a, Options{}); err == nil {
		require.Error(t, err, "duplicate exact result key accepted")
	}
}

func TestEmbeddedResultKeepsWireKeysSeparateFromNestedRow(t *testing.T) {
	uint64Type := model.Type{Kind: "Uint64"}
	utf8Type := model.Type{Kind: "Utf8"}
	books := []model.Column{{Name: "book_id", Type: uint64Type}, {Name: "author_id", Type: uint64Type}}
	authors := []model.Column{{Name: "author_id", Type: uint64Type}, {Name: "name", Type: utf8Type}}
	a := &model.AnalysisResult{
		Catalog: model.Catalog{Tables: []model.Table{{Name: "books", Columns: books}, {Name: "authors", Columns: authors}}},
		Queries: []model.AnalyzedQuery{{Name: "GetBookAndAuthor", Command: model.One, SQL: "SELECT b.book_id, b.author_id, a.author_id, a.name FROM books b JOIN authors a ON b.author_id = a.author_id;", ResultSets: []model.ResultSet{{
			Columns: []model.Column{{Name: "book_id", WireName: "embed_books_book_id", Type: uint64Type}, {Name: "author_id", WireName: "embed_books_author_id", Type: uint64Type}, {Name: "author_id", WireName: "embed_authors_author_id", Type: uint64Type}, {Name: "name", WireName: "embed_authors_name", Type: utf8Type}},
			Embeds:  []model.Embedding{{Start: 0, End: 2, Table: "books", Field: "books"}, {Start: 2, End: 4, Table: "authors", Field: "authors"}},
		}}}, {Name: "ListBookAndAuthor", Command: model.Many, SQL: "SELECT 1;", ResultSets: []model.ResultSet{{
			Columns: []model.Column{{Name: "label", Type: utf8Type}, {Name: "book_id", WireName: "embed_books_book_id", Type: uint64Type}, {Name: "author_id", WireName: "embed_books_author_id", Type: uint64Type}, {Name: "rank", Type: model.Type{Kind: "Int32"}}, {Name: "author_id", WireName: "embed_authors_author_id", Type: uint64Type}, {Name: "name", WireName: "embed_authors_name", Type: utf8Type}, {Name: "active", Type: model.Type{Kind: "Bool"}}},
			Embeds:  []model.Embedding{{Start: 1, End: 3, Table: "books", Field: "books"}, {Start: 4, End: 6, Table: "authors", Field: "authors"}},
		}}}},
	}
	files, err := Generate(a, Options{})
	require.NoError(t, err)
	got := fileContent(t, files, "queries.ts")
	require.Contains(t, got, "export type GetBookAndAuthorRow = {\n  readonly books: Books;\n  readonly authors: Authors;")
	require.Contains(t, got, "type GetBookAndAuthorWireRow = {")
	require.Contains(t, got, `book_id: row["embed_books_book_id"]`)
	require.Contains(t, got, `author_id: row["embed_authors_author_id"]`)
	require.Contains(t, got, "return mapped[0] ?? null;")
	require.Contains(t, got, "export type ListBookAndAuthorRow = {\n  readonly label: string;\n  readonly books: Books;\n  readonly rank: number;\n  readonly authors: Authors;\n  readonly active: boolean;")
	require.Contains(t, got, "      label: row[\"label\"],\n      books: {\n        book_id: row[\"embed_books_book_id\"],\n        author_id: row[\"embed_books_author_id\"],\n      },\n      rank: row[\"rank\"],\n      authors: {\n        author_id: row[\"embed_authors_author_id\"],\n        name: row[\"embed_authors_name\"],\n      },\n      active: row[\"active\"],")
	require.Contains(t, got, "return mapped;")

	a.Queries[0].ResultSets[0].Embeds[1].Field = "books"
	_, err = Generate(a, Options{})
	require.ErrorContains(t, err, "embedded field name collision")
}

func TestEmbeddedResultRejectsInconsistentAnalysis(t *testing.T) {
	u64 := model.Type{Kind: "Uint64"}
	a := &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: "books", Columns: []model.Column{{Name: "id", Type: u64}}}}}, Queries: []model.AnalyzedQuery{{Name: "Read", Command: model.One, SQL: "SELECT id FROM books;", ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Int64"}}}, Embeds: []model.Embedding{{Start: 0, End: 1, Table: "books", Field: "books"}}}}}}}
	_, err := Generate(a, Options{})
	require.ErrorContains(t, err, `embedded table "books" does not match projected columns`)

	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE books (id Uint64 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT sqlc.embed(b), b.id AS books FROM books AS b;"}})
	require.NoError(t, err)
	_, err = Generate(analysis, Options{})
	require.ErrorContains(t, err, "result field name collision")
}

func TestProjectionPreservesSQLAndWireNames(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{"SELECT display_name FROM authors;", "SELECT display_name FROM authors;"},
		{"SELECT * FROM authors;", "readonly display_name: string;"},
		{"SELECT display_name FROM authors UNION ALL SELECT display_name FROM authors;", "readonly display_name: string;"},
	} {
		a, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE authors (id Uint64 NOT NULL, display_name Utf8 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: "-- name: List :many\n" + tc.sql}})
		require.NoError(t, err)
		files, err := Generate(a, Options{})
		require.NoError(t, err)
		assert.Contains(t, fileContent(t, files, "queries.ts"), tc.want, "SQL %s", tc.sql)
	}
}

func testAnalysis() *model.AnalysisResult {
	uint64Type := model.Type{Kind: "Uint64"}
	utf8Type := model.Type{Kind: "Utf8"}
	return &model.AnalysisResult{
		Catalog: model.Catalog{Tables: []model.Table{{Name: "authors", Columns: []model.Column{
			{Name: "id", Type: uint64Type},
			{Name: "display_name", Type: utf8Type},
			{Name: "bio", Type: model.Optional(utf8Type)},
		}}}},
		Queries: []model.AnalyzedQuery{
			{
				Name: "GetAuthor", Command: model.One, DeclaredParameters: []string{"author_id"},
				SQL: "DECLARE $author_id AS Uint64;\nSELECT id, display_name, bio FROM authors WHERE id = $author_id;\n",

				Parameters: []model.Parameter{{Name: "author_id", Type: uint64Type}},
				ResultSets: []model.ResultSet{{Columns: []model.Column{
					{Name: "id", Type: uint64Type}, {Name: "display_name", Type: utf8Type}, {Name: "bio", Type: model.Optional(utf8Type)},
				}}},
			},
			{
				Name: "UpsertAuthor", Command: model.Exec, DeclaredParameters: []string{"author_id", "name"},
				SQL: "DECLARE $author_id AS Uint64;\nDECLARE $name AS Utf8;\nUPSERT INTO authors (id, display_name) VALUES ($author_id, $name);",

				Parameters: []model.Parameter{{Name: "author_id", Type: uint64Type}, {Name: "name", Type: utf8Type}},
			},
			{Name: "ListAuthors", Command: model.Many, SQL: "SELECT id, display_name, bio FROM authors;", ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: uint64Type}, {Name: "display_name", Type: utf8Type}, {Name: "bio", Type: model.Optional(utf8Type)}}}}},
		},
	}
}

func fileContent(t *testing.T, files []model.File, name string) string {
	t.Helper()
	for _, file := range files {
		if file.Name == name {
			return string(file.Content)
		}
	}
	require.FailNow(t, fmt.Sprintf("missing generated file %s", name))
	return ""
}

func TestGenerateTypeScript(t *testing.T) {
	files, err := Generate(testAnalysis(), Options{})
	require.NoError(t, err)
	require.Equal(t, 1, len(files), "got %d files, want 1", len(files))
	ts := fileContent(t, files, "queries.ts")
	for _, want := range []string{
		`import type { Query, SQL } from "@ydbjs/query";`,
		`export type ConfigureQuery = (query: Query) => void;`,
		`import { Uint64, Utf8 } from "@ydbjs/value/primitive";`,
		`export class Queries`,
		`async getAuthor(authorId: bigint, configure?: ConfigureQuery)`,
		`.parameter("author_id", new Uint64(authorId))`,
		`async upsertAuthor(args: UpsertAuthorParams, configure?: ConfigureQuery)`,
		`.parameter("name", new Utf8(args.name))`,
		`async listAuthors(configure?: ConfigureQuery)`,
		`configure?.(stmt);`,
		`return rows[0] ?? null;`,
		`return rows;`,
	} {
		assert.Contains(t, ts, want, "queries.ts missing %q\n%s", want, ts)
	}
	for _, want := range []string{
		`constructor(sql: SQL) {`,
		`async getAuthor(authorId: bigint, configure?: ConfigureQuery): Promise<GetAuthorRow | null>`,
		`async upsertAuthor(args: UpsertAuthorParams, configure?: ConfigureQuery): Promise<void>`,
		`readonly display_name: string;`,
		`readonly bio: string | null;`,
	} {
		assert.Contains(t, ts, want, "queries.ts missing %q\n%s", want, ts)
	}
}

func TestConfigureQueryAndSQLNamesCannotShadowGeneratedBindings(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "BySQL", Command: model.Exec, SQL: "SELECT $sql;", Parameters: []model.Parameter{{Name: "sql", Type: model.Type{Kind: "Utf8"}}}},
		{Name: "ByConfigure", Command: model.Exec, SQL: "SELECT $configure;", Parameters: []model.Parameter{{Name: "configure", Type: model.Type{Kind: "Utf8"}}}},
	}}
	files, err := Generate(a, Options{})
	require.NoError(t, err)
	ts := fileContent(t, files, "queries.ts")
	for _, want := range []string{
		`async bySQL(sql_: string, configure?: ConfigureQuery): Promise<void>`,
		`async byConfigure(configure_: string, configure?: ConfigureQuery): Promise<void>`,
		`new Utf8(sql_)`,
		`new Utf8(configure_)`,
	} {
		assert.Contains(t, ts, want, "queries.ts missing %q\n%s", want, ts)
	}
}

func TestExecutableSQLAppearsOnlyAtCall(t *testing.T) {
	const query = "SELECT $value;"
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "Echo", Command: model.Exec, SQL: query,
		Parameters: []model.Parameter{{Name: "value", Type: model.Type{Kind: "Utf8"}}},
	}}}
	files, err := Generate(a, Options{})
	require.NoError(t, err)
	ts := fileContent(t, files, "queries.ts")
	require.False(t, strings.Contains(ts, "_ECHO_SQL_EXEC"), "equal executable SQL produced a duplicate private constant:\n%s", ts)
	require.Equal(t, 1, strings.Count(ts, `"SELECT $value;"`), "got %d SQL literals, want 1:\n%s", strings.Count(ts, `"SELECT $value;"`), ts)
	require.Contains(t, ts, "const stmt = this.#sql", "method does not construct an inline SQL statement:\n%s", ts)
}

func TestSQLLiteralRoundTripsThroughNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	want := "  DECLARE $id AS Uint64; -- source comment\n\n    -- a readable query\n  SELECT `tick`, '${value}', \\\\path, \"雪\"u, '\t';\r\n-- trailing space \n \t \nSELECT 1;\t"
	want += "\nSELECT '"
	for ch := rune(0); ch < 32; ch++ {
		want += string(ch)
	}
	want += "😀\U0001D173';"
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Exact", Command: model.Exec, SQL: want}}}
	files, err := Generate(a, Options{})
	require.NoError(t, err)
	generated := fileContent(t, files, "queries.ts")
	require.Contains(t, generated, "\n      "+strconv.Quote("    -- a readable query\n")+" +\n", "SQL lines must retain source indentation inside literals aligned with the call")
	for lineNumber, line := range strings.Split(generated, "\n") {
		require.False(t, strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t"), "generated TypeScript line %d has trailing whitespace: %q", lineNumber+1, line)
	}
	dir := t.TempDir()
	module := filepath.Join(dir, "queries.mjs")
	transpileModule(t, node, generated, module)
	expected, _ := json.Marshal(want)
	script := `import { Queries } from ` + string(mustJSON(module)) + `; let actual; await new Queries((parts) => { actual = parts; return Promise.resolve([]); }).exact(); if (actual !== ` + string(expected) + `) { throw new Error(JSON.stringify(actual)); }`
	if out, err := exec.Command(node, "--input-type=module", "--eval", script).CombinedOutput(); err != nil {
		require.NoError(t, err, "generated literal did not round-trip: %v\n%s", err, out)
	}
}

func mustJSON(value string) []byte {
	out, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return out
}

func transpileModule(t *testing.T, node, source, output string) {
	t.Helper()
	compiler, err := filepath.Abs("../../../tests/examples/typescript/node_modules/typescript/lib/typescript.js")
	require.NoError(t, err)
	if _, err := os.Stat(compiler); err != nil {
		t.Skip("the pinned TypeScript compiler is unavailable; run npm ci --prefix tests/examples/typescript")
	}
	script := `import { pathToFileURL } from "node:url";
const ts = (await import(pathToFileURL(` + string(mustJSON(compiler)) + `))).default;
let source = ""; for await (const chunk of process.stdin) source += chunk;
process.stdout.write(ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } }).outputText);`
	cmd := exec.Command(node, "--input-type=module", "--eval", script)
	cmd.Stdin = strings.NewReader(source)
	generated, err := cmd.Output()
	require.NoError(t, err, "transpile generated TypeScript: %v", err)
	require.NoError(t, os.WriteFile(output, generated, 0600))
}

func TestGeneratedModuleBindsAndDecodesWithoutShapeFallbacks(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	a := testAnalysis()
	for _, names := range [][2]string{{"pending", "pending"}, {"result_sets", "result_sets"}, {"rows", "rows"}, {"Named", "decode_named_row"}} {
		q := a.Queries[0]
		q.Name = names[0]
		q.SQL = "SELECT $" + names[1] + ";"
		q.DeclaredParameters = nil
		q.Parameters = []model.Parameter{{Name: names[1], Type: model.Type{Kind: "Uint64"}}}
		a.Queries = append(a.Queries, q)
	}
	files, err := Generate(a, Options{})
	require.NoError(t, err)
	dir := t.TempDir()
	module := filepath.Join(dir, "queries.mjs")
	transpileModule(t, node, fileContent(t, files, "queries.ts"), module)
	// The loader supplies the exact public package exports used by generated code;
	// the query client itself is a small contract probe that records named values.
	loader := filepath.Join(dir, "loader.mjs")
	loaderSource := `
export async function resolve(specifier, context, nextResolve) {
  if (specifier === '@ydbjs/value/primitive') return { url: 'stub:primitive', shortCircuit: true };
  if (specifier === '@ydbjs/value/optional') return { url: 'stub:optional', shortCircuit: true };
  return nextResolve(specifier, context);
}
export async function load(url, context, nextLoad) {
  if (url === 'stub:primitive') return { format: 'module', shortCircuit: true, source: ` + "`" + `
    class V { constructor(value) { this.value = value; } }
    export class Uint64 extends V {} export class Uint64Type {}
    export class Utf8 extends V {}
  ` + "`" + ` };
  if (url === 'stub:optional') return { format: 'module', shortCircuit: true, source: 'export class Optional {}' };
  return nextLoad(url, context);
}`
	require.NoError(t, os.WriteFile(loader, []byte(loaderSource), 0600))
	program := filepath.Join(dir, "main.mjs")
	programSource := `
import { Queries } from './queries.mjs';
const calls = [];
const client = (text) => {
  const call = { text, params: [] }; calls.push(call);
  const promise = Promise.resolve([[{ id: 18446744073709551615n, display_name: 'Ada', bio: null }]]);
  call.pending = promise;
  Object.defineProperty(promise, "text", { value: call.text, configurable: true });
  promise.parameter = (name, value) => { call.params.push([name, value.value]); return promise; };
  return promise;
};
const queries = new Queries(client);
let configured = false;
const row = await queries.getAuthor(18446744073709551615n, (pending) => { configured = pending === calls[0].pending; return pending; });
if (!configured) throw new Error('configure hook did not receive the bound query');
if (row.id !== 18446744073709551615n || row.display_name !== 'Ada' || row.bio !== null) throw new Error('decode failed');
if (!calls[0].text.includes('DECLARE $author_id AS Uint64;')) throw new Error('source declaration was removed');
if (calls[0].params[0][0] !== 'author_id' || calls[0].params[0][1] !== 18446744073709551615n) throw new Error('binding failed');
for (const [method, name] of [['pending', 'pending'], ['resultSets', 'result_sets'], ['rows_', 'rows'], ['named', 'decode_named_row']]) {
  const row = await queries[method](7n);
  const params = calls.at(-1).params;
  if (row.display_name !== 'Ada' || params[0][0] !== name || params[0][1] !== 7n) throw new Error('shadowed parameter: ' + name);
}
`
	require.NoError(t, os.WriteFile(program, []byte(programSource), 0600))
	cmd := exec.Command(node, "--experimental-loader", loader, program)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		require.NoError(t, err, "generated module contract failed: %v\n%s", err, out)
	}
}

func TestRejectsUnsupportedInputsAndNameCollisions(t *testing.T) {
	tests := []struct {
		name string
		a    *model.AnalysisResult
		o    Options
		want string
	}{
		{name: "runtime", a: &model.AnalysisResult{}, o: Options{Runtime: "legacy"}, want: `unsupported TypeScript runtime "legacy"`},
		{name: "diagnostics", a: &model.AnalysisResult{Diagnostics: []model.Diagnostic{{Message: "bad"}}}, want: "analysis has diagnostics"},
		{name: "execrows", a: &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Count", Command: model.ExecRows}}}, want: ":execrows is unsupported"},
		{
			name: "type",
			a: &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
				Name: "Bad", Command: model.Exec,
				SQL:        "SELECT $x;",
				Parameters: []model.Parameter{{Name: "x", Type: model.Type{Kind: "List"}}},
			}}},
			want: `unsupported YQL type "List"`,
		},
		{name: "method collision", a: &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "foo_bar", Command: model.Exec}, {Name: "foo bar", Command: model.Exec}}}, want: "method name collision"},
		{name: "unrepresentable name", a: &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "bad💣name", Command: model.Exec}}}, want: "cannot represent"},
		{
			name: "parameter collision",
			a: &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
				Name: "Bad", Command: model.Exec, SQL: "SELECT $foo_bar, $`foo bar`;",
				Parameters: []model.Parameter{
					{Name: "foo_bar", Type: model.Type{Kind: "Int32"}},
					{Name: "foo bar", Type: model.Type{Kind: "Int32"}},
				},
			}}},
			want: "parameter name collision",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Generate(tc.a, tc.o)
			require.ErrorContains(t, err, tc.want, "got %v, want error containing %q", err, tc.want)
		})
	}
}

func TestSupportedExampleTypes(t *testing.T) {
	types := []model.Type{
		{Kind: "Bool"}, {Kind: "Int8"}, {Kind: "Uint8"}, {Kind: "Int16"}, {Kind: "Uint16"}, {Kind: "Int32"}, {Kind: "Uint32"},
		{Kind: "Int64"}, {Kind: "Uint64"}, {Kind: "Float"}, {Kind: "Double"}, {Kind: "Utf8"}, {Kind: "String"}, {Kind: "Json"}, {Kind: "JsonDocument"}, {Kind: "Timestamp"},
	}
	var params []model.Parameter
	var cols []model.Column
	for _, typ := range types {
		name := strings.ToLower(typ.Kind)
		params = append(params, model.Parameter{Name: name, Type: typ}, model.Parameter{Name: "optional_" + name, Type: model.Optional(typ)})
		cols = append(cols, model.Column{Name: name, Type: typ}, model.Column{Name: "optional_" + name, Type: model.Optional(typ)})
	}
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "AllTypes", Command: model.One, SQL: "SELECT 1;", Parameters: params, ResultSets: []model.ResultSet{{Columns: cols}}}}}
	if _, err := Generate(a, Options{}); err != nil {
		require.NoError(t, err, fmt.Sprint(err))
	}
}

func TestQualifiedProjectionUsesExactWireName(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "Joined", Command: model.Many, SQL: "SELECT b.book_id FROM books AS b;",
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "book_id", WireName: "b.book_id", Type: model.Type{Kind: "Uint64"}}}}},
	}}}
	files, err := Generate(a, Options{})
	require.NoError(t, err)
	ts := fileContent(t, files, "queries.ts")
	for _, want := range []string{
		`readonly "b.book_id": bigint;`,
		`readonly "b.book_id": bigint;`,
	} {
		assert.Contains(t, ts, want, "queries.ts missing %q\n%s", want, ts)
	}
	require.False(t, strings.Contains(ts, `Object.hasOwn(row, "book_id")`), "generated decoder guessed an unqualified fallback:\n%s", ts)
}

func TestStructListParameter(t *testing.T) {
	typ := model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, {Name: "tags", Type: model.Optional(model.Type{Kind: "Json"})}}}}
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "CreateBooks", Command: model.Exec, SQL: "SELECT $books;", Parameters: []model.Parameter{{Name: "books", Type: typ}}}}}
	files, err := Generate(a, Options{})
	require.NoError(t, err)
	var output strings.Builder
	for _, f := range files {
		output.Write(f.Content)
	}
	for _, want := range []string{"CreateBooksBooksItem", "ReadonlyArray<CreateBooksBooksItem>", "new ListType(type)", "new StructType(\n          [\n            \"book_id\",\n            \"tags\",\n", "new OptionalType(new JsonType())", "item.bookId"} {
		assert.Contains(t, output.String(), want, "missing %s", want)
	}
	a.Queries[0].Parameters[0].Type.Elem.Fields[1].Type = model.Type{Kind: "List", Elem: &model.Type{Kind: "Utf8"}}
	if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), "unsupported YQL type") {
		require.FailNow(t, fmt.Sprintf("nested list: %v", err))
	}
}

func TestStructFieldCollision(t *testing.T) {
	typ := model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, {Name: "bookId", Type: model.Type{Kind: "Uint64"}}}}}
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "CreateBooks", Command: model.Exec, SQL: "SELECT $books;", Parameters: []model.Parameter{{Name: "books", Type: typ}}}}}
	if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), "collision") {
		require.FailNow(t, fmt.Sprintf("field collision: %v", err))
	}
}

func TestStructPrototypeMember(t *testing.T) {
	typ := model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "__proto__", Type: model.Type{Kind: "Uint64"}}}}}
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "CreateBooks", Command: model.Exec, SQL: "SELECT $books;", Parameters: []model.Parameter{{Name: "books", Type: typ}}}}}
	files, err := Generate(a, Options{})
	require.NoError(t, err)
	require.Contains(t, string(files[0].Content), `["__proto__"]: new Uint64(item.proto)`, "struct member must be an own property, not an object prototype setter")
}

func TestStructListsEncodeWithPinnedSDK(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	modules, err := filepath.Abs("../../../tests/examples/typescript/node_modules")
	require.NoError(t, err)
	if _, err := os.Stat(filepath.Join(modules, "@ydbjs/value")); err != nil {
		t.Skip("run npm ci --prefix tests/examples/typescript")
	}
	typ := model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "__proto__", Type: model.Type{Kind: "Uint64"}}, {Name: "tags", Type: model.Optional(model.Type{Kind: "Json"})}}}}
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "CreateBooks", Command: model.Exec, SQL: "SELECT $struct_list;", Parameters: []model.Parameter{{Name: "struct_list", Type: typ}}}}}
	files, err := Generate(a, Options{})
	require.NoError(t, err)
	require.Contains(t, string(files[0].Content), "structList(\n        structList_.map(", "list argument shadows SDK binding helper")
	dir := t.TempDir()
	require.NoError(t, os.Symlink(modules, filepath.Join(dir, "node_modules")))
	module := filepath.Join(dir, "queries.mjs")
	transpileModule(t, node, string(files[0].Content), module)
	script := `import assert from 'node:assert/strict'; import { Queries } from ` + string(mustJSON(module)) + `;
let parameter;
const sql = () => {const stmt=Promise.resolve([]);stmt.parameter=(_name,value)=>{parameter=value;return stmt};return stmt};
for(const books of [[], [{proto:18446744073709551615n,tags:null},{proto:1n,tags:'{"x":true}'}]]){
 await new Queries(sql).createBooks(books);
 const members=parameter.type.encode().type.value.item.type.value.members;
 assert.deepEqual(members.map(m=>m.name),['__proto__','tags']);
 assert.equal(members[1].type.type.case,'optionalType');
 const items=parameter.encode().items;assert.equal(items.length,books.length);
 for(let i=0;i<books.length;i++){
  assert.equal(items[i].items[0].value.value,books[i].proto);
  assert.equal(items[i].items[1].value.case,books[i].tags===null?'nullFlagValue':'textValue');
  if(books[i].tags!==null)assert.equal(items[i].items[1].value.value,books[i].tags);
 }
}`
	if out, err := exec.Command(node, "--input-type=module", "--eval", script).CombinedOutput(); err != nil {
		require.NoError(t, err, "SDK struct-list serialization: %v\n%s", err, out)
	}
}

func TestDeclaredSQLReachesPinnedSDKWithoutDuplicateDeclarations(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	modules, err := filepath.Abs("../../../tests/examples/typescript/node_modules")
	require.NoError(t, err)
	if _, err := os.Stat(filepath.Join(modules, "@ydbjs/query")); err != nil {
		t.Skip("run npm ci --prefix tests/examples/typescript")
	}
	typ := model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, {Name: "tags", Type: model.Optional(model.Type{Kind: "Json"})}}}}
	sql := "-- declaration formatting stays intact\nDECLARE $books AS List<Struct<\n    book_id: Uint64,\n    tags: Optional<Json>\n>>;\n\nINSERT INTO books SELECT book_id, tags FROM AS_TABLE($books);"
	mixed := sql[:len(sql)-1] + " WHERE book_id > $minimum;"
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "InsertBooks", Command: model.Exec, SQL: sql, DeclaredParameters: []string{"books"}, Parameters: []model.Parameter{{Name: "books", Type: typ}}},
		{Name: "MixedBooks", Command: model.Exec, SQL: mixed, DeclaredParameters: []string{"books"}, Parameters: []model.Parameter{{Name: "books", Type: typ}, {Name: "minimum", Type: model.Type{Kind: "Uint64"}}}},
	}}
	files, err := Generate(a, Options{})
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.Symlink(modules, filepath.Join(dir, "node_modules")))
	module := filepath.Join(dir, "queries.mjs")
	transpileModule(t, node, string(files[0].Content), module)
	require.Contains(t, string(files[0].Content), "DECLARE $books AS List<Struct<", "SQL declaration brackets must remain readable")
	expected := sql
	expectedMixed := "DECLARE $minimum AS Uint64;\n" + mixed
	script := `import assert from 'node:assert/strict';
import { query } from '@ydbjs/query';
import { StatusIds_StatusCode as Status } from '@ydbjs/api/operation';
import { Queries } from './queries.mjs';
const requests=[];
let failNext=false, commits=0, configurations=0;
const rpc={
 async createSession(){return {status:Status.SUCCESS,sessionId:'declared-sql',nodeId:1n}},
 async *attachSession(_request,{signal}){yield {status:Status.SUCCESS};await new Promise(resolve=>{if(signal.aborted)resolve();else signal.addEventListener('abort',resolve,{once:true})})},
 async *executeQuery(request){requests.push(request);if(failNext){failNext=false;yield {status:Status.OVERLOADED,issues:[]};return}yield {status:Status.SUCCESS}},
 async deleteSession(){return {status:Status.SUCCESS}},
 async beginTransaction(){return {status:Status.SUCCESS,txMeta:{id:'caller-tx'}}},
 async commitTransaction(){commits++;return {status:Status.SUCCESS}},
};
const client=query({identity:'declared-sql-probe',async ready(){},createClient(){return rpc}},{poolOptions:{minSize:0,maxSize:1}});
const probe=client(['SELECT 1;']);
assert.equal(Object.hasOwn(probe,'text'),false);
let prototype=Object.getPrototypeOf(probe), descriptor;
while(prototype && !descriptor){descriptor=Object.getOwnPropertyDescriptor(prototype,'text');prototype=Object.getPrototypeOf(prototype)}
assert.equal(typeof descriptor.get,'function');
assert.equal(descriptor.set,undefined);
assert.equal(descriptor.configurable,true);
assert.equal(Object.isExtensible(probe),true);
assert.throws(() => { probe.text='SELECT 2;'; },TypeError);
assert.equal(Object.hasOwn({...probe},'text'),false);
const configure = stmt => {
 configurations++;
 const own=Object.getOwnPropertyDescriptor(stmt,'text');
 assert.equal(own.writable,false);
 assert.equal(own.configurable,false);
 assert.equal(own.enumerable,false);
 assert.equal(typeof own.value,'string');
 stmt.idempotent(true).timeout(5000);
};
const inputs=[[],[{bookId:18446744073709551615n,tags:null},{bookId:1n,tags:'{"present":true}'}]];
try {
 for(const books of inputs){
  const before=requests.length;
  await new Queries(client).insertBooks(books,configure);
  const request=requests[before];
  assert.equal(request.query.value.text,` + string(mustJSON(expected)) + `);
  assert.equal((request.query.value.text.match(/DECLARE \$books/g)||[]).length,1);
  assert.equal(request.parameters.$books.value.items.length,books.length);
  assert.equal(request.parameters.$books.type.type.value.item.type.case,'structType');
 }
 failNext=true;
 const before=requests.length;
 await new Queries(client).mixedBooks({books:inputs[1],minimum:0n},configure);
 assert.equal(requests.length,before+2);
 for(const request of requests.slice(before)){
  assert.equal(request.query.value.text,` + string(mustJSON(expectedMixed)) + `);
  assert.equal(request.parameters.$minimum.value.value.value,0n);
  assert.equal(request.parameters.$books.value.items.length,2);
 }
 await client.transaction(async tx => {
  await new Queries(tx).insertBooks([],configure);
  assert.equal(requests.at(-1).txControl.txSelector.value,'caller-tx');
  assert.equal(requests.at(-1).txControl.commitTx,false);
  assert.equal(requests.at(-1).query.value.text,` + string(mustJSON(expected)) + `);
 });
 assert.equal(commits,1);
 assert.equal(configurations,4);
} finally {await client[Symbol.asyncDispose]();}
`
	program := filepath.Join(dir, "probe.mjs")
	require.NoError(t, os.WriteFile(program, []byte(script), 0600))
	if out, err := exec.Command(node, program).CombinedOutput(); err != nil {
		require.NoError(t, err, "SDK declaration transport: %v\n%s", err, out)
	}
}

func TestStructListRejectsInvalidFieldsAndAmbiguousTypeNames(t *testing.T) {
	makeQuery := func(name, parameter string, fields []model.StructField) model.AnalyzedQuery {
		return model.AnalyzedQuery{Name: name, Command: model.Exec, SQL: "SELECT 1;", Parameters: []model.Parameter{{Name: parameter, Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: fields}}}}}
	}
	valid := []model.StructField{{Name: "id", Type: model.Type{Kind: "Uint64"}}}
	for _, tc := range []struct {
		name    string
		queries []model.AnalyzedQuery
		want    string
	}{
		{"empty struct", []model.AnalyzedQuery{makeQuery("CreateBooks", "books", nil)}, "at least one scalar field"},
		{"invalid identifier", []model.AnalyzedQuery{makeQuery("CreateBooks", "books", []model.StructField{{Name: "💣", Type: model.Type{Kind: "Utf8"}}})}, "cannot represent"},
		{"ambiguous item type", []model.AnalyzedQuery{makeQuery("CreateBooks", "values", valid), makeQuery("Create", "books_values", valid)}, "item type name collision"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Generate(&model.AnalysisResult{Queries: tc.queries}, Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
				require.FailNow(t, fmt.Sprintf("got %v, want %s", err, tc.want))
			}
		})
	}
}
