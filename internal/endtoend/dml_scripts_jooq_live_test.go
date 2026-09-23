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

func dmlScriptsJooq(t *testing.T) {
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN for live generated jOOQ DML scripts")
	}
	table := fmt.Sprintf("sqlc_jooq_dml_t%d", time.Now().UnixNano())
	schema := strings.NewReplacer("records", table, "copies", table+"_copies").Replace(dmlScriptsSchema)
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: dmlScriptsSchema}}, []model.Source{{Name: "queries.sql", Text: dmlScriptsQueries + dmlScriptsResultQueries + dmlScriptsJooqQueries}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := java.Generate(analysis, java.Options{Package: "dmlscriptslive", Runtime: "jooq"})
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
	program := strings.NewReplacer("$SCHEMA", strconv.Quote(schema), "$TABLE", strconv.Quote(table)).Replace(dmlScriptsJooqProgram)
	files = append(files, model.File{Name: "Main.java", Content: []byte(program)})
	compile := []string{"-cp", strings.TrimSpace(string(cp)), "-d", dir}
	for _, file := range files {
		filename := filepath.Join(dir, file.Name)
		if err := os.WriteFile(filename, file.Content, 0600); err != nil {
			t.Fatal(err)
		}
		compile = append(compile, filename)
	}
	for _, args := range [][]string{append([]string{"javac"}, compile...), {"java", "-cp", dir + string(os.PathListSeparator) + strings.TrimSpace(string(cp)), "dmlscriptslive.Main"}} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("jOOQ DML scripts %s: %v\n%s", args[0], err, out)
		}
	}
}

const dmlScriptsJooqQueries = `
-- name: PatchAndClear :exec
DECLARE $id AS Uint64;
UPDATE records SET label = $label WHERE id = $id;
DELETE FROM copies WHERE id = $id;

-- name: ReadPatchedValue :one
DECLARE $id AS Uint64;
UPDATE records SET label = $label WHERE id = $id;
SELECT id, value, label FROM records WHERE id = $id;
DELETE FROM copies WHERE id = $id;
`

const dmlScriptsJooqProgram = `package dmlscriptslive;
import java.sql.DriverManager;
import java.util.ArrayList;
import java.util.List;
import org.jooq.conf.*;
import org.jooq.types.ULong;
import tech.ydb.jooq.YDB;
public class Main {
    private static void check(Queries queries, long id, long value, long copiedValue, String label) {
        if (!queries.listRecords().equals(List.of(new ListRecordsRow(ULong.valueOf(id),value,label)))) throw new AssertionError("record state");
        if (!queries.listCopies().equals(List.of(new ListCopiesRow(ULong.valueOf(id),copiedValue,label)))) throw new AssertionError("copy/read-your-writes state");
    }
` + dmlScriptsResultJooqMethods + `
    public static void main(String[] args) throws Exception {
      for (boolean stream : List.of(false,true)) {
        var created = new ArrayList<String>();
        String table = $TABLE + (stream ? "_stream" : "");
        var properties = new java.util.Properties();
        properties.setProperty("useStreamResultSets",Boolean.toString(stream));
        try (var connection = DriverManager.getConnection("jdbc:ydb:" + System.getenv("YDB_CONNECTION_STRING"),properties)) {
            try {
                try (var statement = connection.createStatement()) {
                    for (String ddl : $SCHEMA.replace($TABLE,table).split(";")) {
                        if (ddl.isBlank()) continue;
                        statement.execute(ddl);
                        created.add(ddl.trim().split("\\s+")[2]);
                    }
                }
                var settings = new Settings().withRenderMapping(new RenderMapping().withSchemata(new MappedSchema().withInput("").withTables(
                    new MappedTable().withInput("records").withOutput(table),
                    new MappedTable().withInput("copies").withOutput(table + "_copies"))));
                var queries = new Queries(YDB.using(connection,settings));
                queries.seedCopy(ULong.valueOf(99));
                queries.mutateTogether(ULong.valueOf(1),40L,"shared → Привет",ULong.valueOf(99));
                check(queries,1,41,40,"shared → Привет");
                connection.setAutoCommit(false);
                queries.patchAndClear(ULong.valueOf(1),"temporary");
                if (!queries.listCopies().isEmpty() || !queries.listRecords().get(0).label().equals("temporary")) throw new AssertionError("transaction read-your-writes");
                connection.rollback();
                connection.setAutoCommit(true);
                check(queries,1,41,40,"shared → Привет");
                connection.setAutoCommit(false);
                queries.deleteTogether(ULong.valueOf(1));
                queries.mutateTogether(ULong.valueOf(2),7L,"committed",ULong.valueOf(99));
                check(queries,2,8,7,"committed");
                connection.commit();
                connection.setAutoCommit(true);
                check(queries,2,8,7,"committed");
                connection.setAutoCommit(false);
                boolean failed = false;
                try {
                    queries.failLate(ULong.valueOf(3),ULong.valueOf(2),100L,"must roll back");
                } catch (org.jooq.exception.DataAccessException expected) {
                    failed = true;
                } finally {
                    connection.rollback();
                    connection.setAutoCommit(true);
                }
                if (!failed) throw new AssertionError("second INSERT must fail on existing key");
                check(queries,2,8,7,"committed");
                queries.deleteTogether(ULong.valueOf(500));
                check(queries,2,8,7,"committed");
                queries.deleteTogether(ULong.valueOf(2));
                queries.mutateTogether(ULong.valueOf(4),10L,"recovered",ULong.valueOf(99));
                check(queries,4,11,10,"recovered");
                checkMixed(queries,connection);
            } finally {
                if (!connection.getAutoCommit()) {
                    connection.rollback();
                    connection.setAutoCommit(true);
                }
                for (String name : created.reversed()) {
                    try (var statement = connection.createStatement()) { statement.execute("DROP TABLE " + name); }
                }
            }
        }
      }
    }
}`
