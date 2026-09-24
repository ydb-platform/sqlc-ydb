package cpp

import (
	"bytes"
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
	uint64Type := model.Type{Kind: "Uint64"}
	utf8Type := model.Type{Kind: "Utf8"}
	optionalUtf8 := model.Optional(utf8Type)
	row := model.ResultSet{Columns: []model.Column{
		{Name: "id", Type: uint64Type},
		{Name: "name", Type: utf8Type},
		{Name: "bio", Type: optionalUtf8},
	}}
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{
			Name: "GetAuthor", Command: model.One,
			SQL:        "DECLARE $author_id AS Uint64;\nSELECT id, name, bio FROM authors WHERE id = $author_id;",
			Parameters: []model.Parameter{{Name: "author_id", Type: uint64Type}},
			ResultSets: []model.ResultSet{row},
		},
		{
			Name: "ListAuthors", Command: model.Many,
			SQL:        "SELECT id, name, bio FROM authors ORDER BY id;",
			ResultSets: []model.ResultSet{row},
		},
		{
			Name: "UpsertAuthor", Command: model.Exec,
			SQL: "DECLARE $author_id AS Uint64;\nDECLARE $author_name AS Utf8;\nDECLARE $biography AS Optional<Utf8>;\nUPSERT INTO authors (id, name, bio)\nVALUES ($author_id, $author_name, $biography);",
			Parameters: []model.Parameter{
				{Name: "author_id", Type: uint64Type},
				{Name: "author_name", Type: utf8Type},
				{Name: "biography", Type: optionalUtf8},
			},
		},
	}}
}

func generatedContent(t *testing.T, files []model.File, name string) string {
	t.Helper()
	for _, file := range files {
		if file.Name == name {
			return string(file.Content)
		}
	}
	require.FailNow(t, fmt.Sprintf("missing generated file %q", name))
	return ""
}

func TestGenerateNativeYDBAuthorsAPI(t *testing.T) {
	files, err := Generate(authorsAnalysis(), Options{Namespace: "example::authors", Runtime: "ydb"})
	require.NoError(t, err)
	require.Equal(t, 3, len(files), "got %d files, want 3", len(files))
	models := generatedContent(t, files, "models.hpp")
	header := generatedContent(t, files, "queries.hpp")
	source := generatedContent(t, files, "queries.cpp")
	for _, want := range []string{
		"namespace example::authors {",
		"struct GetAuthorRow final {",
		"std::uint64_t id;",
		"std::string name;",
		"std::optional<std::string> bio;",
	} {
		assert.Contains(t, models, want, "models.hpp missing %q:\n%s", want, models)
	}
	for _, want := range []string{
		"explicit Queries(NYdb::NQuery::TQueryClient& client,",
		"explicit Queries(NYdb::NQuery::TTransaction& transaction,",
		"std::optional<GetAuthorRow> GetAuthor(std::uint64_t author_id) const;",
		"std::vector<ListAuthorsRow> ListAuthors() const;",
		"void UpsertAuthor(std::uint64_t author_id, const std::string& author_name, const std::optional<std::string>& biography) const;",
		"NYdb::NQuery::TQueryClient* client_;",
		"NYdb::NQuery::TTransaction* transaction_;",
	} {
		assert.Contains(t, header, want, "queries.hpp missing %q:\n%s", want, header)
	}
	for _, want := range []string{
		"#include <ydb-cpp-sdk/client/types/status/status.h>",
		"client_->RetryQuerySync",
		"NYdb::NQuery::TTxControl::Tx(*this->transaction_)",
		"NYdb::NStatusHelpers::ThrowOnError(sqlc_status);",
		"NYdb::NQuery::TTxControl::BeginTx(this->tx_settings_).CommitTx()",
		".AddParam(\"$author_id\").Uint64(author_id).Build()",
		".AddParam(\"$author_name\").Utf8(author_name).Build()",
		".AddParam(\"$biography\").OptionalUtf8(biography).Build()",
		"GetUint64()",
		"GetUtf8()",
		"GetOptionalUtf8()",
	} {
		assert.Contains(t, source, want, "queries.cpp missing %q:\n%s", want, source)
	}
}

