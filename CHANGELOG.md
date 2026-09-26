# Changelog

## Unreleased

### Added

- Resolve YQL `SELECT * WITHOUT` exclusions before wildcard expansion, including qualified JOIN columns and `IF EXISTS`, across generated runtimes.

## v0.3.0

### Added

- Support `sqlc.embed` for physical table projections with nested result models across generated runtimes.

## v0.2.13

### Added

- Infer direct `BETWEEN` and `NOT BETWEEN` bound parameter types from a uniquely resolved column, including `SYMMETRIC` and `ASYMMETRIC` forms.

## v0.2.12

### Added

- Resolve typed `IN` and `NOT IN` in projections, conditionals, HAVING and lambdas, including List-valued expressions and nullable Boolean results.
- Apply `ALTER COLUMN ... DROP NOT NULL` in the offline schema catalog, including repeated actions, nullable primary keys and connected drift checks.

## v0.2.11

### Added

- Allow query-scoped YQL parameter type contracts in `sql[].analyzer.parameters` for queries without `DECLARE`, including offline analysis, connected EXPLAIN validation and generated bindings across runtime targets.

## v0.2.10

### Added

- Support typed local lambdas, `ListMap`/`ListFilter`, `ListLength`/`ListHas`, `ToDict`/`DictContains`/`DictLookup`, `UNWRAP`, empty `AsList()` in `Json::From`/`Yson::From`, and member access on `Optional<Struct>` across generated runtimes.

## v0.2.9

### Added

- Resolve named and derived SELECT sources, including joins and SELECT-backed DML, and grouped `AGGREGATE_LIST` results. Add an end-to-end booktest query across generated runtime profiles.
- Add offline type rules for the documented YQL UDF modules, including named options, resource and tagged intermediate values, callable regex/date-time functions, literal-dependent regex results, and histogram aggregates. Add representative UDF examples across generated runtime profiles.

## v0.2.8

### Added

- Support named query scripts with multiple DML statements and at most one typed SELECT or RETURNING result, using shared parameter types and one execution of the complete script. Add deletion and mixed read/write examples for every booktest runtime profile.

## v0.2.7

### Added

- Support noncorrelated scalar and tuple-key IN/NOT IN subqueries in SELECT and DML WHERE predicates, resolving inner aliases and parameters independently from the outer relation. Add read and mutation examples across generated runtime profiles.

## v0.2.6

### Added

- Resolve Boolean expressions and string concatenation consistently in projections, function arguments, local bindings and supported DML values. Add conditional `COUNT_IF` aggregates, fitting integer-literal fallbacks in COALESCE/NVL, and verified Boolean, timestamp and JSON casts.
- Support the UTC clock family and additional scalar built-ins, with a documented inventory of the upstream YQL builtin catalog and explicit prerequisites for remaining families.
- Infer YDB result names for computed SELECT columns without AS, including typed jOOQ ORDER BY references to computed aliases and implicit result names. Add authors reporting, prefix-search and export-metadata examples for every language/runtime profile.

### Fixed

- Emit only supported Rust comparison and hashing derives for rows containing YDB byte strings, including optional and list fields.

## v0.2.5

### Added

- Support static absolute TablePathPrefix pragmas in named queries and schema sources, with consistent offline and connected table resolution, preserved SQL execution context and namespaced models. Add a namespaces example for every language/runtime profile.

### Changed

- An explicit table alias is now the only qualifier accepted for that source's columns and wildcards, including queries without TablePathPrefix. Queries such as `SELECT records.id FROM records AS r`, accepted by v0.2.4, must use `r.id` to match YDB's name resolution.

### Fixed

- Preserve case-sensitive table aliases in resolved column bindings, so a JOIN using distinct aliases such as `r` and `R` cannot bind one source's column to the other source.

## v0.2.4

### Added

- Support ordinary and covering GLOBAL SYNC/ASYNC secondary indexes in CREATE TABLE and ADD/DROP INDEX migrations. Resolve `FROM table VIEW index` against the base table, retain index selection in generated SQL, and include index definitions in database discovery and schema-drift checks.
- Accept the verified integer aliases TinyInt, SmallInt, Int, Integer and BigInt while generating bindings with their canonical widths and signedness.

### Fixed

- Preserve supported declared LIMIT/OFFSET parameter types instead of forcing Uint64. Validate pagination expression types against the YDB contract, including optional integers, while retaining Uint64 inference for unresolved direct parameters. Signed and NULL values keep their server-defined behavior.

## v0.2.3

### Added

- Add `:each` SELECT queries for both Go profiles: typed sequential callbacks, cancellation and cleanup on early exit, and no generated retry loop. Methods use the supplied executor directly: native clients retain SDK materialization, while sessions and transactions stream without a generated result collection. Other language targets reject the annotation.

## v0.2.2

### Added

