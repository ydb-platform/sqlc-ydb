package rust

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func representativeAnalysis() *model.AnalysisResult {
	u64 := model.Type{Kind: "Uint64"}
	i32 := model.Type{Kind: "Int32"}
	utf8 := model.Type{Kind: "Utf8"}
	json := model.Type{Kind: "Json"}
	timestamp := model.Type{Kind: "Timestamp"}
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "CreateBook", Command: model.One, SQL: "DECLARE $id AS Uint64;\nINSERT INTO books (id) VALUES ($id) RETURNING id;", SQLWithoutDeclarations: "   \nINSERT INTO books (id) VALUES ($id) RETURNING id;", Parameters: []model.Parameter{{Name: "id", Type: u64}}, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: u64}}}}},
		{Name: "BooksByYear", Command: model.Many, SQL: "DECLARE $year AS Int32;\nSELECT id, title, tags, available, subtitle FROM books WHERE year = $year;", SQLWithoutDeclarations: "   \nSELECT id, title, tags, available, subtitle FROM books WHERE year = $year;", Parameters: []model.Parameter{{Name: "year", Type: i32}}, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: u64}, {Name: "title", Type: utf8}, {Name: "tags", Type: json}, {Name: "available", Type: timestamp}, {Name: "subtitle", Type: model.Optional(utf8)}}}}},
		{Name: "UpdateBook", Command: model.Exec, SQL: "DECLARE $id AS Uint64;\nDECLARE $tags AS Json;\nUPDATE books SET tags = $tags WHERE id = $id;", SQLWithoutDeclarations: "   \n   \nUPDATE books SET tags = $tags WHERE id = $id;", Parameters: []model.Parameter{{Name: "id", Type: u64}, {Name: "tags", Type: json}}},
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
		"pub struct Queries<'a, E: ydb::QueryExecutor>",
		"client: &'a mut E",
		"// -- name: CreateBook :one",
		".query_row(r\"INSERT INTO books (id) VALUES ($id) RETURNING id;\")",
		`.param("$id", id)`,
		"row.remove_field(0)?.try_into()?",
		".query_result_set(",
		".rows()\n            .map(|mut row| {",
		".collect()",
		".exec(r\"UPDATE books SET tags = $tags WHERE id = $id;\")",
		`.param("$tags", JsonParam(tags))`,
	} {
		if !strings.Contains(queries, want) {
			t.Errorf("generated queries missing %q:\n%s", want, queries)
		}
	}
	models := generatedFile(t, files, "models.rs")
	for _, want := range []string{"#[derive(Debug, Clone, PartialEq, Eq, Hash, PartialOrd, Ord)]", "pub id: u64", "pub title: String", "pub tags: String", "pub available: std::time::SystemTime", "pub subtitle: Option<String>"} {
		if !strings.Contains(models, want) {
			t.Errorf("generated models missing %q:\n%s", want, models)
		}
	}
}

func TestResultCopyDerive(t *testing.T) {
	for _, kind := range []string{"Bool", "Int64", "Uint64", "Timestamp", "Utf8", "Json", "JsonDocument", "String", "Yson"} {
		for _, optional := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/optional=%t", kind, optional), func(t *testing.T) {
				typ := model.Type{Kind: kind}
				if optional {
					typ = model.Optional(typ)
				}
				in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
					Name: "Result", Command: model.One, SQL: "SELECT value FROM test;",
					ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "value", Type: typ}}}},
				}}}
				files, err := Generate(in, Options{})
				if err != nil {
					t.Fatal(err)
				}
				wantCopy := kind == "Bool" || kind == "Int64" || kind == "Uint64" || kind == "Timestamp"
				models := generatedFile(t, files, "models.rs")
				if strings.Contains(models, ", Copy,") != wantCopy {
					t.Fatalf("unexpected Copy derive:\n%s", models)
				}
			})
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

