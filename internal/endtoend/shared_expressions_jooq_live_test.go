package endtoend

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/java"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func sharedExpressionsJooq(t *testing.T) {
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN for live generated jOOQ expression checks")
	}
	table := fmt.Sprintf("sqlc_jooq_expressions_t%d", time.Now().UnixNano())
	collision := table + "_collision"
	schema := "CREATE TABLE " + table + " (id Uint64 NOT NULL, a Utf8 NOT NULL, column0 Utf8, PRIMARY KEY(id));"
	collisionSchema := "CREATE TABLE " + collision + " (za Utf8 NOT NULL, zb Uint64 NOT NULL, PRIMARY KEY(zb));"
	queries := strings.ReplaceAll(sharedExpressionsJooqQueries, "records", table)
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: schema + "\n" + collisionSchema}}, []model.Source{{Name: "queries.sql", Text: queries}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := java.Generate(analysis, java.Options{Package: "expressionlive", Runtime: "jooq"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	classpath := filepath.Join(dir, "classpath")
	cmd := exec.Command(maven, "-q", "dependency:build-classpath", "-Dmdep.outputFile="+classpath)
	cmd.Dir = filepath.Join("..", "..", "tests", "examples", "java", "jooq")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("jOOQ SDK classpath: %v\n%s", err, out)
	}
	cp, err := os.ReadFile(classpath)
	if err != nil {
		t.Fatal(err)
	}
	program := strings.NewReplacer("$TABLE", strconv.Quote(table), "$COLLISION_TABLE", strconv.Quote(collision), "$SCHEMA", strconv.Quote(schema), "$COLLISION_SCHEMA", strconv.Quote(collisionSchema)).Replace(sharedExpressionsJooqProgram)
	files = append(files, model.File{Name: "Main.java", Content: []byte(program)})
	compile := []string{"-cp", strings.TrimSpace(string(cp)), "-d", dir}
	for _, file := range files {
		filename := filepath.Join(dir, file.Name)
		if err := os.WriteFile(filename, file.Content, 0600); err != nil {
			t.Fatal(err)
		}
		compile = append(compile, filename)
	}
	for _, args := range [][]string{append([]string{"javac"}, compile...), {"java", "-cp", dir + string(os.PathListSeparator) + strings.TrimSpace(string(cp)), "expressionlive.Main"}} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("jOOQ expressions %s: %v\n%s", args[0], err, out)
		}
	}
}

const sharedExpressionsJooqQueries = `-- name: Implicit :many
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
SELECT 1, 2 AS column0 FROM records ORDER BY column1;
-- name: WildcardCollision :one
SELECT t.*, 1, 2 AS column1 FROM records_collision AS t;`

const sharedExpressionsJooqProgram = `package expressionlive;
import java.sql.DriverManager;
import java.util.ArrayList;
import java.util.List;
import org.jooq.types.ULong;
import tech.ydb.jooq.YDB;
public class Main {
    public static void main(String[] args) throws Exception {
        var created = new ArrayList<String>();
        try (var connection = DriverManager.getConnection("jdbc:ydb:" + System.getenv("YDB_CONNECTION_STRING"))) {
            try {
                try (var statement = connection.createStatement()) {
                    statement.execute($SCHEMA);
                    created.add($TABLE);
                    statement.execute($COLLISION_SCHEMA);
                    created.add($COLLISION_TABLE);
                    statement.execute("UPSERT INTO " + $TABLE + " (id,a,column0) VALUES (1ul,'x'u,'c'u),(2ul,'x'u,'b'u),(3ul,'y'u,'a'u);");
                    statement.execute("UPSERT INTO " + $COLLISION_TABLE + " (za,zb) VALUES ('after'u,18446744073709551615ul);");
                }
                var queries = new Queries(YDB.using(connection));
                var implicit = queries.implicit();
                if (!implicit.equals(List.of(new ImplicitRow(1,2), new ImplicitRow(1,2), new ImplicitRow(1,2)))) throw new AssertionError(implicit);
                var aggregate = queries.aggregate();
                if (!aggregate.equals(List.of(new AggregateRow("x", ULong.valueOf(2)), new AggregateRow("y", ULong.valueOf(1))))) throw new AssertionError(aggregate);
                var mixed = queries.mixed();
                if (mixed.size() != 3 || mixed.stream().anyMatch(r -> r.column1() != 3) || mixed.stream().filter(r -> r.column0().equals("x")).count() != 2 || mixed.stream().filter(r -> r.column0().equals("y")).count() != 1) throw new AssertionError(mixed);
                var parenthesized = queries.parenthesized();
                if (!parenthesized.equals(List.of(new ParenthesizedRow(1), new ParenthesizedRow(1), new ParenthesizedRow(1)))) throw new AssertionError(parenthesized);
                var shadow = queries.shadow();
                if (!shadow.equals(List.of(new ShadowRow(true), new ShadowRow(true), new ShadowRow(true)))) throw new AssertionError(shadow);
                var qualified = queries.qualified();
                if (!qualified.equals(List.of(new QualifiedRow(1), new QualifiedRow(1), new QualifiedRow(1)))) throw new AssertionError(qualified);
                var physical = queries.physical();
                if (!physical.equals(List.of(new PhysicalRow("x",3), new PhysicalRow("x",3), new PhysicalRow("y",3)))) throw new AssertionError(physical);
                var collision = queries.collision();
                if (!collision.equals(List.of(new CollisionRow(2,1), new CollisionRow(2,1), new CollisionRow(2,1)))) throw new AssertionError(collision);
                var wildcard = queries.wildcardCollision().orElseThrow();
                if (!wildcard.equals(new WildcardCollisionRow("after", ULong.MAX, 1, 2))) throw new AssertionError(wildcard);
            } finally {
                for (String table : created.reversed()) {
                    try (var statement = connection.createStatement()) {
                        statement.execute("DROP TABLE " + table);
                    }
                }
            }
        }
    }
}`
