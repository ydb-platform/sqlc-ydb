package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNoDatabaseFlag(t *testing.T) {
	for _, command := range []string{"generate", "compile", "diff"} {
		for _, args := range [][]string{{command, "--no-database"}, {"--no-database", command}} {
			got, err := parseArgs(args)
			if err != nil || !got.noDatabase {
				t.Fatalf("%v: parsed %#v: %v", args, got, err)
			}
		}
	}
	for _, command := range []string{"version", "init"} {
		if _, err := parseArgs([]string{command, "--no-database"}); err == nil || !strings.Contains(err.Error(), "only valid") {
			t.Fatalf("%s accepted --no-database: %v", command, err)
		}
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
		if code, _, stderr := invoke(command, "--no-database", "-f", cfg); code != 0 {
			t.Fatalf("%s --no-database: %s", command, stderr)
		}
	}
	put(t, cfg, base+"  analyzer:\n    database: false\n")
	if code, _, stderr := invoke("compile", "-f", cfg); code != 0 {
		t.Fatal(stderr)
	}
	put(t, cfg, base)
	if code, _, stderr := invoke("compile", "-f", cfg); code == 0 || !strings.Contains(stderr, "SQLC_YDB_UNSET_TEST_URI") {
		t.Fatalf("enabled database skipped missing configuration: %d %s", code, stderr)
	}
	put(t, cfg, strings.Replace(base, "  schema: schema.sql\n", "", 1))
	if code, _, stderr := invoke("compile", "-f", cfg); code == 0 || !strings.Contains(stderr, "SQLC_YDB_UNSET_TEST_URI") {
		t.Fatalf("schema discovery did not reach database configuration: %d %s", code, stderr)
	}
	if code, _, stderr := invoke("compile", "--no-database", "-f", cfg); code == 0 || !strings.Contains(stderr, "schema is required") {
		t.Fatalf("missing offline schema: %d %s", code, stderr)
	}
}