- Support INSERT/UPSERT SELECT without an explicit target column list, matching source columns to destinations by name. Named writes accept wildcard projections and computed columns with aliases, validate destination types, primary keys and required NOT NULL columns, and preserve omitted nullable columns on UPSERT. Explicit target lists retain positional matching and require explicit source projections.

### Fixed

- Reject positional INSERT/UPSERT SELECT queries that omit primary-key or required NOT NULL columns before generation, using the same missing-column checks as named writes. Generated serial keys and nullable non-key columns may still be omitted.
- Accept a trailing comma in Struct type declarations, including structured batch parameters and nested Struct types, while continuing to reject empty or duplicate fields.

## v0.2.1

### Added

- Support typed expressions in INSERT/UPSERT VALUES and UPDATE SET: constants, contextual NULL, target-row column references in UPDATE, scalar calls and binary numeric addition, subtraction and multiplication with parentheses. Assignments validate destination types, support Boolean comparisons, reject duplicate SET targets, and preserve existing direct-parameter inference and RETURNING APIs. Lossless integer widening follows the same rules for VALUES/SET and SELECT-backed DML. The shared expression resolver also supports this arithmetic in SELECT expressions and predicates.

## v0.2.0

### Fixed

- Expand supported SELECT and RETURNING wildcard projections into explicit column lists in generated SQL for both offline schema analysis and connected database discovery. Adding unrelated database columns after generation no longer changes the result width seen by positional decoders; local schema/model order is preserved when database validation is enabled.

### Added

- Opt in to database-assisted analysis for compile, generate and diff: discover table types directly from YDB without local schema files, or check a supplied local schema for drift, and compile original queries on the server before local query analysis without executing them. Connected queries declare parameter types explicitly with `DECLARE`; server parameter errors include an actionable hint. Connection settings support TLS, environment-provided tokens and per-request timeouts; `--no-database` keeps generation offline.

## v0.1.8

### Added

- Add public contribution, conduct, security, and pull request guidance.

## v0.1.7

### Changed

- Include retained MIT license notices for adapted sqlc and sqlc-gen-python material in source and release archives.

## v0.1.6

### Added

- Analyze typed SELECT sources consistently across read queries and DML, including computed INSERT/UPSERT SELECT projections and UPDATE/DELETE ON SELECT with name-based primary-key validation and wildcard expansion.
- Configure exact, query-set-scoped concrete function signatures under `sql[].analyzer.functions`; named and omittable arguments and AutoMap behavior are resolved offline without claiming or loading the complete server UDF registry.
- Generate Go bindings for root `Struct` parameters in both YDB Query SDK and database/sql profiles, and scalar `List` parameters in database/sql. Struct fields bind by YQL name regardless of declaration order.

### Changed

- Treat YQL `Bytes` as binary `String` and `Text` as Unicode `Utf8` throughout schema and declared nested types.
- Match explicit-target INSERT/UPSERT SELECT columns positionally, with explicit projections and optional source aliases; match UPDATE/DELETE ON SELECT columns by result name and require all primary-key fields with compatible types.
- Validate operand types in WHERE, JOIN ON, UPDATE WHERE and DELETE WHERE. Previously generated queries using server-supported String/Utf8 or Decimal/integer comparisons may now need explicit CASTs because the offline common-type resolver does not yet support those coercions. See [compatibility limits](docs/compatibility.md).

## v0.1.5

### Added

- Generate typed list-of-struct parameters for SQL batch inserts through `AS_TABLE`, with a `CreateBooks` query and runtime checks in the existing batch example. Support covers all language targets, including native and framework adapters.

## v0.1.4

### Added

