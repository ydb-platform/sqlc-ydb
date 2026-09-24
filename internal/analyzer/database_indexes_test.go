package analyzer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func indexedDatabaseTable() model.Table {
	return model.Table{
		Columns: []model.Column{
			{Name: "id", Type: model.Type{Kind: "Uint64"}},
			{Name: "label", Type: model.Type{Kind: "Utf8"}},
			{Name: "payload", Type: model.Type{Kind: "Utf8"}},
			{Name: "extra", Type: model.Optional(model.Type{Kind: "Utf8"})},
			{Name: "note", Type: model.Optional(model.Type{Kind: "Utf8"})},
		},
		PrimaryKey: []string{"id"},
		Indexes: []model.Index{
			{Name: "by_label", Kind: "GlobalSync", Columns: []string{"label", "id"}, DataColumns: []string{"payload", "extra"}},
			{Name: "by_payload", Kind: "GlobalAsync", Columns: []string{"payload"}},
		},
	}
}

const indexedDatabaseSchema = `CREATE TABLE records (
    id Uint64 NOT NULL,
    label Utf8 NOT NULL,
    payload Utf8 NOT NULL,
    extra Utf8,
    note Utf8,
    PRIMARY KEY (id),
    INDEX by_label GLOBAL SYNC ON (label, id) COVER (payload, extra),
    INDEX by_payload GLOBAL ASYNC ON (payload)
);`

func TestDatabaseAnalysisIndexDiscoveryAndOfflineParity(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: indexedDatabaseSchema}}
	for _, sql := range []string{
		"-- name: Read :many\nDECLARE $label AS Utf8; SELECT r.* FROM records VIEW by_label AS r WHERE r.label = $label;",
		"-- name: Read :many\nSELECT id, payload FROM `records` VIEW `by_payload`;",
		"-- name: Read :many\nSELECT l.id AS id, r.payload AS payload FROM records VIEW by_label AS l JOIN records VIEW by_payload AS r ON l.id = r.id;",
		"-- name: Copy :exec\nUPSERT INTO records (id, label, payload) SELECT id, label, payload FROM records VIEW by_label;",
	} {
		t.Run(sql, func(t *testing.T) {
			queries := []model.Source{{Name: "query.sql", Text: sql}}
			offline, err := Analyze(schema, queries)
			require.NoError(t, err)
			for _, local := range []bool{false, true} {
				database := &fakeAnalysisDatabase{tables: map[string]model.Table{"records": indexedDatabaseTable()}}
				var sources []model.Source
				if local {
					sources = schema
				}
				connected, err := AnalyzeWithDatabase(context.Background(), sources, queries, Options{}, database)
				require.NoError(t, err)
				require.Equal(t, []string{"records"}, database.described)
				require.Equal(t, []string{sql}, database.validated)
				want, got := offline.Queries[0], connected.Queries[0]
				require.Equal(t, want.SQL, got.SQL)
				require.Equal(t, want.Parameters, got.Parameters)
				require.Equal(t, want.ResultSets, got.ResultSets)
				require.Equal(t, offline.Catalog.Tables[0].Indexes, connected.Catalog.Tables[0].Indexes)
			}
		})
	}
}

func TestDatabaseAnalysisIndexDrift(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: indexedDatabaseSchema}}
	query := []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT id FROM records VIEW by_label;"}}
	for _, tc := range []struct {
		name   string
		change func(*model.Table)
		want   string
	}{
		{"missing", func(table *model.Table) { table.Indexes = table.Indexes[1:] }, `index "by_label" is missing from the database`},
		{"extra", func(table *model.Table) {
			table.Indexes = append(table.Indexes, model.Index{Name: "extra_index", Kind: "GlobalSync", Columns: []string{"extra"}})
		}, `database index "extra_index" is missing from the local schema`},
		{"kind", func(table *model.Table) { table.Indexes[0].Kind = "GlobalAsync" }, `index "by_label" has local kind GlobalSync and database kind GlobalAsync`},
		{"key order", func(table *model.Table) { table.Indexes[0].Columns = []string{"id", "label"} }, `local key columns [label id] and database key columns [id label]`},
		{"key member", func(table *model.Table) { table.Indexes[0].Columns = []string{"label", "payload"} }, `local key columns [label id] and database key columns [label payload]`},
		{"cover missing", func(table *model.Table) { table.Indexes[0].DataColumns = []string{"payload"} }, `local covering columns [payload extra] and database covering columns [payload]`},
		{"cover different", func(table *model.Table) { table.Indexes[0].DataColumns = []string{"payload", "note"} }, `local covering columns [payload extra] and database covering columns [payload note]`},
		{"cover order is not drift", func(table *model.Table) { table.Indexes[0].DataColumns = []string{"extra", "payload"} }, ""},
		{"index order is not drift", func(table *model.Table) { table.Indexes[0], table.Indexes[1] = table.Indexes[1], table.Indexes[0] }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			table := indexedDatabaseTable()
			tc.change(&table)
			database := &fakeAnalysisDatabase{tables: map[string]model.Table{"records": table}}
			result, err := AnalyzeWithDatabase(context.Background(), schema, query, Options{}, database)
			if tc.want == "" {
				require.NoError(t, err)
				require.Equal(t, 1, len(result.Queries))
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), "database schema drift")
			require.Contains(t, err.Error(), tc.want)
			require.Contains(t, err.Error(), "query.sql:2:")
			require.Len(t, result.Queries, 0)
		})
	}
}

func TestDatabaseAnalysisUnknownIndexDoesNotDescribeIndexAsTable(t *testing.T) {
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{"records": indexedDatabaseTable()}}
	_, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT id FROM records VIEW absent_index;"}}, Options{}, database)
	require.Error(t, err)
	require.Contains(t, err.Error(), "absent_index")
	require.Contains(t, err.Error(), "index")
	require.Equal(t, []string{"records"}, database.described)
}
