# SDK implementation evidence

This maintainer reference records API snapshots and non-obvious SDK behavior used to design generated code. Published dependencies used by tests are pinned in the example manifests. Refresh the relevant snapshot and runtime evidence before changing an SDK boundary.

Language-level generated API, value mappings and ownership contracts remain in the public guides under `docs/`. C++ build details are in [C++ development](cpp-development.md), and additional C# source evidence is in [C# SDK evidence](csharp-sdk-evidence.md).

## Go error stack traces

Checked against the pinned Go SDK 3.151.1 on 2026-09-13: [`pkg/xerrors.WithStackTrace`](https://github.com/ydb-platform/ydb-go-sdk/blob/v3.151.1/pkg/xerrors/stacktrace.go) delegates to the [internal wrapper](https://github.com/ydb-platform/ydb-go-sdk/blob/v3.151.1/internal/xerrors/stacktrace.go), which returns `nil` for `nil`, records the caller location and exposes the original error through `Unwrap`. Generated native methods and Decimal validation helpers wrap returned errors; `database/sql` retains its error behavior. `TestGeneratedYDBErrorStackTraces` compiles separate `:one`, `:many` and `:exec` files against this SDK and executes failure paths to verify generated locations, `errors.Is`/`errors.As`, Decimal validation and successful `Exec` returning `nil`.

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
- Java JDBC, Kotlin JDBC and Exposed share the positional binding and declared-query preparation contracts in [JDBC declarations and scalar setters](#jdbc-declarations-and-scalar-setters). Native execution retains the separate Query SDK ownership contract above.
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

The [JavaScript SDK](https://github.com/ydb-platform/ydb-js-sdk) was inspected at `96793dbf49165581a1d5c21afff47905c720f44d`. Its published modules are `@ydbjs/core` 6.3.1, `@ydbjs/query` 6.3.0 and `@ydbjs/value` 6.0.8, pinned in [the shared npm lockfile](../tests/examples/typescript/package-lock.json). Declaration handling is documented [below](#typescript-declarations). The maintainer-approved [TypeScript examples in PR #4](https://github.com/ydb-platform/sqlc-ydb/pull/4) use direct typed tagged templates and the SDK's normal result conversion: `Timestamp` becomes `Date`, and JSON becomes parsed `JSValue`. TypeScript generics describe those runtime values; they do not control SDK decoding. Timestamp inputs use `new Timestamp(date)`. This API has JavaScript's millisecond precision and does not preserve JSON source text. See [TypeScript](../docs/typescript.md).

The [PHP SDK](https://github.com/ydb-platform/ydb-php-sdk) is pinned to 1.16.1 (`5bce112ff6cc4a5eca83147232813cc94a675a50`) in [the shared Composer lockfile](../tests/examples/php/composer.lock). Main was inspected at `56a783e39368745a35a7bc3e206d4a8200184805`. The generated bridge uses the public `Table` accessors and `RequestTrait` to retain raw protobuf result values; [PHP](../docs/php.md) explains why the high-level result conversion is unsuitable. `Session` keeps its current transaction identifier protected, but [`beginTransaction()`](https://github.com/ydb-platform/ydb-php-sdk/blob/5bce112ff6cc4a5eca83147232813cc94a675a50/src/Session.php#L222-L241) returns that ID. Generated `withTx(Session, string)` uses the saved ID with public `YdbQuery::txControl()` and the raw bridge; the caller retains transaction ownership. The SDK's `retryTransaction()` callback does not expose the current ID, so binding from that callback's session alone remains unsupported.

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

`Session::take()` marks a session busy; the default memory pool selects only idle sessions. `withTx()` takes the session, and bound raw requests do not release it, including after errors. SDK `commitTransaction()` and `rollbackTransaction()` use `Session::request()`, which releases the session. The live suite verifies that pool acquisition between bound calls selects a different session and that both transaction completion paths release the original.

## List-of-struct batch parameters

The batch example uses one declared `List<Struct<...>>` parameter and `INSERT ... SELECT ... FROM AS_TABLE($books)`. Empty lists retain their full declared schema and execute normally. Container construction remains specific to each SDK:

- Go SDK 3.151.1: `types.ListValue`, `types.StructValue`, `types.StructFieldValue`, and `types.ZeroValue(types.List(types.Struct(...)))`. The database/sql adapter's `CheckNamedValue` and parameter binder pass SDK typed values through. Generated-code tests compile and inspect both adapters' empty and populated bindings.
- Java/Kotlin SDK 2.4.11: `StructType.of(Map)`, `StructValue.of(Map)`, and `ListType.of(Type).newValue(List)`; actual JDBC `InMemoryQuery` tests validate structured parameter serialization, including empty lists. jOOQ 3.21 exposes `DSLContext.connection(ConnectionRunnable)`; explicitly declared methods borrow that connection, prepare DATA_QUERY statements, and render resolved table references with `dsl.render` to retain table mapping.
- TypeScript `@ydbjs/value` 6.0.8: `List` infers `NullType` without items, so the binder implements the public `Value` contract with explicit `ListType(StructType(...))` and delegates value encoding to `List`. `Struct` and `StructType` sort member names consistently. SDK execution tests include optional fields and the `__proto__` property.
- PHP SDK 1.16.1: protobuf `Type.list_type`, `ListType.item`, `StructType.members` and `Value.items` encode the explicit schema and matching field order. The generated scalar codecs preserve Uint64 and JSON semantics inside each row.
- C++ SDK 3.21.1: `TValueBuilder(const TType&)`, `BeginList`, `AddListItem`, `BeginStruct` and `AddMember` build typed values. userver's `io/list.hpp` uses `EmptyList(ValueTraits<ValueType>::MakeType())`; `io/structs.hpp` derives the type from item fields and `kYdbMemberNames`. The `batch_values` executable checks both SDK serializers without a server.

C# uses the public mutable protobuf returned by `YdbValue.GetProto` because the pinned SDK has no complex empty-list factory; see [C# SDK evidence](csharp-sdk-evidence.md). Runtime tests, rather than rendering assertions alone, establish the empty-list wire schema.

Python SDK 3.29.7 supplies `StructType.add_member` and list/struct protobuf serialization by original field name. Native `TypedValue` and the DBAPI/SQLAlchemy typed-parameter tuples retain that declared type. Serializer regressions use ydb-dbapi 0.1.23, ydb-sqlalchemy 0.1.22 and SQLAlchemy 2.0.52. Rust SDK 0.18.2 provides `Value::struct_from_fields` and `Value::list_from(type exemplar, values)`, including empty lists. Generated structs preserve the borrowed iterable API; SDK tests cover scalar and optional field bindings.

### JDBC declarations and scalar setters

Pinned JDBC 2.4.1 `YdbCache.prepareYdbQuery` takes the AUTO batching branch after preparing a write with a single `List<Struct>` parameter. `BatchedQuery.tryCreateBatched` exposes the struct members as individual statement parameters. The published-SDK regression passes the generated eight-field batch schema: AUTO accepts neither the named `books` list nor that list at position 1, while `PreparedQuery` accepts empty and populated lists. `YdbPrepareMode.DATA_QUERY` bypasses this transformation. Its `prepareStatement(String, YdbPrepareMode)` overload belongs to `YdbConnection`, so a borrowed `java.sql.Connection` must be unwrapped for this path. Source: [YdbCache](https://github.com/ydb-platform/ydb-jdbc-driver/blob/a2a43af922ae90b01341a116a6cac81364656b24/jdbc/src/main/java/tech/ydb/jdbc/context/YdbCache.java), [BatchedQuery](https://github.com/ydb-platform/ydb-jdbc-driver/blob/a2a43af922ae90b01341a116a6cac81364656b24/jdbc/src/main/java/tech/ydb/jdbc/query/params/BatchedQuery.java).

The Java/Kotlin published-SDK tests exercise the same `setParam` calls as ordinary JDBC setters: `setString` produces Text and `setInt` produces Int32 on the positional `InMemoryQuery` path, so Json and unsigned parameters retain SDK-value binding there. `setTimestamp` preserves Timestamp and uses the standard setter. With declared metadata, `PreparedQuery` converts ordinary string/integer inputs into the declared Json/Uint8 types; generated named scalar bindings use standard setters. SDK values remain necessary for structured lists and retain the full Uint64 bit pattern. Nullable SDK reference getter tests verify Text, Bytes, Json and Timestamp return null without a presence check; primitive getters still need one. Present optional values use `makeOptional()`; absent parameters use a typed empty value. These checks run in `TestGeneratedJDBCUsesTypedDriverValuesAndGuardsUnsignedRanges` for both Java and Kotlin.

### TypeScript declarations

Pinned `@ydbjs/query` 6.3.0 prepends declarations in its public `Query.text` getter and has no disable option. For explicitly declared queries, generated code binds inferred parameters first, captures `stmt.text` as an own read-only property, then binds declared parameters. This avoids duplicate declarations while retaining inferred declarations and the same SDK query object. Queries without declarations use the SDK normally. Published-SDK tests verify that the object is extensible and `text` is a prototype getter, then capture actual `executeQuery` requests to check SQL, typed empty and populated lists, retries and caller-owned transactions.

## Database-assisted analysis (2026-09-22)

The compiler transport uses official protobuf APIs through `ydb-go-genproto` at `65bfd5c4b705` (module version `v0.0.0-20260810122915-65bfd5c4b705`), gRPC 1.78.0 and protobuf 1.36.10. It does not import a runtime SDK. `TableService.DescribeTable` supplies `ColumnMeta.type`, `from_sequence` and ordered primary keys; `QueryService.ExecuteQuery` with `EXEC_MODE_EXPLAIN` compiles queries without executing them or requiring parameter values. Both calls are sessionless. Transport tests assert the mode, absence of transaction control and parameter values, database/authentication headers, timeout and cancellation, TLS certificate verification and rejected server metadata.

Server source inspected at [`ydb-platform/ydb@204baf30e62446f850fc0271979aa309e2932d63`](https://github.com/ydb-platform/ydb/tree/204baf30e62446f850fc0271979aa309e2932d63): `ydb/public/api/protos/ydb_table.proto`, `ydb/public/api/protos/ydb_query.proto`, `ydb/core/grpc_services/rpc_describe_table.cpp`, `ydb/core/grpc_services/query/rpc_execute_query.cpp` and `ydb/core/kqp/compile_service/kqp_compile_actor.cpp`. The query handler maps EXPLAIN to the compile action and forwards result sets only for EXECUTE. Compilation replay metadata contains parameter types and table metadata; it is not a public result-column typing contract.

Live probes on local-ydb 26.2.1.14, image digest `sha256:9e46fd45875551a75bcf34d0bb9ca0baa1d8763a4ccf2070af45f4467c4b7402`, verified sessionless DescribeTable and EXPLAIN. VALIDATE returned `BAD_REQUEST: Unexpected query type`, so the transport always uses EXPLAIN. DescribeTable retained DDL column order, whereas wildcard SELECT returned columns in lexicographic order. `TestLiveYDBDatabaseAnalysis` verifies schema discovery, drift rejection, unchanged data across compile/generate/diff and execution of generated Go database/sql, native Go and Python queries, including wildcard order, nullable fields, maximum Uint64 and DML.

The semantic, typed-DML and database-analysis live suites also passed sequentially on the CI-pinned local-ydb 26.3.1.8, digest `sha256:d0c402700a78cbb8a45b35ffd4be69f5fcb241361ffe3fa5c8846d56b5872294`. A fresh container mounted read-only `/init.d` scripts for CREATE TABLE followed by seed data; both scripts ran in filename order, the seeded row was verified, and schema-free compile/generate/diff succeeded.

On 2026-09-22, `TestLiveYDBWildcardSchemaEvolution` reproduced the old failure on nightly: after generation followed by `ALTER TABLE ADD COLUMN`, saved SELECT and RETURNING wildcards failed with `sql: expected 4 destination arguments in Scan, not 3` in offline, discovery and connected-local-schema modes. After shared SQL expansion, the same test passed on the nightly snapshot identified below and stable 26.3.1.8 through Go SDK 3.151.1 database/sql, including SELECT, qualified SELECT and INSERT/UPDATE/DELETE RETURNING. The semantic suite executes the resolved SQL and verifies wildcard types, order and result names for ordinary SELECT, LEFT JOIN and UNION. Explicit projections retain specified order; wildcard aliases preserve unqualified result keys. Catalog-wide sorting is no longer needed. The database-generation suite still verifies native Go and Python against actual DescribeTable order.

A stable 26.3.1.8 probe also rejected unaliased AS_TABLE sources in a JOIN with `JOIN: missing correlation name for source`; the analyzer requests an explicit alias instead of emitting a synthetic qualifier during wildcard expansion.

Separate feasibility probes confirmed that SELECT with LIMIT 0 returns public result-column metadata with zero rows, including computed expressions, but declared parameters still require supplied typed values. Wrapping a projection in `SELECT * FROM (...) LIMIT 0` can change its column order. The current compiler does not use this execution path or synthesize parameter values; arbitrary server-derived projection typing remains planned.

A fresh pull of `local-ydb:nightly` on 2026-09-22 resolved to digest `sha256:ca76a10ab5ef8d2ef3b923375f95d486e86f6ce9dedbac1c3b9502d266f4b2c2`. The image creation label is `2026-09-17T03:16:28.962Z`, image source revision `f03b7bce19a35d7c4c5f3d344776f4789af2926f`; the server binary reports revision `f5322db16f01ca681cff2e0e55a222084c3e6c42`. These are distinct image and binary provenance values, not a stable release version. On this snapshot, complex LIMIT 0 retained ordered result metadata with no rows (`Uint64`, `Optional<Utf8>`, `List<Int32>`, `Optional<Decimal(22,9)>` and a window `Uint64`). DECLARE plus EXPLAIN succeeded without values and returned plan/AST statistics without typed result sets. The same declared query in execution mode failed with `Missing value for parameter: $id` until a typed value was supplied.

Separate nightly EXPLAIN probes for CREATE TABLE, DROP TABLE and a declared-parameter UPDATE left schema and rows unchanged; test-only execution performed setup and cleanup.

An undeclared `$id` in the original SQL produced `GENERIC_ERROR` with issue text `Unknown name: $id` and issue code 0. Connected analysis now sends the original query to EXPLAIN before catalog discovery or local query semantics, without synthesizing declarations or classifying query complexity. Only this server parameter diagnostic receives the explicit DECLARE hint; unrelated server errors retain their original diagnostics. Offline parameter inference is unchanged. `TestLiveYDBQueryMetadata` makes the metadata/parameter distinction reproducible. The updated metadata and database-generation suites passed locally, sequentially, on both this nightly digest and stable 26.3.1.8. CI runs them on both the pinned stable image and the moving nightly tag.

Parameter and result metadata are different contracts. The pinned QueryService EXPLAIN response has no typed parameter map, including with NONE, BASIC, FULL or PROFILE statistics. A separate QueryService.ExecuteScript(EXPLAIN) probe also completed successfully with empty `ExecuteScriptMetadata.result_sets_meta`; its operation was then forgotten. The compiler does not depend on the deprecated ScriptingService to obtain parameter types. With the connected-mode prerequisite of explicit DECLARE statements, parameter types can instead come from those declarations after QueryService validates the query; this requires no expression-type inference. A future result-probing path can preserve the existing resolved model and consume ordered server result metadata, with an explicit probe-value contract and target-specific diagnostics where a generator needs resolved syntax.

A nightly fixed-fixture probe established a concrete LIMIT 1 hazard: `SELECT Ensure($id, $id > 0ul, 'id must be positive') AS id LIMIT 1` with declared Uint64 and a typed test value of zero fails with `PRECONDITION_FAILED`. LIMIT 0 for the same query/value returns `id:Uint64` and no rows. This is evidence for preferring LIMIT 0 when only column metadata is needed; it is not a guarantee that arbitrary synthesized values are valid for every query. The compiler still does not execute application queries or silently substitute test values.
