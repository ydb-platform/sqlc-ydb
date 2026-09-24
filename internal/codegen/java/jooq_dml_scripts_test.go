package java

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

const jooqDMLScriptSchema = `CREATE TABLE records (id Uint64 NOT NULL, label Utf8 NOT NULL, PRIMARY KEY(id));
CREATE TABLE children (id Uint64 NOT NULL, owner Uint64 NOT NULL, PRIMARY KEY(id));`

func TestJooqDMLScriptsRequireDeclaredExecution(t *testing.T) {
	for _, sql := range []string{
		`UPDATE records SET label = "changed"u WHERE id = $id; DELETE FROM records WHERE id = $id;`,
		`UPDATE records SET label = "changed"u WHERE id = 1; DELETE FROM records WHERE id = 2;`,
		`DELETE FROM children WHERE owner = $id; DELETE FROM records WHERE id = $id;`,
	} {
		t.Run(sql, func(t *testing.T) {
			analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqDMLScriptSchema}}, []model.Source{{Name: "queries.sql", Text: "-- name: Change :exec\n" + sql}})
			require.NoError(t, err)
			files, err := Generate(analysis, Options{Package: "scripts", Runtime: "jooq"})
			const want = "Change: multi-statement queries require the jOOQ declared-query path; add an explicit DECLARE for one of the parameters, or use runtime: jdbc or ydb"
			require.False(t, files != nil || err == nil || err.Error() != want, "files=%v, error=%v; want no output and %q", files, err, want)
		})
	}
}

func TestJooqMixedScriptsRequireDeclaredExecution(t *testing.T) {
	for _, sql := range []string{
		"SELECT id FROM records; DELETE FROM records;",
		"DELETE FROM records; SELECT id FROM records;",
		"UPDATE records SET label='changed'u; SELECT id FROM records; DELETE FROM children;",
	} {
		for _, command := range []string{":one", ":many"} {
			t.Run(command+sql, func(t *testing.T) {
				analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqDMLScriptSchema}}, []model.Source{{Name: "queries.sql", Text: "-- name: Change " + command + "\n" + sql}})
				require.NoError(t, err)
				files, err := Generate(analysis, Options{Package: "scripts", Runtime: "jooq"})
				const want = "Change: multi-statement queries require the jOOQ declared-query path; add an explicit DECLARE for one of the parameters, or use runtime: jdbc or ydb"
				require.False(t, files != nil || err == nil || err.Error() != want, "files=%d, error=%v; want %q", len(files), err, want)
			})
		}
	}
}

const jooqDeclaredDMLScripts = `-- name: DeleteBoth :exec
PRAGMA TablePathPrefix('/local/scripts');
DECLARE $id AS Uint64;
-- Keep both operations in the same request.
DELETE FROM children WHERE children.owner = $id;
DELETE FROM records WHERE records.id = $id;
-- name: RenameAndDelete :exec
PRAGMA TablePathPrefix('/local/scripts');
DECLARE $id AS Uint64;
UPDATE records SET label = $label WHERE records.id = $id;
-- A semicolon inside this comment must not split the request: ;
DELETE FROM children WHERE children.owner = $id;`

func TestJooqDeclaredDMLScriptsPreserveWholeRequest(t *testing.T) {
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: "PRAGMA TablePathPrefix('/local/scripts');\n" + jooqDMLScriptSchema}}, []model.Source{{Name: "queries.sql", Text: jooqDeclaredDMLScripts}})
	require.NoError(t, err)
	files, err := Generate(analysis, Options{Package: "scripts", Runtime: "jooq"})
	require.NoError(t, err)
	for _, file := range files {
		if file.Name != "Queries.java" {
			continue
		}
		code := string(file.Content)
		for _, want := range []string{"DECLARE $label AS Utf8;", "DECLARE $id AS Uint64;", "PRAGMA TablePathPrefix('/local/scripts');", "dsl.render(LOCAL_SCRIPTS_RECORDS)", "dsl.render(LOCAL_SCRIPTS_CHILDREN)", "A semicolon inside this comment must not split the request: ;"} {
			require.Contains(t, code, want, "missing %q in generated script:\n%s", want, code)
		}
		require.False(t, strings.Count(code, ".prepareStatement(") != 2 || strings.Count(code, "_prepared.execute();") != 2 || strings.Contains(code, ".executeUpdate("), "expected one JDBC execution per script:\n%s", code)
	}
	t.Run("published SDK", func(t *testing.T) { runJooqDMLScriptSDK(t, files) })
}

