package rust

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

func representativeAnalysis() *model.AnalysisResult {
	u64 := model.Type{Kind: "Uint64"}
	i32 := model.Type{Kind: "Int32"}
	utf8 := model.Type{Kind: "Utf8"}
	json := model.Type{Kind: "Json"}
	timestamp := model.Type{Kind: "Timestamp"}
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "CreateBook", Command: model.One, SQL: "DECLARE $id AS Uint64;\nINSERT INTO books (id) VALUES ($id) RETURNING id;", Parameters: []model.Parameter{{Name: "id", Type: u64}}, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: u64}}}}},
		{Name: "BooksByYear", Command: model.Many, SQL: "DECLARE $year AS Int32;\nSELECT id, title, tags, available, subtitle FROM books WHERE year = $year;", Parameters: []model.Parameter{{Name: "year", Type: i32}}, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: u64}, {Name: "title", Type: utf8}, {Name: "tags", Type: json}, {Name: "available", Type: timestamp}, {Name: "subtitle", Type: model.Optional(utf8)}}}}},
		{Name: "UpdateBook", Command: model.Exec, SQL: "DECLARE $id AS Uint64;\nDECLARE $tags AS Json;\nUPDATE books SET tags = $tags WHERE id = $id;", Parameters: []model.Parameter{{Name: "id", Type: u64}, {Name: "tags", Type: json}}},
	}}
}

func generatedFile(t *testing.T, files []model.File, name string) string {
	t.Helper()
	for _, file := range files {
		if file.Name == name {
			return string(file.Content)
		}
	}
	t.Fatalf("missing generated file %s", name)
	return ""
}

func TestGenerateYDBQuerierUsesNativeQueryClientContract(t *testing.T) {
	files, err := Generate(representativeAnalysis(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{
		"use super::models::*;",
		"pub struct Queries<'a>",
		"client: &'a mut ydb::QueryClient",
		"self.client.query_result_set(CREATE_BOOK)",
		`.param("$id", id)`,
		"row.remove_field(0)?.try_into()?",
		"self.client.query_result_set(BOOKS_BY_YEAR)",
		"for mut row in result_set.rows()",
		"self.client.exec(UPDATE_BOOK)",
		`.param("$tags", JsonParam(tags))`,
	} {
		if !strings.Contains(queries, want) {
			t.Errorf("generated queries missing %q:\n%s", want, queries)
		}
	}
	models := generatedFile(t, files, "models.rs")
	for _, want := range []string{"pub id: u64", "pub title: String", "pub tags: String", "pub available: std::time::SystemTime", "pub subtitle: Option<String>"} {
		if !strings.Contains(models, want) {
			t.Errorf("generated models missing %q:\n%s", want, models)
		}
	}
}

func TestDecodesQualifiedProjectionByResolvedOrdinal(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "BooksByTags", Command: model.Many, SQL: "SELECT b.book_id, a.name FROM books AS b LEFT JOIN authors AS a ON FALSE;",
		ResultSets: []model.ResultSet{{Columns: []model.Column{
			{Name: "book_id", Table: "books", Type: model.Type{Kind: "Uint64"}},
			{Name: "name", Table: "authors", Type: model.Optional(model.Type{Kind: "Utf8"})},
		}}},
	}}}
	files, err := Generate(a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{
		"book_id: row.remove_field(0)?.try_into()?",
		"name: row.remove_field(1)?.try_into()?",
	} {
		if !strings.Contains(queries, want) {
			t.Fatalf("projection decoding missing %q:\n%s", want, queries)
		}
	}
	if strings.Contains(queries, "remove_field_by_name") {
		t.Fatalf("qualified result labels must not be used for decoding:\n%s", queries)
	}
}

func TestJsonInputsUseExplicitYDBValues(t *testing.T) {
	files, err := Generate(representativeAnalysis(), Options{Runtime: "ydb"})
	if err != nil {
		t.Fatal(err)
	}
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{
		"tags: String",
		"struct JsonParam(String);",
		"ydb::Value::Json(value.0)",
		`.param("$tags", JsonParam(tags))`,
	} {
		if !strings.Contains(queries, want) {
			t.Fatalf("Json input must retain its YDB wire type; missing %q:\n%s", want, queries)
		}
	}
}

func TestOptionalJsonInputsUseConstructibleTypedValues(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "CreateAuthor", Command: model.Exec, SQL: "DECLARE $biography AS Optional<Json>; SELECT 1;",
		Parameters: []model.Parameter{{Name: "biography", Type: model.Optional(model.Type{Kind: "Json"})}},
	}}}
	files, err := Generate(a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{
		"biography: Option<String>",
		"#[derive(Default)]\nstruct JsonParam(String);",
		`.param("$biography", biography.map(JsonParam))`,
	} {
		if !strings.Contains(queries, want) {
			t.Fatalf("Optional<Json> must use the SDK's typed Option conversion; missing %q:\n%s", want, queries)
		}
	}
}

