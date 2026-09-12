import java.sql.Connection;
import java.sql.DriverManager;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;

import org.jooq.JSON;
import org.jooq.conf.MappedSchema;
import org.jooq.conf.MappedTable;
import org.jooq.conf.RenderMapping;
import org.jooq.conf.Settings;
import org.jooq.types.ULong;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.condition.EnabledIfEnvironmentVariable;
import tech.ydb.jooq.YDB;
import tech.ydb.jooq.YdbDSLContext;

import static org.junit.jupiter.api.Assertions.*;

@EnabledIfEnvironmentVariable(named = "YDB_CONNECTION_STRING", matches = ".+")
class LiveTest {
    @Test
    void authorsAndBooksRoundTrip() throws Exception {
        String prefix = "sqlc_jooq_" + UUID.randomUUID().toString().replace("-", "") + "_";
        List<String> created = new ArrayList<>();
        try (Connection connection = DriverManager.getConnection("jdbc:ydb:" + System.getenv("YDB_CONNECTION_STRING"))) {
            var settings = new Settings().withRenderMapping(new RenderMapping().withSchemata(
                    new MappedSchema().withInput("").withTables(
                            new MappedTable().withInput("authors").withOutput(prefix + "authors"),
                            new MappedTable().withInput("books").withOutput(prefix + "books"))));
            YdbDSLContext dsl = YDB.using(connection, settings);
            // Refuse to execute generated queries unless mapping isolates their tables.
            String rendered = dsl.selectFrom(booktest.jooq.Tables.BOOKS).getSQL();
            assertTrue(rendered.contains(prefix + "books"), rendered);
            try {
                create(connection, created, prefix + "authors", "author_id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY(author_id)");
                create(connection, created, prefix + "books", "book_id Uint64 NOT NULL, author_id Uint64 NOT NULL, isbn Utf8 NOT NULL, book_type Utf8 NOT NULL, title Utf8 NOT NULL, publication_year Int32 NOT NULL, available Timestamp NOT NULL, tags Json NOT NULL, PRIMARY KEY(book_id)");
                var queries = new booktest.jooq.Queries(dsl);
                ULong id = ULong.MAX;
                Instant available = Instant.parse("2026-01-01T00:00:00.123456Z");
                assertEquals(id, queries.createAuthor(id, "Автор").orElseThrow().authorId());
                assertEquals("Автор", queries.getAuthor(id).orElseThrow().name());
                var inserted = queries.createBook(id, id, "isbn", "paper", "Title", 2026, available, JSON.valueOf("[\"tag\"]")).orElseThrow();
                assertEquals(id, inserted.bookId());
                assertEquals(available, inserted.available());
                assertEquals(id, queries.getBook(id).orElseThrow().bookId());
                assertEquals(1, queries.booksByTitleYear("Title", 2026).size());
                assertEquals("Автор", queries.booksByTags(JSON.valueOf("[\"tag\"]")).get(0).name());
                ULong orphan = ULong.valueOf(7);
                queries.createBook(orphan, orphan, "orphan", "paper", "Orphan", 2026, available, JSON.valueOf("[\"tag\"]"));
                assertTrue(queries.booksByTags(JSON.valueOf("[\"tag\"]")).stream()
                        .anyMatch(row -> row.bookId().equals(orphan) && row.name() == null));
                queries.deleteBook(orphan);
                assertEquals("hello world", queries.sayHello("world").orElseThrow().greeting());
                queries.updateBook("Updated", JSON.valueOf("[]"), id);
                assertEquals("Updated", queries.getBook(id).orElseThrow().title());
                assertTrue(queries.booksByTags(JSON.valueOf("[\"tag\"]")).isEmpty());
                queries.updateBookISBN("ISBN update", JSON.valueOf("[]"), "new-isbn", id);
                assertEquals("new-isbn", queries.getBook(id).orElseThrow().isbn());
                queries.deleteAuthorBeforeYear(2027, id);
                assertTrue(queries.getBook(id).isEmpty());
                queries.deleteBook(id);
            } finally {
                for (String table : created.reversed()) {
                    try (var statement = connection.createStatement()) { statement.execute("DROP TABLE `" + table + "`"); }
                }
            }
        }
    }

    @Test
    void nativeAuthorsContractAndCallerTransaction() throws Exception {
        try (Fixture fixture = new Fixture("authors")) {
            fixture.create("authors", "id Uint64 NOT NULL, name Utf8 NOT NULL, bio Utf8, PRIMARY KEY(id)");
            var queries = new authors.jooq.Queries(fixture.dsl);
            assertNull(queries.createAuthor(ULong.MAX, "Автор", null).orElseThrow().bio());
            assertEquals("Автор", queries.getAuthorName(ULong.MAX).orElseThrow().name());
            queries.upsertAuthor(ULong.MAX, "Updated", "bio");
            assertEquals("bio", queries.getAuthor(ULong.MAX).orElseThrow().bio());
            assertEquals(1, queries.listAuthors().size());
            queries.deleteAuthor(ULong.MAX);
            assertTrue(queries.getAuthor(ULong.MAX).isEmpty());
            fixture.connection.setAutoCommit(false);
            queries.upsertAuthor(ULong.valueOf(1), "rollback", null);
            assertEquals("rollback", queries.getAuthor(ULong.valueOf(1)).orElseThrow().name());
            fixture.connection.rollback();
            fixture.connection.setAutoCommit(true);
            assertTrue(queries.getAuthor(ULong.valueOf(1)).isEmpty());
        }
    }