func TestGenerateUserverAuthorsAPI(t *testing.T) {
	files, err := Generate(authorsAnalysis(), Options{Namespace: "example::authors", Runtime: "userver"})
	require.NoError(t, err)
	models := generatedContent(t, files, "models.hpp")
	header := generatedContent(t, files, "queries.hpp")
	source := generatedContent(t, files, "queries.cpp")
	for _, want := range []string{
		"::userver::ydb::Utf8 name;",
		"std::optional<::userver::ydb::Utf8> bio;",
	} {
		assert.Contains(t, models, want, "models.hpp missing %q:\n%s", want, models)
	}
	for _, want := range []string{
		"explicit Queries(::userver::ydb::TableClient& client,",
		"explicit Queries(::userver::ydb::TxActor& transaction,",
		"void UpsertAuthor(std::uint64_t author_id, const ::userver::ydb::Utf8& author_name, const std::optional<::userver::ydb::Utf8>& biography) const;",
		"::userver::ydb::TableClient* client_;",
		"::userver::ydb::TxActor* transaction_;",
	} {
		assert.Contains(t, header, want, "queries.hpp missing %q:\n%s", want, header)
	}
	for _, want := range []string{
		"::userver::ydb::Query{",
		"::userver::ydb::Query::Name{\"GetAuthor\"}",
		"::userver::ydb::Query::LogMode::kNameOnly",
		"this->transaction_->Execute(",
		"ExecuteQuery(this->operation_settings_, sqlc_query, \"$author_id\", author_id)",
		"sqlc_row.Get<std::uint64_t>(\"id\")",
		"sqlc_row.Get<::userver::ydb::Utf8>(\"name\")",
		"sqlc_row.Get<std::optional<::userver::ydb::Utf8>>(\"bio\")",
	} {
		assert.Contains(t, source, want, "queries.cpp missing %q:\n%s", want, source)
	}
}

func TestUserverRuntimeQueryConstructor(t *testing.T) {
	compiler, err := exec.LookPath("clang++")
	if err != nil {
		t.Skip("clang++ is unavailable")
	}
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Named", Command: model.Exec, SQL: "SELECT 1;\nSELECT 2;"}}}
	files, err := Generate(a, Options{Runtime: "userver"})
	require.NoError(t, err)
	generated := generatedContent(t, files, "queries.cpp")
	start := strings.Index(generated, "::userver::ydb::Query{")
	require.False(t, start < 0, "missing Query construction")
	end := strings.Index(generated[start:], "\n    }")
	require.False(t, end < 0, "missing Query initializer end")
	query := generated[start : start+end+len("\n    }")]
	require.Contains(t, query, "\n        \"SELECT 1;\\n\"\n        \"SELECT 2;\"", "SQL literals must align with the query constructor arguments")
	// The generated runtime Name selects the overload that owns the query text.
	program := `#include <optional>
#include <string>
#include <utility>
namespace userver::ydb {
struct Query {
  struct Name { std::string value; explicit Name(std::string text): value(std::move(text)) {} };
  struct NameLiteral { consteval NameLiteral(const char*) {} };
  struct StringLiteral { consteval StringLiteral(const char*) {} };
  enum class LogMode { kNameOnly };
  Query(StringLiteral, NameLiteral, LogMode) {}
  Query(std::string text, std::optional<Name> name, LogMode): text(std::move(text)), name(std::move(name)) {}
  std::string text;
  std::optional<Name> name;
};
}
int main() {
  auto query = ` + query + `;
	return query.text != "SELECT 1;\nSELECT 2;" || !query.name || query.name->value != "Named";
}
`
	dir := t.TempDir()
	input, binary := filepath.Join(dir, "query.cpp"), filepath.Join(dir, "query")
	require.NoError(t, os.WriteFile(input, []byte(program), 0600))
	if out, err := exec.Command(compiler, "-std=c++20", input, "-o", binary).CombinedOutput(); err != nil {
		require.NoError(t, err, "compile userver Query constructor: %v\n%s", err, out)
	}
	if out, err := exec.Command(binary).CombinedOutput(); err != nil {
		require.NoError(t, err, "query name or SQL bytes changed: %v\n%s", err, out)
	}
}

