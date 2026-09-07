package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func invoke(args ...string) (int, string, string) {
	var out, err bytes.Buffer
	code := Run(args, &out, &err)
	return code, out.String(), err.String()
}
func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateCompileDiff(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE authors (id Uint64 NOT NULL, name Utf8, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "queries.sql"), "-- name: GetAuthor :one\nDECLARE $author_id AS Uint64;\nSELECT name FROM authors WHERE id = $author_id;")
	cfg := filepath.Join(dir, "sqlc.yaml")
	put(t, cfg, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      out: db\n      sql_package: database/sql\n    python:\n      out: py\n      runtime: ydb\n")
	if code, _, err := invoke("-f", cfg, "compile"); code != 0 {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "db")); !os.IsNotExist(err) {
		t.Fatal("compile wrote output")
	}
	if code, _, err := invoke("generate", "-f", cfg); code != 0 {
		t.Fatal(err)
	}
	if code, out, err := invoke("diff", "--file="+cfg); code != 0 || out != "" {
		t.Fatalf("diff %d: %s %s", code, out, err)
	}
	file := filepath.Join(dir, "db", "models.go")
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	put(t, file, string(original)+"// edited\n")
	if code, out, err := invoke("diff", "-f", cfg); code != 1 || !strings.Contains(out, "-// edited") {
		t.Fatalf("diff %d: %s %s", code, out, err)
	}
	if data, _ := os.ReadFile(file); !bytes.HasSuffix(data, []byte("// edited\n")) {
		t.Fatal("diff modified file")
	}
	if code, _, err := invoke("generate", "-f", cfg); code != 0 {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(file); !bytes.Equal(data, original) {
		t.Fatal("regeneration not deterministic")
	}
}

func TestGenerationErrorLeavesOutputsIntact(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "sqlc.yaml")
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE a (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "valid.sql"), "-- name: GetA :one\nSELECT id FROM a;")
	put(t, filepath.Join(dir, "invalid.sql"), "-- name: Broken :one\nSELECT missing FROM a;")
	put(t, filepath.Join(dir, "db", "models.go"), "sentinel")
	put(t, cfg, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: valid.sql\n  gen:\n    go:\n      out: db\n- engine: ydb\n  schema: schema.sql\n  queries: invalid.sql\n  gen:\n    go:\n      out: db2\n")
	if code, _, err := invoke("generate", "-f", cfg); code == 0 || !strings.Contains(err, "missing") {
		t.Fatalf("invalid query accepted: %d %s", code, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "db", "models.go")); string(data) != "sentinel" {
		t.Fatal("partially generated despite analysis error")
	}
}

func TestCLIAndInit(t *testing.T) {
	for _, args := range [][]string{{"generate", "--bogus"}, {"generate", "-f"}, {"version", "extra"}, {"init", "--v1", "--v2"}, {"push"}, {"generate", "--remote"}} {
		if code, _, _ := invoke(args...); code == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
	for _, args := range [][]string{{"--help"}, {"version"}} {
		if code, _, err := invoke(args...); code != 0 {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "new.yaml")
	if code, _, err := invoke("init", "-f", cfg); code != 0 {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(cfg)
	if code, _, err := invoke("init", "-f", cfg, "--v1"); code != 0 {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(cfg)
	if !bytes.Equal(first, second) {
		t.Fatal("init overwrote existing config")
	}
}

func TestSymlinkOutputCannotOverwriteConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "models.go")
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE a (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "queries.sql"), "-- name: GetA :one\nSELECT id FROM a;")
	data := "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      out: linked\n"
	put(t, cfg, data)
	if err := os.Symlink(dir, filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	if code, _, err := invoke("generate", "-f", cfg); code == 0 || !strings.Contains(err, "overwrite input") {
		t.Fatalf("got %d: %s", code, err)
	}
	got, _ := os.ReadFile(cfg)
	if string(got) != data {
		t.Fatal("config overwritten")
	}
}
