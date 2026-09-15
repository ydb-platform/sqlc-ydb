package python

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBatchSQLRuntimePreservesWhitespace(t *testing.T) {
	if os.Getenv("SQLC_YDB_PYTHON_SDK_CHECK") == "" {
		t.Skip("set SQLC_YDB_PYTHON_SDK_CHECK=1 with pinned Python SDK")
	}
	sql := "-- batch comment\nDECLARE $books AS List<Struct<\n    book_id: Uint64,\n\t tags: Json,\n    title: Optional<Utf8>\n>>;\n\n-- preserve the string value too\nINSERT INTO books (book_id, tags, title)\nSELECT book_id, tags, @@first line\n    value indentation\n\nlast line@@\nFROM AS_TABLE($books);  \n"
	for _, runtime := range []string{"ydb", "dbapi", "sqlalchemy"} {
		t.Run(runtime, func(t *testing.T) {
			in := structInput()
			in.Queries[0].SQL = "-- name: CreateBooks :exec\n" + sql
			files, err := Generate(in, Options{Runtime: runtime})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			pkg := filepath.Join(dir, "generated")
			if err := os.Mkdir(pkg, 0700); err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				if f.Name == "queries.py" {
					indent := "            "
					if runtime == "dbapi" {
						indent = "                "
					}
					if !strings.Contains(string(f.Content), "\n"+indent+"parameters,\n"+indent[4:]+")") {
						t.Fatalf("unaligned execute arguments:\n%s", f.Content)
					}
				}
				if err := os.WriteFile(filepath.Join(pkg, f.Name), f.Content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			want := sql
			if runtime == "sqlalchemy" {
				want = strings.ReplaceAll(sql, "AS_TABLE($books)", "AS_TABLE(?)")
			}
			if err := os.WriteFile(filepath.Join(dir, "expected.sql"), []byte(want), 0600); err != nil {
				t.Fatal(err)
			}
			script := `from pathlib import Path
from generated.queries import Querier
from ydb_sqlalchemy.sqlalchemy import YqlDialect
captured=[]
class Connection:
    def cursor(self): return self
    def close(self): pass
    def execute(self,sql,parameters):
        captured.append(str(sql.compile(dialect=YqlDialect())) if hasattr(sql,"compile") else sql)
        return self
q=Querier.__new__(Querier)
q._connection=Connection()
q._execute=lambda sql,parameters: captured.append(sql)
q.create_books([])
assert captured==[Path("expected.sql").read_text()], repr(captured)
`
			cmd := exec.Command("python3", "-c", script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PYTHONPYCACHEPREFIX="+filepath.Join(dir, "cache"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}
