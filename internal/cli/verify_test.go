package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func verifyFixture(t *testing.T, releasedSchema, proposedSchema, releasedQuery, proposedQuery string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	released := filepath.Join(dir, "released")
	proposed := filepath.Join(dir, "proposed")
	for _, fixture := range []struct {
		dir, schema, query string
	}{{released, releasedSchema, releasedQuery}, {proposed, proposedSchema, proposedQuery}} {
		put(t, filepath.Join(fixture.dir, "schema.sql"), fixture.schema)
		put(t, filepath.Join(fixture.dir, "queries.sql"), fixture.query)
		put(t, filepath.Join(fixture.dir, "sqlc.yaml"), "version: '2'\nsql:\n- name: app\n  engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      out: db\n")
	}
	return filepath.Join(released, "sqlc.yaml"), filepath.Join(proposed, "sqlc.yaml"), proposed
}

func TestVerifyReleasedQueriesAgainstProposedSchema(t *testing.T) {
	released, proposed, dir := verifyFixture(t,
		"CREATE TABLE records (id Uint64 NOT NULL, value Utf8, PRIMARY KEY(id));",
		"CREATE TABLE records (id Uint64 NOT NULL, value Utf8, added Utf8, PRIMARY KEY(id));",
		"-- name: ReadRecord :one\nDECLARE $id AS Uint64;\nSELECT * FROM records WHERE id = $id;",
		"-- name: Current :one\nSELECT id FROM records;")
	code, _, stderr := invoke("verify", "--against", released, "-f", proposed)
	require.Zero(t, code, stderr)
	_, err := os.Stat(filepath.Join(dir, "db"))
	require.ErrorIs(t, err, os.ErrNotExist, "verify wrote generated files")

	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE records (id Uint64 NOT NULL, added Utf8, PRIMARY KEY(id));")
	code, _, stderr = invoke("verify", "--against", released, "-f", proposed)
	require.NotZero(t, code)
	require.Contains(t, stderr, "released sql[0]")
	require.Contains(t, stderr, "value")
}

func TestVerifyRejectsChangedResultType(t *testing.T) {
	released, proposed, _ := verifyFixture(t,
		"CREATE TABLE records (id Uint64 NOT NULL, value Utf8, PRIMARY KEY(id));",
		"CREATE TABLE records (id Uint64 NOT NULL, value String, PRIMARY KEY(id));",
		"-- name: ReadRecord :one\nSELECT value FROM records;",
		"-- name: Current :one\nSELECT id FROM records;")
	code, _, stderr := invoke("verify", "--against", released, "-f", proposed)
	require.NotZero(t, code)
	require.Contains(t, stderr, "ReadRecord")
	require.Contains(t, stderr, "Utf8")
	require.Contains(t, stderr, "String")
}

func TestVerifyRejectsChangedParameterType(t *testing.T) {
	released, proposed, _ := verifyFixture(t,
		"CREATE TABLE records (id Uint64 NOT NULL, value Utf8, PRIMARY KEY(id));",
		"CREATE TABLE records (id Uint64 NOT NULL, value String, PRIMARY KEY(id));",
		"-- name: FindRecord :one\nSELECT id FROM records WHERE value = $value;",
		"-- name: Current :one\nSELECT id FROM records;")
	code, _, stderr := invoke("verify", "--against", released, "-f", proposed)
	require.NotZero(t, code)
	require.Contains(t, stderr, "FindRecord")
	require.Contains(t, stderr, "parameter $value")
	require.Contains(t, stderr, "Utf8")
	require.Contains(t, stderr, "String")
}

