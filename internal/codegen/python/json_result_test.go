package python

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			pkg := filepath.Join(dir, "generated")
			if err := os.Mkdir(pkg, 0700); err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				if err := os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600); err != nil {
					t.Fatal(err)
				}
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
				t.Fatalf("%s annotations: %v\n%s", runtime, err, out)
			}
		})
	}
}
func TestJSONResultAliasCollision(t *testing.T) {
	in := jsonResultInput()
	in.Catalog.Tables = []model.Table{{Name: "J_S_O_N_Value", Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}}}}
	_, err := Generate(in, Options{Runtime: "ydb"})
	if err == nil || !strings.Contains(err.Error(), "name collision") {
		t.Fatalf("err=%v", err)
	}
}
func TestNativeJSONResultUsesSDKDecodedValues(t *testing.T) {
	if os.Getenv("SQLC_YDB_PYTHON_SDK_CHECK") == "" {
		t.Skip("set SQLC_YDB_PYTHON_SDK_CHECK=1 with pinned Python SDK")
	}
	in := jsonResultInput()
	files, err := Generate(in, Options{Runtime: "ydb"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pkg := filepath.Join(dir, "generated")
	if err := os.Mkdir(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600); err != nil {
			t.Fatal(err)
		}
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
		t.Fatalf("SDK decoded JSON: %v\n%s", err, out)
	}
}
