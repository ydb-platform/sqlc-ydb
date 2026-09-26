package golang

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func batchInput(fields ...model.StructField) *model.AnalysisResult {
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "CreateBooks", Command: model.Exec, SQL: "DECLARE $books AS List<Struct<book_id: Uint64, tags: Json, title: Optional<Utf8>>>; INSERT INTO books SELECT * FROM AS_TABLE($books);", Parameters: []model.Parameter{{Name: "books", Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: fields}}}}}}}
}

func structInput(fields ...model.StructField) *model.AnalysisResult {
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "UpdateBook", Command: model.Exec, SQL: "DECLARE $book AS Struct<title: Optional<Utf8>, payload: String, tags: Json>; SELECT $book;", Parameters: []model.Parameter{{Name: "book", Type: model.Type{Kind: "Struct", Fields: fields}}}}}}
}

func TestStructParameterAPIAndBinding(t *testing.T) {
	in := structInput(
		model.StructField{Name: "title", Type: model.Optional(model.Type{Kind: "Utf8"})},
		model.StructField{Name: "payload", Type: model.Type{Kind: "String"}},
		model.StructField{Name: "tags", Type: model.Type{Kind: "Json"}},
	)
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			files, err := Generate(in, Options{Package: "db", Runtime: runtime, EmitInterface: true, EmitJSONTags: true})
			require.NoError(t, err)
			source := ""
			for _, f := range files {
				source += string(f.Content)
			}
			for _, want := range []string{"type UpdateBookBook struct", "Title   *string", "Payload []byte", "Tags    string", "json:\"payload\"", "arg UpdateBookBook"} {
				require.Contains(t, source, want, "missing %q:\n%s", want, source)
			}
			runGeneratedRuntimeTest(t, in, Options{Package: "db", Runtime: runtime}, `package db
import("testing";"github.com/ydb-platform/ydb-go-sdk/v3/types")
func TestBindings(t *testing.T) {
 title:="Unicode ☀"; input:=UpdateBookBook{Title:&title,Payload:[]byte{0,1,255},Tags:"{\"ok\":true}"}
 value:=bindUpdateBookBook(input); fields,err:=types.StructFields(value);if err!=nil||len(fields)!=3{t.Fatalf("fields=%v err=%v",fields,err)}
 var titleGot string;if err:=types.CastTo(types.Unwrap(fields["title"]),&titleGot);err!=nil||titleGot!=title{t.Fatalf("title=%q err=%v",titleGot,err)}
 var payload []byte;if err:=types.CastTo(fields["payload"],&payload);err!=nil||string(payload)!=string(input.Payload)||fields["payload"].Type().Yql()!="String"{t.Fatalf("payload=%v err=%v",payload,err)}
 var tags string;if err:=types.CastTo(fields["tags"],&tags);err!=nil||tags!=input.Tags||fields["tags"].Type().Yql()!="Json"{t.Fatalf("tags=%q err=%v",tags,err)}
 nilValue:=bindUpdateBookBook(UpdateBookBook{});nilFields,err:=types.StructFields(nilValue);if err!=nil||!types.IsNull(nilFields["title"]){t.Fatalf("nil fields=%v err=%v",nilFields,err)}
}
`)
		})
	}
}

func TestStructParameterMembersBindByWireName(t *testing.T) {
	in := structInput(model.StructField{Name: "second", Type: model.Type{Kind: "Uint64"}}, model.StructField{Name: "first", Type: model.Type{Kind: "Utf8"}})
	for _, runtime := range []string{"ydb", "database/sql"} {
		runGeneratedRuntimeTest(t, in, Options{Package: "db", Runtime: runtime}, `package db
import("testing";"github.com/ydb-platform/ydb-go-sdk/v3/types")
func TestNames(t *testing.T){ fields,err:=types.StructFields(bindUpdateBookBook(UpdateBookBook{Second:2,First:"one"}));if err!=nil{t.Fatal(err)};var first string;var second uint64;if err:=types.CastTo(fields["first"],&first);err!=nil{t.Fatal(err)};if err:=types.CastTo(fields["second"],&second);err!=nil{t.Fatal(err)};if first!="one"||second!=2{t.Fatalf("first=%q second=%d",first,second)} }
`)
	}
}