func TestJoinedColumnsUseExactResultKeysWithoutChangingAPIFields(t *testing.T) {
	uint64Type := model.Type{Kind: "Uint64"}
	utf8Type := model.Type{Kind: "Utf8"}
	row := model.ResultSet{Columns: []model.Column{
		{Name: "id", WireName: "a.id", Type: uint64Type},
		{Name: "author_name", Type: utf8Type},
	}}
	analysis := &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{
			Name: "GetJoinedAuthor", Command: model.One,
			SQL:        "SELECT a.id, a.name AS author_name FROM authors AS a JOIN books AS b ON a.id = b.author_id;",
			ResultSets: []model.ResultSet{row},
		},
		{
			Name: "ListJoinedAuthors", Command: model.Many,
			SQL:        "SELECT a.id, a.name AS author_name FROM authors AS a JOIN books AS b ON a.id = b.author_id;",
			ResultSets: []model.ResultSet{row},
		},
	}}

	tests := []struct {
		runtime      string
		aliasField   string
		qualifiedGet string
		aliasGet     string
	}{
		{"ydb", "std::string author_name;", `sqlc_parser.ColumnParser("a.id").GetUint64()`, `sqlc_parser.ColumnParser("author_name").GetUtf8()`},
		{"userver", "::userver::ydb::Utf8 author_name;", `sqlc_row.Get<std::uint64_t>("a.id")`, `sqlc_row.Get<::userver::ydb::Utf8>("author_name")`},
	}
	for _, tc := range tests {
		t.Run(tc.runtime, func(t *testing.T) {
			files, err := Generate(analysis, Options{Runtime: tc.runtime})
			require.NoError(t, err)
			models := generatedContent(t, files, "models.hpp")
			source := generatedContent(t, files, "queries.cpp")
			for _, field := range []string{"std::uint64_t id;", tc.aliasField} {
				assert.Equal(t, 2, strings.Count(models, field), "models.hpp should contain %q twice:\n%s", field, models)
			}
			for _, lookup := range []string{tc.qualifiedGet, tc.aliasGet} {
				assert.Equal(t, 2, strings.Count(source, lookup), "queries.cpp should contain %q twice:\n%s", lookup, source)
			}
		})
	}
}

func TestGenerateRejectsUnsupportedAndUnsafeInput(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*model.AnalysisResult)
		opts    Options
		message string
	}{
		{"unknown runtime", func(*model.AnalysisResult) {}, Options{Runtime: "grpc"}, "unsupported C++ runtime"},
		{"execrows", func(a *model.AnalysisResult) { a.Queries[0].Command = model.ExecRows }, Options{Runtime: "ydb"}, ":execrows"},
		{"missing result", func(a *model.AnalysisResult) { a.Queries[0].ResultSets = nil }, Options{Runtime: "ydb"}, "requires one non-empty result set"},
		{"keyword parameter", func(a *model.AnalysisResult) { a.Queries[0].Parameters[0].Name = "class" }, Options{Runtime: "ydb"}, "invalid C++ parameter"},
		{"reserved prefix", func(a *model.AnalysisResult) { a.Queries[0].ResultSets[0].Columns[0].Name = "sqlc_row" }, Options{Runtime: "ydb"}, "reserved sqlc_ prefix"},
		{"userver float", func(a *model.AnalysisResult) { a.Queries[0].Parameters[0].Type = model.Type{Kind: "Float"} }, Options{Runtime: "userver"}, "unsupported YQL type \"Float\" for userver"},
		{"nested optional", func(a *model.AnalysisResult) {
			a.Queries[0].Parameters[0].Type = model.Optional(model.Optional(model.Type{Kind: "Utf8"}))
		}, Options{Runtime: "ydb"}, "nested Optional"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := authorsAnalysis()
			tc.mutate(a)
			_, err := Generate(a, tc.opts)
			require.ErrorContains(t, err, tc.message, "got error %v, want substring %q", err, tc.message)
		})
	}
}

func TestOneReturnsFirstRowWithoutRejectingAdditionalRows(t *testing.T) {
	for _, runtime := range []string{"ydb", "userver"} {
		t.Run(runtime, func(t *testing.T) {
			files, err := Generate(authorsAnalysis(), Options{Runtime: runtime})
			require.NoError(t, err)
			require.NotContains(t, generatedContent(t, files, "queries.cpp"), ":one query returned more than one row")
		})
	}
}