func TestTemporalInputsUseExactConstructibleYDBValues(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "StoreTimes", Command: model.Exec, SQL: "SELECT 1;",
		Parameters: []model.Parameter{
			{Name: "available", Type: model.Type{Kind: "Timestamp"}},
			{Name: "created_at", Type: model.Optional(model.Type{Kind: "Timestamp"})},
			{Name: "day", Type: model.Type{Kind: "Date"}},
		},
	}}}
	files, err := Generate(a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{
		"struct TimestampParam(std::time::SystemTime);",
		"ydb::Value::Timestamp(value.0)",
		`.param("$available", TimestampParam(available))`,
		`.param("$created_at", created_at.map(TimestampParam))`,
		"struct DateParam(std::time::SystemTime);",
		"ydb::Value::Date(value.0)",
	} {
		if !strings.Contains(queries, want) {
			t.Fatalf("temporal input must retain its YDB wire type; missing %q:\n%s", want, queries)
		}
	}
}

func TestGeneratedRawSQLRoundTripsThroughRustCompiler(t *testing.T) {
	sql := "-- Привет\r\nSELECT r###\"quoted\"###, '# hashes', '\x00', '\\n';\r\n"
	files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "SpecialSQL", Command: model.Exec, SQL: sql}}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = generatedFile(t, files, "queries.rs")
	var expected strings.Builder
	expected.WriteString("&[")
	for i, value := range []byte(sql) {
		if i != 0 {
			expected.WriteString(",")
		}
		fmt.Fprintf(&expected, "%d", value)
	}
	expected.WriteString("]")
	source := fmt.Sprintf("const SPECIAL_SQL: &str = %s;\nfn main() { assert_eq!(SPECIAL_SQL.as_bytes(), %s); }\n", rustString(sql), expected.String())
	dir := t.TempDir()
	path := filepath.Join(dir, "main.rs")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "roundtrip")
	if out, err := exec.Command("rustc", path, "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("rustc: %v\n%s\n%s", err, out, source)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("round trip: %v\n%s", err, out)
	}
}

func TestRejectsUnsupportedRuntimeTypeAndNames(t *testing.T) {
	aNew := representativeAnalysis()
	aNew.Queries[0].Name = "New"
	if _, err := Generate(aNew, Options{}); err == nil || !strings.Contains(err.Error(), "constructor") {
		t.Fatalf("constructor collision error: %v", err)
	}
	if _, err := Generate(representativeAnalysis(), Options{Runtime: "sqlx"}); err == nil || !strings.Contains(err.Error(), `unsupported runtime "sqlx"`) {
		t.Fatalf("runtime error: %v", err)
	}
	a := representativeAnalysis()
	a.Queries[0].Parameters[0].Type = model.Type{Kind: "Any"}
	if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), `query "CreateBook" parameter "id": unsupported YQL type "Any"`) {
		t.Fatalf("type error: %v", err)
	}
	a = representativeAnalysis()
	a.Queries[0].Name = "match"
	if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), "generated Rust name") {
		t.Fatalf("name error: %v", err)
	}
	a = representativeAnalysis()
	a.Queries[0].SQL = "SELECT '\xff';"
	if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), "SQL is not valid UTF-8") {
		t.Fatalf("UTF-8 error: %v", err)
	}
}

func TestRejectsExecRowsAndMalformedResults(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Affected", Command: model.ExecRows}}}
	if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), ":execrows is unsupported") {
		t.Fatalf("execrows error: %v", err)
	}
	a.Queries[0] = model.AnalyzedQuery{Name: "Missing", Command: model.One}
	if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), "expected one non-empty result set") {
		t.Fatalf("result error: %v", err)
	}
}

func TestGeneratedRustCompilesAgainstPinnedSDK(t *testing.T) {
	if os.Getenv("SQLC_YDB_RUST_SDK_CHECK") == "" {
		t.Skip("set SQLC_YDB_RUST_SDK_CHECK=1 to compile against the published SDK")
	}
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Fatal("SQLC_YDB_RUST_SDK_CHECK requires cargo")
	}
	files, err := Generate(representativeAnalysis(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(dir, "src", file.Name), file.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cargo := "[package]\nname = \"generated-check\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[dependencies]\nydb = \"=0.18.2\"\n"
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargo), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cargo", "check", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Rust does not compile against ydb 0.18.2: %v\n%s", err, out)
	}
}
