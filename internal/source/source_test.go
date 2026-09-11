package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathsAndMigrations(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"001.sql": "-- +goose Up\nCREATE TABLE `пример` (id Uint64, PRIMARY KEY(id));\n-- +goose Down\nDROP TABLE `пример`;",
		"002.sql": "SELECT 2;", "002.down.sql": "DROP TABLE a;", ".hidden.sql": "SELECT 0;", "README": "ignore",
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "ignore.sql"), []byte("ignore"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir, []string{".", "001.sql"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || filepath.Base(got[0].Name) != "001.sql" || strings.Contains(got[0].Text, "DROP TABLE") {
		t.Fatalf("unexpected sources: %+v", got)
	}
	got, err = Read(dir, []string{"002.sql", "001.sql"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got[0].Name) != "002.sql" {
		t.Fatal("explicit path order changed")
	}
	if _, err := Read(dir, []string{"missing*.sql"}, false); err == nil {
		t.Fatal("unmatched glob accepted")
	}
}

func TestRollbackMarkerInLiteral(t *testing.T) {
	for _, s := range []string{"SELECT '-- +goose Down';", "SELECT @@\n-- +goose Down\n@@;", "SELECT 'привет';\n-- unrelated comment\nSELECT 1;"} {
		if got := upMigration(s); got != s {
			t.Fatalf("literal truncated: %q", got)
		}
	}
}

func TestRollbackMarkerBoundary(t *testing.T) {
	const before = "CREATE TABLE a (id Uint64, PRIMARY KEY(id));\n"
	const after = "\nCREATE TABLE b (id Uint64, PRIMARY KEY(id));"
	for _, marker := range []string{"-- +goose Down", "-- +migrate Down", "---- create above / drop below ----", "-- migrate:down"} {
		for _, suffix := range []string{"stream", "_note"} {
			text := before + marker + suffix + after
			if got := upMigration(text); got != text {
				t.Errorf("ordinary comment truncated schema: %q", text)
			}
		}
		for _, suffix := range []string{"", " transaction", "\ttransaction"} {
			text := before + marker + suffix + after
			if got := upMigration(text); got != before {
				t.Errorf("rollback directive was not recognized: %q", text)
			}
		}
	}
}
