# Built-in targets

| Target | Configuration | Generated API |
|---|---|---|
| Go native SDK | `gen.go.sql_package: ydb` | `New`, `Queries`, context-aware query methods |
| Go database/sql | `gen.go.sql_package: database/sql` | `New`, `DBTX`, `Queries`, `WithTx` |
| Python native SDK | `gen.python.runtime: ydb` | dataclasses and `Querier(QuerySessionPool | QueryTxContext)` |
| Python DB-API | `gen.python.runtime: dbapi` | dataclasses and a connection-based `Querier` |
| Python SQLAlchemy | `gen.python.runtime: sqlalchemy` | dataclasses and synchronous `Querier` |
| C++ native SDK | `gen.cpp.runtime: ydb` | `Queries(TQueryClient&)` or `Queries(TTransaction&)`, structs, optional/vector results |
| C++ userver | `gen.cpp.runtime: userver` | `Queries(TableClient&)` or `Queries(TxActor&)`, userver YDB bindings |
| C# ADO.NET | `gen.csharp.runtime: adonet` | async `Queries(YdbConnection)`, records, cancellation and transactions |
| C# Dapper | `gen.csharp.runtime: dapper` | async query methods on a borrowed `YdbConnection` |
| TypeScript | `gen.typescript.runtime: ydb` | query classes, row and parameter type aliases |
| Rust | `gen.rust.runtime: ydb` | async methods on a borrowed `QueryExecutor` (client or transaction) |
| PHP | `gen.php.runtime: ydb` | typed query methods for the YDB SDK |
| Kotlin Query SDK | `gen.kotlin.runtime: ydb` | data classes and `Queries(SessionRetryContext)` or `Queries(QueryTransaction)` |
| Kotlin JDBC | `gen.kotlin.runtime: jdbc` | typed query methods on a borrowed `Connection` |
| Kotlin Exposed | `gen.kotlin.runtime: exposed` | typed SQL methods on a borrowed `JdbcTransaction` |
| Java native SDK | `gen.java.runtime: ydb` | `Queries(QueryTransaction)`, Java 17 records |
| Java JDBC | `gen.java.runtime: jdbc` | `Queries(Connection)`, positional prepared statements |
| Java jOOQ | `gen.java.runtime: jooq` | `Queries(YdbDSLContext)`, typed jOOQ DSL queries and projection records |

`Utf8` is text (`string` / `str`); `String` is binary (`[]byte` / `bytes`). Optional values preserve nullability. Go integers retain their widths and signedness; Python integers bind using the resolved YQL type. Query results reflect actual selected columns, including aliases. Unsupported type/runtime combinations are errors; no generic `Any` fallback is generated.

For `:one`, Go returns a row and error; Python returns a row or `None` when no row exists. `:many` returns a Go slice or Python list. `:exec` returns only execution status. The selected YDB SDK/driver APIs do not expose a portable affected-row count, so all generators reject `:execrows`.

C++ and Java `:one` results are optional; C# throws `InvalidOperationException` when no row exists. C++, Java native/JDBC and C# return the first row when present. Rust and jOOQ reject multiple rows for `:one`; Rust reports `YdbError::NoRows` for an empty result. `:many` returns a typed collection. Java native/JDBC represent `Uint64` as the full 64-bit `long` bit pattern; use `Long.toUnsignedString` for unsigned decimal formatting. C++ uses `uint64_t` and C# uses `ulong`; jOOQ uses `ULong`. Binary YQL `String` stays binary in every target.