- `init` supports language/runtime selection, complete configurations with comments, and generator-specific help. See the [generator options](docs/targets.md#generator-options).

## v0.1.3

### Added

- PHP `Queries::withTx(Session, string)` binds generated queries to a caller-owned transaction, preserving exact result decoding without per-query retries or automatic commits.

## v0.1.2

### Fixed

- Native Go query methods attach stack traces to returned execution, scan and Decimal validation errors while preserving error unwrapping.

## v0.1.1

### Added

- `version` checks for a newer stable release and prints an update command when available. Offline and failed checks are silent; `--no-remote` skips the check.
- `version --upgrade` downloads and verifies the latest stable release, then replaces the running executable at its real location, preserving symlinks. Automatic in-place upgrades are supported on Linux and macOS. On Windows, `version --upgrade` prints manual upgrade instructions.

### Changed

- Group example code by scenario and language; move shared builds and cross-example runtime tests to `tests/examples`.

## v0.1.0

First release of sqlc-ydb, an independent SQL-first code generator for YDB.

### Capabilities

- Offline YQL analysis and typed code generation in one executable. `generate` writes code, `compile` checks schemas and queries, and `diff` detects generated output drift. `init` creates a configuration; `version --verbose` identifies the version and source commit of a release build.
- YAML/JSON configuration with version 2 query sets. Inputs can be files, ordered lists, directories or globs. Supported migration Up sections build a local catalog without executing DDL.
- Schema analysis covers table creation, removal and renaming; column addition and removal; primary keys; nullability; and YDB serial types. Query analysis covers projections, aliases, joins, parameter inference, supported CASE expressions, CAST operations and built-in functions, UNION, grouping, ordering, LIMIT/OFFSET and parameterized writes with RETURNING. Diagnostics identify the source file, line and column.
- Typed parameters, row models and `:one`, `:many`, `:exec` query methods for **nine languages and eighteen runtime profiles**:

  | Language | Runtimes |
  | --- | --- |
  | Go | YDB Query SDK, database/sql |
  | Python | YDB Query SDK, DB-API, SQLAlchemy |
  | C++ | YDB Query SDK, userver |
  | C# | ADO.NET, Dapper |
  | Java | YDB Query SDK, JDBC, experimental jOOQ DSL |
  | Kotlin | YDB Query SDK, JDBC, Exposed |
  | TypeScript | YDB Query SDK |
  | Rust | YDB Query SDK |
  | PHP | YDB Table SDK |

- Caller-owned clients, connections and transactions. All profiles except PHP support combining several generated calls in one transaction; retry behavior and resource ownership are documented for each [target](docs/targets.md). The [support matrix](README.md#supported-targets) links to execution tests.
- Readable SQL at execution sites and query annotations as source comments. jOOQ translates the supported query subset into typed DSL operations. The five example families cover CRUD, joins, JSON filters, aggregates and schema migrations. Generated output is checked against these examples.
- A Linux/macOS installer with automatic architecture selection, SHA256 verification, pinned versions and installation without administrator privileges.
- Release archives for Linux, macOS and Windows on amd64 and arm64, with SHA256 checksums and build metadata.

### Parity with upstream sqlc

The query-first workflow is shared; configuration and generated APIs are not fully interchangeable with upstream sqlc.

| Area | First-release support |
| --- | --- |
| Core CLI | `generate`, `compile`, `diff`, `init`, `version`, help and alternate config paths. |
| Configuration | v2; only the [listed generator options](docs/compatibility.md#implemented-workflow). Type/name overrides and most upstream Go options are absent. |
| Query annotations | `:one`, `:many`, `:exec`; no affected-row counts, command-tag results, driver batches or COPY helpers. |
| Parameters | Named YQL `$parameters`, with inferred or declared types. No `sqlc.arg`, `sqlc.narg`, `sqlc.embed` or `sqlc.slice` macros. |
| Additional tooling | No `analyze`, `parse`, `fmt`, `completion`, `vet`, `verify`, `push` or `createdb`; no database-assisted analysis or cloud workflow. |
| Engines and generators | YDB and built-in generators; no PostgreSQL/MySQL/SQLite backends or external plugin protocol. |

The comparison follows upstream's [CLI](https://docs.sqlc.dev/en/latest/reference/cli.html), [configuration](https://docs.sqlc.dev/en/latest/reference/config.html), [annotations](https://docs.sqlc.dev/en/latest/reference/query-annotations.html) and [macros](https://docs.sqlc.dev/en/latest/reference/macros.html). The [compatibility contract](docs/compatibility.md) defines the supported subset.

### Known limitations

- Semantic analysis covers a subset of YQL and does not replace the server's type checker. General computed projections, CTEs/subqueries, window functions, FLATTEN, multiple result sets and many functions/types are unsupported. INSERT/UPSERT VALUES and UPDATE SET assignments require direct parameters. Schema analysis does not model views, external tables, indexes or all ALTER operations.
- Type support differs across runtimes. A query accepted by the analyzer can still be rejected by a generator. Python has synchronous adapters only. jOOQ supports a limited translation subset, with no raw-SQL fallback.
- `:many` returns a materialized collection; no streaming or pagination API is generated. Bound large reads in SQL or use the SDK directly. The C++ profiles can hold both the SDK result and decoded rows in memory. PHP detects and rejects truncated Table API results.
- PHP helpers cannot join a caller-owned transaction with the pinned SDK's raw result contract. TypeScript Timestamp values use millisecond-precision `Date`. Numbers in JSON results are subject to JavaScript's numeric precision limits. See the language guides for exact type and transaction contracts.
- Obsolete generated files are reported for manual removal in current output directories. Changing an output directory or removing a generator requires cleaning up its previous output directory. Files are never automatically deleted.

### Deliberate exclusions

- Other database engines and external engine/codegen plugins: this executable owns YDB analysis and its built-in adapters.
- ORM entity/CRUD generation for Hibernate, Spring JPA or linq2db: inferring an entity lifecycle from SQL projections would change the query-first contract. Dapper and SQLAlchemy adapters execute named SQL queries; they do not infer ORM models.
- JavaScript output: TypeScript is the supported target for typed Node.js code.
- `:execrows`: the selected YDB APIs cannot provide its affected-row-count contract. It is rejected rather than replaced with a fabricated count.