func TestJooqDeclaredMixedScriptsPreserveWholeRequest(t *testing.T) {
	const queries = `-- name: UpdateThenRead :many
DECLARE $id AS Uint64;
UPDATE records SET label = $label WHERE id = $id;
SELECT id, label FROM records WHERE id = $id;
-- name: ReadThenDelete :one
DECLARE $id AS Uint64;
SELECT id, label FROM records WHERE id = $id;
DELETE FROM children WHERE owner = $id;`
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqDMLScriptSchema}}, []model.Source{{Name: "queries.sql", Text: queries}})
	require.NoError(t, err)
	files, err := Generate(analysis, Options{Package: "scripts", Runtime: "jooq"})
	require.NoError(t, err)
	for _, file := range files {
		if file.Name != "Queries.java" {
			continue
		}
		code := string(file.Content)
		for _, want := range []string{"DECLARE $label AS Utf8;", "UPDATE", "SELECT id, label FROM", "DELETE FROM", "dsl.render(RECORDS)", "dsl.render(CHILDREN)", "_prepared.getMoreResults()", "Expected one result set"} {
			require.Contains(t, code, want, "missing %q in generated script:\n%s", want, code)
		}
		require.False(t, strings.Count(code, ".prepareStatement(") != 2 || strings.Count(code, "_prepared.execute();") != 2 || strings.Contains(code, ".executeQuery("), "expected one JDBC execution per mixed script:\n%s", code)
	}
}

func TestJooqStructuredDMLScriptsRetainBatchRestrictions(t *testing.T) {
	const insert = "UPSERT INTO records SELECT id, label FROM AS_TABLE($rows);\n"
	for _, tc := range []struct{ name, tail, wantError string }{
		{"delete after batch", "DELETE FROM children WHERE owner = $id;", ""},
		{"two batches", insert, "Change: jOOQ structured parameters require INSERT/UPSERT SELECT FROM AS_TABLE"},
		{"physical source", "DELETE FROM children WHERE owner IN (SELECT id FROM records);", "Change: jOOQ batch insert supports AS_TABLE sources only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sql := "-- name: Change :exec\nDECLARE $rows AS List<Struct<id:Uint64,label:Utf8>>;\n" + insert + tc.tail
			analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqDMLScriptSchema}}, []model.Source{{Name: "queries.sql", Text: sql}})
			require.NoError(t, err)
			files, err := Generate(analysis, Options{Package: "scripts", Runtime: "jooq"})
			if tc.wantError != "" {
				require.False(t, files != nil || err == nil || err.Error() != tc.wantError, "files=%v, error=%v; want %q", files, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			for _, file := range files {
				if file.Name != "Queries.java" {
					continue
				}
				code := string(file.Content)
				for _, want := range []string{"DECLARE $id AS Uint64;", "FROM AS_TABLE($rows)", "DELETE FROM", "WHERE owner = $id;", "dsl.render(RECORDS)", "dsl.render(CHILDREN)"} {
					require.Contains(t, code, want, "missing %q:\n%s", want, code)
				}
				require.False(t, strings.Count(code, ".prepareStatement(") != 1 || strings.Count(code, "_prepared.execute();") != 1, "expected one execution for the complete batch script:\n%s", code)
			}
		})
	}
}

