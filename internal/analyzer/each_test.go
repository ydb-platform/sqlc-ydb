package analyzer

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestEachSelect(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE devices (id Uint64 NOT NULL, name Utf8, PRIMARY KEY (id));"}}
	got, err := Analyze(schema, []model.Source{{Name: "queries.sql", Text: "-- name: Visit :each\nSELECT id, name FROM devices WHERE id >= $min_id ORDER BY id;"}})
	if err != nil {
		t.Fatal(err)
	}
	q := got.Queries[0]
	if q.Command != model.Each || len(q.Parameters) != 1 || q.Parameters[0].Type.Kind != "Uint64" || len(q.ResultSets) != 1 || len(q.ResultSets[0].Columns) != 2 || !q.ResultSets[0].Columns[1].Type.IsOptional() {
		t.Fatalf("unexpected analysis: %+v", q)
	}
	for _, sql := range []string{"DELETE FROM devices;", "UPDATE devices SET name = 'new' RETURNING id;", "UPSERT INTO devices (id) VALUES (1ul);"} {
		_, err := Analyze(schema, []model.Source{{Name: "queries.sql", Text: "-- name: Visit :each\n" + sql}})
		if err == nil || !strings.Contains(err.Error(), ":each requires a SELECT") {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	_, err = Analyze(schema, []model.Source{{Name: "queries.sql", Text: "-- name: Visit :each\nSELECT id FROM devices; SELECT id FROM devices;"}})
	if err == nil {
		t.Fatal("accepted multiple SELECTs")
	}
}
