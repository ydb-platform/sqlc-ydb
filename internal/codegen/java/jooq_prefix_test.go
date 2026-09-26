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

const jooqPrefixSchema = "CREATE TABLE `/local/a/users` (id Uint64 NOT NULL, name Utf8 NOT NULL, INDEX by_name GLOBAL SYNC ON(name), PRIMARY KEY(id));\nCREATE TABLE `/local/b/users` (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY(id));"
const jooqPrefixPragma = "PRAGMA /* namespace 🚀 {0} */ TablePathPrefix = '/local/a'"

func TestJooqPrefixDSL(t *testing.T) {
	queries := `-- name: Read :many
` + jooqPrefixPragma + `;
-- repeated static prefix stays attached to the statement
PRAGMA TablePathPrefix('/local/a');
SELECT users.id FROM users VIEW by_name WHERE users.name = $name;
-- name: JoinUsers :many
` + jooqPrefixPragma + `;
SELECT a.id AS id FROM users AS a JOIN ` + "`/local/b/users`" + ` AS b ON a.id = b.id WHERE a.name = $name;
-- name: Write :exec
` + jooqPrefixPragma + `;
UPSERT INTO users (id, name) VALUES ($id, $name);
-- name: Remove :one
` + jooqPrefixPragma + `;
DELETE FROM users WHERE users.id = $id RETURNING id;
-- name: ReadPath :many
` + jooqPrefixPragma + `;
SELECT ` + "`../a/users`.id FROM `../a/users` WHERE `../a/users`.name = $name;" + `
-- name: QuestionSingle :many
PRAGMA TablePathPrefix('/db/?w');
SELECT u.id FROM ` + "`/local/a/users`" + ` AS u WHERE u.name = $name;
-- name: QuestionDouble :many
PRAGMA TablePathPrefix("/db/?w");
SELECT u.id FROM ` + "`/local/a/users`" + ` AS u WHERE u.name = $name;
`
	a, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqPrefixSchema}}, []model.Source{{Name: "queries.sql", Text: queries}})
	require.NoError(t, err)
	files, err := Generate(a, Options{Package: "prefix", Runtime: "jooq"})
	require.NoError(t, err)
	code := string(files[len(files)-1].Content)
	for _, want := range []string{jooqPrefixPragma, `dsl.resultQuery("{0};\n{1}"`, `dsl.query("{0};\n{1}"`, "LOCAL_A_USERS", "LOCAL_B_USERS"} {
		require.Contains(t, code, want, "missing %q in %s", want, code)
	}
	require.False(t, strings.Contains(code, "DECLARE"), "inferred parameters must retain DSL bindings", code)
	t.Run("published dialect", func(t *testing.T) { runJooqPrefixSDK(t, files) })
}

func TestJooqSeparatedPragmasUseDeclaredSQL(t *testing.T) {
	query := `-- name: Read :many
PRAGMA OrderedColumns;
DECLARE $id AS Uint64;
PRAGMA TablePathPrefix('/local/a');
SELECT id FROM users WHERE id = $id;`
	a, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqPrefixSchema}}, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	files, err := Generate(a, Options{Package: "prefix", Runtime: "jooq"})
	require.NoError(t, err)
	code := string(files[len(files)-1].Content)
	require.Equal(t, 1, strings.Count(code, "PRAGMA OrderedColumns;"))
	require.Equal(t, 1, strings.Count(code, "PRAGMA TablePathPrefix('/local/a');"))
}

func TestJooqAliasCollisionNamesAuthoredAlias(t *testing.T) {
	for _, aliases := range [][2]string{{"a/b", "a_b"}, {"a_b", "a/b"}} {
		t.Run(aliases[0]+" then "+aliases[1], func(t *testing.T) {
			query := fmt.Sprintf("-- name: Colliding :many\nSELECT `%s`.id FROM `/local/a/users` AS `%s` JOIN `/local/b/users` AS `%s` ON `%s`.id = `%s`.id;", aliases[0], aliases[0], aliases[1], aliases[0], aliases[1])
			analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqPrefixSchema}}, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
			files, err := Generate(analysis, Options{Package: "prefix", Runtime: "jooq"})
			want := fmt.Sprintf("Colliding: Java alias collision: aB (from %q and %q)", aliases[0], aliases[1])
			require.False(t, files != nil || err == nil || err.Error() != want, "files %v, error %v; want %q", files, err, want)
		})
	}
}

func TestJooqAliasCollisionWithParametersAndLocals(t *testing.T) {
	for _, tc := range []struct{ alias, parameter, identifier string }{
		{"a/b", "a_b", "aB"},
		{"stmt", "id", "stmt"},
	} {
		t.Run(tc.alias, func(t *testing.T) {
			query := fmt.Sprintf("-- name: Colliding :many\nSELECT `%s`.id FROM `/local/a/users` AS unaffected JOIN `/local/b/users` AS `%s` ON unaffected.id = `%s`.id WHERE `%s`.id = $%s;", tc.alias, tc.alias, tc.alias, tc.alias, tc.parameter)
			analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqPrefixSchema}}, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
			files, err := Generate(analysis, Options{Package: "prefix", Runtime: "jooq"})
			want := fmt.Sprintf("Colliding: Java alias collision: %s (from %q)", tc.identifier, tc.alias)
			require.False(t, files != nil || err == nil || err.Error() != want, "files %v, error %v; want %q", files, err, want)
		})
	}
}

