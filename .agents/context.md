# Context

sqlc-ydb generates typed application code from YQL for YDB in a standalone Go
binary. It follows the familiar sqlc workflow while maintaining its own source
and release cycle. It is a development implementation with explicit coverage
limits; successful parsing alone does not establish semantic support.

Read [decisions](decisions.md) before changing the analysis/generation boundary
or simplifying SDK-specific code.

## Code map

| Area | Responsibility |
| --- | --- |
| `cmd/sqlc-ydb`, `internal/cli` | Commands, pipeline orchestration, output validation and file IO |
| `internal/config` | Strict config parsing, supported options and defaults |
| `internal/source` | Input ordering and migration Up sections |
| `internal/analyzer` | Direct YQL parse contexts, catalog evolution, name/type resolution and diagnostics |
| `internal/yql/builtins` | Strict supported YQL function, cast and common-type rules |
| `internal/model` | Resolved query/catalog data shared by generators; not an AST |
| `internal/codegen/{golang,python,cpp,csharp,java,javascript,rust,php}` | Language naming and SDK-specific bindings, decoding and source rendering |
| `internal/endtoend` | CLI fixtures, expected diagnostics and generated golden files |
| `testdata/legacy-ydb` | Immutable checksummed fork sources and semantic corpus expectations |
| `examples` | All upstream example families adapted for YDB; shared language dependencies and sequential live tests |
| `.github/workflows` | Offline verification and sequential acceptance steps per host |

## Sources of truth

- [README](../README.md): build and first generation.
- [Compatibility](../docs/compatibility.md): supported config, queries, schema
  migrations, intentional exclusions and output ownership.
- [Architecture](../docs/architecture.md): current stages and responsibilities.
- [Targets](../docs/targets.md), [C++](../docs/cpp.md), [C#](../docs/csharp.md),
  [Java](../docs/java.md), [JavaScript](../docs/javascript.md),
  [Rust](../docs/rust.md), [PHP](../docs/php.md): generated API and runtime contracts.
- [Development](../docs/development.md): commands and validation requirements.
- [Roadmap](../docs/roadmap.md): shared macros and deferred database-assisted analysis.
- [Release plan](../docs/release-plan.md): release gates, ydb.tech documentation,
  external query corpus, and user-owned SDK reviews/consumer pilots.
- [Releasing](../docs/releasing.md): packaging, dry runs and publication workflow.
- [Changelog](../CHANGELOG.md): pending Unreleased entries and published stable versions.
- [Provenance](../docs/provenance.md): upstream references and inspected SDK sources.

The Go module name is authoritative in `go.mod`; the executable is `sqlc-ydb`.
Historical repository/module names can differ. Do not infer a rename or restore
the old engine-plugin dependencies from an archive branch. Check the current
Git branch, remote and worktree before any publication.
