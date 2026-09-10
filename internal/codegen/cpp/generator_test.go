package cpp

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
	t.Fatalf("missing generated file %q", name)
	return ""
}

func TestGenerateNativeYDBAuthorsAPI(t *testing.T) {
	files, err := Generate(authorsAnalysis(), Options{Namespace: "example::authors", Runtime: "ydb"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3", len(files))
	}
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
		if !strings.Contains(models, want) {
			t.Errorf("models.hpp missing %q:\n%s", want, models)
		}
	}
	for _, want := range []string{
		"explicit Queries(NYdb::NQuery::TQueryClient& client) noexcept",
		"std::optional<GetAuthorRow> GetAuthor(std::uint64_t author_id) const;",
		"std::vector<ListAuthorsRow> ListAuthors() const;",
		"void UpsertAuthor(std::uint64_t author_id, const std::string& author_name, const std::optional<std::string>& biography) const;",
		"NYdb::NQuery::TQueryClient& client_;",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("queries.hpp missing %q:\n%s", want, header)
		}
	}
	for _, want := range []string{
		"#include <ydb-cpp-sdk/client/types/status/status.h>",
		"client_.RetryQuerySync",
		"NYdb::NStatusHelpers::ThrowOnError(sqlc_status);",
		"NYdb::NQuery::TTxControl::BeginTx(NYdb::NQuery::TTxSettings::SerializableRW()).CommitTx()",
		".AddParam(\"$author_id\").Uint64(author_id).Build()",
		".AddParam(\"$author_name\").Utf8(author_name).Build()",
		".AddParam(\"$biography\").OptionalUtf8(biography).Build()",
		"GetUint64()",
		"GetUtf8()",
		"GetOptionalUtf8()",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("queries.cpp missing %q:\n%s", want, source)
		}
	}
}

func TestGenerateUserverAuthorsAPI(t *testing.T) {
	files, err := Generate(authorsAnalysis(), Options{Namespace: "example::authors", Runtime: "userver"})
	if err != nil {
		t.Fatal(err)
	}
	models := generatedContent(t, files, "models.hpp")
	header := generatedContent(t, files, "queries.hpp")
	source := generatedContent(t, files, "queries.cpp")
	for _, want := range []string{
		"::userver::ydb::Utf8 name;",
		"std::optional<::userver::ydb::Utf8> bio;",
	} {
		if !strings.Contains(models, want) {
			t.Errorf("models.hpp missing %q:\n%s", want, models)
		}
	}
	for _, want := range []string{
		"explicit Queries(::userver::ydb::TableClient& client) noexcept",
		"void UpsertAuthor(std::uint64_t author_id, const ::userver::ydb::Utf8& author_name, const std::optional<::userver::ydb::Utf8>& biography) const;",
		"::userver::ydb::TableClient& client_;",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("queries.hpp missing %q:\n%s", want, header)
		}
	}
	for _, want := range []string{
		"::userver::ydb::Query{",
		"::userver::ydb::Query::NameLiteral{\"GetAuthor\"}",
		"::userver::ydb::Query::LogMode::kNameOnly",
		"}, \"$author_id\", author_id)",
		"sqlc_row.Get<std::uint64_t>(\"id\")",
		"sqlc_row.Get<::userver::ydb::Utf8>(\"name\")",
		"sqlc_row.Get<std::optional<::userver::ydb::Utf8>>(\"bio\")",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("queries.cpp missing %q:\n%s", want, source)
		}
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
			if err != nil {
				t.Fatal(err)
			}
			models := generatedContent(t, files, "models.hpp")
			source := generatedContent(t, files, "queries.cpp")
			for _, field := range []string{"std::uint64_t id;", tc.aliasField} {
				if count := strings.Count(models, field); count != 2 {
					t.Errorf("models.hpp contains %q %d times, want once for :one and once for :many:\n%s", field, count, models)
				}
			}
			for _, lookup := range []string{tc.qualifiedGet, tc.aliasGet} {
				if count := strings.Count(source, lookup); count != 2 {
					t.Errorf("queries.cpp contains %q %d times, want once for :one and once for :many:\n%s", lookup, count, source)
				}
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
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("got error %v, want substring %q", err, tc.message)
			}
		})
	}
}

func TestOneReturnsFirstRowWithoutRejectingAdditionalRows(t *testing.T) {
	for _, runtime := range []string{"ydb", "userver"} {
		t.Run(runtime, func(t *testing.T) {
			files, err := Generate(authorsAnalysis(), Options{Runtime: runtime})
			if err != nil {
				t.Fatal(err)
			}
			if source := generatedContent(t, files, "queries.cpp"); strings.Contains(source, ":one query returned more than one row") {
				t.Fatalf(":one must return the first row, not reject extra rows:\n%s", source)
			}
		})
	}
}

