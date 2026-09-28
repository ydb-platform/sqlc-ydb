package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestVetRunsSelectedRulesWithoutGeneratingFiles(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE items (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "queries.sql"), "-- name: Get :one\nSELECT id FROM items WHERE id = $id;\n\n-- name: Remove :exec\nDELETE FROM items WHERE id = $id;")
	configPath := filepath.Join(dir, "sqlc.yaml")
	put(t, configPath, `version: '2'
sql:
- engine: ydb
  schema: schema.sql
  queries: queries.sql
  rules: [no-delete, at-most-one-param]
rules:
- name: no-delete
  message: DELETE needs review
  rule: query.cmd == 'exec'
- name: at-most-one-param
  rule: query.params.size() > 1
`)
	code, _, stderr := invoke("vet", "-f", configPath)
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "query Remove: vet rule no-delete: DELETE needs review")
	require.NotContains(t, stderr, "at-most-one-param")
	_, err := os.Stat(filepath.Join(dir, "db"))
	require.ErrorIs(t, err, os.ErrNotExist)
	put(t, filepath.Join(dir, "queries.sql"), "-- name: Get :one\nSELECT id FROM items WHERE id = $id;")
	code, _, stderr = invoke("vet", "-f", configPath)
	require.Zero(t, code, stderr)
}

func TestVetReportsAllSelectedRuleFailuresWithQueryContext(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE items (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "queries.sql"), "-- name: Get :one\nSELECT id FROM items WHERE id = $id;\n\n-- name: Remove :exec\nDELETE FROM items WHERE id = $id;")
	configPath := filepath.Join(dir, "sqlc.yaml")
	put(t, configPath, `version: '2'
sql:
- engine: ydb
  schema: schema.sql
  queries: queries.sql
  rules: [typed-id, review-removal]
rules:
- name: typed-id
  rule: config.version == '2' && config.engine == 'ydb' && query.params.exists(p, p.name == 'id' && p.type == 'Uint64')
- name: review-removal
  message: check the DELETE predicate
  rule: query.name == 'Remove' && query.cmd == 'exec'
`)
	code, _, stderr := invoke("vet", "-f", configPath)
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "query Get: vet rule typed-id: rule matched")
	require.Contains(t, stderr, "query Remove: vet rule typed-id: rule matched")
	require.Contains(t, stderr, "query Remove: vet rule review-removal: check the DELETE predicate")
}

func TestVetReportsRuleEvaluationFailure(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE items (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "queries.sql"), "-- name: Get :one\nSELECT id FROM items WHERE id = $id;")
	configPath := filepath.Join(dir, "sqlc.yaml")
	put(t, configPath, `version: '2'
sql:
- engine: ydb
  schema: schema.sql
  queries: queries.sql
  rules: [bad-index]
rules:
- name: bad-index
  rule: query.params[5].name == 'id'
`)
	code, _, stderr := invoke("vet", "-f", configPath)
	require.Equal(t, 1, code)
	require.Contains(t, stderr, `rule "bad-index" on query Get`)
	require.Contains(t, stderr, "index out of bounds")
}

func TestVetRequiresConnectionForPlanRules(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE items (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "queries.sql"), "-- name: List :many\nSELECT id FROM items;")
	configPath := filepath.Join(dir, "sqlc.yaml")
	base := "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  rules: [no-scan]\nrules:\n- name: no-scan\n  rule: ydb.plan.operations.exists(op, op == 'TableFullScan')\n"
	put(t, configPath, base)
	code, _, stderr := invoke("vet", "-f", configPath)
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "requires database.uri and connected analysis for YDB plan checks")
	put(t, configPath, strings.Replace(base, "[no-scan]", "[sqlc/db-prepare]", 1))
	code, _, stderr = invoke("vet", "-f", configPath)
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "sqlc/db-prepare requires database.uri")
}

func TestVetNoDatabaseRequiresOfflineSchema(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "queries.sql"), "-- name: List :many\nSELECT 1;")
	configPath := filepath.Join(dir, "sqlc.yaml")
	put(t, configPath, `version: '2'
sql:
- engine: ydb
  queries: queries.sql
  database:
    uri: grpc://localhost:1/local
`)
	code, _, stderr := invoke("vet", "--no-database", "-f", configPath)
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "schema is required when database-assisted analysis is disabled")
}

func TestVetRejectsInvalidRuleExpressions(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE items (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "queries.sql"), "-- name: List :many\nSELECT id FROM items;")
	configPath := filepath.Join(dir, "sqlc.yaml")
	for _, tc := range []struct{ expression, want string }{
		{"query.sql +", "invalid CEL expression"},
		{"query.name", "must return bool"},
	} {
		put(t, configPath, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  rules: [check]\nrules:\n- name: check\n  rule: "+tc.expression+"\n")
		code, _, stderr := invoke("vet", "-f", configPath)
		require.Equal(t, 1, code)
		require.Contains(t, stderr, tc.want)
	}
}

func TestVetPlanOperationsIgnoreUnrelatedText(t *testing.T) {
	fullScan := `{"Plan":{"Node Type":"Query","Plans":[{"Node Type":"ResultSet","Plans":[{"Node Type":"TableFullScan","Operators":[{"Name":"TableFullScan","Predicate":"x == 'TablePointLookup'"}]}]}]},"SimplifiedPlan":{"Node Type":"TablePointLookup"}}`
	operations, document, err := vetPlan(fullScan)
	require.NoError(t, err)
	require.Equal(t, []string{"Query", "ResultSet", "TableFullScan"}, operations)
	require.Contains(t, document, "Plan")
	lookup := `{"Plan":{"Node Type":"Query","Plans":[{"Node Type":"TablePointLookup","Operators":[{"Name":"TablePointLookup","Predicate":"'TableFullScan'"}]}]}}`
	operations, _, err = vetPlan(lookup)
	require.NoError(t, err)
	require.NotContains(t, operations, "TableFullScan")
	for _, tc := range []struct{ plan, want string }{
		{`not-json`, "decode YDB query plan"},
		{`{}`, "no Plan object"},
		{`{"Plan":{"Operators":{}}}`, "Operators must be a list"},
		{`{"Plan":{"Operators":[1]}}`, "operator must be an object"},
		{`{"Plan":{"Plans":{}}}`, "Plans must be a list"},
		{`{"Plan":{"Plans":[1]}}`, "child must be an object"},
		{`{"Plan":{"Plans":[{"Plans":[1]}]}}`, "child must be an object"},
	} {
		_, _, err := vetPlan(tc.plan)
		require.ErrorContains(t, err, tc.want)
	}
}

func TestVetPlanSQLPreservesSourceDeclarationsAndAddsConfiguredTypes(t *testing.T) {
	query := model.AnalyzedQuery{
		SQL:                "DECLARE $id AS Uint64; SELECT $id, $`имя`;",
		DeclaredParameters: []string{"id"},
		Parameters:         []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "имя", Type: model.Type{Kind: "Utf8"}}},
	}
	require.Equal(t, "DECLARE $`имя` AS Utf8; "+query.SQL, vetValidationSQL(query))
}
