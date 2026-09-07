package csharp

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

func authorsAnalysis() *model.AnalysisResult {
	utf8 := model.Type{Kind: "Utf8"}
	return &model.AnalysisResult{
		Catalog: model.Catalog{Tables: []model.Table{{Name: "authors", Columns: []model.Column{
			{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: utf8}, {Name: "bio", Type: model.Optional(utf8)},
		}}}},
		Queries: []model.AnalyzedQuery{
			{Name: "GetAuthor", Command: model.One, SQL: "DECLARE $author_id AS Uint64;\nSELECT id, name, bio FROM authors WHERE id = $author_id;\n", Parameters: []model.Parameter{{Name: "author_id", Type: model.Type{Kind: "Uint64"}}}, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: utf8}, {Name: "bio", Type: model.Optional(utf8)}}}}},
			{Name: "ListAuthors", Command: model.Many, SQL: "SELECT id, name, bio FROM authors;", ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: utf8}, {Name: "bio", Type: model.Optional(utf8)}}}}},
			{Name: "UpsertAuthor", Command: model.Exec, SQL: "DECLARE $author_id AS Uint64; DECLARE $author_name AS Utf8; DECLARE $biography AS Optional<Utf8>; UPSERT INTO authors VALUES ($author_id, $author_name, $biography);", Parameters: []model.Parameter{{Name: "author_id", Type: model.Type{Kind: "Uint64"}}, {Name: "author_name", Type: utf8}, {Name: "biography", Type: model.Optional(utf8)}}},
		},
	}
}

func generated(t *testing.T, a *model.AnalysisResult) (string, string) {
	t.Helper()
	files, err := Generate(a, Options{Namespace: "Authors.AdoNet"})
	if err != nil {
		t.Fatal(err)
	}
	return string(files[0].Content), string(files[1].Content)
}

func TestGenerateUsesConcreteModernYdbAdoSurface(t *testing.T) {
	models, queries := generated(t, authorsAnalysis())
	for _, want := range []string{
		"namespace Authors.AdoNet;", "public sealed record Authors(", "public sealed record GetAuthorRow(", "string? Bio", "public sealed record UpsertAuthorParams(",
	} {
		if !strings.Contains(models, want) {
			t.Errorf("Models.cs missing %q:\n%s", want, models)
		}
	}
	for _, want := range []string{
		"using Ydb.Sdk.Ado;", "private readonly YdbConnection _connection;", "private readonly YdbTransaction? _transaction;", "WithTransaction(YdbTransaction transaction)",
		"new YdbCommand(SqlGetAuthor, _connection) { Transaction = _transaction }", "new YdbParameter(\"$author_id\", DbType.UInt64, AuthorID)",
		"new YdbParameter(\"$biography\", DbType.String, (object?)args.Biography ?? DBNull.Value)", "ExecuteReaderAsync(cancellationToken)", "ReadAsync(cancellationToken)", "reader.IsDBNull(2) ? null : reader.GetFieldValue<string>(2)",
	} {
		if !strings.Contains(queries, want) {
			t.Errorf("Queries.cs missing %q:\n%s", want, queries)
		}
	}
	if strings.Contains(queries, "YdbDataSource") || strings.Contains(queries, "TableClient") || strings.Contains(queries, "DbConnection") {
		t.Fatalf("generator must not own a data source or use legacy/generic surface:\n%s", queries)
	}
}

func TestSQLLiteralPreservesControlsQuotesAndBackslashes(t *testing.T) {
	a := authorsAnalysis()
	sql := "SELECT '\"', '\\\\', '" + string(rune(0)) + "', '" + string(rune(0x1f)) + "';\r\n-- \"\"\" delimiter-looking text\n"
	a.Queries = a.Queries[:1]
	a.Queries[0].SQL = sql
	_, queries := generated(t, a)
	for _, want := range []string{`"SELECT '\"', '\\\\', '\u0000', '\u001F';\r\n" +`, `"-- \"\"\" delimiter-looking text\n";`} {
		if !strings.Contains(queries, want) {
			t.Errorf("SQL literal did not use portable exact escaping %q:\n%s", want, queries)
		}
	}
}

func TestRejectsUnsupportedOrCollidingInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   *model.AnalysisResult
		opts Options
		want string
	}{
		{"list", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.Exec, Parameters: []model.Parameter{{Name: "x", Type: model.Type{Kind: "List"}}}}}}, Options{}, "unsupported YQL type"},
		{"execrows", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.ExecRows}}}, Options{}, "unsupported command"},
		{"field collision", &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: "t", Columns: []model.Column{{Name: "a_b", Type: model.Type{Kind: "Utf8"}}, {Name: "a b", Type: model.Type{Kind: "Utf8"}}}}}}}, Options{}, "column name collision"},
		{"generated type collision", &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: "queries"}}}}, Options{}, "model name collision"},
		{"namespace", authorsAnalysis(), Options{Namespace: "Bad.class"}, "invalid namespace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Generate(tc.in, tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Generate() error = %v, want %q", err, tc.want)
			}
		})
	}
}

// This opt-in check validates the generated source against the published SDK,
// rather than a mock provider. It is opt-in because contributors may not have
// the .NET SDK installed. Example:
// SQLC_YDB_CSHARP_DOTNET=/path/to/dotnet go test ./internal/codegen/csharp -run Published
func TestGeneratedCodeBuildsAgainstPublishedSDK(t *testing.T) {
	dotnet := os.Getenv("SQLC_YDB_CSHARP_DOTNET")
	if dotnet == "" {
		t.Skip("set SQLC_YDB_CSHARP_DOTNET to run the published-SDK build")
	}
	files, err := Generate(authorsAnalysis(), Options{Namespace: "Authors.AdoNet"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(dir, file.Name), file.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	project := `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework><Nullable>enable</Nullable><TreatWarningsAsErrors>true</TreatWarningsAsErrors></PropertyGroup><ItemGroup><PackageReference Include="Ydb.Sdk" Version="0.33.3" /></ItemGroup></Project>`
	if err := os.WriteFile(filepath.Join(dir, "generated.csproj"), []byte(project), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dotnet, "build", "--nologo")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOTNET_CLI_HOME="+filepath.Join(dir, ".dotnet"), "NUGET_PACKAGES="+filepath.Join(dir, ".nuget"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated C# does not build against Ydb.Sdk 0.33.3: %v\n%s", err, out)
	}
}

