package endtoend

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/cli"
)

func TestMultiUnsupportedTargets(t *testing.T) {
	for _, profile := range []struct{ target, runtime string }{
		{"python", "ydb"}, {"python", "dbapi"}, {"python", "sqlalchemy"},
		{"cpp", "ydb"}, {"cpp", "userver"}, {"csharp", "adonet"}, {"csharp", "dapper"},
		{"java", "ydb"}, {"java", "jdbc"}, {"java", "jooq"},
		{"kotlin", "ydb"}, {"kotlin", "jdbc"}, {"kotlin", "exposed"},
		{"typescript", "ydb"}, {"rust", "ydb"}, {"php", "ydb"},
	} {
		t.Run(profile.target+"/"+profile.runtime, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.sql"), []byte(""), 0600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "queries.sql"), []byte("-- name: Summary :multi\nSELECT 1 AS value;\nSELECT false AS value;"), 0600))
			cfg := "version: \"2\"\nsql:\n  - engine: ydb\n    schema: schema.sql\n    queries: queries.sql\n    gen:\n      go:\n        out: db\n      " + profile.target + ":\n        out: other\n        runtime: " + profile.runtime + "\n"
			require.NoError(t, os.WriteFile(filepath.Join(dir, "sqlc.yaml"), []byte(cfg), 0600))
			var stdout, stderr bytes.Buffer
			code := cli.Run([]string{"generate", "--no-remote", "-f", filepath.Join(dir, "sqlc.yaml")}, &stdout, &stderr)
			require.NotZero(t, code, stderr.String())
			require.Contains(t, stderr.String(), ":multi")
			_, err := os.Stat(filepath.Join(dir, "db"))
			require.ErrorIs(t, err, os.ErrNotExist, "partial Go output despite unsupported target")
		})
	}
}
