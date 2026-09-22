# Compiler roadmap

Current behavior is in [compatibility](../docs/compatibility.md), stage ownership in [architecture](architecture.md), and release gates in [the release plan](release-plan.md).

## Shared macros

Macro processing must run once per `sql` entry through `analyzer.Analyze`, before any generator. Keep executable SQL, resolved metadata and original diagnostic positions together. Use tokens and parse contexts so rewrites preserve strings, comments, quoted identifiers and local bindings.

Implement in this order:

1. `sqlc.arg` and `sqlc.narg`: lower to YDB parameters, infer types, preserve `narg` nullability, and diagnose conflicts with declarations or local bindings. Cover repeated uses and preserve existing public parameter names.
2. `sqlc.embed`: expand projections against the catalog and retain result grouping.
3. `sqlc.slice`: define YDB `List<T>` semantics and verify each runtime's binding.

Resolve each rewrite against the catalog and validate the final YQL. Record external parameter occurrences and update their ranges after rewrites. Driver placeholder rendering, such as SQLAlchemy's `:name`, then uses those ranges without rediscovering parameters.

Acceptance requires matching diagnostics from `compile`, `generate` and `diff`; independent SQL/metadata assertions; and at least two language outputs from one macro fixture. Cover Unicode, CRLF, escaped identifiers, comments, macro-like string contents and local bindings. Existing SQL byte round-trip tests must pass. Runtime placeholder tests must assert the final SQL seen by YDB.

Reference: [upstream sqlc macros](https://docs.sqlc.dev/en/latest/reference/macros.html).

## Database-assisted analysis

Live table discovery, local-schema drift checks and non-executing server compilation are implemented; see [database-assisted analysis](../docs/database-analysis.md). The local semantic analyzer still owns query result inference. Connected analysis must not execute user queries or apply migrations.

Typed result metadata for arbitrary expressions remains future work. Public EXPLAIN responses do not expose result column types. A live SELECT with LIMIT 0 can return them, but it is query execution and requires parameter values even when declarations are present. Any future probing mode needs an explicit read-only execution and parameter-value contract, query rewriting that preserves column order, and validation across supported server versions. Do not parse the private optimizer AST or fabricate parameter values as a fallback.
