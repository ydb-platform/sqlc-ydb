package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoDatabaseFlag(t *testing.T) {
	for _, command := range []string{"generate", "compile", "diff"} {
		for _, args := range [][]string{{command, "--no-database"}, {"--no-database", command}} {
			got, err := parseArgs(args)
			require.NoError(t, err, "%v", args)
			require.True(t, got.noDatabase, "%v", args)
		}
	}
	for _, command := range []string{"version", "init"} {
		_, err := parseArgs([]string{command, "--no-database"})
		require.ErrorContains(t, err, "only valid", "%s accepted --no-database", command)
	}
}

func TestDatabaseAnalysisCanBeDisabledWithoutCredentials(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "sqlc.yaml")
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE a (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "query.sql"), "-- name: Get :one\nSELECT id FROM a WHERE id = $id;")
	base := "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: query.sql\n  database:\n    uri: ${SQLC_YDB_UNSET_TEST_URI}\n    auth_token_env: SQLC_YDB_UNSET_TEST_TOKEN\n  gen:\n    go:\n      out: db\n"
	t.Setenv("SQLC_YDB_UNSET_TEST_URI", "")
	t.Setenv("SQLC_YDB_UNSET_TEST_TOKEN", "")
	put(t, cfg, base)
	for _, command := range []string{"compile", "generate", "diff"} {
		code, _, stderr := invoke(command, "--no-database", "-f", cfg)
		require.Zero(t, code, "%s --no-database: %s", command, stderr)
	}
	put(t, cfg, base+"  analyzer:\n    database: false\n")
	code, _, stderr := invoke("compile", "-f", cfg)
	require.Zero(t, code, stderr)
	put(t, cfg, base)
	code, _, stderr = invoke("compile", "-f", cfg)
	require.NotZero(t, code, stderr)
	require.Contains(t, stderr, "SQLC_YDB_UNSET_TEST_URI")
	put(t, cfg, strings.Replace(base, "  schema: schema.sql\n", "", 1))
	code, _, stderr = invoke("compile", "-f", cfg)
	require.NotZero(t, code, stderr)
	require.Contains(t, stderr, "SQLC_YDB_UNSET_TEST_URI")
	code, _, stderr = invoke("compile", "--no-database", "-f", cfg)
	require.NotZero(t, code, stderr)
	require.Contains(t, stderr, "schema is required")
}
