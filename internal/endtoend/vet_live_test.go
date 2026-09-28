package endtoend

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/cli"
	"github.com/ydb-platform/sqlc-ydb/internal/config"
)

func TestLiveYDBVetPlanRules(t *testing.T) {
	if os.Getenv("YDB_CONNECTION_STRING") == "" {
		t.Skip("set YDB_CONNECTION_STRING for live vet plan checks")
	}
	dir := t.TempDir()
	table := fmt.Sprintf("sqlc_vet_%d", time.Now().UnixNano())
	t.Cleanup(func() { runDatabasePython(t, dir, databaseFixturePython, "drop", table) })
	runDatabasePython(t, dir, databaseFixturePython, "create", table)
	before := runDatabasePython(t, dir, databaseFixturePython, "snapshot", table)
	settings, err := (config.Database{URI: os.Getenv("YDB_CONNECTION_STRING")}).Resolve(dir)
	require.NoError(t, err)
	queries := fmt.Sprintf(`-- name: ByID :many
PRAGMA TablePathPrefix('%s');
SELECT id FROM %s WHERE id = $id;

-- name: ByValue :many
PRAGMA TablePathPrefix('%s');
SELECT id FROM %s WHERE ztext = 'original'u;
`, settings.Database, table, settings.Database, table)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "queries.sql"), []byte(queries), 0600))
	configPath := filepath.Join(dir, "sqlc.yaml")
	configuration := `version: '2'
sql:
- engine: ydb
  queries: queries.sql
  database:
    uri: ${YDB_CONNECTION_STRING}
  analyzer:
    parameters:
      ByID:
        id: Uint64
  rules: [no-full-scan, missing-query-node, sqlc/db-prepare]
rules:
- name: no-full-scan
  message: query scans an entire table
  rule: ydb.plan.operations.exists(op, op == 'TableFullScan')
- name: missing-query-node
  rule: ydb.explain.Plan['Node Type'] != 'Query'
`
	require.NoError(t, os.WriteFile(configPath, []byte(configuration), 0600))
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"vet", "-f", configPath}, &stdout, &stderr)
	require.Equal(t, 1, code, stderr.String())
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "query ByValue: vet rule no-full-scan: query scans an entire table")
	require.NotContains(t, stderr.String(), "query ByID: vet rule no-full-scan")
	require.Equal(t, before, runDatabasePython(t, dir, databaseFixturePython, "snapshot", table))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "queries.sql"), []byte(strings.Split(queries, "\n\n-- name: ByValue")[0]), 0600))
	stderr.Reset()
	code = cli.Run([]string{"vet", "-f", configPath}, &stdout, &stderr)
	require.Zero(t, code, stderr.String())
}