func TestRenamedStructParameterMembersBindByWireName(t *testing.T) {
	in := structInput(model.StructField{Name: "title", Type: model.Type{Kind: "Utf8"}}, model.StructField{Name: "payload", Type: model.Type{Kind: "String"}})
	for _, runtime := range []string{"ydb", "database/sql"} {
		runGeneratedRuntimeTest(t, in, Options{Package: "db", Runtime: runtime, Rename: map[string]string{"title": "Heading", "payload": "Data"}}, `package db
import("testing";"github.com/ydb-platform/ydb-go-sdk/v3/types")
func TestNames(t *testing.T){ fields,err:=types.StructFields(bindUpdateBookBook(UpdateBookBook{Heading:"book",Data:[]byte("text")}));if err!=nil{t.Fatal(err)};var title string;var payload []byte;if err:=types.CastTo(fields["title"],&title);err!=nil{t.Fatal(err)};if err:=types.CastTo(fields["payload"],&payload);err!=nil{t.Fatal(err)};if title!="book"||string(payload)!="text"{t.Fatalf("title=%q payload=%q",title,payload)} }
`)
	}
}

func TestRenamedStructListMembersBindByWireName(t *testing.T) {
	in := batchInput(model.StructField{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, model.StructField{Name: "tags", Type: model.Type{Kind: "Json"}})
	for _, runtime := range []string{"ydb", "database/sql"} {
		runGeneratedRuntimeTest(t, in, Options{Package: "db", Runtime: runtime, Rename: map[string]string{"book_id": "BookIdentifier", "tags": "Labels"}}, `package db
import("testing";"github.com/ydb-platform/ydb-go-sdk/v3/types")
func TestNames(t *testing.T){ values,err:=types.ListItems(bindCreateBooksBooksItem([]CreateBooksBooksItem{{BookIdentifier:7,Labels:"{}"}}));if err!=nil{t.Fatal(err)};fields,err:=types.StructFields(values[0]);if err!=nil{t.Fatal(err)};var id uint64;var tags string;if err:=types.CastTo(fields["book_id"],&id);err!=nil{t.Fatal(err)};if err:=types.CastTo(fields["tags"],&tags);err!=nil{t.Fatal(err)};if id!=7||tags!="{}"{t.Fatalf("id=%d tags=%q",id,tags)} }
`)
	}
}

func TestStructParameterScalarFieldsCompile(t *testing.T) {
	var fields []model.StructField
	for _, kind := range []string{"Bool", "Int8", "Int16", "Int32", "Int64", "Uint8", "Uint16", "Uint32", "Uint64", "Float", "Double", "String", "Utf8", "Json", "JsonDocument", "Yson", "Date", "Datetime", "Timestamp", "Interval", "UUID", "Decimal"} {
		typ := model.Type{Kind: kind}
		if kind == "Decimal" {
			typ.Precision, typ.Scale = 22, 9
		}
		fields = append(fields, model.StructField{Name: "field_" + kind, Type: typ}, model.StructField{Name: "optional_" + kind, Type: model.Optional(typ)})
	}
	for _, runtime := range []string{"ydb", "database/sql"} {
		compileInput(t, structInput(fields...), Options{Package: "db", Runtime: runtime, EmitInterface: true})
	}
}

func TestStructParameterRejectsUnsupportedFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields []model.StructField
		want   string
	}{
		{"empty", nil, "requires at least one field"},
		{"collision", []model.StructField{{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, {Name: "bookID", Type: model.Type{Kind: "Uint64"}}}, "colliding field"},
		{"nested struct", []model.StructField{{Name: "nested", Type: model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "id", Type: model.Type{Kind: "Uint64"}}}}}}, "must be a scalar"},
		{"nested list", []model.StructField{{Name: "nested", Type: model.Type{Kind: "List", Elem: ptr(model.Type{Kind: "Uint64"})}}}, "must be a scalar"},
		{"double optional", []model.StructField{{Name: "nested", Type: model.Optional(model.Optional(model.Type{Kind: "Uint64"}))}}, "must be a scalar"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, runtime := range []string{"ydb", "database/sql"} {
				_, err := Generate(structInput(tc.fields...), Options{Runtime: runtime})
				require.ErrorContains(t, err, tc.want, "%s: %v", runtime, err)
			}
		})
	}
}

