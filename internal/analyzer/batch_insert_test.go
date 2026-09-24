package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestBatchInsertSelect(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Json, PRIMARY KEY(id));`}}
	for _, source := range []string{"SELECT id, label FROM AS_TABLE($values)", "SELECT r.id, r.label FROM AS_TABLE($values) AS r"} {
		sql := "-- name: CreateRecords :exec\nDECLARE $values AS List<Struct<id:Uint64,label:Json>>;\nINSERT INTO records (id,label) " + source + ";"
		result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
		require.NoError(t, err)
		q := result.Queries[0]
		require.Len(t, q.Parameters, 1)
		require.Equal(t, "Struct", q.Parameters[0].Type.Elem.Kind)
		require.Len(t, q.Parameters[0].Type.Elem.Fields, 2)
		require.Equal(t, sql, q.SQL)
		require.True(t, q.IsDeclaredParameter("values"))
	}
}

func TestBatchInsertDiagnostics(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Json, PRIMARY KEY(id));`}}
	for _, tt := range []struct{ decl, sql, want string }{
		{"List<Struct<id:Utf8,label:Json>>", "INSERT INTO records (id,label) SELECT id,label FROM AS_TABLE($books)", "requires Uint64"},
		{"List<Struct<id:Uint64?,label:Json>>", "INSERT INTO records (id,label) SELECT id,label FROM AS_TABLE($books)", "requires Uint64"},
		{"List<Struct<id:Uint64,label:Json>>", "INSERT INTO records (id,label) SELECT id,missing FROM AS_TABLE($books)", "unknown column"},
		{"List<Struct<id:Uint64,label:Json>>", "INSERT INTO records (missing,label) SELECT id,label FROM AS_TABLE($books)", "unknown column"},
		{"List<Struct<id:Uint64,label:Json>>", "INSERT INTO records (id,label) SELECT id FROM AS_TABLE($books)", "1 columns for 2"},
		{"List<Struct<id:Uint64,label:Json>>", "INSERT INTO records (id,id) SELECT id,id FROM AS_TABLE($books)", "duplicate target column"},
		{"List<Uint64>", "INSERT INTO records (id) SELECT id FROM AS_TABLE($books)", "requires DECLARE"},
		{"List<Struct<id:Uint64,id:Json>>", "INSERT INTO records (id) SELECT id FROM AS_TABLE($books)", "duplicate Struct field"},
	} {
		t.Run(tt.want+tt.decl, func(t *testing.T) {
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: CreateRecords :exec\nDECLARE $books AS " + tt.decl + ";\n" + tt.sql + ";"}})
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestBatchInsertExplicitSourceColumns(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Json, PRIMARY KEY(id));`}}
	for _, projection := range []string{"*", "r.*"} {
		sql := "-- name: CreateRecords :exec\nDECLARE $books AS List<Struct<label:Json,id:Uint64>>; INSERT INTO records (id,label) SELECT " + projection + " FROM AS_TABLE($books) r;"
		_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
		require.ErrorContains(t, err, "requires explicit source columns")
	}
	// Declaration order and aliases do not change the explicit SELECT mapping.
	sql := "-- name: CreateRecords :exec\nDECLARE $books AS List<Struct<label:Json,id:Uint64>>; INSERT INTO records (id,label) SELECT r.id AS another_id,r.label AS another_label FROM AS_TABLE($books) AS r;"
	{
		_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
		require.NoError(t, err)
	}
}

func TestBatchStructQuotedFieldNames(t *testing.T) {
	for _, quote := range []string{"`", "\"", "'"} {
		sql := "-- name: ReadRows :many\nDECLARE $books AS List<Struct<" + quote + "book:id" + quote + ":Uint64,label:Json>>; SELECT r.`book:id`, r.label FROM AS_TABLE($books) r;"
		result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: sql}})
		require.NoError(t, err)
		require.Equal(t, "book:id", result.Queries[0].Parameters[0].Type.Elem.Fields[0].Name)
	}
}

