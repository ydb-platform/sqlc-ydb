package golang

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func batchInput(fields ...model.StructField) *model.AnalysisResult {
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "CreateBooks", Command: model.Exec, SQL: "DECLARE $books AS List<Struct<book_id: Uint64, tags: Json, title: Optional<Utf8>>>; INSERT INTO books SELECT * FROM AS_TABLE($books);", Parameters: []model.Parameter{{Name: "books", Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: fields}}}}}}}
}

func TestStructListParameterAPIAndBinding(t *testing.T) {
	in := batchInput(model.StructField{Name: "book_id", Type: model.Type{Kind: "Uint64"}}, model.StructField{Name: "tags", Type: model.Type{Kind: "Json"}}, model.StructField{Name: "title", Type: model.Optional(model.Type{Kind: "Utf8"})})
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			files, err := Generate(in, Options{Package: "db", Runtime: runtime, EmitInterface: true, EmitJSONTags: true})
			if err != nil {
				t.Fatal(err)
			}
			source := ""
			for _, f := range files {
				source += string(f.Content)
			}
			sig := "CreateBooks(ctx context.Context, books []CreateBooksBooksItem"
			if runtime == "ydb" {
				sig += ", opts ...query.ExecuteOption"
			}
			sig += ") error"
			for _, want := range []string{sig, "type CreateBooksBooksItem struct", "BookID uint64", "Tags   string", "Title  *string", "json:\"book_id\""} {
				if !strings.Contains(source, want) {
					t.Fatalf("missing %q:\n%s", want, source)
				}
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
		{"double optional", []model.StructField{{Name: "nested", Type: model.Optional(model.Optional(model.Type{Kind: "Uint64"}))}}, "must be a scalar"},
		{"temporal", []model.StructField{{Name: "date", Type: model.Type{Kind: "Date32"}}}, "extended temporal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, runtime := range []string{"ydb", "database/sql"} {
				_, err := Generate(batchInput(tc.fields...), Options{Runtime: runtime})
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("%s: %v", runtime, err)
				}
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
	if err == nil || !strings.Contains(err.Error(), "declaration CreateBooksBooksItem collides") {
		t.Fatalf("collision: %v", err)
	}
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
