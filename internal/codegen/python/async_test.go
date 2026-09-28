package python

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeAsyncQuerier(t *testing.T) {
	a := sampleAnalysis()
	a.Queries = a.Queries[:3]
	files, err := Generate(a, Options{Runtime: "ydb", EmitAsyncQuerier: true})
	require.NoError(t, err)
	queries := string(files[1].Content)
	require.Contains(t, queries, "class Querier:")
	require.Contains(t, queries, "class AsyncQuerier:")
	require.Contains(t, queries, "async def get_author(")
	require.Contains(t, queries, "async def list_authors(")
	require.Contains(t, queries, "async def delete_author(")
	require.Contains(t, queries, "async with await self._executor.execute(query, parameters) as stream:")
	dir := t.TempDir()
	pkg := filepath.Join(dir, "generated")
	require.NoError(t, os.Mkdir(pkg, 0700))
	for _, f := range files {
		require.NoError(t, os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ydb.py"), []byte(`class RetrySettings:
    def __init__(self, max_retries=None): self.max_retries = max_retries
class Type:
    def __init__(self, name): self.proto = name
class PrimitiveType:
    Uint64 = Type("Uint64")
class TypedValue:
    def __init__(self, value, typ): self.value, self.type = value, typ
class Pool:
    def __init__(self, result_sets): self.result_sets, self.calls = result_sets, []
    async def execute_with_retries(self, sql, parameters, retry_settings=None):
        self.calls.append((sql, parameters, retry_settings))
        return self.result_sets
class Tx:
    def __init__(self, result_sets, fail=False): self.result_sets, self.fail, self.closed = result_sets, fail, False
    async def execute(self, sql, parameters): return Stream(self)
class Stream:
    def __init__(self, tx): self.tx = tx
    async def __aenter__(self): return self
    async def __aexit__(self, *args): self.tx.closed = True
    def __aiter__(self): return self.iterate()
    async def iterate(self):
        for result_set in self.tx.result_sets: yield result_set
        if self.tx.fail: raise RuntimeError("late YDB error")
class AIO:
    QuerySessionPool = Pool
    QueryTxContext = Tx
aio = AIO()
`), 0600))
	script := `import asyncio
import ydb
from generated.queries import AsyncQuerier

class ResultSet:
    def __init__(self, rows): self.rows = rows

async def check():
    pool = ydb.aio.QuerySessionPool([ResultSet([{"id": 7, "display_name": None}])])
    q = AsyncQuerier(pool)
    assert q._retry_settings.max_retries == 0
    row = await q.get_author(7)
    assert row.id == 7 and row.display_name is None
    assert pool.calls[0][1]["$id"].value == 7
    pool.result_sets = [ResultSet([])]
    assert await q.get_author(7) is None
    assert await q.list_authors() == []
    pool.result_sets = []
    assert await q.delete_author(7) is None
    tx = ydb.aio.QueryTxContext([ResultSet([{"id": 9, "display_name": "nine"}])])
    assert (await AsyncQuerier(tx).get_author(9)).id == 9
    assert tx.closed
    try: AsyncQuerier(tx, retry_settings=ydb.RetrySettings())
    except ValueError: pass
    else: raise AssertionError("transaction retry policy accepted")
    tx = ydb.aio.QueryTxContext([ResultSet([{"id": 9, "display_name": "nine"}])], fail=True)
    try: await AsyncQuerier(tx).get_author(9)
    except RuntimeError as exc: assert str(exc) == "late YDB error"
    else: raise AssertionError("late YDB error ignored")
    assert tx.closed

asyncio.run(check())
`
	cmd := exec.Command("python3", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
}

func TestNativeAsyncOnlyAndUnsupportedAdapters(t *testing.T) {
	a := sampleAnalysis()
	a.Queries = a.Queries[:3]
	sync := false
	files, err := Generate(a, Options{Runtime: "ydb", EmitSyncQuerier: &sync, EmitAsyncQuerier: true})
	require.NoError(t, err)
	queries := string(files[1].Content)
	require.NotContains(t, queries, "class Querier:")
	require.Contains(t, queries, "class AsyncQuerier:")
	for _, runtime := range []string{"dbapi", "sqlalchemy"} {
		_, err := Generate(a, Options{Runtime: runtime, EmitAsyncQuerier: true})
		require.ErrorContains(t, err, "async querier is unsupported")
	}
	_, err = Generate(a, Options{Runtime: "ydb", EmitSyncQuerier: &sync})
	require.ErrorContains(t, err, "requires emit_sync_querier or emit_async_querier")
}

func TestNativeAsyncMultiResultSets(t *testing.T) {
	sync := false
	files, err := Generate(multiAnalysis(t), Options{Runtime: "ydb", EmitSyncQuerier: &sync, EmitAsyncQuerier: true})
	require.NoError(t, err)
	queries := string(files[1].Content)
	require.Contains(t, queries, "async def fetch_summary(")
	require.Contains(t, queries, "async def bare_literals(")
	dir := t.TempDir()
	pkg := filepath.Join(dir, "generated")
	require.NoError(t, os.Mkdir(pkg, 0700))
	for _, f := range files {
		require.NoError(t, os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ydb.py"), []byte(`class RetrySettings:
    def __init__(self, max_retries=None): self.max_retries = max_retries
class Type:
    def __init__(self, name): self.proto = name
class PrimitiveType:
    Int32 = Type("Int32")
    Bool = Type("Bool")
    Utf8 = Type("Utf8")
class Pool:
    def __init__(self, sets): self.sets = sets
    async def execute_with_retries(self, sql, parameters, retry_settings=None): return self.sets
class AIO:
    QuerySessionPool = Pool
    QueryTxContext = type("Tx", (), {})
aio = AIO()
`), 0600))
	script := `import asyncio
import ydb
from generated.queries import AsyncQuerier

class Column:
    def __init__(self, name, typ): self.name, self.type = name, typ
class ResultSet:
    def __init__(self, name, typ, rows, truncated=False):
        self.columns, self.rows, self.truncated = [Column(name, typ)], rows, truncated

async def check():
    pool = ydb.aio.QuerySessionPool([
        ResultSet("id", "Int32", []),
        ResultSet("enabled", "Bool", [{"enabled": True}]),
        ResultSet("status", "Utf8", [{"status": "ready"}]),
    ])
    q = AsyncQuerier(pool)
    result = await q.fetch_summary()
    assert result.item == [] and result.flags[0].enabled is True
    assert result.result3[0].status == "ready"
    pool.sets = pool.sets[:2]
    try: await q.fetch_summary()
    except ValueError as exc: assert "expected 3 YDB result sets" in str(exc)
    else: raise AssertionError("missing result set accepted")
    pool.sets = [ResultSet("id", "Bool", []), ResultSet("enabled", "Bool", []), ResultSet("status", "Utf8", [])]
    try: await q.fetch_summary()
    except ValueError as exc: assert "schema mismatch" in str(exc)
    else: raise AssertionError("wrong result schema accepted")

asyncio.run(check())
`
	cmd := exec.Command("python3", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
}
