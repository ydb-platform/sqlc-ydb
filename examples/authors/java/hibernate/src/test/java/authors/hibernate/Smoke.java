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
                .addAnnotatedClass(Authors.class)
                .buildSessionFactory()) {
            try (Session session = factory.openSession()) {
                session.beginTransaction();
                session.createNativeMutationQuery(schema).executeUpdate();
                session.getTransaction().commit();
            }

            try (Session session = factory.openSession()) {
                session.beginTransaction();
                exercise(new Queries(session));
                session.getTransaction().commit();
            } finally {
                try (Session session = factory.openSession()) {
                    session.beginTransaction();
                    session.createNativeMutationQuery("DROP TABLE authors;").executeUpdate();
                    session.getTransaction().commit();
                }
            }
        }
    }

    private static void exercise(Queries queries) {
        queries.createAuthor(MAX_UINT64, "Unsigned", null);
        Authors emptyBio = queries.getAuthor(MAX_UINT64).orElseThrow();
        check(emptyBio.id() == MAX_UINT64 && "Unsigned".equals(emptyBio.name()) && emptyBio.bio() == null,
                "nullable Hibernate row");
        check("Unsigned".equals(queries.getAuthorName(MAX_UINT64).orElseThrow()), "Hibernate name");

        emptyBio.setBio("Biography");
        queries.updateAuthor(emptyBio);

        check("Biography".equals(queries.getAuthor(MAX_UINT64).orElseThrow().bio()), "non-null Hibernate bio");
        queries.createAuthor(SECOND_ID, "Second", null);
        List<Authors> rows = queries.listAuthors();

        check(rows.size() == 2, "Hibernate list result");

        queries.deleteAuthorById(SECOND_ID);
        check(queries.getAuthor(SECOND_ID).isEmpty(), "Hibernate delete result");
        queries.deleteAuthor(emptyBio);

        check(queries.listAuthors().isEmpty(), "Hibernate list result");
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
