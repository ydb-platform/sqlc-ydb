package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const functionSchema = `CREATE TABLE records (
    id Uint64 NOT NULL,
    payload String NOT NULL,
    PRIMARY KEY(id)
);`

const customHashConfig = `  analyzer:
    functions:
    - name: Acme::Hash
      args:
      - {name: value, type: String}
      returns: Uint64
`

func TestConfiguredFunctionCompileGenerateAndDiff(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "schema.sql"), functionSchema)
	put(t, filepath.Join(dir, "queries.sql"), `-- name: InspectFunctions :many
$local = Acme::Hash("local");
SELECT
    Acme::Hash("select") AS direct,
    CAST(Acme::Hash("cast") AS String) AS casted,
    Acme::Hash(r.payload) AS member_value,
    $local AS local_value
FROM records AS r
WHERE r.id = Acme::Hash("where");`)
	cfg := filepath.Join(dir, "sqlc.yaml")
	put(t, cfg, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n"+customHashConfig+"  gen:\n    go:\n      out: db\n")

	code, _, stderr := invoke("compile", "-f", cfg)
	require.Zero(t, code, stderr)
	_, err := os.Stat(filepath.Join(dir, "db"))
	require.ErrorIs(t, err, os.ErrNotExist, "compile wrote generated output")
	code, _, stderr = invoke("generate", "-f", cfg)
	require.Zero(t, code, stderr)
	code, stdout, stderr := invoke("diff", "-f", cfg)
	require.Zero(t, code, stderr)
	require.Empty(t, stdout)
	models, err := os.ReadFile(filepath.Join(dir, "db", "models.go"))
	require.NoError(t, err)
	normalizedModels := strings.Join(strings.Fields(string(models)), " ")
	for _, field := range []string{"Direct uint64", "Casted []byte", "MemberValue uint64", "LocalValue uint64"} {
		assert.True(t, strings.Contains(normalizedModels, field), "generated models do not contain %q:\n%s", field, models)
	}
	generatedQuery, err := os.ReadFile(filepath.Join(dir, "db", "queries.sql.go"))
	require.NoError(t, err)
	require.False(t, strings.Count(string(generatedQuery), "Acme::Hash") != 5 || !strings.Contains(string(generatedQuery), `$local = Acme::Hash`) || !strings.Contains(string(generatedQuery), "Acme::Hash(r.payload)"), "generated SQL did not preserve configured function calls:\n%s", generatedQuery)
}

func TestConfiguredFunctionDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name      string
		functions string
		query     string
		want      string
	}{
		{
			name:      "unknown argument type",
			functions: strings.Replace(customHashConfig, "type: String", "type: Mystery", 1),
			query:     "SELECT Acme::Hash(payload) AS value FROM records;",
			want:      `function "Acme::Hash" argument 1`,
		},
		{
			name:      "unknown result type",
			functions: strings.Replace(customHashConfig, "returns: Uint64", "returns: Mystery", 1),
			query:     "SELECT Acme::Hash(payload) AS value FROM records;",
			want:      `function "Acme::Hash" return type`,
		},
		{
			name:      "duplicate custom signature",
			functions: customHashConfig + strings.Replace(customHashConfig, "  analyzer:\n    functions:\n", "", 1),
			query:     "SELECT Acme::Hash(payload) AS value FROM records;",
			want:      "ambiguous duplicate overload",
		},
		{
			name:      "namespace spelling is case sensitive",
			functions: customHashConfig,
			query:     "SELECT acme::Hash(payload) AS value FROM records;",
			want:      `unsupported YQL function "acme::Hash"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			put(t, filepath.Join(dir, "schema.sql"), functionSchema)
			put(t, filepath.Join(dir, "queries.sql"), "-- name: Check :many\n"+tc.query)
			cfg := filepath.Join(dir, "sqlc.yaml")
			put(t, cfg, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n"+tc.functions)
			code, _, stderr := invoke("compile", "-f", cfg)
			require.NotZero(t, code, stderr)
			require.Contains(t, stderr, tc.want)
		})
	}
}

func TestConfiguredFunctionsDoNotLeakBetweenSQLSetsOrRuns(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "schema.sql"), functionSchema)
	put(t, filepath.Join(dir, "configured.sql"), "-- name: Configured :many\nSELECT Acme::Hash(payload) AS value FROM records;")
	put(t, filepath.Join(dir, "unconfigured.sql"), "-- name: Unconfigured :many\nSELECT Acme::Hash(payload) AS value FROM records;")

	first := filepath.Join(dir, "first.yaml")
	put(t, first, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: configured.sql\n"+customHashConfig+"- engine: ydb\n  schema: schema.sql\n  queries: unconfigured.sql\n")
	code, _, stderr := invoke("compile", "-f", first)
	require.NotZero(t, code, "second SQL set inherited functions: %s", stderr)
	require.Contains(t, stderr, `unsupported YQL function "Acme::Hash"`)

	configuredOnly := filepath.Join(dir, "configured.yaml")
	put(t, configuredOnly, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: configured.sql\n"+customHashConfig)
	code, _, stderr = invoke("compile", "-f", configuredOnly)
	require.Zero(t, code, stderr)
	unconfiguredOnly := filepath.Join(dir, "unconfigured.yaml")
	put(t, unconfiguredOnly, "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: unconfigured.sql\n")
	code, _, stderr = invoke("compile", "-f", unconfiguredOnly)
	require.NotZero(t, code, "later run inherited functions: %s", stderr)
	require.Contains(t, stderr, `unsupported YQL function "Acme::Hash"`)
}
