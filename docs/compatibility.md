# Compatibility contract

Reference workflow: [sqlc v1.31.1](https://docs.sqlc.dev/en/v1.31.1/reference/config.html).
Compatibility is tracked at the user interface and generated API level; sqlc
internal data structures and source history are not a dependency.

## Intentional differences

- Only `engine: ydb` is supported.
- Engine plugins, codegen plugins, WASM/process execution and public plugin
  protocols are excluded. `plugins`, `engines`, and `codegen` fail with a migration
  message. A new generator must be implemented in this repository.
- `gen.python` selects a built-in generator. `runtime` chooses `ydb`, `dbapi`, or
  `sqlalchemy`. Go adds `sql_package: ydb` alongside `database/sql`.
- `gen.cpp` selects `runtime: ydb|userver`; `gen.java` selects
  `runtime: ydb|jdbc|spring|hibernate`. `native` is an alias for `ydb` in these
  two targets. `gen.csharp` uses the modern YDB ADO.NET SDK without a runtime
  selector: the SDK's native API is already ADO.NET.
- No intermediate AST. ANTLR parse contexts feed semantic analysis directly.

## Implemented workflow

- `generate`, `compile`, `diff`, `init`, `version`, `--help`, `-f` / `--file`.
- `sqlc.yaml`, `sqlc.yml`, and `sqlc.json`; config version 2 and the basic version 1
  Go `packages` format. Paths resolve relative to the configuration.
- File paths, lists, nonrecursive directories, and ordinary glob patterns.
  Explicit list order is retained; directory entries and glob matches use
  lexical order. Hidden files and `*.down.sql` are excluded.
- Schema rollback sections for goose, sql-migrate, tern and dbmate are excluded.
  Migration markers inside string literals are preserved.
- Query annotations `-- name: QueryName :one|:many|:exec`. `:execrows` is parsed
  but generation rejects it: the selected YDB APIs cannot provide its required
  affected-row count.
- Go options `package`, `out`, `sql_package`, `emit_json_tags`, `emit_interface`,
  `emit_empty_slices`. The default `sql_package` is `database/sql`, matching sqlc.
- Python options `package`, `out`, `runtime`, `emit_sync_querier`,
  `emit_async_querier`. Synchronous generation defaults to enabled; requesting
  asynchronous generation currently fails explicitly.
- C++ options `namespace`, `out`, `runtime`; C# options `namespace`, `out`;
  Java options `package`, `out`, `runtime`. These are built-in extensions to
  the sqlc configuration shape, not external plugin options.
- Unknown configuration options produce errors. Generation never silently
  discards an option that has not been implemented.

## Remaining compatibility work

This development version does not claim complete sqlc compatibility. Type/name
overrides, the full generator option inventory, sqlc macros, batch commands,
`vet`, `verify`, cloud/remote workflows and live database-assisted analysis still
need implementation. They are not successful no-op commands. Generated files no
longer produced by a query set are not automatically deleted.

Query and type coverage evolves independently of configuration compatibility.
Unsupported YQL must be diagnosed by the analyzer; a resolved type unsupported by
a language adapter is a generation error. Each supported behavior needs a
fixture and, for runtime-sensitive behavior, an execution test.

## Current analyzer coverage

The initial analyzer supports explicit `CREATE TABLE` catalogs, table column
projections and `*`, table/column aliases, supported joins and their optional
sides, `COUNT`, `DECLARE`, direct comparison parameter inference, selected scalar
local bindings, `INSERT`/`UPSERT ... VALUES`, `UPDATE ... SET`, `DELETE`, and
`RETURNING`. It validates names outside the projection and conflicting parameter
constraints. Diagnostics include source file, line and column.

This is a deliberately limited first semantic implementation. Schema evolution
through ALTER/DROP, general computed projections and casts, CTEs/subqueries,
multiple result sets, FLATTEN, full function/type inference and the full YQL
grammar semantics are subsequent work. Accepted syntax is not a claim of full
equivalence to the YDB server's type checker.

Current INSERT/UPSERT VALUES and UPDATE SET values must be direct parameters;
literal and computed assignments are explicitly rejected. Shared query-file
declarations must currently be moved into each named query. These are temporary
coverage limits, separate from the permanent decision to exclude plugins.
