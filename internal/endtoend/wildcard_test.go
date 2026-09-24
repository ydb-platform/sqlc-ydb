package endtoend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/cli"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/python"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

const wildcardSchema = "CREATE TABLE records (ztext Utf8 NOT NULL, id Uint64 NOT NULL, amount Int32 NOT NULL, PRIMARY KEY(id));"

func TestWildcardExpansionReachesEveryGenerator(t *testing.T) {
	for _, profile := range []struct{ language, runtime, key string }{
		{"go", "ydb", "sql_package"}, {"go", "database/sql", "sql_package"},
		{"python", "ydb", "runtime"}, {"python", "dbapi", "runtime"}, {"python", "sqlalchemy", "runtime"},
		{"cpp", "ydb", "runtime"}, {"cpp", "userver", "runtime"},
		{"csharp", "adonet", "runtime"}, {"csharp", "dapper", "runtime"},
		{"java", "ydb", "runtime"}, {"java", "jdbc", "runtime"},
		{"kotlin", "ydb", "runtime"}, {"kotlin", "jdbc", "runtime"}, {"kotlin", "exposed", "runtime"},
		{"typescript", "ydb", "runtime"}, {"rust", "ydb", "runtime"}, {"php", "ydb", "runtime"},
	} {
		t.Run(profile.language+"/"+profile.runtime, func(t *testing.T) {
			queries := "-- name: Read :many\nSELECT * FROM records;\n" +
				"-- name: ReadAlias :many\nSELECT r.* FROM records AS r;\n" +
				"-- name: Create :one\nINSERT INTO records (id,ztext,amount) VALUES ($id,$ztext,$amount) RETURNING *;\n"
			code := generateWildcardProfile(t, profile.language, profile.runtime, profile.key, queries)
			for _, want := range []string{"SELECT `ztext`, `id`, `amount` FROM records", "RETURNING `ztext`, `id`, `amount`", "AS `ztext`", "AS `id`", "AS `amount`"} {
				assert.True(t, strings.Contains(code, want), "generated code is missing expanded projection %q", want)
			}
			for _, wildcard := range []string{"SELECT *", "SELECT r.*", "RETURNING *"} {
				assert.False(t, strings.Contains(code, wildcard), "generated executable SQL retained %q", wildcard)
			}
		})
	}
}

func TestWildcardExpansionKeepsJooqDSLBindings(t *testing.T) {
	queries := "-- name: Read :many\nSELECT * FROM records WHERE id = $id;\n" +
		"-- name: ReadAlias :many\nSELECT r.* FROM records AS r WHERE r.id = $id;\n" +
		"-- name: Delete :many\nDELETE FROM records WHERE id = $id RETURNING *;\n"
	code := generateWildcardProfile(t, "java", "jooq", "runtime", queries)
	for _, want := range []string{"dsl.select(RECORDS.ZTEXT, RECORDS.ID, RECORDS.AMOUNT)", ".from(RECORDS)", ".where(RECORDS.ID.eq(val(id, YdbTypes.UINT64)))", `r.ZTEXT.as("ztext")`, `r.ID.as("id")`, `r.AMOUNT.as("amount")`, ".from(r)", ".where(r.ID.eq(val(id, YdbTypes.UINT64)))"} {
		assert.True(t, strings.Contains(code, want), "jOOQ lost expanded projection or following table/predicate binding %q", want)
	}
	remaining := code
	for _, field := range []string{`field(name("ztext"), YdbTypes.UTF8)`, `field(name("id"), YdbTypes.UINT64)`, `field(name("amount"), YdbTypes.INT32)`} {
		_, tail, ok := strings.Cut(remaining, field)
		require.True(t, ok, "jOOQ RETURNING fields lost catalog order at %s", field)
		remaining = tail
	}
	require.False(t, strings.Contains(code, "asterisk()"), "jOOQ DSL retained a wildcard")
}

func generateWildcardProfile(t *testing.T, language, runtime, runtimeKey, queries string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "schema.sql"), []byte(wildcardSchema))
	write(t, filepath.Join(dir, "queries.sql"), []byte(queries))
	configuration := fmt.Sprintf("version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    %s:\n      out: generated\n      %s: %s\n", language, runtimeKey, runtime)
	write(t, filepath.Join(dir, "sqlc.yaml"), []byte(configuration))
	var stdout, stderr bytes.Buffer
	status := cli.Run([]string{"generate", "--no-remote", "-f", filepath.Join(dir, "sqlc.yaml")}, &stdout, &stderr)
	require.Zero(t, status, "generate failed: %s", stderr.String())
	entries, err := os.ReadDir(filepath.Join(dir, "generated"))
	require.NoError(t, err)
	var code strings.Builder
	for _, entry := range entries {
		if !entry.IsDir() {
			code.Write(mustRead(t, filepath.Join(dir, "generated", entry.Name())))
			code.WriteByte('\n')
		}
	}
	return code.String()
}

type wildcardDatabase struct {
	table model.Table
}

func (d wildcardDatabase) DescribeTable(context.Context, string) (model.Table, error) {
	return d.table, nil
}
func (wildcardDatabase) ValidateQuery(context.Context, string) error { return nil }

func TestConnectedLocalSchemaPreservesPythonModels(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: wildcardSchema}}
	queries := []model.Source{{Name: "query.sql", Text: "-- name: ReadExplicit :many\nSELECT ztext, id, amount FROM records;\n-- name: ReadWildcard :many\nSELECT * FROM records;"}}
	offline, err := analyzer.Analyze(schema, queries)
	require.NoError(t, err)
	remote := model.Table{Name: "records", PrimaryKey: []string{"id"}, Columns: []model.Column{
		{Name: "amount", Type: model.Type{Kind: "Int32"}},
		{Name: "id", Type: model.Type{Kind: "Uint64"}},
		{Name: "ztext", Type: model.Type{Kind: "Utf8"}},
	}}
	online, err := analyzer.AnalyzeWithDatabase(context.Background(), schema, queries, analyzer.Options{}, wildcardDatabase{remote})
	require.NoError(t, err)
	want, err := python.Generate(offline, python.Options{Runtime: "ydb"})
	require.NoError(t, err)
	got, err := python.Generate(online, python.Options{Runtime: "ydb"})
	require.NoError(t, err)
	require.Equal(t, want, got, "connected local-schema generation changed Python model fields, query return classes or SQL")
	for _, file := range got {
		if file.Name == "queries.py" {
			for _, signature := range []string{"def read_explicit(self) -> list[_models.Records]", "def read_wildcard(self) -> list[_models.Records]"} {
				assert.True(t, strings.Contains(string(file.Content), signature), "table model reuse changed: missing %s", signature)
			}
		}
	}
}
