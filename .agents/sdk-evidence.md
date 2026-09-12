# SDK implementation evidence

This maintainer reference records API snapshots and non-obvious SDK behavior used to design generated code. Published dependencies used by tests are pinned in the example manifests. Refresh the relevant snapshot and runtime evidence before changing an SDK boundary.

Language-level generated API, value mappings and ownership contracts remain in the public guides under `docs/`. C++ build details are in [C++ development](cpp-development.md), and additional C# source evidence is in [C# SDK evidence](csharp-sdk-evidence.md).

## Java SDK and framework references

Source snapshots inspected on 2026-09-07:

| Repository | Commit |
| --- | --- |
| [YDB Java SDK](https://github.com/ydb-platform/ydb-java-sdk) | `98aab7828816c9b92cd7583c3383865b834da0af` |
| [YDB JDBC driver](https://github.com/ydb-platform/ydb-jdbc-driver) | `a2a43af922ae90b01341a116a6cac81364656b24` |
| [YDB Java dialects](https://github.com/ydb-platform/ydb-java-dialects) | `ddd81338501c074f93671914fe914aa1addca3a5` |

These snapshots explain API choices; published dependencies used by tests are pinned in the [example Maven build](../examples/authors/java/pom.xml). The [Java guide](../docs/java.md) defines the generated API and resource ownership.

- The SQL-first reference is sqlc's [Kotlin JDBC output](https://github.com/sqlc-dev/sqlc-gen-kotlin/blob/2c6a78075b1b9a075427b403a07b187bc36e7451/examples/src/main/kotlin/com/example/authors/postgresql/QueriesImpl.kt): query constants, typed results and a borrowed connection. Its implementation and plugin protocol were not copied. Java native/JDBC `:one` methods return the first row; jOOQ has a separate, stricter result-cardinality contract.
- Native query execution uses `QueryClient`, `QuerySession`, `tools/SessionRetryContext` and `tools/QueryReader` in the SDK's `query` module. `SessionRetryContext` owns operation sessions; the caller owns the transport and client. `QueryTransaction.createQuery` provides the same result reader while leaving commit and rollback with the caller. Parameters use `PrimitiveValue` and `OptionalType` factories. Kotlin native exposes both ownership modes through separate generated constructors.
- Java JDBC uses standard `?` placeholders, which the driver handles through `query/params/InMemoryQuery` and `SimpleJdbcPrm`. Bind each occurrence by index; this avoids `PreparedQuery`'s name sorting and server-side preparation before binding. Typed setters handle signed primitives, text and bytes; SDK values preserve unsigned types. No generated declarations or unwrap are needed. `YdbResultSetBase.getObject(index, Class)` returns null for absent values. Kotlin JDBC and Exposed use the same positional binding contract, including typed SDK values for unsigned integers, Json and Timestamp.
- Native `ProtoOptionalValueReader.getText/getBytes` return null for absent values; primitive getters throw and still need presence checks. Non-null SDK values provide `makeOptional()`; null parameters need a typed empty value.
- SDK constructors for `Uint8/16/32` mask the signed Java carrier. Generated range checks prevent truncation; `Uint64` intentionally retains every bit of a Java `long`.
- Removed experiments used Spring [JdbcTemplate callbacks](https://docs.spring.io/spring-framework/reference/data-access/jdbc/core.html) and Hibernate [doReturningWork](https://docs.hibernate.org/orm/6.6/javadocs/org/hibernate/SharedSessionContract.html#doReturningWork(org.hibernate.jdbc.ReturningWork)). They preserved YQL but only wrapped JDBC; idiomatic ORM/repository alternatives did not preserve the SQL-first contract. Future design is tracked in [issue #12](https://github.com/ydb-platform/sqlc-ydb/issues/12).

## C#, TypeScript, Rust and PHP references

Sources inspected for these targets on 2026-09-09:

| Source | Snapshot | Verified dependency |
| --- | --- | --- |
| [YDB .NET SDK](https://github.com/ydb-platform/ydb-dotnet-sdk) | `236bfa176940feafbf61f11c1cb9fc000572b237` | `Ydb.Sdk` 0.35.0 |
| [Dapper](https://github.com/DapperLib/Dapper) | `6d48ef664acc7298c649e2d449d903b3360d5a90` | 2.1.79 |
| [YDB Rust SDK](https://github.com/ydb-platform/ydb-rs-sdk) | `fe2d4507781713b634c6584c189e05e58adbc254` | `ydb` 0.18.2 |

Dapper uses `CommandDefinition`, `IDynamicParameters` and typed query methods. It accepts explicit YDB typed values. See [C#](../docs/csharp.md) for ownership and mapping choices. Dependency pins are shared across all example families.

Rust's public `From<Option<T>> for Value` implementation requires `T: Into<Value> + Default`. Small generated wrappers preserve JSON and optional temporal wire types, including typed nulls, without changing the SDK. Rechecked against the pinned 0.18.2 crate and generator on 2026-09-12: `:one` uses `query_row`, whose `client_query/builders.rs::take_single_row` rejects multiple rows with `YdbError::Custom` and reports `YdbError::NoRows` for none. It does not have upstream sqlc's first-row semantics. The generated `:many` methods use `query_result_set`.

The [JavaScript SDK](https://github.com/ydb-platform/ydb-js-sdk) was inspected at `96793dbf49165581a1d5c21afff47905c720f44d`. Its published modules are `@ydbjs/core` 6.3.1, `@ydbjs/query` 6.3.0 and `@ydbjs/value` 6.0.8, pinned in [the shared npm lockfile](../tests/examples/typescript/package-lock.json). The query package reconstructs declarations from `.parameter()` values. The analyzer supplies declaration-free SQL using original ANTLR token spans. The maintainer-approved [TypeScript examples in PR #4](https://github.com/ydb-platform/sqlc-ydb/pull/4) use direct typed tagged templates and the SDK's normal result conversion: `Timestamp` becomes `Date`, and JSON becomes parsed `JSValue`. TypeScript generics describe those runtime values; they do not control SDK decoding. Timestamp inputs use `new Timestamp(date)`. This API has JavaScript's millisecond precision and does not preserve JSON source text. See [TypeScript](../docs/typescript.md).

The [PHP SDK](https://github.com/ydb-platform/ydb-php-sdk) is pinned to 1.16.1 (`5bce112ff6cc4a5eca83147232813cc94a675a50`) in [the shared Composer lockfile](../tests/examples/php/composer.lock). Main was inspected at `56a783e39368745a35a7bc3e206d4a8200184805`. The generated bridge uses the public `Table` accessors and `RequestTrait` to retain raw protobuf result values; [PHP](../docs/php.md) explains why the high-level result conversion is unsuitable. `Session` keeps its transaction identifier protected, and its public `query()` path performs the lossy high-level conversion. A generated helper therefore cannot execute its raw protobuf request inside a caller-owned transaction until the SDK exposes raw execution with the current transaction control.

## Python and C++ transaction executors

Python's pinned Query SDK exports both `QuerySessionPool` and `QueryTxContext`. `retry_tx_sync` passes the latter to the callback; `QueryTxContext.execute` returns the same result-set iterator consumed by the generated decoder. The generated native `Querier` accepts either object and fully consumes transaction results before the callback can finish.

The C++ SDK 3.21.1 `TTransaction` exposes `GetSession()` and can be passed to `TTxControl::Tx`. userver 3.2-rc passes `TxActor&` to `TableClient::RetryTx`, and `TxActor::Execute` accepts the same query and parameter arguments used by `TableClient::ExecuteQuery`. Generated native and userver query classes borrow these transaction objects and never commit, roll back, or retry them.

## Go SDK parameter binding

Go parameter binding adapts the historical ParamsBuilder idea against SDK `v3.151.1`, pinned in [examples/go.mod](../examples/go.mod). Inspected SDK paths:

- `internal/params/parameters.go` and `internal/params/list.go`: scalar, optional, list and Decimal builders. Empty lists use `types.ZeroValue` with an explicit list element type; the SDK's `Any(types.Value)` entry point receives a fully typed value, not an unresolved model type.
- `internal/value/value.go`, `internal/value/nullable.go` and `pkg/decimal/type.go`: Decimal and UUID carriers, typed constructors, nullable values and scanning.
- `internal/query/options/execute.go`: variadic execution options and parameter precedence. Generated parameter bindings are applied after caller options.
- `tests/integration/database_sql_regression_test.go` and `tests/integration/decimal_test.go`: typed database/sql parameter and scanning examples. SDK runtime imports remain outside the generator's own module.

### jOOQ YDB prototype (2026-09-11)

Pinned runtime: jooq 3.21.0, jooq-ydb-dialect 2.0.0, ydb-jdbc-driver 2.4.1, Java 21. Inspected the published jars and corresponding ydb-java-dialects sources: [YdbTypes](https://github.com/ydb-platform/ydb-java-dialects/blob/main/jooq-dialect/src/main/java/tech/ydb/jooq/YdbTypes.java), [UpsertTest](https://github.com/ydb-platform/ydb-java-dialects/blob/main/jooq-dialect/src/test/java/tech/ydb/jooq/UpsertTest.java), [YdbDSLContextImpl](https://github.com/ydb-platform/ydb-java-dialects/blob/main/jooq-dialect/src/main/java/tech/ydb/jooq/impl/YdbDSLContextImpl.java). `org.jooq.impl.YdbListener` quotes Name nodes except Name.Quoted.SYSTEM. The DEFAULT DML execution path uses executeUpdate/getGeneratedKeys, incompatible with YDB RETURNING result sets. Generated RETURNING statements therefore execute as ResultQuery query parts with explicit field coercion. The exact compatibility contract and commands are in [Java generation](../docs/java.md#jooq-prototype); `tests/examples/java/jooq` compiles every example method and executes live checks in isolated tables. No upstream source was copied into the generator.

## Python retry and fetch contracts (2026-09-11)

Pinned `ydb==3.29.7` uses `RetrySettings(idempotent=False)` by default, but `ydb._errors.check_retriable_error` still retries `ConnectionLost` regardless of that flag. Native generated helpers therefore default to `max_retries=0`; applications explicitly provide retry settings for safely repeatable operations. The smoke test exercises the real SDK retry loop with an injected lost response. Transaction-backed helpers do not retry statements independently.

DB-API and SQLAlchemy expose `fetchone()` for first-row queries; generated `:many` APIs return eager lists. Full table projections reuse the table model.
