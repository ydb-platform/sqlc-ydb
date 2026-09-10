package typescript

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

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
				Name: "GetAuthor", Command: model.One,
				SQL:                    "DECLARE $author_id AS Uint64;\nSELECT id, display_name, bio FROM authors WHERE id = $author_id;\n",
				SQLWithoutDeclarations: "\nSELECT id, display_name, bio FROM authors WHERE id = $author_id;\n",
				Parameters:             []model.Parameter{{Name: "author_id", Type: uint64Type}},
				ResultSets: []model.ResultSet{{Columns: []model.Column{
					{Name: "id", Type: uint64Type}, {Name: "display_name", Type: utf8Type}, {Name: "bio", Type: model.Optional(utf8Type)},
				}}},
			},
			{
				Name: "UpsertAuthor", Command: model.Exec,
				SQL:                    "DECLARE $author_id AS Uint64;\nDECLARE $name AS Utf8;\nUPSERT INTO authors (id, display_name) VALUES ($author_id, $name);",
				SQLWithoutDeclarations: "\n\nUPSERT INTO authors (id, display_name) VALUES ($author_id, $name);",
				Parameters:             []model.Parameter{{Name: "author_id", Type: uint64Type}, {Name: "name", Type: utf8Type}},
			},
			{Name: "ListAuthors", Command: model.Many, SQL: "SELECT id, display_name, bio FROM authors;", SQLWithoutDeclarations: "SELECT id, display_name, bio FROM authors;", ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: uint64Type}, {Name: "display_name", Type: utf8Type}, {Name: "bio", Type: model.Optional(utf8Type)}}}}},
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
	t.Fatalf("missing generated file %s", name)
	return ""
}

func TestGenerateTypeScript(t *testing.T) {
	files, err := Generate(testAnalysis(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	ts := fileContent(t, files, "queries.ts")
	for _, want := range []string{
		`import type { Query, SQL } from "@ydbjs/query";`,
		`export type ConfigureQuery = (query: Query) => Query;`,
		`import { Uint64, Utf8 } from "@ydbjs/value/primitive";`,
		`export class Queries`,
		`async getAuthor(authorId: bigint, configure?: ConfigureQuery)`,
		`.parameter("author_id", new Uint64(_uint64(authorId, "author_id")))`,
		`async upsertAuthor(args: UpsertAuthorParams, configure?: ConfigureQuery)`,
		`.parameter("name", new Utf8(_string(args.name, "name")))`,
		`async listAuthors(configure?: ConfigureQuery)`,
		`if (configure) _pending = configure(_pending);`,
		`return _rows.length === 0 ? null : _decodeGetAuthorRow(_rows[0]);`,
		`return _rows.map(_decodeListAuthorsRow);`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("queries.ts missing %q\n%s", want, ts)
		}
	}
	for _, want := range []string{
		`constructor(sql: SQL) {`,
		`async getAuthor(authorId: bigint, configure?: ConfigureQuery): Promise<GetAuthorRow | null>`,
		`async upsertAuthor(args: UpsertAuthorParams, configure?: ConfigureQuery): Promise<void>`,
		`readonly displayName: string;`,
		`readonly bio: string | null;`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("queries.ts missing %q\n%s", want, ts)
		}
	}
}

func TestConfigureQueryAndSQLNamesCannotShadowGeneratedBindings(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "BySQL", Command: model.Exec, SQLWithoutDeclarations: "SELECT $sql;", Parameters: []model.Parameter{{Name: "sql", Type: model.Type{Kind: "Utf8"}}}},
		{Name: "ByConfigure", Command: model.Exec, SQLWithoutDeclarations: "SELECT $configure;", Parameters: []model.Parameter{{Name: "configure", Type: model.Type{Kind: "Utf8"}}}},
	}}
	files, err := Generate(a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ts := fileContent(t, files, "queries.ts")
	for _, want := range []string{
		`async bySQL(sql_: string, configure?: ConfigureQuery): Promise<void>`,
		`async byConfigure(configure_: string, configure?: ConfigureQuery): Promise<void>`,
		`_string(sql_, "sql")`,
		`_string(configure_, "configure")`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("queries.ts missing %q\n%s", want, ts)
		}
	}
}

func TestSQLLiteralRoundTripsThroughNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	want := "-- a readable query\nSELECT `tick`, '${value}', \\\\path, \"雪\"u, '\t';\r\n-- trailing space \n \t \nSELECT 1;\t"
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Exact", Command: model.Exec, SQL: want, SQLWithoutDeclarations: want}}}
	files, err := Generate(a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	generated := fileContent(t, files, "queries.ts")
	for lineNumber, line := range strings.Split(generated, "\n") {
		if strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t") {
			t.Fatalf("generated TypeScript line %d has trailing whitespace: %q", lineNumber+1, line)
		}
	}
	dir := t.TempDir()
	module := filepath.Join(dir, "queries.mjs")
	transpileModule(t, node, generated, module)
	expected, _ := json.Marshal(want)
	script := `import { EXACT_SQL } from ` + string(mustJSON(module)) + `; if (EXACT_SQL !== ` + string(expected) + `) { throw new Error(JSON.stringify(EXACT_SQL)); }`
	if out, err := exec.Command(node, "--input-type=module", "--eval", script).CombinedOutput(); err != nil {
		t.Fatalf("generated literal did not round-trip: %v\n%s", err, out)
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
	compiler, err := filepath.Abs("../../../examples/node_modules/typescript/lib/typescript.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(compiler); err != nil {
		t.Skip("the pinned TypeScript compiler is unavailable; run npm ci --prefix examples")
	}
	script := `import { pathToFileURL } from "node:url";
const ts = (await import(pathToFileURL(` + string(mustJSON(compiler)) + `))).default;
let source = ""; for await (const chunk of process.stdin) source += chunk;
process.stdout.write(ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } }).outputText);`
	cmd := exec.Command(node, "--input-type=module", "--eval", script)
	cmd.Stdin = strings.NewReader(source)
	generated, err := cmd.Output()
	if err != nil {
		t.Fatalf("transpile generated TypeScript: %v", err)
	}
	if err := os.WriteFile(output, generated, 0600); err != nil {
		t.Fatal(err)
	}
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
		q.Parameters = []model.Parameter{{Name: names[1], Type: model.Type{Kind: "Uint64"}}}
		a.Queries = append(a.Queries, q)
	}
	files, err := Generate(a, Options{})
	if err != nil {
		t.Fatal(err)
	}
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
	if err := os.WriteFile(loader, []byte(loaderSource), 0600); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "main.mjs")
	programSource := `
import { Queries, GET_AUTHOR_SQL } from './queries.mjs';
const calls = [];
const client = (text) => {
  const call = { text, params: [] }; calls.push(call);
  const promise = Promise.resolve([[{ id: 18446744073709551615n, display_name: 'Ada', bio: null }]]);
  call.pending = promise;
  promise.parameter = (name, value) => { call.params.push([name, value.value]); return promise; };
  return promise;
};
const queries = new Queries(client);
let configured = false;
const row = await queries.getAuthor(18446744073709551615n, (pending) => { configured = pending === calls[0].pending; return pending; });
if (!configured) throw new Error('configure hook did not receive the bound query');
if (row.id !== 18446744073709551615n || row.displayName !== 'Ada' || row.bio !== null) throw new Error('decode failed');
if (calls[0].text.includes('DECLARE') || !GET_AUTHOR_SQL.includes('DECLARE')) throw new Error('wrong SQL variant');
if (calls[0].params[0][0] !== 'author_id' || calls[0].params[0][1] !== 18446744073709551615n) throw new Error('binding failed');
for (const [method, name] of [['pending', 'pending'], ['resultSets', 'result_sets'], ['rows', 'rows'], ['named', 'decode_named_row']]) {
  const row = await queries[method](7n);
  const params = calls.at(-1).params;
  if (row.displayName !== 'Ada' || params[0][0] !== name || params[0][1] !== 7n) throw new Error('shadowed parameter: ' + name);
}
const bad = new Queries(() => {
  const promise = Promise.resolve([[{ id: 1n, bio: null }]]);
  promise.parameter = () => promise;
  return promise;
});
try { await bad.getAuthor(1n); throw new Error('missing column accepted'); } catch (error) { if (!String(error).includes('display_name')) throw error; }
`
	if err := os.WriteFile(program, []byte(programSource), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--experimental-loader", loader, program)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated module contract failed: %v\n%s", err, out)
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
				SQLWithoutDeclarations: "SELECT $x;",
				Parameters:             []model.Parameter{{Name: "x", Type: model.Type{Kind: "List"}}},
			}}},
			want: `unsupported YQL type "List"`,
		},
		{name: "method collision", a: &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "foo_bar", Command: model.Exec}, {Name: "foo bar", Command: model.Exec}}}, want: "method name collision"},
		{name: "unrepresentable name", a: &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "bad💣name", Command: model.Exec}}}, want: "cannot represent"},
		{
			name: "parameter collision",
			a: &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
				Name: "Bad", Command: model.Exec, SQLWithoutDeclarations: "SELECT $foo_bar, $`foo bar`;",
				Parameters: []model.Parameter{
					{Name: "foo_bar", Type: model.Type{Kind: "Int32"}},
					{Name: "foo bar", Type: model.Type{Kind: "Int32"}},
				},
			}}},
			want: "parameter name collision",
		},
		{
			name: "missing executable sql",
			a: &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
				Name: "Bad", Command: model.Exec, SQL: "DECLARE $x AS Int32; SELECT $x;",
				Parameters: []model.Parameter{{Name: "x", Type: model.Type{Kind: "Int32"}}},
			}}},
			want: "SQL without declarations",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Generate(tc.a, tc.o)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want error containing %q", err, tc.want)
			}
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
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "AllTypes", Command: model.One, SQL: "SELECT 1;", SQLWithoutDeclarations: "SELECT 1;", Parameters: params, ResultSets: []model.ResultSet{{Columns: cols}}}}}
	if _, err := Generate(a, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestTimestampUsesLosslessMicrosecondsAndRawResultDecoding(t *testing.T) {
	timestamp := model.Type{Kind: "Timestamp"}
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "EchoTimestamp", Command: model.One,
		SQL: "DECLARE $value AS Timestamp; SELECT $value AS value;", SQLWithoutDeclarations: " SELECT $value AS value;",
		Parameters: []model.Parameter{{Name: "value", Type: timestamp}},
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "value", Type: timestamp}}}},
	}}}
	files, err := Generate(a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ts := fileContent(t, files, "queries.ts")
	for _, want := range []string{
		`import { Primitive, TimestampType } from "@ydbjs/value/primitive";`,
		`value > 4291747199999999n`,
		`new Primitive({ value: { case: "uint64Value", value: _timestamp(value, "value") } }, new TimestampType())`,
		`const _resultSets = await _pending.raw();`,
		`value: _rawValue<bigint>(record["value"], "EchoTimestamp.value", "uint64Value")`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("queries.ts missing %q\n%s", want, ts)
		}
	}
	if !strings.Contains(ts, "echoTimestamp(value: bigint, configure?: ConfigureQuery): Promise<EchoTimestampRow | null>") || !strings.Contains(ts, "readonly value: bigint;") {
		t.Fatalf("Timestamp types are not lossless bigint values:\n%s", ts)
	}
}

