package csharp

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
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
	require.NoError(t, err)
	return string(files[0].Content), string(files[1].Content)
}

func generatedRuntime(t *testing.T, a *model.AnalysisResult, runtime string) (string, string) {
	t.Helper()
	files, err := Generate(a, Options{Namespace: "Authors." + csName(runtime), Runtime: runtime})
	require.NoError(t, err)
	return string(files[0].Content), string(files[1].Content)
}

func TestGenerateUsesConcreteModernYdbAdoSurface(t *testing.T) {
	models, queries := generated(t, authorsAnalysis())
	for _, want := range []string{
		"namespace Authors.AdoNet;", "public sealed record Authors(", "public sealed record GetAuthorRow(", "string? Bio", "public sealed record UpsertAuthorParams(",
	} {
		assert.Contains(t, models, want, "Models.cs missing %q:\n%s", want, models)
	}
	for _, want := range []string{
		"using Ydb.Sdk.Ado;", "private readonly YdbConnection _connection;", "private readonly YdbTransaction? _transaction;", "WithTransaction(YdbTransaction transaction)",
		"new YdbCommand(" + sqlLiteral(authorsAnalysis().Queries[0].SQL) + ", _connection) { Transaction = _transaction }", "new YdbParameter(\"$author_id\", DbType.UInt64, authorId)",
		"using Ydb.Sdk.Value;", "new YdbParameter(\"$biography\", YdbValue.MakeOptionalUtf8(args.Biography))", "ExecuteReaderAsync(cancellationToken)", "ReadAsync(cancellationToken)", "reader.IsDBNull(2) ? null : reader.GetFieldValue<string>(2)",
	} {
		assert.Contains(t, queries, want, "Queries.cs missing %q:\n%s", want, queries)
	}
	require.False(t, strings.Contains(queries, "YdbDataSource") || strings.Contains(queries, "TableClient") || strings.Contains(queries, "DbConnection"), "generator must not own a data source or use legacy/generic surface:\n%s", queries)
}

func TestGenerateDapperProfileUsesDapperExecutionAndTypedYdbParameters(t *testing.T) {
	_, queries := generatedRuntime(t, authorsAnalysis(), "dapper")
	for _, want := range []string{
		"using Dapper;", "commandText: " + dapperSQLLiteral(authorsAnalysis().Queries[0].SQL), "_connection.QueryFirstAsync<GetAuthorRow>(command)",
		"_connection.ExecuteAsync(command)", "SqlMapper.IDynamicParameters", "command.Parameters.Add(parameter)",
		"new YdbParameter(\"$biography\", YdbValue.MakeOptionalUtf8(args.Biography))",
	} {
		assert.Contains(t, queries, want, "Dapper Queries.cs missing %q:\n%s", want, queries)
	}
}