func TestGetPrefixAndMultilineSQLMatchApprovedRustStyle(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "GetAuthor", Command: model.One,
		SQL:                    "-- name: GetAuthor :one\nINSERT INTO authors (id, name)\nVALUES ($id, $name)\nRETURNING id, name;",
		SQLWithoutDeclarations: "-- name: GetAuthor :one\nINSERT INTO authors (id, name)\nVALUES ($id, $name)\nRETURNING id, name;",
		Parameters:             []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: model.Type{Kind: "Utf8"}}},
		ResultSets:             []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: model.Type{Kind: "Utf8"}}}}},
	}}}
	files, err := Generate(a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{
		"// -- name: GetAuthor :one\n    #[builder(on(String, into))]\n    pub async fn author(",
		".query_row(\n                r\"\n                 INSERT INTO authors (id, name)\n                 VALUES ($id, $name)\n                 RETURNING id, name;\",\n            )",
	} {
		if !strings.Contains(queries, want) {
			t.Fatalf("approved Rust style missing %q:\n%s", want, queries)
		}
	}
	if strings.Contains(queries, "get_author") || strings.Contains(queries, "-- name: GetAuthor :one\n                 INSERT") {
		t.Fatalf("generated Rust API retained the get_ prefix or sent metadata as SQL:\n%s", queries)
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
		Name: "CreateAuthor", Command: model.Exec, SQL: "DECLARE $biography AS Optional<Json>; SELECT 1;", SQLWithoutDeclarations: " SELECT 1;",
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
		Name: "StoreTimes", Command: model.Exec, SQL: "SELECT 1;", SQLWithoutDeclarations: "SELECT 1;",
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
		`.param("$available", ydb::Value::Timestamp(available))`,
		`.param("$created_at", created_at.map(TimestampParam))`,
		`.param("$day", ydb::Value::Date(day))`,
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
	queries := generatedFile(t, files, "queries.rs")
	start := strings.Index(queries, ".exec(")
	if start < 0 || strings.Contains(queries, "pub const ") {
		t.Fatalf("expected inline SQL: %s", queries)
	}
	start += len(".exec(")
	end := strings.Index(queries[start:], ",\n            )")
	if end < 0 {
		t.Fatalf("missing inline SQL closing delimiter: %s", queries)
	}
	literal := queries[start : start+end]
	var expected strings.Builder
	expected.WriteString("&[")
	for i, value := range []byte(strings.Trim(sql, "\r\n")) {
		if i != 0 {
			expected.WriteString(",")
		}
		fmt.Fprintf(&expected, "%d", value)
	}
	expected.WriteString("]")
	source := fmt.Sprintf("fn main() { assert_eq!((%s).as_bytes(), %s); }\n", literal, expected.String())
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

func TestGeneratedRustIsRustfmtClean(t *testing.T) {
	files, err := Generate(representativeAnalysis(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	paths := make([]string, 0, len(files))
	for _, file := range files {
		path := filepath.Join(dir, file.Name)
		if err := os.WriteFile(path, file.Content, 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	args := append([]string{"--edition", "2024", "--check"}, paths...)
	if out, err := exec.Command("rustfmt", args...).CombinedOutput(); err != nil {
		t.Fatalf("generated Rust is not rustfmt-clean: %v\n%s", err, out)
	}
}

func TestGeneratedQueriesUseDeclarationFreeSQL(t *testing.T) {
	files, err := Generate(representativeAnalysis(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	queries := generatedFile(t, files, "queries.rs")
	if strings.Contains(queries, "DECLARE") {
		t.Fatalf("generated Rust query text must omit declarations supplied by typed SDK parameters:\n%s", queries)
	}
	if !strings.Contains(queries, "UPDATE books SET tags = $tags WHERE id = $id;") {
		t.Fatalf("generated Rust query text lost the executable statement:\n%s", queries)
	}
	for _, line := range strings.Split(queries, "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Fatalf("generated Rust contains trailing whitespace after removing declarations: %q", line)
		}
	}
}

func TestRejectsMissingDeclarationFreeSQLForParameterizedQuery(t *testing.T) {
	a := representativeAnalysis()
	a.Queries[0].SQLWithoutDeclarations = ""
	if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), "SQL without declarations") {
		t.Fatalf("missing declaration-free SQL error: %v", err)
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
	analysis := representativeAnalysis()
	scalar := model.Type{Kind: "Uint64"}
	analysis.Queries = append(analysis.Queries, model.AnalyzedQuery{
		Name: "FindIds", Command: model.Many,
		SQL:                    "SELECT id FROM sqlc_rust_list_items WHERE id IN $ids ORDER BY id;",
		SQLWithoutDeclarations: "SELECT id FROM sqlc_rust_list_items WHERE id IN $ids ORDER BY id;",
		Parameters:             []model.Parameter{{Name: "ids", Type: model.Type{Kind: "List", Elem: &scalar}}},
		ResultSets:             []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: scalar}}}},
	})
	for _, kind := range []string{"Bool", "Int8", "Int16", "Int32", "Int64", "Uint8", "Uint16", "Uint32", "Float", "Double", "Utf8", "String", "Yson", "Json", "JsonDocument", "Date", "Datetime", "Timestamp", "Date32", "Datetime64", "Timestamp64"} {
		elem := model.Type{Kind: kind}
		analysis.Queries = append(analysis.Queries, model.AnalyzedQuery{
			Name: "Bind" + kind, Command: model.Exec,
			SQL: "SELECT $values;", SQLWithoutDeclarations: "SELECT $values;",
			Parameters: []model.Parameter{{Name: "values", Type: model.Type{Kind: "List", Elem: &elem}}},
		})
	}
	files, err := Generate(analysis, Options{})
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
	format := exec.Command("rustfmt", "--edition", "2024", "--check", filepath.Join(dir, "src", "lib.rs"))
	if out, err := format.CombinedOutput(); err != nil {
		t.Fatalf("list output format: %v\n%s", err, out)
	}
	cargo := "[package]\nname = \"generated-check\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[dependencies]\nydb = \"=0.18.2\"\nbon = \"=3.10.1\"\ntokio = { version = \"1\", features = [\"macros\", \"rt-multi-thread\"] }\n"
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargo), 0600); err != nil {
		t.Fatal(err)
	}
	consumer := `#![recursion_limit = "256"]
use generated_check::queries::Queries;
async fn check(q: &mut Queries<'_, ydb::QueryClient>) -> ydb::YdbResult<()> {
    q.create_book().id(1).call().await?;
    q.update_book().id(1).tags("[]").call().await?;
    Ok(())
}
async fn check_tx(tx: &mut ydb::Transaction) -> ydb::YdbResult<()> {
    let mut q = Queries::new(tx);
    q.create_book().id(1).call().await?;
    q.update_book().id(1).tags("[]").call().await?;
    Ok(())
}
async fn lists(q: &mut Queries<'_, ydb::QueryClient>) -> ydb::YdbResult<()> {
    assert_eq!(q.find_ids().ids(vec![1, 2]).call().await?.len(), 2);
    let ids = [1u64, 2];
    assert_eq!(q.find_ids().ids(ids.as_slice()).call().await?.len(), 2);
    assert_eq!(q.find_ids().ids(std::collections::HashSet::from(ids)).call().await?.len(), 2);
    assert_eq!(q.find_ids().ids(ids.iter().filter(|id| **id == 2)).call().await?[0].id, 2);
    assert!(q.find_ids().ids(Vec::<u64>::new()).call().await?.is_empty());
    Ok(())
}
#[tokio::main]
async fn main() -> ydb::YdbResult<()> {
    let Ok(dsn) = std::env::var("YDB_CONNECTION_STRING") else { return Ok(()); };
    let client = ydb::ClientBuilder::new_from_connection_string(dsn)?.build().await?;
    let mut db = client.query_client();
    db.exec("CREATE TABLE sqlc_rust_list_items (id Uint64 NOT NULL, PRIMARY KEY(id));").await?;
    let result = async {
        db.exec("UPSERT INTO sqlc_rust_list_items (id) VALUES (1), (2);").await?;
        lists(&mut Queries::new(&mut db)).await
    }.await;
    db.exec("DROP TABLE sqlc_rust_list_items;").await?;
    result
}
`
	mainPath := filepath.Join(dir, "src", "main.rs")
	if err := os.WriteFile(mainPath, []byte(consumer), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cargo", "check", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Rust does not compile against ydb 0.18.2: %v\n%s", err, out)
	}
	if os.Getenv("YDB_CONNECTION_STRING") != "" {
		cmd = exec.Command("cargo", "run", "--quiet")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("live lists: %v\n%s", err, out)
		}
	}
	incomplete := strings.Replace(consumer, ".create_book().id(1).call()", ".create_book().call()", 1)
	if err := os.WriteFile(mainPath, []byte(incomplete), 0600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("cargo", "check", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "IsComplete") {
		t.Fatalf("missing required parameter must fail the builder completeness check: %v\n%s", err, out)
	}
}
