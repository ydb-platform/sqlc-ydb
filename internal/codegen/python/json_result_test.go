package python

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func jsonResultInput() *model.AnalysisResult {
	json := model.Type{Kind: "Json"}
	document := model.Type{Kind: "JsonDocument"}
	optional := model.Optional(json)
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "ReadJSON", Command: model.One, SQL: "SELECT $document AS document;", Parameters: []model.Parameter{{Name: "document", Type: json}}, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "document", Type: json}, {Name: "optional_document", Type: model.Optional(document)}, {Name: "documents", Type: model.Type{Kind: "List", Elem: &optional}}}}}}}}
}
func TestJSONResultAnnotationsFollowRuntime(t *testing.T) {
	for _, runtime := range []string{"ydb", "dbapi", "sqlalchemy"} {
		t.Run(runtime, func(t *testing.T) {
			files, err := Generate(jsonResultInput(), Options{Runtime: runtime})
			require.NoError(t, err)
			dir := t.TempDir()
			pkg := filepath.Join(dir, "generated")
			require.NoError(t, os.Mkdir(pkg, 0700))
			for _, f := range files {
				require.NoError(t, os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600))
			}
			script := `import sys, types
sys.modules["ydb"]=types.ModuleType("ydb")
sys.modules["sqlalchemy"]=types.SimpleNamespace(text=lambda value:value)
sys.modules["sqlalchemy.engine"]=types.SimpleNamespace(Connection=object)
from typing import get_type_hints, get_args, get_origin, Optional
from generated import models
from generated.queries import Querier
assert get_type_hints(Querier.read_j_s_o_n)["document"] is str
`
			if runtime == "ydb" {
				script += `# Python 3.12+ resolves the recursive strings inside builtin generic aliases;
# Python 3.9 leaves them unchanged. Compare raw annotations to the alias and
# check the resolved wrapper relationships rather than alias object equality.
assert models.ReadJSONRow.__annotations__=={"document":models.JSONValue,"optional_document":Optional[models.JSONValue],"documents":list[Optional[models.JSONValue]]}
branches=get_args(models.JSONValue)
assert set(branch for branch in branches if get_origin(branch) is None)=={type(None),bool,int,float,str}
assert [get_args(branch) for branch in branches if get_origin(branch) is list]==[("JSONValue",)]
assert [get_args(branch) for branch in branches if get_origin(branch) is dict]==[(str,"JSONValue")]
hints=get_type_hints(models.ReadJSONRow)
assert hints["optional_document"]==hints["document"]
assert get_origin(hints["documents"]) is list
assert get_args(hints["documents"])[0]==hints["document"]
row=models.ReadJSONRow({"object":[1, True, None]},None,[{"nested":1},None])
assert row.document=={"object":[1,True,None]} and row.documents==[{"nested":1},None]
`
			} else {
				script += `assert get_type_hints(models.ReadJSONRow)=={"document":str,"optional_document":Optional[str],"documents":list[Optional[str]]}
assert not hasattr(models,"JSONValue")
`
			}
			cmd := exec.Command("python3", "-c", script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
			if out, err := cmd.CombinedOutput(); err != nil {
				require.NoError(t, err, "%s annotations: %v\n%s", runtime, err, out)
			}
		})
	}
}
func TestJSONResultAliasCollision(t *testing.T) {
	in := jsonResultInput()
	in.Catalog.Tables = []model.Table{{Name: "J_S_O_N_Value", Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}}}}
	_, err := Generate(in, Options{Runtime: "ydb"})
	require.ErrorContains(t, err, "name collision", "err=%v", err)
}
func TestNativeJSONResultUsesSDKDecodedValues(t *testing.T) {
	if os.Getenv("SQLC_YDB_PYTHON_SDK_CHECK") == "" {
		t.Skip("set SQLC_YDB_PYTHON_SDK_CHECK=1 with pinned Python SDK")
	}
	in := jsonResultInput()
	files, err := Generate(in, Options{Runtime: "ydb"})
	require.NoError(t, err)
	dir := t.TempDir()
	pkg := filepath.Join(dir, "generated")
	require.NoError(t, os.Mkdir(pkg, 0700))
	for _, f := range files {
		require.NoError(t, os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600))
	}
	script := `import types
import ydb
from ydb import convert
from ydb.query.base import QueryClientSettings
from generated.queries import Querier
q=Querier.__new__(Querier)
for text, expected in [('{}',{}), ('[1,true,null]',[1,True,None]), ('"hello"','hello'), ('42',42), ('false',False), ('null',None)]:
    wire=convert.to_typed_value_from_native(ydb.PrimitiveType.Json.proto,text)
    decoded=convert._to_native_value(wire.type,wire.value,QueryClientSettings())
    assert decoded==expected
    q._execute=lambda sql,parameters:[types.SimpleNamespace(rows=[{"document":decoded,"optional_document":None,"documents":[decoded,None]}])]
    row=q.read_j_s_o_n(text)
    assert row.document==expected and row.documents==[expected,None]
    raw=convert._to_native_value(wire.type,wire.value,QueryClientSettings().with_native_json_in_result_sets(False))
    assert raw==text
`
	cmd := exec.Command("python3", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "pycache"))
	if out, err := cmd.CombinedOutput(); err != nil {
		require.NoError(t, err, "SDK decoded JSON: %v\n%s", err, out)
	}
}

func TestNestedJSONResultContainers(t *testing.T) {
	json := model.Type{Kind: "JsonDocument"}
	key := model.Type{Kind: "Utf8"}
	set := model.Type{Kind: "Set", Elem: &json}
	dict := model.Type{Kind: "Dict", Key: &key, Elem: &set}
	for _, runtime := range []string{"ydb", "dbapi", "sqlalchemy"} {
		want := "Optional[dict[str, set[str]]]"
		if runtime == "ydb" {
			want = "Optional[dict[str, set[JSONValue]]]"
		}
		got, err := resultPyType(model.Optional(dict), Options{Runtime: runtime})
		require.False(t, err != nil || got != want, "%s: type=%q, err=%v", runtime, got, err)
	}
}

func TestNativeJSONTableModelAnnotation(t *testing.T) {
	in := &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: "documents", Columns: []model.Column{{Name: "payload", Type: model.Type{Kind: "Json"}}}}}}}
	files, err := Generate(in, Options{Runtime: "ydb"})
	require.NoError(t, err)
	for _, f := range files {
		if f.Name == "models.py" {
			require.False(t, !strings.Contains(string(f.Content), "payload: JSONValue") || !strings.Contains(string(f.Content), "JSONValue = Union["), "missing table JSON annotation:\n%s", f.Content)
			return
		}
	}
	require.FailNow(t, "missing models.py")
}

func TestJSONInsideUnsupportedResultShapesIsRejected(t *testing.T) {
	json := model.Type{Kind: "Json"}
	for _, typ := range []model.Type{{Kind: "Struct", Fields: []model.StructField{{Name: "payload", Type: json}}}, {Kind: "Tuple", Items: []model.Type{json}}} {
		for _, runtime := range []string{"ydb", "dbapi", "sqlalchemy"} {
			in := jsonResultInput()
			in.Queries[0].ResultSets[0].Columns = []model.Column{{Name: "payload", Type: typ}}
			files, err := Generate(in, Options{Runtime: runtime})
			require.False(t, err == nil || !strings.Contains(err.Error(), "unsupported YQL type") || len(files) != 0, "%s %s: files=%d err=%v", runtime, typ.Kind, len(files), err)
		}
	}
}