func TestEmbeddedResultBuildsNestedRecordsInBothCSharpProfiles(t *testing.T) {
	u64 := model.Type{Kind: "Uint64"}
	utf8 := model.Type{Kind: "Utf8"}
	a := &model.AnalysisResult{
		Catalog: model.Catalog{Tables: []model.Table{
			{Name: "books", Columns: []model.Column{{Name: "book_id", Type: u64}, {Name: "author_id", Type: u64}}},
			{Name: "authors", Columns: []model.Column{{Name: "author_id", Type: u64}, {Name: "name", Type: utf8}}},
		}},
		Queries: []model.AnalyzedQuery{{Name: "GetBookAndAuthor", Command: model.One, SQL: "SELECT 1;", ResultSets: []model.ResultSet{{
			Columns: []model.Column{{Name: "book_id", WireName: "book_id_1", Type: u64}, {Name: "author_id", WireName: "author_id_1", Type: u64}, {Name: "author_id", WireName: "author_id_2", Type: u64}, {Name: "name", WireName: "name_2", Type: utf8}},
			Embeds:  []model.Embedding{{Start: 0, End: 2, Table: "books", Field: "books"}, {Start: 2, End: 4, Table: "authors", Field: "authors"}},
		}}}, {Name: "ListBookAndAuthor", Command: model.Many, SQL: "SELECT 1;", ResultSets: []model.ResultSet{{
			Columns: []model.Column{{Name: "label", Type: utf8}, {Name: "book_id", WireName: "book_id_1", Type: u64}, {Name: "author_id", WireName: "author_id_1", Type: u64}, {Name: "rank", Type: model.Type{Kind: "Int32"}}, {Name: "author_id", WireName: "author_id_2", Type: u64}, {Name: "name", WireName: "name_2", Type: utf8}, {Name: "active", Type: model.Type{Kind: "Bool"}}},
			Embeds:  []model.Embedding{{Start: 1, End: 3, Table: "books", Field: "books"}, {Start: 4, End: 6, Table: "authors", Field: "authors"}},
		}}}},
	}
	for _, runtime := range []string{"adonet", "dapper"} {
		t.Run(runtime, func(t *testing.T) {
			models, queries := generatedRuntime(t, a, runtime)
			require.Contains(t, models, "public sealed record GetBookAndAuthorRow(\n    Books Books,\n    Authors Authors\n);")
			require.NotContains(t, models, "public sealed record GetBookAndAuthorRow(\n    ulong")
			require.Contains(t, queries, "new Books(")
			require.Contains(t, queries, "new Authors(")
			require.Contains(t, models, "public sealed record ListBookAndAuthorRow(\n    string Label,\n    Books Books,\n    int Rank,\n    Authors Authors,\n    bool Active\n);")
			if runtime == "dapper" {
				require.Contains(t, queries, "private sealed record GetBookAndAuthorWireRow(")
				require.Contains(t, queries, `["author_id_2"] = nameof(GetBookAndAuthorWireRow.Column2)`)
				require.Contains(t, queries, "QueryFirstAsync<GetBookAndAuthorWireRow>(command)")
				require.Contains(t, queries, "row.Column2")
				require.Contains(t, queries, "QueryAsync<ListBookAndAuthorWireRow>(command).ConfigureAwait(false)).Select(ListBookAndAuthorRowFromWire).ToList()")
				require.Contains(t, queries, "row.Column0,\n        new Books(\n            row.Column1,\n            row.Column2\n        ),\n        row.Column3,\n        new Authors(\n            row.Column4,\n            row.Column5\n        ),\n        row.Column6\n")
			} else {
				require.Contains(t, queries, "reader.GetFieldValue<ulong>(2)")
				require.Contains(t, queries, "reader.GetFieldValue<bool>(6)")
			}
		})
	}
	a.Queries[0].ResultSets[0].Embeds[1].Field = "books"
	_, err := Generate(a, Options{})
	require.ErrorContains(t, err, "column name collision")
}

func TestJsonAndTimestampUseRealSDKTypesInEveryRuntime(t *testing.T) {
	jsonType := model.Type{Kind: "Json"}
	timestampType := model.Type{Kind: "Timestamp"}
	in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "Write", Command: model.One, SQL: "SELECT $json, $when;",
		Parameters: []model.Parameter{{Name: "json", Type: jsonType}, {Name: "when", Type: model.Optional(timestampType)}},
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "json", Type: model.Optional(jsonType)}, {Name: "when", Type: timestampType}}}},
	}}}
	for _, runtime := range []string{"adonet", "dapper"} {
		t.Run(runtime, func(t *testing.T) {
			models, queries := generatedRuntime(t, in, runtime)
			for _, want := range []string{"string? Json", "DateTime When"} {
				assert.Contains(t, models, want, "Models.cs missing %q:\n%s", want, models)
			}
			for _, want := range []string{"YdbValue.MakeJson(args.Json)", "YdbValue.MakeOptionalTimestamp(NormalizeTimestamp(args.When))"} {
				assert.Contains(t, queries, want, "Queries.cs missing %q:\n%s", want, queries)
			}
		})
	}
}

