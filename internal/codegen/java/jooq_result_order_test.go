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

func TestJooqImplicitColumnCollisionResultOrder(t *testing.T) {
	analysis, err := analyzer.Analyze(nil, []model.Source{{Name: "query.sql", Text: `-- name: ReadOne :one
SELECT "z"u AS z, 2 AS column2, 3, 4;
-- name: ReadMany :many
SELECT "z"u AS z, 2 AS column2, 3, 4;`}})
	require.NoError(t, err)
	files, err := Generate(analysis, Options{Package: "resultorder", Runtime: "jooq"})
	require.NoError(t, err)
	for _, file := range files {
		require.False(t, strings.HasSuffix(file.Name, "Row.java") && !strings.Contains(string(file.Content), "Integer column2, Integer column3, Integer column4, String z"), "unexpected resolved row: %s", file.Content)
	}
	t.Run("published dialect", func(t *testing.T) {
		maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
		if maven == "" {
			t.Skip("set SQLC_YDB_TEST_MAVEN to compile and execute collision-ordered rows against the published dialect")
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
		program := `package resultorder;
import java.util.*;
import org.jooq.tools.jdbc.*;
import tech.ydb.jooq.YDB;
import tech.ydb.jooq.YdbTypes;
import static org.jooq.impl.DSL.*;
public class Main {
    public static void main(String[] args) throws Exception {
        var statements = new ArrayList<String>();
        try (var connection = new MockConnection(ctx -> {
            String sql = ctx.sql().replace("` + "`" + `", "").replaceAll("\\s+", " ").trim().toLowerCase(Locale.ROOT);
            statements.add(sql);
            if (!sql.equals("select 'z' z, 2 column2, 3, 4")) throw new AssertionError(sql);
            if (ctx.bindings().length != 0) throw new AssertionError(Arrays.toString(ctx.bindings()));
            var dsl = YDB.using();
            var column2 = field(name("column2"), YdbTypes.INT32);
            var column3 = field(name("column3"), YdbTypes.INT32);
            var column4 = field(name("column4"), YdbTypes.INT32);
            var z = field(name("z"), YdbTypes.UTF8);
            // YDB returns collision-renamed expressions in lexical result-name order.
            var result = dsl.newResult(column2, column3, column4, z);
            result.add(dsl.newRecord(column2, column3, column4, z).values(2, 3, 4, "z"));
            return new MockResult[]{new MockResult(1, result)};
        })) {
            var queries = new Queries(YDB.using(connection));
            var one = queries.readOne().orElseThrow();
            if (!one.equals(new ReadOneRow(2, 3, 4, "z"))) throw new AssertionError(one);
            var many = queries.readMany();
            if (!many.equals(List.of(new ReadManyRow(2, 3, 4, "z")))) throw new AssertionError(many);
            if (statements.size() != 2) throw new AssertionError(statements);
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
		for _, args := range [][]string{append([]string{"javac"}, compile...), {"java", "-cp", dir + string(os.PathListSeparator) + strings.TrimSpace(string(cp)), "resultorder.Main"}} {
			if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
				require.NoError(t, err, "%s: %v\n%s", args[0], err, out)
			}
		}
	})
}