func TestRejectsGeneratedNameCollisions(t *testing.T) {
	t.Run("query method hides row type", func(t *testing.T) {
		a := authorsAnalysis()
		a.Queries = append(a.Queries, model.AnalyzedQuery{Name: "GetAuthorRow", Command: model.Exec, SQL: "SELECT 1;"})
		_, err := Generate(a, Options{Runtime: "ydb"})
		require.False(t, err == nil || !strings.Contains(err.Error(), "GetAuthorRow") || !strings.Contains(err.Error(), "row type"), "unexpected error: %v", err)
	})
	t.Run("field has its row type name", func(t *testing.T) {
		a := authorsAnalysis()
		a.Queries[0].ResultSets[0].Columns[0].Name = "GetAuthorRow"
		_, err := Generate(a, Options{Runtime: "ydb"})
		require.False(t, err == nil || !strings.Contains(err.Error(), "GetAuthorRow") || !strings.Contains(err.Error(), "row type"), "unexpected error: %v", err)
	})
	t.Run("parameter hides row type", func(t *testing.T) {
		a := authorsAnalysis()
		a.Queries[0].Parameters[0].Name = "GetAuthorRow"
		_, err := Generate(a, Options{Runtime: "ydb"})
		require.False(t, err == nil || !strings.Contains(err.Error(), "GetAuthorRow") || !strings.Contains(err.Error(), "row type"), "unexpected error: %v", err)
	})
	t.Run("former SQL constant name is available", func(t *testing.T) {
		a := authorsAnalysis()
		a.Queries[0].Parameters[0].Name = "kGetAuthorSql"
		_, err := Generate(a, Options{Runtime: "ydb"})
		require.NoError(t, err, "unexpected error: %v", err)
	})
	for _, member := range []string{"client_", "transaction_"} {
		t.Run("query method conflicts with "+member, func(t *testing.T) {
			a := authorsAnalysis()
			a.Queries[0].Name = member
			_, err := Generate(a, Options{Runtime: "ydb"})
			require.ErrorContains(t, err, "invalid C++ query name", "unexpected error: %v", err)
		})
	}
}

func TestParameterCannotShadowClientMember(t *testing.T) {
	a := authorsAnalysis()
	a.Queries[0].Parameters[0].Name = "client_"
	files, err := Generate(a, Options{Runtime: "ydb"})
	require.NoError(t, err)
	source := generatedContent(t, files, "queries.cpp")
	require.Contains(t, source, "this->client_->RetryQuerySync", "client member is not explicitly qualified:\n%s", source)
}

func TestScalarWidthsAndStringKindsRemainDistinct(t *testing.T) {
	tests := []struct {
		kind          string
		cpp           string
		nativeBuilder string
		nativeParser  string
	}{
		{"Bool", "bool", "Bool", "GetBool"},
		{"Int8", "std::int8_t", "Int8", "GetInt8"},
		{"Uint8", "std::uint8_t", "Uint8", "GetUint8"},
		{"Int16", "std::int16_t", "Int16", "GetInt16"},
		{"Uint16", "std::uint16_t", "Uint16", "GetUint16"},
		{"Int32", "std::int32_t", "Int32", "GetInt32"},
		{"Uint32", "std::uint32_t", "Uint32", "GetUint32"},
		{"Int64", "std::int64_t", "Int64", "GetInt64"},
		{"Uint64", "std::uint64_t", "Uint64", "GetUint64"},
		{"Float", "float", "Float", "GetFloat"},
		{"Double", "double", "Double", "GetDouble"},
		{"String", "std::string", "String", "GetString"},
		{"Utf8", "std::string", "Utf8", "GetUtf8"},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			info, err := typeInfo(model.Type{Kind: tc.kind}, "ydb")
			require.NoError(t, err)
			require.False(t, info.cpp != tc.cpp || info.builder != tc.nativeBuilder || info.parser != tc.nativeParser, "got %+v, want C++ %q builder %q parser %q", info, tc.cpp, tc.nativeBuilder, tc.nativeParser)
		})
	}
	stringInfo, err := typeInfo(model.Type{Kind: "String"}, "userver")
	require.NoError(t, err)
	utf8Info, err := typeInfo(model.Type{Kind: "Utf8"}, "userver")
	require.NoError(t, err)
	require.False(t, stringInfo.cpp != "std::string" || utf8Info.cpp != "::userver::ydb::Utf8", "userver string types collapsed: String=%q Utf8=%q", stringInfo.cpp, utf8Info.cpp)
}

