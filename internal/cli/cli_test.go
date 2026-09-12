package cli

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/update"
)

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestCLIReportsOutputFailures(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "sqlc.yaml")
	for _, args := range [][]string{{"--help"}, {"version"}, {"version", "--verbose"}, {"init", "-f", configPath}, {"init", "-f", configPath}} {
		var stderr bytes.Buffer
		if code := Run(args, brokenWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), io.ErrClosedPipe.Error()) {
			t.Errorf("%v: status %d, stderr %q", args, code, stderr.String())
		}
	}
	if code := Run([]string{"unknown"}, io.Discard, brokenWriter{}); code != 1 {
		t.Errorf("diagnostic write failure changed exit status: %d", code)
	}
	_, err := compare([]output{{path: filepath.Join(t.TempDir(), "missing.go"), content: []byte("generated\n")}}, brokenWriter{})
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("diff output failure: %v", err)
	}
}

func invoke(args ...string) (int, string, string) {
	var out, err bytes.Buffer
	client := update.NewClient()
	client.HTTP.Transport = offlineTransport{}
	code := run(args, &out, &err, client)
	return code, out.String(), err.String()
}

type offlineTransport struct{}

func (offlineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline")
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

func TestOutputFileCannotBeAnotherOutputDirectory(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "sqlc.yaml")
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE a (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "queries.sql"), "-- name: GetA :one\nSELECT id FROM a;")
	put(t, cfg, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      out: db\n    python:\n      out: db/db.go\n")
	for _, command := range []string{"diff", "generate"} {
		if code, _, err := invoke(command, "-f", cfg); code != 1 || !strings.Contains(err, "output path conflict") {
			t.Errorf("%s: got %d: %s", command, code, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "db")); !os.IsNotExist(err) {
		t.Fatal("generate wrote files before reporting conflicting paths")
	}
}

func TestCLIAndInit(t *testing.T) {
	for _, args := range [][]string{{"generate", "--bogus"}, {"generate", "-f"}, {"version", "extra"}, {"init", "--v1"}, {"push"}, {"generate", "--remote"}} {
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
	if code, _, err := invoke("init", "-f", cfg, "--v2"); code != 0 {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(cfg)
	if !bytes.Equal(first, second) {
		t.Fatal("init overwrote existing config")
	}
}

func TestVersionVerboseIsOnlyAvailableForVersion(t *testing.T) {
	if code, out, err := invoke("version"); code != 0 || out != Version+"\n" || err != "" {
		t.Fatalf("version: %d %q %q", code, out, err)
	}
	if code, out, err := invoke("version", "--verbose"); code != 0 || out != Version+"\ncommit: "+Commit+"\n" || err != "" {
		t.Fatalf("verbose version: %d %q %q", code, out, err)
	}
	if code, _, err := invoke("generate", "--verbose"); code != 1 || !strings.Contains(err, "only valid for version") {
		t.Fatalf("generate --verbose: %d %q", code, err)
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

func TestRenamedQueryLeavesStaleOutput(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "sqlc.yaml")
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE a (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "queries", "old.sql"), "-- name: GetA :one\nSELECT id FROM a;")
	put(t, cfg, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries\n  gen:\n    go:\n      out: db\n")
	if code, _, err := invoke("generate", "-f", cfg); code != 0 {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "queries", "old.sql"), filepath.Join(dir, "queries", "new.sql")); err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(dir, "db", "old.sql.go")
	staleBefore, err := os.ReadFile(stalePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"diff", "generate"} {
		if code, _, err := invoke(command, "-f", cfg); code != 1 || !strings.Contains(err, "old.sql.go") || !strings.Contains(err, "stale") {
			t.Fatalf("%s accepted stale output: %d %s", command, code, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "db", "new.sql.go")); !os.IsNotExist(err) {
		t.Fatal("generate wrote files before reporting stale output")
	}
	if data, err := os.ReadFile(stalePath); err != nil || !bytes.Equal(data, staleBefore) {
		t.Fatal("stale output was removed or changed")
	}
	if err := os.Remove(stalePath); err != nil {
		t.Fatal(err)
	}
	// Handwritten files and packages nested in out are not generator-owned.
	put(t, filepath.Join(dir, "db", "custom.go"), "package db\n// Code generated by sqlc-ydb. DO NOT EDIT.\n")
	put(t, filepath.Join(dir, "db", "nested", "models.go"), "// Code generated by sqlc-ydb. DO NOT EDIT.\npackage nested\n")
	for _, command := range []string{"generate", "diff"} {
		if code, _, err := invoke(command, "-f", cfg); code != 0 {
			t.Fatalf("%s after cleanup: %s", command, err)
		}
	}
}

func TestStaleOutputsAcrossLanguages(t *testing.T) {
	for _, file := range []struct{ name, header string }{
		{"Unused.java", "// Code generated by sqlc-ydb. DO NOT EDIT.\n"},
		{"Unused.kt", "// Code generated by sqlc-ydb. DO NOT EDIT.\n"},
		{"unused.py", "# Code generated by sqlc-ydb. DO NOT EDIT.\n"},
		{"Unused.cs", "// Code generated by sqlc-ydb. DO NOT EDIT.\r\n"},
		{"unused.js", "// Code generated by sqlc-ydb. DO NOT EDIT.\n"},
		{"unused.rs", "// Code generated by sqlc-ydb. DO NOT EDIT.\n"},
		{"Unused.php", "<?php\n// Code generated by sqlc-ydb. DO NOT EDIT.\n"},
	} {
		t.Run(file.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := filepath.Join(dir, "sqlc.yaml")
			put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE a (id Uint64 NOT NULL, PRIMARY KEY(id));")
			put(t, filepath.Join(dir, "queries.sql"), "-- name: GetA :one\nSELECT id FROM a;")
			put(t, cfg, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      out: db\n    python:\n      out: db\n")
			if code, _, err := invoke("generate", "-f", cfg); code != 0 {
				t.Fatal(err)
			}
			put(t, filepath.Join(dir, "db", file.name), file.header)
			if code, _, err := invoke("diff", "-f", cfg); code != 1 || !strings.Contains(err, file.name) {
				t.Fatalf("diff accepted stale %s: %d %s", file.name, code, err)
			}
			if code, _, err := invoke("compile", "-f", cfg); code != 0 {
				t.Fatalf("compile inspected output: %s", err)
			}
		})
	}
}

func TestKotlinGenerateCompileDiff(t *testing.T) {
	for _, runtime := range []string{"ydb", "jdbc", "exposed"} {
		t.Run(runtime, func(t *testing.T) {
			dir := t.TempDir()
			cfg := filepath.Join(dir, "sqlc.yaml")
			outDir := filepath.Join(dir, "generated", "kotlin")
			put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE authors (id Int64 NOT NULL, name Utf8, PRIMARY KEY(id));")
			put(t, filepath.Join(dir, "queries.sql"), "-- name: GetAuthor :one\nDECLARE $id AS Int64;\nSELECT name FROM authors WHERE id = $id;")
			put(t, cfg, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    kotlin:\n      out: generated/kotlin\n      package: example.db\n      runtime: "+runtime+"\n")
			if code, _, err := invoke("compile", "-f", cfg); code != 0 {
				t.Fatal(err)
			}
			if _, err := os.Stat(outDir); !os.IsNotExist(err) {
				t.Fatal("compile wrote Kotlin output")
			}
			if code, _, err := invoke("generate", "-f", cfg); code != 0 {
				t.Fatal(err)
			}
			files, err := filepath.Glob(filepath.Join(outDir, "*.kt"))
			if err != nil || len(files) == 0 {
				t.Fatalf("no Kotlin files in configured output directory: %v", err)
			}
			for _, file := range files {
				content, err := os.ReadFile(file)
				if err != nil || !bytes.Contains(content, []byte("package example.db")) {
					t.Fatalf("Kotlin package option was not applied to %s: %v", file, err)
				}
			}
			if code, out, err := invoke("diff", "-f", cfg); code != 0 || out != "" {
				t.Fatalf("diff %d: %s %s", code, out, err)
			}
			put(t, filepath.Join(outDir, "Obsolete.kt"), "// Code generated by sqlc-ydb. DO NOT EDIT.\n")
			if code, _, err := invoke("generate", "-f", cfg); code != 1 || !strings.Contains(err, "Obsolete.kt") {
				t.Fatalf("accepted stale Kotlin output: %d %s", code, err)
			}
		})
	}
}