// Generate must reject incomplete semantic input rather than mapping an authored
// relative table name as though it were the resolved physical namespace.
func TestJooqPrefixRejectsMissingTableResolution(t *testing.T) {
	for _, tc := range []struct{ name, command, statement string }{
		{"select", ":many", "SELECT users.id FROM users WHERE users.id = $id;"},
		{"delete", ":exec", "DELETE FROM users WHERE users.id = $id;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := "-- name: Incomplete " + tc.command + "\n" + jooqPrefixPragma + ";\nDECLARE $id AS Uint64;\n" + tc.statement
			analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqPrefixSchema}}, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
			// The analyzer normally supplies this metadata. Exercise the generator's
			// explicit invalid-input contract while retaining the real parsed query.
			analysis.Queries[0].Syntax.Tables = nil
			files, err := Generate(analysis, Options{Package: "prefix", Runtime: "jooq"})
			require.False(t, files != nil || err == nil || err.Error() != `Incomplete: missing resolved jOOQ table "users"`, "incomplete table resolution produced files %v, error %v", files, err)
		})
	}
}

func TestJooqPrefixDeclaredSQLBytesThroughJava(t *testing.T) {
	header := "-- name: Read :many\n" + jooqPrefixPragma + ";\nDECLARE $id AS Uint64;\n-- literals and comments are retained 🪄\n"
	var program strings.Builder
	program.WriteString("public class Main { static final String LOCAL_A_USERS = \"`/local/a/mapped_users`\", LOCAL_B_USERS = \"`/local/b/mapped_users`\"; static final Main dsl = new Main(); String render(String table) { return table; } public static void main(String[] args) {\n")
	for i, tc := range []struct{ sql, want string }{
		{"SELECT users.id FROM users WHERE users.id = $id;", "SELECT users.id FROM `/local/a/mapped_users` AS `users` WHERE users.id = $id;"},
		{"SELECT users.id FROM users /* index 🐘 */ VIEW by_name WHERE users.id = $id;", "SELECT users.id FROM `/local/a/mapped_users` /* index 🐘 */ VIEW by_name AS `users` WHERE users.id = $id;"},
		{"SELECT a.id AS id FROM users AS a JOIN `/local/b/users` AS b ON a.id = b.id WHERE a.id = $id;", "SELECT a.id AS id FROM `/local/a/mapped_users` AS a JOIN `/local/b/mapped_users` AS b ON a.id = b.id WHERE a.id = $id;"},
		{"DELETE FROM users WHERE users.id = $id RETURNING id;", "DELETE FROM `/local/a/mapped_users` WHERE `/local/a/mapped_users`.id = $id RETURNING id;"},
		{"SELECT users.id, 'users /local/a/users'u AS label FROM users WHERE users.id = $id;", "SELECT users.id, 'users /local/a/users'u AS label FROM `/local/a/mapped_users` AS `users` WHERE users.id = $id;"},
		{"SELECT `../a/users`.id FROM `../a/users` WHERE `../a/users`.id = $id;", "SELECT `../a/users`.id FROM `/local/a/mapped_users` AS `../a/users` WHERE `../a/users`.id = $id;"},
		{"UPSERT INTO users SELECT users.id AS id, users.name AS name FROM users WHERE users.id = $id;", "UPSERT INTO `/local/a/mapped_users` SELECT users.id AS id, users.name AS name FROM `/local/a/mapped_users` AS `users` WHERE users.id = $id;"},
	} {
		annotation := header
		if strings.HasPrefix(tc.sql, "UPSERT") {
			annotation = strings.Replace(header, ":many", ":exec", 1)
		}
		a, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqPrefixSchema}}, []model.Source{{Name: "q.sql", Text: annotation + tc.sql}})
		require.NoError(t, err)
		q := a.Queries[0]
		sql, _ := jdbc.SQL(q)
		expression, err := jooqDeclaredSQL(q, sql)
		require.NoError(t, err)
		want := strings.TrimPrefix(header, "-- name: Read :many\n") + tc.want
		fmt.Fprintf(&program, "if (!java.util.Base64.getEncoder().encodeToString((%s).getBytes(java.nio.charset.StandardCharsets.UTF_8)).equals(%q)) throw new AssertionError(\"case %d\");\n", expression, base64.StdEncoding.EncodeToString([]byte(want)), i)
	}
	program.WriteString("}}")
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Main.java"), []byte(program.String()), 0600))
	for _, args := range [][]string{{"javac", "--release", "17", "Main.java"}, {"java", "-cp", dir, "Main"}} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			require.NoError(t, err, "%s: %v\n%s\n%s", args[0], err, out, program.String())
		}
	}
}

