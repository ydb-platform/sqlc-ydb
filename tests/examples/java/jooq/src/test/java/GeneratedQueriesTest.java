import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;

import org.jooq.JSON;
import org.jooq.impl.DSL;
import org.jooq.tools.jdbc.MockConnection;
import org.jooq.tools.jdbc.MockResult;
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
            var dsl = YDB.using(connection);
            for (String family : List.of("authors", "batch", "booktest", "jets", "ondeck")) {
                Class<?> type = Class.forName(family + ".jooq.Queries");
                Object queries = type.getConstructor(tech.ydb.jooq.YdbDSLContext.class).newInstance(dsl);
                for (Method method : type.getDeclaredMethods()) {
                    Object[] args = new Object[method.getParameterCount()];
                    for (int i = 0; i < args.length; i++) {
                        Class<?> parameter = method.getParameterTypes()[i];
                        if (parameter == ULong.class) args[i] = ULong.MAX;
                        else if (parameter == String.class) args[i] = "value";
                        else if (parameter == Integer.class) args[i] = 2026;
                        else if (parameter == Instant.class) args[i] = Instant.parse("2026-01-01T00:00:00.123456Z");
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
                    if (method.getName().startsWith("create") || method.getName().equals("updateVenueName")) {
                        assertTrue(sql.toLowerCase().contains("returning"), sql);
                    }
                    if (method.getName().equals("booksByTags")) {
                        assertTrue(sql.contains("Yson::ConvertToStringList"), sql);
                        assertTrue(sql.contains("left outer join") || sql.contains("left join"), sql);
                    }
                }
            }
        }
        assertEquals(40, statements.size());
    }
}
