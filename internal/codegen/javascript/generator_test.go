package javascript

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

func TestGenerateQueriesAndDeclarations(t *testing.T) {
	files, err := Generate(testAnalysis(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	js := fileContent(t, files, "queries.js")
	dts := fileContent(t, files, "queries.d.ts")
	for _, want := range []string{
		`import { Uint64, Utf8 } from "@ydbjs/value/primitive";`,
		`export class Queries`,
		`async getAuthor(authorId)`,
		`.parameter("author_id", new Uint64(_uint64(authorId, "author_id")))`,
		`async upsertAuthor(args)`,
		`.parameter("name", new Utf8(_string(args.name, "name")))`,
		`return _rows.length === 0 ? null : _decodeGetAuthorRow(_rows[0]);`,
		`return _rows.map(_decodeListAuthorsRow);`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("queries.js missing %q\n%s", want, js)
		}
	}
	for _, want := range []string{
		`constructor(client: QueryClient);`,
		`getAuthor(authorId: bigint): Promise<GetAuthorRow | null>;`,
		`upsertAuthor(args: UpsertAuthorParams): Promise<void>;`,
		`readonly displayName: string;`,
		`readonly bio: string | null;`,
	} {
		if !strings.Contains(dts, want) {
			t.Errorf("queries.d.ts missing %q\n%s", want, dts)
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
	generated := fileContent(t, files, "queries.js")
	for lineNumber, line := range strings.Split(generated, "\n") {
		if strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t") {
			t.Fatalf("generated JavaScript line %d has trailing whitespace: %q", lineNumber+1, line)
		}
	}
	dir := t.TempDir()
	module := filepath.Join(dir, "queries.mjs")
	if err := os.WriteFile(module, []byte(generated), 0600); err != nil {
		t.Fatal(err)
	}
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
	if err := os.WriteFile(module, []byte(fileContent(t, files, "queries.js")), 0600); err != nil {
		t.Fatal(err)
	}
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
  promise.parameter = (name, value) => { call.params.push([name, value.value]); return promise; };
  return promise;
};
const queries = new Queries(client);
const row = await queries.getAuthor(18446744073709551615n);
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
		{name: "runtime", a: &model.AnalysisResult{}, o: Options{Runtime: "legacy"}, want: `unsupported JavaScript runtime "legacy"`},
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
	js := fileContent(t, files, "queries.js")
	dts := fileContent(t, files, "queries.d.ts")
	for _, want := range []string{
		`import { Primitive, TimestampType } from "@ydbjs/value/primitive";`,
		`value > 4291747199999999n`,
		`new Primitive({ value: { case: "uint64Value", value: _timestamp(value, "value") } }, new TimestampType())`,
		`const _resultSets = await _pending.raw();`,
		`value: _rawValue(row["value"], "EchoTimestamp.value", "uint64Value")`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("queries.js missing %q\n%s", want, js)
		}
	}
	if !strings.Contains(dts, "echoTimestamp(value: bigint): Promise<EchoTimestampRow | null>") || !strings.Contains(dts, "readonly value: bigint;") {
		t.Fatalf("Timestamp declarations are not lossless bigint values:\n%s", dts)
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
	if err := os.WriteFile(module, []byte(fileContent(t, files, "queries.js")), 0600); err != nil {
		t.Fatal(err)
	}
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
	js := fileContent(t, files, "queries.js")
	dts := fileContent(t, files, "queries.d.ts")
	for _, want := range []string{
		`function _json(value, name)`,
		`return value;`,
		`new Json(_json(value, "value"))`,
		`const _resultSets = await _pending.raw();`,
		`value: _rawValue(row["value"], "EchoJSON.value", "textValue")`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("queries.js missing %q\n%s", want, js)
		}
	}
	if !strings.Contains(dts, "echoJSON(value: string): Promise<EchoJSONRow | null>") || !strings.Contains(dts, "readonly value: string;") {
		t.Fatalf("JSON declarations do not preserve exact text:\n%s", dts)
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
	js := fileContent(t, files, "queries.js")
	for _, want := range []string{
		`Object.hasOwn(row, "b.book_id")`,
		`bookId: row["b.book_id"]`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("queries.js missing %q\n%s", want, js)
		}
	}
	if strings.Contains(js, `Object.hasOwn(row, "book_id")`) {
		t.Fatalf("generated decoder guessed an unqualified fallback:\n%s", js)
	}
}
