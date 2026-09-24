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
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func structInput() *model.AnalysisResult {
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "CreateBooks", Command: model.Exec, SQL: "DECLARE $books AS List<Struct<book_id: Uint64, tags: Json, title: Optional<Utf8>>>; INSERT INTO books SELECT * FROM AS_TABLE($books);", Parameters: []model.Parameter{{Name: "books", Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, {Name: "tags", Type: model.Type{Kind: "Json"}}, {Name: "title", Type: model.Optional(model.Type{Kind: "Utf8"})}}}}}}}}}
}
func TestStructListGeneration(t *testing.T) {
	for _, runtime := range []string{"ydb", "dbapi", "sqlalchemy"} {
		files, err := Generate(structInput(), Options{Runtime: runtime})
		require.NoError(t, err)
		source := ""
		for _, f := range files {
			source += string(f.Content)
		}
		for _, want := range []string{"class CreateBooksBooksItem:", "book_id: int", "tags: str", "title: Optional[str]", "books: list[_models.CreateBooksBooksItem]", "                    {\n                        \"book_id\": item.book_id,\n                        \"tags\": item.tags,\n                        \"title\": item.title,\n                    }\n                    for item in books\n                ],", "\"book_id\": item.book_id,\n", "_ydb.ListType(\n                    _ydb.StructType()\n                    .add_member(\"book_id\", _ydb.PrimitiveType.Uint64)"} {
			require.Contains(t, source, want, "%s missing %s\n%s", runtime, want, source)
		}
	}
}
func TestStructListRejectsNestedField(t *testing.T) {
	in := structInput()
	in.Queries[0].Parameters[0].Type.Elem.Fields[0].Type = model.Type{Kind: "List", Elem: &model.Type{Kind: "Uint64"}}
	_, err := Generate(in, Options{})
	require.ErrorContains(t, err, "field book_id must be a supported scalar", "err=%v", err)
}
func TestStructListSDKSerialization(t *testing.T) {
	if os.Getenv("SQLC_YDB_PYTHON_SDK_CHECK") == "" {
		t.Skip("set SQLC_YDB_PYTHON_SDK_CHECK=1 with pinned Python SDK dependencies")
	}
	for _, runtime := range []string{"ydb", "dbapi", "sqlalchemy"} {
		t.Run(runtime, func(t *testing.T) {
			dir := t.TempDir()
			pkg := filepath.Join(dir, "generated")
			require.NoError(t, os.Mkdir(pkg, 0700))
			files, err := Generate(structInput(), Options{Runtime: runtime})
			require.NoError(t, err)
			for _, f := range files {
				require.NoError(t, os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600))
			}
			script := `
import ydb
from ydb import convert
from typing import get_type_hints
from generated.models import CreateBooksBooksItem
from generated.queries import Querier
assert get_type_hints(Querier.create_books)["books"] == list[CreateBooksBooksItem]
class Capture:
    def __init__(self): self.parameters = None; self.closed = False
    def execute(self, sql, parameters): self.parameters = parameters; return self
    def cursor(self): return self
    def close(self): self.closed = True
capture = Capture()
q = Querier.__new__(Querier)
q._connection = capture
q._execute = capture.execute
populated = [CreateBooksBooksItem(2**64-1, '{"a":1}', None), CreateBooksBooksItem(1, '{}', 'Unicode ☀')]
types = []
for items in [[], populated]:
    q.create_books(items)
    typed = next(iter(capture.parameters.values()))
    if isinstance(typed, ydb.TypedValue): value, typ = typed.value, typed.value_type
    else: value, typ = typed
    wire = convert.to_typed_value_from_native(typ.proto, value)
    types.append(wire.type)
    assert len(wire.value.items) == len(items)
    if items:
        assert wire.value.items[0].items[0].uint64_value == 2**64-1
        assert wire.value.items[0].items[1].text_value == '{"a":1}'
        assert wire.type.list_type.item.struct_type.members[1].type.type_id == ydb.PrimitiveType.Json.proto.type_id
        assert wire.value.items[0].items[2].WhichOneof('value') == 'null_flag_value'
        assert wire.value.items[1].items[2].text_value == 'Unicode ☀'
assert types[0] == types[1]
`
			cmd := exec.Command("python3", "-c", script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
			if out, err := cmd.CombinedOutput(); err != nil {
				require.NoError(t, err, "%s SDK serialization: %v\n%s", runtime, err, out)
			}
		})
	}
}

