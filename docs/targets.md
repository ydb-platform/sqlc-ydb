# Built-in targets

| Target | Configuration | Generated API |
|---|---|---|
| Go native SDK | `gen.go.sql_package: ydb` | `New`, `Queries`, context-aware query methods |
| Go database/sql | `gen.go.sql_package: database/sql` | `New`, `DBTX`, `Queries`, `WithTx` |
| Python native SDK | `gen.python.runtime: ydb` | dataclasses and `Querier(QuerySessionPool)` |
| Python DB-API | `gen.python.runtime: dbapi` | dataclasses and a connection-based `Querier` |
| Python SQLAlchemy | `gen.python.runtime: sqlalchemy` | dataclasses and synchronous `Querier` |
| C++ native SDK | `gen.cpp.runtime: ydb` | `Queries(TQueryClient&)`, structs, optional/vector results |
| C++ userver | `gen.cpp.runtime: userver` | `Queries(TableClient&)`, userver YDB bindings |
| C# ADO.NET | `gen.csharp.runtime: adonet` | async `Queries(YdbConnection)`, records, cancellation and transactions |
| C# Dapper | `gen.csharp.runtime: dapper` | async query methods on a borrowed `YdbConnection` |
| C# linq2db | `gen.csharp.runtime: linq2db` | SQL query methods on a borrowed `DataConnection` |
| JavaScript | `gen.javascript.runtime: ydb` | ESM queries and TypeScript declarations |
| Rust | `gen.rust.runtime: ydb` | async methods on a borrowed `QueryClient` |
| PHP | `gen.php.runtime: ydb` | typed query methods for the YDB SDK |
| Kotlin Query SDK | `gen.kotlin.runtime: ydb` | data classes and `Queries(SessionRetryContext)` |
| Kotlin JDBC | `gen.kotlin.runtime: jdbc` | typed query methods on a borrowed `Connection` |
| Kotlin Exposed | `gen.kotlin.runtime: exposed` | typed SQL methods on a borrowed `JdbcTransaction` |
| Java native SDK | `gen.java.runtime: ydb` | `Queries(SessionRetryContext)`, Java 17 records |
| Java JDBC | `gen.java.runtime: jdbc` | `Queries(Connection)`, named YDB prepared statements |
| Java Spring JDBC | `gen.java.runtime: spring` | `Queries(JdbcTemplate)`, framework-owned connections |
| Java Hibernate | `gen.java.runtime: hibernate` | `Queries(Session)`, JDBC work inside the session |

`Utf8` is text (`string` / `str`); `String` is binary (`[]byte` / `bytes`).
Optional values preserve nullability. Go integers retain their widths and signedness;
Python integers bind using the resolved YQL type. Query results reflect actual
selected columns, including aliases. Unsupported type/runtime combinations are
errors; no generic `Any` fallback is generated.

For `:one`, Go returns a row and error; Python returns a row or `None` when no
row exists. `:many` returns a Go slice or Python iterable. `:exec` returns only
execution status. The selected YDB SDK/driver APIs do not expose a portable
affected-row count, so all generators reject `:execrows`.

C++ and Java `:one` results are optional; C# throws `InvalidOperationException`
when no row exists. All
return the first row when present. `:many` returns a typed collection. Java
represents `Uint64` as the full 64-bit `long` bit pattern; use
`Long.toUnsignedString` for unsigned decimal formatting. C++ uses `uint64_t`
and C# uses `ulong`. Binary YQL `String` stays binary in every target.

Type coverage differs by target. Unsupported temporal, decimal, container or
other unmapped types fail explicitly; see the individual target docs.
Spring, Hibernate, Dapper and linq2db integrations generate SQL query
projections and methods; they do not infer ORM entities or LINQ expressions
from query results. See the [C#](csharp.md), [JavaScript](javascript.md),
[Rust](rust.md) and [PHP](php.md) contracts for supported types and API details.

Generated code uses caller-provided clients/connections. The caller controls
connection lifetime and credentials. Transaction behavior is target-specific:
both C++ profiles and native Java execute a transaction per method; C#, JDBC,
Spring, and Hibernate use the caller's connection or transaction. Generated
DB-API code closes its own cursors and does not commit caller-owned transactions.
The language-specific pages document the remaining runtime ownership contracts.

Python row decoding follows the selected runtime: native YDB rows are indexed by
column name, DB-API rows by position, and SQLAlchemy rows through `row._mapping`.
Custom wrappers must provide that same contract. Missing result sets or columns
raise errors; they are not converted into empty results or tried as other row shapes.

Python names support Unicode letters. Names that normalize to invalid Python
identifiers (for example, a leading digit) are rejected, as are model names that
conflict with Python keywords or the imported `Optional` type. Parameter names
such as `ydb`, `models`, and `text` do not shadow the generated runtime imports.

The verified `ydb-sqlalchemy` 0.1.22 has no asynchronous dialect. Requests for
`emit_async_querier: true` are explicitly rejected. Async Python adapters,
Pydantic, custom naming/type overrides and additional framework profiles remain
subsequent work.

Verified runtime versions: Go SDK 3.151.1; Python SDK 3.29.7, ydb-dbapi 0.1.23,
ydb-sqlalchemy 0.1.22, SQLAlchemy 2.0.52. Live tests cover both Go adapters and
all three Python adapters on YDB 26.3.1.8, including high Uint64, binary/text,
optional values and query cardinalities. Native Go supports scalar list
parameters, including optional elements and empty lists with an explicit element
type. Extended temporal list elements (`Date32`, `Datetime64`, `Timestamp64`,
`Interval64`) are rejected because the pinned SDK lacks their list-builder methods.
`Optional<List>`, nested lists, and list elements with more than one `Optional`
wrapper remain errors. database/sql rejects list parameters and results. Native Go also
supports scalar list results; complex container and temporal boundary coverage
remains incomplete.

Both Go adapters map `Decimal(P,S)` to `types.Decimal` and `Uuid` to
`uuid.UUID`; optional values use pointers. Decimal carriers must have the
precision and scale declared in the SQL model. A mismatch returns an error
before execution, including for Decimal list elements. The generator does not
reinterpret bytes using a different scale. database/sql uses typed YDB values
for Decimal/UUID parameters rather than generic driver conversion.

Native Go query methods and generated interfaces accept trailing
`opts ...query.ExecuteOption`. Options are forwarded without changing the
caller's slice. Generated typed parameters are applied last, so a caller option
cannot replace the bindings represented by the method arguments. Existing calls
without execution options continue to compile.

Kotlin types, nullable results and transaction ownership are documented in
[Kotlin](kotlin.md). Kotlin examples use the shared authors schema and queries.