func TestAllSupportedScalarsBuildAgainstPublishedSDK(t *testing.T) {
	dotnet := os.Getenv("SQLC_YDB_CSHARP_DOTNET")
	if dotnet == "" {
		t.Skip("set SQLC_YDB_CSHARP_DOTNET to run the published-SDK build")
	}
	types := []model.Type{
		{Kind: "Bool"}, {Kind: "Int8"}, {Kind: "Int16"}, {Kind: "Int32"}, {Kind: "Int64"},
		{Kind: "Uint8"}, {Kind: "Uint16"}, {Kind: "Uint32"}, {Kind: "Uint64"},
		{Kind: "Float"}, {Kind: "Double"}, {Kind: "Utf8"}, {Kind: "String"}, {Kind: "Uuid"},
	}
	var parameters []model.Parameter
	var columns []model.Column
	for _, typ := range types {
		parameters = append(parameters, model.Parameter{Name: strings.ToLower(typ.Kind), Type: typ})
		parameters = append(parameters, model.Parameter{Name: "optional_" + strings.ToLower(typ.Kind), Type: model.Optional(typ)})
		columns = append(columns, model.Column{Name: strings.ToLower(typ.Kind), Type: typ})
		columns = append(columns, model.Column{Name: "optional_" + strings.ToLower(typ.Kind), Type: model.Optional(typ)})
	}
	in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name:       "AllScalars",
		Command:    model.One,
		SQL:        "SELECT 1;",
		Parameters: parameters,
		ResultSets: []model.ResultSet{{Columns: columns}},
	}}}
	files, err := Generate(in, Options{Namespace: "Scalars"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(dir, file.Name), file.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	project := `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework><Nullable>enable</Nullable><TreatWarningsAsErrors>true</TreatWarningsAsErrors></PropertyGroup><ItemGroup><PackageReference Include="Ydb.Sdk" Version="0.33.3" /></ItemGroup></Project>`
	if err := os.WriteFile(filepath.Join(dir, "scalars.csproj"), []byte(project), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dotnet, "build", "--nologo")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOTNET_CLI_HOME="+filepath.Join(dir, ".dotnet"), "NUGET_PACKAGES="+filepath.Join(dir, ".nuget"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("all generated scalar bindings must compile against Ydb.Sdk 0.33.3: %v\n%s", err, out)
	}
}

func TestRejectsModelNamesThatShadowFrameworkTypes(t *testing.T) {
	for _, table := range []string{"guid", "task", "cancellation_token"} {
		t.Run(table, func(t *testing.T) {
			_, err := Generate(&model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: table}}}}, Options{})
			if err == nil || !strings.Contains(err.Error(), "model name collision") {
				t.Fatalf("Generate() error = %v, want framework model-name collision", err)
			}
		})
	}
}

// This is an execution check, not merely a source inspection: C# evaluates the
// emitted literal and compares its UTF-8 bytes with the original SQL.
func TestSQLLiteralRoundTripsThroughCSharpRuntime(t *testing.T) {
	dotnet := os.Getenv("SQLC_YDB_CSHARP_DOTNET")
	if dotnet == "" {
		t.Skip("set SQLC_YDB_CSHARP_DOTNET to run the C# SQL-literal check")
	}
	sql := "SELECT '\"', '\\\\', '" + string(rune(0)) + "', '" + string(rune(0x1f)) + "';\r\n-- \"\"\" delimiter-looking text\n"
	in := authorsAnalysis()
	in.Queries = in.Queries[:1]
	in.Queries[0].SQL = sql
	files, err := Generate(in, Options{Namespace: "Authors.AdoNet"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(dir, file.Name), file.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	project := `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework><Nullable>enable</Nullable><TreatWarningsAsErrors>true</TreatWarningsAsErrors></PropertyGroup><ItemGroup><PackageReference Include="Ydb.Sdk" Version="0.33.3" /></ItemGroup></Project>`
	if err := os.WriteFile(filepath.Join(dir, "literal.csproj"), []byte(project), 0600); err != nil {
		t.Fatal(err)
	}
	program := fmt.Sprintf(`using System; using System.Reflection; using System.Text; using Authors.AdoNet; internal static class Program { static int Main() { var actual = (string)typeof(Queries).GetField("SqlGetAuthor", BindingFlags.Static | BindingFlags.NonPublic)!.GetValue(null)!; if (Convert.ToBase64String(Encoding.UTF8.GetBytes(actual)) != %q) throw new Exception("SQL changed"); return 0; } }`, base64.StdEncoding.EncodeToString([]byte(sql)))
	if err := os.WriteFile(filepath.Join(dir, "Program.cs"), []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "DOTNET_CLI_HOME="+filepath.Join(dir, ".dotnet"), "NUGET_PACKAGES="+filepath.Join(dir, ".nuget"))
	build := exec.Command(dotnet, "build", "--nologo")
	build.Dir, build.Env = dir, env
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("literal build: %v\n%s", err, out)
	}
	run := exec.Command(dotnet, "run", "--no-build", "--nologo")
	run.Dir, run.Env = dir, env
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("literal runtime: %v\n%s", err, out)
	}
}
