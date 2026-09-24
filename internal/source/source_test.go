package source

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPathsAndMigrations(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"001.sql": "-- +goose Up\nCREATE TABLE `пример` (id Uint64, PRIMARY KEY(id));\n-- +goose Down\nDROP TABLE `пример`;",
		"002.sql": "SELECT 2;", "002.down.sql": "DROP TABLE a;", ".hidden.sql": "SELECT 0;", "README": "ignore",
	}
	for name, data := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(data), 0600))
	}
	require.NoError(t, os.Mkdir(filepath.Join(dir, "nested"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nested", "ignore.sql"), []byte("ignore"), 0600))
	got, err := Read(dir, []string{".", "001.sql"}, true)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "001.sql", filepath.Base(got[0].Name))
	require.NotContains(t, got[0].Text, "DROP TABLE")
	got, err = Read(dir, []string{"002.sql", "001.sql"}, false)
	require.NoError(t, err)
	require.NotEmpty(t, got)
	require.Equal(t, "002.sql", filepath.Base(got[0].Name), "explicit path order changed")
	_, err = Read(dir, []string{"missing*.sql"}, false)
	require.Error(t, err, "unmatched glob accepted")
}

func TestRollbackMarkerInLiteral(t *testing.T) {
	for _, s := range []string{"SELECT '-- +goose Down';", "SELECT @@\n-- +goose Down\n@@;", "SELECT 'привет';\n-- unrelated comment\nSELECT 1;"} {
		require.Equal(t, s, upMigration(s), "literal truncated")
	}
}

func TestRollbackMarkerBoundary(t *testing.T) {
	const before = "CREATE TABLE a (id Uint64, PRIMARY KEY(id));\n"
	const after = "\nCREATE TABLE b (id Uint64, PRIMARY KEY(id));"
	for _, marker := range []string{"-- +goose Down", "-- +migrate Down", "---- create above / drop below ----", "-- migrate:down"} {
		for _, suffix := range []string{"stream", "_note"} {
			text := before + marker + suffix + after
			require.Equal(t, text, upMigration(text), "ordinary comment truncated schema")
		}
		for _, suffix := range []string{"", " transaction", "\ttransaction"} {
			text := before + marker + suffix + after
			require.Equal(t, before, upMigration(text), "rollback directive was not recognized")
		}
	}
}
