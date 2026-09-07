# Source provenance

The standalone CLI, model, analyzer and generator renderers were written for
this implementation. They do not import or embed sqlc's intermediate AST,
compiler, plugin protocol, or generator implementation.

Behavior/API references:

- sqlc v1.31.1 configuration and command documentation;
- sqlc's Go generator for familiar query method shapes and defaults;
- sqlc-gen-python checkout `53fa0b2e3d10c4201f7a5a344d00a560330da3bb` for
  dataclass/Querier conventions;
- this repository's previous Apache-licensed engine/plugin code, preserved at
  `da046efe95d7ec65c13cd1f88a9f55804c322f73`, for YDB SDK integration examples.

The parser is an external dependency from `ydb-platform/yql-parsers`, pinned to
`2fbaa5e71a0388828bffc5c6a2c2b0e2cf9680dc` (YQL source
`d9544073fd13d17b30f609fa4fe7b034cd28ba02`). Dependency license notices remain in
their modules. If upstream source code or tests are copied in future changes,
record the source commit and preserve the applicable notices alongside them.

## Java SDK and framework references

Source snapshots inspected on 2026-09-07:

| Repository | Commit |
| --- | --- |
| [YDB Java SDK](https://github.com/ydb-platform/ydb-java-sdk) | `98aab7828816c9b92cd7583c3383865b834da0af` |
| [YDB JDBC driver](https://github.com/ydb-platform/ydb-jdbc-driver) | `a2a43af922ae90b01341a116a6cac81364656b24` |
| [YDB Java dialects](https://github.com/ydb-platform/ydb-java-dialects) | `ddd81338501c074f93671914fe914aa1addca3a5` |

These snapshots explain API choices; published dependencies used by tests are
pinned in the [example Maven build](../examples/authors/java/pom.xml).
The [Java guide](java.md) defines the generated API and resource ownership.

- The SQL-first reference is sqlc's [Kotlin JDBC output](https://github.com/sqlc-dev/sqlc-gen-kotlin/blob/2c6a78075b1b9a075427b403a07b187bc36e7451/examples/src/main/kotlin/com/example/authors/postgresql/QueriesImpl.kt):
  query constants, typed results and a borrowed connection. Its implementation
  and plugin protocol were not copied. Our `:one` returns the first row, as the
  other sqlc-ydb adapters do; it does not add Kotlin's multiple-row check.
- Native query execution uses `QueryClient`, `QuerySession`,
  `tools/SessionRetryContext` and `tools/QueryReader` in the SDK's `query` module.
  `SessionRetryContext` owns operation sessions; the caller owns the transport
  and client. Parameters use `PrimitiveValue` and `OptionalType` factories.
- JDBC's `query/params/PreparedQuery.java` sorts indexed `$pN` parameters before
  other names. The generated code binds by name through `YdbPreparedStatement`
  to avoid depending on positional order. It supplies SDK `Value<?>` objects,
  handled by `SimpleJdbcPrm.setValue` and `ValueFactory.readValue`, to preserve
  unsigned and optional types. The inspected `setObject(name, object, Type)`
  overload ignores its `Type` argument and is deliberately not used.
- SDK constructors for `Uint8/16/32` mask the signed Java carrier. Generated
  range checks prevent truncation; `Uint64` intentionally retains every bit
  of a Java `long`.
- Spring [JdbcTemplate callbacks](https://docs.spring.io/spring-framework/reference/data-access/jdbc/core.html)
  and Hibernate [doReturningWork](https://docs.hibernate.org/orm/6.6/javadocs/org/hibernate/SharedSessionContract.html#doReturningWork(org.hibernate.jdbc.ReturningWork))
  provide the selected SQL execution APIs. The generator does not infer JPA
  entities from query projections or require Spring Data repository support.
