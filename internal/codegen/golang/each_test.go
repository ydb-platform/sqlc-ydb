package golang

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func eachInput() *model.AnalysisResult {
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "Visit", Command: model.Each, SQL: "-- name: Visit :each\nSELECT id, name FROM devices;", Source: model.Position{File: "queries.sql"},
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: model.Optional(model.Type{Kind: "Utf8"})}}}},
	}}}
}

func TestEachRuntime(t *testing.T) {
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			filename := "native.txt"
			if runtime == "database/sql" {
				filename = "sql.txt"
			}
			source, err := os.ReadFile(filepath.Join("testdata", "each", filename))
			if err != nil {
				t.Fatal(err)
			}
			runGeneratedRuntimeTest(t, eachInput(), Options{Package: "db", Runtime: runtime, EmitInterface: true}, string(source))
		})
	}
}

func TestEachShapes(t *testing.T) {
	for _, runtime := range []string{"ydb", "database/sql"} {
		in := eachInput()
		q := in.Queries[0]
		for _, n := range []int{1, 2} {
			q.Name += "Arg"
			q.Parameters = append(q.Parameters, model.Parameter{Name: strings.Repeat("id", n), Type: model.Type{Kind: "Uint64"}})
			in.Queries = append(in.Queries, q)
		}
		runGeneratedRuntimeTest(t, in, Options{Package: "db", Runtime: runtime, EmitInterface: true}, "package db\nvar _ Querier = (*Queries)(nil)\n")
		decimal := model.Type{Kind: "Decimal", Precision: 22, Scale: 9}
		in.Queries[0].Parameters = []model.Parameter{{Name: "amount", Type: decimal}}
		runGeneratedRuntimeTest(t, in, Options{Package: "db", Runtime: runtime}, `package db
import ("context"; "strings"; "testing"; "github.com/ydb-platform/ydb-go-sdk/v3/types")
func TestEachDecimalValidation(t *testing.T) {
 err := New(nil).Visit(context.Background(), types.Decimal{Precision:21,Scale:9}, func(VisitRow)error{return nil})
 if err == nil || !strings.Contains(err.Error(), "expects Decimal(22,9)") { t.Fatalf("%v",err) }
 if wantNative := `+fmt.Sprint(runtime == "ydb")+`; wantNative && strings.Count(err.Error(), "(*Queries).Visit") != 1 { t.Fatalf("expected one query stack frame: %v",err) }
}
`)
		in.Queries[0].ResultSets = nil
		if _, err := Generate(in, Options{Runtime: runtime}); err == nil || !strings.Contains(err.Error(), "requires one non-empty result set") {
			t.Fatalf("%s: %v", runtime, err)
		}
	}
	in := eachInput()
	in.Queries[0].Parameters = []model.Parameter{{Name: "row", Type: model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "id", Type: model.Type{Kind: "Uint64"}}}}}}
	if _, err := Generate(in, Options{}); err == nil || !strings.Contains(err.Error(), "VisitRow") {
		t.Fatalf("row/parameter declaration collision: %v", err)
	}
}
