package analyzer

import (
	"strings"
	"testing"

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
			if err != nil {
				t.Fatal(err)
			}
			if value := got.Queries[0].ResultSets[0].Columns[0].Type.String(); value != tc.want {
				t.Fatalf("type = %s, want %s", value, tc.want)
			}
		})
	}
}

func TestListCreateRequiresLiteralType(t *testing.T) {
	for _, expr := range []string{"ListCreate()", "ListCreate($type)", "ListCreate(1)", "ListCreate(String, Uint64)"} {
		query := "-- name: Read :one\nSELECT " + expr + " AS value;"
		_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: query}})
		if err == nil || !strings.Contains(err.Error(), "ListCreate") {
			t.Fatalf("%s error = %v", expr, err)
		}
	}
}

func TestJsonSerializationFromList(t *testing.T) {
	query := "-- name: Read :one\nDECLARE $items AS List<String>;\nSELECT Yson::SerializeJson(Json::From($items)) AS payload;"
	got, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: query}})
	if err != nil {
		t.Fatal(err)
	}
	if value := got.Queries[0].ResultSets[0].Columns[0].Type.String(); value != "Optional<Json>" {
		t.Fatalf("type = %s, want Optional<Json>", value)
	}
}
