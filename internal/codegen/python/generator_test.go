package python

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func sampleAnalysis() *model.AnalysisResult {
	return &model.AnalysisResult{
		Catalog: model.Catalog{Tables: []model.Table{{Name: "authors", Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "display_name", Type: model.Optional(model.Type{Kind: "Utf8"})}}}}},
		Queries: []model.AnalyzedQuery{
			{Name: "get_author", Command: model.One, SQL: "DECLARE $id AS Uint64; SELECT '$id', `x` FROM authors WHERE id = $id;", Parameters: []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}}, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "display_name", Type: model.Optional(model.Type{Kind: "Utf8"})}}}}},
			{Name: "list_authors", Command: model.Many, SQL: "SELECT * FROM authors;", ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}}}}},
			{Name: "delete_author", Command: model.Exec, SQL: "DELETE FROM authors WHERE id = $id;", Parameters: []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}}},
			{Name: "count_authors", Command: model.ExecRows, SQL: "UPDATE authors SET display_name = $name;", Parameters: []model.Parameter{{Name: "name", Type: model.Optional(model.Type{Kind: "Utf8"})}}},
		},
	}
}

func liveAnalysis(table string) *model.AnalysisResult {
	utf8 := model.Type{Kind: "Utf8"}
	blob := model.Type{Kind: "String"}
	id := model.Type{Kind: "Uint64"}
	cols := []model.Column{{Name: "id", Type: id, Table: table}, {Name: "name", Type: utf8, Table: table}, {Name: "blob", Type: blob, Table: table}}
	decl := "DECLARE $id AS Uint64; DECLARE $name AS Utf8; DECLARE $blob AS String; "
	return &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: table, Columns: cols}}}, Queries: []model.AnalyzedQuery{
		{Name: "insert_author", Command: model.Exec, SQL: decl + "INSERT INTO " + table + " (id,name,blob) VALUES ($id,$name,$blob);", Parameters: []model.Parameter{{Name: "id", Type: id}, {Name: "name", Type: utf8}, {Name: "blob", Type: blob}}},
		{Name: "get_author", Command: model.One, SQL: "DECLARE $id AS Uint64; SELECT id,name,blob FROM " + table + " WHERE id=$id;", Parameters: []model.Parameter{{Name: "id", Type: id}}, ResultSets: []model.ResultSet{{Columns: cols}}},
		{Name: "get_joined_author", Command: model.One, SQL: "DECLARE $id AS Uint64; SELECT a.id FROM " + table + " AS a INNER JOIN " + table + " AS b ON a.id=b.id WHERE a.id=$id;", Parameters: []model.Parameter{{Name: "id", Type: id}}, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", WireName: "a.id", Type: id, Table: table}}}}},
		{Name: "find_author", Command: model.One, SQL: "DECLARE $name AS Optional<Utf8>; SELECT id,name,blob FROM " + table + " WHERE ($name IS NULL OR name=$name);", Parameters: []model.Parameter{{Name: "name", Type: model.Optional(utf8)}}, ResultSets: []model.ResultSet{{Columns: cols}}},
		{Name: "list_authors", Command: model.Many, SQL: "SELECT id,name,blob FROM " + table + " ORDER BY id;", ResultSets: []model.ResultSet{{Columns: cols}}},
		{Name: "delete_author", Command: model.Exec, SQL: "DECLARE $id AS Uint64; DELETE FROM " + table + " WHERE id=$id;", Parameters: []model.Parameter{{Name: "id", Type: id}}},
	}}
}

func TestGenerateProfilesCompile(t *testing.T) {
	for _, runtime := range []string{"ydb", "dbapi", "sqlalchemy"} {
		a := sampleAnalysis()
		a.Queries = a.Queries[:3]
		a.Catalog.Tables = append(a.Catalog.Tables, model.Table{Name: "авторы", Columns: []model.Column{{Name: "имя", Type: model.Type{Kind: "Utf8"}}}})
		a.Queries[0].Name = "получить_автора"
		files, err := Generate(a, Options{Runtime: runtime})
		if err != nil {
			t.Fatalf("%s: %v", runtime, err)
		}
		dir := t.TempDir()
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(dir, f.Name), f.Content, 0600); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command("python3", "-m", "py_compile", filepath.Join(dir, "models.py"), filepath.Join(dir, "queries.py"))
		cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s generated invalid Python: %v\n%s\n%s", runtime, err, out, files[1].Content)
		}
	}
}

