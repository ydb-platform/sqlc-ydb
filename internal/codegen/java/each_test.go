package java

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestJDBCEachConsumesRowsAndClosesOnEveryExit(t *testing.T) {
	files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "Visit", Command: model.Each, SQL: "-- name: Visit :each\nSELECT id FROM devices;",
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Int32"}}}}},
	}}}, Options{Package: "streaming", Runtime: "jdbc"})
	require.NoError(t, err)
	dir := t.TempDir()
	for _, f := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f.Name), f.Content, 0600))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tech", "ydb", "jdbc"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tech", "ydb", "jdbc", "YdbConnection.java"), []byte(`package tech.ydb.jdbc;
public interface YdbConnection extends java.sql.Connection {
    Context getCtx();
    tech.ydb.jdbc.context.YdbExecutor getExecutor();
    record Context(boolean streaming) {
        public Properties getOperationProperties() { return new Properties(streaming); }
    }
    record Properties(boolean streaming) {
        public boolean getUseStreamResultSets() { return streaming; }
    }
}
`), 0600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tech", "ydb", "jdbc", "context"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tech", "ydb", "jdbc", "context", "YdbExecutor.java"), []byte(`package tech.ydb.jdbc.context;
public interface YdbExecutor {}
`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tech", "ydb", "jdbc", "context", "QueryServiceExecutor.java"), []byte(`package tech.ydb.jdbc.context;
public final class QueryServiceExecutor implements YdbExecutor {}
`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`package streaming;
import java.lang.reflect.Proxy;
import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.SQLException;

public final class Main {
    private int next, reads, preparedCloses, resultCloses, executions;
    private boolean lateError, extraSet, streaming = true, queryService = true;

    private ResultSet rows() {
        return (ResultSet) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[]{ResultSet.class}, (proxy, method, args) -> switch (method.getName()) {
            case "next" -> {
                next++;
                if (lateError && next == 3) throw new SQLException("late row failure");
                yield next <= 3;
            }
            case "getInt" -> { reads++; yield next; }
            case "close" -> { resultCloses++; yield null; }
            default -> throw new AssertionError(method.getName());
        });
    }

    private Queries queries() {
        var rows = rows();
        var statement = (PreparedStatement) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[]{PreparedStatement.class}, (proxy, method, args) -> switch (method.getName()) {
            case "executeQuery" -> { executions++; yield rows; }
            case "getMoreResults" -> extraSet;
            case "getResultSet" -> rows;
            case "getUpdateCount" -> -1;
            case "close" -> { preparedCloses++; yield null; }
            default -> throw new AssertionError(method.getName());
        });
        var connection = (Connection) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[]{tech.ydb.jdbc.YdbConnection.class}, (proxy, method, args) -> switch (method.getName()) {
            case "unwrap" -> proxy;
            case "getCtx" -> new tech.ydb.jdbc.YdbConnection.Context(streaming);
            case "getExecutor" -> queryService ? new tech.ydb.jdbc.context.QueryServiceExecutor() : new tech.ydb.jdbc.context.YdbExecutor() {};
            case "prepareStatement" -> statement;
            default -> throw new AssertionError(method.getName());
        });
        return new Queries(connection);
    }

    private void checkClosed() {
        if (preparedCloses != 1 || resultCloses != 1) throw new AssertionError("leaked JDBC resources");
    }

    public static void main(String[] args) throws Exception {
        var full = new Main();
        var seen = new java.util.ArrayList<Integer>();
        full.queries().visit(row -> { seen.add(row.id()); if (full.reads != seen.size()) throw new AssertionError("rows decoded ahead of callback"); });
        if (!seen.equals(java.util.List.of(1, 2, 3)) || full.next != 4 || full.executions != 1) throw new AssertionError("full iteration");
        full.checkClosed();

        var stopped = new Main();
        var stop = new IllegalStateException("stop");
        try { stopped.queries().visit(row -> { throw stop; }); throw new AssertionError("no callback failure"); }
        catch (IllegalStateException e) { if (e != stop) throw e; }
        if (stopped.next != 1 || stopped.reads != 1) throw new AssertionError("read after early stop");
        stopped.checkClosed();

        var failed = new Main();
        failed.lateError = true;
        try { failed.queries().visit(row -> {}); throw new AssertionError("late error ignored"); }
        catch (SQLException e) { if (!e.getMessage().equals("late row failure")) throw e; }
        failed.checkClosed();

        var extra = new Main();
        extra.extraSet = true;
        try { extra.queries().visit(row -> {}); throw new AssertionError("extra result accepted"); }
        catch (SQLException e) { if (!e.getMessage().equals("Expected one result set")) throw e; }
        extra.checkClosed();

        var nil = new Main();
        try { nil.queries().visit(null); throw new AssertionError("null callback accepted"); }
        catch (NullPointerException e) { if (!e.getMessage().equals("consume")) throw e; }
        if (nil.executions != 0 || nil.preparedCloses != 0) throw new AssertionError("executed with null callback");

        var buffered = new Main();
        buffered.streaming = false;
        try { buffered.queries().visit(row -> {}); throw new AssertionError("buffered driver accepted"); }
        catch (SQLException e) { if (!e.getMessage().contains("useStreamResultSets=true")) throw e; }
        if (buffered.executions != 0 || buffered.preparedCloses != 0) throw new AssertionError("executed in buffered mode");

        var tableService = new Main();
        tableService.queryService = false;
        try { tableService.queries().visit(row -> {}); throw new AssertionError("table service accepted"); }
        catch (SQLException e) { if (!e.getMessage().contains("useQueryService=true")) throw e; }
        if (tableService.executions != 0 || tableService.preparedCloses != 0) throw new AssertionError("executed with table service");
    }
}
`), 0600))
	classes := filepath.Join(dir, "classes")
	compile := exec.Command("javac", "--release", "17", "-d", classes, "Queries.java", "VisitRow.java", "Main.java", "tech/ydb/jdbc/YdbConnection.java", "tech/ydb/jdbc/context/YdbExecutor.java", "tech/ydb/jdbc/context/QueryServiceExecutor.java")
	compile.Dir = dir
	out, err := compile.CombinedOutput()
	require.NoError(t, err, "%s", out)
	run := exec.Command("java", "-cp", classes, "streaming.Main")
	run.Dir = dir
	out, err = run.CombinedOutput()
	require.NoError(t, err, "%s", out)
}
