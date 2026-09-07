package config

import (
	"strings"
	"testing"
)

func TestVersionsAndDefaults(t *testing.T) {
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
		`{"version":"1","packages":[{"engine":"ydb","schema":"schema.sql","queries":["a.sql","b.sql"],"path":"db"}]}`,
	} {
		c, err := Parse([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		if c.SQL[0].Gen.Go.Package != "db" || c.SQL[0].Gen.Go.SQLPackage != "database/sql" || len(c.SQL[0].Queries) != 2 {
			t.Fatalf("unexpected config: %+v", c.SQL[0])
		}
	}
}

func TestRejectUnsupportedConfiguration(t *testing.T) {
	base := "version: '2'\nsql:\n- engine: ydb\n  schema: s.sql\n  queries: q.sql\n"
	for _, tc := range []struct{ name, input, want string }{
		{"plugin", base + "plugins: []\n", "migrate"},
		{"codegen", base + "  codegen: []\n", "migrate"},
		{"unknown", base + "  surprise: true\n", "field surprise"},
		{"option", base + "  gen:\n    go:\n      out: db\n      emit_prepared_queries: true\n", "field emit_prepared_queries"},
		{"engine", strings.Replace(base, "ydb", "postgresql", 1), "engine must be ydb"},
		{"documents", base + "---\nversion: '2'\n", "exactly one"},
		{"empty path", strings.Replace(base, "s.sql", "''", 1), "non-empty path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.input))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v; want %s", err, tc.want)
			}
		})
	}
}

func TestAdditionalBuiltinTargets(t *testing.T) {
	base := "version: '2'\nsql:\n- engine: ydb\n  schema: s.sql\n  queries: q.sql\n  gen:\n"
	c, err := Parse([]byte(base + "    cpp:\n      out: cpp\n    csharp:\n      out: cs\n    java:\n      out: java\n"))
	if err != nil {
		t.Fatal(err)
	}
	g := c.SQL[0].Gen
	if g.CPP.Namespace != "db" || g.CPP.Runtime != "ydb" || g.CSharp.Namespace != "Db" || g.Java.Package != "db" || g.Java.Runtime != "ydb" {
		t.Fatalf("unexpected defaults: %+v %+v %+v", g.CPP, g.CSharp, g.Java)
	}
	for _, runtime := range []string{"native", "ydb", "jdbc", "spring", "hibernate"} {
		if _, err := Parse([]byte(base + "    java:\n      out: java\n      runtime: " + runtime + "\n")); err != nil {
			t.Fatalf("Java %s: %v", runtime, err)
		}
	}
	for _, runtime := range []string{"native", "ydb", "userver"} {
		if _, err := Parse([]byte(base + "    cpp:\n      out: cpp\n      runtime: " + runtime + "\n")); err != nil {
			t.Fatalf("C++ %s: %v", runtime, err)
		}
	}
	for _, options := range []string{
		"    cpp:\n      namespace: db\n",
		"    csharp:\n      namespace: Db\n",
		"    java:\n      package: db\n",
		"    cpp:\n      out: cpp\n      runtime: imaginary\n",
		"    java:\n      out: java\n      runtime: imaginary\n",
		"    csharp:\n      out: cs\n      runtime: native\n",
	} {
		if _, err := Parse([]byte(base + options)); err == nil {
			t.Errorf("expected invalid options to fail: %s", options)
		}
	}
}
