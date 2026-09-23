# Compiler roadmap

Current behavior is in [compatibility](../docs/compatibility.md), stage ownership in [architecture](architecture.md), and release gates in [the release plan](release-plan.md).

## Query compatibility priorities

The following issues track planned work; their examples and acceptance criteria define the scope. Name-mapped INSERT/UPSERT SELECT, trailing Struct commas, verified integer aliases and compatible LIMIT/OFFSET types, secondary-index metadata with VIEW selection, and static absolute TablePathPrefix resolution are implemented; their original scope is in [#25](https://github.com/ydb-platform/sqlc-ydb/issues/25), [#26](https://github.com/ydb-platform/sqlc-ydb/issues/26), [#27](https://github.com/ydb-platform/sqlc-ydb/issues/27), [#28](https://github.com/ydb-platform/sqlc-ydb/issues/28), and [#29](https://github.com/ydb-platform/sqlc-ydb/issues/29). The [compatibility contract](../docs/compatibility.md#current-analyzer-coverage) records their boundaries. Subsequent changes should remain separate, reviewable PRs.

Shared expressions and scalar conversions ([#31](https://github.com/ydb-platform/sqlc-ydb/issues/31)), Go streaming callbacks ([#35](https://github.com/ydb-platform/sqlc-ydb/issues/35)), and the noncorrelated scalar/tuple IN subquery portion of [#32](https://github.com/ydb-platform/sqlc-ydb/issues/32) are implemented. The remaining planned work is:

| Order | Planned capability | Tracking |
| --- | --- | --- |
| 2 | ALTER COLUMN DROP NOT NULL | [#30](https://github.com/ydb-platform/sqlc-ydb/issues/30) |
| 4 | Derived FROM/JOIN sources, named SELECT bindings and collection aggregation | [#32](https://github.com/ydb-platform/sqlc-ydb/issues/32) |
| 5 | Multi-statement query scripts with at most one typed result | [#33](https://github.com/ydb-platform/sqlc-ydb/issues/33) |
| 6 | Typed lambdas and JSON/Yson collection transformations | [#34](https://github.com/ydb-platform/sqlc-ydb/issues/34) |

Semantic changes belong in the shared analyzer. Acceptance requires focused diagnostics, generated code compiled against pinned SDKs, and sequential local-ydb execution that asserts values, types and transaction behavior where applicable. Passing generation alone is not runtime acceptance.

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

Typed result metadata for arbitrary expressions remains future work. Public EXPLAIN responses do not expose result column types. A live SELECT with LIMIT 0 can return them, but it is query execution and requires parameter values even when declarations are present. With explicit DECLARE as the prerequisite, use those declared types after QueryService EXPLAIN validation rather than depending on deprecated ScriptingService or parsing private plan/AST data. LIMIT 1 can evaluate content-sensitive expressions and reject otherwise type-correct probe values; the fixed Ensure regression demonstrates this difference. Any future probing mode needs an explicit read-only execution and parameter-value contract, query rewriting that preserves column order, and validation across supported server versions. Do not parse the private optimizer AST or fabricate parameter values as a fallback.