Type coverage differs by target. Unsupported temporal, decimal, container or other unmapped types fail explicitly; see the individual target docs. See the [C#](csharp.md), [TypeScript](typescript.md), [Rust](rust.md) and [PHP](php.md) contracts for supported types and API details.

Generated code uses caller-provided clients/connections. The caller controls connection lifetime and credentials. Transaction behavior is target-specific. Python native, both C++ profiles and Kotlin native accept transaction-scoped executors in addition to their standalone retry clients. C#, native Java and JDBC use the caller's connection or transaction. Generated DB-API code closes its own cursors and does not commit caller-owned transactions. The language-specific pages document the remaining runtime ownership contracts. The [README matrix](../README.md#supported-targets) links directly to integration tests for individual calls and multiple calls in one transaction.

For Python DB-API, select `IsolationLevel.SERIALIZABLE` and call `begin()` before constructing or using the transaction's `Querier`; the driver's default is `AUTOCOMMIT`. For SQLAlchemy, set `isolation_level="SERIALIZABLE"` on the connection before `begin()`. A transaction block alone does not override the YDB driver's autocommit isolation. The [Python smoke](../examples/authors/python/smoke.py) checks read-your-writes and rollback for all three runtimes.

Python row decoding follows the selected runtime: native YDB rows are indexed by column name, DB-API rows by position, and SQLAlchemy rows through `row._mapping`. Custom wrappers must provide that same contract. Missing result sets or columns raise errors; they are not converted into empty results or tried as other row shapes.

Python names support Unicode letters. Names that normalize to invalid Python identifiers (for example, a leading digit) are rejected, as are model names that conflict with Python keywords or the imported `Optional` type. Parameter names such as `ydb`, `models`, and `text` do not shadow the generated runtime imports.

The verified `ydb-sqlalchemy` 0.1.22 has no asynchronous dialect. Requests for `emit_async_querier: true` are explicitly rejected. Async Python adapters, Pydantic, custom naming/type overrides and additional framework profiles remain subsequent work.

Verified runtime versions: Go SDK 3.151.1; Python SDK 3.29.7, ydb-dbapi 0.1.23, ydb-sqlalchemy 0.1.22, SQLAlchemy 2.0.52. Live tests cover both Go adapters and all three Python adapters on YDB 26.3.1.8, including high Uint64, binary/text, optional values and query cardinalities. Native Go supports scalar list parameters, including optional elements and empty lists with an explicit element type. Extended temporal list elements (`Date32`, `Datetime64`, `Timestamp64`, `Interval64`) are rejected because the pinned SDK lacks their list-builder methods. `Optional<List>`, nested lists, and list elements with more than one `Optional` wrapper remain errors. database/sql rejects list parameters and results. Native Go also supports scalar list results; complex container and temporal boundary coverage remains incomplete.

Both Go adapters map `Decimal(P,S)` to `types.Decimal` and `Uuid` to `uuid.UUID`; optional values use pointers. Decimal carriers must have the precision and scale declared in the SQL model. A mismatch returns an error before execution, including for Decimal list elements. The generator does not reinterpret bytes using a different scale. database/sql uses typed YDB values for Decimal/UUID parameters rather than generic driver conversion.

Native Go query methods and generated interfaces accept trailing `opts ...query.ExecuteOption`. Options are forwarded without changing the caller's slice. Generated typed parameters are applied last, so a caller option cannot replace the bindings represented by the method arguments.

Kotlin types, nullable results and transaction ownership are documented in [Kotlin](kotlin.md). Kotlin examples use the shared authors schema and queries.

### Python result and retry contracts

All Python `:many` methods return an eagerly materialized `list`, not a stream. Use bounded queries or keyset pagination for large results. DB-API and SQLAlchemy `:one` methods fetch a single row and always close their cursor/result, including on conversion failure. Native methods validate that exactly one result set was returned. A result matching a complete table reuses the table dataclass; partial projections use query row dataclasses.

Native pool-backed helpers default to `RetrySettings(max_retries=0)`. SDK 3.29.7 retries `ConnectionLost` even with `idempotent=False`, so that flag alone does not protect a write whose commit response was lost. Opt in only for operations the application can safely repeat:

```python
reads = Querier(pool, retry_settings=ydb.RetrySettings(idempotent=True))
```

Settings apply to every call through that helper. Transaction-backed helpers never retry individual statements; pass retry settings to the surrounding SDK transaction operation. Supplying `retry_settings` to `Querier(transaction)` raises `ValueError` instead of silently ignoring them. The Python smoke suite covers `INSERT ... RETURNING`, composed rollback, and explicit versus default pool retries.