func TestStructParameterDeclarationCollision(t *testing.T) {
	in := structInput(model.StructField{Name: "id", Type: model.Type{Kind: "Uint64"}})
	in.Queries[0].Command = model.One
	in.Queries[0].Parameters[0].Name = "row"
	in.Queries[0].ResultSets = []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}}}}
	_, err := Generate(in, Options{Runtime: "ydb"})
	require.ErrorContains(t, err, "declaration UpdateBookRow collides", "collision: %v", err)
}

func TestStructParameterDecimalMetadataValidated(t *testing.T) {
	decimal := model.Type{Kind: "Decimal", Precision: 22, Scale: 9}
	in := structInput(model.StructField{Name: "amount", Type: decimal}, model.StructField{Name: "discount", Type: model.Optional(decimal)})
	for _, runtime := range []string{"ydb", "database/sql"} {
		runGeneratedRuntimeTest(t, in, Options{Package: "db", Runtime: runtime}, `package db
import("context";"strings";"testing";"github.com/ydb-platform/ydb-go-sdk/v3/types")
func TestMetadata(t *testing.T){good:=types.Decimal{Precision:22,Scale:9};bad:=types.Decimal{Precision:21,Scale:9};q:=New(nil);for _,tc:=range []struct{item UpdateBookBook;field string}{{UpdateBookBook{Amount:bad},"amount"},{UpdateBookBook{Amount:good,Discount:&bad},"discount"}}{err:=q.UpdateBook(context.Background(),tc.item);if err==nil||!strings.Contains(err.Error(),"$book."+tc.field+" expects Decimal(22,9)"){t.Fatalf("err=%v",err)}}}
`)
	}
}

func TestStructListParameterAPIAndBinding(t *testing.T) {
	in := batchInput(model.StructField{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, model.StructField{Name: "tags", Type: model.Type{Kind: "Json"}}, model.StructField{Name: "title", Type: model.Optional(model.Type{Kind: "Utf8"})})
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			files, err := Generate(in, Options{Package: "db", Runtime: runtime, EmitInterface: true, EmitJSONTags: true})
			require.NoError(t, err)
			source := ""
			for _, f := range files {
				source += string(f.Content)
			}
			sig := "CreateBooks(ctx context.Context, arg []CreateBooksBooksItem"
			if runtime == "ydb" {
				sig += ", opts ...query.ExecuteOption"
			}
			sig += ") error"
			for _, want := range []string{sig, "type CreateBooksBooksItem struct", "BookID uint64", "Tags   string", "Title  *string", "json:\"book_id\""} {
				require.Contains(t, source, want, "missing %q:\n%s", want, source)
			}
			runGeneratedRuntimeTest(t, in, Options{Package: "db", Runtime: runtime, EmitInterface: true}, `package db
import("testing";"github.com/ydb-platform/ydb-go-sdk/v3/types")
func TestBindings(t *testing.T) {
 title:="Unicode ☀"; input:=[]CreateBooksBooksItem{{BookID:^uint64(0),Tags:"{\"ok\":true}"},{BookID:1,Tags:"{}",Title:&title}}
 populated:=bindCreateBooksBooksItem(input)
 for _,empty:=range [][]CreateBooksBooksItem{nil,{}} {
  v:=bindCreateBooksBooksItem(empty)
  if v.Type().Yql()!=populated.Type().Yql() {t.Fatalf("empty type %s != populated %s",v.Type().Yql(),populated.Type().Yql())}
  rows,err:=types.ListItems(v);if err!=nil||len(rows)!=0 {t.Fatalf("empty=%v err=%v",rows,err)}
 }
 rows,err:=types.ListItems(populated);if err!=nil||len(rows)!=2 {t.Fatalf("rows=%v err=%v",rows,err)}
 for i,row:=range rows {
  fields,err:=types.StructFields(row);if err!=nil||len(fields)!=3 {t.Fatalf("fields=%v err=%v",fields,err)}
  var id uint64;if err:=types.CastTo(fields["book_id"],&id);err!=nil||id!=input[i].BookID {t.Fatalf("id=%v err=%v",id,err)}
  var tags string;if err:=types.CastTo(fields["tags"],&tags);err!=nil||tags!=input[i].Tags||fields["tags"].Type().Yql()!="Json" {t.Fatalf("tags=%v err=%v",tags,err)}
  if types.IsNull(fields["title"])!=(input[i].Title==nil) {t.Fatal("optional presence lost")}
  if i==1 { var got string;if err:=types.CastTo(types.Unwrap(fields["title"]),&got);err!=nil||got!=title {t.Fatalf("title=%v err=%v",got,err)} }
 }
}
`)
		})
	}
}