func runJooqPrefixSDK(t *testing.T, files []model.File) {
	t.Helper()
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN to execute prefixed queries against the published dialect")
	}
	dir := t.TempDir()
	classpath := filepath.Join(dir, "classpath")
	cmd := exec.Command(maven, "-q", "dependency:build-classpath", "-Dmdep.outputFile="+classpath)
	cmd.Dir = filepath.Join("..", "..", "..", "tests", "examples", "java", "jooq")
	if out, err := cmd.CombinedOutput(); err != nil {
		require.NoError(t, err, "SDK classpath: %v\n%s", err, out)
	}
	cp, err := os.ReadFile(classpath)
	require.NoError(t, err)
	program := `package prefix;
import java.util.*;
import org.jooq.conf.*;
import org.jooq.tools.jdbc.*;
import org.jooq.types.ULong;
import tech.ydb.jooq.YDB;
import tech.ydb.table.values.PrimitiveValue;
public class Main {
    public static void main(String[] args) throws Exception {
        for (String target : List.of("mapped_users", "/local/mapped/users")) {
            var statements = new ArrayList<String>();
            try (var connection = new MockConnection(ctx -> {
                String sql = ctx.sql();
                statements.add(sql);
                String pragma = switch (statements.size()) {
                    case 6 -> "PRAGMA TablePathPrefix('/db/?w')";
                    case 7 -> "PRAGMA TablePathPrefix(\"/db/?w\")";
                    default -> "` + jooqPrefixPragma + `";
                };
                if (!sql.substring(0, sql.indexOf(';')).equals(pragma) || !sql.contains("` + "`" + `" + target + "` + "`" + `")) throw new AssertionError(sql);
                Object[] want = switch (statements.size()) {
                    case 1, 2, 5, 6, 7 -> new Object[]{"Name"};
                    case 3 -> new Object[]{PrimitiveValue.newUint64(-1L), "Name"};
                    case 4 -> new Object[]{PrimitiveValue.newUint64(-1L)};
                    default -> throw new AssertionError("extra execution " + sql);
                };
                if (!Arrays.equals(ctx.bindings(), want)) throw new AssertionError(Arrays.toString(ctx.bindings()) + " type=" + ctx.bindings()[0].getClass() + " SQL=" + sql);
                if (statements.size() == 3 && !sql.contains("into ` + "`" + `" + target + "` + "`" + ` (` + "`id`, `name`" + `)")) throw new AssertionError(sql);
                if (statements.size() == 3) return new MockResult[]{new MockResult(1)};
                var dsl = YDB.using();
                var result = dsl.newResult(Tables.LOCAL_A_USERS.ID);
                result.add(dsl.newRecord(Tables.LOCAL_A_USERS.ID).values(ULong.MAX));
                return new MockResult[]{new MockResult(1, result)};
            })) {
                var settings = new Settings().withRenderMapping(new RenderMapping().withSchemata(new MappedSchema().withInput("").withTables(
                        new MappedTable().withInput("/local/a/users").withOutput(target), new MappedTable().withInput("/local/b/users").withOutput("/local/b/mapped"))));
                var queries = new Queries(YDB.using(connection, settings));
                if (!queries.read("Name").get(0).id().equals(ULong.MAX)) throw new AssertionError("read");
                if (!queries.joinUsers("Name").get(0).id().equals(ULong.MAX)) throw new AssertionError("join");
                queries.write(ULong.MAX, "Name");
                if (!queries.remove(ULong.MAX).orElseThrow().id().equals(ULong.MAX)) throw new AssertionError("returning");
                if (!queries.readPath("Name").get(0).id().equals(ULong.MAX)) throw new AssertionError("relative path");
                if (!queries.questionSingle("Name").get(0).id().equals(ULong.MAX)) throw new AssertionError("single-quoted question mark");
                if (!queries.questionDouble("Name").get(0).id().equals(ULong.MAX)) throw new AssertionError("double-quoted question mark");
                if (statements.size() != 7 || !statements.get(0).contains("VIEW ` + "`by_name`" + `") || !statements.get(1).contains("` + "`/local/b/mapped`" + `")) throw new AssertionError(statements);
                if (!statements.get(0).contains("-- repeated static prefix stays attached to the statement\nPRAGMA TablePathPrefix('/local/a');") || !statements.get(4).contains("` + "`../a/users`.`id`" + `")) throw new AssertionError(statements);
            }
        }
    }
}`
	files = append(files, model.File{Name: "Main.java", Content: []byte(program)})
	compile := []string{"-cp", strings.TrimSpace(string(cp)), "-d", dir}
	for _, file := range files {
		path := filepath.Join(dir, file.Name)
		require.NoError(t, os.WriteFile(path, file.Content, 0600))
		compile = append(compile, path)
	}
	for _, args := range [][]string{append([]string{"javac"}, compile...), {"java", "-cp", dir + string(os.PathListSeparator) + strings.TrimSpace(string(cp)), "prefix.Main"}} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			require.NoError(t, err, "%s: %v\n%s", args[0], err, out)
		}
	}
}
