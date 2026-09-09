package analyzer

import (
	"reflect"
	"testing"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

func TestAnalyzeInfersLimitAndOffsetParametersAsUint64(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Page :many
SELECT id FROM records LIMIT $limit OFFSET $offset;`}}

	got, err := Analyze(schema, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Parameter{
		{Name: "limit", Type: model.Type{Kind: "Uint64"}},
		{Name: "offset", Type: model.Type{Kind: "Uint64"}},
	}
	if parameters := got.Queries[0].Parameters; !reflect.DeepEqual(parameters, want) {
		t.Fatalf("parameters = %#v, want %#v", parameters, want)
	}
}

func TestAnalyzeInfersParametersInDirectInList(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));`}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: Selected :many
SELECT id FROM records WHERE id IN ($first, $second);`}}

	got, err := Analyze(schema, queries)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := []model.Parameter{
		{Name: "first", Type: model.Type{Kind: "Uint64"}},
		{Name: "second", Type: model.Type{Kind: "Uint64"}},
	}
	if parameters := got.Queries[0].Parameters; !reflect.DeepEqual(parameters, want) {
		t.Fatalf("parameters = %#v, want %#v", parameters, want)
	}
}
