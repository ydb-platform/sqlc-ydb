package kotlin

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func batchQuery() model.AnalyzedQuery {
	fields := []model.StructField{
		{Name: "book_id", Type: model.Type{Kind: "Uint64"}},
		{Name: "author_id", Type: model.Type{Kind: "Uint64"}},
		{Name: "isbn", Type: model.Type{Kind: "Utf8"}},
		{Name: "book_type", Type: model.Type{Kind: "Utf8"}},
		{Name: "title", Type: model.Type{Kind: "Utf8"}},
		{Name: "year", Type: model.Type{Kind: "Int32"}},
		{Name: "available", Type: model.Type{Kind: "Timestamp"}},
		{Name: "tags", Type: model.Type{Kind: "Json"}},
	}
	return model.AnalyzedQuery{Name: "CreateBooks", Command: model.Exec,
		SQL:        "INSERT INTO books SELECT * FROM AS_TABLE($books);",
		Parameters: []model.Parameter{{Name: "books", Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: fields}}}},
	}
}

func TestBatchInsertTypedCollection(t *testing.T) {
	for _, runtime := range []string{"ydb", "jdbc", "exposed"} {
		files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{batchQuery()}}, Options{Runtime: runtime})
		require.NoError(t, err)
		var code string
		for _, file := range files {
			code += string(file.Content)
		}
		for _, want := range []string{"CreateBooksBooksItem", "tech.ydb.table.values.ListType.of(\n", "tech.ydb.table.values.StructType.of(", "PrimitiveValue.newJson(", "PrimitiveValue.newTimestamp("} {
			require.Contains(t, code, want, "%s missing %s in %s", runtime, want, code)
		}
	}
}

func optionalBatchQuery() model.AnalyzedQuery {
	return model.AnalyzedQuery{Name: "OptionalBooks", Command: model.Exec, SQL: "SELECT * FROM AS_TABLE($books);", Parameters: []model.Parameter{{Name: "books", Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: []model.StructField{
		{Name: "rank", Type: model.Optional(model.Type{Kind: "Uint8"})},
		{Name: "data", Type: model.Optional(model.Type{Kind: "Json"})},
	}}}}}}
}

func listBooksQuery() model.AnalyzedQuery {
	q := batchQuery()
	q.Name = "ListBooks"
	q.SQL = "SELECT * FROM AS_TABLE($books);"
	return q
}

func declaredBatchQuery() model.AnalyzedQuery {
	q := batchQuery()
	q.Name = "DeclaredBooks"
	q.SQL = "DECLARE $books AS " + strings.ReplaceAll(q.Parameters[0].Type.String(), "`", "") + ";\n" + q.SQL
	q.DeclaredParameters = []string{"books"}
	return q
}
func declaredMixedQuery() model.AnalyzedQuery {
	return model.AnalyzedQuery{Name: "DeclaredMixed", Command: model.Exec, SQL: "DECLARE $z AS Uint64;\nSELECT $z, $a, $z;", DeclaredParameters: []string{"z"}, Parameters: []model.Parameter{{Name: "a", Type: model.Type{Kind: "Utf8"}}, {Name: "z", Type: model.Type{Kind: "Uint64"}}}}
}

func TestBatchOptionalSchemaAndRangeChecks(t *testing.T) {
	files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{optionalBatchQuery()}}, Options{Runtime: "jdbc"})
	require.NoError(t, err)
	var code string
	for _, f := range files {
		code += string(f.Content)
	}
	for _, want := range []string{"OptionalType.of(tech.ydb.table.values.PrimitiveType.Uint8)", "OptionalType.of(tech.ydb.table.values.PrimitiveType.Json)", `parameter \$books.rank is outside Uint8 range`, "_batchItem"} {
		require.Contains(t, code, want, "missing %q in %s", want, code)
	}
}

func TestBatchRejectsUnsupportedFieldsAndNames(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields []model.StructField
		want   string
	}{
		{"nested list", []model.StructField{{Name: "children", Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Uint64"}}}}, "CreateBooksBooksItem.children"},
		{"nested struct", []model.StructField{{Name: "child", Type: model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "id", Type: model.Type{Kind: "Uint64"}}}}}}, "CreateBooksBooksItem.child"},
		{"field collision", []model.StructField{{Name: "user_id", Type: model.Type{Kind: "Uint64"}}, {Name: "userId", Type: model.Type{Kind: "Uint64"}}}, "field name collision"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := batchQuery()
			q.Parameters[0].Type.Elem.Fields = tc.fields
			files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{q}}, Options{Runtime: "jdbc"})
			require.False(t, err == nil || files != nil || !strings.Contains(err.Error(), tc.want), "expected %q, got files=%v err=%v", tc.want, files, err)
		})
	}
}
