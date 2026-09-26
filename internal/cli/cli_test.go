package cli

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/update"
)

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestCLIReportsOutputFailures(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "sqlc.yaml")
	for _, args := range [][]string{{"--help"}, {"version"}, {"version", "--verbose"}, {"init", "-f", configPath}, {"init", "-f", configPath}} {
		var stderr bytes.Buffer
		code := Run(args, brokenWriter{}, &stderr)
		assert.Equal(t, 1, code, "%v: stderr %q", args, stderr.String())
		assert.Contains(t, stderr.String(), io.ErrClosedPipe.Error(), "%v", args)
	}
	assert.Equal(t, 1, Run([]string{"unknown"}, io.Discard, brokenWriter{}), "diagnostic write failure changed exit status")
	_, err := compare([]output{{path: filepath.Join(t.TempDir(), "missing.go"), content: []byte("generated\n")}}, brokenWriter{})
	assert.ErrorIs(t, err, io.ErrClosedPipe, "diff output failure")
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
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(text), 0600))
}

func TestGenerateCompileDiff(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE authors (id Uint64 NOT NULL, name Utf8, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "queries.sql"), "-- name: GetAuthor :one\nDECLARE $author_id AS Uint64;\nSELECT name FROM authors WHERE id = $author_id;")
	cfg := filepath.Join(dir, "sqlc.yaml")
	put(t, cfg, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      out: db\n      sql_package: database/sql\n    python:\n      out: py\n      runtime: ydb\n")
	code, _, stderr := invoke("-f", cfg, "compile")
	require.Zero(t, code, stderr)
	_, err := os.Stat(filepath.Join(dir, "db"))
	require.ErrorIs(t, err, os.ErrNotExist, "compile wrote output")
	code, _, stderr = invoke("generate", "-f", cfg)
	require.Zero(t, code, stderr)
	code, out, stderr := invoke("diff", "--file="+cfg)
	require.Zero(t, code, stderr)
	require.Empty(t, out)
	file := filepath.Join(dir, "db", "models.go")
	original, err := os.ReadFile(file)
	require.NoError(t, err)
	put(t, file, string(original)+"// edited\n")
	code, out, stderr = invoke("diff", "-f", cfg)
	require.Equal(t, 1, code, stderr)
	require.Contains(t, out, "-// edited")
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	require.True(t, bytes.HasSuffix(data, []byte("// edited\n")), "diff modified file")
	code, _, stderr = invoke("generate", "-f", cfg)
	require.Zero(t, code, stderr)
	data, err = os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, original, data, "regeneration not deterministic")
}

func TestSQLCArgumentDiagnosticsAcrossCommands(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE foo (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "queries.sql"), "-- name: Broken :many\nSELECT id FROM foo WHERE id = sqlc.narg();")
	cfg := filepath.Join(dir, "sqlc.yaml")
	put(t, cfg, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      out: db\n")
	var first string
	for _, command := range []string{"compile", "generate", "diff"} {
		code, _, stderr := invoke(command, "-f", cfg)
		require.NotZero(t, code, command)
		require.Contains(t, stderr, "sqlc.narg expects exactly one parameter name")
		if first == "" {
			first = stderr
		} else {
			require.Equal(t, first, stderr)
		}
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
	code, _, stderr := invoke("generate", "-f", cfg)
	require.NotZero(t, code, stderr)
	require.Contains(t, stderr, "missing")
	data, err := os.ReadFile(filepath.Join(dir, "db", "models.go"))
	require.NoError(t, err)
	require.Equal(t, "sentinel", string(data), "partially generated despite analysis error")
}

func TestOutputFileCannotBeAnotherOutputDirectory(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "sqlc.yaml")
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE a (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "queries.sql"), "-- name: GetA :one\nSELECT id FROM a;")
	put(t, cfg, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      out: db\n    python:\n      out: db/db.go\n")
	for _, command := range []string{"diff", "generate"} {
		code, _, stderr := invoke(command, "-f", cfg)
		assert.Equal(t, 1, code, "%s: %s", command, stderr)
		assert.Contains(t, stderr, "output path conflict", command)
	}
	_, err := os.Stat(filepath.Join(dir, "db"))
	require.ErrorIs(t, err, os.ErrNotExist, "generate wrote files before reporting conflicting paths")
}

func TestCLIAndInit(t *testing.T) {
	for _, args := range [][]string{{"generate", "--bogus"}, {"generate", "-f"}, {"version", "extra"}, {"init", "--v1"}, {"push"}, {"generate", "--remote"}} {
		code, _, _ := invoke(args...)
		require.NotZero(t, code, "accepted %v", args)
	}
	for _, args := range [][]string{{"--help"}, {"version"}} {
		code, _, stderr := invoke(args...)
		require.Zero(t, code, stderr)
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "new.yaml")
	code, _, stderr := invoke("init", "-f", cfg)
	require.Zero(t, code, stderr)
	first, err := os.ReadFile(cfg)
	require.NoError(t, err)
	code, _, stderr = invoke("init", "-f", cfg, "--v2")
	require.Zero(t, code, stderr)
	second, err := os.ReadFile(cfg)
	require.NoError(t, err)
	require.Equal(t, first, second, "init overwrote existing config")
}