func TestRejectsInvalidPythonNames(t *testing.T) {
	for _, name := range []string{"123authors", "none", "true", "false", "optional"} {
		t.Run("model_"+name, func(t *testing.T) {
			a := &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: name}}}}
			if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), "generated Python name") {
				t.Fatalf("name %q: %v", name, err)
			}
		})
	}
	for _, scope := range []string{"query", "parameter", "column", "table column"} {
		t.Run(scope, func(t *testing.T) {
			a := sampleAnalysis()
			a.Queries = a.Queries[:1]
			switch scope {
			case "query":
				a.Queries[0].Name = "123query"
			case "parameter":
				a.Queries[0].Parameters[0].Name = "123id"
			case "column":
				a.Queries[0].ResultSets[0].Columns[0].Name = "123id"
			case "table column":
				a.Catalog.Tables[0].Columns[0].Name = "123id"
			}
			if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), "generated Python name") {
				t.Fatalf("invalid %s: %v", scope, err)
			}
		})
	}
}

func TestSQLAlchemyLexicalRewriteExact(t *testing.T) {
	q := model.AnalyzedQuery{
		SQL:        "-- name: Пример :one\nDECLARE $author_id AS Uint64;\n$local = $author_id;\nSELECT ':ghost', @@:ghost $author_id@@, `:column`, $local FROM authors WHERE id = $author_id;",
		Parameters: []model.Parameter{{Name: "author_id", Type: model.Type{Kind: "Uint64"}}},
	}
	want := "-- name\\: Пример \\:one\nDECLARE $author_id AS Uint64;\n$local = :author_id;\nSELECT '\\:ghost', @@\\:ghost $author_id@@, `\\:column`, $local FROM authors WHERE id = :author_id;"
	got, err := sqlalchemySQL(q)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("SQL rewrite:\n got: %s\nwant: %s", got, want)
	}
}

