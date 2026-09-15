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
		in := batchAnalysis()
		in.Queries[0].Parameters = append(in.Queries[0].Parameters, model.Parameter{Name: "limit", Type: model.Type{Kind: "Uint64"}})
		files, err := Generate(in, Options{Namespace: runtime, Runtime: runtime})
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

func TestStructListRejectsUnsupportedFieldShapes(t *testing.T) {
	for _, runtime := range []string{"adonet", "dapper"} {
		for _, tc := range []struct {
			name   string
			fields []model.StructField
			want   string
		}{
			{"empty", nil, "at least one field"},
			{"nested struct", []model.StructField{{Name: "nested", Type: model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "id", Type: model.Type{Kind: "Uint64"}}}}}}, `column "nested": unsupported YQL type "Struct"`},
			{"nested list", []model.StructField{{Name: "items", Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Utf8"}}}}, `column "items": unsupported YQL type "List"`},
		} {
			t.Run(runtime+"/"+tc.name, func(t *testing.T) {
				in := batchAnalysis()
				in.Queries[0].Parameters[0].Type.Elem.Fields = tc.fields
				_, err := Generate(in, Options{Runtime: runtime})
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("got %v, want diagnostic %q", err, tc.want)
				}
			})
		}
	}
}

func TestStructListItemNamesAreValidated(t *testing.T) {
	for _, runtime := range []string{"adonet", "dapper"} {
		t.Run(runtime+"/table collision", func(t *testing.T) {
			in := batchAnalysis()
			in.Catalog.Tables = []model.Table{{Name: "create_books_books_item"}}
			if _, err := Generate(in, Options{Runtime: runtime}); err == nil || !strings.Contains(err.Error(), "model name collision") {
				t.Fatalf("got %v", err)
			}
		})
		t.Run(runtime+"/field collision", func(t *testing.T) {
			in := batchAnalysis()
			in.Queries[0].Parameters[0].Type.Elem.Fields = []model.StructField{{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, {Name: "book_ID", Type: model.Type{Kind: "Uint64"}}}
			if _, err := Generate(in, Options{Runtime: runtime}); err == nil || !strings.Contains(err.Error(), "column name collision") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestStructListAndScalarParameters(t *testing.T) {
	for _, runtime := range []string{"adonet", "dapper"} {
		in := batchAnalysis()
		in.Queries[0].Parameters = append(in.Queries[0].Parameters, model.Parameter{Name: "limit", Type: model.Type{Kind: "Uint64"}})
		models, queries := generatedRuntime(t, in, runtime)
		for _, want := range []string{"record CreateBooksParams", "IReadOnlyList<CreateBooksBooksItem> Books", "ulong Limit"} {
			if !strings.Contains(models, want) {
				t.Fatalf("missing %q in %s", want, models)
			}
		}
		if !strings.Contains(queries, "BindCreateBooksBooksItem(args.Books)") {
			t.Fatal(queries)
		}
	}
}
