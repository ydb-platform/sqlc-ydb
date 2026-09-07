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