func TestSQLLiteralPreservesSourceBytes(t *testing.T) {
	compiler, err := exec.LookPath("clang++")
	if err != nil {
		t.Skip("clang++ is unavailable")
	}
	cases := []struct {
		name, sql string
	}{
		{"multiline", "-- Привет\nSELECT '\\\"', `name`\nFROM authors;"},
		{"delimiter collision", "SELECT ')sql\"', ')sql1\"';"},
		{"declared indentation", "  DECLARE $id AS Uint64; -- keep\n\n    SELECT 'first\n  second';\n"},
		{"CRLF and whitespace", "DECLARE $id AS Uint64; \r\n \t \r\nSELECT $id;\t"},
		{"control byte", "SELECT '\x00A\x01';"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			literal := sqlLiteral(tc.sql)
			require.False(t, strings.HasPrefix(literal, "R\""), "unexpected literal form: %s", literal)
			for _, line := range strings.Split(literal, "\n") {
				require.False(t, strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t"), "literal adds trailing whitespace: %q", line)
			}
			dir := t.TempDir()
			input := filepath.Join(dir, "literal.cpp")
			binary := filepath.Join(dir, "literal")
			source := "#include <iostream>\n#include <string>\nint main(){const std::string sql=" + literal + ";std::cout.write(sql.data(),sql.size());}\n"
			require.NoError(t, os.WriteFile(input, []byte(source), 0600))
			if output, err := exec.Command(compiler, "-std=c++20", input, "-o", binary).CombinedOutput(); err != nil {
				require.NoError(t, err, "compile: %v %s", err, output)
			}
			actual, err := exec.Command(binary).Output()
			require.NoError(t, err)
			require.False(t, !bytes.Equal(actual, []byte(tc.sql)), "SQL source changed: got %q want %q", actual, tc.sql)
		})
	}
}

func TestSettingsAndHeaderHygiene(t *testing.T) {
	for _, runtime := range []string{"ydb", "userver"} {
		files, err := Generate(authorsAnalysis(), Options{Runtime: runtime})
		require.NoError(t, err)
		source := generatedContent(t, files, "queries.cpp")
		assert.Contains(t, source, "this->execute_settings_", "%s: missing execution settings", runtime)
		if runtime == "ydb" {
			for _, want := range []string{"}, this->retry_settings_)", "GetResultSets().size() != 1"} {
				assert.Contains(t, source, want, "native: missing %s", want)
			}
		} else {
			assert.False(t, strings.Contains(source, "#include <utility>"), "unused userver utility include")
			for _, name := range []string{"models.hpp", "queries.hpp"} {
				assert.False(t, strings.Contains(generatedContent(t, files, name), "#include <string>"), "unused string include in %s", name)
			}
		}
	}
	assert.Error(t, validateIdent("a__b"), "C++ reserved double underscore accepted")
}

func TestJsonTimestampTypes(t *testing.T) {
	for _, runtime := range []string{"ydb", "userver"} {
		for _, optional := range []bool{false, true} {
			jsonType, timestampType := model.Type{Kind: "Json"}, model.Type{Kind: "Timestamp"}
			if optional {
				jsonType, timestampType = model.Optional(jsonType), model.Optional(timestampType)
			}
			in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
				Name: "Values", Command: model.One, SQL: "SELECT $data AS data, $at AS at;",
				Parameters: []model.Parameter{{Name: "data", Type: jsonType}, {Name: "at", Type: timestampType}},
				ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "data", Type: jsonType}, {Name: "at", Type: timestampType}}}},
			}}}
			files, err := Generate(in, Options{Runtime: runtime})
			require.NoError(t, err)
			models := generatedContent(t, files, "models.hpp")
			source := generatedContent(t, files, "queries.cpp")
			if runtime == "ydb" {
				require.False(t, !strings.Contains(models, "TInstant") || !strings.Contains(models, "std::string"), models)
				prefix := ""
				if optional {
					prefix = "Optional"
				}
				for _, kind := range []string{"Json", "Timestamp"} {
					require.False(t, !strings.Contains(source, "."+prefix+kind+"(") || !strings.Contains(source, "Get"+prefix+kind+"()"), source)
				}
			} else {
				require.False(t, !strings.Contains(models, "::userver::formats::json::Value") || !strings.Contains(models, "std::chrono::system_clock::time_point") || !strings.Contains(models, "<userver/formats/json/value.hpp>"), models)
			}
		}
	}
}

