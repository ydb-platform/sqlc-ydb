package java

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestJDBCMultiGeneration(t *testing.T) {
	const sql = "-- name: FetchSummary :multi\nDECLARE $id AS Uint64;\n-- result: Item\nSELECT $id AS id FROM (SELECT 1 AS x) AS source WHERE false;\n-- result: Flags\nSELECT true AS enabled LIMIT 1;\nSELECT \"ready\"u AS status;"
	analysis, err := analyzer.Analyze(nil, []model.Source{{Name: "queries.sql", Text: sql}})
	require.NoError(t, err)
	files, err := Generate(analysis, Options{Package: "multires.jdbc", Runtime: "jdbc"})
	require.NoError(t, err)
	require.Len(t, files, 5)
	result := string(files[3].Content)
	require.Contains(t, result, "record FetchSummaryResult(")
	require.Contains(t, result, "List<FetchSummaryItemRow> item")
	require.Contains(t, result, "List<FetchSummaryFlagsRow> flags")
	require.Contains(t, result, "List<FetchSummaryResult3Row> result3")
	query := string(files[4].Content)
	require.Contains(t, query, "result set 1")
	require.Contains(t, query, "getColumnName(1)")
	require.Contains(t, query, "getColumnTypeName(1)")
	require.Contains(t, query, "getMoreResults()")
	require.Contains(t, query, "unexpected extra result set")
}

func TestJDBCMultiNamesAndShapes(t *testing.T) {
	query := model.AnalyzedQuery{
		Name: "read_summary", Command: model.Multi, SQL: "SELECT 1 AS id; SELECT true AS enabled;",
		ResultSets: []model.ResultSet{
			{Name: "First", Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Int32"}}}},
			{Name: "Result2", Columns: []model.Column{{Name: "enabled", Type: model.Type{Kind: "Bool"}}}},
		},
	}
	files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{query}}, Options{Package: "multi", Runtime: "jdbc"})
	require.NoError(t, err)
	require.Equal(t, "ReadSummaryFirstRow.java", files[0].Name)
	require.Equal(t, "ReadSummaryResult2Row.java", files[1].Name)
	require.Equal(t, "ReadSummaryResult.java", files[2].Name)
	require.Contains(t, string(files[3].Content), "return new ReadSummaryResult(_set1, _set2)")

	query.ResultSets = query.ResultSets[:1]
	_, err = Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{query}}, Options{Runtime: "jdbc"})
	require.ErrorContains(t, err, ":multi requires at least two result sets")
	query.ResultSets = append(query.ResultSets, model.ResultSet{Name: "Result2"})
	_, err = Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{query}}, Options{Runtime: "jdbc"})
	require.ErrorContains(t, err, "requires at least one column")
	query.ResultSets[1] = model.ResultSet{Name: "First_", Columns: query.ResultSets[0].Columns}
	_, err = Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{query}}, Options{Runtime: "jdbc"})
	require.ErrorContains(t, err, "result field name collision")
}

func TestJDBCMultiRuntime(t *testing.T) {
	const sql = "-- name: Read :multi\n-- result: Empty\nSELECT 1 AS id FROM (SELECT 1 AS x) AS source WHERE false;\n-- result: Flag\nSELECT true AS enabled LIMIT 1;\nSELECT CAST(NULL AS Optional<Utf8>) AS status;"
	analysis, err := analyzer.Analyze(nil, []model.Source{{Name: "queries.sql", Text: sql}})
	require.NoError(t, err)
	files, err := Generate(analysis, Options{Package: "multires", Runtime: "jdbc"})
	require.NoError(t, err)
	dir := t.TempDir()
	args := []string{"--release", "17", "-d", filepath.Join(dir, "classes")}
	for _, file := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file.Name), file.Content, 0600))
		args = append(args, file.Name)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Main.java"), []byte(jdbcMultiRuntime), 0600))
	args = append(args, "Main.java")
	compile := exec.Command("javac", args...)
	compile.Dir = dir
	out, err := compile.CombinedOutput()
	require.NoError(t, err, "generated Java runtime did not compile:\n%s", out)
	run := exec.Command("java", "-cp", filepath.Join(dir, "classes"), "multires.Main")
	run.Dir = dir
	out, err = run.CombinedOutput()
	require.NoError(t, err, "generated Java runtime failed:\n%s", out)
}

