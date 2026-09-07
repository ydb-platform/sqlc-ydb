package analyzer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

func TestAnalyzeTablelessScalarSelect(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		parameters []model.Parameter
		result     model.Type
	}{
		{
			name:   "literal",
			query:  `SELECT "hello"u AS greeting;`,
			result: model.Type{Kind: "Utf8"},
		},
		{
			name:       "utf8 concatenation",
			query:      "DECLARE $name AS Utf8;\n" + `SELECT "hello "u || $name AS greeting;`,
			parameters: []model.Parameter{{Name: "name", Type: model.Type{Kind: "Utf8"}}},
			result:     model.Type{Kind: "Utf8"},
		},
		{
			name:       "string concatenation",
			query:      "DECLARE $suffix AS String;\n" + `SELECT "hello" || $suffix AS greeting;`,
			parameters: []model.Parameter{{Name: "suffix", Type: model.Type{Kind: "String"}}},
			result:     model.Type{Kind: "String"},
		},
		{
			name:       "optional operand",
			query:      "DECLARE $name AS Optional<Utf8>;\n" + `SELECT "hello "u || $name AS greeting;`,
			parameters: []model.Parameter{{Name: "name", Type: model.Optional(model.Type{Kind: "Utf8"})}},
			result:     model.Optional(model.Type{Kind: "Utf8"}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: SayHello :one\n" + tt.query}})
			if err != nil {
				t.Fatalf("Analyze() error = %v", err)
			}
			if len(got.Queries) != 1 || len(got.Queries[0].ResultSets) != 1 || len(got.Queries[0].ResultSets[0].Columns) != 1 {
				t.Fatalf("Queries = %#v", got.Queries)
			}
			if !reflect.DeepEqual(got.Queries[0].Parameters, tt.parameters) {
				t.Errorf("Parameters = %#v, want %#v", got.Queries[0].Parameters, tt.parameters)
			}
			if result := got.Queries[0].ResultSets[0].Columns[0]; result.Name != "greeting" || !reflect.DeepEqual(result.Type, tt.result) {
				t.Errorf("result = %#v, want greeting %#v", result, tt.result)
			}
		})
	}
}

func TestAnalyzeRejectsUnsupportedConcatenationOperands(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "different string families",
			query: "DECLARE $suffix AS String;\n" + `SELECT "hello"u || $suffix AS greeting;`,
			want:  "concatenation operands must both be String or both be Utf8",
		},
		{
			name:  "non string",
			query: "DECLARE $value AS Uint64;\n" + `SELECT "hello"u || $value AS greeting;`,
			want:  "concatenation operand $value has unsupported type Uint64",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Invalid :one\n" + tt.query}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestAnalyzeRejectsColumnReferencesWithoutTable(t *testing.T) {
	for _, query := range []string{
		"SELECT COUNT(missing) AS n;",
		"SELECT 1 AS n WHERE missing = 1;",
	} {
		_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Invalid :one\n" + query}})
		if err == nil || !strings.Contains(err.Error(), `unknown column "missing"`) {
			t.Errorf("query %q error = %v", query, err)
		}
	}
}
