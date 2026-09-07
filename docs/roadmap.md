# Compiler roadmap

This page describes planned compiler work. Current stages are in
[architecture](architecture.md); implemented behavior is in
[compatibility](compatibility.md).
Release delivery, user documentation and acceptance responsibilities are tracked
in [the release plan](release-plan.md).

## 1. Schema evolution: implemented subset

The implemented migration behavior is recorded in
[schema migration coverage](compatibility.md#schema-migration-coverage).
General ALTER COLUMN, indexes, views and other schema objects remain future work.
Further schema operations should be added with corresponding YDB semantics and
fixtures, not accepted merely because the parser recognizes them.

## 2. Shared compiler and macros: planned

Keep one compilation entry point per `sql` configuration entry. It consumes
loaded schema/query sources and returns the semantic compilation result. Every
selected generator receives that same completed result. `compile`, `generate`
and `diff` use the same entry point; `compile` stops before generation.

The existing `analyzer.Analyze` entry point can own this coordination. A separate
`internal/compiler` package is optional: extract it only if macro processing or
other responsibilities make that boundary useful. Keep the existing
`model.AnalysisResult`; avoid an empty wrapper or a package added for naming
consistency alone. A stateful `Compiler` object is only needed if later caching
or server resources justify its lifecycle.

Macro processing belongs inside this compilation boundary. It happens once per
compilation unit, independent of the number or language of its generators. This
does not mean all macros can be resolved in one pass before analysis:

1. Recognize supported sqlc macro syntax using tokens, preserving strings,
   comments, quoted identifiers and source positions. Lower syntax that YQL
   cannot parse into YQL parameters while recording macro metadata.
2. Build the catalog and analyze the YQL parse contexts. Resolve macro-dependent
   names, types, optional parameters, result grouping and list element types.
3. Finalize executable YQL and semantic metadata once. If a rewrite changes
   syntax, validate the rewritten query before passing it to generators.

Implement `sqlc.arg` and `sqlc.narg` first: map names to YDB parameters, preserve
`narg` nullability, reconcile explicit `DECLARE` statements, infer required
types and diagnose conflicts. Cover repeated uses and collisions with existing
parameters/local bindings. Do not silently rename existing public parameters.

Then address `sqlc.embed`: projection expansion depends on the catalog and must
retain grouping metadata for generated models. It is not only string
replacement. Plan `sqlc.slice` after defining its YDB `List<T>` parameter
semantics and each runtime's list binding. Unsupported combinations must fail;
there is no need to copy another database's placeholder expansion strategy.

Upstream macro behavior is the reference:
[sqlc macros](https://docs.sqlc.dev/en/latest/reference/macros.html).

Runtime placeholder rendering is a separate last step: SQLAlchemy's `:name` is
different from executable YQL's `$name`. During shared macro implementation, record
resolved external parameter occurrences and their roles/ranges alongside the
SQL, so adapters can render their syntax without re-lexing or rediscovering
parameters. Update ranges after shared rewrites. Declarations and local bindings
must not be mistaken for external parameter occurrences. These ranges and
macro/result metadata are not a recursive AST.

Acceptance criteria:

- One compilation result feeds several generators; adding another generator
  never repeats macro resolution or semantic analysis.
- `compile` reports the same macro errors as `generate` and `diff`.
- Diagnostics refer to original SQL even after expansion; tests cover Unicode,
  CRLF, escaped identifiers, comments and macro-like text inside strings.
- Compiler tests assert rewritten SQL and semantic metadata independently of
  any generator. End-to-end tests verify at least two language outputs from the
  same macro fixture, and existing exact SQL-literal round-trip tests remain.
- Runtime-specific placeholder tests assert the SQL ultimately seen by YDB,
  including parameter-like text in strings/comments and YQL local bindings.

## 3. Database-assisted analysis: deferred

Implement when a concrete feature request or a query the local analyzer cannot
resolve justifies it. Local generation remains the default, with no implicit
connection to a configured or developer database.

First investigate the current YDB APIs for schema description and query type
metadata without executing user queries. Record which parameter/result types
they actually expose, what schema state they require and their limitations.
Do not assume server EXPLAIN/prepare can replace the local analyzer.

Then define explicit opt-in configuration, schema-drift behavior, type metadata
reconciliation, diagnostics, timeouts and cache invalidation. If an isolated
database needs schema preparation, treat that as an explicit separate mode;
compilation must not apply migrations to an arbitrary application database.
All generators consume the same enriched semantic result.

Acceptance includes reproducible offline behavior, clear errors when requested
server metadata is unavailable, and sequential live-YDB integration tests.
Different local-ydb images and runtime suites stay sequential on each host.