func TestNumericInputsRejectValuesOutsideYQLRange(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{
			Name: "StoreTimestamp", Command: model.Exec,
			SQL: "DECLARE $value AS Timestamp; SELECT $value;", SQLWithoutDeclarations: " SELECT $value;",
			Parameters: []model.Parameter{{Name: "value", Type: model.Type{Kind: "Timestamp"}}},
		},
		{
			Name: "StoreFloat", Command: model.Exec,
			SQL: "DECLARE $value AS Float; SELECT $value;", SQLWithoutDeclarations: " SELECT $value;",
			Parameters: []model.Parameter{{Name: "value", Type: model.Type{Kind: "Float"}}},
		},
		{
			Name: "StoreOptionalFloat", Command: model.Exec,
			SQL: "DECLARE $value AS Optional<Float>; SELECT $value;", SQLWithoutDeclarations: " SELECT $value;",
			Parameters: []model.Parameter{{Name: "value", Type: model.Optional(model.Type{Kind: "Float"})}},
		},
	}}
	files, err := Generate(a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	module := filepath.Join(dir, "queries.mjs")
	transpileModule(t, node, fileContent(t, files, "queries.ts"), module)
	loader := filepath.Join(dir, "loader.mjs")
	loaderSource := `
export async function resolve(specifier, context, nextResolve) {
  if (specifier === '@ydbjs/value/primitive') return { url: 'stub:primitive', shortCircuit: true };
  if (specifier === '@ydbjs/value/optional') return { url: 'stub:optional', shortCircuit: true };
  return nextResolve(specifier, context);
}
export async function load(url, context, nextLoad) {
  if (url === 'stub:primitive') return { format: 'module', shortCircuit: true, source: ` + "`" + `
    export class Primitive {}
    export class TimestampType {}
    export class FloatType {}
    export class Float { constructor(value) { this.value = value; } }
  ` + "`" + ` };
  if (url === 'stub:optional') return { format: 'module', shortCircuit: true, source: 'export class Optional {}' };
  return nextLoad(url, context);
}`
	if err := os.WriteFile(loader, []byte(loaderSource), 0600); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "main.mjs")
	programSource := `
import { Queries } from './queries.mjs';
const client = () => {
  const pending = Promise.resolve([]);
  pending.parameter = () => pending;
  return pending;
};
const queries = new Queries(client);
for (const [run, message] of [
  [() => queries.storeTimestamp(4291747200000000n), 'Timestamp microsecond range'],
  [() => queries.storeFloat(1e40), 'YQL Float range'],
  [() => queries.storeOptionalFloat(1e40), 'YQL Float range'],
]) {
  try {
    await run();
    throw new Error('out-of-range value accepted');
  } catch (error) {
    if (!(error instanceof RangeError) || !String(error).includes(message)) throw error;
  }
}
`
	if err := os.WriteFile(program, []byte(programSource), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--experimental-loader", loader, program)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated numeric validation failed: %v\n%s", err, out)
	}
}

