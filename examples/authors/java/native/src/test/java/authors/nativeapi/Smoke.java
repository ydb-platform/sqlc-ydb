package authors.nativeapi;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

import tech.ydb.core.grpc.GrpcTransport;
import tech.ydb.query.QueryClient;
import tech.ydb.query.tools.SessionRetryContext;

/** Run from examples/authors; the smoke creates and drops its authors table. */
public final class Smoke {
    private static final long MAX_UINT64 = -1L;
    private static final long SECOND_ID = 7L;

    private Smoke() { }

    public static void main(String[] args) throws Exception {
        String url = System.getenv("SQLC_YDB_TEST_DSN");
        if (url == null || url.isBlank()) {
            throw new IllegalStateException("SQLC_YDB_TEST_DSN is required");
        }
        String schema = readSchema();
        try (GrpcTransport transport = GrpcTransport.forConnectionString(url).build();
             QueryClient client = QueryClient.newClient(transport).build()) {
            SessionRetryContext retry = SessionRetryContext.create(client).build();
            createSchema(retry, schema);
            Queries queries = new Queries(retry);
            try {
                exercise(queries);
            } finally {
                queries.deleteAuthor(MAX_UINT64);
                queries.deleteAuthor(SECOND_ID);
                dropSchema(retry);
            }
        }
    }

    private static void exercise(Queries queries) {
        queries.upsertAuthor(MAX_UINT64, "Unsigned", null);
        GetAuthorRow emptyBio = queries.getAuthor(MAX_UINT64).orElseThrow();
        check(emptyBio.id() == MAX_UINT64 && "Unsigned".equals(emptyBio.name()) && emptyBio.bio() == null,
                "nullable native row");
        check("Unsigned".equals(queries.getAuthorName(MAX_UINT64).orElseThrow().name()), "native name");

        queries.upsertAuthor(MAX_UINT64, "Unsigned", "Biography");
        check("Biography".equals(queries.getAuthor(MAX_UINT64).orElseThrow().bio()), "non-null native bio");
        queries.upsertAuthor(SECOND_ID, "Second", null);
        List<ListAuthorsRow> rows = queries.listAuthors();
        check(rows.stream().anyMatch(row -> row.id() == MAX_UINT64), "native list result");

        queries.deleteAuthor(SECOND_ID);
        check(queries.getAuthor(SECOND_ID).isEmpty(), "native delete result");
    }

    private static String readSchema() throws Exception {
        Path schema = Path.of("schema.sql");
        if (!Files.isRegularFile(schema) || Files.readString(schema).isBlank()) {
            throw new IllegalStateException("run this smoke from examples/authors with schema.sql present");
        }
        return Files.readString(schema);
    }

    private static void createSchema(SessionRetryContext retry, String schema) {
        var result = retry.supplyResult(session -> tech.ydb.query.tools.QueryReader.readFrom(session.createQuery(schema, tech.ydb.common.transaction.TxMode.NONE))).join();
        if (!result.isSuccess()) throw new IllegalStateException("CREATE TABLE authors failed: " + result);
    }

    private static void dropSchema(SessionRetryContext retry) {
        var result = retry.supplyResult(session -> tech.ydb.query.tools.QueryReader.readFrom(session.createQuery("DROP TABLE authors;", tech.ydb.common.transaction.TxMode.NONE))).join();
        if (!result.isSuccess()) throw new IllegalStateException("DROP TABLE authors failed: " + result);
    }

    private static void check(boolean condition, String message) {
        if (!condition) throw new AssertionError(message);
    }
}