func TestSQLLiteralPreservesControlsQuotesAndBackslashes(t *testing.T) {
	a := authorsAnalysis()
	sql := declaredBatchSQL + "SELECT '\"', '\\\\', '" + string(rune(0)) + "', '" + string(rune(0x1f)) + "';\r\n-- \"\"\" delimiter-looking text\n"
	a.Queries = a.Queries[:1]
	a.Queries[0].SQL = sql
	_, queries := generated(t, a)
	for _, want := range []string{`"SELECT '\"', '\\\\', '\u0000', '\u001F';\r\n" +`, `"-- \"\"\" delimiter-looking text\n"`} {
		assert.Contains(t, queries, want, "SQL literal did not use portable exact escaping %q:\n%s", want, queries)
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
		{"duplicate query", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Same", Command: model.Exec}, {Name: "Same", Command: model.Exec}}}, Options{}, "method name collision"},
		{"unrepresentable table name", &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: "---"}}}}, Options{}, "invalid model name"},
		{"namespace", authorsAnalysis(), Options{Namespace: "Bad.class"}, "invalid namespace"},
		{"runtime", authorsAnalysis(), Options{Runtime: "entity-framework"}, "unsupported runtime"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Generate(tc.in, tc.opts)
			require.ErrorContains(t, err, tc.want, "Generate() error = %v, want %q", err, tc.want)
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
	require.NoError(t, err)
	dir := t.TempDir()
	for _, file := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file.Name), file.Content, 0600))
	}
	project := `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework><Nullable>enable</Nullable><TreatWarningsAsErrors>true</TreatWarningsAsErrors></PropertyGroup><ItemGroup><PackageReference Include="Ydb.Sdk" Version="0.35.0" /></ItemGroup></Project>`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "generated.csproj"), []byte(project), 0600))
	cmd := exec.Command(dotnet, "build", "--nologo")
	cmd.Dir = dir
	cmd.Env = dotnetEnv(dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		require.NoError(t, err, "generated C# does not build against Ydb.Sdk 0.35.0: %v\n%s", err, out)
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
		{Kind: "Float"}, {Kind: "Double"}, {Kind: "Utf8"}, {Kind: "String"}, {Kind: "Json"}, {Kind: "Timestamp"}, {Kind: "Uuid"},
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
	require.NoError(t, err)
	dir := t.TempDir()
	for _, file := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file.Name), file.Content, 0600))
	}
	project := `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework><Nullable>enable</Nullable><TreatWarningsAsErrors>true</TreatWarningsAsErrors></PropertyGroup><ItemGroup><PackageReference Include="Ydb.Sdk" Version="0.35.0" /></ItemGroup></Project>`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scalars.csproj"), []byte(project), 0600))
	cmd := exec.Command(dotnet, "build", "--nologo")
	cmd.Dir = dir
	cmd.Env = dotnetEnv(dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		require.NoError(t, err, "all generated scalar bindings must compile against Ydb.Sdk 0.35.0: %v\n%s", err, out)
	}
}

func TestGeneratedRuntimeProfilesBuildAgainstPublishedPackages(t *testing.T) {
	dotnet := os.Getenv("SQLC_YDB_CSHARP_DOTNET")
	if dotnet == "" {
		t.Skip("set SQLC_YDB_CSHARP_DOTNET to run the published-package builds")
	}
	jsonType := model.Type{Kind: "Json"}
	timestampType := model.Type{Kind: "Timestamp"}
	in := authorsAnalysis()
	in.Queries = append(in.Queries, model.AnalyzedQuery{
		Name: "CreateEvent", Command: model.One,
		SQL:        "DECLARE $document AS Json;\nDECLARE $at AS Optional<Timestamp>;\nSELECT $document AS document, $at AS at;\n",
		Parameters: []model.Parameter{{Name: "document", Type: jsonType}, {Name: "at", Type: model.Optional(timestampType)}},
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "document", Type: jsonType}, {Name: "at", Type: model.Optional(timestampType)}}}},
	})
	for _, tc := range []struct {
		runtime   string
		packages  string
		namespace string
	}{
		{"adonet", "", "Build.AdoNet"},
		{"dapper", `<PackageReference Include="Dapper" Version="2.1.79" />`, "Build.Dapper"},
	} {
		t.Run(tc.runtime, func(t *testing.T) {
			files, err := Generate(in, Options{Namespace: tc.namespace, Runtime: tc.runtime})
			require.NoError(t, err)
			dir := t.TempDir()
			for _, file := range files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, file.Name), file.Content, 0600))
			}
			project := `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework><Nullable>enable</Nullable><TreatWarningsAsErrors>true</TreatWarningsAsErrors></PropertyGroup><ItemGroup><PackageReference Include="Ydb.Sdk" Version="0.35.0" />` + tc.packages + `</ItemGroup></Project>`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "generated.csproj"), []byte(project), 0600))
			cmd := exec.Command(dotnet, "build", "--nologo")
			cmd.Dir, cmd.Env = dir, dotnetEnv(dir)
			if out, err := cmd.CombinedOutput(); err != nil {
				require.NoError(t, err, "generated %s profile does not build against published packages: %v\n%s", tc.runtime, err, out)
			}
		})
	}
}

func TestRejectsModelNamesThatShadowFrameworkTypes(t *testing.T) {
	for _, table := range []string{"guid", "date_time", "task", "cancellation_token", "db_data_reader", "argument_null_exception", "invalid_operation_exception", "ydb_value"} {
		t.Run(table, func(t *testing.T) {
			_, err := Generate(&model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: table}}}}, Options{})
			require.ErrorContains(t, err, "model name collision", "Generate() error = %v, want framework model-name collision", err)
		})
	}
}

func TestDapperAllowsModelNamedLikeNestedParameterHelper(t *testing.T) {
	files, err := Generate(&model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: "ydb_parameters"}}}}, Options{Runtime: "dapper"})
	require.NoError(t, err)
	require.Contains(t, string(files[0].Content), "public sealed record YdbParameters(", "Models.cs missing YdbParameters record")
}