func TestVerifyMatchesNamedSetsAfterReordering(t *testing.T) {
	released, proposed, dir := verifyFixture(t,
		"CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));",
		"CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));",
		"-- name: ReadRecord :one\nSELECT id FROM records;",
		"-- name: Current :one\nSELECT id FROM records;")
	put(t, filepath.Join(filepath.Dir(released), "other.sql"), "-- name: Other :one\nSELECT id FROM other;")
	put(t, filepath.Join(filepath.Dir(released), "other-schema.sql"), "CREATE TABLE other (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, released, "version: '2'\nsql:\n- name: records\n  engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n- name: other\n  engine: ydb\n  schema: other-schema.sql\n  queries: other.sql\n")
	put(t, filepath.Join(dir, "other.sql"), "-- name: CurrentOther :one\nSELECT id FROM other;")
	put(t, filepath.Join(dir, "other-schema.sql"), "CREATE TABLE other (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, proposed, "version: '2'\nsql:\n- name: other\n  engine: ydb\n  schema: other-schema.sql\n  queries: other.sql\n- name: records\n  engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n")
	code, _, stderr := invoke("verify", "--against", released, "-f", proposed)
	require.Zero(t, code, stderr)
}

func TestVerifyRejectsInvalidBaseline(t *testing.T) {
	released, proposed, _ := verifyFixture(t,
		"CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));",
		"CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));",
		"-- name: ReadRecord :one\nSELECT missing FROM records;",
		"-- name: Current :one\nSELECT id FROM records;")
	code, _, stderr := invoke("verify", "--against", released, "-f", proposed)
	require.NotZero(t, code)
	require.Contains(t, stderr, "released")
	require.Contains(t, stderr, "missing")
}

func TestVerifyRequiresBaseline(t *testing.T) {
	code, _, stderr := invoke("verify")
	require.NotZero(t, code)
	require.Contains(t, stderr, "--against")
	code, _, stderr = invoke("compile", "--against", "sqlc.yaml")
	require.NotZero(t, code)
	require.Contains(t, stderr, "--against")
}

func TestVerifyRejectsMissingQuerySetBeforeDatabaseAccess(t *testing.T) {
	released, proposed, _ := verifyFixture(t,
		"CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));",
		"CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));",
		"-- name: ReadRecord :one\nSELECT id FROM records;",
		"-- name: Current :one\nSELECT id FROM records;")
	put(t, proposed, "version: '2'\nsql:\n- name: different\n  engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  database:\n    uri: grpc://127.0.0.1:1/local\n")
	code, _, stderr := invoke("verify", "--against", released, "-f", proposed)
	require.NotZero(t, code)
	require.Contains(t, stderr, "no proposed query set named \"app\"")
}

func TestVerifyUnnamedSetsAndLocalSchemaRequirement(t *testing.T) {
	released, proposed, _ := verifyFixture(t,
		"CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));",
		"CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));",
		"-- name: ReadRecord :one\nSELECT id FROM records;",
		"-- name: Current :one\nSELECT id FROM records;")
	for _, cfg := range []string{released, proposed} {
		data, err := os.ReadFile(cfg)
		require.NoError(t, err)
		put(t, cfg, strings.Replace(string(data), "- name: app\n  engine: ydb", "- engine: ydb", 1))
	}
	code, _, stderr := invoke("verify", "--against", released, "-f", proposed)
	require.Zero(t, code, stderr)
	put(t, proposed, "version: '2'\nsql:\n- engine: ydb\n  queries: queries.sql\n  database:\n    uri: grpc://127.0.0.1:1/local\n")
	code, _, stderr = invoke("verify", "--against", released, "-f", proposed)
	require.NotZero(t, code)
	require.Contains(t, stderr, "require local schema inputs")
}

func TestVerifyResolvedQueryContract(t *testing.T) {
	uint64Type := model.Type{Kind: "Uint64"}
	utf8Type := model.Type{Kind: "Utf8"}
	old := model.AnalyzedQuery{
		Name:       "ReadRecord",
		Command:    model.Many,
		Parameters: []model.Parameter{{Name: "id", Type: uint64Type}},
		ResultSets: []model.ResultSet{{Name: "Rows", Columns: []model.Column{{Name: "value", Type: utf8Type}}}},
	}
	require.NoError(t, verifyQuery(old, old))
	for _, tc := range []struct {
		name    string
		updated model.AnalyzedQuery
		message string
	}{
		{"command", model.AnalyzedQuery{Name: "ReadRecord", Command: model.One, Parameters: old.Parameters, ResultSets: old.ResultSets}, "identity changed"},
		{"parameter count", model.AnalyzedQuery{Name: old.Name, Command: old.Command, ResultSets: old.ResultSets}, "parameter count changed"},
		{"parameter name", model.AnalyzedQuery{Name: old.Name, Command: old.Command, Parameters: []model.Parameter{{Name: "other", Type: uint64Type}}, ResultSets: old.ResultSets}, "parameter $id is missing"},
		{"result count", model.AnalyzedQuery{Name: old.Name, Command: old.Command, Parameters: old.Parameters}, "result-set count changed"},
		{"result name", model.AnalyzedQuery{Name: old.Name, Command: old.Command, Parameters: old.Parameters, ResultSets: []model.ResultSet{{Name: "Other", Columns: old.ResultSets[0].Columns}}}, "result set 1 changed shape"},
		{"column name", model.AnalyzedQuery{Name: old.Name, Command: old.Command, Parameters: old.Parameters, ResultSets: []model.ResultSet{{Name: "Rows", Columns: []model.Column{{Name: "renamed", Type: utf8Type}}}}}, "column 1 changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorContains(t, verifyQuery(old, tc.updated), tc.message)
		})
	}
}
