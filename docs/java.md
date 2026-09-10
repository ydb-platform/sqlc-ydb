# Java generation

The authors example has four independent Java 17 profiles. The generated API is
SQL first: each profile keeps the query text and produces a final `Queries`
class with top-level row records. `:one` methods return `Optional<Row>`,
`:many` methods return `List<Row>`, and `:exec` methods return `void`.

Configure the Java package and its output directory relative to `sqlc.yaml`:

```yaml
gen:
  java:
    package: authors.nativeapi
    out: java/native/src/main/java/authors/nativeapi
    runtime: ydb
```

`runtime` accepts `ydb` (also `native`), `jdbc`, `spring`, or `hibernate`.
Files are emitted directly into `out`; match it to your Java package directory.
Each schema table and query projection gets a record, without ORM annotations.

Build with Java 17 or newer and Maven, then run the smoke programs:

```sh
cd examples/authors
mvn -f java/pom.xml test-compile
export SQLC_YDB_TEST_DSN=grpc://localhost:2136/local
# Each smoke creates and drops `authors`; use a disposable database.
sh java/run-smoke.sh
```

The smoke sources are compile-time examples and require a running target only
when their `main` methods are invoked. Each checks both nullable biography
states, a `Uint64` whose bit pattern is `2^64-1` (`-1L` in Java), result
mapping, and deletion. The runner invokes profiles sequentially. Each creates
and drops its own `authors` table and fails if a table already exists.

Published dependencies are pinned to YDB SDK BOM `2.4.11`, JDBC `2.4.1`, and
Hibernate YDB dialect `1.7.0`. Spring uses `spring-jdbc` directly.

The native constructor receives a borrowed
`tech.ydb.query.tools.SessionRetryContext`. The application owns and closes
`GrpcTransport` and `QueryClient`; generated query methods do not close either.
The JDBC constructor receives a borrowed `java.sql.Connection`; statements and
result sets are method-owned and the connection remains application-owned.
The Spring constructor receives a `JdbcTemplate`; the example wraps one
borrowed connection in `SingleConnectionDataSource`. The Hibernate constructor
receives an open `org.hibernate.Session` and runs the same typed JDBC operations
inside `Session.doReturningWork`, so a projection does not require a generated
JPA entity.

For JDBC, variables are bound through the driver's `YdbPreparedStatement`
name-based `setObject` with an explicitly typed SDK `Value`. Names omit the
leading `$`, which the driver adds itself. This is required because the driver orders
indexed `$p1` parameters first and then other names alphabetically. `Uint64`
uses `long` as a bit-preserving representation, so `-1L` must remain `-1L`; do
not convert it through `int` or floating point. Nullable `Utf8` is `String`
with a null binding and nullable result; binary YQL `String` values are
`byte[]` in the generated Java API.
Explicit values also retain inferred YQL types when the SQL has no `DECLARE`.
`Uint8`, `Uint16`, and `Uint32` inputs are checked before execution, so a wider
Java integer cannot be silently truncated by an SDK constructor.

Supported scalar types are `Bool`, signed and unsigned integers, `Float`,
`Double`, `Utf8`, and `String`, plus one level of `Optional<T>`. Optional
primitives use boxed Java types; unsupported types fail generation. Native
query methods execute one transaction per method using `SERIALIZABLE_RW`.
JDBC, Spring, and Hibernate methods use the caller's transaction; they never
commit, roll back, or close caller-owned connections or sessions.
Before using the Hibernate adapter, flush any pending ORM changes that the
query must see: `doReturningWork` does not infer Hibernate entity flush rules
from YQL. Transaction boundaries and entity lifecycle remain application-owned.

SQL uses Java 17 text blocks with escaped delimiters, control characters, and
trailing whitespace. Literal tests compile and execute the emitted Java and
compare exact UTF-8 bytes with the original SQL.

The inspected SDK sources and framework API references are recorded in
[source provenance](../.agents/sdk-evidence.md#java-sdk-and-framework-references).
Dependency versions used by the example are pinned in [its Maven build](../examples/authors/java/pom.xml).
