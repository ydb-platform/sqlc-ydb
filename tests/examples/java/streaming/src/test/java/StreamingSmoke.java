import java.sql.DriverManager;
import java.sql.SQLException;
import java.util.ArrayList;
import java.util.List;
import java.util.Properties;
import streaming.jdbc.Queries;

/** Run against a disposable database without a streaming_devices table. */
public final class StreamingSmoke {
    private static final String SCHEMA = "CREATE TABLE streaming_devices (id Uint64 NOT NULL, name Utf8, PRIMARY KEY(id));";

    public static void main(String[] args) throws Exception {
        String endpoint = java.util.Objects.requireNonNull(System.getenv("YDB_CONNECTION_STRING"));
        var bufferedProperties = new Properties();
        bufferedProperties.setProperty("useStreamResultSets", "false");
        try (var buffered = DriverManager.getConnection("jdbc:ydb:" + endpoint, bufferedProperties)) {
            try {
                new Queries(buffered).visitAllDevices(row -> { throw new AssertionError("buffered query executed"); });
                throw new AssertionError("buffered JDBC connection was accepted");
            } catch (SQLException expected) {
                if (!expected.getMessage().contains("useStreamResultSets=true")) throw new AssertionError(expected);
            }
        }
        var properties = new Properties();
        properties.setProperty("useStreamResultSets", "true");
        try (var connection = DriverManager.getConnection("jdbc:ydb:" + endpoint, properties)) {
            try (var statement = connection.createStatement()) { statement.execute(SCHEMA); }
            try {
                var queries = new Queries(connection);
                var seen = new ArrayList<Long>();
                queries.visitAllDevices(row -> seen.add(row.id()));
                check(seen.isEmpty());

                queries.upsertDevice(1L, "one");
                queries.upsertDevice(2L, null);
                queries.upsertDevice(3L, "три");
                queries.visitDevices(1L, 3L, row -> {
                    seen.add(row.id());
                    if (row.id() == 2L) check(row.name() == null);
                    if (row.id() == 3L) check("три".equals(row.name()));
                });
                check(seen.equals(List.of(1L, 2L, 3L)));

                var stop = new IllegalStateException("stop after one row");
                int[] calls = {0};
                try {
                    queries.visitAllDevices(row -> { calls[0]++; throw stop; });
                    throw new AssertionError("callback error lost");
                } catch (IllegalStateException expected) {
                    check(expected == stop && calls[0] == 1);
                }
                seen.clear();
                queries.visitAllDevices(row -> seen.add(row.id()));
                check(seen.equals(List.of(1L, 2L, 3L)));

                connection.setAutoCommit(false);
                try {
                    queries.upsertDevice(4L, "uncommitted");
                    seen.clear();
                    queries.visitDevices(4L, 4L, row -> seen.add(row.id()));
                    check(seen.equals(List.of(4L)));
                } finally {
                    connection.rollback();
                    connection.setAutoCommit(true);
                }
                seen.clear();
                queries.visitDevices(4L, 4L, row -> seen.add(row.id()));
                check(seen.isEmpty());
                System.out.println("Java JDBC streaming callback smoke passed");
            } finally {
                if (!connection.getAutoCommit()) { connection.rollback(); connection.setAutoCommit(true); }
                try (var statement = connection.createStatement()) { statement.execute("DROP TABLE streaming_devices;"); }
            }
        }
    }

    private static void check(boolean value) {
        if (!value) throw new AssertionError("streaming callback contract failed");
    }
}
