package endtoend

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/java"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func tablePathPrefixJooq(t *testing.T, schemas []model.Source, a, b string) {
	t.Helper()
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN for live generated jOOQ namespace checks")
	}
	queries := strings.NewReplacer("$PREFIX_A", a, "$PREFIX_B", b).Replace(tablePathPrefixJooqQueries)
	analysis, err := analyzer.Analyze(schemas, []model.Source{{Name: "jooq.sql", Text: queries}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := java.Generate(analysis, java.Options{Package: "prefixlive", Runtime: "jooq"})
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
	program := strings.NewReplacer("$TABLE_A", strconv.Quote(a+"/users"), "$MAPPED_A", strconv.Quote(a+"/mapped_users")).Replace(tablePathPrefixJooqProgram)
	files = append(files, model.File{Name: "Main.java", Content: []byte(program)})
	compile := []string{"-cp", strings.TrimSpace(string(cp)), "-d", dir}
	for _, file := range files {
		filename := filepath.Join(dir, file.Name)
		if err := os.WriteFile(filename, file.Content, 0600); err != nil {
			t.Fatal(err)
		}
		compile = append(compile, filename)
	}
	for _, args := range [][]string{append([]string{"javac"}, compile...), {"java", "-cp", dir + string(os.PathListSeparator) + strings.TrimSpace(string(cp)), "prefixlive.Main"}} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("prefixed jOOQ %s: %v\n%s", args[0], err, out)
		}
	}
}

const tablePathPrefixJooqQueries = `-- name: UpsertInferred :exec
PRAGMA TablePathPrefix("$PREFIX_A");
UPSERT INTO users (id,name) VALUES ($id,$name);

-- name: ReadInferred :many
PRAGMA TablePathPrefix("$PREFIX_A");
SELECT u.* FROM users VIEW by_name AS u WHERE u.name = $name ORDER BY u.id;

-- name: UpsertDeclared :exec
PRAGMA TablePathPrefix("$PREFIX_A");
DECLARE $id AS Uint64;
DECLARE $name AS Utf8;
UPSERT INTO users (id,name) VALUES ($id,$name);

-- name: ReadDeclared :one
PRAGMA TablePathPrefix("$PREFIX_A");
DECLARE $id AS Uint64;
SELECT users.* FROM users WHERE users.id = $id;

-- name: CopyDeclared :exec
PRAGMA TablePathPrefix("$PREFIX_A");
DECLARE $id AS Uint64;
UPSERT INTO users SELECT users.id, users.name, users.note FROM users WHERE users.id = $id;

-- name: RemoveInferred :one
PRAGMA TablePathPrefix("$PREFIX_A");
DELETE FROM users WHERE id = $id RETURNING id;

-- name: ReadAbsoluteB :one
PRAGMA TablePathPrefix("$PREFIX_A");
DECLARE $id AS Uint64;
SELECT u.* FROM ` + "`$PREFIX_B/users`" + ` AS u WHERE u.id = $id;
`

const tablePathPrefixJooqProgram = `package prefixlive;
import java.sql.*;
import org.jooq.conf.*;
import org.jooq.types.ULong;
import tech.ydb.jooq.YDB;
public class Main {
    public static void main(String[] args) throws Exception {
        try (var connection = DriverManager.getConnection("jdbc:ydb:" + System.getenv("YDB_CONNECTION_STRING"))) {
            int mode = 0;
            for (String mapped : new String[]{"mapped_users", $MAPPED_A}) {
                var settings = new Settings().withRenderMapping(new RenderMapping().withSchemata(new MappedSchema().withInput("")
                        .withTables(new MappedTable().withInput($TABLE_A).withOutput(mapped))));
                var queries = new Queries(YDB.using(connection, settings));
                ULong id = mode == 0 ? ULong.MAX : ULong.valueOf(100);
                String label = "mapped inferred " + mode;
                queries.upsertInferred(id, label);
                var inferred = queries.readInferred(label);
                if (inferred.size() != 1 || !inferred.get(0).id().equals(id) || inferred.get(0).note() != null) throw new AssertionError(inferred);
                var declared = queries.readDeclared(id).orElseThrow();
                if (!declared.name().equals(label)) throw new AssertionError(declared);
                ULong declaredId = ULong.valueOf(200 + mode);
                String declaredLabel = "mapped declared " + mode;
                queries.upsertDeclared(declaredId, declaredLabel);
                var declaredViaDSL = queries.readInferred(declaredLabel);
                if (declaredViaDSL.size() != 1 || !declaredViaDSL.get(0).id().equals(declaredId)) throw new AssertionError(declaredViaDSL);
                queries.copyDeclared(declaredId);
                if (!queries.readDeclared(declaredId).orElseThrow().name().equals(declaredLabel)) throw new AssertionError("self-select mapping");
                var deleted = queries.removeInferred(declaredId).orElseThrow();
                if (!deleted.id().equals(declaredId) || !queries.readInferred(declaredLabel).isEmpty()) throw new AssertionError(deleted);
                var absolute = queries.readAbsoluteB(ULong.valueOf(1)).orElseThrow();
                if (!absolute.active() || !absolute.name().equals("B")) throw new AssertionError(absolute);
                mode++;
            }
            try (var statement = connection.createStatement(); var rows = statement.executeQuery("SELECT id,name FROM " + '` + "`" + `' + $TABLE_A + '` + "`" + `' + " ORDER BY id")) {
                if (!rows.next() || rows.getLong(1) != 1 || !rows.getString(2).equals("A") || rows.next()) throw new AssertionError("RenderMapping wrote to the original table");
            }
        }
    }
}
`
