# Built-in targets

| Target | Configuration | Generated API |
|---|---|---|
| Go native SDK | `gen.go.sql_package: ydb` | `New`, `Queries`, context-aware query methods |
| Go database/sql | `gen.go.sql_package: database/sql` | `New`, `DBTX`, `Queries`, `WithTx` |
| Python native SDK | `gen.python.runtime: ydb` | dataclasses and `Querier(QuerySessionPool)` |
| Python DB-API | `gen.python.runtime: dbapi` | dataclasses and a connection-based `Querier` |
| Python SQLAlchemy | `gen.python.runtime: sqlalchemy` | dataclasses and synchronous `Querier` |

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

Generated code uses caller-provided clients/connections. The caller controls
connection lifetime, credentials and transactions. Generated DB-API code closes
its own cursors and does not commit caller-owned transactions.

The verified `ydb-sqlalchemy` 0.1.22 has no asynchronous dialect. Requests for
`emit_async_querier: true` are explicitly rejected. Async Python adapters,
Pydantic, custom naming/type overrides and additional framework profiles remain
subsequent work.

Verified runtime versions: Go SDK 3.125.1; Python SDK 3.29.7, ydb-dbapi 0.1.23,
ydb-sqlalchemy 0.1.22, SQLAlchemy 2.0.52. Live tests exercised both Go adapters and
all three Python adapters on YDB 26.3.1.8, including high Uint64, binary/text,
optional values and query cardinalities. Native Go additionally supports a
List<Uint64> result; database/sql cannot scan this driver value and rejects List
results. Complex type and temporal boundary coverage remains incomplete.
