package rust

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func structInput() *model.AnalysisResult {
	fields := []model.StructField{{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, {Name: "tags", Type: model.Type{Kind: "Json"}}, {Name: "available", Type: model.Type{Kind: "Timestamp"}}, {Name: "title", Type: model.Optional(model.Type{Kind: "Utf8"})}}
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "CreateBooks", Command: model.Exec, SQL: "INSERT INTO books SELECT * FROM AS_TABLE($books);", Parameters: []model.Parameter{{Name: "books", Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: fields}}}}}}}
}
func TestStructListGeneration(t *testing.T) {
	files, err := Generate(structInput(), Options{})
	require.NoError(t, err)
	models := generatedFile(t, files, "models.rs")
	queries := generatedFile(t, files, "queries.rs")
	for _, want := range []string{"pub struct CreateBooksBooksItem", "pub book_id: u64", "pub tags: String", "pub title: Option<String>"} {
		require.Contains(t, models, want, "missing %s\n%s", want, models)
	}
	for _, want := range []string{"books: impl IntoIterator<Item = impl std::borrow::Borrow<CreateBooksBooksItem>>", "impl From<CreateBooksBooksItem> for ydb::Value", "JsonParam(item.tags)", "create_books_books_item_type()"} {
		require.Contains(t, queries, want, "missing %s\n%s", want, queries)
	}
}
func TestStructListRejectsNestedField(t *testing.T) {
	in := structInput()
	in.Queries[0].Parameters[0].Type.Elem.Fields[0].Type = model.Type{Kind: "List", Elem: &model.Type{Kind: "Uint64"}}
	_, err := Generate(in, Options{})
	require.ErrorContains(t, err, "field book_id must be scalar", "err=%v", err)
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
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "src"), 0700))
	for _, f := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "src", f.Name), f.Content, 0600))
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
        let expected = ydb::Value::struct_from_fields(vec![
            ("book_id".to_string(), ydb::Value::Uint64(0)),
            ("tags".to_string(), ydb::Value::Json(String::new())),
            ("available".to_string(), ydb::Value::Timestamp(std::time::SystemTime::UNIX_EPOCH)),
            ("title".to_string(), ydb::Value::from(None::<String>)),
        ]);
        assert_eq!(create_books_books_item_type(), expected);
        let empty = ydb::Value::list_from(create_books_books_item_type(), vec![]).unwrap();
        assert_eq!(empty, ydb::Value::list_from(expected, vec![]).unwrap());
        let wrong = ydb::Value::struct_from_fields(vec![
            ("book_id".to_string(), ydb::Value::Uint64(0)),
            ("tags".to_string(), ydb::Value::Json(String::new())),
            ("available".to_string(), ydb::Value::Timestamp(std::time::SystemTime::UNIX_EPOCH)),
            ("title".to_string(), ydb::Value::from(None::<u64>)),
        ]);
        assert_ne!(empty, ydb::Value::list_from(wrong, vec![]).unwrap());
    }
}
`
	qfile := filepath.Join(dir, "src", "queries.rs")
	f, err := os.OpenFile(qfile, os.O_APPEND|os.O_WRONLY, 0600)
	require.NoError(t, err)
	_, err = f.WriteString(runtime)
	if closeErr := f.Close(); closeErr != nil {
		require.NoError(t, closeErr, fmt.Sprint(closeErr))
	}
	require.NoError(t, err)
	mod := "[package]\nname=\"struct-batch-check\"\nversion=\"0.0.0\"\nedition=\"2024\"\n[dependencies]\nydb=\"=0.18.2\"\nbon=\"=3.10.1\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(mod), 0600))
	cmd := exec.Command("cargo", "test", "--quiet", "--offline")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		require.NoError(t, err, "Rust SDK: %v\n%s", err, out)
	}
}

func TestStructListRustfmt(t *testing.T) {
	files, err := Generate(structInput(), Options{})
	require.NoError(t, err)
	dir := t.TempDir()
	var paths []string
	for _, f := range files {
		path := filepath.Join(dir, f.Name)
		require.NoError(t, os.WriteFile(path, f.Content, 0600))
		paths = append(paths, path)
	}
	args := append([]string{"--edition", "2024", "--check"}, paths...)
	if out, err := exec.Command("rustfmt", args...).CombinedOutput(); err != nil {
		require.NoError(t, err, "rustfmt: %v\n%s", err, out)
	}
}

func TestStructListFieldDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields []model.StructField
		want   string
	}{
		{"empty", nil, "requires at least one field"},
		{"keyword", []model.StructField{{Name: "type", Type: model.Type{Kind: "Uint64"}}}, "invalid or colliding field"},
		{"collision", []model.StructField{{Name: "bookID", Type: model.Type{Kind: "Uint64"}}, {Name: "book_id", Type: model.Type{Kind: "Uint64"}}}, "invalid or colliding field"},
		{"unsupported scalar", []model.StructField{{Name: "amount", Type: model.Type{Kind: "Decimal", Precision: 22, Scale: 9}}}, "unsupported YQL type"},
		{"nested optional", []model.StructField{{Name: "id", Type: model.Optional(model.Optional(model.Type{Kind: "Uint64"}))}}, "must be scalar or Optional<scalar>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := structInput()
			in.Queries[0].Parameters[0].Type.Elem.Fields = tc.fields
			_, err := Generate(in, Options{})
			require.ErrorContains(t, err, tc.want, "got %v, want %s", err, tc.want)
		})
	}
}

func TestStructListItemNameCollisionAcrossQueries(t *testing.T) {
	in := structInput()
	other := structInput().Queries[0]
	other.Name = "Create"
	other.Parameters[0].Name = "books_books"
	in.Queries = append(in.Queries, other)
	_, err := Generate(in, Options{})
	require.ErrorContains(t, err, "model name collision", "err=%v", err)
}
