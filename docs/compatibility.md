# Compatibility contract

Compatibility is tracked by individual CLI, configuration and generated API contracts. See the [release summary](../CHANGELOG.md#parity-with-upstream-sqlc) for a feature comparison. sqlc-ydb is independently versioned and does not designate a compatible upstream sqlc version.

## Intentional differences

- Only `engine: ydb` is supported.
- Engine plugins, codegen plugins, WASM/process execution and public plugin protocols are excluded. `plugins`, `engines`, and `codegen` fail with a migration message. A new generator must be implemented in this repository.
- `gen.python` selects a built-in generator. `runtime` chooses `ydb`, `dbapi`, or `sqlalchemy`. Go adds `sql_package: ydb` alongside `database/sql`.
- `gen.cpp` selects `runtime: ydb|userver`; `gen.java` selects `runtime: ydb|jdbc|jooq`. `native` is an alias for `ydb` in these two targets. `gen.csharp.runtime` selects `adonet` (the default) or `dapper`; both use the YDB ADO.NET provider.
- `gen.kotlin` selects `runtime: ydb|jdbc|exposed`; `native` aliases `ydb`. Exposed uses SQL query methods inside a caller-owned JDBC transaction, not ORM table definitions or DSL translation. See [Kotlin](kotlin.md).
- `gen.typescript`, `gen.rust`, and `gen.php` use `runtime: ydb` (the default). They generate code for the official YDB SDKs.

## Implemented workflow

- `generate`, `compile`, `diff`, `init`, `version`, `--help`, `-f` / `--file`.
- `version` prints the version and optionally a newer-stable-release notification; `version --verbose` also prints the commit embedded by release builds. Ordinary source builds report `unknown` unless the commit is supplied through linker flags. The update check times out after two seconds and hides failures. Use `--no-remote` for version output without a network check.
- `self-update` replaces the running executable with the verified latest stable release, resolving symlinks and preserving their paths. Download and verification failures preserve the installed binary. See [installation](installation.md#update-the-installed-executable).
- `sqlc.yaml`, `sqlc.yml`, and `sqlc.json`; configuration version 2. Paths resolve relative to the configuration.
- File paths, lists, nonrecursive directories, and ordinary glob patterns. Explicit list order is retained; directory entries and glob matches use lexical order. Hidden files and `*.down.sql` are excluded.
- Schema rollback sections for goose, sql-migrate, tern and dbmate are excluded. Migration markers inside string literals are preserved.
- Schema migrations update an in-memory catalog in input order. Supported DDL: `CREATE TABLE [IF NOT EXISTS]`, `DROP TABLE [IF EXISTS]`, and `ALTER TABLE` with `ADD [COLUMN]`, `DROP [COLUMN]`, or table `RENAME TO`.
- Query annotations `-- name: QueryName :one|:many|:exec`. `:execrows` is parsed but generation rejects it: the selected YDB APIs cannot provide its required affected-row count.
- Go options `package`, `out`, `sql_package`, `emit_json_tags`, `emit_interface`, `emit_empty_slices`. The default `sql_package` is `database/sql`, matching sqlc.
- Python options `out`, `runtime`, `emit_sync_querier`, `emit_async_querier`. Synchronous generation defaults to enabled; requesting asynchronous generation currently fails explicitly. The Python package directory is selected by `out`; `gen.python.package` is unsupported.
- C++ and C# options `namespace`, `out`, `runtime`; Java and Kotlin options `package`, `out`, `runtime`; TypeScript and Rust options `out`, `runtime`; PHP options `namespace`, `out`, `runtime`. These are built-in extensions to the sqlc configuration shape, not external plugin options.
- Unknown configuration options produce errors. Generation never silently discards an option that has not been implemented.

## Differences from upstream sqlc

Only the commands and options above are implemented. In particular:

- No `analyze`, `parse`, `fmt`, `completion`, `createdb`, `push`, `verify` or `vet` commands, database-assisted analysis, or cloud/remote workflow.
- No `sqlc.arg`, `sqlc.narg`, `sqlc.embed` or `sqlc.slice` macros, type/name overrides, driver batch APIs, COPY helpers or command-tag results.
- `--no-remote` skips the optional release check for `version`; analysis and generation always run locally. It conflicts with `self-update`. `--remote` and upstream's `--no-database` are unsupported. `init` creates a version 2 configuration (`--v2` is also accepted); `version --verbose` and `self-update` are sqlc-ydb extensions.
- SQL parameters use YQL `$name` syntax. Driver-specific placeholder rewriting happens during generation; `$1`, `?` and `@name` are not accepted as an alternative input dialect.

These are explicit errors, not successful no-ops. The comparison uses upstream's [CLI](https://docs.sqlc.dev/en/latest/reference/cli.html), [configuration](https://docs.sqlc.dev/en/latest/reference/config.html), [query annotations](https://docs.sqlc.dev/en/latest/reference/query-annotations.html) and [macros](https://docs.sqlc.dev/en/latest/reference/macros.html).

Other database engines and external plugins are intentionally excluded. ORM entity/CRUD generation for Hibernate, Spring JPA and linq2db is also excluded: a query projection does not define an entity lifecycle. TypeScript is the supported Node.js target; JavaScript output is not provided.

Query analysis and runtime type support are separate: a resolved type that a selected adapter cannot bind or decode is a generation error. Check the [target reference](targets.md) before choosing a runtime.

## Output ownership

Use separate output directories for independently invoked configurations. Several generators in one configuration may share a directory if their filenames do not collide. Files written by sqlc-ydb carry a generated header and are overwritten by `generate`; handwritten files with other names are retained. `generate` and `diff` reject output paths that use the same path as both a file and a directory, before writing any files.

If a query file or model is renamed or removed, `generate` and `diff` report obsolete files with the sqlc-ydb header in the current output directories and exit with status 1. Remove the listed files and rerun. No output is written before this check passes. `diff` also reports missing or changed expected files.

This check covers regular files directly in directories the current generation writes. It does not follow unrelated symlinks, scan nested packages, or remember previously configured output directories. When changing `out` or removing a generator entirely, clean up its old directory yourself. `compile` does not inspect outputs. Files are never automatically deleted.

## Current analyzer coverage

The analyzer supports explicit `CREATE TABLE` catalogs and the schema migration operations listed above, table column projections and `*`, table/column aliases, supported joins and their optional sides, supported scalar/aggregate functions, `DECLARE`, direct comparison parameter inference, selected scalar local bindings, `INSERT`/`UPSERT ... VALUES`, `UPDATE ... SET`, `DELETE`, and `RETURNING`. It validates names outside the projection and conflicting parameter constraints. Diagnostics include source file, line and column. Table, column, alias and parameter names are case-sensitive, as in YQL.

Schema and declared `Decimal(P,S)` types require `1 <= P <= 35` and `0 <= S <= P`, including inside containers. Invalid values fail during analysis.

YDB serial columns are represented by their public integer value type while the catalog retains sequence-generation metadata. `SmallSerial` and `Serial2` map to `Int16`; `Serial` and `Serial4` map to `Int32`; `Serial8` and `BigSerial` map to `Int64`. A serial column must participate in the table's `PRIMARY KEY`. `INSERT` and `UPSERT` may omit it to allocate the next sequence value, or bind an explicit integer value without advancing that sequence. Avoid using a serial column as the primary key of a high-write table: monotonically increasing keys can create hot partitions. See the YDB [serial type documentation](https://ydb.tech/docs/en/yql/reference/types/serial).

Direct scalar literal projections retain their YQL types, including integer width/signedness, `Float` versus `Double`, and `String` versus `Utf8`. Integer suffixes and ranges follow the [YQL lexical rules](https://ydb.tech/docs/en/yql/reference/syntax/lexer). Non-column projections need an explicit `AS` name. A compound expression such as `$value = 1ul` is rejected instead of inheriting the parameter's type.

Scalar `SELECT` queries can omit `FROM`. String concatenation with `||` is supported for direct string literals and declared parameters of the same string family (`String` or `Utf8`); an optional operand makes the result optional. Mixed families, nested expressions and computed operands remain unsupported. The [booktest greeting](../examples/booktest/queries.sql) exercises this path.

CASE expressions with an explicit ELSE branch, the supported CAST matrix and nested calls to [supported built-ins](../internal/yql/builtins/README.md) have resolved result types. CASE/IF conditions and HAVING support the implemented comparison predicates; standalone comparison and arithmetic projections remain unsupported. NULL can participate in typed branches but cannot escape as an unresolved result.

COALESCE/NVL require matching base argument types; use explicit CASTs for mixed types. Core SUBSTRING accepts byte strings, while Unicode::Substring handles Utf8. Core SUBSTRING/FIND/RFIND positions accept unsigned integers up to Uint32, including optional values. Literal-dependent conversions outside this subset require an explicit CAST. See the built-in resolver's coverage notes for the separate library signatures.

UNION and UNION ALL reconcile columns by YQL result name, preserve server column ordering, reconcile supported common types and make missing columns optional. Direct-column GROUP BY and HAVING validate grouped references and supported aggregate calls. Aggregate result nullability accounts for empty global input versus nonempty groups and nullable arguments. Grouping expressions, windows and advanced grouping constructs remain unsupported.

Direct LIMIT/OFFSET parameters infer Uint64. Direct values in a column IN list infer the column's type; `column IN $values` and `NOT IN $values` infer `List<column type>`. An explicit List declaration is also accepted. In contrast, `column IN ($value)` contains a scalar parameter. These are compiler constraints, independent of a target's list binding support.

This remains a deliberately limited semantic implementation. General computed projections, the full CAST matrix, CTEs/subqueries, multiple result sets, FLATTEN, full function/type inference and the full YQL grammar semantics are subsequent work. Unary numeric expressions in projections or local assignments and backslash escapes in quoted identifiers are also explicitly rejected until their YQL semantics are implemented. `EXPLAIN` cannot be used as a named data query. Accepted syntax is not a claim of full equivalence to the YDB server's type checker.

Current INSERT/UPSERT VALUES and UPDATE SET values must be direct parameters; literal and computed assignments are explicitly rejected. Shared query-file declarations must currently be moved into each named query. These are temporary coverage limits, separate from the permanent decision to exclude plugins. Comments and whitespace may precede the first query annotation; comment-only query files are ignored.

## Schema migration coverage

Use a migration directory, glob or ordered file list as `schema`. The analyzer applies its supported Up statements to the catalog before analyzing any query. It never executes migrations or data statements against YDB. Dropping and recreating a table replaces its schema; renaming updates column ownership. Adding a column preserves its declared YQL type and nullability, and dropping a primary-key column fails. Table/column order stays deterministic.

An ALTER with several supported column actions is applied atomically to the in-memory catalog. This does not describe server transaction behavior. Missing objects, duplicate names, rename collisions and invalid primary keys produce source-located errors. Existing tables remain unchanged by a guarded CREATE. `RENAME TO` must currently be the only action in its ALTER statement. DDL inside action definitions and EXPLAIN statements is rejected, not applied to the catalog.

`ALTER COLUMN` changes to types/nullability/defaults and other ALTER actions such as indexes, changefeeds or table settings are currently rejected. External tables, views, table stores and CREATE TABLE AS are also outside this catalog's scope. Physical CREATE options that do not affect modeled columns are not represented in the catalog; this is not a full server DDL validator.

References: YDB [columns](https://ydb.tech/docs/en/yql/reference/syntax/alter_table/columns), [table rename](https://ydb.tech/docs/en/yql/reference/syntax/alter_table/rename), and [DROP TABLE](https://ydb.tech/docs/en/yql/reference/syntax/drop_table). Shared macros and database-assisted analysis remain planned; the supported workflow above does not include them. Implementation planning is maintained in [the contributor roadmap](../.agents/roadmap.md).
