# Context

sqlc-ydb generates typed application code from YQL for YDB in a standalone Go binary. It follows the familiar sqlc workflow while maintaining its own source and release cycle. Coverage limits are in [compatibility](../docs/compatibility.md).

Read [decisions](decisions.md) before changing the analysis/generation boundary or simplifying SDK-specific code.

## Code map

| Area | Responsibility |
| --- | --- |
| `cmd/sqlc-ydb`, `internal/cli` | Commands, pipeline orchestration, output validation and file IO |
| `internal/config` | Strict version 2 config parsing, supported options and defaults |
| `internal/source` | Input ordering and migration Up sections |
| `internal/analyzer` | Direct YQL parse contexts, catalog evolution, name/type resolution and diagnostics |
| `internal/yql/builtins` | Strict supported YQL function, cast and common-type rules |
| `internal/model` | Resolved query/catalog data, type equality and diagnostic formatting |
| `internal/codegen/{golang,python,cpp,csharp,java,kotlin,typescript,rust,php}` | Language naming and SDK-specific bindings, decoding and source rendering |
| `internal/endtoend` | CLI fixtures, expected diagnostics and generated golden files |
| `examples` | SQL, configuration and code grouped by example family, then language and runtime |
| `tests/examples` | Shared runtime builds and sequential cross-example acceptance tests |
| `.github/workflows` | Offline verification and sequential acceptance steps per host |

## Public sources of truth

- [README](../README.md): build and first generation.
- [Compatibility](../docs/compatibility.md): supported config, queries, schema migrations, intentional exclusions and output ownership.
- [Targets](../docs/targets.md), [C++](../docs/cpp.md), [C#](../docs/csharp.md), [Java](../docs/java.md), [Kotlin](../docs/kotlin.md), [TypeScript](../docs/typescript.md), [Rust](../docs/rust.md), [PHP](../docs/php.md): generated API and runtime contracts.
- [Installation](../docs/installation.md): release artifacts, checksum and version checks.
- [Changelog](../CHANGELOG.md): pending Unreleased entries and published stable versions.
- [Provenance](../docs/provenance.md): source attribution, adaptations and licenses.
- [History](../docs/history.md): upstream YDB proposals and the standalone decision.

## Maintainer sources of truth

- [Architecture](architecture.md): current stages and responsibilities.
- [Development](development.md): contributor commands, CI and runtime validation.
- [Roadmap](roadmap.md): shared macros and deferred database-assisted analysis.
- [Release plan](release-plan.md): publication checks, ydb.tech documentation, production-corpus validation and consumer pilots.
- [Release operations](releasing.md): packaging, dry runs, publication and recovery.
- [SDK evidence](sdk-evidence.md), [YQL evidence](yql-evidence.md), [C++ development](cpp-development.md) and [C# SDK evidence](csharp-sdk-evidence.md): source-level implementation evidence and build constraints.

The repository is `ydb-platform/sqlc-ydb`; the Go module is `github.com/ydb-platform/sqlc-ydb` and the executable is `sqlc-ydb`. Check the current Git branch, remote and worktree before any publication.
