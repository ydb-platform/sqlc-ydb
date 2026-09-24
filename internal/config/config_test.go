package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatsAndDefaults(t *testing.T) {
	for _, s := range []string{
		`version: "2"
sql:
- engine: ydb
  schema: schema.sql
  queries: [a.sql, b.sql]
  gen:
    go:
      out: db
    python:
      out: py
`,
		`{"version":"2","sql":[{"engine":"ydb","schema":"schema.sql","queries":["a.sql","b.sql"],"gen":{"go":{"out":"db"}}}]}`,
	} {
		c, err := Parse([]byte(s))
		require.NoError(t, err)
		require.Len(t, c.SQL, 1)
		require.Equal(t, "db", c.SQL[0].Gen.Go.Package)
		require.Equal(t, "database/sql", c.SQL[0].Gen.Go.SQLPackage)
		require.Len(t, c.SQL[0].Queries, 2)
	}
}

func TestRejectUnsupportedConfiguration(t *testing.T) {
	base := "version: '2'\nsql:\n- engine: ydb\n  schema: s.sql\n  queries: q.sql\n"
	for _, tc := range []struct{ name, input, want string }{
		{"unsupported version", strings.Replace(base, "version: '2'", "version: '1'", 1), `version must be "2"`},
		{"legacy packages", "version: '1'\npackages: []\n", "field packages"},
		{"plugin", base + "plugins: []\n", "migrate"},
		{"codegen", base + "  codegen: []\n", "migrate"},
		{"unknown", base + "  surprise: true\n", "field surprise"},
		{"option", base + "  gen:\n    go:\n      out: db\n      emit_prepared_queries: true\n", "field emit_prepared_queries"},
		{"removed JavaScript target", base + "  gen:\n    javascript:\n      out: js\n", "field javascript"},
		{"ignored Python package", base + "  gen:\n    python:\n      out: py\n      package: ignored\n", "Python package directory is selected with out"},
		{"engine", strings.Replace(base, "ydb", "postgresql", 1), "engine must be ydb"},
		{"documents", base + "---\nversion: '2'\n", "exactly one"},
		{"empty path", strings.Replace(base, "s.sql", "''", 1), "non-empty path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.input))
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestFunctionSignatureConfiguration(t *testing.T) {
	c, err := Parse([]byte(`version: "2"
sql:
- engine: ydb
  schema: schema.sql
  queries: queries.sql
  analyzer:
    functions:
    - name: Acme::Score
      args:
      - {name: value, type: Utf8, auto_map: true}
      - {name: mode, type: Uint32, optional: true}
      returns: Double
`))
	require.NoError(t, err)
	require.Len(t, c.SQL, 1)
	require.Len(t, c.SQL[0].Analyzer.Functions, 1)
	got := c.SQL[0].Analyzer.Functions[0]
	require.Equal(t, "Acme::Score", got.Name)
	require.Equal(t, "Double", got.Returns)
	require.Len(t, got.Args, 2)
	require.Equal(t, "value", got.Args[0].Name)
	require.True(t, got.Args[0].AutoMap)
	require.True(t, got.Args[1].Optional)
}

func TestRejectInvalidFunctionSignatureConfiguration(t *testing.T) {
	base := "version: '2'\nsql:\n- engine: ydb\n  schema: s.sql\n  queries: q.sql\n  analyzer:\n    functions:\n"
	for _, tc := range []struct{ name, input, want string }{
		{"empty function name", "    - name: ''\n      args: [{type: Utf8}]\n      returns: Uint64\n", "function name"},
		{"invalid function name", "    - name: Acme::bad-name\n      args: [{type: Utf8}]\n      returns: Uint64\n", "function name"},
		{"empty argument type", "    - name: Acme::Hash\n      args: [{type: ''}]\n      returns: Uint64\n", "argument 1 type"},
		{"invalid argument name", "    - name: Acme::Hash\n      args: [{name: bad-name, type: Utf8}]\n      returns: Uint64\n", "argument 1 name"},
		{"duplicate argument name", "    - name: Acme::Hash\n      args: [{name: value, type: Utf8}, {name: value, type: Uint64}]\n      returns: Uint64\n", "duplicate argument name"},
		{"empty return type", "    - name: Acme::Hash\n      args: [{type: Utf8}]\n      returns: ''\n", "return type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(base + tc.input))
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestAdditionalBuiltinTargets(t *testing.T) {
	base := "version: '2'\nsql:\n- engine: ydb\n  schema: s.sql\n  queries: q.sql\n  gen:\n"
	c, err := Parse([]byte(base + "    cpp:\n      out: cpp\n    csharp:\n      out: cs\n    java:\n      out: java\n    typescript:\n      out: js\n    rust:\n      out: rust\n    php:\n      out: php\n"))
	require.NoError(t, err)
	g := c.SQL[0].Gen
	require.Equal(t, "db", g.CPP.Namespace)
	require.Equal(t, "ydb", g.CPP.Runtime)
	require.Equal(t, "Db", g.CSharp.Namespace)
	require.Equal(t, "db", g.Java.Package)
	require.Equal(t, "ydb", g.Java.Runtime)
	require.Equal(t, "adonet", g.CSharp.Runtime)
	require.Equal(t, "ydb", g.TypeScript.Runtime)
	require.Equal(t, "ydb", g.Rust.Runtime)
	require.Equal(t, "ydb", g.PHP.Runtime)
	require.Equal(t, "Db", g.PHP.Namespace)
	for _, runtime := range []string{"adonet", "dapper"} {
		_, err = Parse([]byte(base + "    csharp:\n      out: cs\n      runtime: " + runtime + "\n"))
		require.NoError(t, err, "C# %s", runtime)
	}
	for _, target := range []string{"typescript", "rust", "php"} {
		for _, options := range []string{"{}", "{out: output, runtime: imaginary}"} {
			_, err = Parse([]byte(base + "    " + target + ": " + options + "\n"))
			assert.Error(t, err, "accepted invalid %s options: %s", target, options)
		}
	}
	for _, runtime := range []string{"native", "ydb", "jdbc", "jooq"} {
		_, err = Parse([]byte(base + "    java:\n      out: java\n      runtime: " + runtime + "\n"))
		require.NoError(t, err, "Java %s", runtime)
	}
	for _, runtime := range []string{"spring", "hibernate"} {
		_, err = Parse([]byte(base + "    java:\n      out: java\n      runtime: " + runtime + "\n"))
		assert.Error(t, err, "accepted removed Java runtime %s", runtime)
	}
	for _, runtime := range []string{"native", "ydb", "userver"} {
		_, err = Parse([]byte(base + "    cpp:\n      out: cpp\n      runtime: " + runtime + "\n"))
		require.NoError(t, err, "C++ %s", runtime)
	}
	for _, options := range []string{
		"    cpp:\n      namespace: db\n",
		"    csharp:\n      namespace: Db\n",
		"    java:\n      package: db\n",
		"    cpp:\n      out: cpp\n      runtime: imaginary\n",
		"    java:\n      out: java\n      runtime: imaginary\n",
		"    csharp:\n      out: cs\n      runtime: native\n",
	} {
		_, err = Parse([]byte(base + options))
		assert.Error(t, err, "expected invalid options to fail: %s", options)
	}
}

func TestKotlinConfiguration(t *testing.T) {
	base := "version: '2'\nsql:\n- engine: ydb\n  schema: s.sql\n  queries: q.sql\n  gen:\n    kotlin:\n"
	for _, runtime := range []string{"", "native", "ydb", "jdbc", "exposed"} {
		t.Run("runtime_"+runtime, func(t *testing.T) {
			options := "      out: generated/kotlin\n"
			if runtime != "" {
				options += "      runtime: " + runtime + "\n      package: example.db\n"
			}
			c, err := Parse([]byte(base + options))
			require.NoError(t, err)
			g := c.SQL[0].Gen.Kotlin
			wantRuntime, wantPackage := runtime, "example.db"
			if runtime == "" || runtime == "native" {
				wantRuntime = "ydb"
			}
			if runtime == "" {
				wantPackage = "db"
			}
			require.Equal(t, "generated/kotlin", g.Out)
			require.Equal(t, wantPackage, g.Package)
			require.Equal(t, wantRuntime, g.Runtime)
		})
	}
	for _, tc := range []struct{ name, options, want string }{
		{"missing output", "      package: db\n", "gen.kotlin.out is required"},
		{"unsupported runtime", "      out: kt\n      runtime: spring\n", "unsupported kotlin runtime"},
		{"unknown option", "      out: kt\n      emit_async: true\n", "field emit_async"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(base + tc.options))
			require.ErrorContains(t, err, tc.want)
		})
	}
}
