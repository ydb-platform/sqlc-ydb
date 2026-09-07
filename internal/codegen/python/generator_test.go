package python

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
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
		{Name: "find_author", Command: model.One, SQL: "DECLARE $name AS Optional<Utf8>; SELECT id,name,blob FROM " + table + " WHERE ($name IS NULL OR name=$name);", Parameters: []model.Parameter{{Name: "name", Type: model.Optional(utf8)}}, ResultSets: []model.ResultSet{{Columns: cols}}},
		{Name: "list_authors", Command: model.Many, SQL: "SELECT id,name,blob FROM " + table + " ORDER BY id;", ResultSets: []model.ResultSet{{Columns: cols}}},
		{Name: "delete_author", Command: model.Exec, SQL: "DECLARE $id AS Uint64; DELETE FROM " + table + " WHERE id=$id;", Parameters: []model.Parameter{{Name: "id", Type: id}}},
	}}
}

func TestGenerateProfilesCompile(t *testing.T) {
	for _, runtime := range []string{"ydb", "dbapi", "sqlalchemy"} {
		a := sampleAnalysis()
		a.Queries = a.Queries[:3]
		files, err := Generate(a, Options{Runtime: runtime, EmitSyncQuerier: true})
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

func TestIdenticalTableProjectionsReuseRowModel(t *testing.T) {
	a := liveAnalysis("authors")
	if _, err := Generate(a, Options{Runtime: "dbapi"}); err != nil {
		t.Fatalf("identical table projections should reuse a model: %v", err)
	}
}

func TestSQLAlchemyParameterScannerPreservesLiterals(t *testing.T) {
	a := sampleAnalysis()
	a.Queries = a.Queries[:3]
	files, err := Generate(a, Options{Runtime: "sqlalchemy", EmitSyncQuerier: true})
	if err != nil {
		t.Fatal(err)
	}
	s := string(files[1].Content)
	if !strings.Contains(s, "'$id'") || !strings.Contains(s, "`x`") {
		t.Fatalf("generated SQL lost literal/identifier: %s", s)
	}
}

func TestGeneratedYDBQuerierWithMockAdapter(t *testing.T) {
	a := sampleAnalysis()
	a.Queries = a.Queries[:1]
	files, err := Generate(a, Options{Runtime: "ydb", EmitSyncQuerier: true})
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
class QuerySessionPool: pass
`), 0600); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("import sys; sys.path.insert(0, %q); from db.queries import Querier; import ydb\nclass P:\n def __init__(self): self.calls=[]\n def execute_with_retries(self, sql, parameters):\n  self.calls.append((sql, parameters)); return [ydb.ResultSet([ydb.Row(id=7, display_name=None)])]\np=P(); row=Querier(p).get_author(7); assert row.id == 7 and row.display_name is None; assert p.calls[0][1]['$id'].type == ydb.PrimitiveType.Uint64\n", dir)
	cmd := exec.Command("python3", "-c", script)
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mock YDB execution failed: %v\n%s", err, out)
	}
}

func TestGeneratedSQLAlchemyClosesResultsWithMockAdapter(t *testing.T) {
	a := sampleAnalysis()
	a.Queries = a.Queries[:1]
	a.Queries[0].SQL = "-- name: get_author :one\nDECLARE $id AS Uint64; SELECT '$ghost', `x` FROM authors WHERE id = $id;"
	files, err := Generate(a, Options{Runtime: "sqlalchemy", EmitSyncQuerier: true})
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
from db.queries import Querier, _row_value
assert _row_value((3,), "count", 0) == 3
assert _row_value((4,), "index", 0) == 4
class R:
    def __init__(self, fail): self.closed = False; self.fail = fail
    def fetchall(self):
        if self.fail: raise RuntimeError("fetch failure")
        return [{'id': 7, 'display_name': None}]
    def close(self): self.closed = True
class C:
    def __init__(self): self.calls = []; self.fail = False
    def execute(self, sql, params):
        self.calls.append((sql, params)); self.result = R(self.fail); return self.result
c = C()
row = Querier(c).get_author(7)
assert row.id == 7 and row.display_name is None and c.result.closed
assert c.calls[0][1]['id'][0] == 7
assert r'\:one' in c.calls[0][0] and 'id = :id;' in c.calls[0][0], repr(c.calls[0][0])
c.fail = True
try: Querier(c).get_author(7)
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
	files, err := Generate(a, Options{Runtime: "dbapi", EmitSyncQuerier: true})
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
	script := fmt.Sprintf("import sys; sys.path.insert(0, %q); from db.queries import Querier\nclass Cur:\n rowcount=1\n def execute(self, sql, params): self.params=params\n def fetchall(self): return [{'id': 7, 'display_name': None}]\n def close(self): self.closed=True\nclass C:\n def __init__(self): self.cur=Cur(); self.commits=0\n def cursor(self): return self.cur\nc=C(); row=Querier(c).get_author(7); assert row.id == 7 and c.cur.closed and c.commits == 0 and c.cur.params['$id'][0] == 7\n", dir)
	cmd := exec.Command("python3", "-c", script)
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mock DBAPI execution failed: %v\n%s", err, out)
	}
}

func TestLiveYDBGeneratedRuntimes(t *testing.T) {
	dsn, py := os.Getenv("SQLC_YDB_TEST_DSN"), os.Getenv("SQLC_YDB_TEST_PYTHON")
	if dsn == "" || py == "" {
		t.Skip("set SQLC_YDB_TEST_DSN and SQLC_YDB_TEST_PYTHON to run live YDB adapter validation")
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
		files, err := Generate(a, Options{Runtime: runtime, EmitSyncQuerier: true})
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
dsn = os.environ["SQLC_YDB_TEST_DSN"]
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
    assert y.get_author(1).blob == b"bytes" and y.get_author(99) is None
    assert list(y.list_authors())[0].name == "one"; y.find_author(None); y.find_author("one")
    import ydb_dbapi
    host, port = u.hostname, u.port
    c = ydb_dbapi.connect(host=host, port=port, database=database, protocol=u.scheme)
    from dbapi_generated.queries import Querier as DQuerier
    d = DQuerier(c); d.insert_author(2, "two", b"two"); assert d.get_author(2).blob == b"two"; assert d.get_author(999) is None; c.close()
    import sqlalchemy as sa
    import ydb.sqlalchemy
    e = sa.create_engine("yql+ydb://%%s/%%s" %% (u.netloc, database.lstrip("/")))
    with e.begin() as conn:
        from sa_generated.queries import Querier as SQuerier
        s = SQuerier(conn); s.insert_author(3, "three", b"three"); assert s.get_author(3).name == "three"; assert s.get_author(999) is None
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
	cmd := exec.CommandContext(ctx, py, "-c", script)
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(root, "pycache"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("live YDB validation failed: %v\n%s", err, out)
	}
}
