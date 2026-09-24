package java

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/jdbc"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestJooqTabularSourcesUseNamedSQL(t *testing.T) {
	const schema = "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"
	for _, tc := range []struct {
		name, query string
		want        []string
	}{
		{"binding", "$selected = (SELECT id FROM records); SELECT id FROM $selected;", []string{"$selected = (SELECT id FROM", "dsl.render(RECORDS)", "FROM $selected"}},
		{"inferred parameter", "$selected = (SELECT id FROM records WHERE id = $id); SELECT id FROM $selected;", []string{"DECLARE $id AS Uint64;", "WHERE id = $id", "dsl.render(RECORDS)", "FROM $selected"}},
		{"derived source", "SELECT d.id FROM (SELECT id FROM records) AS d;", []string{"FROM (SELECT id FROM", "dsl.render(RECORDS)", " AS d"}},
		{"nested derived source", "SELECT id FROM records WHERE id IN (SELECT id FROM (SELECT id FROM records) AS d);", []string{"WHERE id IN (SELECT id FROM (SELECT id FROM", "dsl.render(RECORDS)", " AS d"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + tc.query}})
			require.NoError(t, err)
			files, err := Generate(analysis, Options{Package: "tabular", Runtime: "jooq"})
			require.NoError(t, err)
			for _, file := range files {
				if file.Name != "Queries.java" {
					continue
				}
				code := string(file.Content)
				for _, want := range tc.want {
					require.Contains(t, code, want, "generated query is missing %q:\n%s", want, code)
				}
				require.False(t, strings.Contains(code, "WHERE id = ?"), "named parameter became a positional placeholder:\n%s", code)
				return
			}
			require.FailNow(t, "Queries.java was not generated")
		})
	}
}

func TestJooqTabularNamedSQLCompilesAndPreservesText(t *testing.T) {
	const schema = "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"
	const query = "-- name: Read :many\n$selected = (SELECT id FROM records WHERE id = $id); SELECT id FROM $selected;"
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	expression, err := jooqDeclaredSQL(analysis.Queries[0], jdbc.NamedSQL(analysis.Queries[0]))
	require.NoError(t, err)
	const want = "DECLARE $id AS Uint64;\n$selected = (SELECT id FROM `/mapped/records` AS `records` WHERE id = $id); SELECT id FROM $selected;"
	program := fmt.Sprintf(`public class Main {
    static final String RECORDS = "`+"`"+`/mapped/records`+"`"+`";
    static final Main dsl = new Main();
    String render(String table) { return table; }
    public static void main(String[] args) {
        if (!java.util.Base64.getEncoder().encodeToString((%s).getBytes(java.nio.charset.StandardCharsets.UTF_8)).equals(%q)) throw new AssertionError("SQL changed");
    }
}`, expression, base64.StdEncoding.EncodeToString([]byte(want)))
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Main.java"), []byte(program), 0600))
	for _, args := range [][]string{{"javac", "Main.java"}, {"java", "-cp", dir, "Main"}} {
		command := exec.Command(args[0], args[1:]...)
		command.Dir = dir
		if out, err := command.CombinedOutput(); err != nil {
			require.NoError(t, err, "%s: %v\n%s", args[0], err, out)
		}
	}
}

func TestJooqTabularNamedSQLRejectsReservedParameter(t *testing.T) {
	const schema = "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"
	const query = "-- name: Read :many\n$selected = (SELECT id FROM records WHERE id = $tech); SELECT id FROM $selected;"
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	files, err := Generate(analysis, Options{Package: "tabular", Runtime: "jooq"})
	require.False(t, files != nil || err == nil || !strings.Contains(err.Error(), "Java parameter collision: tech"), "files = %v, error = %v; want Java parameter collision and no output", files, err)
}

func TestJooqUDFCallsUseNamedSQL(t *testing.T) {
	for _, tc := range []struct {
		name, schema, query string
		want                []string
	}{
		{"literal callable", "", `SELECT String::Base32Encode("book") AS encoded, Pire::Grep("book")("notebook") AS matched;`, []string{`String::Base32Encode(\"book\")`, `Pire::Grep(\"book\")(\"notebook\")`, "YdbPrepareMode.DATA_QUERY"}},
		{"inferred parameter and table", "CREATE TABLE books (id Uint64 NOT NULL, PRIMARY KEY(id));", `SELECT Pire::Grep("book")("notebook") AS matched FROM books WHERE id = $id;`, []string{"DECLARE $id AS Uint64;", "dsl.render(BOOKS)", "id = $id", `Pire::Grep(\"book\")(\"notebook\")`, "_prepared.setObject(\"id\""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var schema []model.Source
			if tc.schema != "" {
				schema = []model.Source{{Name: "schema.sql", Text: tc.schema}}
			}
			analysis, err := analyzer.Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + tc.query}})
			require.NoError(t, err)
			wantSQL := tc.query
			if tc.name == "inferred parameter and table" {
				wantSQL = "DECLARE $id AS Uint64;\n" + wantSQL
			}
			if got := jdbc.NamedSQL(analysis.Queries[0]); got != wantSQL {
				require.Equal(t, wantSQL, got, "named SQL = %q, want %q", got, wantSQL)
			}
			files, err := Generate(analysis, Options{Package: "udf", Runtime: "jooq"})
			require.NoError(t, err)
			for _, file := range files {
				if file.Name != "Queries.java" {
					continue
				}
				code := string(file.Content)
				for _, want := range tc.want {
					require.Contains(t, code, want, "generated query is missing %q:\n%s", want, code)
				}
				require.False(t, strings.Contains(code, "id = ?") || strings.Contains(code, "_prepared.setObject(\"id\", null)"), "named parameter was lost:\n%s", code)
				return
			}
			require.FailNow(t, "Queries.java was not generated")
		})
	}
}