func TestStructListScalarFieldsCompile(t *testing.T) {
	var fields []model.StructField
	for _, kind := range []string{"Bool", "Int8", "Int16", "Int32", "Int64", "Uint8", "Uint16", "Uint32", "Uint64", "Float", "Double", "String", "Utf8", "Json", "JsonDocument", "Yson", "Date", "Datetime", "Timestamp", "Interval", "UUID", "Decimal"} {
		typ := model.Type{Kind: kind}
		if kind == "Decimal" {
			typ.Precision = 22
			typ.Scale = 9
		}
		fields = append(fields, model.StructField{Name: "field_" + kind, Type: typ}, model.StructField{Name: "optional_" + kind, Type: model.Optional(typ)})
	}
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			compileInput(t, batchInput(fields...), Options{Package: "db", Runtime: runtime, EmitInterface: true})
		})
	}
}

func TestStructListRejectsUnsupportedFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields []model.StructField
		want   string
	}{
		{"empty", nil, "requires at least one field"},
		{"collision", []model.StructField{{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, {Name: "bookID", Type: model.Type{Kind: "Uint64"}}}, "colliding field"},
		{"nested", []model.StructField{{Name: "nested", Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Uint64"}}}}, "must be a scalar"},
		{"dict", []model.StructField{{Name: "nested", Type: model.Type{Kind: "Dict", Key: &model.Type{Kind: "Utf8"}, Elem: &model.Type{Kind: "Uint64"}}}}, "must be a scalar"},
		{"optional dict", []model.StructField{{Name: "nested", Type: model.Optional(model.Type{Kind: "Dict", Key: &model.Type{Kind: "Utf8"}, Elem: &model.Type{Kind: "Uint64"}})}}, "must be a scalar"},
		{"double optional", []model.StructField{{Name: "nested", Type: model.Optional(model.Optional(model.Type{Kind: "Uint64"}))}}, "must be a scalar"},
		{"temporal", []model.StructField{{Name: "date", Type: model.Type{Kind: "Date32"}}}, "extended temporal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, runtime := range []string{"ydb", "database/sql"} {
				_, err := Generate(batchInput(tc.fields...), Options{Runtime: runtime})
				require.ErrorContains(t, err, tc.want, "%s: %v", runtime, err)
			}
		})
	}
}

func TestStructListDeclarationCollision(t *testing.T) {
	in := batchInput(model.StructField{Name: "id", Type: model.Type{Kind: "Uint64"}})
	other := in.Queries[0]
	other.Name = "Create"
	other.Parameters = []model.Parameter{{Name: "books_books", Type: in.Queries[0].Parameters[0].Type}}
	in.Queries = append(in.Queries, other)
	_, err := Generate(in, Options{Runtime: "ydb"})
	require.ErrorContains(t, err, "declaration CreateBooksBooksItem collides", "collision: %v", err)
}

func TestStructListDecimalMetadataValidated(t *testing.T) {
	decimal := model.Type{Kind: "Decimal", Precision: 22, Scale: 9}
	in := batchInput(model.StructField{Name: "amount", Type: decimal}, model.StructField{Name: "discount", Type: model.Optional(decimal)})
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			runGeneratedRuntimeTest(t, in, Options{Package: "db", Runtime: runtime}, `package db
import("context";"strings";"testing";"github.com/ydb-platform/ydb-go-sdk/v3/types")
func TestMetadata(t *testing.T) {
 good:=types.Decimal{Precision:22,Scale:9};bad:=types.Decimal{Precision:21,Scale:9};q:=New(nil)
 for _,tc:=range []struct{item CreateBooksBooksItem;field string}{{CreateBooksBooksItem{Amount:bad},"amount"},{CreateBooksBooksItem{Amount:good,Discount:&bad},"discount"}} {
  err:=q.CreateBooks(context.Background(),[]CreateBooksBooksItem{tc.item})
  if err==nil||!strings.Contains(err.Error(),"$books."+tc.field+" expects Decimal(22,9)") {t.Fatalf("err=%v",err)}
 }
}
`)
		})
	}
}