func runJooqDMLScriptSDK(t *testing.T, files []model.File) {
	t.Helper()
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN to compile and execute generated DML scripts against the published SDK")
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
	program := `package scripts;
import java.lang.reflect.Proxy;
import java.sql.Connection;
import java.util.*;
import org.jooq.conf.*;
import org.jooq.tools.jdbc.*;
import org.jooq.types.ULong;
import tech.ydb.jdbc.*;
import tech.ydb.jooq.YDB;
import tech.ydb.table.values.PrimitiveValue;
public class Main {
    public static void main(String[] args) throws Exception {
        var statements = new ArrayList<String>();
        var bindings = new ArrayList<Map<String,Object>>();
        try (var mock = new MockConnection(ctx -> {
            statements.add(ctx.sql());
            return new MockResult[]{new MockResult(0)};
        })) {
            var named = (YdbConnection) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[]{YdbConnection.class}, (proxy, method, values) -> {
                if (!method.getName().equals("prepareStatement") || values[1] != YdbPrepareMode.DATA_QUERY) throw new AssertionError(method);
                var statement = mock.prepareStatement((String) values[0]);
                var params = new HashMap<String,Object>();
                bindings.add(params);
                return Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[]{YdbPreparedStatement.class}, (prepared, operation, parameters) -> {
                    if (operation.getName().startsWith("set") && parameters[0] instanceof String) {
                        Object value = parameters[1];
                        if (parameters[0].equals("id")) {
                            if (!(value instanceof PrimitiveValue) || ((PrimitiveValue) value).getUint64() != -1L) throw new AssertionError(value);
                            value = ULong.MAX;
                        }
                        if (params.put((String) parameters[0], value) != null) throw new AssertionError("duplicate binding");
                        return null;
                    }
                    return java.sql.PreparedStatement.class.getMethod(operation.getName(), operation.getParameterTypes()).invoke(statement, parameters);
                });
            });
            var connection = (Connection) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[]{Connection.class}, (proxy, method, values) -> {
                if (method.getName().equals("unwrap") && values[0] == YdbConnection.class) return named;
                return method.invoke(mock, values);
            });
            var settings = new Settings().withRenderMapping(new RenderMapping().withSchemata(new MappedSchema().withInput("").withTables(
                new MappedTable().withInput("/local/scripts/records").withOutput("/local/mapped/records"),
                new MappedTable().withInput("/local/scripts/children").withOutput("/local/mapped/children"))));
            var queries = new Queries(YDB.using(connection, settings));
            queries.deleteBoth(ULong.MAX);
            queries.renameAndDelete(ULong.MAX, "changed");
            if (statements.size() != 2 || bindings.size() != 2) throw new AssertionError(statements);
            String first = "PRAGMA TablePathPrefix('/local/scripts');\nDECLARE $id AS Uint64;\n-- Keep both operations in the same request.\nDELETE FROM ` + "`/local/mapped/children` WHERE `/local/mapped/children`" + `.owner = $id;\nDELETE FROM ` + "`/local/mapped/records` WHERE `/local/mapped/records`" + `.id = $id;";
            String second = "DECLARE $label AS Utf8;\nPRAGMA TablePathPrefix('/local/scripts');\nDECLARE $id AS Uint64;\nUPDATE ` + "`/local/mapped/records`" + ` SET label = $label WHERE ` + "`/local/mapped/records`" + `.id = $id;\n-- A semicolon inside this comment must not split the request: ;\nDELETE FROM ` + "`/local/mapped/children` WHERE `/local/mapped/children`" + `.owner = $id;";
            if (!statements.equals(List.of(first, second))) throw new AssertionError(statements);
            if (!bindings.equals(List.of(Map.of("id", ULong.MAX), Map.of("id", ULong.MAX, "label", "changed")))) throw new AssertionError(bindings);
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
	for _, args := range [][]string{append([]string{"javac"}, compile...), {"java", "-cp", dir + string(os.PathListSeparator) + strings.TrimSpace(string(cp)), "scripts.Main"}} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			require.NoError(t, err, "%s: %v\n%s", args[0], err, out)
		}
	}
}
