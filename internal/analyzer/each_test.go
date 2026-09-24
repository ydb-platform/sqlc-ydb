package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestEachSelect(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE devices (id Uint64 NOT NULL, name Utf8, PRIMARY KEY (id));"}}
	got, err := Analyze(schema, []model.Source{{Name: "queries.sql", Text: "-- name: Visit :each\nSELECT id, name FROM devices WHERE id >= $min_id ORDER BY id;"}})
	require.NoError(t, err)
	q := got.Queries[0]
	require.Equal(t, model.Each, q.Command)
	require.Len(t, q.Parameters, 1)
	require.Equal(t, "Uint64", q.Parameters[0].Type.Kind)
	require.Len(t, q.ResultSets, 1)
	require.Len(t, q.ResultSets[0].Columns, 2)
	require.True(t, q.ResultSets[0].Columns[1].Type.IsOptional())
	for _, sql := range []string{"DELETE FROM devices;", "UPDATE devices SET name = 'new' RETURNING id;", "UPSERT INTO devices (id) VALUES (1ul);"} {
		_, err := Analyze(schema, []model.Source{{Name: "queries.sql", Text: "-- name: Visit :each\n" + sql}})
		require.ErrorContains(t, err, ":each requires a SELECT")
	}
	_, err = Analyze(schema, []model.Source{{Name: "queries.sql", Text: "-- name: Visit :each\nSELECT id FROM devices; SELECT id FROM devices;"}})
	require.Error(t, err)
}

func TestMalformedAnnotationListsEach(t *testing.T) {
	_, err := Analyze(nil, []model.Source{{Name: "queries.sql", Text: "-- name: Visit :each extra\nSELECT 1;"}})
	require.ErrorContains(t, err, "expected -- name: QueryName :one|:many|:each|:exec|:execrows")
}
