package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestListCreateTypeExpressionAndContextualFallback(t *testing.T) {
	for _, tc := range []struct {
		name, expr, want string
	}{
		{"direct", "ListCreate(List<String>?)", "List<Optional<List<String>>>"},
		{"fallback", "NVL(CAST(NULL AS List<String>?), ListCreate(List<String>?))", "List<String>"},
		{"unrelated element type", "NVL(CAST(NULL AS List<String>?), ListCreate(Int32))", "List<String>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := "-- name: Read :one\nSELECT " + tc.expr + " AS value;"
			got, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
			{
				value := got.Queries[0].ResultSets[0].Columns[0].Type.String()
				require.Equal(t, tc.want, value)
			}
		})
	}
}

func TestListCreateRequiresLiteralType(t *testing.T) {
	for _, expr := range []string{"ListCreate()", "ListCreate($type)", "ListCreate(1)", "ListCreate(String, Uint64)"} {
		query := "-- name: Read :one\nSELECT " + expr + " AS value;"
		_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: query}})
		require.ErrorContains(t, err, "ListCreate")
	}
}

func TestJsonSerializationFromList(t *testing.T) {
	query := "-- name: Read :one\nDECLARE $items AS List<String>;\nSELECT Yson::SerializeJson(Json::From($items)) AS payload;"
	got, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	{
		value := got.Queries[0].ResultSets[0].Columns[0].Type.String()
		require.Equal(t, "Optional<Json>", value)
	}
}
