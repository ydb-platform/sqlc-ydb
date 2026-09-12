# Changelog

## Unreleased

- `version` checks for a newer stable release and prints an update command when available. Offline and failed checks are silent; `--no-remote` skips the check.
- `self-update` downloads and verifies the latest stable release, then replaces the running executable at its real location, preserving symlinks.

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
