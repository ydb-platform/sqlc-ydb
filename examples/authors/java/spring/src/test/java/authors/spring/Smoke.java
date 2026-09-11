package authors.spring;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Iterator;

import javax.sql.DataSource;

import org.springframework.boot.CommandLineRunner;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.boot.jdbc.DataSourceBuilder;
import org.springframework.context.ApplicationContext;
import org.springframework.context.annotation.Bean;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.transaction.annotation.Transactional;

import tech.ydb.data.repository.config.AbstractYdbJdbcConfiguration;

/** Run from examples/authors; the smoke creates and drops its authors table. */

@SpringBootApplication
public class Smoke extends AbstractYdbJdbcConfiguration implements CommandLineRunner {
    private static final long MAX_UINT64 = -1L;
    private static final long SECOND_ID = 7L;

    private final ApplicationContext context;

    public Smoke(ApplicationContext context) {
        this.context = context;
    }

    @Bean
    @ConfigurationProperties("spring.datasource")
    public DataSource dataSource() {
        String endpoint = System.getenv("YDB_CONNECTION_STRING");
        if (endpoint == null || endpoint.isBlank()) {
            throw new IllegalStateException("YDB_CONNECTION_STRING is required");
        }
        return DataSourceBuilder.create().url("jdbc:ydb:" + endpoint).build();
    }

    @Override
    public void run(String... args) throws Exception {
        String schema = readSchema();
        JdbcTemplate jdbc = context.getBean(JdbcTemplate.class);
        jdbc.execute(schema);
        try {
            context.getBean(Smoke.class).exercise(context.getBean(Queries.class));
        } finally {
            jdbc.execute("DROP TABLE authors;");
        }
    }

    public static void main(String[] args) throws Exception {
        SpringApplication.run(Smoke.class, args).close();
    }

    @Transactional
    public void exercise(Queries queries) {
        queries.save(new Authors(MAX_UINT64, "Unsigned", null));

        Authors emptyBio = queries.findById(MAX_UINT64).orElseThrow();
        check(emptyBio.id() == MAX_UINT64 && "Unsigned".equals(emptyBio.name()) && emptyBio.bio() == null,
                "nullable Spring row");
        check("Unsigned".equals(queries.findAuthorNameById(MAX_UINT64).orElseThrow().name()), "Spring name");

        emptyBio.setBio("Biography");
        queries.save(emptyBio);
        check("Biography".equals(queries.findById(MAX_UINT64).orElseThrow().bio()), "non-null Spring bio");
        queries.save(new Authors(SECOND_ID, "Second", null));

        Iterator<Authors> rows = queries.findAll().iterator();
        check(rows.hasNext() && "Second".equals(rows.next().name()), "Spring list result");
        check(rows.hasNext() && "Unsigned".equals(rows.next().name()), "Spring list result");
        check(!rows.hasNext(), "Spring list result");

        queries.deleteById(SECOND_ID);
        check(queries.findById(SECOND_ID).isEmpty(), "Spring delete result");
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