func TestYDBRejectsExecRows(t *testing.T) {
	if _, err := Generate(sampleAnalysis(), Options{Runtime: "ydb"}); err == nil || !strings.Contains(err.Error(), "execrows") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRejectsUnsupportedAsyncQuerier(t *testing.T) {
	if _, err := Generate(sampleAnalysis(), Options{Runtime: "sqlalchemy", EmitAsyncQuerier: true}); err == nil || !strings.Contains(err.Error(), "async querier is unsupported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateRejectsUnknownType(t *testing.T) {
	a := sampleAnalysis()
	a.Queries[0].Parameters[0].Type = model.Type{Kind: "Any"}
	if _, err := Generate(a, Options{Runtime: "ydb"}); err == nil || !strings.Contains(err.Error(), "unsupported YQL type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestYDBTypeExpressionRejectsUnknownType(t *testing.T) {
	if _, err := ydbTypeExpr(model.Type{Kind: "Any"}); err == nil || !strings.Contains(err.Error(), "unsupported YQL type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRejectsParameterNamedSelf(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name:       "delete_author",
		Command:    model.Exec,
		Parameters: []model.Parameter{{Name: "self", Type: model.Type{Kind: "Uint64"}}},
	}}}
	if _, err := Generate(a, Options{Runtime: "ydb"}); err == nil || !strings.Contains(err.Error(), `parameter name "self" conflicts with the generated method receiver`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIdenticalTableProjectionsReuseRowModel(t *testing.T) {
	a := liveAnalysis("authors")
	if _, err := Generate(a, Options{Runtime: "dbapi"}); err != nil {
		t.Fatalf("identical table projections should reuse a model: %v", err)
	}
}

func TestSQLAlchemyParameterScannerPreservesLiterals(t *testing.T) {
	a := sampleAnalysis()
	a.Queries = a.Queries[:3]
	files, err := Generate(a, Options{Runtime: "sqlalchemy"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(files[1].Content)
	if !strings.Contains(s, "'$id'") || !strings.Contains(s, "`x`") {
		t.Fatalf("generated SQL lost literal/identifier: %s", s)
	}
}

func TestGeneratedMultilineSQLLiteralIsReadableAndRoundTrips(t *testing.T) {
	queries := []struct {
		name string
		sql  string
	}{
		{"multiline_sql", "-- Привет\r\n\tSELECT \"\"\"quoted\"\"\" AS text;\r\n\\"},
		{"trailing_single_quote", `SELECT "`},
		{"trailing_double_quote", `SELECT ""`},
		{"four_quotes", `SELECT """"`},
		{"five_quotes", `SELECT """""`},
	}
	a := &model.AnalysisResult{}
	for _, q := range queries {
		a.Queries = append(a.Queries, model.AnalyzedQuery{Name: q.name, Command: model.Exec, SQL: q.sql})
	}
	files, err := Generate(a, Options{Runtime: "dbapi"})
	if err != nil {
		t.Fatal(err)
	}
	source := string(files[1].Content)
	if strings.Contains(source, "SQL_MULTILINE_SQL =") || !strings.Contains(source, "cursor.execute(\n") {
		t.Fatalf("expected inline SQL: %s", source)
	}
	dir := t.TempDir()
	pkg := filepath.Join(dir, "db")
	if err := os.Mkdir(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ydb.py"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("import ast, pathlib\ntree = ast.parse(pathlib.Path(%q).read_text())\nvalues = {node.name: next(ast.literal_eval(call.args[0]) for call in ast.walk(node) if isinstance(call, ast.Call) and isinstance(call.func, ast.Attribute) and call.func.attr == 'execute') for node in ast.walk(tree) if isinstance(node, ast.FunctionDef) and not node.name.startswith('_')}\n", filepath.Join(pkg, "queries.py"))
	for _, q := range queries {
		script += fmt.Sprintf("assert values[%q] == %q, repr(values[%q])\n", q.name, q.sql, q.name)
	}
	cmd := exec.Command("python3", "-c", script)
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("multiline SQL did not round-trip through Python: %v\n%s\n%s", err, out, source)
	}
}

func TestGeneratedSQLLiteralsRoundTripSpecialCharacters(t *testing.T) {
	type queryCase struct {
		name string
		sql  string
	}
	queries := []queryCase{
		{"ascii_controls", "SELECT '\x00\a\b\f\v\x1b\x7f';"},
		{"line_endings", "-- LF\n-- CR\r-- CRLF\r\n-- mixed\n\rSELECT 1;"},
		{"layout", "SELECT\n\tcolumn,\n\n\tother\n\n"},
		{"literal_escapes", `SELECT '\n\r\t\u1234\U0001F600\x41';`},
		{"quotes_backticks", "SELECT '\"\\`', `\"$name\\`, \"double\\\"quote\";"},
		{"unicode", "-- Привет 漢字 😀 e\u0301\u2028sep\u2029end\ufeffbom\nSELECT 1;"},
		{"comment_dollar_colon", "-- $name :comment @@:tag\nSELECT '$name:literal', `:column`, $name;"},
	}

	roundTrip := func(t *testing.T, runtime string) {
		t.Helper()
		a := &model.AnalysisResult{}
		expected := map[string]string{}
		for _, q := range queries {
			a.Queries = append(a.Queries, model.AnalyzedQuery{Name: q.name, Command: model.Exec, SQL: q.sql})
			expected["SQL_"+strings.ToUpper(q.name)] = q.sql
		}
		files, err := Generate(a, Options{Runtime: runtime})
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		pkg := filepath.Join(dir, "db")
		if err := os.Mkdir(pkg, 0700); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "ydb.py"), []byte(""), 0600); err != nil {
			t.Fatal(err)
		}
		expectedJSON, err := json.Marshal(expected)
		if err != nil {
			t.Fatal(err)
		}
		expectedPath := filepath.Join(dir, "expected.json")
		if err := os.WriteFile(expectedPath, expectedJSON, 0600); err != nil {
			t.Fatal(err)
		}
		script := fmt.Sprintf("import ast, inspect, json, sys; sys.path.insert(0, %q); from db import queries\ntree = ast.parse(inspect.getsource(queries))\nwith open(%q, encoding='utf-8') as f: expected = json.load(f)\nfor name, want in expected.items():\n    node = next(n for n in ast.walk(tree) if isinstance(n, ast.FunctionDef) and n.name == name.removeprefix('SQL_').lower())\n    call = next(c for c in ast.walk(node) if isinstance(c, ast.Call) and isinstance(c.func, ast.Attribute) and c.func.attr in ('_execute', 'execute', 'execute_with_retries'))\n    got = ast.literal_eval(call.args[0])\n    assert got == want, (name, repr(got), repr(want))\n", dir, expectedPath)
		cmd := exec.Command("python3", "-c", script)
		cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s inline SQL expressions did not round-trip through Python: %v\n%s", runtime, err, out)
		}
	}

	for _, runtime := range []string{"ydb", "dbapi"} {
		t.Run(runtime, func(t *testing.T) { roundTrip(t, runtime) })
	}
}

func TestGeneratedSQLAlchemySQLLiteralRoundTripsLexicalRewrite(t *testing.T) {
	sql := "-- name: Пример :one\n-- comment $author_id :note\nDECLARE $author_id AS Uint64;\n$local = $author_id;\nSELECT ':ghost', @@:ghost $author_id@@, `:column`, $local FROM authors WHERE id = $author_id;"
	want := "-- comment $author_id \\:note\nDECLARE $author_id AS Uint64;\n$local = :author_id;\nSELECT '\\:ghost', @@\\:ghost $author_id@@, `\\:column`, $local FROM authors WHERE id = :author_id;"
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "find_author", Command: model.Exec, SQL: sql, Parameters: []model.Parameter{{Name: "author_id", Type: model.Type{Kind: "Uint64"}}}}}}
	files, err := Generate(a, Options{Runtime: "sqlalchemy"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pkg := filepath.Join(dir, "db")
	if err := os.Mkdir(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		"ydb.py":                 "",
		"sqlalchemy/__init__.py": "def text(s): return s\n",
		"sqlalchemy/engine.py":   "class Connection: pass\n",
	} {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	expectedJSON, err := json.Marshal(map[string]string{"SQL_FIND_AUTHOR": want})
	if err != nil {
		t.Fatal(err)
	}
	expectedPath := filepath.Join(dir, "expected.json")
	if err := os.WriteFile(expectedPath, expectedJSON, 0600); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("import ast, inspect, json, sys; sys.path.insert(0, %q); from db import queries\ntree = ast.parse(inspect.getsource(queries))\nwith open(%q, encoding='utf-8') as f: expected = json.load(f)\ncall = next(c for c in ast.walk(tree) if isinstance(c, ast.Call) and isinstance(c.func, ast.Name) and c.func.id == '_text')\ngot = ast.literal_eval(call.args[0])\nassert got == expected['SQL_FIND_AUTHOR'], (repr(got), repr(expected['SQL_FIND_AUTHOR']))\n", dir, expectedPath)
	cmd := exec.Command("python3", "-c", script)
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("SQLAlchemy inline SQL expression did not round-trip through Python: %v\n%s", err, out)
	}
}

func TestGeneratedYDBQuerierWithMockAdapter(t *testing.T) {
	a := sampleAnalysis()
	a.Queries = a.Queries[:2]
	for _, name := range []string{"ydb", "models", "text"} {
		a.Queries[0].Parameters = append(a.Queries[0].Parameters, model.Parameter{Name: name, Type: model.Type{Kind: "Uint64"}})
	}
	a.Queries[0].ResultSets[0].Columns[0].WireName = "a.id"
	a.Queries[1].ResultSets[0].Columns[0].WireName = "a.id"
	files, err := Generate(a, Options{Runtime: "ydb"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pkg := filepath.Join(dir, "db")
	if err := os.Mkdir(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ydb.py"), []byte(""+
		`class PrimitiveType:
    Uint64 = "Uint64"
class OptionalType:
    def __init__(self, item): self.item = item
class TypedValue:
    def __init__(self, value, typ): self.value, self.type = value, typ
class Row(dict): pass
class ResultSet:
    def __init__(self, rows): self.rows = rows
class RetrySettings:
    def __init__(self, max_retries): self.max_retries = max_retries
class QuerySessionPool: pass
class QueryTxContext: pass
`), 0600); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`import sys
sys.path.insert(0, %q)
from db.queries import Querier
import ydb

class Pool(ydb.QuerySessionPool):
    def __init__(self, results):
        self.results = results
        self.calls = []
    def execute_with_retries(self, sql, parameters, retry_settings=None):
        self.retry_settings = retry_settings
        self.calls.append((sql, parameters))
        return self.results

class Transaction(ydb.QueryTxContext):
    def __init__(self, results):
        self.results = results
        self.calls = []
    def execute(self, sql, parameters):
        self.calls.append((sql, parameters))
        return iter(self.results)

pool = Pool([ydb.ResultSet([ydb.Row({'a.id': 7, 'display_name': None})])])
row = Querier(pool).get_author(7, 8, 9, 10)
assert row.id == 7 and row.display_name is None
assert pool.retry_settings.max_retries == 0
assert pool.calls[0][1]['$id'].type == ydb.PrimitiveType.Uint64
assert [pool.calls[0][1]['$' + name].value for name in ('ydb', 'models', 'text')] == [8, 9, 10]
pool.results = [ydb.ResultSet([ydb.Row({'a.id': 8})])]
rows = Querier(pool).list_authors()
assert isinstance(rows, list)
assert len(rows) == 1 and rows[0].id == 8
assert Querier(Pool([ydb.ResultSet([])])).get_author(7, 8, 9, 10) is None

tx = Transaction([ydb.ResultSet([ydb.Row({'a.id': 9, 'display_name': 'transaction'})])])
row = Querier(tx).get_author(7, 8, 9, 10)
assert row.id == 9 and row.display_name == 'transaction'
assert len(tx.calls) == 1
settings = object()
Querier(pool, retry_settings=settings).list_authors()
assert pool.retry_settings is settings
try:
    Querier(tx, retry_settings=settings)
except ValueError:
    pass
else:
    raise AssertionError("transaction retry settings were silently ignored")

try:
    Querier(Pool([])).get_author(7, 8, 9, 10)
except ValueError:
    pass
else:
    raise AssertionError("missing result set was masked as a missing row")

try:
    Querier(Pool([ydb.ResultSet([]), ydb.ResultSet([])])).get_author(7, 8, 9, 10)
except ValueError:
    pass
else:
    raise AssertionError("additional result set was silently discarded")

try:
    Querier(Pool([ydb.ResultSet([{0: 7, 1: None}])])).get_author(7, 8, 9, 10)
except KeyError:
    pass
else:
    raise AssertionError("missing column names were masked by positional fallback")
`, dir)
	cmd := exec.Command("python3", "-c", script)
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mock YDB execution failed: %v\n%s", err, out)
	}
}