func TestStructListParameterNamesDoNotShadowRuntime(t *testing.T) {
	in := &model.AnalysisResult{}
	for i, name := range []string{"ctx", "q", "query", "type", "nil", "append", "books"} {
		q := batchInput(model.StructField{Name: "id", Type: model.Type{Kind: "Uint64"}}).Queries[0]
		q.Name = fmt.Sprintf("Insert%d", i)
		q.Parameters[0].Name = name
		in.Queries = append(in.Queries, q)
	}
	lookup := batchInput(model.StructField{Name: "id", Type: model.Type{Kind: "Uint64"}}).Queries[0]
	lookup.Name = "Lookup"
	lookup.Command = model.One
	lookup.Parameters[0].Name = "LookupRow"
	lookup.ResultSets = []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}}}}
	in.Queries = append(in.Queries, lookup)
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) { compileInput(t, in, Options{Package: "db", Runtime: runtime}) })
	}
}

func TestStructListUnsupportedScalarDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		typ  model.Type
		want string
	}{
		{model.Type{Kind: "Unknown"}, "field value:"},
		{model.Type{Kind: "Date32"}, "extended temporal type Date32 is unsupported"},
		{model.Type{Kind: "Decimal", Precision: 3, Scale: 4}, "Decimal"},
	} {
		for _, runtime := range []string{"ydb", "database/sql"} {
			_, err := Generate(batchInput(model.StructField{Name: "value", Type: tc.typ}), Options{Package: "db", Runtime: runtime})
			require.ErrorContains(t, err, tc.want, "%s %s: %v", runtime, tc.typ.String(), err)
		}
	}
}

func TestStructItemNameCanMatchUnusedCatalogTable(t *testing.T) {
	in := batchInput(model.StructField{Name: "book_id", Type: model.Type{Kind: "Uint64"}})
	in.Catalog.Tables = []model.Table{{Name: "create_books_books_item", Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}}}}
	for _, runtime := range []string{"ydb", "database/sql"} {
		runGeneratedRuntimeTest(t, in, Options{Package: "db", Runtime: runtime}, `package db
import "testing"
func TestItem(t *testing.T) { item:=CreateBooksBooksItem{BookID:42};if item.BookID!=42 {t.Fatal(item)} }
`)
	}
}
