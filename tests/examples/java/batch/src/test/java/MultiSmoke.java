import java.sql.DriverManager;
import java.util.Properties;

import multires.jdbc.Queries;

public final class MultiSmoke {
    private MultiSmoke() { }

    public static void main(String[] args) throws Exception {
        String endpoint = java.util.Objects.requireNonNull(System.getenv("YDB_CONNECTION_STRING"));
        for (boolean stream : new boolean[]{false, true}) {
            Properties properties = new Properties();
            properties.setProperty("useStreamResultSets", Boolean.toString(stream));
            try (var connection = DriverManager.getConnection("jdbc:ydb:" + endpoint, properties)) {
                Queries queries = new Queries(connection);
                var summary = queries.fetchSummary(42);
                check(summary.item().isEmpty(), "empty named result");
                check(summary.flags().size() == 1 && summary.flags().get(0).enabled(), "scalar named result");
                check(summary.result3().size() == 1 && summary.result3().get(0).status().equals("ready"), "default-named result");

                var literals = queries.bareLiterals();
                check(literals.result1().size() == 1 && literals.result1().get(0).column0() == 1, "first unnamed result");
                check(literals.result2().size() == 1 && literals.result2().get(0).column0().equals("2"), "second unnamed result");
                check(literals.result3().size() == 1 && !literals.result3().get(0).column0(), "third unnamed result");
            }
        }
    }

    private static void check(boolean condition, String message) {
        if (!condition) throw new AssertionError(message);
    }
}