func TestGeneratedSQLAlchemyClosesResultsWithMockAdapter(t *testing.T) {
	a := sampleAnalysis()
	a.Queries = a.Queries[:2]
	for _, name := range []string{"ydb", "models", "text"} {
		a.Queries[0].Parameters = append(a.Queries[0].Parameters, model.Parameter{Name: name, Type: model.Type{Kind: "Uint64"}})
	}
	a.Queries[0].ResultSets[0].Columns[0].WireName = "a.id"
	a.Queries[1].ResultSets[0].Columns[0].WireName = "a.id"
	a.Queries[0].SQL = "-- name: get_author :one\nDECLARE $id AS Uint64; SELECT '$ghost', `x` FROM authors WHERE id = $id;"
	files, err := Generate(a, Options{Runtime: "sqlalchemy"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pkg := filepath.Join(dir, "db")
	if err := os.Mkdir(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		"sqlalchemy/__init__.py":             "def text(s): return s\n",
		"sqlalchemy/engine.py":               "class Connection: pass\n",
		"sqlalchemy/ext/__init__.py":         "",
		"sqlalchemy/ext/asyncio/__init__.py": "class AsyncConnection: pass\n",
	} {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ydb.py"), []byte("class PrimitiveType:\n    Uint64 = 'Uint64'\nclass OptionalType:\n    def __init__(self, item): self.item=item\nclass TypedValue: pass\n"), 0600); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`import sys
sys.path.insert(0, %q)
from db.queries import Querier
class Row:
    def __init__(self, values): self._mapping = values
class R:
    def __init__(self, fail, values): self.closed = False; self.fail = fail; self.values = values
    def fetchone(self):
        if self.fail: raise RuntimeError("fetch failure")
        return Row(self.values)
    def fetchall(self):
        if self.fail: raise RuntimeError("fetch failure")
        return [Row(self.values)]
    def close(self): self.closed = True
class C:
    def __init__(self): self.calls = []; self.fail = False; self.values = {'a.id': 7, 'display_name': None}
    def execute(self, sql, params):
        self.calls.append((sql, params)); self.result = R(self.fail, self.values); return self.result
c = C()
row = Querier(c).get_author(7, 8, 9, 10)
assert row.id == 7 and row.display_name is None and c.result.closed
assert c.calls[0][1]['id'][0] == 7
assert [c.calls[0][1][name][0] for name in ('ydb', 'models', 'text')] == [8, 9, 10]
assert '-- name:' not in c.calls[0][0] and 'id = :id;' in c.calls[0][0], repr(c.calls[0][0])
c.values = {'a.id': 8}
rows = Querier(c).list_authors()
assert isinstance(rows, list)
assert len(rows) == 1 and rows[0].id == 8 and c.result.closed
c.fail = True
try: Querier(c).get_author(7, 8, 9, 10)
except RuntimeError: pass
else: raise AssertionError("fetch error swallowed")
assert c.result.closed
`, dir)
	cmd := exec.Command("python3", "-c", script)
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mock SQLAlchemy execution failed: %v\n%s", err, out)
	}
}

func TestGeneratedDBAPIClosesCursorAndPreservesTransaction(t *testing.T) {
	a := sampleAnalysis()
	a.Queries = a.Queries[:1]
	for _, name := range []string{"ydb", "models", "text"} {
		a.Queries[0].Parameters = append(a.Queries[0].Parameters, model.Parameter{Name: name, Type: model.Type{Kind: "Uint64"}})
	}
	a.Queries[0].ResultSets[0].Columns[0].WireName = "a.id"
	files, err := Generate(a, Options{Runtime: "dbapi"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pkg := filepath.Join(dir, "db")
	if err := os.Mkdir(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ydb.py"), []byte("class PrimitiveType:\n    Uint64 = 'Uint64'\nclass OptionalType:\n    def __init__(self, item): self.item=item\n"), 0600); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`import sys
sys.path.insert(0, %q)
from db.queries import Querier

class Cursor:
    def execute(self, sql, params): self.params = params
    def fetchone(self): return (7, None)
    def fetchall(self): raise AssertionError("fetchall used for :one")
    def close(self): self.closed = True

class Connection:
    def __init__(self): self.cur = Cursor(); self.commits = 0
    def cursor(self): return self.cur
    def commit(self): self.commits += 1

connection = Connection()
row = Querier(connection).get_author(7, 8, 9, 10)
assert row.id == 7 and row.display_name is None
assert connection.cur.closed and connection.commits == 0
assert connection.cur.params['$id'][0] == 7
assert [connection.cur.params['$' + name][0] for name in ('ydb', 'models', 'text')] == [8, 9, 10]
`, dir)
	cmd := exec.Command("python3", "-c", script)
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mock DBAPI execution failed: %v\n%s", err, out)
	}
}

func TestLiveYDBGeneratedRuntimes(t *testing.T) {
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING to run live YDB adapter validation")
	}
	table := "sqlc_python_live_" + fmt.Sprint(time.Now().UnixNano())
	a := liveAnalysis(table)
	root := t.TempDir()
	paths := map[string]string{}
	for _, runtime := range []string{"ydb", "dbapi", "sqlalchemy"} {
		dir := filepath.Join(root, runtime)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		files, err := Generate(a, Options{Runtime: runtime})
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(dir, f.Name), f.Content, 0600); err != nil {
				t.Fatal(err)
			}
		}
		paths[runtime] = dir
	}
	script := fmt.Sprintf(`import os, sys, urllib.parse
dsn = os.environ["YDB_CONNECTION_STRING"]
u = urllib.parse.urlsplit(dsn); endpoint = u.scheme + "://" + u.netloc; database = u.path
sys.path[:0] = [%q, %q, %q, %q]
import ydb
from ydb_generated.queries import Querier as YQuerier
driver = ydb.Driver(ydb.DriverConfig(endpoint, database, credentials=ydb.AnonymousCredentials(), disable_discovery=True)); driver.wait(20)
pool = ydb.QuerySessionPool(driver)
table = %q
pool.execute_with_retries("CREATE TABLE %%s (id Uint64, name Utf8, blob String, PRIMARY KEY(id));" %% table)
try:
    y = YQuerier(pool); y.insert_author(1, "one", b"bytes")
    assert y.get_author(1).blob == b"bytes" and y.get_author(99) is None and y.get_joined_author(1).id == 1
    assert list(y.list_authors())[0].name == "one"; y.find_author(None); y.find_author("one")
    def rollback_generated_calls(tx):
        q = YQuerier(tx)
        q.insert_author(4, "rollback", b"transaction")
        assert q.get_author(4).name == "rollback"
        tx.rollback()
    pool.retry_tx_sync(rollback_generated_calls)
    assert y.get_author(4) is None
    import ydb_dbapi
    host, port = u.hostname, u.port
    c = ydb_dbapi.connect(host=host, port=port, database=database, protocol=u.scheme)
    from dbapi_generated.queries import Querier as DQuerier
    d = DQuerier(c); d.insert_author(2, "two", b"two"); assert d.get_author(2).blob == b"two"; assert d.get_joined_author(2).id == 2; assert d.get_author(999) is None; c.close()
    import sqlalchemy as sa
    import ydb.sqlalchemy
    e = sa.create_engine("yql+ydb://%%s/%%s" %% (u.netloc, database.lstrip("/")))
    with e.begin() as conn:
        from sa_generated.queries import Querier as SQuerier
        s = SQuerier(conn); s.insert_author(3, "three", b"three"); assert s.get_author(3).name == "three"; assert s.get_joined_author(3).id == 3; assert s.get_author(999) is None
    e.dispose()
finally:
    pool.execute_with_retries("DROP TABLE IF EXISTS %%s;" %% table); driver.stop()
`, filepath.Join(paths["ydb"], ".."), paths["ydb"], paths["dbapi"], paths["sqlalchemy"], table)
	// Rename package directories so their relative imports remain isolated and unambiguous.
	for _, pair := range []struct{ from, to string }{{paths["ydb"], filepath.Join(root, "ydb_generated")}, {paths["dbapi"], filepath.Join(root, "dbapi_generated")}, {paths["sqlalchemy"], filepath.Join(root, "sa_generated")}} {
		if err := os.Rename(pair.from, pair.to); err != nil {
			t.Fatal(err)
		}
	}
	// The script uses the final package paths after the rename.
	script = strings.ReplaceAll(script, paths["ydb"], filepath.Join(root, "ydb_generated"))
	script = strings.ReplaceAll(script, paths["dbapi"], filepath.Join(root, "dbapi_generated"))
	script = strings.ReplaceAll(script, paths["sqlalchemy"], filepath.Join(root, "sa_generated"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(root, "pycache"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("live YDB validation failed: %v\n%s", err, out)
	}
}

func TestGeneratedParameterAnnotationsResolve(t *testing.T) {
	query := model.AnalyzedQuery{Name: "write_types", Command: model.Exec, SQL: "SELECT 1;", Parameters: []model.Parameter{
		{Name: "day", Type: model.Type{Kind: "Date"}},
		{Name: "at", Type: model.Type{Kind: "Timestamp"}},
		{Name: "duration", Type: model.Optional(model.Type{Kind: "Interval"})},
		{Name: "ids", Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Uuid"}}},
	}}
	for _, runtime := range []string{"ydb", "dbapi", "sqlalchemy"} {
		t.Run(runtime, func(t *testing.T) {
			files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{query}}, Options{Runtime: runtime})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for _, file := range files {
				path := filepath.Join(dir, "db", file.Name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, file.Content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			// These stubs only let Python import the module; no SDK code is called.
			for name, content := range map[string]string{
				"ydb.py": "", "sqlalchemy/__init__.py": "def text(value): return value\n", "sqlalchemy/engine.py": "class Connection: pass\n",
			} {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			script := `from datetime import date, datetime, timedelta
from typing import Optional, get_type_hints
from uuid import UUID
from db.queries import Querier
assert get_type_hints(Querier.write_types) == {
    "day": date, "at": datetime, "duration": Optional[timedelta], "ids": list[UUID], "return": type(None),
}
`
			cmd := exec.Command("python3", "-c", script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated annotations cannot be resolved: %v\n%s", err, out)
			}
		})
	}
}
