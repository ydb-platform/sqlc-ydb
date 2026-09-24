package analyzer

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
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
			require.NoError(t, err)
			require.Len(t, got.Queries, 1)
			require.Len(t, got.Queries[0].ResultSets, 1)
			require.Len(t, got.Queries[0].ResultSets[0].Columns, 1)
			assert.Equal(t, tt.parameters, got.Queries[0].Parameters)
			{
				result := got.Queries[0].ResultSets[0].Columns[0]
				assert.False(t, result.Name != "greeting" || !reflect.DeepEqual(result.Type, tt.result))
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
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestAnalyzeRejectsColumnReferencesWithoutTable(t *testing.T) {
	for _, test := range []struct{ query, want string }{
		{"SELECT COUNT(missing) AS n;", "aggregate functions require a FROM source"},
		{"SELECT 1 AS n WHERE missing = 1;", `unknown column "missing"`},
	} {
		_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Invalid :one\n" + test.query}})
		assert.ErrorContains(t, err, test.want)
	}
}