func TestRejectsGeneratedNameCollisions(t *testing.T) {
	t.Run("query method hides row type", func(t *testing.T) {
		a := authorsAnalysis()
		a.Queries = append(a.Queries, model.AnalyzedQuery{Name: "GetAuthorRow", Command: model.Exec, SQL: "SELECT 1;"})
		_, err := Generate(a, Options{Runtime: "ydb"})
		if err == nil || !strings.Contains(err.Error(), "GetAuthorRow") || !strings.Contains(err.Error(), "row type") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("field has its row type name", func(t *testing.T) {
		a := authorsAnalysis()
		a.Queries[0].ResultSets[0].Columns[0].Name = "GetAuthorRow"
		_, err := Generate(a, Options{Runtime: "ydb"})
		if err == nil || !strings.Contains(err.Error(), "GetAuthorRow") || !strings.Contains(err.Error(), "row type") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("parameter hides row type", func(t *testing.T) {
		a := authorsAnalysis()
		a.Queries[0].Parameters[0].Name = "GetAuthorRow"
		_, err := Generate(a, Options{Runtime: "ydb"})
		if err == nil || !strings.Contains(err.Error(), "GetAuthorRow") || !strings.Contains(err.Error(), "row type") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("former SQL constant name is available", func(t *testing.T) {
		a := authorsAnalysis()
		a.Queries[0].Parameters[0].Name = "kGetAuthorSql"
		_, err := Generate(a, Options{Runtime: "ydb"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("query method conflicts with client member", func(t *testing.T) {
		a := authorsAnalysis()
		a.Queries[0].Name = "client_"
		_, err := Generate(a, Options{Runtime: "ydb"})
		if err == nil || !strings.Contains(err.Error(), "invalid C++ query name") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestParameterCannotShadowClientMember(t *testing.T) {
	a := authorsAnalysis()
	a.Queries[0].Parameters[0].Name = "client_"
	files, err := Generate(a, Options{Runtime: "ydb"})
	if err != nil {
		t.Fatal(err)
	}
	source := generatedContent(t, files, "queries.cpp")
	if !strings.Contains(source, "this->client_.RetryQuerySync") {
		t.Fatalf("client member is not explicitly qualified:\n%s", source)
	}
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
			if err != nil {
				t.Fatal(err)
			}
			if info.cpp != tc.cpp || info.builder != tc.nativeBuilder || info.parser != tc.nativeParser {
				t.Fatalf("got %+v, want C++ %q builder %q parser %q", info, tc.cpp, tc.nativeBuilder, tc.nativeParser)
			}
		})
	}
	stringInfo, err := typeInfo(model.Type{Kind: "String"}, "userver")
	if err != nil {
		t.Fatal(err)
	}
	utf8Info, err := typeInfo(model.Type{Kind: "Utf8"}, "userver")
	if err != nil {
		t.Fatal(err)
	}
	if stringInfo.cpp != "std::string" || utf8Info.cpp != "::userver::ydb::Utf8" {
		t.Fatalf("userver string types collapsed: String=%q Utf8=%q", stringInfo.cpp, utf8Info.cpp)
	}
}

func TestSQLLiteralReadableAndRoundTripsAllBytes(t *testing.T) {
	compiler, err := exec.LookPath("clang++")
	if err != nil {
		t.Skip("clang++ is unavailable")
	}
	tests := []struct {
		name string
		sql  string
	}{
		{"multiline", "-- Привет\nSELECT '\\\"', `name`\nFROM authors;\n"},
		{"delimiter collision", "SELECT ')sqlc\"', ')sqlc1\"';"},
		{"controls", "SELECT '\x00\a\b\f\r\v\x1b\x7f';\n"},
		{"invalid utf8", string([]byte{'S', 'E', 'L', 'E', 'C', 'T', ' ', 0xff, ';'})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			literal := sqlLiteral(tc.sql)
			if tc.name == "multiline" && (!strings.Contains(literal, `R"sqlc(SELECT`) || strings.Contains(literal, `\nSELECT`)) {
				t.Fatalf("multiline SQL is not a readable raw literal: %s", literal)
			}
			dir := t.TempDir()
			source := "#include <iostream>\n#include <string>\nint main() { const std::string value = " + literal + "; std::cout.write(value.data(), value.size()); }\n"
			input := filepath.Join(dir, "literal.cpp")
			binary := filepath.Join(dir, "literal")
			if err := os.WriteFile(input, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(compiler, "-std=c++20", input, "-o", binary)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("compile SQL literal: %v\n%s\n%s", err, out, source)
			}
			got, err := exec.Command(binary).Output()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, []byte(tc.sql)) {
				t.Fatalf("round trip mismatch:\n got: %q\nwant: %q\nliteral: %s", got, []byte(tc.sql), literal)
			}
		})
	}
}
