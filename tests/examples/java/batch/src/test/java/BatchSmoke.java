import java.sql.DriverManager;
import java.time.Instant;
import java.util.List;
import tech.ydb.common.transaction.TxMode;
import tech.ydb.core.grpc.GrpcTransport;
import tech.ydb.query.QueryClient;
import tech.ydb.query.tools.SessionRetryContext;

/** Run against a disposable database without a books table. */
public final class BatchSmoke {
    private static final String SCHEMA = "CREATE TABLE books (book_id Uint64 NOT NULL, author_id Uint64 NOT NULL, isbn Utf8 NOT NULL, book_type Utf8 NOT NULL, title Utf8 NOT NULL, year Int32 NOT NULL, available Timestamp NOT NULL, tags Json NOT NULL, PRIMARY KEY(book_id));";
    public static void main(String[] args) throws Exception {
        String endpoint = java.util.Objects.requireNonNull(System.getenv("YDB_CONNECTION_STRING"));
        try (var connection = DriverManager.getConnection("jdbc:ydb:" + endpoint)) {
            connection.createStatement().execute(SCHEMA);
            try {
                var jdbc = new batch.jdbc.Queries(connection);
                jdbc.createBooks(List.of());
                check(jdbc.booksByYear(2026).isEmpty());
                jdbc.createBooks(List.of(new batch.jdbc.CreateBooksBooksItem(-1L, 42L, "isbn", "paper", "First", 2026, Instant.EPOCH, "[1,true]"),
                        new batch.jdbc.CreateBooksBooksItem(7L, 42L, "isbn2", "paper", "Second", 2026, Instant.EPOCH, "[]")));
                check(jdbc.booksByYear(2026).size() == 2);
                check(jdbc.booksByYear(2026).stream().anyMatch(row -> row.bookId() == -1L && row.tags().equals("[1,true]")));
                connection.setAutoCommit(false);
                jdbc.createBooks(List.of(new batch.jdbc.CreateBooksBooksItem(8L, 42L, "rollback", "paper", "Rollback", 2027, Instant.EPOCH, "[]")));
                connection.rollback();
                connection.setAutoCommit(true);
                check(jdbc.booksByYear(2027).isEmpty());
                try (var transport = GrpcTransport.forConnectionString(endpoint).build(); var client = QueryClient.newClient(transport).build()) {
                    var retry = SessionRetryContext.create(client).build();
                    retry.supplyResult(session -> {
                        var tx = session.createNewTransaction(TxMode.SERIALIZABLE_RW);
                        var nativeQueries = new batch.nativeapi.Queries(tx);
                        nativeQueries.createBooks(List.of());
                        nativeQueries.createBooks(List.of(new batch.nativeapi.CreateBooksBooksItem(9L, 42L, "native", "paper", "Native", 2028, Instant.EPOCH, "[false]")));
                        check(nativeQueries.booksByYear(2028).get(0).tags().equals("[false]"));
                        return tx.commit();
                    }).join().getValue();
                    check(jdbc.booksByYear(2028).size() == 1);
                    retry.supplyStatus(session -> {
                        var tx = session.createNewTransaction(TxMode.SERIALIZABLE_RW);
                        new batch.nativeapi.Queries(tx).createBooks(List.of(new batch.nativeapi.CreateBooksBooksItem(10L, 42L, "rollback", "paper", "Rollback", 2029, Instant.EPOCH, "[]")));
                        return tx.rollback();
                    }).join().expectSuccess();
                    check(jdbc.booksByYear(2029).isEmpty());
                }
                System.out.println("Java native/JDBC batch smoke passed");
            } finally {
                if (!connection.getAutoCommit()) { connection.rollback(); connection.setAutoCommit(true); }
                connection.createStatement().execute("DROP TABLE books;");
            }
        }
    }
    private static void check(boolean value) { if (!value) throw new AssertionError("batch insert contract failed"); }
}
