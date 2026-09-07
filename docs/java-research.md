# Java target research

Checked 2026-09-07 against these current default-branch snapshots:

| repository | commit |
| --- | --- |
| `ydb-platform/ydb-java-sdk` | `98aab7828816c9b92cd7583c3383865b834da0af` |
| `ydb-platform/ydb-jdbc-driver` | `a2a43af922ae90b01341a116a6cac81364656b24` |
| `ydb-platform/ydb-java-dialects` | `ddd81338501c074f93671914fe914aa1addca3a5` |

The example pins published artifacts rather than these source snapshots:
SDK BOM `2.4.11`, JDBC `2.4.1`, Hibernate dialect `1.7.0`. Maven Central metadata
on 2026-09-07 reports these as latest/release values.
The Spring Data JDBC dialect is intentionally absent: the generated Spring
profile uses `JdbcTemplate`, not Spring Data repository support.

## Final generated architecture

All four profiles emit a final `Queries` class and top-level Java records.
Methods are lower camel case and follow SQL command cardinality:
`Optional<Row>` for `:one`, `List<Row>` for `:many`, and `void` for `:exec`.
`Uint64` is a Java `long` carrying the original 64-bit pattern. Nullable
`Utf8` is `String`; nullable output is guarded with the driver's `wasNull()`
for JDBC paths and optional-item checks for native paths.

The constructors borrow application resources:

* native: `Queries(SessionRetryContext)`;
* JDBC: `Queries(Connection)`;
* Spring: `Queries(JdbcTemplate)`, using `JdbcTemplate.execute` with a
  `ConnectionCallback`;
* Hibernate: `Queries(Session)`, using `Session.doReturningWork` and the same
  typed JDBC operations. Query projections are records, not generated JPA
  entities.

Generated JDBC, Spring, and Hibernate methods prepare the original declared
YQL, unwrap `tech.ydb.jdbc.YdbPreparedStatement`, and bind `author_id`,
`author_name`, and `biography` by name (the setter adds `$`). This follows the current driver's
`YdbPreparedStatement` API and avoids relying on positional order. The driver
source in `jdbc/src/main/java/tech/ydb/jdbc/query/params/PreparedQuery.java`
(lines 42-73) sorts indexed `$pN` parameters first and then other names; this is
why generated code uses name setters. `MappingSetters.castToUint64` in
`jdbc/src/main/java/tech/ydb/jdbc/common/MappingSetters.java` (lines 370-418)
passes a `Long` to `PrimitiveValue.newUint64`, preserving `-1L` as `2^64-1`.
Generated setters pass a concrete SDK `Value<?>`, which is explicitly handled
by both `SimpleJdbcPrm.setValue` and `ValueFactory.readValue`. This preserves
unsigned and optional types for both declared and inferred parameters. The
custom `setObject(name, object, Type)` overload is not used: its implementation
does not use the supplied `Type` argument.

SDK `Uint8`, `Uint16`, and `Uint32` constructors mask their signed Java carrier.
Generated methods reject negative or oversized values before calling any SDK
or JDBC method; `Uint64` intentionally preserves all bits of `long`.

## Verified native SDK path

The published SDK API used by the native profile is:

```java
try (GrpcTransport transport = GrpcTransport.forConnectionString(dsn).build();
     QueryClient client = QueryClient.newClient(transport).build()) {
    SessionRetryContext retry = SessionRetryContext.create(client).build();
    QueryReader reader = retry.supplyResult(session ->
        QueryReader.readFrom(session.createQuery(sql, TxMode.SERIALIZABLE_RW, params)))
        .join().getValue();
}
```

The exact classes are in `query/src/main/java/tech/ydb/query/QueryClient.java`,
`QuerySession.java`, `QueryStream.java`, and
`tools/{QueryReader,SessionRetryContext}.java`; `TxMode` is in
`common/src/main/java/tech/ydb/common/transaction/TxMode.java`. DDL in the
smoke uses `TxMode.NONE`; reads and writes use the appropriate query transaction
mode. `QueryReader.getResultSetCount/getResultSet` return
`ResultSetReader`; its `next`, `getColumn`, and `ValueReader` getters decode
rows. `PrimitiveValue.newUint64(long)`, `newText(String)`, and
`OptionalType.emptyValue/newValue` are the verified parameter factories.

The caller closes transport and query client. `SessionRetryContext` creates and
closes per-operation query sessions internally, so generated methods borrow the
retry context and never close it.

## Framework notes

The SQL-first JVM reference is sqlc's own
[Kotlin JDBC output](https://github.com/sqlc-dev/sqlc-gen-kotlin/blob/2c6a78075b1b9a075427b403a07b187bc36e7451/examples/src/main/kotlin/com/example/authors/postgresql/QueriesImpl.kt):
query constants, typed methods/results, a borrowed `Connection`, and owned
prepared statements. The new Java implementation follows that shape without
copying the Kotlin implementation or its plugin protocol. Its `:one` behavior
matches this project's Go/Python adapters (first row), rather than Kotlin's
additional multiple-row check.

Spring's documented
[JdbcTemplate callbacks](https://docs.spring.io/spring-framework/reference/data-access/jdbc/core.html)
provide connection management and exception translation for handwritten SQL.
Hibernate's documented
[doReturningWork](https://docs.hibernate.org/orm/6.6/javadocs/org/hibernate/SharedSessionContract.html#doReturningWork(org.hibernate.jdbc.ReturningWork))
provides JDBC access using the session's connection. These are the framework
integration points selected here; inferring JPA entities from SQL projections
is not part of this generator.

Spring's generated API stays SQL first and works with `JdbcTemplate`; the smoke
uses `SingleConnectionDataSource` only to make connection ownership explicit.
Hibernate uses its native JDBC connection callback, so no entity mapping is
needed for a manual SQL projection. The YDB Hibernate 6 dialect class checked
in the current dialect source is `tech.ydb.hibernate.dialect.YdbDialect`.

The four smoke programs read and execute `examples/authors/schema.sql`, then
drop `authors` only after their own successful create. They require
`SQLC_YDB_TEST_DSN` and are intended to run sequentially against a disposable
database. They do not run automatically during Maven compilation.
