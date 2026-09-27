package python

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func multiAnalysis(t *testing.T) *model.AnalysisResult {
	t.Helper()
	a, err := analyzer.Analyze(nil, []model.Source{{Name: "queries.sql", Text: `-- name: FetchSummary :multi
-- result: Item
SELECT 1 AS id FROM (SELECT 1 AS x) AS source WHERE false;
-- result: Flags
SELECT true AS enabled LIMIT 1;
SELECT "ready"u AS status;

-- name: BareLiterals :multi
SELECT 1;
SELECT "2"u;
SELECT false;`}})
	require.NoError(t, err)
	return a
}

func TestNativeMultiResultSets(t *testing.T) {
	files, err := Generate(multiAnalysis(t), Options{Runtime: "ydb"})
	require.NoError(t, err)
	models, queries := string(files[0].Content), string(files[1].Content)
	require.Contains(t, models, "class FetchSummaryResult:\n    item: list[FetchSummaryItemRow]\n    flags: list[FetchSummaryFlagsRow]\n    result3: list[FetchSummaryResult3Row]")
	require.Contains(t, queries, "if len(result_sets) != 3:")
	dir := t.TempDir()
	pkg := filepath.Join(dir, "generated")
	require.NoError(t, os.Mkdir(pkg, 0700))
	for _, file := range files {
		require.NoError(t, os.WriteFile(filepath.Join(pkg, file.Name), file.Content, 0600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ydb.py"), []byte(`class Type:
    def __init__(self, name): self.proto = name
class PrimitiveType:
    Int32 = Type("Int32")
    Bool = Type("Bool")
    Utf8 = Type("Utf8")
class QuerySessionPool: pass
class QueryTxContext: pass
`), 0600))
	script := `from generated.queries import Querier
from generated.models import FetchSummaryResult

class Column:
    def __init__(self, name, typ): self.name, self.type = name, typ
class ResultSet:
    def __init__(self, name, typ, rows, truncated=False):
        self.columns, self.rows, self.truncated = [Column(name, typ)], rows, truncated

sets = [ResultSet("id", "Int32", []), ResultSet("enabled", "Bool", [{"enabled": True}]), ResultSet("status", "Utf8", [{"status": "ready"}])]
q = Querier.__new__(Querier)
q._execute = lambda sql, params: sets
result = q.fetch_summary()
assert isinstance(result, FetchSummaryResult)
assert result.item == [] and result.flags[0].enabled is True and result.result3[0].status == "ready"

literal_sets = [ResultSet("column0", "Int32", [{"column0": 1}]), ResultSet("column0", "Utf8", [{"column0": "2"}]), ResultSet("column0", "Bool", [{"column0": False}])]
q._execute = lambda sql, params: literal_sets
bare = q.bare_literals()
assert [row.column0 for row in bare.result1] == [1]
assert [row.column0 for row in bare.result2] == ["2"]
assert [row.column0 for row in bare.result3] == [False]

for broken, message in [(sets[:-1], "expected 3 YDB result sets"),
                        (sets + sets[:1], "expected 3 YDB result sets"),
                        ([ResultSet("wrong", "Int32", []), *sets[1:]], "schema mismatch"),
                        ([ResultSet("id", "Bool", []), *sets[1:]], "schema mismatch"),
                        ([ResultSet("id", "Int32", [], True), *sets[1:]], "truncated by the server")]:
    q._execute = lambda sql, params: broken
    try: q.fetch_summary()
    except ValueError as exc: assert message in str(exc), str(exc)
    else: raise AssertionError("invalid result sets accepted")

def late_error():
    yield sets[0]
    raise RuntimeError("late YDB error")
q._execute = lambda sql, params: list(late_error())
try: q.fetch_summary()
except RuntimeError as exc: assert str(exc) == "late YDB error"
else: raise AssertionError("late error ignored")
`
	cmd := exec.Command("python3", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
}

func TestMultiRejectsDBAPIRuntimes(t *testing.T) {
	for _, runtime := range []string{"dbapi", "sqlalchemy"} {
		_, err := Generate(multiAnalysis(t), Options{Runtime: runtime})
		require.ErrorContains(t, err, fmt.Sprintf(":multi requires runtime ydb; %s does not expose separate result sets", runtime))
	}
}

func TestMultiRejectsPythonResultNameCollision(t *testing.T) {
	a := multiAnalysis(t)
	a.Queries[0].ResultSets[0].Name = "FooBar"
	a.Queries[0].ResultSets[1].Name = "Foo_Bar"
	_, err := Generate(a, Options{Runtime: "ydb"})
	require.ErrorContains(t, err, "model name collision")
}

func TestMultiRejectsMalformedPythonResults(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*model.AnalyzedQuery)
		want   string
	}{
		{"one result", func(q *model.AnalyzedQuery) { q.ResultSets = q.ResultSets[:1] }, "requires at least two result sets"},
		{"empty columns", func(q *model.AnalyzedQuery) { q.ResultSets[0].Columns = nil }, "requires at least one column"},
		{"field collision", func(q *model.AnalyzedQuery) { q.ResultSets[1].Name = "Item_" }, "collides at Python field name \"item\""},
		{"receiver name", func(q *model.AnalyzedQuery) { q.ResultSets[0].Name = "Self" }, "invalid generated Python field name \"self\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := multiAnalysis(t)
			tc.change(&a.Queries[0])
			_, err := Generate(a, Options{Runtime: "ydb"})
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestLiveYDBNativeMultiResultSets(t *testing.T) {
	if os.Getenv("YDB_CONNECTION_STRING") == "" {
		t.Skip("set YDB_CONNECTION_STRING to run live YDB adapter validation")
	}
	files, err := Generate(multiAnalysis(t), Options{Runtime: "ydb"})
	require.NoError(t, err)
	dir := t.TempDir()
	pkg := filepath.Join(dir, "generated")
	require.NoError(t, os.Mkdir(pkg, 0700))
	for _, file := range files {
		require.NoError(t, os.WriteFile(filepath.Join(pkg, file.Name), file.Content, 0600))
	}
	script := `import os, urllib.parse
import ydb
from generated.queries import Querier

u = urllib.parse.urlsplit(os.environ["YDB_CONNECTION_STRING"])
driver = ydb.Driver(ydb.DriverConfig(u.scheme + "://" + u.netloc, u.path, credentials=ydb.AnonymousCredentials(), disable_discovery=True))
driver.wait(20)
pool = ydb.QuerySessionPool(driver)
try:
    def check(q):
        value = q.fetch_summary()
        assert value.item == []
        assert [r.enabled for r in value.flags] == [True]
        assert [r.status for r in value.result3] == ["ready"]
    check(Querier(pool))
    pool.retry_tx_sync(lambda tx: check(Querier(tx)))
    bare = Querier(pool).bare_literals()
    assert [row.column0 for row in bare.result1] == [1]
    assert [row.column0 for row in bare.result2] == ["2"]
    assert [row.column0 for row in bare.result3] == [False]
finally:
    driver.stop()
`
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
}
