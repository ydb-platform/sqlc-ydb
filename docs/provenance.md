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
their modules.

The [examples](../examples/README.md) adapt SQL schemas, queries and scenarios
from all five families in `sqlc-dev/sqlc` at commit
[`3c2546a4b47fabbcec3e07df420effb1a464728f`](https://github.com/sqlc-dev/sqlc/tree/3c2546a4b47fabbcec3e07df420effb1a464728f/examples).
The upstream MIT notice is preserved in [UPSTREAM_LICENSE](../examples/UPSTREAM_LICENSE).
PostgreSQL/MySQL/SQLite variants were inspected for distinct scenarios; the
adaptations use YDB SQL and SDKs. Each example README records material changes.
Upstream generated Go, pgx batch wrappers and multi-engine test helpers are not
embedded in this implementation.

YDB-specific adaptations follow the main documentation for
[scalar SELECT](https://ydb.tech/docs/en/yql/reference/syntax/select/?version=main),
[string concatenation](https://ydb.tech/docs/en/yql/reference/syntax/expressions?version=main),
[Yson JSON conversion](https://ydb.tech/docs/en/yql/reference/udf/list/yson?version=main),
and [set operations](https://ydb.tech/docs/en/yql/reference/builtins/dict).
Runtime checks execute the resulting SQL on the local-ydb version pinned in CI.

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

## C# framework, JavaScript, Rust and PHP references

Sources inspected for these targets on 2026-09-09:

| Source | Snapshot | Verified dependency |
| --- | --- | --- |
| [YDB .NET SDK](https://github.com/ydb-platform/ydb-dotnet-sdk) | `236bfa176940feafbf61f11c1cb9fc000572b237` | `Ydb.Sdk` 0.35.0 |
| [Dapper](https://github.com/DapperLib/Dapper) | `6d48ef664acc7298c649e2d449d903b3360d5a90` | 2.1.79 |
| [linq2db](https://github.com/linq2db/linq2db/tree/v6.4.0) | `82fbf0f91399cc8c9cea22d09dcae20e4d7568c6` | 6.4.0 |
| [YDB Rust SDK](https://github.com/ydb-platform/ydb-rs-sdk) | `fe2d4507781713b634c6584c189e05e58adbc254` | `ydb` 0.18.2 |

Dapper uses `CommandDefinition`, `IDynamicParameters` and reader execution;
linq2db supplies a YDB provider and `YdbTools` connection/transaction adapters.
Both accept explicit YDB typed values. See [C#](csharp.md) for ownership and
mapping choices. Dependency pins are shared across all example families.

Rust's public `From<Option<T>> for Value` implementation requires
`T: Into<Value> + Default`. Small generated wrappers preserve JSON and temporal
wire types, including typed nulls, without changing the SDK. `query_result_set`
provides first-row semantics for `:one`; the SDK's stricter `query_row` would
reject a result containing several rows.

The [JavaScript SDK](https://github.com/ydb-platform/ydb-js-sdk) was inspected at
`96793dbf49165581a1d5c21afff47905c720f44d`. Its published modules are
`@ydbjs/core` 6.3.1, `@ydbjs/query` 6.3.0 and `@ydbjs/value` 6.0.8, pinned in
[the shared npm lockfile](../examples/package-lock.json).
The query package reconstructs declarations from `.parameter()` values. The
analyzer supplies declaration-free SQL using original ANTLR token spans. The
SDK's public `.raw()` result mode and primitive value constructors preserve
microsecond timestamps without conversion through JavaScript `Date`.

The [PHP SDK](https://github.com/ydb-platform/ydb-php-sdk) is pinned to 1.16.1
(`5bce112ff6cc4a5eca83147232813cc94a675a50`) in
[the shared Composer lockfile](../examples/php/composer.lock). Main was inspected
at `56a783e39368745a35a7bc3e206d4a8200184805`. The generated bridge uses the
public `Table` accessors and `RequestTrait` to retain raw protobuf result values;
[PHP](php.md) explains why the high-level result conversion is unsuitable.
