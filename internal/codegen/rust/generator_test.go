package rust

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
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
	require.FailNow(t, fmt.Sprintf("missing generated file %s", name))
	return ""
}

func TestGenerateEmbeddedResult(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE books (book_id Uint64 NOT NULL, author_id Uint64 NOT NULL, PRIMARY KEY(book_id)); CREATE TABLE authors (author_id Uint64 NOT NULL, name Utf8, PRIMARY KEY(author_id));`}}
	queries := []model.Source{{Name: "queries.sql", Text: `-- name: Read :many
SELECT sqlc.embed(b), b.author_id AS selected_author_id, sqlc.embed(a)
FROM books b JOIN authors a ON b.author_id = a.author_id;`}}
	analysis, err := analyzer.Analyze(schema, queries)
	require.NoError(t, err)
	files, err := Generate(analysis, Options{})
	require.NoError(t, err)
	models := generatedFile(t, files, "models.rs")
	queriesSource := generatedFile(t, files, "queries.rs")
	require.Contains(t, models, "pub struct Books {\n    pub book_id: u64,\n    pub author_id: u64,")
	require.Contains(t, models, "pub struct Authors {\n    pub author_id: u64,\n    pub name: Option<String>,")
	require.Contains(t, models, "pub struct ReadRow {\n    pub books: Books,\n    pub selected_author_id: u64,\n    pub authors: Authors,")
	require.Contains(t, queriesSource, "books: Books {\n")
	require.Contains(t, queriesSource, "book_id: row.remove_field(0)?.try_into()?")
	require.Contains(t, queriesSource, "selected_author_id: row.remove_field(2)?.try_into()?")
	require.Contains(t, queriesSource, "authors: Authors {\n")
	require.Contains(t, queriesSource, "name: row.remove_field(4)?.try_into()?")
	previous := -1
	for i := 0; i < 5; i++ {
		position := strings.Index(queriesSource, fmt.Sprintf("row.remove_field(%d)?.try_into()?", i))
		require.Greater(t, position, previous, "physical result column %d must decode in projection order", i)
		previous = position
	}
}

func TestGenerateYDBQuerierUsesNativeQueryClientContract(t *testing.T) {
	files, err := Generate(representativeAnalysis(), Options{})
	require.NoError(t, err)
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{
		"use super::models::*;",
		"pub struct Queries<'a, E: ydb::QueryExecutor>",
		"client: &'a mut E",
		"// -- name: CreateBook :one",
		"DECLARE $id AS Uint64;",
		"INSERT INTO books (id) VALUES ($id) RETURNING id;",
		`.param("$id", id)`,
		"row.remove_field(0)?.try_into()?",
		".query_result_set(",
		".rows()\n            .map(|mut row| {",
		".collect()",
		"DECLARE $tags AS Json;",
		"UPDATE books SET tags = $tags WHERE id = $id;",
		`.param("$tags", JsonParam(tags))`,
	} {
		assert.Contains(t, queries, want, "generated queries missing %q:\n%s", want, queries)
	}
	models := generatedFile(t, files, "models.rs")
	for _, want := range []string{"#[derive(Debug, Clone, PartialEq, Eq, Hash, PartialOrd, Ord)]", "pub id: u64", "pub title: String", "pub tags: String", "pub available: std::time::SystemTime", "pub subtitle: Option<String>"} {
		assert.Contains(t, models, want, "generated models missing %q:\n%s", want, models)
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
				require.NoError(t, err)
				wantCopy := kind == "Bool" || kind == "Int64" || kind == "Uint64" || kind == "Timestamp"
				models := generatedFile(t, files, "models.rs")
				require.Equal(t, wantCopy, strings.Contains(models, ", Copy,"), "unexpected Copy derive:\n%s", models)
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
	require.NoError(t, err)
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{
		"book_id: row.remove_field(0)?.try_into()?",
		"name: row.remove_field(1)?.try_into()?",
	} {
		require.Contains(t, queries, want, "projection decoding missing %q:\n%s", want, queries)
	}
	require.False(t, strings.Contains(queries, "remove_field_by_name"), "qualified result labels must not be used for decoding:\n%s", queries)
}

func TestGetPrefixAndMultilineSQLMatchApprovedRustStyle(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "GetAuthor", Command: model.One,
		SQL: "-- name: GetAuthor :one\nINSERT INTO authors (id, name)\nVALUES ($id, $name)\nRETURNING id, name;",

		Parameters: []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: model.Type{Kind: "Utf8"}}},
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: model.Type{Kind: "Utf8"}}}}},
	}}}
	files, err := Generate(a, Options{})
	require.NoError(t, err)
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{
		"// -- name: GetAuthor :one\n    #[builder(on(String, into))]\n    pub async fn author(",
		`.query_row(concat!(
                "INSERT INTO authors (id, name)\n",
                "VALUES ($id, $name)\n",
                "RETURNING id, name;",
            ))`,
	} {
		require.Contains(t, queries, want, "approved Rust style missing %q:\n%s", want, queries)
	}
	require.False(t, strings.Contains(queries, "get_author") || strings.Contains(queries, "-- name: GetAuthor :one\n                 INSERT"), "generated Rust API retained the get_ prefix or sent metadata as SQL:\n%s", queries)
}

func TestJsonInputsUseExplicitYDBValues(t *testing.T) {
	files, err := Generate(representativeAnalysis(), Options{Runtime: "ydb"})
	require.NoError(t, err)
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{
		"tags: String",
		"struct JsonParam(String);",
		"ydb::Value::Json(value.0)",
		`.param("$tags", JsonParam(tags))`,
	} {
		require.Contains(t, queries, want, "Json input must retain its YDB wire type; missing %q:\n%s", want, queries)
	}
}

func TestOptionalJsonInputsUseConstructibleTypedValues(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "CreateAuthor", Command: model.Exec, SQL: "DECLARE $biography AS Optional<Json>; SELECT 1;",
		Parameters: []model.Parameter{{Name: "biography", Type: model.Optional(model.Type{Kind: "Json"})}},
	}}}
	files, err := Generate(a, Options{})
	require.NoError(t, err)
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{
		"biography: Option<String>",
		"#[derive(Default)]\nstruct JsonParam(String);",
		`.param("$biography", biography.map(JsonParam))`,
	} {
		require.Contains(t, queries, want, "Optional<Json> must use the SDK's typed Option conversion; missing %q:\n%s", want, queries)
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
	require.NoError(t, err)
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{
		"struct TimestampParam(std::time::SystemTime);",
		"ydb::Value::Timestamp(value.0)",
		`.param("$available", ydb::Value::Timestamp(available))`,
		`.param("$created_at", created_at.map(TimestampParam))`,
		`.param("$day", ydb::Value::Date(day))`,
	} {
		require.Contains(t, queries, want, "temporal input must retain its YDB wire type; missing %q:\n%s", want, queries)
	}
}

func TestGeneratedSQLRoundTripsThroughRustCompiler(t *testing.T) {
	for _, sql := range []string{
		"-- Привет 😀\r\nSELECT r###\"quoted\"###, '# hashes', '\x00', '\\n';\r\n",
		"-- name: SpecialSQL :exec\n\n-- preserve comment\nDECLARE $books AS List<Struct<\n    book_id: Uint64, \n\tdata: Json\n>>;\n\nINSERT INTO books (book_id, data)\nSELECT\n    book_id,\n    data\nFROM AS_TABLE($books);  \n",
		"-- name: SpecialSQL :exec\nSELECT @@first line\n    value indentation\n\nlast line@@ AS value;\n",
	} {
		t.Run(fmt.Sprint(len(sql)), func(t *testing.T) {
			files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "SpecialSQL", Command: model.Exec, SQL: sql}}}, Options{})
			require.NoError(t, err)
			queries := generatedFile(t, files, "queries.rs")
			start := strings.Index(queries, ".exec(")
			require.False(t, start < 0, "inline SQL missing")
			start += len(".exec(")
			end := strings.Index(queries[start:], "\n            ))")
			require.False(t, end < 0, "inline SQL delimiter missing")
			literal := queries[start : start+end+len("\n            )")]
			lines := strings.Split(literal, "\n")
			for _, line := range lines[1 : len(lines)-1] {
				require.False(t, !strings.HasPrefix(line, "                \""), "SQL line lacks external code indent: %q", line)
			}
			var expected strings.Builder
			expected.WriteString("&[")
			for i, value := range []byte(model.WithoutQueryAnnotation(sql)) {
				if i != 0 {
					expected.WriteString(",")
				}
				fmt.Fprintf(&expected, "%d", value)
			}
			expected.WriteString("]")
			source := fmt.Sprintf("fn main() { assert_eq!((%s).as_bytes(), %s); }\n", literal, expected.String())
			dir := t.TempDir()
			path := filepath.Join(dir, "main.rs")
			require.NoError(t, os.WriteFile(path, []byte(source), 0600))
			bin := filepath.Join(dir, "roundtrip")
			if out, err := exec.Command("rustc", path, "-o", bin).CombinedOutput(); err != nil {
				require.NoError(t, err, "rustc: %v\n%s", err, out)
			}
			if out, err := exec.Command(bin).CombinedOutput(); err != nil {
				require.NoError(t, err, "round trip: %v\n%s", err, out)
			}
		})
	}
}

func TestGeneratedRustIsRustfmtClean(t *testing.T) {
	files, err := Generate(representativeAnalysis(), Options{})
	require.NoError(t, err)
	dir := t.TempDir()
	paths := make([]string, 0, len(files))
	for _, file := range files {
		path := filepath.Join(dir, file.Name)
		require.NoError(t, os.WriteFile(path, file.Content, 0600))
		paths = append(paths, path)
	}
	args := append([]string{"--edition", "2024", "--check"}, paths...)
	if out, err := exec.Command("rustfmt", args...).CombinedOutput(); err != nil {
		require.NoError(t, err, "generated Rust is not rustfmt-clean: %v\n%s", err, out)
	}
}

func TestGeneratedQueriesPreserveExplicitDeclarations(t *testing.T) {
	files, err := Generate(representativeAnalysis(), Options{})
	require.NoError(t, err)
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{"DECLARE $id AS Uint64;", "DECLARE $year AS Int32;", "DECLARE $tags AS Json;", "UPDATE books SET tags = $tags WHERE id = $id;"} {
		require.Contains(t, queries, want, "generated Rust query lost %q:\n%s", want, queries)
	}
}

func TestRejectsUnsupportedRuntimeTypeAndNames(t *testing.T) {
	aNew := representativeAnalysis()
	aNew.Queries[0].Name = "New"
	if _, err := Generate(aNew, Options{}); err == nil || !strings.Contains(err.Error(), "constructor") {
		require.FailNow(t, fmt.Sprintf("constructor collision error: %v", err))
	}
	if _, err := Generate(representativeAnalysis(), Options{Runtime: "sqlx"}); err == nil || !strings.Contains(err.Error(), `unsupported runtime "sqlx"`) {
		require.FailNow(t, fmt.Sprintf("runtime error: %v", err))
	}
	a := representativeAnalysis()
	a.Queries[0].Parameters[0].Type = model.Type{Kind: "Any"}
	if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), `query "CreateBook" parameter "id": unsupported YQL type "Any"`) {
		require.FailNow(t, fmt.Sprintf("type error: %v", err))
	}
	a = representativeAnalysis()
	a.Queries[0].Name = "match"
	if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), "generated Rust name") {
		require.FailNow(t, fmt.Sprintf("name error: %v", err))
	}
	a = representativeAnalysis()
	a.Queries[0].SQL = "SELECT '\xff';"
	if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), "SQL is not valid UTF-8") {
		require.FailNow(t, fmt.Sprintf("UTF-8 error: %v", err))
	}
}

func TestRejectsExecRowsAndMalformedResults(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Affected", Command: model.ExecRows}}}
	if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), ":execrows is unsupported") {
		require.FailNow(t, fmt.Sprintf("execrows error: %v", err))
	}
	a.Queries[0] = model.AnalyzedQuery{Name: "Missing", Command: model.One}
	if _, err := Generate(a, Options{}); err == nil || !strings.Contains(err.Error(), "expected one non-empty result set") {
		require.FailNow(t, fmt.Sprintf("result error: %v", err))
	}
}

func TestGeneratedRustCompilesAgainstPinnedSDK(t *testing.T) {
	if os.Getenv("SQLC_YDB_RUST_SDK_CHECK") == "" {
		t.Skip("set SQLC_YDB_RUST_SDK_CHECK=1 to compile against the published SDK")
	}
	if _, err := exec.LookPath("cargo"); err != nil {
		require.NoError(t, err, "SQLC_YDB_RUST_SDK_CHECK requires cargo")
	}
	analysis := representativeAnalysis()
	scalar := model.Type{Kind: "Uint64"}
	analysis.Queries = append(analysis.Queries, model.AnalyzedQuery{
		Name: "FindIds", Command: model.Many,
		SQL: "SELECT id FROM sqlc_rust_list_items WHERE id IN $ids ORDER BY id;",

		Parameters: []model.Parameter{{Name: "ids", Type: model.Type{Kind: "List", Elem: &scalar}}},
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: scalar}}}},
	})
	for _, kind := range []string{"Bool", "Int8", "Int16", "Int32", "Int64", "Uint8", "Uint16", "Uint32", "Float", "Double", "Utf8", "String", "Yson", "Json", "JsonDocument", "Date", "Datetime", "Timestamp", "Date32", "Datetime64", "Timestamp64"} {
		elem := model.Type{Kind: kind}
		analysis.Queries = append(analysis.Queries, model.AnalyzedQuery{
			Name: "Bind" + kind, Command: model.Exec,
			SQL:        "SELECT $values;",
			Parameters: []model.Parameter{{Name: "values", Type: model.Type{Kind: "List", Elem: &elem}}},
		})
	}
	files, err := Generate(analysis, Options{})
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "src"), 0700))
	for _, file := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "src", file.Name), file.Content, 0600))
	}
	format := exec.Command("rustfmt", "--edition", "2024", "--check", filepath.Join(dir, "src", "lib.rs"))
	if out, err := format.CombinedOutput(); err != nil {
		require.NoError(t, err, "list output format: %v\n%s", err, out)
	}
	cargo := "[package]\nname = \"generated-check\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[dependencies]\nydb = \"=0.18.2\"\nbon = \"=3.10.1\"\ntokio = { version = \"1\", features = [\"macros\", \"rt-multi-thread\"] }\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargo), 0600))
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
	require.NoError(t, os.WriteFile(mainPath, []byte(consumer), 0600))
	cmd := exec.Command("cargo", "check", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		require.NoError(t, err, "generated Rust does not compile against ydb 0.18.2: %v\n%s", err, out)
	}
	if os.Getenv("YDB_CONNECTION_STRING") != "" {
		cmd = exec.Command("cargo", "run", "--quiet")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			require.NoError(t, err, "live lists: %v\n%s", err, out)
		}
	}
	incomplete := strings.Replace(consumer, ".create_book().id(1).call()", ".create_book().call()", 1)
	require.NoError(t, os.WriteFile(mainPath, []byte(incomplete), 0600))
	cmd = exec.Command("cargo", "check", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "IsComplete") {
		require.FailNow(t, fmt.Sprintf("missing required parameter must fail the builder completeness check: %v\n%s", err, out))
	}
}

func TestFloatingPointResultsCompileAndRetainPartialComparison(t *testing.T) {
	var queries []model.AnalyzedQuery
	for _, kind := range []string{"Float", "Double"} {
		scalar := model.Type{Kind: kind}
		for i, typ := range []model.Type{scalar, model.Optional(scalar), {Kind: "List", Elem: &scalar}} {
			queries = append(queries, model.AnalyzedQuery{
				Name: fmt.Sprintf("Read%s%d", kind, i), Command: model.One, SQL: "SELECT value FROM readings;",
				ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "value", Type: typ}}}},
			})
		}
	}
	files, err := Generate(&model.AnalysisResult{Queries: queries}, Options{})
	require.NoError(t, err)
	dir := t.TempDir()
	source := generatedFile(t, files, "models.rs") + `
fn main() {
    let x = ReadFloat0Row { value: 1.5 };
    assert_eq!(x, x.clone());
    assert!(x < ReadFloat0Row { value: 2.0 });
    assert_ne!(ReadDouble0Row { value: f64::NAN }, ReadDouble0Row { value: f64::NAN });
    assert!(ReadFloat2Row { value: vec![1.0] } < ReadFloat2Row { value: vec![2.0] });
}
`
	path, bin := filepath.Join(dir, "main.rs"), filepath.Join(dir, "main")
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	if out, err := exec.Command("rustc", path, "-o", bin).CombinedOutput(); err != nil {
		require.NoError(t, err, "floating-point result models do not compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		require.NoError(t, err, "floating-point comparison contract failed: %v\n%s", err, out)
	}
}
