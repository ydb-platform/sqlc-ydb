package authors.spring;

import java.nio.file.Files;
import java.nio.file.Path;
import java.sql.Connection;
import java.sql.DriverManager;
import java.util.List;

import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.datasource.SingleConnectionDataSource;

/** Run from examples/authors; the smoke creates and drops its authors table. */
public final class Smoke {
    private static final long MAX_UINT64 = -1L;
    private static final long SECOND_ID = 7L;

    private Smoke() { }

    public static void main(String[] args) throws Exception {
        String endpoint = System.getenv("YDB_CONNECTION_STRING");
        if (endpoint == null || endpoint.isBlank()) throw new IllegalStateException("YDB_CONNECTION_STRING is required");
        String schema = readSchema();
        try (Connection connection = DriverManager.getConnection("jdbc:ydb:" + endpoint)) {
            try (var statement = connection.createStatement()) { statement.execute(schema); }
            try {
                JdbcTemplate template = new JdbcTemplate(new SingleConnectionDataSource(connection, true));
                Queries queries = new Queries(template);
                exercise(queries);
                check(!connection.isClosed(), "Spring borrowed connection remains open");
            } finally {
                try (var statement = connection.createStatement()) { statement.execute("DROP TABLE authors;"); }
            }
        }
    }

    private static void exercise(Queries queries) {
        queries.upsertAuthor(MAX_UINT64, "Unsigned", null);
        GetAuthorRow emptyBio = queries.getAuthor(MAX_UINT64).orElseThrow();
        check(emptyBio.id() == MAX_UINT64 && "Unsigned".equals(emptyBio.name()) && emptyBio.bio() == null,
                "nullable Spring row");
        check("Unsigned".equals(queries.getAuthorName(MAX_UINT64).orElseThrow().name()), "Spring name");

        queries.upsertAuthor(MAX_UINT64, "Unsigned", "Biography");
        check("Biography".equals(queries.getAuthor(MAX_UINT64).orElseThrow().bio()), "non-null Spring bio");
        queries.upsertAuthor(SECOND_ID, "Second", null);
        List<ListAuthorsRow> rows = queries.listAuthors();
        check(rows.stream().anyMatch(row -> row.id() == MAX_UINT64), "Spring list result");

        queries.deleteAuthor(SECOND_ID);
        check(queries.getAuthor(SECOND_ID).isEmpty(), "Spring delete result");
    }

    private static String readSchema() throws Exception {
        Path schema = Path.of("schema.sql");
        if (!Files.isRegularFile(schema) || Files.readString(schema).isBlank()) {
            throw new IllegalStateException("run this smoke from examples/authors with schema.sql present");
        }
        return Files.readString(schema);
    }

    private static void check(boolean condition, String message) {
        if (!condition) throw new AssertionError(message);
    }
}
