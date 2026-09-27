package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestDirectParameterColumnLineage(t *testing.T) {
	result, err := Analyze(
		[]model.Source{{Name: "schema.sql", Text: "CREATE TABLE customers (id Uint64 NOT NULL, name Utf8, nickname Utf8, PRIMARY KEY(id));"}},
		[]model.Source{{Name: "queries.sql", Text: `-- name: InsertCustomer :exec
INSERT INTO customers (id, name) VALUES ($id, $name);
-- name: UpsertCustomer :exec
UPSERT INTO customers (id, name) VALUES ($id, $name);
-- name: UpdateCustomer :exec
UPDATE customers SET name = $name WHERE id = $id;
-- name: ReadCustomer :one
SELECT name FROM customers WHERE id = $id AND name = $name;
-- name: Shared :many
SELECT id FROM customers WHERE name = $value AND nickname = $value;`}},
	)
	require.NoError(t, err)
	require.Empty(t, result.Diagnostics)
	require.Len(t, result.Queries, 5)
	for _, query := range result.Queries {
		if query.Name == "Shared" {
			require.Len(t, query.ParameterColumns["value"], 2, query.Name)
			require.Equal(t, []string{"name", "nickname"}, []string{query.ParameterColumns["value"][0].Name, query.ParameterColumns["value"][1].Name})
			continue
		}
		require.NotEmpty(t, query.ParameterColumns["id"], "%s: %#v, bindings=%#v", query.Name, query.ParameterColumns, query.Syntax.Columns)
		require.NotEmpty(t, query.ParameterColumns["name"], query.Name)
		require.Equal(t, "id", query.ParameterColumns["id"][0].Name)
		require.Equal(t, "name", query.ParameterColumns["name"][0].Name)
	}
}
