package endtoend

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/cli"
)

func TestSQLCSliceInvalidContextHasConsistentCLIDiagnostic(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "schema.sql"), []byte("CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"))
	write(t, filepath.Join(dir, "queries.sql"), []byte("-- name: Invalid :many\nSELECT sqlc.slice(ids) AS ids FROM records;"))
	write(t, filepath.Join(dir, "sqlc.yaml"), []byte("version: \"2\"\nsql:\n  - engine: ydb\n    schema: schema.sql\n    queries: queries.sql\n    gen:\n      go:\n        package: records\n        out: go\n        sql_package: ydb\n"))
	for _, command := range []string{"compile", "generate", "diff"} {
		t.Run(command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := cli.Run([]string{command, "--no-remote", "-f", filepath.Join(dir, "sqlc.yaml")}, &stdout, &stderr)
			require.NotZero(t, status)
			require.Empty(t, stdout.String())
			require.Contains(t, stderr.String(), "sqlc.slice must be directly after IN or directly inside IN (...), without additional parentheses")
		})
	}
}
