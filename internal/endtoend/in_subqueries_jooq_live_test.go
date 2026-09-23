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

func inSubqueriesJooq(t *testing.T) {
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN for live generated jOOQ IN subqueries")
	}
	table := fmt.Sprintf("sqlc_jooq_in_t%d", time.Now().UnixNano())
	schema := strings.NewReplacer("records", table, "selected", table+"_selected", "small_keys", table+"_small").Replace(inSubqueriesJooqSchema)
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: inSubqueriesJooqSchema}}, []model.Source{{Name: "queries.sql", Text: inSubqueriesJooqQueries}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := java.Generate(analysis, java.Options{Package: "insubquerieslive", Runtime: "jooq"})
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
	program := strings.NewReplacer("$SCHEMA", strconv.Quote(schema), "$TABLE", strconv.Quote(table)).Replace(inSubqueriesJooqProgram)
	files = append(files, model.File{Name: "Main.java", Content: []byte(program)})
	compile := []string{"-cp", strings.TrimSpace(string(cp)), "-d", dir}
	for _, file := range files {
		filename := filepath.Join(dir, file.Name)
		if err := os.WriteFile(filename, file.Content, 0600); err != nil {
			t.Fatal(err)
		}
		compile = append(compile, filename)
	}
	for _, args := range [][]string{append([]string{"javac"}, compile...), {"java", "-cp", dir + string(os.PathListSeparator) + strings.TrimSpace(string(cp)), "insubquerieslive.Main"}} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("jOOQ IN subqueries %s: %v\n%s", args[0], err, out)
		}
	}
}

const inSubqueriesJooqSchema = `CREATE TABLE records (id Uint64 NOT NULL, group_id Uint64 NOT NULL, label Utf8 NOT NULL, PRIMARY KEY(id));
CREATE TABLE selected (id Uint64 NOT NULL, group_id Uint64 NOT NULL, label Utf8 NOT NULL, PRIMARY KEY(id));
CREATE TABLE small_keys (id Int32 NOT NULL, PRIMARY KEY(id));`

const inSubqueriesJooqQueries = `-- name: ReadSelected :many
SELECT r.id,r.label FROM records AS r WHERE r.id IN (SELECT r.id FROM selected AS r WHERE r.label=$label) ORDER BY r.id;
-- name: ReadExcluded :many
SELECT id FROM records WHERE id NOT IN (SELECT id FROM selected) ORDER BY id;
-- name: ReadNested :many
SELECT id FROM records WHERE id IN (SELECT id FROM selected WHERE id IN (SELECT id FROM records WHERE label=$label)) ORDER BY id;
-- name: UpdateSelected :exec
UPDATE records SET label=$new_label WHERE id IN (SELECT id FROM selected WHERE label=$label);
-- name: DeleteSelected :many
DELETE FROM records WHERE id IN (SELECT id FROM selected WHERE label=$label) RETURNING id,label;
-- name: ReadMixedTypes :many
SELECT id FROM records WHERE id IN (SELECT id FROM small_keys WHERE id >= $minimum) ORDER BY id;
-- name: ReadInnerOrder :many
SELECT id FROM records WHERE label IN (SELECT label AS chosen FROM selected ORDER BY chosen DESC LIMIT 1) ORDER BY id;
-- name: ReadAggregate :many
SELECT id FROM records WHERE id IN (SELECT COUNT(*) AS n FROM selected GROUP BY label ORDER BY n DESC LIMIT 1) ORDER BY id;
-- name: ReadTuple :many
DECLARE $label AS Utf8;
SELECT id FROM records WHERE (group_id,label) IN (SELECT (group_id,label) FROM selected WHERE label=$label) ORDER BY id;`

const inSubqueriesJooqProgram = `package insubquerieslive;
import java.sql.DriverManager;
import java.util.ArrayList;
import java.util.List;
import org.jooq.conf.*;
import org.jooq.types.ULong;
import tech.ydb.jooq.YDB;
public class Main {
    public static void main(String[] args) throws Exception {
        var created = new ArrayList<String>();
        String table = $TABLE;
        try (var connection = DriverManager.getConnection("jdbc:ydb:" + System.getenv("YDB_CONNECTION_STRING"))) {
            try {
                try (var statement = connection.createStatement()) {
                    for (String ddl : $SCHEMA.split(";")) {
                        if (ddl.isBlank()) continue;
                        statement.execute(ddl);
                        created.add(ddl.trim().split("\\s+")[2]);
                    }
                    statement.execute("UPSERT INTO " + table + " (id,group_id,label) VALUES (1ul,10ul,'a'u),(2ul,20ul,'b'u),(3ul,30ul,'c'u),(18446744073709551615ul,18446744073709551615ul,'max'u);");
                    statement.execute("UPSERT INTO " + table + "_selected (id,group_id,label) VALUES (1ul,10ul,'a'u),(2ul,999ul,'b'u),(18446744073709551615ul,18446744073709551615ul,'max'u);");
                    statement.execute("UPSERT INTO " + table + "_small (id) VALUES (1),(2);");
                }
                var settings = new Settings().withRenderMapping(new RenderMapping().withSchemata(new MappedSchema().withInput("").withTables(
                    new MappedTable().withInput("records").withOutput(table),
                    new MappedTable().withInput("selected").withOutput(table + "_selected"),
                    new MappedTable().withInput("small_keys").withOutput(table + "_small"))));
                var queries = new Queries(YDB.using(connection,settings));
                if (!queries.readSelected("a").equals(List.of(new ReadSelectedRow(ULong.valueOf(1),"a")))) throw new AssertionError("shadowed alias");
                if (!queries.readSelected("absent").isEmpty()) throw new AssertionError("empty inner results");
                if (!queries.readExcluded().equals(List.of(new ReadExcludedRow(ULong.valueOf(3))))) throw new AssertionError("NOT IN");
                if (!queries.readNested("b").equals(List.of(new ReadNestedRow(ULong.valueOf(2))))) throw new AssertionError("nested IN");
                if (!queries.readMixedTypes(2).equals(List.of(new ReadMixedTypesRow(ULong.valueOf(2))))) throw new AssertionError("numeric comparison");
                if (!queries.readInnerOrder().equals(List.of(new ReadInnerOrderRow(ULong.MAX)))) throw new AssertionError("inner ORDER BY");
                if (!queries.readAggregate().equals(List.of(new ReadAggregateRow(ULong.valueOf(1))))) throw new AssertionError("aggregate inner ORDER BY");
                if (!queries.readTuple("a").equals(List.of(new ReadTupleRow(ULong.valueOf(1))))) throw new AssertionError("declared tuple");
                if (!queries.readTuple("b").isEmpty()) throw new AssertionError("tuple must compare both components");
                if (!queries.readTuple("max").equals(List.of(new ReadTupleRow(ULong.MAX)))) throw new AssertionError("tuple Uint64 boundary");
                queries.updateSelected("changed","b");
                if (!queries.readSelected("b").equals(List.of(new ReadSelectedRow(ULong.valueOf(2),"changed")))) throw new AssertionError("UPDATE IN");
                if (!queries.deleteSelected("b").equals(List.of(new DeleteSelectedRow(ULong.valueOf(2),"changed")))) throw new AssertionError("DELETE IN RETURNING");
                if (!queries.readSelected("b").isEmpty()) throw new AssertionError("deleted row remains");
            } finally {
                for (String name : created.reversed()) {
                    try (var statement = connection.createStatement()) { statement.execute("DROP TABLE " + name); }
                }
            }
        }
    }
}`
