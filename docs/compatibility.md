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
- `init` supports `--language`, `--runtime`, `--all-options`, and generator-specific help. See the [generator option reference](targets.md#generator-options).
- `version` prints the version and optionally a newer-stable-release notification; `version --verbose` also prints the commit embedded by release builds. Ordinary source builds report `unknown` unless the commit is supplied through linker flags. The update check times out after two seconds and hides failures. Use `--no-remote` for version output without a network check.
- `version --upgrade` replaces the running executable with the verified latest stable release, resolving symlinks and preserving their paths. Download and verification failures preserve the installed binary. See [installation](installation.md#update-the-installed-executable). Automatic in-place upgrades are supported on Linux and macOS. On Windows, `version --upgrade` prints manual upgrade instructions.
- `sqlc.yaml`, `sqlc.yml`, and `sqlc.json`; configuration version 2. Paths resolve relative to the configuration.
- Concrete offline function signatures can be declared per query set under `sql[].analyzer.functions`; see [function signatures](functions.md). They are scoped to that `sql` entry and shared by every selected generator.
- Optional [database-assisted analysis](database-analysis.md) discovers referenced table schemas, checks supplied schemas for drift and compiles queries on YDB without executing them. `--no-database` forces offline analysis; local schema inputs are then required.
- File paths, lists, nonrecursive directories, and ordinary glob patterns. Explicit list order is retained; directory entries and glob matches use lexical order. Hidden files and `*.down.sql` are excluded.
- Schema rollback sections for goose, sql-migrate, tern and dbmate are excluded. Migration markers inside string literals are preserved.
- Schema migrations update an in-memory catalog in input order. Supported DDL: `CREATE TABLE [IF NOT EXISTS]`, `DROP TABLE [IF EXISTS]`, and `ALTER TABLE` with `ADD [COLUMN]`, `DROP [COLUMN]`, `ADD INDEX`, `DROP INDEX`, or table `RENAME TO`. Ordinary and covering global secondary indexes are retained in the catalog.
- Query annotations `-- name: QueryName :one|:many|:exec`, plus Go-only [`:each` streaming callbacks](streaming.md) for SELECT. `:execrows` is parsed but generation rejects it: the selected YDB APIs cannot provide its required affected-row count.
- The Python package directory is selected by `out`; `gen.python.package` is unsupported. Go's default `sql_package` is `database/sql`, matching upstream sqlc.
- Unknown configuration options produce errors. Generation never silently discards an option that has not been implemented.

## Differences from upstream sqlc

Only the commands and options above are implemented. In particular:

- No `analyze`, `parse`, `fmt`, `completion`, `createdb`, `push`, `verify` or `vet` commands or cloud/remote workflow.
- No `sqlc.arg`, `sqlc.narg`, `sqlc.embed` or `sqlc.slice` macros, type/name overrides, driver batch APIs, COPY helpers or command-tag results.
- `--no-remote` skips the optional release check for `version`; analysis and generation run locally and may use the explicitly configured YDB connection. It conflicts with `version --upgrade`. `--remote` is unsupported; `--no-database` disables database-assisted analysis. `init` creates a version 2 configuration (`--v2` is also accepted); `version --verbose` and `version --upgrade` are sqlc-ydb extensions.
- SQL parameters use YQL `$name` syntax. Driver-specific placeholder rewriting happens during generation; `$1`, `?` and `@name` are not accepted as an alternative input dialect.

These are explicit errors, not successful no-ops. The comparison uses upstream's [CLI](https://docs.sqlc.dev/en/latest/reference/cli.html), [configuration](https://docs.sqlc.dev/en/latest/reference/config.html), [query annotations](https://docs.sqlc.dev/en/latest/reference/query-annotations.html) and [macros](https://docs.sqlc.dev/en/latest/reference/macros.html).

Other database engines and external plugins are intentionally excluded. ORM entity/CRUD generation for Hibernate, Spring JPA and linq2db is also excluded: a query projection does not define an entity lifecycle. TypeScript is the supported Node.js target; JavaScript output is not provided.

Query analysis and runtime type support are separate: a resolved type that a selected adapter cannot bind or decode is a generation error. Check the [target reference](targets.md) before choosing a runtime.

## Output ownership

Use separate output directories for independently invoked configurations. Several generators in one configuration may share a directory if their filenames do not collide. Files written by sqlc-ydb carry a generated header and are overwritten by `generate`; handwritten files with other names are retained. `generate` and `diff` reject output paths that use the same path as both a file and a directory, before writing any files.

If a query file or model is renamed or removed, `generate` and `diff` report obsolete files with the sqlc-ydb header in the current output directories and exit with status 1. Remove the listed files and rerun. No output is written before this check passes. `diff` also reports missing or changed expected files.

This check covers regular files directly in directories the current generation writes. It does not follow unrelated symlinks, scan nested packages, or remember previously configured output directories. When changing `out` or removing a generator entirely, clean up its old directory yourself. `compile` does not inspect outputs. Files are never automatically deleted.

## Current analyzer coverage

The analyzer supports explicit `CREATE TABLE` catalogs and the schema migration operations listed above, table column and wildcard projections, table/column aliases, supported joins and their optional sides, supported scalar/aggregate functions, `DECLARE`, direct comparison parameter inference, scalar local bindings, `INSERT`/`UPSERT ... VALUES`, typed `INSERT`/`UPSERT ... SELECT`, `UPDATE ... SET`, `UPDATE ... ON SELECT`, `DELETE`, `DELETE ... ON SELECT`, and `RETURNING`. SELECT expression, source, predicate and grouping analysis is shared between read queries and DML SELECT sources. It validates names outside the projection and conflicting parameter constraints. Diagnostics include source file, line and column. Table, column, alias and parameter names are case-sensitive, as in YQL.

Supported `SELECT *`, `SELECT alias.*` and `RETURNING *` projections are expanded into explicit quoted columns once in shared analysis before code generation. Offline analysis uses the local schema catalog; connected analysis uses the same catalog when supplied, or discovers columns from YDB. The executable SQL and result metadata agree on column order. Adding unrelated database columns after generation leaves the selected result shape unchanged; removing or changing selected columns remains incompatible. Wildcard expansion preserves source text outside the replaced projection spans, including declarations, comments, string literals and `COUNT(*)`.

Schema and declared `Decimal(P,S)` types require `1 <= P <= 35` and `0 <= S <= P`, including inside containers. Invalid values fail during analysis.

`Bytes` is accepted as the canonical binary `String` type and `Text` as the canonical Unicode `Utf8` type in schema, declaration and nested type positions. Struct field identity is name-based rather than declaration-order-based. Struct type declarations accept a trailing comma after the final named field, including inside List and Optional types; empty fields and repeated commas remain errors.

Integer type aliases are canonicalized in schema columns, parameter declarations, nested types and CAST targets: `TinyInt` → `Int8`, `SmallInt` → `Int16`, `Int`/`Integer` → `Int32`, and `BigInt` → `Int64`. Type names are case-insensitive; generated bindings use the canonical width and signedness while executable SQL retains the original spelling. `Uint` is not a supported alias; use an explicit width such as `Uint32`.

YDB serial columns are represented by their public integer value type while the catalog retains sequence-generation metadata. `SmallSerial` and `Serial2` map to `Int16`; `Serial` and `Serial4` map to `Int32`; `Serial8` and `BigSerial` map to `Int64`. A serial column must participate in the table's `PRIMARY KEY`. `INSERT` and `UPSERT` may omit it to allocate the next sequence value, or bind an explicit integer value without advancing that sequence. Avoid using a serial column as the primary key of a high-write table: monotonically increasing keys can create hot partitions. See the YDB [serial type documentation](https://ydb.tech/docs/en/yql/reference/types/serial).

Direct scalar literal projections retain their YQL types, including integer width/signedness, `Float` versus `Double`, and `String` versus `Utf8`. Integer suffixes and ranges follow the [YQL lexical rules](https://ydb.tech/docs/en/yql/reference/syntax/lexer). Non-column projections need an explicit `AS` name when the consumer requires named results; explicit-target INSERT/UPSERT SELECT does not. A compound expression such as `$value = 1ul` is rejected instead of inheriting the parameter's type.

Declared root `Struct` parameters are accessed with `$record.member`; nested members use the same form (for example, `$record.address.city`). Member names are resolved by declaration name, including quoted names, independently of field order. Named query results require an alias, for example `SELECT $key.id AS id`. Struct values returned by configured functions can also be accessed by member name. An `Optional<Struct<...>>` base is rejected because optional-struct unwrapping is not implemented. Struct members may be scalar or optional scalar, but generated bindings currently reject nested Struct/List fields; this limitation is independent of offline member resolution.

Scalar `SELECT` queries can omit `FROM`. String concatenation with `||` is supported for direct string literals and declared parameters of the same string family (`String` or `Utf8`); an optional operand makes the result optional. Mixed families, nested expressions and computed operands remain unsupported. The [booktest greeting](../examples/booktest/queries.sql) exercises this path.

CASE expressions with an explicit ELSE branch, the supported CAST matrix and nested calls to [supported built-ins](../internal/yql/builtins/README.md) have resolved result types. CASE/IF conditions and HAVING support the implemented comparison predicates; standalone comparison projections remain unsupported. Binary numeric `+`, `-`, and `*` and parentheses around supported expressions use the same resolver in projections, predicates and DML values. NULL can participate in typed branches and can use an optional DML destination as its contextual type, but cannot otherwise escape as an unresolved result.

The shipped function catalog is bounded. `sql[].analyzer.functions` adds exact concrete signatures using supported model types, including named and omittable arguments and declared AutoMap propagation, without loading or executing code. Polymorphic and resource-valued signatures remain unsupported. Unknown functions remain errors; configured contracts do not prove that a UDF is installed on the destination server and do not imply coverage of the complete YDB UDF registry.

COALESCE/NVL require matching base argument types; use explicit CASTs for mixed types. Core SUBSTRING accepts byte strings, while Unicode::Substring handles Utf8. Core SUBSTRING/FIND/RFIND positions accept unsigned integers up to Uint32, including optional values. Literal-dependent conversions outside this subset require an explicit CAST. See the built-in resolver's coverage notes for the separate library signatures.

UNION and UNION ALL reconcile columns by YQL result name, preserve server column ordering, reconcile supported common types and make missing columns optional. Direct-column GROUP BY and HAVING validate grouped references and supported aggregate calls. Aggregate result nullability accounts for empty global input versus nonempty groups and nullable arguments. Grouping expressions, windows and advanced grouping constructs remain unsupported.

LIMIT/OFFSET retain an explicitly declared parameter's type. Supported types are `Int8`, `Int16`, `Int32`, `Uint8`, `Uint16`, `Uint32`, `Uint64` and their optional forms. YDB rejects bound or computed `Int64` counts, including the `BigInt` alias; floating-point, Boolean and string counts are also rejected. Nonnegative `Int64` literals such as `LIMIT 1l` are accepted without converting them to parameters. Local bindings that retain only an `Int64` type, such as `$n = 1l; ... LIMIT $n`, remain outside offline analysis even though YDB accepts them.

An unresolved direct LIMIT/OFFSET parameter defaults to `Uint64`. Add DECLARE when a parameter first defaults in one UNION arm but needs a narrower type in a later arm; the analyzer does not revisit that earlier default. The analyzer validates count expressions separately from row expressions; it does not add an unsigned cast to the executable SQL or reinterpret signed SDK arguments.

Negative signed arguments and NULL are passed to YDB unchanged. NULL LIMIT leaves the result unbounded, and NULL OFFSET skips no rows. Negative counts follow the server's expression semantics: with an Int32 LIMIT of -1 and a Uint32 OFFSET, pinned-server tests returned all available rows at offset zero but no rows at offset one. A signed OFFSET parameter of -1 also returned no rows. Direct unary literals such as `LIMIT -1` remain unsupported by the expression resolver; this is an analyzer restriction, not a YDB rejection. Applications that require nonnegative pagination must enforce that input contract themselves. Maximum Uint64 counts are accepted; out-of-range YQL literals are errors. See the [pagination example](../examples/authors/README.md), [LIMIT/OFFSET syntax](https://ydb.tech/docs/en/yql/reference/syntax/select/limit_offset), and the dated [server evidence](../.agents/yql-evidence.md).

Direct values in a column IN list infer the column's type; `column IN $values` and `NOT IN $values` infer `List<column type>`. An explicit List declaration is also accepted. In contrast, `column IN ($value)` contains a scalar parameter. A parenthesized direct List parameter such as `column IN ($values)` is rejected with an actionable diagnostic; use the unparenthesized `IN $values` form. These are compiler constraints, independent of a target's list binding support.

Predicate comparison uses the supported common-type resolver. Integer kinds reconcile by width and signedness, and Float/Double combinations reconcile within the primitive numeric family. Compared with v0.1.5, `WHERE`, `JOIN ... ON`, `UPDATE ... WHERE` and `DELETE ... WHERE` now undergo operand type validation, so previously generated queries can be rejected. In particular, YDB accepts `String`/`Utf8` and `Decimal(P,S)`/integer comparisons, but the offline resolver currently rejects these mixed pairs. Use explicit CASTs to give the operands a common type. This is an offline-analysis limitation, not a restriction of YQL; the analyzer does not infer broader server-side coercions.

This remains a deliberately limited semantic implementation. General computed projections, the full CAST matrix, CTEs/subqueries, multiple result sets, FLATTEN, full function/type inference and the full YQL grammar semantics are subsequent work. Unary numeric expressions in projections or local assignments and backslash escapes in quoted identifiers are also explicitly rejected until their YQL semantics are implemented. `EXPLAIN` cannot be used as a named data query. Accepted syntax is not a claim of full equivalence to the YDB server's type checker.

INSERT/UPSERT VALUES and individual UPDATE SET assignments accept direct parameters, literals, contextual NULL, parentheses around supported expressions, supported comparisons, scalar functions and CAST/CASE expressions. UPDATE expressions can read the target row's columns, so `SET value = value + $delta` executes the calculation in YDB. VALUES expressions cannot read the destination table. Boolean assignments such as `SET enabled = (value > 1)` are supported; nullable comparison results require an optional Bool destination. Duplicate SET targets are rejected. Logical AND/OR/NOT combinations and IN predicates in assignments remain unsupported. Binary `+`, `-`, and `*` support primitive integer, Float and Double operands; mixed numeric types use YQL's common numeric type, and an optional operand makes the result optional. Arithmetic preserves server overflow behavior; generated code does not calculate, saturate or split an assignment into a read and a write. Division, remainder, unary numeric operators, Decimal/temporal arithmetic, unresolved NULL operands, aggregates, windows, tuples and subqueries remain unsupported in these assignments.

Computed values must match the destination type, optionally lifting a required value into an optional destination. NULL is accepted only for an optional destination. All these DML assignment forms, including INSERT/UPSERT SELECT and UPDATE/DELETE ON SELECT, share the same compatibility check. Lossless integer widening is also accepted: same-signedness widening and unsigned-to-signed conversion into a strictly wider type. Narrowing, signed-to-unsigned and integer/float assignment conversions require an explicit compatible CAST; a CAST whose result can be NULL still requires an optional destination. Value-dependent literal narrowing is not inferred. Direct parameter assignments retain destination-based inference and its existing type constraints, including when parenthesized. Parameters inside computed expressions must already have a resolved type, normally through DECLARE; the destination type is not propagated through arithmetic.

```sql
-- name: IncrementCounter :one
DECLARE $id AS Utf8;
DECLARE $delta AS Int64;
UPDATE counters SET value = value + $delta WHERE id = $id
RETURNING value;
```

The [computed DML fixture](../internal/endtoend/testdata/computed_dml/queries.sql) covers constants, counters, nullable fields and UPSERT arithmetic. These operations work with the existing generated transaction APIs: callers own transaction boundaries and retry policy. The jOOQ DSL does not translate arithmetic; use explicitly declared queries (its typed JDBC path), `runtime: jdbc`, or `runtime: ydb`.

INSERT/UPSERT SELECT supports two mapping contracts. With an explicit target column list, explicit source expressions match the targets positionally; source aliases are optional and do not retarget values by name, and wildcard source projections remain unsupported. Without a target column list, source result names select the destination columns; supported expressions need aliases, while column projections keep their names. This named form accepts `*`, `alias.*` and computed columns combined with wildcards. Duplicate names, unknown destination columns and incompatible types fail analysis. Both forms reject missing required NOT NULL columns and missing non-generated primary-key columns before generation. Omitted generated serial keys use the sequence; other primary-key columns must be supplied. UPSERT preserves omitted nullable columns on an existing row, but all NOT NULL columns without generated defaults must still be supplied, even for an empty source or when the row already exists. UPDATE/DELETE ON SELECT instead match source result names to target columns and require every primary-key column with a compatible declared type. Shared query-file declarations must currently be moved into each named query. These are temporary coverage limits, separate from the permanent decision to exclude plugins. Comments and whitespace may precede the first query annotation; comment-only query files are ignored.

## Table path resolution

Named queries and schema sources accept `PRAGMA TablePathPrefix("/database/folder");` with a nonempty absolute string literal. Relative table references then resolve under that prefix; an absolute table reference bypasses it. Include the database name in the prefix. Relative and empty prefix values are outside the compiler's current coverage and are rejected with an explicit-path hint. Parameterized or computed prefix values and other pragmas remain unsupported.

```sql
-- name: GetStagingUser :one
PRAGMA TablePathPrefix("/database/staging");
DECLARE $id AS Uint64;
SELECT u.id, u.name
FROM users AS u
WHERE u.id = $id;
```

The offline catalog must describe the same resolved table path: use this prefix in its schema source or declare the table as `/database/staging/users`. Each named query and each schema source has its own prefix scope. Put the pragma before local bindings and data or schema statements, and repeat it in every source that needs it. Identical repeated prefixes are accepted; prefix changes within a named query or schema source are not supported by the compiler. Query-file preambles before `-- name:` remain unsupported.

Path resolution is shared by SELECT sources, joins, secondary-index VIEW selection, supported writes and schema migrations. Tables with the same basename in different directories remain distinct catalog entries. Aliases, column names, strings and comments retain their original meaning. Executable SQL retains the pragma and authored table references; wildcard expansion continues to replace only the projection spans. The jOOQ target renders its resolved table identities through the dialect, preserving the pragma's execution context and table mappings.

The prefix is static SQL, not a generated method argument or a runtime environment-variable substitution. To select a different environment, prepare matching schema/query inputs with the intended absolute prefix and regenerate. The [namespaces example](../examples/namespaces) demonstrates two catalogs, indexed reads, writes and a join that bypasses the prefix with an absolute table path. Connected analysis uses the same resolved identities; see [database-assisted analysis](database-analysis.md).

## Schema migration coverage

Ordinary secondary indexes support `INDEX name GLOBAL [SYNC|ASYNC] ON (key_columns) [COVER (data_columns)]` in CREATE TABLE and `ALTER TABLE ... ADD INDEX`; omitting SYNC/ASYNC selects SYNC. `DROP INDEX` removes the named index from the catalog. `FROM table VIEW index` resolves the index against the underlying table, including quoted names, aliases, joins, wildcard projections and supported SELECT-backed DML. Column types and wildcard order come from the base table. Generated SQL preserves explicit index selection; an unknown index is an error. Vector, unique, local and other specialized index types and index settings are currently unsupported.

Synchronous indexes are updated transactionally with the table; asynchronous indexes are eventually consistent and require a Stale Read Only transaction for indexed reads. Generated methods do not choose a transaction mode or wait for an asynchronous index to catch up. The application owns that consistency choice. See YDB's [secondary-index contract](https://ydb.tech/docs/en/concepts/query_execution/secondary_indexes) and [VIEW syntax](https://ydb.tech/docs/en/yql/reference/syntax/select/secondary_index).

For native Go, pass `query.WithTxControl(query.StaleReadOnlyTxControl())` through the generated method's execute options when reading an asynchronous index. The [live index fixture](../internal/endtoend/indexes_live_test.go) verifies both the default-mode rejection and this explicit mode. The [authors example](../examples/authors/README.md) uses synchronous indexes and generates ordinary and covering reads for all runtime profiles.

Use a migration directory, glob or ordered file list as `schema`. The analyzer applies its supported Up statements to the catalog before analyzing any query. It never executes migrations or data statements against YDB. Dropping and recreating a table replaces its schema; renaming updates column ownership. Adding a column preserves its declared YQL type and nullability, and dropping a primary-key column fails. Table/column order stays deterministic.

An ALTER with several supported column or index actions is applied atomically to the in-memory catalog. This does not describe server transaction behavior. Missing objects, duplicate names, rename collisions and invalid primary keys produce source-located errors. Existing tables remain unchanged by a guarded CREATE. `RENAME TO` must currently be the only action in its ALTER statement. DDL inside action definitions and EXPLAIN statements is rejected, not applied to the catalog.

`ALTER COLUMN` changes to types/nullability/defaults and other ALTER actions such as index renames, changefeeds or table settings are currently rejected. External tables, schema views, table stores and CREATE TABLE AS are also outside this catalog's scope. Secondary-index `VIEW` selection is distinct from a schema view. Physical CREATE options that do not affect modeled columns are not represented in the catalog; this is not a full server DDL validator.

References: YDB [columns](https://ydb.tech/docs/en/yql/reference/syntax/alter_table/columns), [table rename](https://ydb.tech/docs/en/yql/reference/syntax/alter_table/rename), and [DROP TABLE](https://ydb.tech/docs/en/yql/reference/syntax/drop_table). Shared macros remain planned; database-assisted analysis is described in [its contract](database-analysis.md). Implementation planning is maintained in [the contributor roadmap](../.agents/roadmap.md).

### Structured batch parameters

`DECLARE $books AS List<Struct<...>>` can supply an `AS_TABLE($books)` source. The analyzer resolves Struct fields by name regardless of declaration order. INSERT/UPSERT SELECT checks explicit source expressions positionally when a target column list is present, or matches source result names when the target list is omitted. Named writes can expand qualified or unqualified wildcards; an explicit target list still requires an explicit source projection because AS_TABLE field order is not a positional contract. INSERT/UPSERT SELECT currently accepts one SELECT input; UNION inputs remain unsupported. UPDATE/DELETE ON SELECT can use qualified or unqualified wildcards because those statements match by result name. An AS_TABLE source in a join requires an explicit alias, as it does in YDB. The jOOQ runtime restricts structured-list inputs to its INSERT/UPSERT batch path; UPDATE/DELETE ON SELECT with these inputs are unsupported (see [jOOQ limits](java.md#jooq-prototype)). All nine language generators support list-of-struct parameters with fields supported by the selected runtime's scalar and optional-scalar mapping. Nested containers and nested structs remain unsupported by these bindings. Generated APIs expose named item types and preserve the declared type for empty lists. See the [batch example](../examples/batch/README.md).