func TestLiveYDBBatchInsertRuntimes(t *testing.T) {
	if os.Getenv("YDB_CONNECTION_STRING") == "" {
		t.Skip("set YDB_CONNECTION_STRING for live batch insert validation")
	}
	table := "sqlc_python_batch_" + fmt.Sprint(time.Now().UnixNano())
	in := structInput()
	in.Queries[0].SQL = "DECLARE $books AS List<Struct<book_id: Uint64, tags: Json, title: Optional<Utf8>>>; INSERT INTO " + table + " (book_id,tags,title) SELECT book_id,tags,title FROM AS_TABLE($books);"
	in.Queries = append(in.Queries, model.AnalyzedQuery{Name: "ListBooks", Command: model.Many, SQL: "SELECT book_id,tags,title FROM " + table + ";", ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, {Name: "tags", Type: model.Type{Kind: "Json"}}, {Name: "title", Type: model.Optional(model.Type{Kind: "Utf8"})}}}}})
	root := t.TempDir()
	for _, runtime := range []string{"ydb", "dbapi", "sqlalchemy"} {
		dir := filepath.Join(root, runtime+"_generated")
		require.NoError(t, os.Mkdir(dir, 0700))
		files, err := Generate(in, Options{Runtime: runtime})
		require.NoError(t, err)
		for _, f := range files {
			require.NoError(t, os.WriteFile(filepath.Join(dir, f.Name), f.Content, 0600))
		}
	}
	script := fmt.Sprintf(`import os, urllib.parse, json
import ydb
import ydb_dbapi
import sqlalchemy as sa
import ydb.sqlalchemy
from ydb_generated.queries import Querier as YQuerier
from ydb_generated.models import CreateBooksBooksItem as YItem
from dbapi_generated.queries import Querier as DQuerier
from dbapi_generated.models import CreateBooksBooksItem as DItem
from sqlalchemy_generated.queries import Querier as SQuerier
from sqlalchemy_generated.models import CreateBooksBooksItem as SItem
u=urllib.parse.urlsplit(os.environ["YDB_CONNECTION_STRING"])
driver=ydb.Driver(ydb.DriverConfig(u.scheme+"://"+u.netloc,u.path,credentials=ydb.AnonymousCredentials(),disable_discovery=True));driver.wait(20)
pool=ydb.QuerySessionPool(driver)
table=%q
pool.execute_with_retries("CREATE TABLE %%s (book_id Uint64 NOT NULL, tags Json NOT NULL, title Utf8, PRIMARY KEY(book_id));" %% table)
def check_empty(query, runtime):
    query.create_books([])
    print(runtime + " empty: accepted")
try:
    y=YQuerier(pool)
    check_empty(y,"native")
    y.create_books([YItem(2**64-1,'{"source":"native"}',None),YItem(1,'[]','Unicode ☀')])
    c=ydb_dbapi.connect(host=u.hostname,port=u.port,database=u.path,protocol=u.scheme)
    try:
        d=DQuerier(c)
        check_empty(d,"dbapi")
        c.rollback()
        d.create_books([DItem(2,'["dbapi"]',None),DItem(3,'{}','Third')])
        c.commit()
        direct_rows={row.book_id:row for row in d.list_books()}
        assert isinstance(direct_rows[2].tags,str) and json.loads(direct_rows[2].tags)==["dbapi"]
    finally:
        c.close()
    engine=sa.create_engine("yql+ydb://%%s/%%s" %% (u.netloc,u.path.lstrip("/")))
    try:
        with engine.connect() as conn:
            check_empty(SQuerier(conn),"sqlalchemy")
            conn.rollback()
        with engine.begin() as conn:
            sq=SQuerier(conn)
            sq.create_books([SItem(4,'["sqlalchemy"]',None),SItem(5,'{}','Fifth')])
            direct_rows={row.book_id:row for row in sq.list_books()}
            assert isinstance(direct_rows[4].tags,str) and json.loads(direct_rows[4].tags)==["sqlalchemy"]
    finally:
        engine.dispose()
    rows={row.book_id:row for row in y.list_books()}
    assert set(rows)=={2**64-1,1,2,3,4,5}, rows
    assert rows[2**64-1].tags=={"source":"native"} and rows[2**64-1].title is None
    assert rows[1].title=='Unicode ☀'
    assert rows[2].tags==["dbapi"] and rows[2].title is None
    assert rows[4].tags==["sqlalchemy"] and rows[4].title is None
    try:
        y.create_books([YItem(1,'{}',None)])
        raise AssertionError("duplicate INSERT accepted")
    except ydb.Error:
        pass
finally:
    pool.execute_with_retries("DROP TABLE IF EXISTS %%s;" %% table)
    driver.stop()
`, table)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(root, "pycache"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "live batch: %v\n%s", err, out)
	t.Log(string(out))
}