func TestStructListParameter(t *testing.T) {
	for _, runtime := range []string{"ydb", "userver"} {
		t.Run(runtime, func(t *testing.T) {
			typ := model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, {Name: "tags", Type: model.Optional(model.Type{Kind: "Json"})}, {Name: "available", Type: model.Type{Kind: "Timestamp"}}}}}
			a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "CreateBooks", Command: model.Exec, SQL: "SELECT $books;", Parameters: []model.Parameter{{Name: "books", Type: typ}}}}}
			files, err := Generate(a, Options{Runtime: runtime})
			require.NoError(t, err)
			var output strings.Builder
			for _, f := range files {
				output.Write(f.Content)
			}
			expected := []string{"struct CreateBooksBooksItem final", "const std::vector<CreateBooksBooksItem>& books"}
			if runtime == "ydb" {
				expected = append(expected, "NYdb::TValueBuilder sqlc_builder(sqlc_type)", ".BeginList().BeginStruct()", ".AddMember(\"tags\").BeginOptional().Primitive(NYdb::EPrimitiveType::Json).EndOptional()", ".OptionalJson(sqlc_item.tags)")
			} else {
				expected = append(expected, "static constexpr ::userver::ydb::StructMemberNames kYdbMemberNames{}", "std::optional<::userver::formats::json::Value>", "#include <chrono>")
			}
			for _, want := range expected {
				assert.Contains(t, output.String(), want, "missing %s", want)
			}
			typ.Elem.Fields[1].Type = model.Type{Kind: "List", Elem: &model.Type{Kind: "Utf8"}}
			if _, err := Generate(a, Options{Runtime: runtime}); err == nil || !strings.Contains(err.Error(), "unsupported YQL type") {
				require.FailNow(t, fmt.Sprintf("nested list: %v", err))
			}
		})
	}
}

func TestStructListRejectsInvalidFieldsAndAmbiguousTypeNames(t *testing.T) {
	makeQuery := func(name, param string, fields []model.StructField) model.AnalyzedQuery {
		return model.AnalyzedQuery{Name: name, Command: model.Exec, SQL: "SELECT 1;", Parameters: []model.Parameter{{Name: param, Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: fields}}}}}
	}
	valid := []model.StructField{{Name: "id", Type: model.Type{Kind: "Uint64"}}}
	for _, tc := range []struct {
		name, runtime string
		queries       []model.AnalyzedQuery
		want          string
	}{
		{name: "empty struct", queries: []model.AnalyzedQuery{makeQuery("CreateBooks", "books", nil)}, want: "at least one scalar field"},
		{name: "invalid identifier", queries: []model.AnalyzedQuery{makeQuery("CreateBooks", "books", []model.StructField{{Name: "class", Type: model.Type{Kind: "Utf8"}}})}, want: "C++ keyword"},
		{name: "duplicate field", queries: []model.AnalyzedQuery{makeQuery("CreateBooks", "books", append(valid, valid...))}, want: "duplicate struct field"},
		{name: "member shadows item type", queries: []model.AnalyzedQuery{makeQuery("CreateBooks", "books", []model.StructField{{Name: "CreateBooksBooksItem", Type: model.Type{Kind: "Uint64"}}})}, want: "collides with generated member"},
		{name: "userver metadata", runtime: "userver", queries: []model.AnalyzedQuery{makeQuery("CreateBooks", "books", []model.StructField{{Name: "kYdbMemberNames", Type: model.Type{Kind: "Uint64"}}})}, want: "collides with generated member"},
		{name: "ambiguous item type", queries: []model.AnalyzedQuery{makeQuery("CreateBooks", "values", valid), makeQuery("Create", "Books_values", valid)}, want: "collides"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Generate(&model.AnalysisResult{Queries: tc.queries}, Options{Runtime: tc.runtime}); err == nil || !strings.Contains(err.Error(), tc.want) {
				require.FailNow(t, fmt.Sprintf("got %v, want %s", err, tc.want))
			}
		})
	}
}