func TestJSONStaysExactText(t *testing.T) {
	jsonType := model.Type{Kind: "Json"}
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "EchoJSON", Command: model.One,
		SQL: "DECLARE $value AS Json; SELECT $value AS value;", SQLWithoutDeclarations: " SELECT $value AS value;",
		Parameters: []model.Parameter{{Name: "value", Type: jsonType}},
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "value", Type: jsonType}}}},
	}}}
	files, err := Generate(a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ts := fileContent(t, files, "queries.ts")
	for _, want := range []string{
		`function _json(value: unknown, name: string): string`,
		`return value;`,
		`new Json(_json(value, "value"))`,
		`const _resultSets = await _pending.raw();`,
		`value: _rawValue<string>(record["value"], "EchoJSON.value", "textValue")`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("queries.ts missing %q\n%s", want, ts)
		}
	}
	if !strings.Contains(ts, "echoJSON(value: string, configure?: ConfigureQuery): Promise<EchoJSONRow | null>") || !strings.Contains(ts, "readonly value: string;") {
		t.Fatalf("JSON types do not preserve exact text:\n%s", ts)
	}
}

func TestQualifiedProjectionUsesExactWireName(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "Joined", Command: model.Many, SQL: "SELECT b.book_id FROM books AS b;", SQLWithoutDeclarations: "SELECT b.book_id FROM books AS b;",
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "book_id", WireName: "b.book_id", Type: model.Type{Kind: "Uint64"}}}}},
	}}}
	files, err := Generate(a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ts := fileContent(t, files, "queries.ts")
	for _, want := range []string{
		`Object.hasOwn(row, "b.book_id")`,
		`bookId: record["b.book_id"] as bigint`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("queries.ts missing %q\n%s", want, ts)
		}
	}
	if strings.Contains(ts, `Object.hasOwn(row, "book_id")`) {
		t.Fatalf("generated decoder guessed an unqualified fallback:\n%s", ts)
	}
}
