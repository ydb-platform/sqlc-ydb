package java

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

const jooqOrderBySchema = `CREATE TABLE records (id Uint64 NOT NULL, a Utf8 NOT NULL, column0 Utf8, PRIMARY KEY(id));`

func TestJooqOrderByResolvedOutputNames(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		orders    []string
	}{
		{"implicit", `SELECT 1, 2 FROM records ORDER BY column0 DESC, column1;`, []string{`field(name("column0"), YdbTypes.INT32).desc()`, `field(name("column1"), YdbTypes.INT32)`}},
		{"aggregate", `SELECT a, COUNT(*) AS n FROM records GROUP BY a ORDER BY n DESC;`, []string{`field(name("n"), YdbTypes.UINT64).desc()`}},
		{"explicit and implicit", `SELECT a AS column0, 3 FROM records ORDER BY column1;`, []string{`field(name("column1"), YdbTypes.INT32)`}},
		{"parenthesized", `SELECT 1 FROM records ORDER BY ((column0)) DESC;`, []string{`field(name("column0"), YdbTypes.INT32).desc()`}},
		{"shadowing", `SELECT id IS NOT NULL AS a FROM records ORDER BY a DESC;`, []string{`field(name("a"), YdbTypes.BOOL).desc()`}},
		{"qualified source", `SELECT 1 FROM records AS r ORDER BY r.column0;`, []string{`r.COLUMN0`}},
		{"physical alias", `SELECT a AS column0, 3 FROM records ORDER BY column0;`, []string{`RECORDS.A`}},
		{"renamed collision", `SELECT 1, 2 AS column0 FROM records ORDER BY column1;`, []string{`field(name("column1"), YdbTypes.INT32)`}},
		{"quoted output", "SELECT 1 AS `some-name` FROM records ORDER BY `some-name`;", []string{`field(name("some-name"), YdbTypes.INT32)`}},
		{"quoted keyword", "SELECT id IS NOT NULL AS `true` FROM records ORDER BY `true`;", []string{`field(name("true"), YdbTypes.BOOL)`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqOrderBySchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + tc.sql}})
			if err != nil {
				t.Fatal(err)
			}
			files, err := Generate(analysis, Options{Package: "orderby", Runtime: "jooq"})
			if err != nil {
				t.Fatal(err)
			}
			var queries string
			for _, file := range files {
				if file.Name == "Queries.java" {
					queries = string(file.Content)
				}
			}
			if queries == "" {
				t.Fatal("Queries.java was not generated")
			}
			if strings.Count(queries, ".orderBy(") != 1 {
				t.Fatalf("expected one ORDER BY call:\n%s", queries)
			}
			ordering := queries[strings.Index(queries, ".orderBy("):]
			ordering = ordering[:strings.Index(ordering, ".coerce(")]
			for _, order := range tc.orders {
				index := strings.Index(ordering, order)
				if index < 0 {
					t.Errorf("missing typed ORDER BY %s:\n%s", order, queries)
					continue
				}
				ordering = ordering[index+len(order):]
			}
		})
	}
}

func TestJooqOrderByDoesNotInventOutputReferences(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{`SELECT 1 FROM records ORDER BY missing;`, `unknown column "missing"`},
		{`SELECT 1 AS n FROM records WHERE n = 1;`, `unknown column "n"`},
		{`SELECT 1 AS n FROM records ORDER BY n + 1;`, `ORDER BY expressions referencing projection aliases are not yet supported`},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			_, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqOrderBySchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + tc.sql}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqOrderBySchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT id IS NOT NULL AS `true` FROM records ORDER BY true;"}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(analysis, Options{Package: "orderby", Runtime: "jooq"})
	if files != nil || err == nil || err.Error() != `Read: unsupported jOOQ syntax "true"` {
		t.Fatalf("literal was treated as a reference to the quoted output: files=%v, error=%v", files, err)
	}
}

const jooqOrderByQueries = `-- name: Implicit :many
SELECT 1, 2 FROM records ORDER BY column0 DESC, column1;
-- name: Aggregate :many
SELECT a, COUNT(*) AS n FROM records GROUP BY a ORDER BY n DESC;
-- name: Mixed :many
SELECT a AS column0, 3 FROM records ORDER BY column1;
-- name: Parenthesized :many
SELECT 1 FROM records ORDER BY ((column0)) DESC;
-- name: Shadow :many
SELECT id IS NOT NULL AS a FROM records ORDER BY a DESC;
-- name: Qualified :many
SELECT 1 FROM records AS r ORDER BY r.column0;
-- name: Physical :many
SELECT a AS column0, 3 FROM records ORDER BY column0;
-- name: Collision :many
SELECT 1, 2 AS column0 FROM records ORDER BY column1;`

