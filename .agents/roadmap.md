# Compiler roadmap

Current behavior is in [compatibility](../docs/compatibility.md), stage ownership in
[architecture](architecture.md), and release gates in [the release plan](release-plan.md).

## Shared macros

Macro processing must run once per `sql` entry through `analyzer.Analyze`, before
any generator. Keep executable SQL, resolved metadata and original diagnostic
positions together. Use tokens and parse contexts so rewrites preserve strings,
comments, quoted identifiers and local bindings.

Implement in this order:

1. `sqlc.arg` and `sqlc.narg`: lower to YDB parameters, infer types, preserve
   `narg` nullability, and diagnose conflicts with declarations or local bindings.
   Cover repeated uses and preserve existing public parameter names.
2. `sqlc.embed`: expand projections against the catalog and retain result grouping.
3. `sqlc.slice`: define YDB `List<T>` semantics and verify each runtime's binding.

Resolve each rewrite against the catalog and validate the final YQL. Record
external parameter occurrences and update their ranges after rewrites. Driver
placeholder rendering, such as SQLAlchemy's `:name`, then uses those ranges
without rediscovering parameters.

Acceptance requires matching diagnostics from `compile`, `generate` and `diff`;
independent SQL/metadata assertions; and at least two language outputs from one
macro fixture. Cover Unicode, CRLF, escaped identifiers, comments, macro-like
string contents and local bindings. Existing SQL byte round-trip tests must pass.
Runtime placeholder tests must assert the final SQL seen by YDB.

Reference: [upstream sqlc macros](https://docs.sqlc.dev/en/latest/reference/macros.html).

## Database-assisted analysis

Deferred until a concrete query or feature needs server metadata. First establish
which YDB APIs expose schema, parameter and result types without executing user
queries. Then define explicit opt-in configuration, schema-drift handling,
timeouts, cache invalidation and reconciliation with local analysis.

Offline generation remains the default. Compilation must not apply migrations
to an application database. All generators consume the same enriched result;
unavailable requested metadata produces an error. Validate with sequential
live-YDB tests as described in [development](development.md).
