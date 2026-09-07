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
| C# ADO.NET | `gen.csharp` | async `Queries(YdbConnection)`, records, cancellation and transactions |
| Java native SDK | `gen.java.runtime: ydb` | `Queries(SessionRetryContext)`, Java 17 records |
| Java JDBC | `gen.java.runtime: jdbc` | `Queries(Connection)`, named YDB prepared statements |
| Java Spring JDBC | `gen.java.runtime: spring` | `Queries(JdbcTemplate)`, framework-owned connections |
| Java Hibernate | `gen.java.runtime: hibernate` | `Queries(Session)`, JDBC work inside the session |

Output references: the legacy YDB generators preserved at archive commit
`da046efe95d7ec65c13cd1f88a9f55804c322f73`, sqlc's Go generator, and
[sqlc-gen-python](https://github.com/sqlc-dev/sqlc-gen-python).
The previous implementations are references for API shape, not an authority for
YQL type semantics.

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

The C++/C#/Java generators initially cover scalar primitives and their optional
forms. Unsupported temporal, decimal, or container types fail explicitly; see
the individual [C++](cpp.md), [C#](csharp.md), and [Java](java.md) target docs.
Spring and Hibernate integrations generate query projections and methods;
they do not infer ORM entities from query results.

Generated code uses caller-provided clients/connections. The caller controls
connection lifetime and credentials. Transaction behavior is target-specific:
both C++ profiles and native Java execute a transaction per method; C#, JDBC,
Spring, and Hibernate use the caller's connection or transaction. Generated DB-API code closes
its own cursors and does not commit caller-owned transactions.

Python row decoding follows the selected runtime: native YDB rows are indexed by
column name, DB-API rows by position, and SQLAlchemy rows through `row._mapping`.
Custom wrappers must provide that same contract. Missing result sets or columns
raise errors; they are not converted into empty results or tried as other row shapes.

The verified `ydb-sqlalchemy` 0.1.22 has no asynchronous dialect. Requests for
`emit_async_querier: true` are explicitly rejected. Async Python adapters,
Pydantic, custom naming/type overrides and additional framework profiles remain
subsequent work.

Verified runtime versions: Go SDK 3.151.1; Python SDK 3.29.7, ydb-dbapi 0.1.23,
ydb-sqlalchemy 0.1.22, SQLAlchemy 2.0.52. Live tests cover both Go adapters and
all three Python adapters on YDB 26.3.1.8, including high Uint64, binary/text,
optional values and query cardinalities. Native Go additionally supports a
List<Uint64> result; database/sql cannot scan this driver value and rejects List
results. Complex type and temporal boundary coverage remains incomplete.
