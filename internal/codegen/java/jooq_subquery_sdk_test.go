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

func TestJooqSubqueriesPublishedSDK(t *testing.T) {
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN to compile and execute IN subqueries against the published dialect")
	}
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: jooqSubquerySchema}}, []model.Source{{Name: "queries.sql", Text: jooqSubqueryQueries + "\n-- name: ReadNegatedDistinct :many\nSELECT id FROM records WHERE NOT (id IN (SELECT DISTINCT id FROM selected));"}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(analysis, Options{Package: "subqueries", Runtime: "jooq"})
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
	program := `package subqueries;
import java.util.*;
import org.jooq.conf.*;
import org.jooq.tools.jdbc.*;
import org.jooq.types.ULong;
import tech.ydb.jooq.YDB;
import tech.ydb.jooq.YdbTypes;
import static org.jooq.impl.DSL.*;
public class Main {
    public static void main(String[] args) throws Exception {
        var statements = new ArrayList<String>();
        try (var connection = new MockConnection(ctx -> {
            String sql = ctx.sql().replace("` + "`" + `", "").replaceAll("\\s+", " ").trim().toLowerCase(Locale.ROOT);
            int index = statements.size();
            statements.add(sql);
            String inner = index == 5 ? "mapped_small_keys" : "mapped_selected";
            if (!sql.contains("mapped_records") || !sql.contains(inner)) throw new AssertionError(sql);
            if (index == 0 && !sql.contains("r.id in (select r.id from mapped_selected r where r.label = ?")) throw new AssertionError(sql);
            if (index == 1 && !sql.contains("not in (select")) throw new AssertionError(sql);
            int orderCount = sql.split("order by", -1).length - 1;
            int wantOrders = (index == 6 || index == 7) ? 2 : index <= 2 ? 1 : 0;
            if (orderCount != wantOrders) throw new AssertionError(sql);
            if (index == 6 && (!sql.contains("order by mapped_selected.label desc") || !sql.endsWith("order by mapped_records.id"))) throw new AssertionError(sql);
            if (index == 7 && !sql.contains("order by n desc limit 1")) throw new AssertionError(sql);
            if (index == 8 && (!sql.contains("not (") || !sql.contains("select distinct"))) throw new AssertionError(sql);
            Object[] want = switch(index) {
                case 0, 2, 4 -> new Object[]{"chosen"};
                case 3 -> new Object[]{"changed", "chosen"};
                case 5 -> new Object[]{Integer.valueOf(2)};
                default -> new Object[0];
            };
            if (!Arrays.equals(ctx.bindings(), want)) throw new AssertionError(Arrays.toString(ctx.bindings()) + " for " + sql);
            if (index == 3) return new MockResult[]{new MockResult(1)};
            var dsl = YDB.using();
            var id = field(name("id"), YdbTypes.UINT64);
            if (index == 0 || index == 4) {
                var label = field(name("label"), YdbTypes.UTF8);
                var rows = dsl.newResult(id, label);
                rows.add(dsl.newRecord(id, label).values(ULong.MAX, "chosen"));
                return new MockResult[]{new MockResult(1, rows)};
            }
            var rows = dsl.newResult(id);
            rows.add(dsl.newRecord(id).values(ULong.MAX));
            return new MockResult[]{new MockResult(1, rows)};
        })) {
            var settings = new Settings().withRenderMapping(new RenderMapping().withSchemata(new MappedSchema().withInput("").withTables(
                    new MappedTable().withInput("records").withOutput("mapped_records"),
                    new MappedTable().withInput("selected").withOutput("mapped_selected"),
                    new MappedTable().withInput("small_keys").withOutput("mapped_small_keys"))));
            var queries = new Queries(YDB.using(connection, settings));
            if (!queries.readSelected("chosen").equals(List.of(new ReadSelectedRow(ULong.MAX, "chosen")))) throw new AssertionError("selected");
            if (!queries.readExcluded().equals(List.of(new ReadExcludedRow(ULong.MAX)))) throw new AssertionError("excluded");
            if (!queries.readNested("chosen").equals(List.of(new ReadNestedRow(ULong.MAX)))) throw new AssertionError("nested");
            queries.updateSelected("changed", "chosen");
            if (!queries.deleteSelected("chosen").equals(List.of(new DeleteSelectedRow(ULong.MAX, "chosen")))) throw new AssertionError("delete");
            if (!queries.readMixedTypes(2).equals(List.of(new ReadMixedTypesRow(ULong.MAX)))) throw new AssertionError("mixed numeric types");
            if (!queries.readInnerOrder().equals(List.of(new ReadInnerOrderRow(ULong.MAX)))) throw new AssertionError("inner order");
            if (!queries.readAggregate().equals(List.of(new ReadAggregateRow(ULong.MAX)))) throw new AssertionError("aggregate order");
            if (!queries.readNegatedDistinct().equals(List.of(new ReadNegatedDistinctRow(ULong.MAX)))) throw new AssertionError("negated distinct membership");
            if (statements.size() != 9) throw new AssertionError(statements);
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
	for _, args := range [][]string{append([]string{"javac"}, compile...), {"java", "-cp", dir + string(os.PathListSeparator) + strings.TrimSpace(string(cp)), "subqueries.Main"}} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", args[0], err, out)
		}
	}
}