func TestStructListDunderFieldsUseSafeNamesAndKeepWireNames(t *testing.T) {
	in := structInput()
	names := []string{"__dict__", "__weakref__", "__init__", "__post_init__", "__annotations__", "self"}
	var fields []model.StructField
	for _, name := range names {
		fields = append(fields, model.StructField{Name: name, Type: model.Type{Kind: "Utf8"}})
	}
	in.Queries[0].Parameters[0].Type.Elem.Fields = fields
	files, err := Generate(in, Options{})
	require.NoError(t, err)
	dir := t.TempDir()
	pkg := filepath.Join(dir, "generated")
	require.NoError(t, os.Mkdir(pkg, 0700))
	for _, f := range files {
		require.NoError(t, os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600))
	}
	script := `import sys, types
class StructType:
    def add_member(self, name, typ): return self
sys.modules["ydb"] = types.SimpleNamespace(StructType=StructType, ListType=lambda typ: typ, PrimitiveType=types.SimpleNamespace(Utf8="Utf8"), TypedValue=lambda value, typ: value)
from generated.models import CreateBooksBooksItem
from generated.queries import Querier
row=CreateBooksBooksItem(dict="dict",weakref="weakref",init="init",post_init="post_init",annotations="annotations",self="self")
assert row.__dict__ == {"dict":"dict","weakref":"weakref","init":"init","post_init":"post_init","annotations":"annotations","self":"self"}
q=Querier.__new__(Querier)
captured=[]
q._execute=lambda sql, parameters: captured.append(parameters)
q.create_books([row])
assert captured == [{"$books":[{"__dict__":"dict","__weakref__":"weakref","__init__":"init","__post_init__":"post_init","__annotations__":"annotations","self":"self"}]}]
`
	cmd := exec.Command("python3", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
	if out, err := cmd.CombinedOutput(); err != nil {
		require.NoError(t, err, "dunder fields: %v\n%s", err, out)
	}
}

func TestStructListNamesCollideAfterNormalization(t *testing.T) {
	for _, names := range [][]string{{"__dict__", "dict"}, {"__weakref__", "weakref"}, {"__init__", "init"}, {"__post_init__", "post_init"}} {
		in := structInput()
		var fields []model.StructField
		for _, name := range names {
			fields = append(fields, model.StructField{Name: name, Type: model.Type{Kind: "Utf8"}})
		}
		in.Queries[0].Parameters[0].Type.Elem.Fields = fields
		_, err := Generate(in, Options{})
		require.ErrorContains(t, err, "colliding field", "%v: %v", names, err)
	}
	in := structInput()
	in.Catalog.Tables = []model.Table{{Name: "create_books_books_item", Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}}}}
	_, err := Generate(in, Options{})
	require.ErrorContains(t, err, "name collision", "class namespace collision: %v", err)
}

func TestStructListFieldDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields []model.StructField
		want   string
	}{
		{"empty", nil, "requires at least one field"},
		{"invalid name", []model.StructField{{Name: "123id", Type: model.Type{Kind: "Uint64"}}}, "invalid or colliding field"},
		{"unsupported scalar", []model.StructField{{Name: "amount", Type: model.Type{Kind: "Decimal", Precision: 22, Scale: 9}}}, "must be a supported scalar"},
		{"nested optional", []model.StructField{{Name: "id", Type: model.Optional(model.Optional(model.Type{Kind: "Uint64"}))}}, "must be a supported scalar"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := structInput()
			in.Queries[0].Parameters[0].Type.Elem.Fields = tc.fields
			_, err := Generate(in, Options{})
			require.ErrorContains(t, err, tc.want, "got %v, want %s", err, tc.want)
		})
	}
}