    @Test
    void batchJsonNullsAndAllDeleteVariants() throws Exception {
        try (Fixture fixture = new Fixture("authors", "books")) {
            fixture.create("authors", "author_id Uint64 NOT NULL, name Utf8 NOT NULL, biography Json, PRIMARY KEY(author_id)");
            fixture.create("books", "book_id Uint64 NOT NULL, author_id Uint64 NOT NULL, isbn Utf8 NOT NULL, book_type Utf8 NOT NULL, title Utf8 NOT NULL, year Int32 NOT NULL, available Timestamp NOT NULL, tags Json NOT NULL, PRIMARY KEY(book_id)");
            var queries = new batch.jooq.Queries(fixture.dsl);
            assertNull(queries.createAuthor(ULong.MAX, "Author", null).orElseThrow().biography());
            assertNull(queries.getBiography(ULong.MAX).orElseThrow().biography());
            assertEquals("Author", queries.getAuthor(ULong.MAX).orElseThrow().name());
            for (long i = 1; i <= 4; i++) {
                queries.createBook(ULong.valueOf(i), ULong.MAX, "isbn", "paper", "Title", 2026,
                        Instant.parse("2026-01-01T00:00:00.123456Z"), JSON.valueOf("[]"));
            }
            assertEquals(4, queries.booksByYear(2026).size());
            queries.updateBook("Updated", JSON.valueOf("[1]"), ULong.valueOf(1));
            assertTrue(queries.booksByYear(2026).stream().anyMatch(row -> row.title().equals("Updated")));
            queries.deleteBook(ULong.valueOf(1));
            queries.deleteBookExecResult(ULong.valueOf(2));
            queries.deleteBookNamedFunc(ULong.valueOf(3));
            queries.deleteBookNamedSign(ULong.valueOf(4));
            assertTrue(queries.booksByYear(2026).isEmpty());
        }
    }

    @Test
    void aggregatesLimitAndUpdateReturning() throws Exception {
        try (Fixture fixture = new Fixture("pilots", "city", "venue")) {
            fixture.create("pilots", "id Int32 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY(id)");
            var pilots = new jets.jooq.Queries(fixture.dsl);
            assertEquals(ULong.valueOf(0), pilots.countPilots().orElseThrow().pilotCount());
            for (int i = 0; i < 6; i++) {
                fixture.dsl.insertInto(jets.jooq.Tables.PILOTS)
                        .set(jets.jooq.Tables.PILOTS.ID, i).set(jets.jooq.Tables.PILOTS.NAME, "Pilot").execute();
            }
            assertEquals(5, pilots.listPilots().size());
            pilots.deletePilot(0);
            assertEquals(ULong.valueOf(5), pilots.countPilots().orElseThrow().pilotCount());
            fixture.create("city", "slug Utf8 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY(slug)");
            fixture.create("venue", "id Uint64 NOT NULL, slug Utf8 NOT NULL, name Utf8 NOT NULL, city Utf8 NOT NULL, status Utf8 NOT NULL, statuses Json, spotify_playlist Utf8 NOT NULL, songkick_id Utf8, tags Json, created_at Timestamp, PRIMARY KEY(id)");
            var queries = new ondeck.jooq.Queries(fixture.dsl);
            queries.createCity("City", "city");
            queries.updateCityName("Updated", "city");
            assertEquals("Updated", queries.getCity("city").orElseThrow().name());
            assertEquals(1, queries.listCities().size());
            queries.createVenue(ULong.MAX, "venue", "Venue", "city", null, "playlist", "open", null, null);
            assertNull(queries.getVenue("venue", "city").orElseThrow().createdAt());
            assertEquals(ULong.MAX, queries.updateVenueName("Updated", "venue").orElseThrow().id());
            assertEquals("Updated", queries.listVenues("city").get(0).name());
            assertEquals(ULong.valueOf(1), queries.venueCountByCity().get(0).venueCount());
            queries.deleteVenue("venue");
            assertTrue(queries.listVenues("city").isEmpty());
        }
    }

    private static final class Fixture implements AutoCloseable {
        final String prefix = "sqlc_jooq_" + UUID.randomUUID().toString().replace("-", "") + "_";
        final Connection connection;
        final YdbDSLContext dsl;
        final List<String> created = new ArrayList<>();

        Fixture(String... tables) throws Exception {
            var mappings = new ArrayList<MappedTable>();
            for (String table : tables) mappings.add(new MappedTable().withInput(table).withOutput(prefix + table));
            connection = DriverManager.getConnection("jdbc:ydb:" + System.getenv("YDB_CONNECTION_STRING"));
            dsl = YDB.using(connection, new Settings().withRenderMapping(new RenderMapping().withSchemata(
                    new MappedSchema().withInput("").withTables(mappings))));
            for (String table : tables) {
                String rendered = dsl.selectFrom(org.jooq.impl.DSL.table(org.jooq.impl.DSL.name(table))).getSQL();
                assertTrue(rendered.contains(prefix + table), rendered);
            }
        }

        void create(String table, String columns) throws Exception {
            LiveTest.create(connection, created, prefix + table, columns);
        }

        @Override public void close() throws Exception {
            try {
                for (String table : created.reversed()) {
                    try (var statement = connection.createStatement()) { statement.execute("DROP TABLE `" + table + "`"); }
                }
            } finally {
                connection.close();
            }
        }
    }

    private static void create(Connection connection, List<String> created, String name, String columns) throws Exception {
        try (var statement = connection.createStatement()) {
            statement.execute("CREATE TABLE `" + name + "` (" + columns + ")");
            created.add(name);
        }
    }
}
