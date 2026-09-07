# Initial standalone implementation

The module path follows the actual repository: `github.com/ydb-platform/sqlc-engine-ydb`.
The executable is `sqlc-ydb`. The only SQL dialect is YQL for YDB.

Pipeline: config and source loading → ANTLR YQL parse tree → semantic analyzer →
`model.AnalysisResult` → built-in Go/Python generators → files.
There is no intermediate AST, plugin protocol, external generator, or sqlc dependency.

Shared interfaces for parallel implementation:

- `analyzer.Analyze(schema, queries []model.Source) (*model.AnalysisResult, error)`.
- `golang.Generate(*model.AnalysisResult, golang.Options) ([]model.File, error)`.
  Options: `Package, Runtime string; EmitJSONTags, EmitInterface, EmitEmptySlices bool`.
  Runtime values: `ydb` and `database/sql`.
- `python.Generate(*model.AnalysisResult, python.Options) ([]model.File, error)`.
  Options: `Package, Runtime string; EmitSyncQuerier, EmitAsyncQuerier bool`.
  Runtime values: `ydb`, `dbapi`, `sqlalchemy`.

`AnalyzedQuery.SQL` contains executable YQL, including required declarations.
Parameter names do not include `$`. Column and parameter types must be resolved.
Unsupported syntax/types/commands must fail explicitly; never substitute `Any`.
Generators consume only semantic results and do not walk the ANTLR tree.
Generated files must use safe language string literals and deterministic ordering.

Each implementation owns its directory; root owns this model, config, CLI and integration.