func TestRejectsRecordMemberCollisions(t *testing.T) {
	utf8 := model.Type{Kind: "Utf8"}
	for _, tc := range []struct {
		name string
		in   *model.AnalysisResult
		want string
	}{
		{
			name: "table member equals record", want: "collides with record name",
			in: &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{
				Name: "authors", Columns: []model.Column{{Name: "authors", Type: utf8}},
			}}}},
		},
		{
			name: "row member equals record", want: "collides with record name",
			in: &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
				Name: "get_author", Command: model.One, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "get_author_row", Type: utf8}}}},
			}}},
		},
		{
			name: "params member equals record", want: "collides with record name",
			in: &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
				Name: "upsert", Command: model.Exec, Parameters: []model.Parameter{{Name: "upsert_params", Type: utf8}, {Name: "other", Type: utf8}},
			}}},
		},
		{
			name: "synthesized clone", want: "generated record member",
			in: &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{
				Name: "authors", Columns: []model.Column{{Name: "clone", Type: utf8}},
			}}}},
		},
		{
			name: "synthesized equality contract", want: "generated record member",
			in: &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{
				Name: "authors", Columns: []model.Column{{Name: "equality_contract", Type: utf8}},
			}}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Generate(tc.in, Options{})
			require.ErrorContains(t, err, tc.want, "Generate() error = %v, want %q", err, tc.want)
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
	sql := declaredBatchSQL + "SELECT '\"', '\\\\', '" + string(rune(0)) + "', '" + string(rune(0x1f)) + "';\r\n-- \"\"\" delimiter-looking text\n"
	in := authorsAnalysis()
	in.Queries = in.Queries[:1]
	in.Queries[0].SQL = sql
	files, err := Generate(in, Options{Namespace: "Authors.AdoNet"})
	require.NoError(t, err)
	dir := t.TempDir()
	for _, file := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file.Name), file.Content, 0600))
	}
	project := `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework><Nullable>enable</Nullable><TreatWarningsAsErrors>true</TreatWarningsAsErrors></PropertyGroup><ItemGroup><PackageReference Include="Ydb.Sdk" Version="0.35.0" /></ItemGroup></Project>`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "literal.csproj"), []byte(project), 0600))
	_, inline, ok := strings.Cut(string(files[1].Content), "new YdbCommand(")
	require.False(t, !ok, "generated query does not construct a YdbCommand")
	inline, _, ok = strings.Cut(inline, ", _connection)")
	require.False(t, !ok, "generated query constructor has no connection argument")
	program := fmt.Sprintf(`using System; using System.Text; using Ydb.Sdk.Ado; internal static class Program { static int Main() { using var command = new YdbCommand(%s); var actual = command.CommandText; if (Convert.ToBase64String(Encoding.UTF8.GetBytes(actual)) != %q) throw new Exception("SQL changed"); return 0; } }`, inline, base64.StdEncoding.EncodeToString([]byte(sql)))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Program.cs"), []byte(program), 0600))
	env := dotnetEnv(dir)
	build := exec.Command(dotnet, "build", "--nologo")
	build.Dir, build.Env = dir, env
	if out, err := build.CombinedOutput(); err != nil {
		require.NoError(t, err, "literal build: %v\n%s", err, out)
	}
	run := exec.Command(dotnet, "run", "--no-build", "--nologo")
	run.Dir, run.Env = dir, env
	if out, err := run.CombinedOutput(); err != nil {
		require.NoError(t, err, "literal runtime: %v\n%s", err, out)
	}
}

func dotnetEnv(dir string) []string {
	env := append([]string{}, os.Environ()...)
	env = append(env, "DOTNET_CLI_HOME="+filepath.Join(dir, ".dotnet"))
	if os.Getenv("NUGET_PACKAGES") == "" {
		env = append(env, "NUGET_PACKAGES="+filepath.Join(dir, ".nuget"))
	}
	return env
}

