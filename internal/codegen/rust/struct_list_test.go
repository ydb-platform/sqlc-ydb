package rust

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func structInput() *model.AnalysisResult {
	fields := []model.StructField{{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, {Name: "tags", Type: model.Type{Kind: "Json"}}, {Name: "available", Type: model.Type{Kind: "Timestamp"}}, {Name: "title", Type: model.Optional(model.Type{Kind: "Utf8"})}}
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "CreateBooks", Command: model.Exec, SQL: "INSERT INTO books SELECT * FROM AS_TABLE($books);", Parameters: []model.Parameter{{Name: "books", Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: fields}}}}}}}
}
func TestStructListGeneration(t *testing.T) {
	files, err := Generate(structInput(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	models := generatedFile(t, files, "models.rs")
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{"pub struct CreateBooksBooksItem", "pub book_id: u64", "pub tags: String", "pub title: Option<String>"} {
		if !strings.Contains(models, want) {
			t.Fatalf("missing %s\n%s", want, models)
		}
	}
	for _, want := range []string{"books: impl IntoIterator<Item = impl std::borrow::Borrow<CreateBooksBooksItem>>", "impl From<CreateBooksBooksItem> for ydb::Value", "JsonParam(item.tags)", "create_books_books_item_type()"} {
		if !strings.Contains(queries, want) {
			t.Fatalf("missing %s\n%s", want, queries)
		}
	}
}
func TestStructListRejectsNestedField(t *testing.T) {
	in := structInput()
	in.Queries[0].Parameters[0].Type.Elem.Fields[0].Type = model.Type{Kind: "List", Elem: &model.Type{Kind: "Uint64"}}
	_, err := Generate(in, Options{})
	if err == nil || !strings.Contains(err.Error(), "field book_id must be scalar") {
		t.Fatalf("err=%v", err)
	}
}
func TestStructListSDK(t *testing.T) {
	if os.Getenv("SQLC_YDB_RUST_SDK_CHECK") == "" {
		t.Skip("set SQLC_YDB_RUST_SDK_CHECK=1 for SDK compilation and value tests")
	}
	in := structInput()
	for _, kind := range []string{"Bool", "Int8", "Int16", "Int32", "Int64", "Uint8", "Uint16", "Uint32", "Uint64", "Float", "Double", "Utf8", "String", "Yson", "Json", "JsonDocument", "Date", "Datetime", "Timestamp", "Date32", "Datetime64", "Timestamp64"} {
		typ := model.Type{Kind: kind}
		q := in.Queries[0]
		q.Name = "Bind" + kind
		q.Parameters = []model.Parameter{{Name: "values", Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "value", Type: typ}, {Name: "optional_value", Type: model.Optional(typ)}}}}}}
		in.Queries = append(in.Queries, q)
	}
	files, err := Generate(in, Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, "src", f.Name), f.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Execute real SDK values, including typed empty list construction, without a server.
	runtime := `
#[cfg(test)]
mod struct_tests {
    use super::*;
    #[test]
    fn fields_and_empty_list() {
        let item = CreateBooksBooksItem { book_id: u64::MAX, tags: "{\"key\":1}".to_string(), available: std::time::SystemTime::UNIX_EPOCH, title: None };
        let value: ydb::Value = item.into();
        let ydb::Value::Struct(fields) = value.clone() else { panic!("expected struct") };
        let fields: std::collections::HashMap<String,ydb::Value> = fields.into();
        assert_eq!(fields["book_id"], ydb::Value::Uint64(u64::MAX));
        assert_eq!(fields["tags"], ydb::Value::Json("{\"key\":1}".to_string()));
        assert!(fields["title"].is_optional());
        assert_eq!(fields["title"].clone().to_option(), None);
        ydb::Value::list_from(create_books_books_item_type(), vec![value]).unwrap();
        ydb::Value::list_from(create_books_books_item_type(), vec![]).unwrap();
    }
}
`
	qfile := filepath.Join(dir, "src", "queries.rs")
	f, err := os.OpenFile(qfile, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(runtime)
	if closeErr := f.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	mod := "[package]\nname=\"struct-batch-check\"\nversion=\"0.0.0\"\nedition=\"2024\"\n[dependencies]\nydb=\"=0.18.2\"\nbon=\"=3.10.1\"\n"
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(mod), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cargo", "test", "--quiet", "--offline")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Rust SDK: %v\n%s", err, out)
	}
}

func TestStructListRustfmt(t *testing.T) {
	files, err := Generate(structInput(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var paths []string
	for _, f := range files {
		path := filepath.Join(dir, f.Name)
		if err := os.WriteFile(path, f.Content, 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	args := append([]string{"--edition", "2024", "--check"}, paths...)
	if out, err := exec.Command("rustfmt", args...).CombinedOutput(); err != nil {
		t.Fatalf("rustfmt: %v\n%s", err, out)
	}
}
