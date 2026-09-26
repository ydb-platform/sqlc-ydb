import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;

import org.jooq.JSON;
import org.jooq.impl.DSL;
import org.jooq.tools.jdbc.MockConnection;
import org.jooq.tools.jdbc.MockResult;
import org.jooq.types.UInteger;
import org.jooq.types.ULong;
import org.junit.jupiter.api.Test;
import tech.ydb.jooq.YDB;

import static org.junit.jupiter.api.Assertions.*;

class GeneratedQueriesTest {
    @Test
    void everyExampleRendersAndBindsThroughThePublishedDialect() throws Exception {
        List<String> statements = new ArrayList<>();
        try (var connection = new MockConnection(ctx -> {
            statements.add(ctx.sql());
            return new MockResult[] {new MockResult(0, DSL.using(YDB.DIALECT).newResult())};
        })) {
            var dsl = YDB.using(withNamedBinding(connection));
            for (String family : List.of("authors", "batch", "booktest", "jets", "ondeck", "namespaces")) {
                Class<?> type = Class.forName(family + ".jooq.Queries");
                Object queries = type.getConstructor(tech.ydb.jooq.YdbDSLContext.class).newInstance(dsl);
                for (Method method : type.getDeclaredMethods()) {
                    if (!java.lang.reflect.Modifier.isPublic(method.getModifiers()) || method.isSynthetic()) continue;
                    Object[] args = new Object[method.getParameterCount()];
                    for (int i = 0; i < args.length; i++) {
                        Class<?> parameter = method.getParameterTypes()[i];
                        if (parameter == ULong.class) args[i] = ULong.MAX;
                        else if (parameter == UInteger.class) args[i] = UInteger.MAX;
                        else if (parameter == String.class) args[i] = "value";
                        else if (parameter == byte[].class) args[i] = "Book".getBytes(java.nio.charset.StandardCharsets.UTF_8);
                        else if (parameter == Integer.class) args[i] = 2026;
                        else if (parameter == Instant.class) args[i] = Instant.parse("2026-01-01T00:00:00.123456Z");
                        else if (parameter == List.class && method.getName().equals("createAuthors")) args[i] = List.of(new batch.jooq.CreateAuthorsAuthorsItem("Author", -1L));
                        else if (parameter == List.class && method.getName().equals("upsertAuthors")) args[i] = List.of(new batch.jooq.UpsertAuthorsAuthorsItem("Author", -1L));
                        else if (parameter == List.class) args[i] = List.of(new batch.jooq.CreateBooksBooksItem(-1L, 42L, "isbn", "paper", "Batch", 2026, Instant.EPOCH, "[]"));
                        else if (parameter == JSON.class) args[i] = JSON.valueOf("[\"tag\"]");
                        else fail("uncovered parameter type " + parameter);
                    }
                    int before = statements.size();
                    try {
                        method.invoke(queries, args);
                    } catch (InvocationTargetException e) {
                        throw new AssertionError(family + "." + method.getName(), e.getCause());
                    }
                    assertEquals(before + 1, statements.size(), family + "." + method.getName());
                    String sql = statements.get(before);
                    assertFalse(sql.contains("-- name:"), sql);
                    if ((method.getName().startsWith("create") && method.getReturnType() != void.class) || method.getName().equals("updateVenueName")) {
                        assertTrue(sql.toLowerCase().contains("returning"), sql);
                    }
                    if (method.getName().equals("findAuthorsByName") || method.getName().equals("findAuthorsByNameCovering")) {
                        assertTrue(sql.replace("`", "").contains("VIEW by_name"), sql);
                    }
                    if (family.equals("authors") && method.getName().equals("listAuthorsWithoutBio")) {
                        assertFalse(sql.toLowerCase().contains("bio"), sql);
                        assertFalse(sql.toLowerCase().contains("without"), sql);
                    }
                    if (family.equals("booktest") && method.getName().equals("findAuthors")) {
                        assertTrue(sql.contains("author_id` >= ?"), sql);
                        assertTrue(sql.contains("? is null or"), sql);
                        assertTrue(sql.contains("name` = ?"), sql);
                    }
                    if (List.of("listAuthorsWithRecentBooks", "listBooksWithRecentEditions", "deleteBooksByAuthorName").contains(method.getName())) {
                        assertTrue(sql.toLowerCase().contains(" in ("), sql);
                        assertTrue(sql.toLowerCase().contains("select"), sql);
                        if (method.getName().equals("listBooksWithRecentEditions")) {
                            assertTrue(sql.contains("DECLARE $since_year AS Int32;"), sql);
                            assertTrue(sql.contains("SELECT (recent.author_id, recent.book_type)"), sql);
                        }
                    }
                    if (method.getName().equals("booksByTags")) {
                        assertTrue(sql.contains("Yson::ConvertToStringList"), sql);
                        assertTrue(sql.toLowerCase().contains("left outer join") || sql.toLowerCase().contains("left join"), sql);
                    }
                    if (family.equals("booktest") && method.getName().equals("getBookAndAuthor")) {
                        assertTrue(sql.contains("PRAGMA OrderedColumns;"), sql);
                        assertTrue(sql.contains("__sqlc_embed_0_1"), sql);
                        assertTrue(sql.contains("__sqlc_embed_1_0"), sql);
                    }
                    if (method.getName().equals("removeBookTag")) {
                        assertTrue(sql.contains("UPDATE `books`"), sql);
                        assertTrue(sql.contains("ListFilter("), sql);
                    }
                    if (method.getName().equals("updateAuthorAndListBooks")) {
                        assertEquals("DECLARE $author_id AS Uint64;\nDECLARE $name AS Utf8;\nUPDATE `authors` SET name = $name WHERE author_id = $author_id;\nSELECT book_id, title FROM `books` AS `books` WHERE author_id = $author_id ORDER BY book_id;", sql);
                    }
                    if (method.getName().equals("selectAuthorAndDeleteBooks")) {
                        assertEquals("DECLARE $author_id AS Uint64;\nSELECT author_id, name FROM `authors` AS `authors` WHERE author_id = $author_id;\nDELETE FROM `books` WHERE author_id = $author_id;", sql);
                    }
                    if (method.getName().equals("deleteAuthorWithBooks")) {
                        assertEquals("DECLARE $author_id AS Uint64;\nDELETE FROM `books` WHERE author_id = $author_id;\nDELETE FROM `authors` WHERE author_id = $author_id;", sql);
                    }
                }
            }
        }
        assertEquals(68, statements.size());
    }
    @Test
    void declaredQueryReadsDialectCarriers() throws Exception {
        var schema = booktest.jooq.Tables.BOOKS;
        var authors = booktest.jooq.Tables.AUTHORS;
        try (var connection = new MockConnection(ctx -> {
            assertTrue(ctx.sql().contains("DECLARE $tags AS Json;"));
            var dsl = DSL.using(YDB.DIALECT);
            var result = dsl.newResult(schema.BOOK_ID, schema.TITLE, authors.NAME, schema.ISBN, schema.TAGS);
            result.add(dsl.newRecord(schema.BOOK_ID, schema.TITLE, authors.NAME, schema.ISBN, schema.TAGS)
                    .values(ULong.MAX, "Book", null, "isbn", JSON.valueOf("[1,true]")));
            return new MockResult[]{new MockResult(1, result)};
        })) {
            var queries = new booktest.jooq.Queries(YDB.using(withNamedBinding(connection)));
            var rows = queries.booksByTags(JSON.valueOf("[]"));
            assertEquals(1, rows.size());
            assertEquals(ULong.MAX, rows.get(0).bookId());
            assertNull(rows.get(0).name());
            assertEquals("[1,true]", rows.get(0).tags().data());
        }
    }