// This exercises the public 0.35.0 SDK value serializer, rather than relying
// on DbType/DBNull behavior. Generated Optional<T> expressions use these
// values so both a present value and null retain Optional<primitive> on wire.
func TestOptionalParametersSerializeAsTypedYdbValues(t *testing.T) {
	dotnet := os.Getenv("SQLC_YDB_CSHARP_DOTNET")
	if dotnet == "" {
		t.Skip("set SQLC_YDB_CSHARP_DOTNET to run the published-SDK optional wire-type check")
	}
	_, queries := generated(t, authorsAnalysis())
	require.Contains(t, queries, "YdbValue.MakeOptionalUtf8(args.Biography)", "generated optional parameter did not use YdbValue: %s", queries)
	dir := t.TempDir()
	project := `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework><Nullable>enable</Nullable><TreatWarningsAsErrors>true</TreatWarningsAsErrors></PropertyGroup><ItemGroup><PackageReference Include="Ydb.Sdk" Version="0.35.0" /></ItemGroup></Project>`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "wire.csproj"), []byte(project), 0600))
	program := `using System;
using Ydb.Sdk.Ado;
using Ydb.Sdk.Value;

internal static class Program
{
    private static void Check(YdbValue value, Ydb.Type.Types.PrimitiveTypeId primitive, bool isNull)
    {
        var proto = value.GetProto();
        if (proto.Type.OptionalType?.Item.TypeId != primitive) throw new Exception("optional item type changed");
        if ((proto.Value.ValueCase == Ydb.Value.ValueOneofCase.NullFlagValue) != isNull) throw new Exception("optional presence changed");
    }

    private static int Main()
    {
        var present = new YdbParameter("$present", YdbValue.MakeOptionalUtf8("present"));
        var absent = new YdbParameter("$absent", YdbValue.MakeOptionalUtf8(null));
        var bytes = new YdbParameter("$bytes", YdbValue.MakeOptionalString(new byte[] { 0, 255 }));
        Check((YdbValue)present.Value!, Ydb.Type.Types.PrimitiveTypeId.Utf8, false);
        Check((YdbValue)absent.Value!, Ydb.Type.Types.PrimitiveTypeId.Utf8, true);
        Check((YdbValue)bytes.Value!, Ydb.Type.Types.PrimitiveTypeId.String, false);
        return 0;
    }
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Program.cs"), []byte(program), 0600))
	build := exec.Command(dotnet, "build", "--nologo")
	build.Dir, build.Env = dir, dotnetEnv(dir)
	if out, err := build.CombinedOutput(); err != nil {
		require.NoError(t, err, "optional wire-type build: %v\n%s", err, out)
	}
	run := exec.Command(dotnet, "run", "--no-build", "--nologo")
	run.Dir, run.Env = dir, dotnetEnv(dir)
	if out, err := run.CombinedOutput(); err != nil {
		require.NoError(t, err, "optional wire-type runtime: %v\n%s", err, out)
	}
}

func TestLocalParameterNames(t *testing.T) {
	for input, want := range map[string]string{"author_id": "authorId", "id": "id", "event": "@event", "command": "commandValue", "cancellation_token": "cancellationTokenValue"} {
		assert.Equal(t, want, localParameterName(input), "input %s", input)
	}
}

func TestRejectsRemovedLinq2DBRuntime(t *testing.T) {
	if _, err := Generate(authorsAnalysis(), Options{Runtime: "linq2db"}); err == nil {
		require.Error(t, err, "removed runtime accepted")
	}
}

func TestDapperRawLiteralRuntime(t *testing.T) {
	dotnet := os.Getenv("SQLC_YDB_CSHARP_DOTNET")
	if dotnet == "" {
		t.Skip("set SQLC_YDB_CSHARP_DOTNET")
	}
	samples := []string{declaredBatchSQL, "SELECT 1;", "SELECT \"\"\"\";\n\t-- конец\n", "\n  SELECT 1;\n\n", "SELECT 1;\r\n", "SELECT '\x00';", ""}
	var program strings.Builder
	program.WriteString("using System; using System.Text; class Program { static void Main() {\n")
	for _, sql := range samples {
		fmt.Fprintf(&program, "if (Convert.ToBase64String(Encoding.UTF8.GetBytes(%s)) != %q) throw new Exception(\"SQL changed\");\n", dapperSQLLiteral(sql), base64.StdEncoding.EncodeToString([]byte(sql)))
	}
	program.WriteString("} }")
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Program.cs"), []byte(program.String()), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "literal.csproj"), []byte(`<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`), 0600))
	cmd := exec.Command(dotnet, "run", "--project", "literal.csproj")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		require.NoError(t, err, "%v\n%s", err, out)
	}
}

// Mixed whitespace and a multiline SQL value distinguish formatting from SQL semantics.
const declaredBatchSQL = "  DECLARE $books AS List<Struct<\n    book_id: Uint64,\n\tdata: Json\n  >>;\n\n  -- batch books\n  INSERT INTO books (book_id, data)\n  SELECT\n      book_id,\n\t  data\n  FROM AS_TABLE($books);\n  SELECT 'first\n    second';\n"
