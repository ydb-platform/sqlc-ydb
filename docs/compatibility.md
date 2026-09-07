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
- `version` prints one version string; `version --verbose` also prints the
  commit embedded by release builds. Ordinary source builds report `unknown`
  unless the commit is supplied through linker flags.
- `sqlc.yaml`, `sqlc.yml`, and `sqlc.json`; config version 2 and the basic version 1
  Go `packages` format. Paths resolve relative to the configuration.
- File paths, lists, nonrecursive directories, and ordinary glob patterns.
  Explicit list order is retained; directory entries and glob matches use
  lexical order. Hidden files and `*.down.sql` are excluded.
- Schema rollback sections for goose, sql-migrate, tern and dbmate are excluded.
  Migration markers inside string literals are preserved.
- Schema migrations update an in-memory catalog in input order. Supported DDL:
  `CREATE TABLE [IF NOT EXISTS]`, `DROP TABLE [IF EXISTS]`, and `ALTER TABLE`
  with `ADD [COLUMN]`, `DROP [COLUMN]`, or table `RENAME TO`.
- Query annotations `-- name: QueryName :one|:many|:exec`. `:execrows` is parsed
  but generation rejects it: the selected YDB APIs cannot provide its required
  affected-row count.
- Go options `package`, `out`, `sql_package`, `emit_json_tags`, `emit_interface`,
  `emit_empty_slices`. The default `sql_package` is `database/sql`, matching sqlc.
- Python options `out`, `runtime`, `emit_sync_querier`,
  `emit_async_querier`. Synchronous generation defaults to enabled; requesting
  asynchronous generation currently fails explicitly.
  The Python package directory is selected by `out`; remove `gen.python.package`
  from older configurations. That option was ignored and now produces an error.
- C++ options `namespace`, `out`, `runtime`; C# options `namespace`, `out`;
  Java options `package`, `out`, `runtime`. These are built-in extensions to
  the sqlc configuration shape, not external plugin options.
- Unknown configuration options produce errors. Generation never silently
  discards an option that has not been implemented.

## Remaining compatibility work

This development version does not claim complete sqlc compatibility. Type/name
overrides, the full generator option inventory, sqlc macros, batch commands,
`vet`, `verify`, cloud/remote workflows and live database-assisted analysis still
need implementation. They are not successful no-op commands.

Query and type coverage evolves independently of configuration compatibility.
Unsupported YQL must be diagnosed by the analyzer; a resolved type unsupported by
a language adapter is a generation error. Each supported behavior needs a
fixture and, for runtime-sensitive behavior, an execution test.

## Output ownership

Use separate output directories for independently invoked configurations. Several
generators in one configuration may share a directory if their filenames do not
collide. Files written by sqlc-ydb carry a generated header and are overwritten
by `generate`; handwritten files with other names are retained.

If a query file or model is renamed or removed, `generate` and `diff` report
obsolete files with the sqlc-ydb header in the current output directories and
exit with status 1. Remove the listed files and rerun. No output is written
before this check passes. `diff` also reports missing or changed expected files.

This check covers regular files directly in directories the current generation
writes. It does not follow unrelated symlinks, scan nested packages, or remember
previously configured output directories. When changing `out` or removing a
generator entirely, clean up its old directory yourself. `compile` does not
inspect outputs. Files are never automatically deleted.

## Current analyzer coverage

The analyzer supports explicit `CREATE TABLE` catalogs and the schema migration
operations listed below, table column
projections and `*`, table/column aliases, supported joins and their optional
sides, `COUNT`, `DECLARE`, direct comparison parameter inference, selected scalar
local bindings, `INSERT`/`UPSERT ... VALUES`, `UPDATE ... SET`, `DELETE`, and
`RETURNING`. It validates names outside the projection and conflicting parameter
constraints. Diagnostics include source file, line and column. Table, column,
alias and parameter names are case-sensitive, as in YQL.

Direct scalar literal projections retain their YQL types, including integer
width/signedness, `Float` versus `Double`, and `String` versus `Utf8`. Integer
suffixes and ranges follow the [YQL lexical rules](https://ydb.tech/docs/en/yql/reference/syntax/lexer).
Non-column projections need an explicit `AS` name. A compound expression such
as `$value = 1ul` is rejected instead of inheriting the parameter's type.

Scalar `SELECT` queries can omit `FROM`. String concatenation with `||` is
supported for direct string literals and declared parameters of the same string
family (`String` or `Utf8`); an optional operand makes the result optional.
Mixed families, nested expressions and computed operands remain unsupported.
The [booktest greeting](../examples/booktest/queries.sql) exercises this path.

This is a deliberately limited first semantic implementation. General computed
projections and casts, CTEs/subqueries,
multiple result sets, FLATTEN, full function/type inference and the full YQL
grammar semantics are subsequent work. Unary numeric expressions in projections
or local assignments and backslash escapes in quoted identifiers are also
explicitly rejected until their YQL semantics are implemented. `EXPLAIN` cannot
be used as a named data query. Accepted syntax is not a claim of full
equivalence to the YDB server's type checker.

Current INSERT/UPSERT VALUES and UPDATE SET values must be direct parameters;
literal and computed assignments are explicitly rejected. Shared query-file
declarations must currently be moved into each named query. These are temporary
coverage limits, separate from the permanent decision to exclude plugins.

## Schema migration coverage

Use a migration directory, glob or ordered file list as `schema`. The analyzer
applies its supported Up statements to the catalog before analyzing any query.
It never executes migrations or data statements against YDB. Dropping and
recreating a table replaces its schema; renaming updates column ownership.
Adding a column preserves its declared YQL type and nullability, and dropping a
primary-key column fails. Table/column order stays deterministic.

An ALTER with several supported column actions is applied atomically to the
in-memory catalog. This does not describe server transaction behavior. Missing
objects, duplicate names, rename collisions and invalid primary keys produce
source-located errors. Existing tables remain unchanged by a guarded CREATE.
`RENAME TO` must currently be the only action in its ALTER statement. DDL inside
action definitions and EXPLAIN statements is rejected, not applied to the catalog.

`ALTER COLUMN` changes to types/nullability/defaults and other ALTER actions
such as indexes, changefeeds or table settings are currently rejected. External
tables, views, table stores and CREATE TABLE AS are also outside this catalog's
scope. Physical CREATE options that do not affect modeled columns are not
represented in the catalog; this is not a full server DDL validator.

References: YDB [columns](https://ydb.tech/docs/en/yql/reference/syntax/alter_table/columns),
[table rename](https://ydb.tech/docs/en/yql/reference/syntax/alter_table/rename),
and [DROP TABLE](https://ydb.tech/docs/en/yql/reference/syntax/drop_table).
YQL main at `d62403dadf7588c33d2d0a61296a157b61163d52` explicitly handles
[`DROP TABLE IF EXISTS` through `missingOk`](https://github.com/ydb-platform/ydb/blob/d62403dadf7588c33d2d0a61296a157b61163d52/yql/essentials/sql/v1/translation/sql_query.cpp#L575)
and [rejects combining RENAME TO with other ALTER actions](https://github.com/ydb-platform/ydb/blob/d62403dadf7588c33d2d0a61296a157b61163d52/yql/essentials/sql/v1/translation/sql_query.cpp#L2399).
See [the roadmap](roadmap.md) for shared macros and deferred database-assisted
analysis.