func TestBatchStructRejectsDynamicFieldName(t *testing.T) {
	sql := "-- name: ReadRows :many\nDECLARE $books AS List<Struct<$name:Uint64>>; SELECT id FROM AS_TABLE($books);"
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: sql}})
	require.ErrorContains(t, err, "unsupported Struct field")
}

func TestBatchInsertSourceDiagnostics(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Json, PRIMARY KEY(id));`}}
	for _, tc := range []struct{ name, statement, want string }{
		{"union", "INSERT INTO records (id,label) SELECT id,label FROM AS_TABLE($books) UNION ALL SELECT id,label FROM AS_TABLE($books)", "UNION is not yet supported"},
		{"unknown source", "INSERT INTO records (id,label) SELECT id,label FROM missing_table", "unknown table"},
		{"other function", "INSERT INTO records (id,label) SELECT id,label FROM OtherTable($books)", "dynamic table references"},
		{"multiple args", "INSERT INTO records (id,label) SELECT id,label FROM AS_TABLE($books,$books)", "dynamic table references"},
		{"expression arg", "INSERT INTO records (id,label) SELECT id,label FROM AS_TABLE(ListReverse($books))", "one direct List<Struct> parameter"},
		{"named arg", "INSERT INTO records (id,label) SELECT id,label FROM AS_TABLE($books AS rows)", "one direct List<Struct> parameter"},
		{"alias typo", "INSERT INTO records (id,label) SELECT missing.id,r.label FROM AS_TABLE($books) r", "unknown column"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: InsertRows :exec\nDECLARE $books AS List<Struct<id:Uint64,label:Json>>;\n" + tc.statement + ";"}})
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestBatchInsertSourceInfersFilterParameters(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Json, PRIMARY KEY(id));`}}
	query := "-- name: InsertRows :exec\nDECLARE $books AS List<Struct<id:Uint64,label:Json>>;\nINSERT INTO records (id,label) SELECT r.id,r.label FROM AS_TABLE($books) AS r WHERE r.id >= $minimum LIMIT $count;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	params := map[string]string{}
	for _, p := range result.Queries[0].Parameters {
		params[p.Name] = p.Type.Kind
	}
	require.Equal(t, "List", params["books"])
	require.Equal(t, "Uint64", params["minimum"])
	require.Equal(t, "Uint64", params["count"])
}

func TestPositionalInsertSelectRequiredColumns(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (
    id Serial,
    tenant Uint64 NOT NULL,
    label Utf8 NOT NULL,
    note Utf8,
    PRIMARY KEY(tenant, id)
);`}}
	for _, verb := range []string{"INSERT", "UPSERT"} {
		for _, tt := range []struct{ name, statement, want string }{
			{"missing primary key", "(label) SELECT 'hello'u", `missing primary key column "tenant"`},
			{"missing required column", "(tenant) SELECT 1ul", `missing required column "label"`},
			{"omit generated key and optional column", "(label, tenant) SELECT 'hello'u AS tenant, 1ul AS label", ""},
			{"explicit generated key", "(label, id, tenant) SELECT 'hello'u AS tenant, 7 AS label, 1ul AS id", ""},
			{"explicit null optional column", "(tenant, label, note) SELECT 1ul, 'hello'u, NULL", ""},
		} {
			t.Run(verb+"/"+tt.name, func(t *testing.T) {
				sql := "-- name: Write :exec\n" + verb + " INTO records " + tt.statement + ";"
				got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
				if tt.want != "" {
					require.ErrorContains(t, err, tt.want)
					return
				}
				require.NoError(t, err)
				require.Equal(t, sql, got.Queries[0].SQL)
			})
		}
	}
}

func TestPositionalInsertSelectRequiresNullablePrimaryKey(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64, label Utf8 NOT NULL, PRIMARY KEY(id));"}}
	for _, verb := range []string{"INSERT", "UPSERT"} {
		_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + verb + " INTO records (label) SELECT 'hello'u;"}})
		require.ErrorContains(t, err, `missing primary key column "id"`)
	}
}
