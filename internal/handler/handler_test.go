package handler

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sqlc-dev/sqlc-engine-ydb/internal/schema"
	"github.com/sqlc-dev/sqlc-engine-ydb/internal/schema/mock"
	"github.com/sqlc-dev/sqlc/pkg/engine"
	"github.com/stretchr/testify/require"
)

// requireSingleStatement expects exactly one block and returns it.
func requireSingleStatement(t *testing.T, resp *engine.ParseResponse) *engine.Statement {
	t.Helper()
	require.NotNil(t, resp)
	list := resp.GetStatements()
	require.Len(t, list, 1, "expected one statement, got %d", len(list))
	return list[0]
}

func TestParse_Empty(t *testing.T) {
	resp, err := Parse(&engine.ParseRequest{Sql: ""})
	require.NoError(t, err)
	require.Empty(t, resp.GetStatements())
}

func TestParse_ExtractParameters(t *testing.T) {
	sql := `-- name: GetAuthor :one
SELECT * FROM authors WHERE id = $id LIMIT 1`
	resp, err := Parse(&engine.ParseRequest{Sql: sql})
	require.NoError(t, err)
	st := requireSingleStatement(t, resp)
	require.Len(t, st.Parameters, 1)
	require.Equal(t, "id", st.Parameters[0].Name)
	require.Equal(t, int32(1), st.Parameters[0].Position)
}

// TestParse_SelectStarWithSchema uses schema_sql (parsed via ddl.Registry).
func TestParse_SelectStarWithSchema(t *testing.T) {
	schemaSQL := `CREATE TABLE authors (
    id Uint64,
    name Utf8 NOT NULL,
    bio Utf8,
    PRIMARY KEY (id)
);`
	sql := `-- name: ListAuthors :many
SELECT * FROM authors ORDER BY name`
	resp, err := Parse(&engine.ParseRequest{Sql: sql, SchemaSource: &engine.ParseRequest_SchemaSql{SchemaSql: schemaSQL}})
	require.NoError(t, err)
	st := requireSingleStatement(t, resp)
	wantCols := []*engine.Column{
		{Name: "id", DataType: "Uint64", Nullable: true, TableName: "authors"},
		{Name: "name", DataType: "Utf8", Nullable: false, TableName: "authors"},
		{Name: "bio", DataType: "Utf8", Nullable: true, TableName: "authors"},
	}
	require.True(t, cmp.Equal(wantCols, st.Columns, cmp.Comparer(compareColumn)), "columns diff:\n%s", cmp.Diff(wantCols, st.Columns, cmp.Comparer(compareColumn)))
}

// TestParse_SelectStarWithMockRegistry uses ParseWithRegistry and schema/mock.
func TestParse_SelectStarWithMockRegistry(t *testing.T) {
	reg := mock.New(map[string][]schema.ColumnInfo{
		"authors": {
			{Name: "id", DataType: "Uint64", Nullable: true},
			{Name: "name", DataType: "Utf8", Nullable: false},
			{Name: "bio", DataType: "Utf8", Nullable: true},
		},
	})
	sql := `-- name: ListAuthors :many
SELECT * FROM authors ORDER BY name`
	resp, err := ParseWithRegistry(&engine.ParseRequest{Sql: sql}, reg)
	require.NoError(t, err)
	st := requireSingleStatement(t, resp)
	wantCols := []*engine.Column{
		{Name: "id", DataType: "Uint64", Nullable: true, TableName: "authors"},
		{Name: "name", DataType: "Utf8", Nullable: false, TableName: "authors"},
		{Name: "bio", DataType: "Utf8", Nullable: true, TableName: "authors"},
	}
	require.True(t, cmp.Equal(wantCols, st.Columns, cmp.Comparer(compareColumn)), "columns diff:\n%s", cmp.Diff(wantCols, st.Columns, cmp.Comparer(compareColumn)))
}

func TestParse_ReturningStarWithMockRegistry(t *testing.T) {
	reg := mock.New(map[string][]schema.ColumnInfo{
		"authors": {
			{Name: "id", DataType: "Uint64", Nullable: true},
			{Name: "name", DataType: "Utf8", Nullable: false},
		},
	})
	sql := `-- name: CreateAuthor :one
INSERT INTO authors (name, bio) VALUES ($name, $bio) RETURNING *`
	resp, err := ParseWithRegistry(&engine.ParseRequest{Sql: sql}, reg)
	require.NoError(t, err)
	st := requireSingleStatement(t, resp)
	wantCols := []*engine.Column{
		{Name: "id", DataType: "Uint64", Nullable: true, TableName: "authors"},
		{Name: "name", DataType: "Utf8", Nullable: false, TableName: "authors"},
	}
	require.True(t, cmp.Equal(wantCols, st.Columns, cmp.Comparer(compareColumn)), "columns diff:\n%s", cmp.Diff(wantCols, st.Columns, cmp.Comparer(compareColumn)))
}

func compareColumn(a, b *engine.Column) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Name == b.Name && a.DataType == b.DataType && a.Nullable == b.Nullable && a.TableName == b.TableName
}

func TestParse_InvalidSQL(t *testing.T) {
	sql := `-- name: Bad :one
SELECT FROM WHERE`
	_, err := Parse(&engine.ParseRequest{Sql: sql})
	require.Error(t, err)
}