package authors.hibernate;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

import org.hibernate.Session;
import org.hibernate.SessionFactory;
import org.hibernate.cfg.AvailableSettings;
import org.hibernate.cfg.Configuration;
import tech.ydb.hibernate.dialect.YdbDialect;
import tech.ydb.jdbc.YdbDriver;

/** Run from examples/authors; the smoke creates and drops its authors table. */
public final class Smoke {
    private static final long MAX_UINT64 = -1L;
    private static final long SECOND_ID = 7L;

    private Smoke() { }

    public static void main(String[] args) throws Exception {
        String endpoint = System.getenv("YDB_CONNECTION_STRING");
        if (endpoint == null || endpoint.isBlank()) throw new IllegalStateException("YDB_CONNECTION_STRING is required");
        String schema = readSchema();
        String jdbcUrl = "jdbc:ydb:" + endpoint;
        try (SessionFactory factory = new Configuration()
                .setProperty(AvailableSettings.DRIVER, YdbDriver.class.getName())
                .setProperty(AvailableSettings.DIALECT, YdbDialect.class.getName())
                .setProperty(AvailableSettings.URL, jdbcUrl)
                .buildSessionFactory();
             Session session = factory.openSession()) {
            session.doWork(connection -> { try (var statement = connection.createStatement()) { statement.execute(schema); } });
            try {
                session.beginTransaction();
                try {
                    exercise(new Queries(session));
                    session.getTransaction().commit();
                } catch (RuntimeException | Error e) {
                    if (session.getTransaction().isActive()) {
                        try {
                            session.getTransaction().rollback();
                        } catch (RuntimeException rollbackError) {
                            e.addSuppressed(rollbackError);
                        }
                    }
                    throw e;
                }
            } finally {
                session.doWork(connection -> { try (var statement = connection.createStatement()) { statement.execute("DROP TABLE authors;"); } });
            }
        }
    }

    private static void exercise(Queries queries) {
        queries.upsertAuthor(MAX_UINT64, "Unsigned", null);
        GetAuthorRow emptyBio = queries.getAuthor(MAX_UINT64).orElseThrow();
        check(emptyBio.id() == MAX_UINT64 && "Unsigned".equals(emptyBio.name()) && emptyBio.bio() == null,
                "nullable Hibernate row");
        check("Unsigned".equals(queries.getAuthorName(MAX_UINT64).orElseThrow().name()), "Hibernate name");

        queries.upsertAuthor(MAX_UINT64, "Unsigned", "Biography");
        check("Biography".equals(queries.getAuthor(MAX_UINT64).orElseThrow().bio()), "non-null Hibernate bio");
        queries.upsertAuthor(SECOND_ID, "Second", null);
        List<ListAuthorsRow> rows = queries.listAuthors();
        check(rows.stream().anyMatch(row -> row.id() == MAX_UINT64), "Hibernate list result");

        queries.deleteAuthor(SECOND_ID);
        check(queries.getAuthor(SECOND_ID).isEmpty(), "Hibernate delete result");
        queries.deleteAuthor(MAX_UINT64);
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
