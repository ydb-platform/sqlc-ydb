package csharp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func batchAnalysis() *model.AnalysisResult {
	row := model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, {Name: "tags", Type: model.Type{Kind: "Json"}}, {Name: "available", Type: model.Type{Kind: "Timestamp"}}, {Name: "label", Type: model.Optional(model.Type{Kind: "Utf8"})}}}
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "CreateBooks", Command: model.Exec, SQL: "INSERT INTO books SELECT * FROM AS_TABLE($books);", Parameters: []model.Parameter{{Name: "books", Type: model.Type{Kind: "List", Elem: &row}}}}}}
}
func TestStructListParameters(t *testing.T) {
	for _, runtime := range []string{"adonet", "dapper"} {
		models, queries := generatedRuntime(t, batchAnalysis(), runtime)
		if !strings.Contains(models, "record CreateBooksBooksItem") || !strings.Contains(queries, "IReadOnlyList<CreateBooksBooksItem> books") {
			t.Fatal(models, queries)
		}
	}
}
func TestStructListPublishedSDK(t *testing.T) {
	dotnet := os.Getenv("SQLC_YDB_CSHARP_DOTNET")
	if dotnet == "" {
		t.Skip("set SQLC_YDB_CSHARP_DOTNET for SDK wire tests")
	}
	dir := t.TempDir()
	for _, runtime := range []string{"adonet", "dapper"} {
		files, err := Generate(batchAnalysis(), Options{Namespace: runtime, Runtime: runtime})
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(dir, runtime+f.Name), f.Content, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	project := `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework><Nullable>enable</Nullable><TreatWarningsAsErrors>true</TreatWarningsAsErrors></PropertyGroup><ItemGroup><PackageReference Include="Ydb.Sdk" Version="0.35.0" /><PackageReference Include="Dapper" Version="2.1.79" /></ItemGroup></Project>`
	program := `using System;
using System.Reflection;
using Ydb.Sdk.Value;
class Program {
 static void Check(System.Type type, object items, int count) {
  var bind=type.GetMethod("BindCreateBooksBooksItem",BindingFlags.NonPublic|BindingFlags.Static)!;
  var value=(YdbValue)bind.Invoke(null,new[]{items})!;
  var proto=value.GetProto();
  if(proto.Type.ListType.Item.StructType.Members.Count!=4 || proto.Value.Items.Count!=count) throw new Exception("list structure");
  var members=proto.Type.ListType.Item.StructType.Members;
  if(members[0].Name!="book_id" || members[0].Type.TypeId!=Ydb.Type.Types.PrimitiveTypeId.Uint64 || members[1].Type.TypeId!=Ydb.Type.Types.PrimitiveTypeId.Json || members[3].Type.OptionalType.Item.TypeId!=Ydb.Type.Types.PrimitiveTypeId.Utf8) throw new Exception("member schema");
  if(count>0 && (proto.Value.Items[0].Items[0].Uint64Value!=ulong.MaxValue || proto.Value.Items[0].Items[1].TextValue!="[]" || proto.Value.Items[0].Items[3].ValueCase!=Ydb.Value.ValueOneofCase.NullFlagValue)) throw new Exception("member values");
 }
 static void Main(){
  Check(typeof(adonet.Queries),Array.Empty<adonet.CreateBooksBooksItem>(),0);
  Check(typeof(dapper.Queries),Array.Empty<dapper.CreateBooksBooksItem>(),0);
  Check(typeof(adonet.Queries),new[]{new adonet.CreateBooksBooksItem(ulong.MaxValue,"[]",DateTime.UnixEpoch,null)},1);
  Check(typeof(dapper.Queries),new[]{new dapper.CreateBooksBooksItem(ulong.MaxValue,"[]",DateTime.UnixEpoch,null),new dapper.CreateBooksBooksItem(1,"{}",DateTime.UnixEpoch,"present")},2);
 }
}`
	for name, content := range map[string]string{"wire.csproj": project, "Program.cs": program} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(dotnet, "run", "--project", "wire.csproj", "--nologo")
	cmd.Dir = dir
	cmd.Env = dotnetEnv(dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("SDK wire test: %v\n%s", err, out)
	}
}

func TestStructListRejectsSDKTypeNameCollision(t *testing.T) {
	analysis := batchAnalysis()
	analysis.Catalog.Tables = []model.Table{{Name: "ydbTypeId", Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}}}}
	for _, runtime := range []string{"adonet", "dapper"} {
		_, err := Generate(analysis, Options{Runtime: runtime})
		if err == nil || !strings.Contains(err.Error(), "model name collision") {
			t.Fatalf("%s: expected SDK type name collision, got %v", runtime, err)
		}
	}
}
