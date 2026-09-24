package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestNamedInsertSelect(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, note Utf8, PRIMARY KEY(id));"}}
	for _, verb := range []string{"INSERT", "UPSERT"} {
		for _, projection := range []string{"r.label, r.id", "*", "r.*", "r.*, NULL AS note", "1ul AS id, r.label"} {
			t.Run(verb+"/"+projection, func(t *testing.T) {
				query := "-- name: Write :exec\nDECLARE $rows AS List<Struct<label:Utf8,id:Uint64,>>;\n" + verb + " INTO records SELECT " + projection + " FROM AS_TABLE($rows) AS r;"
				got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
				require.NoError(t, err)
				wantRows := model.Type{Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: []model.StructField{
					{Name: "label", Type: model.Type{Kind: "Utf8"}},
					{Name: "id", Type: model.Type{Kind: "Uint64"}},
				}}}
				require.Equal(t, []model.Parameter{{Name: "rows", Type: wantRows}}, got.Queries[0].Parameters)
				sql := got.Queries[0].SQL
				require.NotContains(t, sql, "*")
				require.Contains(t, sql, verb+" INTO records SELECT ")
			})
		}
	}
}

func TestNamedInsertSelectTableSources(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `
CREATE TABLE records (id Uint64 NOT NULL, label Utf8, note Utf8, PRIMARY KEY(id));
CREATE TABLE source_records (label Utf8 NOT NULL, id Uint32 NOT NULL, PRIMARY KEY(id));
CREATE TABLE source_notes (id Uint32 NOT NULL, text Utf8, PRIMARY KEY(id));`}}
	for _, verb := range []string{"INSERT", "UPSERT"} {
		for _, tt := range []struct{ name, selectSQL, expandedSQL string }{
			{"reordered columns", "SELECT label, id FROM source_records", "SELECT label, id FROM source_records"},
			{"wildcard", "SELECT * FROM source_records", "SELECT `label`, `id` FROM source_records"},
			{"qualified wildcard", "SELECT r.* FROM source_records AS r", "SELECT r.`label` AS `label`, `r`.`id` AS `id` FROM source_records AS r"},
			{"mixed wildcard and alias", "SELECT r.*, n.text AS note FROM source_records AS r JOIN source_notes AS n ON r.id = n.id", "SELECT r.`label` AS `label`, `r`.`id` AS `id`, n.text AS note FROM source_records AS r JOIN source_notes AS n ON r.id = n.id"},
			{"joined aliases", "SELECT n.text AS note, r.id AS id FROM source_records AS r JOIN source_notes AS n ON r.id = n.id", "SELECT n.text AS note, r.id AS id FROM source_records AS r JOIN source_notes AS n ON r.id = n.id"},
		} {
			t.Run(verb+"/"+tt.name, func(t *testing.T) {
				prefix := "-- name: Write :many\n" + verb + " INTO records "
				result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: prefix + tt.selectSQL + " RETURNING *;"}})
				require.NoError(t, err)
				query := result.Queries[0]
				wantSQL := prefix + tt.expandedSQL + " RETURNING `id`, `label`, `note`;"
				require.Equal(t, wantSQL, query.SQL)
				require.Len(t, query.ResultSets, 1)
				require.Equal(t, result.Catalog.Tables[0].Columns, query.ResultSets[0].Columns)
			})
		}
	}
}

func TestNamedInsertSelectInfersFilterParameters(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY(id));"}}
	sql := "-- name: Copy :exec\nINSERT INTO records SELECT * FROM records AS r WHERE r.id >= $minimum LIMIT $count;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	want := []model.Parameter{{Name: "minimum", Type: model.Type{Kind: "Uint64"}}, {Name: "count", Type: model.Type{Kind: "Uint64"}}}
	require.Equal(t, want, result.Queries[0].Parameters)
}

func TestNamedInsertSelectDiagnostics(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY(id));"}}
	for _, tt := range []struct{ name, sql, want string }{
		{"missing key", "SELECT label FROM records", `missing primary key column "id"`},
		{"unknown target", "SELECT id, label AS extra FROM records", `unknown target column "extra"`},
		{"duplicate", "SELECT id, id FROM records", `duplicate source column "id"`},
		{"duplicate wildcard", "SELECT *, id FROM records", `duplicate source column "id"`},
		{"duplicate alias", "SELECT id AS id, label AS `id` FROM records", `duplicate source column "id"`},
		{"type mismatch", "SELECT label AS id FROM records", `source column "id" has type Optional<Utf8>`},
		{"null key", "SELECT NULL AS id", `source column "id" has type Null`},
		{"missing alias", "SELECT 1ul", `unknown target column "column0"`},
		{"bad source", "SELECT id FROM missing", `unknown table "missing"`},
		{"unknown qualifier", "SELECT absent.* FROM records", `unknown table or alias "absent"`},
		{"qualified join", "SELECT r.id FROM records r JOIN records other ON r.id = other.id", `unknown target column "r.id"; use AS id`},
		{"union", "SELECT id FROM records UNION ALL SELECT id FROM records", `INSERT/UPSERT SELECT supports one SELECT input`},
		{"except", "SELECT id FROM records EXCEPT SELECT id FROM records", `EXCEPT is not yet supported`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\nUPSERT INTO records " + tt.sql + ";"}})
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestNamedInsertSelectAllowsGeneratedKey(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Serial, tenant Uint64 NOT NULL, label Utf8, PRIMARY KEY(tenant, id));"}}
	for _, verb := range []string{"INSERT", "UPSERT"} {
		t.Run(verb, func(t *testing.T) {
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :many\n" + verb + " INTO records SELECT 1ul AS tenant, NULL AS label RETURNING id;"}})
			require.NoError(t, err)
			column := result.Queries[0].ResultSets[0].Columns[0]
			require.Equal(t, "id", column.Name)
			require.Equal(t, "Int32", column.Type.Kind)
			require.True(t, column.SequenceGenerated)
			_, err = Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + verb + " INTO records SELECT NULL AS label;"}})
			require.ErrorContains(t, err, `missing primary key column "tenant"`)
		})
	}
	for _, statement := range []string{"UPDATE records", "DELETE FROM records"} {
		_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + statement + " ON SELECT 1ul AS tenant;"}})
		require.ErrorContains(t, err, `missing primary key column "id"`)
	}
}

func TestNamedInsertSelectRequiresNotNullColumns(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8 NOT NULL, note Utf8, PRIMARY KEY(id));"}}
	for _, verb := range []string{"INSERT", "UPSERT"} {
		t.Run(verb, func(t *testing.T) {
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + verb + " INTO records SELECT 1ul AS id;"}})
			require.ErrorContains(t, err, `missing required column "label"`)
			{
				_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + verb + " INTO records SELECT 1ul AS id, 'hello'u AS label;"}})
				require.NoError(t, err)
			}
		})
	}
	for _, statement := range []string{"UPDATE records", "DELETE FROM records"} {
		{
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + statement + " ON SELECT 1ul AS id;"}})
			require.NoError(t, err)
		}
	}
}

func TestTrailingStructComma(t *testing.T) {
	for _, typ := range []string{"Struct<id:Uint64,>", "List<Struct<id:Uint64,note:Utf8?,>>", "Struct<inner:Struct<id:Uint64,>,>"} {
		parsed, err := parseType(typ)
		require.NoError(t, err)
		require.NotContains(t, parsed.String(), ",>")
	}
	for _, typ := range []string{"Struct<,>", "Struct<id:Uint64,,>", "Struct<id:Uint64,id:Utf8,>"} {
		{
			_, err := parseType(typ)
			require.Error(t, err)
		}
	}
}