func TestJooqOrderByPublishedSDK(t *testing.T) {
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN to compile and execute ORDER BY references against the published dialect")
	}
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqOrderBySchema}}, []model.Source{{Name: "queries.sql", Text: jooqOrderByQueries}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(analysis, Options{Package: "orderby", Runtime: "jooq"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	classpath := filepath.Join(dir, "classpath")
	cmd := exec.Command(maven, "-q", "dependency:build-classpath", "-Dmdep.outputFile="+classpath)
	cmd.Dir = filepath.Join("..", "..", "..", "tests", "examples", "java", "jooq")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("SDK classpath: %v\n%s", err, out)
	}
	cp, err := os.ReadFile(classpath)
	if err != nil {
		t.Fatal(err)
	}
	program := `package orderby;
import java.util.*;
import org.jooq.tools.jdbc.*;
import org.jooq.types.ULong;
import tech.ydb.jooq.YDB;
import tech.ydb.jooq.YdbTypes;
import static org.jooq.impl.DSL.*;
public class Main {
    public static void main(String[] args) throws Exception {
        var orders = List.of("column0 desc, column1", "n desc", "column1", "column0 desc", "a desc", "r.column0", "records.a", "column1");
        var statements = new ArrayList<String>();
        try (var connection = new MockConnection(ctx -> {
            String sql = ctx.sql().replace("` + "`" + `", "").replaceAll("\\s+", " ").replace(" asc", "").trim().toLowerCase(Locale.ROOT);
            int index = statements.size();
            statements.add(sql);
            if (!sql.endsWith(" order by " + orders.get(index))) throw new AssertionError(sql);
            if (ctx.bindings().length != 0) throw new AssertionError(Arrays.toString(ctx.bindings()));
            var dsl = YDB.using();
            var column0 = field(name("column0"), YdbTypes.INT32);
            var column1 = field(name("column1"), YdbTypes.INT32);
            var textColumn0 = field(name("column0"), YdbTypes.UTF8);
            var a = field(name("a"), YdbTypes.UTF8);
            var n = field(name("n"), YdbTypes.UINT64);
            var booleanA = field(name("a"), YdbTypes.BOOL);
            org.jooq.Result<?> result;
            switch (index) {
                case 0, 7 -> {
                    var rows = dsl.newResult(column0, column1);
                    rows.add(dsl.newRecord(column0, column1).values(index == 0 ? 1 : 2, index == 0 ? 2 : 1));
                    result = rows;
                }
                case 1 -> {
                    var rows = dsl.newResult(a, n);
                    rows.add(dsl.newRecord(a, n).values("x", ULong.valueOf(9)));
                    result = rows;
                }
                case 2, 6 -> {
                    var rows = dsl.newResult(textColumn0, column1);
                    rows.add(dsl.newRecord(textColumn0, column1).values("x", 3));
                    result = rows;
                }
                case 3, 5 -> {
                    var rows = dsl.newResult(column0);
                    rows.add(dsl.newRecord(column0).values(1));
                    result = rows;
                }
                case 4 -> {
                    var rows = dsl.newResult(booleanA);
                    rows.add(dsl.newRecord(booleanA).values(true));
                    result = rows;
                }
                default -> throw new AssertionError(sql);
            }
            return new MockResult[]{new MockResult(1, result)};
        })) {
            var queries = new Queries(YDB.using(connection));
            if (!queries.implicit().equals(List.of(new ImplicitRow(1, 2)))) throw new AssertionError("implicit");
            if (!queries.aggregate().equals(List.of(new AggregateRow("x", ULong.valueOf(9))))) throw new AssertionError("aggregate");
            if (!queries.mixed().equals(List.of(new MixedRow("x", 3)))) throw new AssertionError("mixed");
            if (!queries.parenthesized().equals(List.of(new ParenthesizedRow(1)))) throw new AssertionError("parenthesized");
            if (!queries.shadow().equals(List.of(new ShadowRow(true)))) throw new AssertionError("shadow");
            if (!queries.qualified().equals(List.of(new QualifiedRow(1)))) throw new AssertionError("qualified");
            if (!queries.physical().equals(List.of(new PhysicalRow("x", 3)))) throw new AssertionError("physical");
            if (!queries.collision().equals(List.of(new CollisionRow(2, 1)))) throw new AssertionError("collision");
            if (statements.size() != orders.size()) throw new AssertionError(statements);
        }
    }
}`
	files = append(files, model.File{Name: "Main.java", Content: []byte(program)})
	compile := []string{"-cp", strings.TrimSpace(string(cp)), "-d", dir}
	for _, file := range files {
		path := filepath.Join(dir, file.Name)
		if err := os.WriteFile(path, file.Content, 0600); err != nil {
			t.Fatal(err)
		}
		compile = append(compile, path)
	}
	for _, args := range [][]string{append([]string{"javac"}, compile...), {"java", "-cp", dir + string(os.PathListSeparator) + strings.TrimSpace(string(cp)), "orderby.Main"}} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", args[0], err, out)
		}
	}
}