func TestVersionVerboseIsOnlyAvailableForVersion(t *testing.T) {
	code, out, stderr := invoke("version")
	require.Zero(t, code, stderr)
	require.Equal(t, Version+"\n", out)
	require.Empty(t, stderr)
	code, out, stderr = invoke("version", "--verbose")
	require.Zero(t, code, stderr)
	require.Equal(t, Version+"\ncommit: "+Commit+"\n", out)
	require.Empty(t, stderr)
	code, _, stderr = invoke("generate", "--verbose")
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "only valid for version")
}

func TestSymlinkOutputCannotOverwriteConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "models.go")
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE a (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "queries.sql"), "-- name: GetA :one\nSELECT id FROM a;")
	data := "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      out: linked\n"
	put(t, cfg, data)
	require.NoError(t, os.Symlink(dir, filepath.Join(dir, "linked")))
	code, _, stderr := invoke("generate", "-f", cfg)
	require.NotZero(t, code)
	require.Contains(t, stderr, "overwrite input")
	got, err := os.ReadFile(cfg)
	require.NoError(t, err)
	require.Equal(t, data, string(got), "config overwritten")
}

func TestRenamedQueryLeavesStaleOutput(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "sqlc.yaml")
	put(t, filepath.Join(dir, "schema.sql"), "CREATE TABLE a (id Uint64 NOT NULL, PRIMARY KEY(id));")
	put(t, filepath.Join(dir, "queries", "old.sql"), "-- name: GetA :one\nSELECT id FROM a;")
	put(t, cfg, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries\n  gen:\n    go:\n      out: db\n")
	code, _, stderr := invoke("generate", "-f", cfg)
	require.Zero(t, code, stderr)
	require.NoError(t, os.Rename(filepath.Join(dir, "queries", "old.sql"), filepath.Join(dir, "queries", "new.sql")))
	stalePath := filepath.Join(dir, "db", "old.sql.go")
	staleBefore, err := os.ReadFile(stalePath)
	require.NoError(t, err)
	for _, command := range []string{"diff", "generate"} {
		code, _, stderr := invoke(command, "-f", cfg)
		require.Equal(t, 1, code, "%s: %s", command, stderr)
		require.Contains(t, stderr, "old.sql.go", command)
		require.Contains(t, stderr, "stale", command)
	}
	_, err = os.Stat(filepath.Join(dir, "db", "new.sql.go"))
	require.ErrorIs(t, err, os.ErrNotExist, "generate wrote files before reporting stale output")
	data, err := os.ReadFile(stalePath)
	require.NoError(t, err)
	require.Equal(t, staleBefore, data, "stale output was removed or changed")
	require.NoError(t, os.Remove(stalePath))
	// Handwritten files and packages nested in out are not generator-owned.
	put(t, filepath.Join(dir, "db", "custom.go"), "package db\n// Code generated by sqlc-ydb. DO NOT EDIT.\n")
	put(t, filepath.Join(dir, "db", "nested", "models.go"), "// Code generated by sqlc-ydb. DO NOT EDIT.\npackage nested\n")
	for _, command := range []string{"generate", "diff"} {
		code, _, stderr := invoke(command, "-f", cfg)
		require.Zero(t, code, "%s after cleanup: %s", command, stderr)
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
			code, _, stderr := invoke("generate", "-f", cfg)
			require.Zero(t, code, stderr)
			put(t, filepath.Join(dir, "db", file.name), file.header)
			code, _, stderr = invoke("diff", "-f", cfg)
			require.Equal(t, 1, code, stderr)
			require.Contains(t, stderr, file.name)
			code, _, stderr = invoke("compile", "-f", cfg)
			require.Zero(t, code, stderr)
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
			code, _, stderr := invoke("compile", "-f", cfg)
			require.Zero(t, code, stderr)
			_, err := os.Stat(outDir)
			require.ErrorIs(t, err, os.ErrNotExist, "compile wrote Kotlin output")
			code, _, stderr = invoke("generate", "-f", cfg)
			require.Zero(t, code, stderr)
			files, err := filepath.Glob(filepath.Join(outDir, "*.kt"))
			require.NoError(t, err)
			require.NotEmpty(t, files, "no Kotlin files in configured output directory")
			for _, file := range files {
				content, err := os.ReadFile(file)
				require.NoError(t, err)
				require.Contains(t, string(content), "package example.db", file)
			}
			code, out, stderr := invoke("diff", "-f", cfg)
			require.Zero(t, code, stderr)
			require.Empty(t, out)
			put(t, filepath.Join(outDir, "Obsolete.kt"), "// Code generated by sqlc-ydb. DO NOT EDIT.\n")
			code, _, stderr = invoke("generate", "-f", cfg)
			require.Equal(t, 1, code, stderr)
			require.Contains(t, stderr, "Obsolete.kt")
		})
	}
}