const jdbcMultiRuntime = `package multires;
import java.lang.reflect.Proxy;
import java.sql.*;

public final class Main {
    private record Set(String name, String type, int nullable, Object... values) { }
    private static Set empty() { return new Set("id", "Int32", 0); }
    private static Set flag() { return new Set("enabled", "Bool", 0, true); }
    private static Set optional() { return new Set("status", "Text", 1, (Object) null); }

    private static ResultSet rows(Set set) {
        var metadata = (ResultSetMetaData) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[]{ResultSetMetaData.class}, (proxy, method, args) -> switch (method.getName()) {
            case "getColumnCount" -> 1;
            case "getColumnName" -> set.name();
            case "getColumnTypeName" -> set.type();
            case "isNullable" -> set.nullable();
            default -> throw new AssertionError(method);
        });
        int[] cursor = {-1};
        return (ResultSet) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[]{ResultSet.class}, (proxy, method, args) -> switch (method.getName()) {
            case "getMetaData" -> metadata;
            case "next" -> ++cursor[0] < set.values().length;
            case "getInt", "getBoolean", "getString" -> set.values()[cursor[0]];
            case "close" -> null;
            default -> throw new AssertionError(method);
        });
    }

    private static Connection connection(Set[] sets, int failAtAdvance) {
        ResultSet[] results = new ResultSet[sets.length];
        for (int i = 0; i < sets.length; i++) results[i] = rows(sets[i]);
        int[] position = {0};
        int[] executions = {0};
        var statement = (PreparedStatement) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[]{PreparedStatement.class}, (proxy, method, args) -> switch (method.getName()) {
            case "execute" -> { if (++executions[0] != 1) throw new AssertionError("script replayed"); yield true; }
            case "getResultSet" -> position[0] < results.length ? results[position[0]] : null;
            case "getUpdateCount" -> -1;
            case "getMoreResults" -> {
                if (position[0] == failAtAdvance) throw new SQLException("late result failure");
                position[0]++;
                yield position[0] < results.length;
            }
            case "close" -> null;
            default -> throw new AssertionError(method);
        });
        return (Connection) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[]{Connection.class}, (proxy, method, args) -> {
            if (method.getName().equals("prepareStatement")) return statement;
            throw new AssertionError(method);
        });
    }

    private static void fails(Set[] sets, int failAtAdvance, String expected) throws Exception {
        try {
            new Queries(connection(sets, failAtAdvance)).read();
            throw new AssertionError("expected " + expected);
        } catch (SQLException error) {
            if (!error.getMessage().contains(expected)) throw error;
        }
    }

    public static void main(String[] args) throws Exception {
        var result = new Queries(connection(new Set[]{empty(), flag(), optional()}, -1)).read();
        if (!result.empty().isEmpty() || result.flag().size() != 1 || !result.flag().get(0).enabled() || result.result3().size() != 1 || result.result3().get(0).status() != null)
            throw new AssertionError("wrong rows, empty list, or result order");
        fails(new Set[]{new Set("wrong", "Int32", 0), flag(), optional()}, -1, "schema mismatch");
        fails(new Set[]{empty(), new Set("enabled", "Int32", 0, true), optional()}, -1, "schema mismatch");
        fails(new Set[]{empty(), flag(), new Set("status", "Text", 0, (Object) null)}, -1, "schema mismatch");
        fails(new Set[]{empty(), flag()}, -1, "missing result set 3");
        fails(new Set[]{empty(), flag(), optional(), empty()}, -1, "unexpected extra result set");
        fails(new Set[]{empty(), flag(), optional()}, 2, "late result failure");
    }
}
`