    // The mock has no YDB extension; adapt named setters while retaining SQL execution tracing.
    // Published-driver named value conversion is exercised by the generator SDK tests.
    private static java.sql.Connection withNamedBinding(java.sql.Connection connection) {
        var named = (tech.ydb.jdbc.YdbConnection) java.lang.reflect.Proxy.newProxyInstance(
                GeneratedQueriesTest.class.getClassLoader(), new Class<?>[]{tech.ydb.jdbc.YdbConnection.class}, (proxy, method, args) -> {
                    if (!method.getName().equals("prepareStatement")) throw new AssertionError(method);
                    assertEquals(tech.ydb.jdbc.YdbPrepareMode.DATA_QUERY, args[1]);
                    assertTrue(((String) args[0]).contains("DECLARE"));
                    var statement = connection.prepareStatement((String) args[0]);
                    return java.lang.reflect.Proxy.newProxyInstance(GeneratedQueriesTest.class.getClassLoader(),
                            new Class<?>[]{tech.ydb.jdbc.YdbPreparedStatement.class}, (statementProxy, operation, values) -> {
                                if (operation.getName().startsWith("set") && values[0] instanceof String) {
                                    if (operation.getName().equals("setObject")) assertInstanceOf(tech.ydb.table.values.Value.class, values[1]);
                                    return null;
                                }
                                if (operation.getName().equals("executeQuery") || operation.getName().equals("getResultSet")) {
                                    var rows = operation.getName().equals("executeQuery") ? statement.executeQuery() : statement.getResultSet();
                                    if (rows == null) return null;
                                    return java.lang.reflect.Proxy.newProxyInstance(GeneratedQueriesTest.class.getClassLoader(),
                                            new Class<?>[]{tech.ydb.jdbc.YdbResultSet.class}, (rowProxy, getter, indexes) -> {
                                                if (getter.getName().equals("getMetaData")) {
                                                    var metadata = rows.getMetaData();
                                                    return java.lang.reflect.Proxy.newProxyInstance(GeneratedQueriesTest.class.getClassLoader(),
                                                            new Class<?>[]{tech.ydb.jdbc.YdbResultSetMetaData.class}, (metaProxy, property, arguments) ->
                                                                    java.sql.ResultSetMetaData.class.getMethod(property.getName(), property.getParameterTypes()).invoke(metadata, arguments));
                                                }
                                                return java.sql.ResultSet.class.getMethod(getter.getName(), getter.getParameterTypes()).invoke(rows, indexes);
                                            });
                                }
                                return java.sql.PreparedStatement.class.getMethod(operation.getName(), operation.getParameterTypes()).invoke(statement, values);
                            });
                });
        return (java.sql.Connection) java.lang.reflect.Proxy.newProxyInstance(GeneratedQueriesTest.class.getClassLoader(),
                new Class<?>[]{java.sql.Connection.class}, (proxy, method, args) -> {
                    if (method.getName().equals("unwrap") && args[0] == tech.ydb.jdbc.YdbConnection.class) return named;
                    return method.invoke(connection, args);
                });
    }

}
