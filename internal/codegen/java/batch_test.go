package java

import (
	"strings"
	"testing"

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
	for _, runtime := range []string{"ydb", "jdbc"} {
		files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{batchQuery()}}, Options{Runtime: runtime})
		if err != nil {
			t.Fatal(err)
		}
		var code string
		for _, file := range files {
			code += string(file.Content)
		}
		for _, want := range []string{"CreateBooksBooksItem", "tech.ydb.table.values.ListType.of(tech.ydb.table.values.StructType.of(", "PrimitiveValue.newJson(", "PrimitiveValue.newTimestamp("} {
			if !strings.Contains(code, want) {
				t.Fatalf("%s missing %s in %s", runtime, want, code)
			}
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
